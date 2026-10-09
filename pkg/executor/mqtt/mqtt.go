// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package mqtt provides an MQTT probe executor that connects to an MQTT
// server, subscribes to a topic, and waits for a message within a
// configurable window, optionally matching the payload content.
package mqtt

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/bytedance/sonic"
	"github.com/eclipse/paho.golang/paho"
	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/executor"
	"github.com/tickraft/tickraft/pkg/executor/internal/deadline"
	"github.com/tickraft/tickraft/pkg/types"
)

// executorName is the executor name identifier returned by Name.
const executorName = string(types.ExecutorMQTTProbe)

// defaultWait is the message wait window applied when the config leaves
// wait_seconds unset or non-positive.
const defaultWait = 5 * time.Second

// keepAliveSeconds is the MQTT keep-alive advertised in the CONNECT
// packet. The probe connection is short-lived, so the value only needs to
// exceed the message wait window.
const keepAliveSeconds = 30

// Executor connects to an MQTT server, subscribes to a topic, and waits
// for a message within the configured window. It implements the
// executor.Executor interface and is safe for concurrent use.
type Executor struct {
	timeout time.Duration
	logger  *zap.Logger
}

// Compile-time assertion that Executor implements executor.Executor.
var _ executor.Executor = (*Executor)(nil)

// Option configures the MQTT prober.
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

// New creates a new MQTT prober with the given fallback timeout.
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
	// Address is the MQTT server address. It may carry a scheme
	// (tcp://, mqtt://, mqtts://, ssl://, tls://); a bare host:port is
	// treated as tcp://. The mqtts/ssl/tls schemes dial with TLS.
	Address string `json:"address"`
	// Topic is the topic filter to subscribe to while waiting for a
	// message. MQTT wildcards (+ and #) are permitted.
	Topic string `json:"topic"`
	// Username and Password are the optional MQTT credentials sent in the
	// CONNECT packet.
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	// ClientID overrides the generated probe client id.
	ClientID string `json:"client_id,omitempty"`
	// ExpectPayload, when non-empty, requires the received payload to
	// contain it as a substring; otherwise the probe reports abnormal
	// with payload_match=0.
	ExpectPayload string `json:"expect_payload,omitempty"`
	// WaitSeconds bounds the message wait window after the subscription
	// is established. Defaults to 5.
	WaitSeconds int `json:"wait_seconds,omitempty"`
}

// Execute runs the MQTT probe based on the execution request.
// It parses the Config JSON into a config and performs the probe.
//
// Panic isolation: a defer-recover catches any unexpected panic from the
// client or config parsing, logs it at Error level, and returns an
// abnormal Result so the Runner can record the failure and optionally
// retry.
func (p *Executor) Execute(ctx context.Context, req executor.ExecutionRequest) (result *executor.Result, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			p.logger.Error("mqtt executor panic recovered",
				zap.Int64("asset_id", req.AssetID),
				zap.Any("panic", rec),
				zap.Stack("stack"),
			)
			r := executor.AcquireResult()
			r.Status = types.AssetStatusAbnormal
			r.ErrorMsg = fmt.Sprintf("mqtt executor panic: %v", rec)
			result = r
			err = nil
		}
	}()

	var cfg config
	if req.Config != "" {
		if err := sonic.Unmarshal([]byte(req.Config), &cfg); err != nil {
			return nil, fmt.Errorf("mqtt: parse executor config: %w", err)
		}
	}
	return p.probe(ctx, cfg)
}

// validateProbeConfig returns an abnormal result when a required config
// key is missing, or nil when the probe may proceed.
func validateProbeConfig(cfg config) *executor.Result {
	switch {
	case cfg.Address == "":
		return missingConfigResult("address is required")
	case cfg.Topic == "":
		return missingConfigResult("topic is required")
	default:
		return nil
	}
}

// missingConfigResult builds the abnormal result for a rejected config.
func missingConfigResult(msg string) *executor.Result {
	r := executor.AcquireResult()
	r.Status = types.AssetStatusAbnormal
	r.ErrorMsg = msg
	return r
}

// failureResult builds the abnormal result shared by every probe phase
// failure; connectDur carries the CONNECT latency measured so far (zero
// before the handshake starts).
func failureResult(errMsg string, connectDur time.Duration) *executor.Result {
	r := executor.AcquireResult()
	r.Status = types.AssetStatusAbnormal
	r.Duration = connectDur
	r.ErrorMsg = errMsg
	r.Metrics["connect_ms"] = float64(connectDur.Milliseconds())
	r.Metrics["message_received"] = 0
	return r
}

// evalMessage records the received payload on the result. Without
// expect_payload configured, any message marks the point normal; with it,
// the payload must contain the configured substring.
func evalMessage(r *executor.Result, cfg config, payload []byte) {
	r.Metrics["message_received"] = 1
	r.Body = string(payload)
	if cfg.ExpectPayload == "" {
		r.Status = types.AssetStatusNormal
		return
	}
	if strings.Contains(string(payload), cfg.ExpectPayload) {
		r.Metrics["payload_match"] = 1
		r.Status = types.AssetStatusNormal
		return
	}
	r.Metrics["payload_match"] = 0
	r.Status = types.AssetStatusAbnormal
	r.ErrorMsg = fmt.Sprintf("payload mismatch: expected substring %q", cfg.ExpectPayload)
}

// probe connects to the MQTT server, subscribes to the topic, and waits
// for a message within the wait window. The produced metrics are
// connect_ms (CONNECT handshake latency), message_received (1/0), and —
// when expect_payload is configured — payload_match (1/0).
func (p *Executor) probe(ctx context.Context, cfg config) (*executor.Result, error) {
	if r := validateProbeConfig(cfg); r != nil {
		return r, nil
	}

	timeout := p.timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	// Single-source timeout: the caller's context deadline (set by the
	// runner lifecycle from the task's TimeoutSeconds) governs the probe;
	// the executor timeout is only a fallback for direct callers with an
	// unbounded context, so it never shortens a configured task timeout.
	ctx, cancel := deadline.Fallback(ctx, timeout)
	defer cancel()

	conn, err := dial(ctx, cfg.Address)
	if err != nil {
		return failureResult(fmt.Sprintf("mqtt dial failed: %v", err), 0), nil
	}
	defer func() { _ = conn.Close() }() // best-effort close, error not actionable

	msgs := make(chan *paho.Publish, 1)
	client := paho.NewClient(paho.ClientConfig{
		Conn:     conn,
		ClientID: cfg.ClientID,
		// PacketTimeout bounds each client-side packet round trip
		// (CONNECT, SUBSCRIBE); aligning it with the fallback timeout
		// keeps the effective deadline equal to the context deadline.
		PacketTimeout: timeout,
		OnPublishReceived: []func(paho.PublishReceived) (bool, error){
			func(pr paho.PublishReceived) (bool, error) {
				select {
				case msgs <- pr.Packet:
				default: // keep only the first message; later ones are dropped
				}
				return true, nil
			},
		},
	})
	defer func() {
		// A graceful DISCONNECT lets the server drop the session cleanly;
		// failures are ignored because the connection closes anyway.
		_ = client.Disconnect(&paho.Disconnect{ReasonCode: 0})
	}()

	connect := &paho.Connect{
		KeepAlive:  keepAliveSeconds,
		CleanStart: true,
		ClientID:   clientID(cfg.ClientID),
		Username:   cfg.Username,
		Password:   []byte(cfg.Password),
	}
	if cfg.Username != "" {
		connect.UsernameFlag = true
	}
	if cfg.Password != "" {
		connect.PasswordFlag = true
	}

	start := time.Now()
	if _, err := client.Connect(ctx, connect); err != nil {
		return failureResult(fmt.Sprintf("mqtt connect failed: %v", err), time.Since(start)), nil
	}
	connectDuration := time.Since(start)

	if _, err := client.Subscribe(ctx, &paho.Subscribe{
		Subscriptions: []paho.SubscribeOptions{{Topic: cfg.Topic, QoS: 0}},
	}); err != nil {
		return failureResult(fmt.Sprintf("mqtt subscribe failed: %v", err), connectDuration), nil
	}

	wait := defaultWait
	if cfg.WaitSeconds > 0 {
		wait = time.Duration(cfg.WaitSeconds) * time.Second
	}

	r := executor.AcquireResult()
	r.Duration = connectDuration
	r.Metrics["connect_ms"] = float64(connectDuration.Milliseconds())
	r.Metrics["message_received"] = 0

	noMessage := fmt.Sprintf("no message received on topic %q within %s", cfg.Topic, wait)
	select {
	case msg := <-msgs:
		evalMessage(r, cfg, msg.Payload)
		return r, nil
	case <-ctx.Done():
		return failureResult(noMessage, connectDuration), nil
	case <-time.After(wait):
		return failureResult(noMessage, connectDuration), nil
	}
}

// dial establishes the transport connection for the given MQTT address.
// The mqtts, ssl, and tls schemes dial with TLS (server certificate
// verified against the system roots); every other scheme — including a
// bare host:port — dials plain TCP.
func dial(ctx context.Context, address string) (net.Conn, error) {
	u, err := parseAddress(address)
	if err != nil {
		return nil, err
	}
	var d net.Dialer
	switch u.Scheme {
	case "mqtts", "ssl", "tls":
		raw, err := d.DialContext(ctx, "tcp", u.Host)
		if err != nil {
			return nil, err
		}
		tlsConn := tls.Client(raw, &tls.Config{ServerName: u.Hostname()})
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			_ = raw.Close() // best-effort close, error not actionable
			return nil, fmt.Errorf("tls handshake with %q: %w", u.Host, err)
		}
		return tlsConn, nil
	default:
		return d.DialContext(ctx, "tcp", u.Host)
	}
}

// parseAddress normalizes the configured address into a *url.URL. A bare
// host:port is treated as tcp://host:port; the mqtt scheme is an alias
// for tcp.
func parseAddress(address string) (*url.URL, error) {
	if !strings.Contains(address, "://") {
		address = "tcp://" + address
	}
	u, err := url.Parse(address)
	if err != nil {
		return nil, fmt.Errorf("parse address %q: %w", address, err)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("address %q missing host", address)
	}
	return u, nil
}

// clientID returns the configured client id or generates a unique probe
// client id. MQTT servers disconnect concurrent clients that share a
// client id, so probes must not reuse a fixed default.
func clientID(configured string) string {
	if configured != "" {
		return configured
	}
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// crypto/rand failure is effectively unrecoverable; fall back to
		// a time-based id so the probe still runs.
		return fmt.Sprintf("tickraft-probe-%d", time.Now().UnixNano())
	}
	return "tickraft-probe-" + hex.EncodeToString(buf[:])
}
