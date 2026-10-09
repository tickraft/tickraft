// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package telegram

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/circuitbreaker"
	"github.com/tickraft/tickraft/pkg/i18n"
	"github.com/tickraft/tickraft/pkg/prism/alert/template"
	"github.com/tickraft/tickraft/pkg/retry"

	"github.com/tickraft/tickraft/pkg/prism/channel/format"
	"github.com/tickraft/tickraft/pkg/prism/channel/httpclient"
)

// Default configuration values applied when the corresponding Config field
// is zero or negative. Telegram is an international endpoint so the default
// timeout aligns with the global region default (15s).
const (
	defaultTimeout          = 15 * time.Second
	defaultRetryMaxAttempts = 3
	defaultRetryBase        = 1 * time.Second
	defaultCircuitThreshold = 5
	defaultCircuitCooldown  = 30 * time.Second
	retryMaxBackoff         = 30 * time.Second
	defaultAPIBase          = "https://api.telegram.org"
)

// Config configures a Telegram Channel. Zero-valued numeric fields and
// durations are replaced with sensible defaults by New.
type Config struct {
	// BotToken is the Telegram bot token. It has the form
	// "123456:ABC-DEF..." and is embedded in the Bot API request URL path.
	// It is a credential: it is never logged in plaintext (see
	// redactToken).
	BotToken string `json:"bot_token"`
	// ChatID is the target chat or channel ID to send messages to.
	ChatID string `json:"chat_id"`
	// FrontendBaseURL is the optional base URL of the frontend, used to
	// construct a deep link to the triggering resource detail page. When
	// empty the message omits the resource link.
	FrontendBaseURL string `json:"frontend_base_url"`
	// ProxyURL is an optional proxy URL used to reach the Telegram Bot API
	// from networks where it is not directly accessible (e.g. mainland
	// China). Supported schemes are http, https, and socks5. When empty
	// the client connects directly.
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
	// Timeout is the HTTP client timeout. When zero or negative a
	// region-aware default is applied by the httpclient builder.
	Timeout time.Duration
	// Region selects the default timeout policy when Timeout is not set.
	// RegionCN yields a 10s default; RegionGlobal yields a 15s default.
	// Defaults to RegionGlobal for Telegram (an international endpoint).
	Region httpclient.Region `json:"region"`
	// APIBase is the Telegram Bot API base URL. It defaults to
	// "https://api.telegram.org" when empty. It can be overridden for
	// testing or to route through a Bot API server instance.
	APIBase string `json:"api_base"`
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

// Validate checks that the Config is usable. Both BotToken and ChatID must
// be non-empty.
func (c Config) Validate() error {
	if c.BotToken == "" {
		return errors.New("telegram: bot token is required")
	}
	if c.ChatID == "" {
		return errors.New("telegram: chat id is required")
	}
	if err := httpclient.ValidateProxyURL(c.ProxyURL); err != nil {
		return fmt.Errorf("telegram: %w", err)
	}
	return nil
}

// Option configures a Channel at construction time. Options are applied
// after the Config and may override Config fields or inject a custom HTTP
// client and logger.
type Option interface {
	apply(*telegramOptions)
}

// telegramOptions is the internal builder that merges a Config with Option
// overrides.
type telegramOptions struct {
	cfg       Config
	client    *http.Client
	logger    *zap.Logger
	formatter i18n.Formatter
	library   template.Library
	registry  i18n.Registry
}

type botTokenOption string

func (o botTokenOption) apply(options *telegramOptions) { options.cfg.BotToken = string(o) }

// WithBotToken overrides the Telegram bot token.
func WithBotToken(token string) Option {
	return botTokenOption(token)
}

type chatIDOption string

func (o chatIDOption) apply(options *telegramOptions) { options.cfg.ChatID = string(o) }

// WithChatID overrides the target chat or channel ID.
func WithChatID(chatID string) Option {
	return chatIDOption(chatID)
}

type frontendBaseURLOption string

func (o frontendBaseURLOption) apply(options *telegramOptions) {
	options.cfg.FrontendBaseURL = string(o)
}

// WithFrontendBaseURL overrides the frontend base URL used to build
// resource deep links inside the message.
func WithFrontendBaseURL(frontendBaseURL string) Option {
	return frontendBaseURLOption(frontendBaseURL)
}

type proxyURLOption string

func (o proxyURLOption) apply(options *telegramOptions) { options.cfg.ProxyURL = string(o) }

// WithProxyURL overrides the outbound proxy URL used by the HTTP client.
func WithProxyURL(proxyURL string) Option {
	return proxyURLOption(proxyURL)
}

type apiBaseOption string

func (o apiBaseOption) apply(options *telegramOptions) { options.cfg.APIBase = string(o) }

// WithAPIBase overrides the Telegram Bot API base URL.
func WithAPIBase(apiBase string) Option {
	return apiBaseOption(apiBase)
}

type retryOption struct {
	maxAttempts  int
	baseInterval time.Duration
}

func (o retryOption) apply(options *telegramOptions) {
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

func (o circuitBreakerOption) apply(options *telegramOptions) {
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

func (o loggerOption) apply(options *telegramOptions) { options.logger = o.logger }

// WithLogger sets the structured logger. When not set, a no-op logger is
// used.
func WithLogger(logger *zap.Logger) Option {
	return loggerOption{logger: logger}
}

type formatterOption struct {
	f i18n.Formatter
}

func (o formatterOption) apply(options *telegramOptions) { options.formatter = o.f }

// WithFormatter injects an i18n Formatter for locale-aware alert rendering.
func WithFormatter(f i18n.Formatter) Option {
	return formatterOption{f: f}
}

type libraryOption struct {
	l template.Library
}

func (o libraryOption) apply(options *telegramOptions) { options.library = o.l }

// WithLibrary injects an alert template Library for template-based rendering.
func WithLibrary(l template.Library) Option {
	return libraryOption{l: l}
}

type registryOption struct {
	r i18n.Registry
}

func (o registryOption) apply(options *telegramOptions) { options.registry = o.r }

// WithRegistry injects an i18n Registry used by the template renderer.
func WithRegistry(r i18n.Registry) Option {
	return registryOption{r: r}
}

type httpClientOption struct {
	client *http.Client
}

func (o httpClientOption) apply(options *telegramOptions) { options.client = o.client }

// WithHTTPClient injects a custom HTTP client. Useful for tests and for
// tuning transport parameters. When not set, a new client is built from
// the ProxyURL, Timeout, and Region via the httpclient builder.
func WithHTTPClient(client *http.Client) Option {
	return httpClientOption{client: client}
}

// applyDefaults replaces zero or negative Config fields with default
// values. The APIBase defaults to the public Telegram Bot API endpoint,
// and Region defaults to RegionGlobal (Telegram is an international
// endpoint). The Timeout is intentionally left to the httpclient builder
// so a region-aware default is applied when zero.
func applyDefaults(c *Config) {
	if c.APIBase == "" {
		c.APIBase = defaultAPIBase
	}
	if c.Region == "" {
		c.Region = httpclient.RegionGlobal
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
// validated after Options are applied, so Options such as WithBotToken may
// supply a value the Config left empty.
//
// When no custom HTTP client is provided, a client is built from the
// ProxyURL, Timeout, and Region via the httpclient builder (supporting
// proxy routing and region-aware timeouts).
//
// Returns an error if the effective configuration is invalid, the HTTP
// client cannot be constructed, or the retry backoff cannot be
// constructed.
func New(cfg Config, options ...Option) (*Channel, error) {
	opts := &telegramOptions{cfg: cfg}
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
			Region:      opts.cfg.Region,
		})
		if err != nil {
			return nil, fmt.Errorf("telegram: build http client: %w", err)
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
		return nil, fmt.Errorf("telegram: build backoff: %w", err)
	}
	r, err := retry.New(
		retry.WithMaxAttempts(opts.cfg.RetryMaxAttempts),
		retry.WithBackoff(backoff),
		retry.WithRetryable(isRetryableSendErr),
	)
	if err != nil {
		return nil, fmt.Errorf("telegram: build retry: %w", err)
	}

	breaker := circuitbreaker.New(circuitbreaker.Config{
		FailureThreshold: opts.cfg.CircuitFailureThreshold,
		Cooldown:         opts.cfg.CircuitCooldown,
	})

	return &Channel{
		cfg:        opts.cfg,
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
