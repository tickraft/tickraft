// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package local provides a local script executor that runs commands on the
// host machine via os/exec.
package local

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/tickraft/tickraft/pkg/executor"
	"github.com/tickraft/tickraft/pkg/types"
	"go.uber.org/zap"
)

// defaultTimeout is the default execution timeout for the local executor.
const defaultTimeout = 300 * time.Second

// Executor executes commands on the host machine via os/exec.
// It is safe for concurrent use.
type Executor struct {
	timeout     time.Duration
	logger      *zap.Logger
	allowedCmds []string // empty = allow all
}

// Option configures the local executor.
type Option interface {
	apply(*Executor)
}

type funcOption func(*Executor)

func (f funcOption) apply(e *Executor) { f(e) }

// WithTimeout sets the maximum execution duration.
func WithTimeout(timeout time.Duration) Option {
	return funcOption(func(e *Executor) { e.timeout = timeout })
}

// WithLogger sets the structured logger.
func WithLogger(logger *zap.Logger) Option {
	return funcOption(func(e *Executor) {
		if logger != nil {
			e.logger = logger
		}
	})
}

// WithAllowedCommands restricts the executor to only run commands whose path
// matches one of the given prefixes. An empty list (the default) allows all
// commands, preserving the default behavior. Typical prefixes are directory
// paths like "/usr/local/bin/", "/opt/tickraft/bin/", or exact binary paths
// like "/usr/bin/curl".
func WithAllowedCommands(paths ...string) Option {
	return funcOption(func(e *Executor) { e.allowedCmds = paths })
}

// New creates a new local script executor with sensible defaults.
// The default timeout is 300 seconds and the default logger is a no-op logger.
func New(opts ...Option) *Executor {
	e := &Executor{
		timeout: defaultTimeout,
		logger:  zap.NewNop(),
	}
	for _, o := range opts {
		o.apply(e)
	}
	return e
}

// Name returns the executor name identifier.
func (e *Executor) Name() string { return "local" }

// Capabilities returns the executor capability bitmask.
func (e *Executor) Capabilities() executor.Capability { return executor.CapExec }

// config holds the local executor-specific configuration parsed from Config.
type config struct {
	// Command is the binary to execute.
	Command string `json:"command"`
	// Args are the arguments passed to the command.
	Args []string `json:"args,omitempty"`
	// Env holds additional environment variables for the child process.
	Env map[string]string `json:"env,omitempty"`
}

// Execute runs the local command task and returns the result.
//
// The req.Config must be a JSON object with the following fields:
//   - command (string, required): the binary to execute.
//   - args ([]string, optional): the arguments passed to the command.
//   - env (map[string]string, optional): additional environment variables.
//
// A zero exit code yields StatusNormal with stdout in Body. A non-zero exit
// code yields StatusAbnormal with stdout in Body and stderr in ErrorMsg. A
// context deadline yields StatusAbnormal with ErrorMsg "execution timeout".
//
// Panic isolation: a defer-recover catches any unexpected panic from the
// exec machinery or config parsing, logs it at Error level, and returns an
// abnormal Result so the Runner can record the failure and optionally retry.
func (e *Executor) Execute(ctx context.Context, req executor.ExecutionRequest) (result *executor.Result, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			e.logger.Error("local executor panic recovered",
				zap.Int64("asset_id", req.AssetID),
				zap.Any("panic", rec),
				zap.Stack("stack"),
			)
			r := executor.AcquireResult()
			r.Status = types.AssetStatusAbnormal
			r.ErrorMsg = fmt.Sprintf("local executor panic: %v", rec)
			result = r
			err = nil
		}
	}()

	if req.Config == "" {
		return nil, fmt.Errorf("local: executor config is empty")
	}

	var cfg config
	if err := json.Unmarshal([]byte(req.Config), &cfg); err != nil {
		return nil, fmt.Errorf("local: parse config: %w", err)
	}
	if cfg.Command == "" {
		return nil, fmt.Errorf("local: command is required")
	}

	// Security: validate the command against the whitelist when configured.
	// An empty whitelist means all commands are allowed.
	if len(e.allowedCmds) > 0 && !isCommandAllowed(cfg.Command, e.allowedCmds) {
		e.logger.Warn("local executor: command rejected by whitelist",
			zap.Int64("asset_id", req.AssetID),
			zap.String("command", cfg.Command),
		)
		return nil, fmt.Errorf("local: command %q is not in the allowed list", cfg.Command)
	}

	// Timeout control: apply the executor's configured timeout when set.
	if e.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, e.timeout)
		defer cancel()
	}

	// Security: log every execution for auditability.
	e.logger.Info("local executor: starting",
		zap.Int64("asset_id", req.AssetID),
		zap.String("command", cfg.Command),
		zap.Strings("args", cfg.Args),
	)

	start := time.Now()
	cmd := exec.CommandContext(ctx, cfg.Command, cfg.Args...)
	// TimeoutReaper: run the command in its own process group so a timeout
	// can kill the command and any children it spawned (shells, pipelines,
	// daemons) together, and reap them rather than orphaning them as
	// zombies. On context cancellation cmd.Cancel sends SIGKILL to the
	// whole group; WaitDelay bounds the grace period before forceful
	// resource reclamation.
	applyProcessGroup(cmd)
	cmd.Cancel = func() error {
		return killProcessGroup(cmd)
	}
	cmd.WaitDelay = 5 * time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if len(cfg.Env) > 0 {
		// Inherit the parent environment and override with the configured values.
		cmd.Env = os.Environ()
		for k, v := range cfg.Env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}

	err = cmd.Run()
	duration := time.Since(start)

	// Timeout takes precedence: the context deadline elapsed.
	if ctx.Err() == context.DeadlineExceeded {
		e.logger.Warn("local executor: timeout",
			zap.String("command", cfg.Command),
			zap.Duration("duration", duration),
		)
		r := executor.AcquireResult()
		r.Status = types.AssetStatusAbnormal
		r.ErrorMsg = "execution timeout"
		r.Duration = duration
		return r, nil
	}

	if err != nil {
		// Distinguish non-zero exit codes from other failures (e.g. command not found).
		errorMsg := stderr.String()
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) && errorMsg == "" {
			errorMsg = err.Error()
		}
		e.logger.Warn("local executor: failed",
			zap.String("command", cfg.Command),
			zap.Error(err),
			zap.Duration("duration", duration),
		)
		r := executor.AcquireResult()
		r.Status = types.AssetStatusAbnormal
		r.Body = stdout.String()
		r.ErrorMsg = errorMsg
		r.Duration = duration
		return r, nil
	}

	e.logger.Info("local executor: succeeded",
		zap.String("command", cfg.Command),
		zap.Duration("duration", duration),
	)
	r := executor.AcquireResult()
	r.Status = types.AssetStatusNormal
	r.Body = stdout.String()
	r.Duration = duration
	return r, nil
}

// Compile-time interface assertion.
var _ executor.Executor = (*Executor)(nil)

// isCommandAllowed reports whether the command path matches one of the
// allowed prefixes. A prefix can be a directory path (e.g. "/usr/local/bin/")
// or an exact binary path (e.g. "/usr/bin/curl"). The match is prefix-based
// so that scripts in a whitelisted directory (e.g. /opt/tickraft/bin/check.sh)
// are automatically allowed.
func isCommandAllowed(command string, allowedPrefixes []string) bool {
	for _, prefix := range allowedPrefixes {
		if strings.HasPrefix(command, prefix) {
			return true
		}
	}
	return false
}
