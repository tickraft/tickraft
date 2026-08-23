// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package webhook provides an HTTP callback executor that sends HTTP requests
// to external endpoints.
package webhook

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/bytedance/sonic"
	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/executor"
	"github.com/tickraft/tickraft/pkg/executor/internal/deadline"
	"github.com/tickraft/tickraft/pkg/executor/internal/httputil"
	"github.com/tickraft/tickraft/pkg/httpx"
	"github.com/tickraft/tickraft/pkg/types"
)

// Executor performs HTTP callback execution.
type Executor struct {
	client *http.Client
	logger *zap.Logger
}

// Option configures the webhook executor.
type Option interface {
	apply(*Executor)
}

// loggerOption sets the structured logger.
type loggerOption struct {
	logger *zap.Logger
}

func (o loggerOption) apply(e *Executor) {
	if o.logger != nil {
		e.logger = o.logger
	}
}

// WithLogger sets the structured logger.
func WithLogger(logger *zap.Logger) Option { return loggerOption{logger: logger} }

// httpClientOption sets the HTTP client.
type httpClientOption struct {
	client *http.Client
}

func (o httpClientOption) apply(e *Executor) { e.client = o.client }

// WithHTTPClient sets the HTTP client.
func WithHTTPClient(client *http.Client) Option { return httpClientOption{client: client} }

// New creates a new webhook executor.
//
// Per-request timeouts are controlled by the caller's context (this executor
// applies req.Timeout on top; the runner's lifecycle and the remediation
// operator wrap every call too). The pooled client only carries the defensive
// httputil.HardTimeout ceiling.
func New(options ...Option) *Executor {
	e := &Executor{
		client: httpx.NewPoolClient(httpx.Config{Timeout: httputil.HardTimeout}),
		logger: zap.NewNop(),
	}
	for _, o := range options {
		o.apply(e)
	}
	return e
}

// Name returns the executor name identifier.
func (e *Executor) Name() string { return string(types.ExecutorWebhook) }

// Capabilities returns the executor capability bitmask.
func (e *Executor) Capabilities() executor.Capability { return executor.CapNotify }

// config holds the webhook-specific configuration parsed from Config.
type config struct {
	Method  string              `json:"method"`
	URL     string              `json:"url"`
	Headers map[string][]string `json:"headers,omitempty"`
	Body    string              `json:"body,omitempty"`
	// ExpectStatus is the required HTTP response status code. Zero (the
	// default) accepts any 2xx status as normal, sharing the judgment of
	// httputil.ResponseStatus with the http executor.
	ExpectStatus int `json:"expect_status,omitempty"`
}

// parseConfig decodes and normalizes the executor config JSON: it rejects
// empty configs and missing URLs and defaults the HTTP method to POST.
func parseConfig(raw string) (*config, error) {
	if raw == "" {
		return nil, fmt.Errorf("webhook: executor config is empty")
	}
	var cfg config
	if err := sonic.Unmarshal([]byte(raw), &cfg); err != nil {
		return nil, fmt.Errorf("webhook: parse config: %w", err)
	}
	if cfg.URL == "" {
		return nil, fmt.Errorf("webhook: url is required")
	}
	if cfg.Method == "" {
		cfg.Method = http.MethodPost
	}
	return &cfg, nil
}

// Execute runs the webhook task and returns the result.
//
// Panic isolation: a defer-recover catches any unexpected panic from the
// HTTP client or config parsing, logs it at Error level, and returns an
// abnormal Result so the Runner can record the failure and optionally retry.
func (e *Executor) Execute(ctx context.Context, req executor.ExecutionRequest) (result *executor.Result, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			e.logger.Error("webhook executor panic recovered",
				zap.Int64("asset_id", req.AssetID),
				zap.Any("panic", rec),
				zap.Stack("stack"),
			)
			r := executor.AcquireResult()
			r.Status = types.AssetStatusAbnormal
			r.ErrorMsg = fmt.Sprintf("webhook executor panic: %v", rec)
			result = r
			err = nil
		}
	}()

	cfg, err := parseConfig(req.Config)
	if err != nil {
		return nil, err
	}

	// Timeout control. The caller's context is the single source: through
	// the runner's lifecycle it already carries req.Timeout as its
	// deadline. The fallback below only serves direct callers whose
	// context has no deadline.
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := deadline.Fallback(ctx, timeout)
	defer cancel()

	start := time.Now()

	// Build request.
	var body io.Reader
	if cfg.Body != "" {
		body = bytes.NewBufferString(cfg.Body)
	}
	httpReq, err := http.NewRequestWithContext(ctx, cfg.Method, cfg.URL, body)
	if err != nil {
		return nil, fmt.Errorf("webhook: build request: %w", err)
	}

	// Set headers.
	for k, vs := range cfg.Headers {
		for _, v := range vs {
			httpReq.Header.Add(k, v)
		}
	}

	// Execute request.
	resp, err := e.client.Do(httpReq)
	duration := time.Since(start)

	if err != nil {
		r := executor.AcquireResult()
		r.Status = types.AssetStatusAbnormal
		r.ErrorMsg = err.Error()
		r.Duration = duration
		r.Metrics["response_ms"] = float64(duration.Milliseconds())
		return r, nil
	}
	defer func() { _ = resp.Body.Close() }() // best-effort close, error not actionable

	// Read response body with truncation protection.
	bodyBytes := httputil.ReadBody(resp.Body, httputil.ActionBodyLimit)

	r := executor.AcquireResult()
	r.Status = httputil.ResponseStatus(cfg.ExpectStatus, resp.StatusCode)
	r.StatusCode = resp.StatusCode
	r.Body = string(bodyBytes)
	r.Duration = duration
	r.Metrics["response_ms"] = float64(duration.Milliseconds())
	r.Metrics["status_code"] = float64(resp.StatusCode)
	r.Metrics["content_length"] = float64(len(bodyBytes))
	return r, nil
}

// Compile-time interface assertion.
var _ executor.Executor = (*Executor)(nil)
