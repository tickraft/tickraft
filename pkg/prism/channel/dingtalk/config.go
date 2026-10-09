// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package dingtalk

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/circuitbreaker"
	"github.com/tickraft/tickraft/pkg/i18n"
	"github.com/tickraft/tickraft/pkg/prism/alert/template"
	"github.com/tickraft/tickraft/pkg/prism/channel/format"
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

// MessageType constants for the supported DingTalk message formats.
const (
	MessageTypeText     = "text"
	MessageTypeMarkdown = "markdown"
)

// Config configures a Dingtalk Channel. Zero-valued numeric fields and
// durations are replaced with sensible defaults by New.
type Config struct {
	// WebhookURL is the DingTalk robot webhook endpoint (must be https).
	WebhookURL string `json:"webhook_url"`
	// Secret is the HMAC-SHA256 signing key. When empty, requests are
	// sent unsigned.
	Secret string `json:"secret"`
	// Keyword is the robot's keyword-filter string. When non-empty it is
	// appended to the message text so keyword-protected robots accept the
	// message.
	Keyword string `json:"keyword"`
	// AppKey and AppSecret carry the application credentials (robot
	// code / StreamMode client identity) configured in the DingTalk
	// developer console. The outbound webhook channel itself does not
	// use them: they are stored here so the deployment's inbound event
	// subscription (the Stream-mode WebSocket client) can resolve them
	// from the same channel configuration. Both are optional.
	AppKey    string `json:"app_key,omitempty"`
	AppSecret string `json:"app_secret,omitempty"`
	// BaseURL overrides the DingTalk open API base URL (default
	// https://api.dingtalk.com). Dedicated-edition deployments point it at
	// their private DingTalk OpenAPI endpoint. The outbound webhook channel
	// itself does not use it: like AppKey/AppSecret it is stored here so the
	// deployment's inbound event subscription (the Stream-mode client) can
	// resolve it from the same channel configuration.
	BaseURL string `json:"base_url,omitempty"`
	// MessageType is the DingTalk message type: "text" or "markdown".
	// Defaults to "text" when empty.
	MessageType string `json:"message_type"`
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
	// FrontendBaseURL is the optional base URL of the frontend, used to
	// construct a deep link to the triggering asset detail page. When
	// empty the message omits the asset link.
	FrontendBaseURL string `json:"frontend_base_url"`
	// ProxyURL is an optional per-channel outbound proxy. Supported
	// schemes are http, https, and socks5. When empty the
	// deployment-wide egress proxy applies (when configured).
	ProxyURL string `json:"proxy_url"`
	// ProxyBypass is the NO_PROXY-style host/domain suffix list applied
	// together with ProxyURL. It is set by the build layer from the
	// deployment-wide egress settings and is never part of the stored
	// user config JSON.
	ProxyBypass []string `json:"-"`
	// Scope carries the network-scope rendering policy (M5 private
	// deployment: scoped template variants, content masking, link
	// adaptation). Set by the build layer from the deployment-wide scope
	// settings and never part of the stored user config JSON.
	Scope format.ScopeOptions `json:"-"`
	// ID is the sys_prism_channel row this channel was built
	// from. It is set by the DB loader and carried into delivery
	// records; it is never part of the stored user config JSON.
	ID int64 `json:"-"`
	// Name is the display name of the channel configuration, carried
	// into delivery records. Not part of the stored user config JSON.
	Name string `json:"-"`
}

// Validate checks that the Config is usable. The WebhookURL must be
// non-empty and use the https scheme. The Secret may be empty (no
// signing).
func (c Config) Validate() error {
	if c.WebhookURL == "" {
		return errors.New("dingtalk: webhook url is required")
	}
	if !isHTTPSURL(c.WebhookURL) {
		return fmt.Errorf("dingtalk: webhook url must use https scheme, got %q", c.WebhookURL)
	}
	if c.BaseURL != "" && !isHTTPURL(c.BaseURL) {
		return fmt.Errorf("dingtalk: base url must use http or https scheme, got %q", c.BaseURL)
	}
	if err := httpclient.ValidateProxyURL(c.ProxyURL); err != nil {
		return fmt.Errorf("dingtalk: %w", err)
	}
	return nil
}

// isHTTPURL reports whether s is a URL with the http or https scheme.
func isHTTPURL(s string) bool {
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Scheme, "http") || strings.EqualFold(u.Scheme, "https")
}

// isHTTPSURL reports whether s is a valid https URL.
func isHTTPSURL(s string) bool {
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Scheme, "https")
}

// Option configures a Channel at construction time. Options are applied
// after the Config and may override Config fields or inject a custom HTTP
// client and logger.
type Option interface {
	apply(*dingtalkOptions)
}

// dingtalkOptions is the internal builder that merges a Config with Option
// overrides.
type dingtalkOptions struct {
	cfg       Config
	client    *http.Client
	logger    *zap.Logger
	formatter i18n.Formatter
	library   template.Library
	registry  i18n.Registry
}

type webhookURLOption string

func (o webhookURLOption) apply(options *dingtalkOptions) { options.cfg.WebhookURL = string(o) }

// WithWebhookURL overrides the webhook endpoint URL.
func WithWebhookURL(webhookURL string) Option {
	return webhookURLOption(webhookURL)
}

type secretOption string

func (o secretOption) apply(options *dingtalkOptions) { options.cfg.Secret = string(o) }

// WithSecret overrides the signing secret. An empty secret disables
// request signing.
func WithSecret(secret string) Option {
	return secretOption(secret)
}

type messageTypeOption string

func (o messageTypeOption) apply(options *dingtalkOptions) { options.cfg.MessageType = string(o) }

// WithMessageType overrides the DingTalk message type ("text" or
// "markdown").
func WithMessageType(msgType string) Option {
	return messageTypeOption(msgType)
}

type httpClientOption struct {
	client *http.Client
}

func (o httpClientOption) apply(options *dingtalkOptions) { options.client = o.client }

// WithHTTPClient injects a custom HTTP client. Useful for tests and for
// tuning transport parameters. When not set, a new client with a 10s
// timeout is created.
func WithHTTPClient(client *http.Client) Option {
	return httpClientOption{client: client}
}

type retryOption struct {
	maxAttempts  int
	baseInterval time.Duration
}

func (o retryOption) apply(options *dingtalkOptions) {
	options.cfg.RetryMaxAttempts = o.maxAttempts
	options.cfg.RetryBaseInterval = o.baseInterval
}

// WithRetry overrides the retry configuration: maxAttempts is the total
// number of attempts (including the first) and baseInterval is the base
// for exponential backoff.
func WithRetry(maxAttempts int, baseInterval time.Duration) Option {
	return retryOption{maxAttempts: maxAttempts, baseInterval: baseInterval}
}

type circuitBreakerOption struct {
	failureThreshold int
	cooldown         time.Duration
}

func (o circuitBreakerOption) apply(options *dingtalkOptions) {
	options.cfg.CircuitFailureThreshold = o.failureThreshold
	options.cfg.CircuitCooldown = o.cooldown
}

// WithCircuitBreaker overrides the circuit breaker configuration:
// failureThreshold is the consecutive failure count that opens the
// breaker and cooldown is how long it stays open.
func WithCircuitBreaker(failureThreshold int, cooldown time.Duration) Option {
	return circuitBreakerOption{failureThreshold: failureThreshold, cooldown: cooldown}
}

type loggerOption struct {
	logger *zap.Logger
}

func (o loggerOption) apply(options *dingtalkOptions) { options.logger = o.logger }

// WithLogger sets the structured logger. When not set, a no-op logger is
// used.
func WithLogger(logger *zap.Logger) Option {
	return loggerOption{logger: logger}
}

type formatterOption struct {
	f i18n.Formatter
}

func (o formatterOption) apply(options *dingtalkOptions) { options.formatter = o.f }

// WithFormatter injects an i18n Formatter for locale-aware alert rendering.
func WithFormatter(f i18n.Formatter) Option {
	return formatterOption{f: f}
}

type libraryOption struct {
	l template.Library
}

func (o libraryOption) apply(options *dingtalkOptions) { options.library = o.l }

// WithLibrary injects an alert template Library for template-based rendering.
func WithLibrary(l template.Library) Option {
	return libraryOption{l: l}
}

type registryOption struct {
	r i18n.Registry
}

func (o registryOption) apply(options *dingtalkOptions) { options.registry = o.r }

// WithRegistry injects an i18n Registry used by the template renderer.
func WithRegistry(r i18n.Registry) Option {
	return registryOption{r: r}
}

type frontendBaseURLOption string

func (o frontendBaseURLOption) apply(options *dingtalkOptions) {
	options.cfg.FrontendBaseURL = string(o)
}

// WithFrontendBaseURL overrides the frontend base URL used to build asset
// deep links inside the message.
func WithFrontendBaseURL(frontendBaseURL string) Option {
	return frontendBaseURLOption(frontendBaseURL)
}

// applyDefaults replaces zero or negative Config fields with default
// values.
func applyDefaults(c *Config) {
	if c.MessageType == "" {
		c.MessageType = MessageTypeText
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
// validated after Options are applied, so Options such as WithWebhookURL
// may supply a value the Config left empty.
//
// Returns an error if the effective configuration is invalid or the
// retry backoff cannot be constructed.
func New(cfg Config, options ...Option) (*Channel, error) {
	opts := &dingtalkOptions{cfg: cfg}
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
			Region:      httpclient.RegionCN,
		})
		if err != nil {
			return nil, fmt.Errorf("dingtalk: build http client: %w", err)
		}
		client = c
	} else if client.Timeout <= 0 {
		client.Timeout = defaultTimeout
	}

	logger := opts.logger
	if logger == nil {
		logger = zap.NewNop()
	}

	base := opts.cfg.RetryBaseInterval
	maxBackoff := max(retryMaxBackoff, base)
	backoff, err := retry.NewExponential(
		base,
		maxBackoff,
		retry.WithJitter(retry.NewFullJitter()),
	)
	if err != nil {
		return nil, fmt.Errorf("dingtalk: build backoff: %w", err)
	}
	r, err := retry.New(
		retry.WithMaxAttempts(opts.cfg.RetryMaxAttempts),
		retry.WithBackoff(backoff),
		retry.WithRetryable(isRetryableSendErr),
	)
	if err != nil {
		return nil, fmt.Errorf("dingtalk: build retry: %w", err)
	}

	breaker := circuitbreaker.New(circuitbreaker.Config{
		FailureThreshold: opts.cfg.CircuitFailureThreshold,
		Cooldown:         opts.cfg.CircuitCooldown,
	})

	return &Channel{
		cfg:        opts.cfg,
		httpClient: client,
		logger:     logger,
		retry:      r,
		circuit:    breaker,
		formatter:  opts.formatter,
		library:    opts.library,
		registry:   opts.registry,
	}, nil
}

// SetIdentity stamps the originating configuration row's identity onto
// the config. It implements the channel.IdentitySetter interface used by
// the database loader.
func (c *Config) SetIdentity(id int64, name string) {
	c.ID, c.Name = id, name
}
