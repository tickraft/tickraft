// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package http implements the HTTP executor, which runs tasks by issuing HTTP
// requests to configured endpoints and reporting status, metrics and logs.
package http

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	nethttp "net/http"
	"strings"
	"time"

	"github.com/bytedance/sonic"
	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/executor"
	"github.com/tickraft/tickraft/pkg/httpx"
	"github.com/tickraft/tickraft/pkg/types"
)

const (
	executorName    = string(types.ExecutorHTTP)
	httpMaxBodySize = 4 * 1024 // 4 KiB
)

// Option configures an HTTP prober at construction time.
type Option func(*Executor)

// WithMethod sets the HTTP method for the probe request.
// An empty value is ignored, leaving the default (GET).
func WithMethod(method string) Option {
	return func(h *Executor) {
		if method != "" {
			h.method = method
		}
	}
}

// WithHeaders sets the HTTP request headers to send with each probe.
func WithHeaders(headers map[string]string) Option {
	return func(h *Executor) {
		h.headers = headers
	}
}

// WithBody sets the HTTP request body for each probe.
func WithBody(body string) Option {
	return func(h *Executor) {
		h.body = body
	}
}

// WithExpectStatus sets the expected HTTP response status code.
// A value of 0 (the default) accepts any 2xx status as normal.
func WithExpectStatus(code int) Option {
	return func(h *Executor) {
		h.expectStatus = code
	}
}

// WithLogger sets the structured logger.
func WithLogger(logger *zap.Logger) Option {
	return func(h *Executor) {
		if logger != nil {
			h.logger = logger
		}
	}
}

// Executor sends HTTP requests to check endpoint availability and measure
// response time. It implements the executor.Executor interface and is safe
// for concurrent use.
type Executor struct {
	timeout      time.Duration
	method       string
	headers      map[string]string
	body         string
	expectStatus int
	client       *nethttp.Client
	logger       *zap.Logger
}

// Compile-time assertion that Executor implements executor.Executor.
var _ executor.Executor = (*Executor)(nil)

// New creates a new HTTP prober with the given timeout and options.
// Defaults: method GET, expectStatus 0 (any 2xx is normal).
// A non-positive timeout defaults to 10 seconds.
func New(timeout time.Duration, opts ...Option) *Executor {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	h := &Executor{
		timeout:      timeout,
		method:       nethttp.MethodGet,
		headers:      make(map[string]string),
		expectStatus: 0,
		client: httpx.NewPoolClient(httpx.Config{
			Timeout: timeout,
			TLSConfig: &tls.Config{
				InsecureSkipVerify: false,
			},
		}),
		logger: zap.NewNop(),
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// Name returns the executor name identifier.
func (p *Executor) Name() string {
	return executorName
}

// Capabilities returns the executor capability bitmask.
func (p *Executor) Capabilities() executor.Capability {
	return executor.CapProbe
}

// config holds the per-execution configuration parsed from
// ExecutionRequest.Config. HTTP-specific fields override the
// prober's constructor-time defaults when set.
type config struct {
	Address      string            `json:"address"`
	Method       string            `json:"method,omitempty"`
	Headers      map[string]string `json:"headers,omitempty"`
	Body         string            `json:"body,omitempty"`
	ExpectStatus int               `json:"expect_status,omitempty"`
	Params       map[string]string `json:"params,omitempty"`
}

// Execute runs the HTTP probe based on the execution request.
// It parses the Config JSON into a config and performs the HTTP probe.
// When HTTP-specific fields are present in the config, a derived prober is
// constructed to apply the per-request overrides without mutating the shared
// receiver, preserving concurrency safety.
//
// Panic isolation: a defer-recover catches any unexpected panic from the
// HTTP client or config parsing, logs it at Error level, and returns an
// abnormal Result so the Runner can record the failure and optionally retry.
func (p *Executor) Execute(ctx context.Context, req executor.ExecutionRequest) (result *executor.Result, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			p.logger.Error("http executor panic recovered",
				zap.Int64("asset_id", req.AssetID),
				zap.Any("panic", rec),
				zap.Stack("stack"),
			)
			r := executor.AcquireResult()
			r.Status = types.AssetStatusAbnormal
			r.ErrorMsg = fmt.Sprintf("http executor panic: %v", rec)
			result = r
			err = nil
		}
	}()

	var cfg config
	if req.Config != "" {
		if err := sonic.Unmarshal([]byte(req.Config), &cfg); err != nil {
			return nil, fmt.Errorf("http: parse executor config: %w", err)
		}
	}
	target := executor.TargetConfig{
		AssetID: req.AssetID,
		Address: cfg.Address,
		Params:  cfg.Params,
	}

	// If no HTTP-specific overrides are present, use the receiver directly.
	if cfg.Method == "" && cfg.Body == "" && cfg.ExpectStatus == 0 && len(cfg.Headers) == 0 {
		return p.probe(ctx, target)
	}

	// Build a derived prober with the per-request overrides applied on top of
	// the receiver's constructor-time defaults.
	runner := newDerived(p, cfg)
	return runner.probe(ctx, target)
}

// newDerived constructs a fresh HTTP prober that inherits the receiver's
// timeout and applies the per-request config overrides. This keeps the
// internal probe logic unchanged while supporting per-request customization.
func newDerived(base *Executor, cfg config) *Executor {
	opts := []Option{WithLogger(base.logger)}
	if cfg.Method != "" {
		opts = append(opts, WithMethod(cfg.Method))
	}
	if cfg.Body != "" {
		opts = append(opts, WithBody(cfg.Body))
	}
	if cfg.ExpectStatus > 0 {
		opts = append(opts, WithExpectStatus(cfg.ExpectStatus))
	}
	if len(cfg.Headers) > 0 {
		opts = append(opts, WithHeaders(cfg.Headers))
	}
	return New(base.timeout, opts...)
}

// probe sends an HTTP request to the target URL and checks the response
// status code against the expected value, measuring the total response time.
func (p *Executor) probe(ctx context.Context, target executor.TargetConfig) (*executor.Result, error) {
	if target.Address == "" {
		r := executor.AcquireResult()
		r.Status = types.AssetStatusAbnormal
		r.ErrorMsg = "address (URL) is required"
		return r, nil
	}

	var bodyReader io.Reader
	if p.body != "" {
		bodyReader = strings.NewReader(p.body)
	}

	req, err := nethttp.NewRequestWithContext(ctx, p.method, target.Address, bodyReader)
	if err != nil {
		r := executor.AcquireResult()
		r.Status = types.AssetStatusAbnormal
		r.ErrorMsg = fmt.Sprintf("invalid request: %v", err)
		return r, nil
	}

	for key, val := range p.headers {
		req.Header.Set(key, val)
	}

	start := time.Now()
	resp, err := p.client.Do(req)
	duration := time.Since(start)

	if err != nil {
		r := executor.AcquireResult()
		r.Status = types.AssetStatusAbnormal
		r.ErrorMsg = fmt.Sprintf("http request failed: %v", err)
		r.Duration = duration
		r.Metrics["response_ms"] = float64(duration.Milliseconds())
		return r, nil
	}
	defer func() { _ = resp.Body.Close() }() // best-effort close, error not actionable

	// Read up to httpMaxBodySize of the response body for status reporting.
	body, _ := io.ReadAll(io.LimitReader(resp.Body, httpMaxBodySize))

	// Determine status based on the expected status code.
	status := types.AssetStatusNormal
	if p.expectStatus > 0 {
		if resp.StatusCode != p.expectStatus {
			status = types.AssetStatusAbnormal
		}
	} else {
		// Default: any 2xx is normal; everything else is abnormal.
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			status = types.AssetStatusAbnormal
		}
	}

	r := executor.AcquireResult()
	r.Status = status
	r.StatusCode = resp.StatusCode
	r.Body = string(body)
	r.Duration = duration
	r.Metrics["response_ms"] = float64(duration.Milliseconds())
	r.Metrics["status_code"] = float64(resp.StatusCode)
	r.Metrics["content_length"] = float64(len(body))
	return r, nil
}
