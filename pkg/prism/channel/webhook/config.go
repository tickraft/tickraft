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
	"github.com/tickraft/tickraft/pkg/prism/channel/httpclient"
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
	// ProxyURL is an optional per-channel outbound proxy. Supported
	// schemes are http, https, and socks5. When empty the
	// deployment-wide egress proxy applies (when configured).
	ProxyURL string
	// ProxyBypass is the NO_PROXY-style host/domain suffix list applied
	// together with ProxyURL. It is set by the build layer from the
	// deployment-wide egress settings and is never user-facing config.
	ProxyBypass []string
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
	if err := httpclient.ValidateProxyURL(c.ProxyURL); err != nil {
		return fmt.Errorf("webhook: %w", err)
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
	apply(*webhookOptions)
}

// webhookOptions is the internal builder that merges a Config with Option
// overrides.
type webhookOptions struct {
	cfg    Config
	client *http.Client
	logger *zap.Logger
}

// urlOption overrides the endpoint URL.
type urlOption string

func (o urlOption) apply(options *webhookOptions) { options.cfg.URL = string(o) }

// WithURL overrides the endpoint URL.
func WithURL(url string) Option {
	return urlOption(url)
}

// timeoutOption overrides the HTTP client timeout.
type timeoutOption time.Duration

func (o timeoutOption) apply(options *webhookOptions) { options.cfg.Timeout = time.Duration(o) }

// WithTimeout overrides the HTTP client timeout.
func WithTimeout(timeout time.Duration) Option {
	return timeoutOption(timeout)
}

// headersOption overrides the custom HTTP headers added to every request.
type headersOption struct {
	headers map[string]string
}

func (o headersOption) apply(options *webhookOptions) { options.cfg.Headers = o.headers }

// WithHeaders overrides the custom HTTP headers added to every request.
func WithHeaders(headers map[string]string) Option {
	return headersOption{headers: headers}
}

// httpClientOption injects a custom HTTP client.
type httpClientOption struct {
	client *http.Client
}

func (o httpClientOption) apply(options *webhookOptions) { options.client = o.client }

// WithHTTPClient injects a custom HTTP client. Useful for tests and for
// tuning transport parameters. When not set, a new client with the
// configured timeout is created.
func WithHTTPClient(client *http.Client) Option {
	return httpClientOption{client: client}
}

// retryOption overrides the retry configuration.
type retryOption struct {
	maxAttempts  int
	baseInterval time.Duration
}

func (o retryOption) apply(options *webhookOptions) {
	options.cfg.RetryMaxAttempts = o.maxAttempts
	options.cfg.RetryBaseInterval = o.baseInterval
}

// WithRetry overrides the retry configuration: maxAttempts is the total
// number of attempts (including the first) and baseInterval is the base
// for exponential backoff.
func WithRetry(maxAttempts int, baseInterval time.Duration) Option {
	return retryOption{maxAttempts: maxAttempts, baseInterval: baseInterval}
}

// circuitBreakerOption overrides the circuit breaker configuration.
type circuitBreakerOption struct {
	failureThreshold int
	cooldown         time.Duration
}

func (o circuitBreakerOption) apply(options *webhookOptions) {
	options.cfg.CircuitFailureThreshold = o.failureThreshold
	options.cfg.CircuitCooldown = o.cooldown
}

// WithCircuitBreaker overrides the circuit breaker configuration:
// failureThreshold is the consecutive failure count that opens the
// breaker and cooldown is how long it stays open.
func WithCircuitBreaker(failureThreshold int, cooldown time.Duration) Option {
	return circuitBreakerOption{failureThreshold: failureThreshold, cooldown: cooldown}
}

// loggerOption sets the structured logger.
type loggerOption struct {
	logger *zap.Logger
}

func (o loggerOption) apply(options *webhookOptions) { options.logger = o.logger }

// WithLogger sets the structured logger. When not set, a no-op logger is
// used.
func WithLogger(logger *zap.Logger) Option {
	return loggerOption{logger: logger}
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
func New(cfg Config, options ...Option) (*Channel, error) {
	opts := &webhookOptions{cfg: cfg}
	for _, o := range options {
		o.apply(opts)
	}
	if err := opts.cfg.Validate(); err != nil {
		return nil, err
	}
	applyDefaults(&opts.cfg)

	client := opts.client
	if client == nil {
		c, err := httpclient.New(httpclient.Config{
			ProxyURL:    opts.cfg.ProxyURL,
			ProxyBypass: opts.cfg.ProxyBypass,
			Timeout:     opts.cfg.Timeout,
		})
		if err != nil {
			return nil, fmt.Errorf("webhook: build http client: %w", err)
		}
		client = c
	} else if client.Timeout <= 0 {
		client.Timeout = opts.cfg.Timeout
	}

	logger := opts.logger
	if logger == nil {
		logger = zap.NewNop()
	}

	base := opts.cfg.RetryBaseInterval
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
		retry.WithMaxAttempts(opts.cfg.RetryMaxAttempts),
		retry.WithBackoff(backoff),
		retry.WithRetryable(isRetryableSendErr),
	)
	if err != nil {
		return nil, fmt.Errorf("webhook: build retry: %w", err)
	}

	breaker := circuitbreaker.New(circuitbreaker.Config{
		FailureThreshold: opts.cfg.CircuitFailureThreshold,
		Cooldown:         opts.cfg.CircuitCooldown,
	})

	headers := make(map[string]string, len(opts.cfg.Headers))
	maps.Copy(headers, opts.cfg.Headers)

	return &Channel{
		cfg:     opts.cfg,
		headers: headers,
		client:  client,
		logger:  logger,
		retry:   r,
		breaker: breaker,
	}, nil
}
