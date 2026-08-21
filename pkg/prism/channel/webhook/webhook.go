// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package webhook implements an HTTP webhook notification channel for the
// tickraft alerting pipeline.
//
// A Channel POSTs alert events as JSON to a configured endpoint. It
// integrates a circuit breaker (to avoid hammering a degraded endpoint)
// and a retry mechanism with exponential backoff and full jitter (to
// tolerate transient failures). 5xx responses and network errors are
// retried; 4xx responses fail fast.
package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/circuitbreaker"
	"github.com/tickraft/tickraft/pkg/prism/alert"
	"github.com/tickraft/tickraft/pkg/prism/channel"
	"github.com/tickraft/tickraft/pkg/retry"
	"github.com/tickraft/tickraft/pkg/types"
)

// Channel sends alert notifications as JSON POST requests to a configured
// HTTP endpoint. It satisfies the alert.Channel interface.
type Channel struct {
	cfg     Config
	headers map[string]string
	client  *http.Client
	logger  *zap.Logger
	retry   *retry.Retry
	breaker *circuitbreaker.CircuitBreaker
}

// Compile-time assertion that Channel implements alert.Channel.
var _ alert.Channel = (*Channel)(nil)

// Name implements alert.Channel.
func (c *Channel) Name() string { return types.ChannelWebhook }

// Send implements alert.Channel. It marshals the alert as JSON and
// POSTs it to the configured URL with the configured headers. The
// Content-Type header defaults to "application/json" unless overridden
// via WithHeaders.
//
// The send is protected by a circuit breaker: when the breaker is open
// Send short-circuits with channel.ErrCircuitOpen. Transient failures
// (5xx and network errors) are retried with exponential backoff and full
// jitter; 4xx responses fail immediately without retrying. On success the
// breaker is reset; on failure the breaker records a failure and a
// *channel.SendError is returned indicating whether the failure is
// retryable.
//
// A panic in any sub-operation is isolated and returned as a
// non-retryable SendError so the prism alert engine is not crashed.
// Context cancellations and marshal failures (programming errors) do not
// count against the circuit breaker.
func (c *Channel) Send(ctx context.Context, evt alert.Event) (err error) {
	defer func() {
		if r := recover(); r != nil {
			c.breaker.RecordFailure()
			c.logger.Error("webhook send panicked",
				zap.Any("panic", r),
				zap.Stack("stack"),
			)
			err = channel.NewSendError(c.Name(), false,
				fmt.Errorf("webhook: panic: %v", r))
		}
	}()

	if c.cfg.URL == "" {
		return channel.NewSendError(c.Name(), false, errors.New("webhook: url not configured"))
	}
	if !c.breaker.Allow() {
		c.logger.Debug("webhook send suppressed: circuit breaker open")
		return channel.ErrCircuitOpen
	}
	body, err := json.Marshal(evt)
	if err != nil {
		// Marshal failures are programming errors, not transient
		// server failures. Do not penalise the circuit breaker.
		return channel.NewSendError(c.Name(), false, fmt.Errorf("marshal alert: %w", err))
	}
	err = c.retry.Do(ctx, func() error {
		return c.doSend(ctx, body)
	})
	if err == nil {
		c.breaker.RecordSuccess()
		return nil
	}

	// Context cancellations are client-initiated; do not penalise the
	// circuit breaker for them.
	if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		c.breaker.RecordFailure()
	}
	retryable := isRetryableSendErr(err)
	c.logger.Warn("webhook send failed",
		zap.Error(err),
		zap.Bool("retryable", retryable),
	)
	return channel.NewSendError(c.Name(), retryable, err)
}

// doSend performs a single HTTP POST with the pre-marshaled body. It
// returns an *httpError for non-2xx responses and network errors so the
// retry predicate can classify retryability.
func (c *Channel) doSend(ctx context.Context, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return &httpError{statusCode: 0, err: err}
	}
	defer func() {
		// ignored because: draining the body allows the underlying connection to
		// be reused by the HTTP keepalive pool; the discard target means the
		// bytes are intentionally unobserved.
		_, _ = io.Copy(io.Discard, resp.Body)
		// ignored because: deferred response body close on the HTTP client path;
		// close errors are not actionable here since the response has already
		// been processed above.
		_ = resp.Body.Close()
	}()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	return &httpError{
		statusCode: resp.StatusCode,
		err:        fmt.Errorf("status %d from %s", resp.StatusCode, c.cfg.URL),
	}
}

// httpError wraps an HTTP failure with the response status code so the
// retry predicate can distinguish retryable (5xx, network) from
// non-retryable (4xx) failures. A statusCode of zero denotes a network
// error.
type httpError struct {
	statusCode int
	err        error
}

// Error implements the error interface.
func (e *httpError) Error() string {
	if e.statusCode > 0 {
		return fmt.Sprintf("webhook: status %d: %v", e.statusCode, e.err)
	}
	return fmt.Sprintf("webhook: %v", e.err)
}

// Unwrap returns the underlying error.
func (e *httpError) Unwrap() error { return e.err }

// isRetryableSendErr reports whether err represents a retryable webhook
// failure. Network errors (statusCode 0) and 5xx responses are retryable;
// 4xx responses and any non-httpError are not.
func isRetryableSendErr(err error) bool {
	var he *httpError
	if errors.As(err, &he) {
		return he.statusCode == 0 || he.statusCode >= 500
	}
	return false
}
