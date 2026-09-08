// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package feishu

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
	"github.com/tickraft/tickraft/pkg/retry"

	"github.com/tickraft/tickraft/pkg/prism/channel/httpclient"
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

// MessageType selects the feishu message format.
type MessageType string

const (
	// MessageTypeText renders the alert as a plain-text feishu message.
	MessageTypeText MessageType = "text"
	// MessageTypeInteractive renders the alert as a feishu interactive
	// card with a header, level, description, timestamp, and an optional
	// resource link button. This is the default format.
	MessageTypeInteractive MessageType = "interactive"
)

// Mode selects between robot (custom-bot webhook) and app (application
// API) delivery modes. App mode is the mode whose cards can carry
// callback buttons: cards sent by an application route their button
// callbacks to the application's event subscription, while custom-robot
// cards only support link buttons.
type Mode string

const (
	// ModeRobot delivers messages via a custom robot webhook.
	ModeRobot Mode = "webhook"
	// ModeApp delivers messages via the application messaging API.
	ModeApp Mode = "app"
)

// Config configures a feishu Channel. Zero-valued numeric fields and
// durations are replaced with sensible defaults by New.
type Config struct {
	// Mode selects the delivery mode: webhook (custom robot) or app
	// (application API). When empty the mode is inferred: a webhook_url
	// selects webhook mode, otherwise an app_id selects app mode.
	Mode Mode `json:"mode"`
	// WebhookURL is the feishu custom robot webhook endpoint. Required
	// for webhook mode. Must use the https scheme.
	WebhookURL string `json:"webhook_url"`
	// Secret is the optional HMAC-SHA256 signing secret. When non-empty,
	// each request body is augmented with a fresh timestamp and sign
	// field computed from the current time. When empty, requests are
	// sent unsigned. Webhook mode only.
	Secret string `json:"secret"`
	// AppID is the application ID, required for app mode.
	AppID string `json:"app_id"`
	// AppSecret is the application secret, required for app mode. It
	// authenticates the tenant_access_token request; the token is cached
	// and refreshed proactively before expiry.
	AppSecret string `json:"app_secret"`
	// ChatID is the group chat the application sends to, required for
	// app mode (the application's bot must be a member of the chat).
	ChatID string `json:"chat_id"`
	// BaseURL overrides the feishu open API base URL for app mode
	// (default https://open.feishu.cn). Dedicated-edition deployments
	// point it at their private feishu OpenAPI endpoint. Ignored in
	// webhook mode.
	BaseURL string `json:"base_url,omitempty"`
	// MessageType is the feishu message format: text or interactive.
	// Defaults to interactive when empty.
	MessageType MessageType `json:"message_type"`
	// FrontendBaseURL is the optional base URL of the frontend, used to
	// construct a deep link to the triggering asset detail page. When
	// empty the interactive card omits the asset link button.
	FrontendBaseURL string `json:"frontend_base_url"`
	// ProxyURL is an optional proxy URL used for outbound network
	// optimization. Supported schemes are http, https, and socks5. When
	// empty the client connects directly (or via the deployment-wide
	// egress proxy resolved by the build layer).
	ProxyURL string `json:"proxy_url"`
	// ProxyBypass is the NO_PROXY-style host/domain suffix list applied
	// together with ProxyURL. It is set by the build layer from the
	// deployment-wide egress settings and is never part of the stored
	// user config JSON.
	ProxyBypass []string `json:"-"`
	// PlainNotificationOnly is the L0 degraded-notification policy: the
	// channel always renders plain text (never interactive cards) and
	// annotates resource links as intranet addresses. Set by the build
	// layer for network-isolated deployments; never stored.
	PlainNotificationOnly bool `json:"-"`
	// Scope carries the network-scope rendering policy (M5 private
	// deployment: scoped template variants, content masking, link
	// adaptation). Set by the build layer from the deployment-wide scope
	// settings and never part of the stored user config JSON.
	Scope format.ScopeOptions `json:"-"`
	// Interaction carries the interactive-card policy (M5 L1 inbound
	// events). When enabled and the channel runs in app mode, the card
	// renders callback buttons whose values carry the action verb and the
	// alert's EventID. Set by the build layer; never stored.
	Interaction format.InteractionOptions `json:"-"`
	// Timeout is the HTTP client timeout. When zero or negative a
	// region-aware default is applied by the httpclient builder.
	Timeout time.Duration
	// Region selects the default timeout policy when Timeout is not set.
	// RegionCN yields a 10s default; RegionGlobal yields a 15s default.
	// Defaults to RegionCN for feishu (a domestic endpoint).
	Region httpclient.Region `json:"region"`
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
	// ID is the sys_prism_channel row this channel was built
	// from. It is set by the DB loader and carried into delivery
	// records; it is never part of the stored user config JSON.
	ID int64 `json:"-"`
	// Name is the display name of the channel configuration, carried
	// into delivery records. Not part of the stored user config JSON.
	Name string `json:"-"`
}

// Validate checks that the Config is usable for the configured Mode.
// Webhook mode requires a non-empty https WebhookURL; app mode requires
// AppID, AppSecret, and ChatID. An empty Mode is inferred the same way
// applyDefaults infers it (webhook_url selects webhook mode, otherwise
// app_id selects app mode) so configs stored without an explicit mode
// validate. A non-empty ProxyURL must be a well-formed http/https/socks5
// URL.
func (c Config) Validate() error {
	mode := c.Mode
	if mode == "" {
		switch {
		case c.WebhookURL != "":
			mode = ModeRobot
		case c.AppID != "":
			mode = ModeApp
		}
	}
	switch mode {
	case ModeApp:
		if c.AppID == "" {
			return errors.New("feishu: app_id is required for app mode")
		}
		if c.AppSecret == "" {
			return errors.New("feishu: app_secret is required for app mode")
		}
		if c.ChatID == "" {
			return errors.New("feishu: chat_id is required for app mode")
		}
	case ModeRobot:
		if c.WebhookURL == "" {
			return errors.New("feishu: webhook url is required")
		}
		if !isHTTPSURL(c.WebhookURL) {
			return fmt.Errorf("feishu: webhook url must use https scheme, got %q", c.WebhookURL)
		}
	default:
		return fmt.Errorf("feishu: invalid mode %q, must be %q or %q", c.Mode, ModeRobot, ModeApp)
	}
	if c.BaseURL != "" && !isHTTPURL(c.BaseURL) {
		return fmt.Errorf("feishu: base url must use http or https scheme, got %q", c.BaseURL)
	}
	if err := httpclient.ValidateProxyURL(c.ProxyURL); err != nil {
		return fmt.Errorf("feishu: %w", err)
	}
	return nil
}

// isHTTPURL reports whether s is a URL with the http or https scheme and a
// parseable structure.
func isHTTPURL(s string) bool {
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Scheme, "http") || strings.EqualFold(u.Scheme, "https")
}

// isHTTPSURL reports whether s is a URL with the https scheme and a
// parseable structure.
func isHTTPSURL(s string) bool {
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Scheme, "https")
}

// Option configures a Channel at construction time. Options are applied
// after the Config and may override Config fields or inject a custom HTTP
// client and logger.
type Option interface {
	apply(*feishuOptions)
}

// feishuOptions is the internal builder that merges a Config with Option
// overrides.
type feishuOptions struct {
	cfg       Config
	client    *http.Client
	logger    *zap.Logger
	formatter i18n.Formatter
	library   template.Library
	registry  i18n.Registry
	baseURL   string
}

type webhookURLOption string

func (o webhookURLOption) apply(options *feishuOptions) { options.cfg.WebhookURL = string(o) }

// WithWebhookURL overrides the feishu robot webhook endpoint.
func WithWebhookURL(webhookURL string) Option {
	return webhookURLOption(webhookURL)
}

type secretOption string

func (o secretOption) apply(options *feishuOptions) { options.cfg.Secret = string(o) }

// WithSecret overrides the signing secret. An empty secret disables
// request signing.
func WithSecret(secret string) Option {
	return secretOption(secret)
}

type messageTypeOption struct {
	mt MessageType
}

func (o messageTypeOption) apply(options *feishuOptions) { options.cfg.MessageType = o.mt }

// WithMessageType overrides the feishu message format (text or
// interactive).
func WithMessageType(mt MessageType) Option {
	return messageTypeOption{mt: mt}
}

type frontendBaseURLOption string

func (o frontendBaseURLOption) apply(options *feishuOptions) { options.cfg.FrontendBaseURL = string(o) }

// WithFrontendBaseURL overrides the frontend base URL used to build
// asset deep links inside interactive cards.
func WithFrontendBaseURL(frontendBaseURL string) Option {
	return frontendBaseURLOption(frontendBaseURL)
}

type proxyURLOption string

func (o proxyURLOption) apply(options *feishuOptions) { options.cfg.ProxyURL = string(o) }

// WithProxyURL overrides the outbound proxy URL used by the HTTP client.
func WithProxyURL(proxyURL string) Option {
	return proxyURLOption(proxyURL)
}

type plainNotificationOnlyOption bool

func (o plainNotificationOnlyOption) apply(options *feishuOptions) {
	options.cfg.PlainNotificationOnly = bool(o)
}

// WithPlainNotificationOnly enables the L0 degraded-notification policy:
// plain-text rendering with intranet-annotated links, never interactive
// cards. It overrides the MessageType-derived format.
func WithPlainNotificationOnly(enabled bool) Option {
	return plainNotificationOnlyOption(enabled)
}

type interactionOption struct{ interaction format.InteractionOptions }

func (o interactionOption) apply(options *feishuOptions) {
	options.cfg.Interaction = o.interaction
}

// WithInteraction injects the interactive-card policy. When enabled and
// the channel runs in app mode, cards carry callback buttons instead of
// link-only actions.
func WithInteraction(interaction format.InteractionOptions) Option {
	return interactionOption{interaction: interaction}
}

type baseURLOption string

func (o baseURLOption) apply(options *feishuOptions) { options.baseURL = string(o) }

// withBaseURL overrides the feishu API base URL used by app mode. It is
// intended primarily for tests that point the channel at an httptest
// server.
func withBaseURL(baseURL string) Option { return baseURLOption(baseURL) }

type retryOption struct {
	maxAttempts  int
	baseInterval time.Duration
}

func (o retryOption) apply(options *feishuOptions) {
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

func (o circuitBreakerOption) apply(options *feishuOptions) {
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

func (o loggerOption) apply(options *feishuOptions) { options.logger = o.logger }

// WithLogger sets the structured logger. When not set, a no-op logger is
// used.
func WithLogger(logger *zap.Logger) Option {
	return loggerOption{logger: logger}
}

type formatterOption struct {
	f i18n.Formatter
}

func (o formatterOption) apply(options *feishuOptions) { options.formatter = o.f }

// WithFormatter injects an i18n Formatter for locale-aware alert rendering.
func WithFormatter(f i18n.Formatter) Option {
	return formatterOption{f: f}
}

type libraryOption struct {
	l template.Library
}

func (o libraryOption) apply(options *feishuOptions) { options.library = o.l }

// WithLibrary injects an alert template Library for template-based rendering.
func WithLibrary(l template.Library) Option {
	return libraryOption{l: l}
}

type registryOption struct {
	r i18n.Registry
}

func (o registryOption) apply(options *feishuOptions) { options.registry = o.r }

// WithRegistry injects an i18n Registry used by the template renderer.
func WithRegistry(r i18n.Registry) Option {
	return registryOption{r: r}
}

type httpClientOption struct {
	client *http.Client
}

func (o httpClientOption) apply(options *feishuOptions) { options.client = o.client }

// WithHTTPClient injects a custom HTTP client. Useful for tests and for
// tuning transport parameters. When not set, a new client is built from
// the ProxyURL, Timeout, and Region via the httpclient builder.
func WithHTTPClient(client *http.Client) Option {
	return httpClientOption{client: client}
}

// applyDefaults replaces zero or negative Config fields with default
// values. The MessageType defaults to interactive and the Region defaults
// to RegionCN. The Timeout is intentionally left to the httpclient
// builder so a region-aware default is applied when zero.
func applyDefaults(c *Config) {
	if c.Mode == "" {
		// A config that carries only a webhook (the shape stored by the
		// channel management UI since the first release) implicitly
		// selects webhook mode; app credentials select app mode.
		switch {
		case c.WebhookURL != "":
			c.Mode = ModeRobot
		case c.AppID != "":
			c.Mode = ModeApp
		}
	}
	if c.MessageType == "" {
		c.MessageType = MessageTypeInteractive
	}
	if c.Region == "" {
		c.Region = httpclient.RegionCN
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
// When no custom HTTP client is provided, a client is built from the
// ProxyURL, Timeout, and Region via the httpclient builder (supporting
// proxy routing and region-aware timeouts).
//
// Returns an error if the effective configuration is invalid, the HTTP
// client cannot be constructed, or the retry backoff cannot be
// constructed.
func New(cfg Config, options ...Option) (*Channel, error) {
	opts := &feishuOptions{cfg: cfg}
	for _, o := range options {
		o.apply(opts)
	}
	// An explicit option (test injection) wins over the stored config;
	// an empty value falls back to the official endpoint at call time.
	if opts.baseURL == "" {
		opts.baseURL = opts.cfg.BaseURL
	}
	// The mode is inferred before validation so configs stored by the
	// channel management UI (a bare webhook_url, no explicit mode) and
	// app-credential configs both validate without an explicit mode.
	applyDefaults(&opts.cfg)
	if err := opts.cfg.Validate(); err != nil {
		return nil, err
	}

	client := opts.client
	if client == nil {
		c, err := httpclient.New(httpclient.Config{
			ProxyURL:    opts.cfg.ProxyURL,
			ProxyBypass: opts.cfg.ProxyBypass,
			Timeout:     opts.cfg.Timeout,
			Region:      opts.cfg.Region,
		})
		if err != nil {
			return nil, fmt.Errorf("feishu: build http client: %w", err)
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
		return nil, fmt.Errorf("feishu: build backoff: %w", err)
	}
	r, err := retry.New(
		retry.WithMaxAttempts(opts.cfg.RetryMaxAttempts),
		retry.WithBackoff(backoff),
		retry.WithRetryable(isRetryableSendErr),
	)
	if err != nil {
		return nil, fmt.Errorf("feishu: build retry: %w", err)
	}

	breaker := circuitbreaker.New(circuitbreaker.Config{
		FailureThreshold: opts.cfg.CircuitFailureThreshold,
		Cooldown:         opts.cfg.CircuitCooldown,
	})

	return &Channel{
		cfg:        opts.cfg,
		baseURL:    opts.baseURL,
		httpClient: client,
		retry:      r,
		circuit:    breaker,
		logger:     logger,
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
