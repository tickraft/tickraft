// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package icmp provides an ICMP echo prober that measures reachability and
// round-trip time.
package icmp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/bytedance/sonic"
	"github.com/tickraft/tickraft/pkg/executor"
	"github.com/tickraft/tickraft/pkg/types"
	"go.uber.org/zap"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

const (
	echoData    = "tickraft-icmp-probe"
	readBufSize = 1500
	protocolV4  = 1 // protocol number for ICMPv4
)

// Executor sends ICMP echo requests to measure reachability and round-trip
// time. It implements the executor.Executor interface and is safe for
// concurrent use.
type Executor struct {
	timeout time.Duration
	logger  *zap.Logger
}

// Compile-time assertion that Executor implements executor.Executor.
var _ executor.Executor = (*Executor)(nil)

// Option configures the ICMP prober.
type Option func(*Executor)

// WithLogger sets the structured logger.
func WithLogger(logger *zap.Logger) Option {
	return func(e *Executor) {
		if logger != nil {
			e.logger = logger
		}
	}
}

// New creates a new ICMP prober with the given timeout.
// A non-positive timeout defaults to 5 seconds at probe time.
func New(timeout time.Duration, opts ...Option) *Executor {
	e := &Executor{
		timeout: timeout,
		logger:  zap.NewNop(),
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// Name returns the executor name identifier.
func (p *Executor) Name() string {
	return "icmp"
}

// Capabilities returns the executor capability bitmask.
func (p *Executor) Capabilities() executor.Capability {
	return executor.CapProbe
}

// config holds the per-execution configuration parsed from
// ExecutionRequest.Config.
type config struct {
	Address string            `json:"address"`
	Params  map[string]string `json:"params,omitempty"`
}

// Execute runs the ICMP probe based on the execution request.
// It parses the Config JSON into a config and performs the ICMP probe.
//
// Panic isolation: a defer-recover catches any unexpected panic from the
// ICMP library or config parsing, logs it at Error level, and returns an
// abnormal Result so the Runner can record the failure and optionally retry.
func (p *Executor) Execute(ctx context.Context, req executor.ExecutionRequest) (result *executor.Result, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			p.logger.Error("icmp executor panic recovered",
				zap.Int64("asset_id", req.AssetID),
				zap.Any("panic", rec),
				zap.Stack("stack"),
			)
			r := executor.AcquireResult()
			r.Status = types.AssetStatusAbnormal
			r.ErrorMsg = fmt.Sprintf("icmp executor panic: %v", rec)
			result = r
			err = nil
		}
	}()

	var cfg config
	if req.Config != "" {
		if err := sonic.Unmarshal([]byte(req.Config), &cfg); err != nil {
			return nil, fmt.Errorf("icmp: parse executor config: %w", err)
		}
	}
	target := executor.TargetConfig{
		AssetID: req.AssetID,
		Address: cfg.Address,
		Params:  cfg.Params,
	}
	return p.probe(ctx, target)
}

// probe executes an ICMP echo request against the target and measures the
// round-trip time. The target Address may be an IP address or a hostname.
//
// Timeout control: the probe wraps the context with context.WithTimeout so
// that both the connection deadline and the context cancellation are
// respected. This ensures the probe terminates promptly when either the
// caller's context expires or the prober's own timeout elapses.
func (p *Executor) probe(ctx context.Context, target executor.TargetConfig) (*executor.Result, error) {
	if target.Address == "" {
		r := executor.AcquireResult()
		r.Status = types.AssetStatusAbnormal
		r.ErrorMsg = "address is required"
		return r, nil
	}

	timeout := p.timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	// Wrap the caller's context with the prober's timeout so that both
	// context cancellation and the timeout deadline are enforced.
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Resolve the target address to an IP address. The resolver is bound
	// to ctx so that the prober timeout also applies to DNS lookups, which
	// would otherwise be able to block beyond the deadline.
	ip := net.ParseIP(target.Address)
	if ip == nil {
		addrs, err := net.DefaultResolver.LookupIPAddr(ctx, target.Address)
		if err != nil {
			r := executor.AcquireResult()
			r.Status = types.AssetStatusAbnormal
			r.ErrorMsg = fmt.Sprintf("resolve %q failed: %v", target.Address, err)
			return r, nil
		}
		if len(addrs) == 0 {
			r := executor.AcquireResult()
			r.Status = types.AssetStatusAbnormal
			r.ErrorMsg = fmt.Sprintf("no addresses resolved for %q", target.Address)
			return r, nil
		}
		ip = addrs[0].IP
	}

	// Listen for ICMP replies. The "udp4" network uses unprivileged ICMP
	// (SOCK_DGRAM with IPPROTO_ICMP) on Linux, avoiding the need for root
	// when the ping_group_range sysctl is configured.
	conn, err := icmp.ListenPacket("udp4", "")
	if err != nil {
		if isPermissionError(err) {
			r := executor.AcquireResult()
			r.Status = types.AssetStatusAbnormal
			r.ErrorMsg = "icmp requires root privileges or CAP_NET_RAW capability " +
				"(grant with: sudo setcap cap_net_raw+ep <binary>); " +
				"alternatively configure net.ipv4.ping_group_range for unprivileged ICMP. " +
				"Original error: " + err.Error()
			return r, nil
		}
		r := executor.AcquireResult()
		r.Status = types.AssetStatusAbnormal
		r.ErrorMsg = fmt.Sprintf("icmp listen failed: %v", err)
		return r, nil
	}
	defer func() { _ = conn.Close() }() // best-effort close, error not actionable

	// Set the connection deadline from the timeout context.
	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		r := executor.AcquireResult()
		r.Status = types.AssetStatusAbnormal
		r.ErrorMsg = fmt.Sprintf("set deadline failed: %v", err)
		return r, nil
	}

	// Respect context cancellation before sending.
	select {
	case <-ctx.Done():
		r := executor.AcquireResult()
		r.Status = types.AssetStatusAbnormal
		r.ErrorMsg = ctx.Err().Error()
		return r, nil
	default:
	}

	// Build the ICMP echo request message.
	msg := &icmp.Message{
		Type: ipv4.ICMPTypeEcho,
		Code: 0,
		Body: &icmp.Echo{
			ID:   os.Getpid() & 0xffff,
			Seq:  1,
			Data: []byte(echoData),
		},
	}
	msgBytes, err := msg.Marshal(nil)
	if err != nil {
		r := executor.AcquireResult()
		r.Status = types.AssetStatusAbnormal
		r.ErrorMsg = fmt.Sprintf("marshal icmp message failed: %v", err)
		return r, nil
	}

	// Send the echo request and measure round-trip time.
	dst := &net.UDPAddr{IP: ip}
	start := time.Now()
	if _, err := conn.WriteTo(msgBytes, dst); err != nil {
		duration := time.Since(start)
		r := executor.AcquireResult()
		r.Status = types.AssetStatusAbnormal
		r.ErrorMsg = fmt.Sprintf("icmp write failed: %v", err)
		r.Duration = duration
		r.Metrics["rtt_ms"] = float64(duration.Milliseconds())
		r.Metrics["packet_loss"] = 100.0
		return r, nil
	}

	// Read the reply.
	buf := make([]byte, readBufSize)
	n, peer, err := conn.ReadFrom(buf)
	duration := time.Since(start)
	if err != nil {
		r := executor.AcquireResult()
		r.Status = types.AssetStatusAbnormal
		r.ErrorMsg = fmt.Sprintf("icmp read failed: %v", err)
		r.Duration = duration
		r.Metrics["rtt_ms"] = float64(duration.Milliseconds())
		r.Metrics["packet_loss"] = 100.0
		return r, nil
	}

	// Parse the reply (protocol number 1 = ICMPv4).
	rm, err := icmp.ParseMessage(protocolV4, buf[:n])
	if err != nil {
		r := executor.AcquireResult()
		r.Status = types.AssetStatusAbnormal
		r.ErrorMsg = fmt.Sprintf("parse icmp reply failed: %v", err)
		r.Duration = duration
		return r, nil
	}

	if rm.Type != ipv4.ICMPTypeEchoReply {
		r := executor.AcquireResult()
		r.Status = types.AssetStatusAbnormal
		r.ErrorMsg = fmt.Sprintf("unexpected icmp reply type: %v", rm.Type)
		r.Duration = duration
		return r, nil
	}

	r := executor.AcquireResult()
	r.Status = types.AssetStatusNormal
	r.Duration = duration
	r.Body = fmt.Sprintf("reply from %s", peer.String())
	r.Metrics["rtt_ms"] = float64(duration.Milliseconds())
	r.Metrics["packet_loss"] = 0.0
	return r, nil
}

// isPermissionError reports whether the error indicates a lack of privileges
// to create raw or datagram ICMP sockets.
func isPermissionError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, os.ErrPermission) {
		return true
	}
	// Fallback string check for platforms where the syscall error does not
	// unwrap cleanly to os.ErrPermission.
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "permission denied") ||
		strings.Contains(msg, "operation not permitted")
}
