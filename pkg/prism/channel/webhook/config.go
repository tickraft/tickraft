// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package webhook

import (
	"errors"
	"fmt"
	"maps"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/circuitbreaker"
	"github.com/tickraft/tickraft/pkg/httpx"
	"github.com/tickraft/tickraft/pkg/retry"
)

// Default configuration values applied when the corresponding Config field
// is zero or negative.
const (
	defaultTimeout          = 10 * time.Second
	defaultRetryMaxAttempts = 3
	defaultRetryBase        = 1 * time.Second
	defaultCircuitThreshold = 5
	defaultCircuitCooldown  = 30 * time.Second
	retryMaxBackoff         = 30 * time.Second
)

// Config configures a webhook Channel. Zero-valued numeric fields and
// durations are replaced with sensible defaults by New.
type Config struct {
	// URL is the HTTP(S) endpoint that receives alert POST requests.
	URL string
	// Timeout is the HTTP client timeout. Defaults to 10s when zero or
	// negative.
	Timeout time.Duration
	// Headers are custom HTTP headers added to every outbound request.
	Headers map[string]string
	// RetryMaxAttempts is the maximum number of send attempts including
	// the first. Defaults to 3 when zero or negative.
	RetryMaxAttempts int
	// RetryBaseInterval is the base interval for exponential backoff
	// between retries. Defaults to 1s when zero or negative.
	RetryBaseInterval time.Duration
	// CircuitFailureThreshold is the number of consecutive send failures
	// that opens the circuit breaker. Defaults to 5 when zero or
	// negative.
	CircuitFailureThreshold int
	// CircuitCooldown is how long the circuit breaker stays open before
	// transitioning to half-open. Defaults to 30s when zero or negative.
	CircuitCooldown time.Duration
}

// Validate checks that the Config is usable. At minimum the URL must be
// non-empty and use the http or https scheme.
func (c Config) Validate() error {
	if c.URL == "" {
		return errors.New("webhook: url is required")
	}
	if !isHTTPURL(c.URL) {
		return fmt.Errorf("webhook: url must use http or https scheme, got %q", c.URL)
	}
	return nil
}

// isHTTPURL reports whether s begins with http:// or https://.
func isHTTPURL(s string) bool {
	lower := strings.ToLower(s)
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}

// Option configures a Channel at construction time. Options are applied
// after the Config and may override Config fields or inject a custom HTTP
// client and logger.
type Option interface {
	apply(*options)
}

// options is the internal builder that merges a Config with Option
// overrides.
type options struct {
	cfg    Config
	client *http.Client
	logger *zap.Logger
}

type funcOption func(*options)

func (f funcOption) apply(o *options) { f(o) }

// WithURL overrides the endpoint URL.
func WithURL(url string) Option {
	return funcOption(func(o *options) { o.cfg.URL = url })
}

// WithTimeout overrides the HTTP client timeout.
func WithTimeout(d time.Duration) Option {
	return funcOption(func(o *options) { o.cfg.Timeout = d })
}

// WithHeaders overrides the custom HTTP headers added to every request.
func WithHeaders(headers map[string]string) Option {
	return funcOption(func(o *options) { o.cfg.Headers = headers })
}

// WithHTTPClient injects a custom HTTP client. Useful for tests and for
// tuning transport parameters. When not set, a new client with the
// configured timeout is created.
func WithHTTPClient(client *http.Client) Option {
	return funcOption(func(o *options) { o.client = client })
}

// WithRetry overrides the retry configuration: maxAttempts is the total
// number of attempts (including the first) and baseInterval is the base
// for exponential backoff.
func WithRetry(maxAttempts int, baseInterval time.Duration) Option {
	return funcOption(func(o *options) {
		o.cfg.RetryMaxAttempts = maxAttempts
		o.cfg.RetryBaseInterval = baseInterval
	})
}

// WithCircuitBreaker overrides the circuit breaker configuration:
// failureThreshold is the consecutive failure count that opens the
// breaker and cooldown is how long it stays open.
func WithCircuitBreaker(failureThreshold int, cooldown time.Duration) Option {
	return funcOption(func(o *options) {
		o.cfg.CircuitFailureThreshold = failureThreshold
		o.cfg.CircuitCooldown = cooldown
	})
}

// WithLogger sets the structured logger. When not set, a no-op logger is
// used.
func WithLogger(logger *zap.Logger) Option {
	return funcOption(func(o *options) { o.logger = logger })
}

// applyDefaults replaces zero or negative Config fields with default
// values.
func applyDefaults(c *Config) {
	if c.Timeout <= 0 {
		c.Timeout = defaultTimeout
	}
	if c.RetryMaxAttempts <= 0 {
		c.RetryMaxAttempts = defaultRetryMaxAttempts
	}
	if c.RetryBaseInterval <= 0 {
		c.RetryBaseInterval = defaultRetryBase
	}
	if c.CircuitFailureThreshold <= 0 {
		c.CircuitFailureThreshold = defaultCircuitThreshold
	}
	if c.CircuitCooldown <= 0 {
		c.CircuitCooldown = defaultCircuitCooldown
	}
}

// New creates a Channel from the given Config and Options. The Config is
// validated after Options are applied, so Options such as WithURL may
// supply a value the Config left empty.
//
// Returns an error if the effective configuration is invalid or the
// retry backoff cannot be constructed.
func New(cfg Config, opts ...Option) (*Channel, error) {
	o := &options{cfg: cfg}
	for _, opt := range opts {
		opt.apply(o)
	}
	if err := o.cfg.Validate(); err != nil {
		return nil, err
	}
	applyDefaults(&o.cfg)

	client := o.client
	if client == nil {
		client = httpx.NewPoolClient(httpx.Config{Timeout: o.cfg.Timeout})
	} else if client.Timeout <= 0 {
		client.Timeout = o.cfg.Timeout
	}

	logger := o.logger
	if logger == nil {
		logger = zap.NewNop()
	}

	base := o.cfg.RetryBaseInterval
	maxBackoff := retryMaxBackoff
	maxBackoff = max(maxBackoff, base)
	backoff, err := retry.NewExponential(
		base,
		maxBackoff,
		retry.WithJitter(retry.NewFullJitter()),
	)
	if err != nil {
		return nil, fmt.Errorf("webhook: build backoff: %w", err)
	}
	r, err := retry.New(
		retry.WithMaxAttempts(o.cfg.RetryMaxAttempts),
		retry.WithBackoff(backoff),
		retry.WithRetryable(isRetryableSendErr),
	)
	if err != nil {
		return nil, fmt.Errorf("webhook: build retry: %w", err)
	}

	breaker := circuitbreaker.New(circuitbreaker.Config{
		FailureThreshold: o.cfg.CircuitFailureThreshold,
		Cooldown:         o.cfg.CircuitCooldown,
	})

	headers := make(map[string]string, len(o.cfg.Headers))
	maps.Copy(headers, o.cfg.Headers)

	return &Channel{
		cfg:     o.cfg,
		headers: headers,
		client:  client,
		logger:  logger,
		retry:   r,
		breaker: breaker,
	}, nil
}
