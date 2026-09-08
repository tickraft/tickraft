// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package wecom implements a WeCom (企业微信) notification channel for the
// tickraft alerting pipeline.
//
// A Channel delivers alert notifications to WeCom in two modes:
//
//   - Robot mode: POSTs text or markdown messages to a robot webhook URL.
//   - App mode: obtains an access_token via the WeCom application API and
//     sends application messages to specified users. The access_token is
//     cached and refreshed proactively before expiry.
//
// Both modes integrate a circuit breaker (to avoid hammering a degraded
// endpoint) and a retry mechanism with exponential backoff and full
// jitter (to tolerate transient failures). 5xx responses and network
// errors are retried; 4xx responses fail fast. WeCom API errcode values
// indicating an expired or invalid access_token trigger a token refresh
// and are retried.
package wecom

import (
	"errors"
	"fmt"
	"net/http"
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

	// defaultBaseURL is the WeCom API base URL used by app mode.
	defaultBaseURL = "https://qyapi.weixin.qq.com"

	// tokenRefreshLeadTime is subtracted from the token's reported
	// expires_in so the channel refreshes proactively before the
	// server-side expiry.
	tokenRefreshLeadTime = 5 * time.Minute
)

// Mode selects between robot (webhook) and app (application message)
// delivery modes.
type Mode string

const (
	// ModeRobot delivers messages via a robot webhook URL.
	ModeRobot Mode = "robot"
	// ModeApp delivers messages via the WeCom application API.
	ModeApp Mode = "app"
)

// MessageType selects between text and markdown message formats.
type MessageType string

const (
	// MessageTypeText formats alerts as plain text.
	MessageTypeText MessageType = "text"
	// MessageTypeMarkdown formats alerts as markdown.
	MessageTypeMarkdown MessageType = "markdown"
)

// Config configures a wecom Channel. Zero-valued numeric fields and
// durations are replaced with sensible defaults by New. MessageType
// defaults to text when empty.
type Config struct {
	// Mode selects the delivery mode. Must be ModeRobot or ModeApp.
	Mode Mode `json:"mode"`
	// RobotWebhookURL is the robot webhook endpoint, required for robot
	// mode. Must use the https scheme.
	RobotWebhookURL string `json:"robot_webhook_url"`
	// CorpID is the WeCom corp ID, required for app mode.
	CorpID string `json:"corp_id"`
	// AgentID is the application agent ID, required for app mode.
	AgentID int `json:"agent_id"`
	// Secret is the application secret, required for app mode.
	Secret string `json:"secret"`
	// ToUser is the message recipient (user ID, or "@all"), required for
	// app mode.
	ToUser string `json:"to_user"`
	// BaseURL overrides the WeCom API base URL for app mode (default
	// https://qyapi.weixin.qq.com). Dedicated-edition deployments point
	// it at their private WeCom API endpoint. Ignored in robot mode.
	BaseURL string `json:"base_url,omitempty"`
	// CallbackToken and EncodingAESKey carry the application's callback
	// credentials configured in the WeCom admin console. The outbound
	// channel itself does not use them: they are stored here so the
	// deployment's inbound callback endpoint (which receives and decrypts
	// the card events triggered from application messages) can resolve
	// them from the same channel configuration. Both are optional.
	CallbackToken  string `json:"callback_token,omitempty"`
	EncodingAESKey string `json:"encoding_aes_key,omitempty"`
	// MessageType is the message format. Defaults to text when empty.
	MessageType MessageType `json:"message_type"`
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
	// Interaction carries the interactive-card policy (M5 L1 inbound
	// events). When enabled and the channel runs in app mode, alerts are
	// sent as button_interaction template cards whose button keys carry
	// the action verb and the alert's EventID. Set by the build layer;
	// never stored.
	Interaction format.InteractionOptions `json:"-"`
	// Name is the display name of the channel configuration, carried
	// into delivery records. Not part of the stored user config JSON.
	Name string `json:"-"`
}

// Validate checks that the Config is usable for the configured Mode. Robot
// mode requires a non-empty https RobotWebhookURL; app mode requires
// CorpID, a non-zero AgentID, Secret and ToUser.
func (c Config) Validate() error {
	switch c.Mode {
	case ModeRobot:
		if c.RobotWebhookURL == "" {
			return errors.New("wecom: robot_webhook_url is required for robot mode")
		}
		if !isHTTPSURL(c.RobotWebhookURL) {
			return fmt.Errorf("wecom: robot_webhook_url must use https scheme, got %q", c.RobotWebhookURL)
		}
	case ModeApp:
		if c.CorpID == "" {
			return errors.New("wecom: corp_id is required for app mode")
		}
		if c.AgentID == 0 {
			return errors.New("wecom: agent_id is required for app mode")
		}
		if c.Secret == "" {
			return errors.New("wecom: secret is required for app mode")
		}
		if c.ToUser == "" {
			return errors.New("wecom: to_user is required for app mode")
		}
	default:
		return fmt.Errorf("wecom: invalid mode %q, must be %q or %q", c.Mode, ModeRobot, ModeApp)
	}
	if c.BaseURL != "" && !isHTTPURL(c.BaseURL) {
		return fmt.Errorf("wecom: base url must use http or https scheme, got %q", c.BaseURL)
	}
	if err := httpclient.ValidateProxyURL(c.ProxyURL); err != nil {
		return fmt.Errorf("wecom: %w", err)
	}
	return nil
}

// isHTTPSURL reports whether s begins with https:// (case-insensitive).
func isHTTPSURL(s string) bool {
	return strings.HasPrefix(strings.ToLower(s), "https://")
}

// isHTTPURL reports whether s begins with http:// or https://
// (case-insensitive).
func isHTTPURL(s string) bool {
	low := strings.ToLower(s)
	return strings.HasPrefix(low, "http://") || strings.HasPrefix(low, "https://")
}

// Option configures a Channel at construction time. Options are applied
// after the Config and may override Config fields or inject a custom HTTP
// client and logger.
type Option interface {
	apply(*wecomOptions)
}

// wecomOptions is the internal builder that merges a Config with Option
// overrides.
type wecomOptions struct {
	cfg       Config
	client    *http.Client
	logger    *zap.Logger
	baseURL   string
	formatter i18n.Formatter
	library   template.Library
	registry  i18n.Registry
}

type robotModeOption struct{}

func (o robotModeOption) apply(options *wecomOptions) { options.cfg.Mode = ModeRobot }

// WithRobotMode sets the delivery mode to robot (webhook).
func WithRobotMode() Option {
	return robotModeOption{}
}

type appModeOption struct{}

func (o appModeOption) apply(options *wecomOptions) { options.cfg.Mode = ModeApp }

// WithAppMode sets the delivery mode to app (application message).
func WithAppMode() Option {
	return appModeOption{}
}

type robotWebhookURLOption string

func (o robotWebhookURLOption) apply(options *wecomOptions) { options.cfg.RobotWebhookURL = string(o) }

// WithRobotWebhookURL overrides the robot webhook endpoint URL.
func WithRobotWebhookURL(url string) Option {
	return robotWebhookURLOption(url)
}

type corpIDOption string

func (o corpIDOption) apply(options *wecomOptions) { options.cfg.CorpID = string(o) }

// WithCorpID overrides the WeCom corp ID (app mode).
func WithCorpID(corpID string) Option {
	return corpIDOption(corpID)
}

type agentIDOption int

func (o agentIDOption) apply(options *wecomOptions) { options.cfg.AgentID = int(o) }

// WithAgentID overrides the application agent ID (app mode).
func WithAgentID(agentID int) Option {
	return agentIDOption(agentID)
}

type secretOption string

func (o secretOption) apply(options *wecomOptions) { options.cfg.Secret = string(o) }

// WithSecret overrides the application secret (app mode).
func WithSecret(secret string) Option {
	return secretOption(secret)
}

type toUserOption string

func (o toUserOption) apply(options *wecomOptions) { options.cfg.ToUser = string(o) }

// WithToUser overrides the message recipient (app mode).
func WithToUser(toUser string) Option {
	return toUserOption(toUser)
}

type interactionOption struct{ interaction format.InteractionOptions }

func (o interactionOption) apply(options *wecomOptions) {
	options.cfg.Interaction = o.interaction
}

// WithInteraction injects the interactive-card policy. When enabled and
// the channel runs in app mode, alerts are sent as button_interaction
// template cards instead of text/markdown.
func WithInteraction(interaction format.InteractionOptions) Option {
	return interactionOption{interaction: interaction}
}

type messageTypeOption struct {
	mt MessageType
}

func (o messageTypeOption) apply(options *wecomOptions) { options.cfg.MessageType = o.mt }

// WithMessageType overrides the message format (text or markdown).
func WithMessageType(mt MessageType) Option {
	return messageTypeOption{mt: mt}
}

type retryOption struct {
	maxAttempts  int
	baseInterval time.Duration
}

func (o retryOption) apply(options *wecomOptions) {
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

func (o circuitBreakerOption) apply(options *wecomOptions) {
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

func (o loggerOption) apply(options *wecomOptions) { options.logger = o.logger }

// WithLogger sets the structured logger. When not set, a no-op logger is
// used.
func WithLogger(logger *zap.Logger) Option {
	return loggerOption{logger: logger}
}

type formatterOption struct {
	f i18n.Formatter
}

func (o formatterOption) apply(options *wecomOptions) { options.formatter = o.f }

// WithFormatter injects an i18n Formatter for locale-aware alert rendering.
// Should be injected at startup; when nil, Render returns a zero-value
// Message and logs an error.
func WithFormatter(f i18n.Formatter) Option {
	return formatterOption{f: f}
}

type libraryOption struct {
	l template.Library
}

func (o libraryOption) apply(options *wecomOptions) { options.library = o.l }

// WithLibrary injects an alert template Library for template-based rendering.
// When non-nil and alert.TemplateID is non-empty, the channel renders via
// Library.Render instead of Formatter.Format.
func WithLibrary(l template.Library) Option {
	return libraryOption{l: l}
}

type registryOption struct {
	r i18n.Registry
}

func (o registryOption) apply(options *wecomOptions) { options.registry = o.r }

// WithRegistry injects an i18n Registry used by the template renderer to
// resolve level labels, field labels, and time formats. When nil the
// template renderer falls back to English defaults.
func WithRegistry(r i18n.Registry) Option {
	return registryOption{r: r}
}

type httpClientOption struct {
	client *http.Client
}

func (o httpClientOption) apply(options *wecomOptions) { options.client = o.client }

// WithHTTPClient injects a custom HTTP client. Useful for tests and for
// tuning transport parameters. When not set, a new client with the
// configured timeout is created.
func WithHTTPClient(client *http.Client) Option {
	return httpClientOption{client: client}
}

type baseURLOption string

func (o baseURLOption) apply(options *wecomOptions) { options.baseURL = string(o) }

// withBaseURL overrides the WeCom API base URL used by app mode. It is
// intended primarily for tests that point the channel at an httptest
// server.
func withBaseURL(baseURL string) Option {
	return baseURLOption(baseURL)
}

// applyDefaults replaces zero or negative Config fields with default
// values and applies the default message type.
func applyDefaults(c *Config) {
	// A config that carries only a robot webhook (the shape stored by the
	// channel management UI) implicitly selects robot mode.
	if c.Mode == "" && c.RobotWebhookURL != "" {
		c.Mode = ModeRobot
	}
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

// New creates a Channel from the given Config and Options. Defaults are
// applied after Options so option-supplied values participate in the
// defaulting (for example an option-provided robot webhook selects robot
// mode), and validation runs on the defaulted config.
//
// Returns an error if the effective configuration is invalid or the
// retry backoff cannot be constructed.
func New(cfg Config, options ...Option) (*Channel, error) {
	opts := &wecomOptions{cfg: cfg}
	for _, o := range options {
		o.apply(opts)
	}
	applyDefaults(&opts.cfg)
	if err := opts.cfg.Validate(); err != nil {
		return nil, err
	}

	client := opts.client
	if client == nil {
		c, err := httpclient.New(httpclient.Config{
			ProxyURL:    opts.cfg.ProxyURL,
			ProxyBypass: opts.cfg.ProxyBypass,
			Region:      httpclient.RegionCN,
		})
		if err != nil {
			return nil, fmt.Errorf("wecom: build http client: %w", err)
		}
		client = c
	}

	logger := opts.logger
	if logger == nil {
		logger = zap.NewNop()
	}

	// An explicit option (test injection) wins over the stored config;
	// an empty value falls back to the official endpoint.
	baseURL := opts.baseURL
	if baseURL == "" {
		baseURL = opts.cfg.BaseURL
	}
	if baseURL == "" {
		baseURL = defaultBaseURL
	}

	base := opts.cfg.RetryBaseInterval
	maxBackoff := max(retryMaxBackoff, base)
	backoff, err := retry.NewExponential(
		base,
		maxBackoff,
		retry.WithJitter(retry.NewFullJitter()),
	)
	if err != nil {
		return nil, fmt.Errorf("wecom: build backoff: %w", err)
	}
	r, err := retry.New(
		retry.WithMaxAttempts(opts.cfg.RetryMaxAttempts),
		retry.WithBackoff(backoff),
		retry.WithRetryable(isRetryable),
	)
	if err != nil {
		return nil, fmt.Errorf("wecom: build retry: %w", err)
	}

	breaker := circuitbreaker.New(circuitbreaker.Config{
		FailureThreshold: opts.cfg.CircuitFailureThreshold,
		Cooldown:         opts.cfg.CircuitCooldown,
	})

	return &Channel{
		cfg:        opts.cfg,
		baseURL:    baseURL,
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
