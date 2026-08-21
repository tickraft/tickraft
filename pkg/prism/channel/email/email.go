// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package email implements an SMTP email notification channel for the
// tickraft alerting pipeline.
//
// A Channel formats alert events as email messages and delivers them via
// SMTP. It supports plaintext and HTML (multipart/alternative) message
// bodies, implicit TLS (port 465), STARTTLS (port 587), and PLAIN, LOGIN,
// and CRAM-MD5 authentication. A circuit breaker prevents hammering a
// degraded server and a retry mechanism with exponential backoff and full
// jitter tolerates transient failures.
package email

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/smtp"
	"net/textproto"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/circuitbreaker"
	"github.com/tickraft/tickraft/pkg/prism/alert"
	"github.com/tickraft/tickraft/pkg/prism/channel"
	"github.com/tickraft/tickraft/pkg/retry"
	"github.com/tickraft/tickraft/pkg/types"
)

// Channel sends alert notifications via SMTP email. It satisfies the
// alert.Channel interface.
type Channel struct {
	config Config
	cb     *circuitbreaker.CircuitBreaker
	retry  *retry.Retry
	logger *zap.Logger
	tls    *tls.Config
}

// Compile-time assertion that Channel implements alert.Channel.
var _ alert.Channel = (*Channel)(nil)

// Name implements alert.Channel.
func (c *Channel) Name() string { return types.ChannelEmail }

// Send implements alert.Channel. It formats the alert as an email message
// and delivers it via SMTP.
//
// The send is protected by a circuit breaker: when the breaker is open Send
// short-circuits with channel.ErrCircuitOpen. Transient failures (network
// errors, 4xx SMTP responses) are retried with exponential backoff and full
// jitter; permanent failures (authentication errors, 5xx SMTP responses) fail
// immediately. On success the breaker is reset; on failure the breaker
// records a failure and a *channel.SendError is returned indicating whether
// the failure is retryable.
//
// The entire operation (message rendering + SMTP delivery) is bounded by
// the configured Timeout. A panic in any sub-operation is isolated and
// returned as a non-retryable SendError so the prism alert engine is not
// crashed.
func (c *Channel) Send(ctx context.Context, evt alert.Event) (err error) {
	defer func() {
		if r := recover(); r != nil {
			c.cb.RecordFailure()
			c.logger.Error("email send panicked",
				zap.Any("panic", r),
				zap.Stack("stack"),
			)
			err = channel.NewSendError(c.Name(), false,
				fmt.Errorf("email: panic: %v", r))
		}
	}()

	if !c.cb.Allow() {
		c.logger.Debug("email send suppressed: circuit breaker open")
		return channel.ErrCircuitOpen
	}

	sendCtx, cancel := context.WithTimeout(ctx, c.config.Timeout)
	defer cancel()

	msg := buildMessage(sendCtx, evt, c.config, c.logger)

	err = c.retry.Do(sendCtx, func() error {
		return c.sendOnce(sendCtx, msg)
	})
	if err == nil {
		c.cb.RecordSuccess()
		return nil
	}

	// Context cancellations are client-initiated; do not penalise the
	// circuit breaker for them.
	if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		c.cb.RecordFailure()
	}
	retryable := isRetryableErr(err)
	c.logger.Warn("email send failed",
		zap.Error(err),
		zap.Bool("retryable", retryable),
	)
	return channel.NewSendError(c.Name(), retryable, err)
}

// sendOnce performs a single SMTP delivery attempt.
func (c *Channel) sendOnce(ctx context.Context, msg []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	addr := net.JoinHostPort(c.config.Host, fmt.Sprintf("%d", c.config.Port))

	conn, err := c.dial(ctx, addr)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := conn.Close(); cerr != nil {
			c.logger.Debug("email: connection close error", zap.Error(cerr))
		}
	}()

	// Apply the context deadline to the connection so that slow SMTP
	// operations (Auth, Mail, Rcpt, Data) time out instead of blocking
	// indefinitely. net/smtp does not accept a context, so connection
	// deadlines are the only way to enforce per-operation timeouts.
	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			return fmt.Errorf("email: set deadline: %w", err)
		}
	}

	client, err := smtp.NewClient(conn, c.config.Host)
	if err != nil {
		return fmt.Errorf("email: new smtp client: %w", err)
	}

	if err := c.helloAndAuth(client); err != nil {
		return err
	}

	return c.deliver(client, msg)
}

// helloAndAuth performs the SMTP HELO handshake, the optional STARTTLS
// upgrade, and authentication on an established client connection.
func (c *Channel) helloAndAuth(client *smtp.Client) error {
	if err := client.Hello(heloDomain); err != nil {
		return fmt.Errorf("email: hello: %w", err)
	}

	if c.config.TLSMode == TLSModeStartTLS {
		if err := c.startTLS(client); err != nil {
			return err
		}
	}

	if c.config.Username != "" {
		auth := c.buildAuth()
		if auth != nil {
			if err := client.Auth(auth); err != nil {
				return fmt.Errorf("email: auth: %w", err)
			}
		}
	}
	return nil
}

// deliver sends the envelope (MAIL FROM / RCPT TO), streams the message
// body over DATA, and closes the session with QUIT. A QUIT failure
// after DATA is accepted is logged and ignored.
func (c *Channel) deliver(client *smtp.Client, msg []byte) error {
	if err := client.Mail(c.config.From); err != nil {
		return fmt.Errorf("email: mail from: %w", err)
	}
	for _, to := range c.config.To {
		if err := client.Rcpt(to); err != nil {
			return fmt.Errorf("email: rcpt to %s: %w", to, err)
		}
	}

	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("email: data: %w", err)
	}
	if _, err = w.Write(msg); err != nil {
		return fmt.Errorf("email: write message: %w", err)
	}
	if err = w.Close(); err != nil {
		return fmt.Errorf("email: close data: %w", err)
	}

	if err = client.Quit(); err != nil {
		// Quit failure after DATA is accepted is non-critical; the
		// message has already been queued by the server.
		c.logger.Debug("email: quit failed", zap.Error(err))
	}
	return nil
}

// dial establishes a connection to the SMTP server according to the
// configured TLS mode. The provided context is respected during dial so
// that cancellation or deadline expiry interrupts an in-progress dial.
func (c *Channel) dial(ctx context.Context, addr string) (net.Conn, error) {
	switch c.config.TLSMode {
	case TLSModeImplicit:
		tlsCfg := c.tls
		if tlsCfg == nil {
			tlsCfg = &tls.Config{ServerName: c.config.Host}
		}
		d := tls.Dialer{Config: tlsCfg, NetDialer: &net.Dialer{Timeout: 30 * time.Second}}
		conn, err := d.DialContext(ctx, "tcp", addr)
		if err != nil {
			return nil, fmt.Errorf("email: tls dial %s: %w", addr, err)
		}
		return conn, nil
	case TLSModeStartTLS, TLSModeNone:
		d := net.Dialer{Timeout: 30 * time.Second}
		conn, err := d.DialContext(ctx, "tcp", addr)
		if err != nil {
			return nil, fmt.Errorf("email: dial %s: %w", addr, err)
		}
		return conn, nil
	default:
		return nil, fmt.Errorf("email: unsupported TLS mode %s", c.config.TLSMode)
	}
}

// startTLS upgrades the client connection to TLS.
func (c *Channel) startTLS(client *smtp.Client) error {
	config := c.tls
	if config == nil {
		config = &tls.Config{ServerName: c.config.Host}
	}
	if err := client.StartTLS(config); err != nil {
		return fmt.Errorf("email: starttls: %w", err)
	}
	return nil
}

// buildAuth creates the smtp.Auth for the configured AuthType. Returns nil
// when no authentication is needed.
func (c *Channel) buildAuth() smtp.Auth {
	switch c.config.AuthType {
	case AuthTypePlain:
		return smtp.PlainAuth("", c.config.Username, c.config.Password, c.config.Host)
	case AuthTypeLogin:
		return &loginAuth{username: c.config.Username, password: c.config.Password}
	case AuthTypeCramMD5:
		return smtp.CRAMMD5Auth(c.config.Username, c.config.Password)
	default:
		return nil
	}
}

// loginAuth implements the SMTP LOGIN authentication mechanism. The standard
// library does not provide LOGIN auth, so it is implemented here. A
// loginAuth instance is intended for single-use; create a fresh instance per
// Auth call.
type loginAuth struct {
	username string
	password string
	step     int
}

// Start begins the LOGIN authentication. It advertises the "LOGIN" mechanism
// and sends no initial response; the server is expected to prompt for the
// username.
func (a *loginAuth) Start(_ *smtp.ServerInfo) (mech string, ir []byte, err error) {
	return authLogin, nil, nil
}

// Next responds to server challenges. The first challenge expects the
// username; the second expects the password.
func (a *loginAuth) Next(_ []byte, more bool) ([]byte, error) {
	if !more {
		return nil, nil
	}
	a.step++
	switch a.step {
	case 1:
		return []byte(a.username), nil
	case 2:
		return []byte(a.password), nil
	default:
		return nil, fmt.Errorf("email: unexpected LOGIN challenge at step %d", a.step)
	}
}

// isRetryableErr reports whether err represents a retryable SMTP failure.
// Network errors (dial failures, connection resets, EOF) and 4xx SMTP
// responses are retryable; 5xx responses, authentication configuration
// errors, and context cancellations are not.
func isRetryableErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	// Network errors are retryable.
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	// EOF from a prematurely closed connection is retryable.
	if errors.Is(err, io.EOF) {
		return true
	}
	// SMTP protocol errors: 4xx temporary (retryable), 5xx permanent.
	var protoErr *textproto.Error
	if errors.As(err, &protoErr) {
		return protoErr.Code >= 400 && protoErr.Code < 500
	}
	// Authentication configuration errors (e.g. "smtp: unencrypted
	// connection") are not retryable.
	if strings.Contains(err.Error(), "unencrypted connection") {
		return false
	}
	// Unknown errors are treated as retryable; the circuit breaker
	// provides a backstop against sustained failures.
	return true
}
