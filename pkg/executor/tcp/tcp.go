// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package tcp provides a TCP connection prober that checks reachability and
// measures connection latency.
package tcp

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/bytedance/sonic"
	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/executor"
	"github.com/tickraft/tickraft/pkg/types"
)

// executorName is the executor name identifier returned by Name.
const executorName = string(types.ExecutorTCP)

// Executor dials a TCP connection to check reachability and measure
// connection latency. It implements the executor.Executor interface and is
// safe for concurrent use.
type Executor struct {
	timeout time.Duration
	logger  *zap.Logger
}

// Compile-time assertion that Executor implements executor.Executor.
var _ executor.Executor = (*Executor)(nil)

// Option configures the TCP prober.
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

// New creates a new TCP prober with the given connection timeout.
// A non-positive timeout defaults to 5 seconds at probe time.
func New(timeout time.Duration, options ...Option) *Executor {
	e := &Executor{
		timeout: timeout,
		logger:  zap.NewNop(),
	}
	for _, o := range options {
		o.apply(e)
	}
	return e
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
// ExecutionRequest.Config.
type config struct {
	Address string            `json:"address"`
	Port    int               `json:"port"`
	Params  map[string]string `json:"params,omitempty"`
}

// Execute runs the TCP probe based on the execution request.
// It parses the Config JSON into a config and performs the TCP probe.
//
// Panic isolation: a defer-recover catches any unexpected panic from the
// dialer or config parsing, logs it at Error level, and returns an abnormal
// Result so the Runner can record the failure and optionally retry.
func (p *Executor) Execute(ctx context.Context, req executor.ExecutionRequest) (result *executor.Result, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			p.logger.Error("tcp executor panic recovered",
				zap.Int64("asset_id", req.AssetID),
				zap.Any("panic", rec),
				zap.Stack("stack"),
			)
			r := executor.AcquireResult()
			r.Status = types.AssetStatusAbnormal
			r.ErrorMsg = fmt.Sprintf("tcp executor panic: %v", rec)
			result = r
			err = nil
		}
	}()

	var cfg config
	if req.Config != "" {
		if err := sonic.Unmarshal([]byte(req.Config), &cfg); err != nil {
			return nil, fmt.Errorf("tcp: parse executor config: %w", err)
		}
	}
	target := executor.TargetConfig{
		AssetID: req.AssetID,
		Address: cfg.Address,
		Port:    cfg.Port,
		Params:  cfg.Params,
	}
	return p.probe(ctx, target)
}

// probe dials a TCP connection to the target address and port, measuring the
// connection establishment time.
func (p *Executor) probe(ctx context.Context, target executor.TargetConfig) (*executor.Result, error) {
	if target.Address == "" {
		r := executor.AcquireResult()
		r.Status = types.AssetStatusAbnormal
		r.ErrorMsg = "address is required"
		return r, nil
	}
	if target.Port <= 0 {
		r := executor.AcquireResult()
		r.Status = types.AssetStatusAbnormal
		r.ErrorMsg = "port is required"
		return r, nil
	}

	addr := fmt.Sprintf("%s:%d", target.Address, target.Port)
	timeout := p.timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	start := time.Now()
	dialer := &net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	duration := time.Since(start)

	if err != nil {
		r := executor.AcquireResult()
		r.Status = types.AssetStatusAbnormal
		r.ErrorMsg = fmt.Sprintf("tcp dial failed: %v", err)
		r.Duration = duration
		r.Metrics["connect_ms"] = float64(duration.Milliseconds())
		return r, nil
	}
	defer func() { _ = conn.Close() }() // best-effort close, error not actionable

	r := executor.AcquireResult()
	r.Status = types.AssetStatusNormal
	r.Duration = duration
	r.Metrics["connect_ms"] = float64(duration.Milliseconds())
	return r, nil
}
