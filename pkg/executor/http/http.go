// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package http implements the dual-mode HTTP executor. In probe mode
// (OpProbe) it checks endpoint availability and measures response time; in
// task mode (OpExecute) it issues configured HTTP requests as scheduled
// actions (refresh callbacks, trigger calls, heartbeat posts). Both modes
// share one judgment: an explicit expect_status requires an exact match,
// otherwise any 2xx counts as normal/successful.
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
	"github.com/tickraft/tickraft/pkg/executor/internal/httputil"
	"github.com/tickraft/tickraft/pkg/httpx"
	"github.com/tickraft/tickraft/pkg/types"
)

const executorName = string(types.ExecutorHTTP)

// Option configures an HTTP prober at construction time.
type Option interface {
	apply(*Executor)
}

// methodOption sets the HTTP method for the probe request.
type methodOption string

func (o methodOption) apply(h *Executor) {
	if string(o) != "" {
		h.method = string(o)
	}
}

// WithMethod sets the HTTP method for the probe request.
// An empty value is ignored, leaving the default (GET).
func WithMethod(method string) Option { return methodOption(method) }

// headersOption sets the HTTP request headers to send with each probe.
type headersOption struct {
	headers map[string]string
}

func (o headersOption) apply(h *Executor) { h.headers = o.headers }

// WithHeaders sets the HTTP request headers to send with each probe.
func WithHeaders(headers map[string]string) Option { return headersOption{headers: headers} }

// bodyOption sets the HTTP request body for each probe.
type bodyOption string

func (o bodyOption) apply(h *Executor) { h.body = string(o) }

// WithBody sets the HTTP request body for each probe.
func WithBody(body string) Option { return bodyOption(body) }

// expectStatusOption sets the expected HTTP response status code.
type expectStatusOption int

func (o expectStatusOption) apply(h *Executor) { h.expectStatus = int(o) }

// WithExpectStatus sets the expected HTTP response status code.
// A value of 0 (the default) accepts any 2xx status as normal.
func WithExpectStatus(code int) Option { return expectStatusOption(code) }

// loggerOption sets the structured logger.
type loggerOption struct {
	logger *zap.Logger
}

func (o loggerOption) apply(h *Executor) {
	if o.logger != nil {
		h.logger = o.logger
	}
}

// WithLogger sets the structured logger.
func WithLogger(logger *zap.Logger) Option { return loggerOption{logger: logger} }

// Executor sends HTTP requests to check endpoint availability and measure
// response time. It implements the executor.Executor interface and is safe
// for concurrent use.
type Executor struct {
	method       string
	headers      map[string]string
	body         string
	expectStatus int
	client       *nethttp.Client
	logger       *zap.Logger
}

// Compile-time assertion that Executor implements executor.Executor.
var _ executor.Executor = (*Executor)(nil)

// New creates a new HTTP executor with the given options.
// Defaults: method GET, expectStatus 0 (any 2xx is normal).
//
// Per-request timeouts are controlled by the caller's context (the runner's
// lifecycle or the remediation operator); the pooled client only carries the
// defensive httputil.HardTimeout ceiling.
func New(options ...Option) *Executor {
	h := &Executor{
		method:  nethttp.MethodGet,
		headers: make(map[string]string),
		client: httpx.NewPoolClient(httpx.Config{
			Timeout: httputil.HardTimeout,
			TLSConfig: &tls.Config{
				InsecureSkipVerify: false,
			},
		}),
		logger: zap.NewNop(),
	}
	for _, o := range options {
		o.apply(h)
	}
	return h
}

// Name returns the executor name identifier.
func (p *Executor) Name() string {
	return executorName
}

// Capabilities returns the executor capability bitmask. The executor is
// dual-mode: it probes endpoints (OpProbe) and issues HTTP requests as
// scheduled actions (OpExecute), so it declares CapProbe | CapExec.
func (p *Executor) Capabilities() executor.Capability {
	return executor.CapProbe | executor.CapExec
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

// Execute runs the HTTP request described by the execution request. It serves
// both operations — probes (OpProbe) and task actions (OpExecute) — with one
// judgment: expect_status or any 2xx maps to normal/success. It parses the
// Config JSON into a config; when HTTP-specific fields are present, a derived
// executor is constructed to apply the per-request overrides without mutating
// the shared receiver, preserving concurrency safety.
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

// newDerived constructs a per-request view of the executor with the config
// overrides applied on top of the receiver's constructor-time defaults. The
// derived executor shares the receiver's pooled HTTP client: building a
// fresh pool per request would discard connection reuse, which is the main
// win of pooling for a per-interval prober.
func newDerived(base *Executor, cfg config) *Executor {
	derived := &Executor{
		method:       base.method,
		headers:      make(map[string]string, len(base.headers)+len(cfg.Headers)),
		body:         base.body,
		expectStatus: base.expectStatus,
		client:       base.client,
		logger:       base.logger,
	}
	for k, v := range base.headers {
		derived.headers[k] = v
	}
	if cfg.Method != "" {
		derived.method = cfg.Method
	}
	if cfg.Body != "" {
		derived.body = cfg.Body
	}
	if cfg.ExpectStatus > 0 {
		derived.expectStatus = cfg.ExpectStatus
	}
	for k, v := range cfg.Headers {
		derived.headers[k] = v
	}
	return derived
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

	// Read up to the probe body limit for status reporting and judgment.
	body := httputil.ReadBody(resp.Body, httputil.ProbeBodyLimit)

	r := executor.AcquireResult()
	r.Status = httputil.ResponseStatus(p.expectStatus, resp.StatusCode)
	r.StatusCode = resp.StatusCode
	r.Body = string(body)
	r.Duration = duration
	r.Metrics["response_ms"] = float64(duration.Milliseconds())
	r.Metrics["status_code"] = float64(resp.StatusCode)
	r.Metrics["content_length"] = float64(len(body))
	return r, nil
}
