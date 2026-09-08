// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package slack implements a Slack Incoming Webhook notification channel
// for the tickraft alerting pipeline.
//
// A Channel POSTs alert events as Slack Block Kit JSON to a configured
// Incoming Webhook endpoint. It integrates a circuit breaker (to avoid
// hammering a degraded endpoint) and a retry mechanism with exponential
// backoff and full jitter (to tolerate transient failures). 5xx
// responses and network errors are retried; 4xx responses and non-"ok"
// API responses fail fast.
package slack

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/bytedance/sonic"
	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/circuitbreaker"
	"github.com/tickraft/tickraft/pkg/i18n"
	"github.com/tickraft/tickraft/pkg/prism/alert"
	"github.com/tickraft/tickraft/pkg/prism/alert/template"
	"github.com/tickraft/tickraft/pkg/prism/channel"
	"github.com/tickraft/tickraft/pkg/retry"

	"github.com/tickraft/tickraft/pkg/prism/channel/format"
)

// okBody is the literal response body Slack returns for a successful
// Incoming Webhook delivery.
const okBody = "ok"

// Channel sends alert notifications to Slack via Incoming Webhooks. It
// satisfies the alert.Channel interface.
type Channel struct {
	cfg        Config
	httpClient *http.Client
	retry      *retry.Retry
	circuit    *circuitbreaker.CircuitBreaker
	logger     *zap.Logger
	formatter  i18n.Formatter
	library    template.Library
	registry   i18n.Registry
}

// Compile-time assertion that Channel implements alert.Channel.
var _ alert.Channel = (*Channel)(nil)

// typeName is the canonical channel type identifier used by Name and by
// delivery tracking records.
const typeName = "slack"

// Name implements alert.Channel.
func (c *Channel) Name() string { return typeName }

// Send implements alert.Channel. It formats the alert as a Slack Block
// Kit message and POSTs it to the configured Incoming Webhook URL.
//
// The send is protected by a circuit breaker: when the breaker is open
// Send short-circuits with channel.ErrCircuitOpen. Transient failures
// (5xx and network errors) are retried with exponential backoff and full
// jitter; 4xx responses and Slack API rejections (a non-"ok" body) fail
// immediately without retrying. On success the breaker is reset; on
// failure the breaker records a failure and a *channel.SendError is
// returned indicating whether the failure is retryable.
func (c *Channel) Send(ctx context.Context, evt alert.Event) error {
	if c.cfg.WebhookURL == "" {
		return channel.NewSendError(c.Name(), false, errors.New("slack: webhook url not configured"))
	}
	if !c.circuit.Allow() {
		c.logger.Debug("slack send suppressed: circuit breaker open")
		return channel.ErrCircuitOpen
	}
	body, err := c.formatPayload(ctx, evt)
	if err != nil {
		c.circuit.RecordFailure()
		return channel.NewSendError(c.Name(), false, fmt.Errorf("format payload: %w", err))
	}
	err = c.retry.Do(ctx, func() error {
		return c.doSend(ctx, body)
	})
	if err != nil {
		c.circuit.RecordFailure()
		c.logger.Warn("slack send failed",
			zap.Error(err),
			zap.Bool("retryable", isRetryableSendErr(err)),
		)
		return channel.NewSendError(c.Name(), isRetryableSendErr(err), err)
	}
	c.circuit.RecordSuccess()
	return nil
}

// doSend performs a single HTTP POST with the pre-marshaled body. It
// returns an *httpError for non-2xx responses, non-"ok" bodies, and
// network errors so the retry predicate can classify retryability.
func (c *Channel) doSend(ctx context.Context, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return &httpError{statusCode: 0, err: err}
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()
	respBody, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return &httpError{
			statusCode: resp.StatusCode,
			err:        fmt.Errorf("read response: %w", readErr),
		}
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if strings.TrimSpace(string(respBody)) == okBody {
			return nil
		}
		return &httpError{
			statusCode: resp.StatusCode,
			err:        fmt.Errorf("slack api rejected payload: %s", strings.TrimSpace(string(respBody))),
		}
	}
	return &httpError{
		statusCode: resp.StatusCode,
		err: fmt.Errorf("status %d from %s: %s",
			resp.StatusCode, c.cfg.WebhookURL, strings.TrimSpace(string(respBody))),
	}
}

// httpError wraps an HTTP failure with the response status code so the
// retry predicate can distinguish retryable (5xx, network) from
// non-retryable (4xx, api rejection) failures. A statusCode of zero
// denotes a network error.
type httpError struct {
	statusCode int
	err        error
}

// Error implements the error interface.
func (e *httpError) Error() string {
	if e.statusCode > 0 {
		return fmt.Sprintf("slack: status %d: %v", e.statusCode, e.err)
	}
	return fmt.Sprintf("slack: %v", e.err)
}

// Unwrap returns the underlying error.
func (e *httpError) Unwrap() error { return e.err }

// StatusCode reports the HTTP status of the failed response.
// Zero denotes a network error. Consumed by the delivery tracking
// decorator via tracking.ResponseCodeOf.
func (e *httpError) StatusCode() int { return e.statusCode }

// isRetryableSendErr reports whether err represents a retryable Slack
// failure. Network errors (statusCode 0) and 5xx responses are
// retryable; 4xx responses and Slack API rejections (2xx with non-"ok"
// body, whose status code is < 500) are not.
func isRetryableSendErr(err error) bool {
	var he *httpError
	if errors.As(err, &he) {
		return he.statusCode == 0 || he.statusCode >= 500
	}
	return false
}

// payload is the JSON body sent to a Slack Incoming Webhook.
type payload struct {
	Text    string  `json:"text"`
	Channel string  `json:"channel,omitempty"`
	Blocks  []block `json:"blocks"`
}

// block is a single Slack Block Kit block.
type block struct {
	Type   string       `json:"type"`
	Text   *textObject  `json:"text,omitempty"`
	Fields []textObject `json:"fields,omitempty"`
}

// textObject is a Slack text composition object.
type textObject struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// formatPayload builds a Slack-compatible JSON payload from the alert
// event. It uses the shared format Render helper to produce a canonical
// Message (via template Library or i18n Formatter), then adapts it into
// a Slack Block Kit payload: a plain-text summary (text), a header block
// with the message title, and a section block whose fields are derived
// from the canonical Message fields. The optional Channel override is
// applied at the payload level when set.
func (c *Channel) formatPayload(ctx context.Context, evt alert.Event) ([]byte, error) {
	msg := format.Render(ctx, evt, format.RenderOptions{
		Formatter:       c.formatter,
		Library:         c.library,
		Registry:        c.registry,
		Logger:          c.logger,
		FrontendBaseURL: c.cfg.FrontendBaseURL,
		Scope:           c.cfg.Scope,
	})

	p := payload{
		Text: msg.Description,
		Blocks: []block{
			{Type: "header", Text: &textObject{Type: "plain_text", Text: msg.Title}},
			{Type: "section", Fields: buildSectionFields(msg)},
		},
	}
	if c.cfg.Channel != "" {
		p.Channel = c.cfg.Channel
	}
	return sonic.Marshal(p)
}

// buildSectionFields converts the canonical Message fields into Slack Block
// Kit section fields. Field keys are sorted for deterministic output, empty
// values are omitted, and a non-empty AssetLink is appended as a final
// field.
func buildSectionFields(msg format.Message) []textObject {
	keys := make([]string, 0, len(msg.Fields))
	for k := range msg.Fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fields := make([]textObject, 0, len(keys)+1)
	for _, k := range keys {
		v := msg.Fields[k]
		if v == "" {
			continue
		}
		fields = append(fields, textObject{Type: "mrkdwn", Text: fmt.Sprintf("*%s:* %s", k, v)})
	}
	if msg.AssetLink != "" {
		fields = append(fields, textObject{Type: "mrkdwn", Text: fmt.Sprintf("*Asset Link:* %s", msg.AssetLink)})
	}
	return fields
}
