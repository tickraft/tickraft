// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package email

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/circuitbreaker"
	"github.com/tickraft/tickraft/pkg/i18n"
	"github.com/tickraft/tickraft/pkg/prism/alert/template"
	"github.com/tickraft/tickraft/pkg/retry"
)

// Default configuration values applied when the corresponding Config field is
// zero or negative.
const (
	defaultPort             = 25
	defaultTimeout          = 30 * time.Second
	defaultRetryMaxAttempts = 3
	defaultRetryBase        = 1 * time.Second
	defaultCircuitThreshold = 5
	defaultCircuitCooldown  = 30 * time.Second
	retryMaxBackoff         = 30 * time.Second
	heloDomain              = "localhost"
)

// SASL mechanism names advertised by the supported AuthType values.
const (
	authPlain = "plain"
	authLogin = "LOGIN"
)

// TLSMode controls the TLS strategy used when connecting to the SMTP server.
type TLSMode int

const (
	// TLSModeNone sends mail over a plaintext connection. This mode should
	// only be used for local testing as credentials are transmitted in the
	// clear.
	TLSModeNone TLSMode = iota
	// TLSModeImplicit establishes a TLS tunnel from the start of the
	// connection (typically on port 465).
	TLSModeImplicit
	// TLSModeStartTLS connects in plaintext and upgrades to TLS via the
	// STARTTLS extension (typically on port 587).
	TLSModeStartTLS
)

// String returns a human-readable description of the TLS mode.
func (m TLSMode) String() string {
	switch m {
	case TLSModeNone:
		return "none"
	case TLSModeImplicit:
		return "implicit"
	case TLSModeStartTLS:
		return "starttls"
	default:
		return fmt.Sprintf("unknown(%d)", int(m))
	}
}

// valid reports whether m is one of the defined TLSMode constants.
func (m TLSMode) valid() bool {
	switch m {
	case TLSModeNone, TLSModeImplicit, TLSModeStartTLS:
		return true
	default:
		return false
	}
}

// AuthType selects the SMTP authentication mechanism.
type AuthType int

const (
	// AuthTypePlain uses the PLAIN SASL mechanism.
	AuthTypePlain AuthType = iota
	// AuthTypeLogin uses the LOGIN SASL mechanism (non-standard but widely
	// supported).
	AuthTypeLogin
	// AuthTypeCramMD5 uses the CRAM-MD5 challenge-response mechanism.
	AuthTypeCramMD5
)

// String returns a human-readable description of the auth type.
func (a AuthType) String() string {
	switch a {
	case AuthTypePlain:
		return authPlain
	case AuthTypeLogin:
		return "login"
	case AuthTypeCramMD5:
		return "cram-md5"
	default:
		return fmt.Sprintf("unknown(%d)", int(a))
	}
}

// valid reports whether a is one of the defined AuthType constants.
func (a AuthType) valid() bool {
	switch a {
	case AuthTypePlain, AuthTypeLogin, AuthTypeCramMD5:
		return true
	default:
		return false
	}
}

// Config configures an email Channel. Zero-valued numeric fields and
// durations are replaced with sensible defaults by New.
type Config struct {
	// Host is the SMTP server hostname or IP address.
	Host string
	// Port is the SMTP server port. Defaults to 25 when zero. Use 465 for
	// implicit TLS and 587 for STARTTLS.
	Port int
	// Timeout is the maximum duration for a complete send operation,
	// including dial, TLS handshake, authentication, and message
	// transmission. Defaults to 30s when zero or negative. Per-attempt
	// deadlines are derived from the remaining time budget so retries
	// stay within the overall timeout.
	Timeout time.Duration
	// Username is the authentication username. When empty, authentication
	// is skipped.
	Username string
	// Password is the authentication password.
	Password string
	// From is the sender email address.
	From string
	// To is the list of recipient email addresses.
	To []string
	// TLSMode controls the TLS strategy. Defaults to TLSModeNone (no TLS).
	TLSMode TLSMode
	// AuthType selects the authentication mechanism. Ignored when Username
	// is empty.
	AuthType AuthType
	// HTMLMode controls whether messages are sent as multipart/alternative
	// (plain text + HTML) instead of plain text only.
	HTMLMode bool
	// RetryMaxAttempts is the maximum number of send attempts including
	// the first. Defaults to 3 when zero or negative.
	RetryMaxAttempts int
	// RetryBaseInterval is the base interval for exponential backoff
	// between retries. Defaults to 1s when zero or negative.
	RetryBaseInterval time.Duration
	// CircuitFailureThreshold is the number of consecutive send failures
	// that opens the circuit breaker. Defaults to 5 when zero or negative.
	CircuitFailureThreshold int
	// CircuitCooldown is how long the circuit breaker stays open before
	// transitioning to half-open. Defaults to 30s when zero or negative.
	CircuitCooldown time.Duration
	// Formatter renders alert events into localized messages. When nil,
	// buildMessage falls back to the default Formatter from
	// pkg/prism/channel/format, which uses the built-in i18n asset bundle.
	// Inject a custom Formatter when the deployment needs locale-aware
	// rendering backed by a merged Registry (built-in + extended
	// asset files).
	Formatter i18n.Formatter
	// Library is the alert template library used for template-based
	// rendering. When non-nil and alert.TemplateID is non-empty,
	// buildMessage calls Library.Render instead of Formatter.Format,
	// producing output from the named template (built-in or custom).
	Library template.Library
}

// Validate checks that the Config is usable. It verifies that Host is
// non-empty, Port is in the valid range, From and all To addresses are valid
// email addresses, and that authentication credentials are consistent with
// the configured AuthType.
func (c Config) Validate() error {
	if strings.TrimSpace(c.Host) == "" {
		return errors.New("email: host is required")
	}
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("email: port must be between 1 and 65535, got %d", c.Port)
	}
	if !c.TLSMode.valid() {
		return fmt.Errorf("email: invalid TLS mode %d", c.TLSMode)
	}
	if !c.AuthType.valid() {
		return fmt.Errorf("email: invalid auth type %d", c.AuthType)
	}
	if c.From == "" {
		return errors.New("email: from address is required")
	}
	if _, err := mail.ParseAddress(c.From); err != nil {
		return fmt.Errorf("email: invalid from address %q: %w", c.From, err)
	}
	if len(c.To) == 0 {
		return errors.New("email: at least one recipient is required")
	}
	for i, addr := range c.To {
		if _, err := mail.ParseAddress(addr); err != nil {
			return fmt.Errorf("email: invalid to address at index %d %q: %w", i, addr, err)
		}
	}
	// Auth consistency: when username is empty, no authentication is
	// performed and password should also be empty. When username is set,
	// password must also be set.
	if c.Username == "" && c.Password != "" {
		return errors.New("email: password set without username")
	}
	if c.Username != "" && c.Password == "" {
		return errors.New("email: username set without password")
	}
	return nil
}

// Option configures a Channel at construction time. Options are applied
// after the Config and may override Config fields or inject a custom logger
// and TLS configuration.
type Option interface {
	apply(*emailOptions)
}

// emailOptions is the internal builder that merges a Config with Option overrides.
type emailOptions struct {
	cfg       Config
	logger    *zap.Logger
	tls       *tls.Config
	formatter i18n.Formatter
	library   template.Library
}

// hostOption overrides the SMTP server host.
type hostOption string

func (o hostOption) apply(options *emailOptions) { options.cfg.Host = string(o) }

// WithHost overrides the SMTP server host.
func WithHost(host string) Option {
	return hostOption(host)
}

// portOption overrides the SMTP server port.
type portOption int

func (o portOption) apply(options *emailOptions) { options.cfg.Port = int(o) }

// WithPort overrides the SMTP server port.
func WithPort(port int) Option {
	return portOption(port)
}

// timeoutOption overrides the maximum duration for a complete send operation.
type timeoutOption time.Duration

func (o timeoutOption) apply(options *emailOptions) { options.cfg.Timeout = time.Duration(o) }

// WithTimeout overrides the maximum duration for a complete send
// operation, including dial, TLS handshake, authentication, and message
// transmission.
func WithTimeout(timeout time.Duration) Option {
	return timeoutOption(timeout)
}

// credentialsOption sets the authentication username and password.
type credentialsOption struct {
	username string
	password string
}

func (o credentialsOption) apply(options *emailOptions) {
	options.cfg.Username = o.username
	options.cfg.Password = o.password
}

// WithCredentials sets the authentication username and password.
func WithCredentials(username, password string) Option {
	return credentialsOption{username: username, password: password}
}

// fromOption overrides the sender email address.
type fromOption string

func (o fromOption) apply(options *emailOptions) { options.cfg.From = string(o) }

// WithFrom overrides the sender email address.
func WithFrom(from string) Option {
	return fromOption(from)
}

// toOption overrides the recipient list.
type toOption struct {
	to []string
}

func (o toOption) apply(options *emailOptions) { options.cfg.To = o.to }

// WithTo overrides the recipient list.
func WithTo(to ...string) Option {
	return toOption{to: to}
}

// tlsModeOption overrides the TLS mode.
type tlsModeOption TLSMode

func (o tlsModeOption) apply(options *emailOptions) { options.cfg.TLSMode = TLSMode(o) }

// WithTLSMode overrides the TLS mode.
func WithTLSMode(mode TLSMode) Option {
	return tlsModeOption(mode)
}

// authTypeOption overrides the authentication mechanism.
type authTypeOption AuthType

func (o authTypeOption) apply(options *emailOptions) { options.cfg.AuthType = AuthType(o) }

// WithAuthType overrides the authentication mechanism.
func WithAuthType(authType AuthType) Option {
	return authTypeOption(authType)
}

// htmlModeOption enables or disables HTML email mode.
type htmlModeOption bool

func (o htmlModeOption) apply(options *emailOptions) { options.cfg.HTMLMode = bool(o) }

// WithHTMLMode enables or disables HTML email mode.
func WithHTMLMode(enabled bool) Option {
	return htmlModeOption(enabled)
}

// retryOption overrides the retry configuration.
type retryOption struct {
	maxAttempts  int
	baseInterval time.Duration
}

func (o retryOption) apply(options *emailOptions) {
	options.cfg.RetryMaxAttempts = o.maxAttempts
	options.cfg.RetryBaseInterval = o.baseInterval
}

// WithRetry overrides the retry configuration: maxAttempts is the total
// number of attempts (including the first) and baseInterval is the base for
// exponential backoff.
func WithRetry(maxAttempts int, baseInterval time.Duration) Option {
	return retryOption{maxAttempts: maxAttempts, baseInterval: baseInterval}
}

// circuitBreakerOption overrides the circuit breaker configuration.
type circuitBreakerOption struct {
	failureThreshold int
	cooldown         time.Duration
}

func (o circuitBreakerOption) apply(options *emailOptions) {
	options.cfg.CircuitFailureThreshold = o.failureThreshold
	options.cfg.CircuitCooldown = o.cooldown
}

// WithCircuitBreaker overrides the circuit breaker configuration:
// failureThreshold is the consecutive failure count that opens the breaker
// and cooldown is how long it stays open.
func WithCircuitBreaker(failureThreshold int, cooldown time.Duration) Option {
	return circuitBreakerOption{failureThreshold: failureThreshold, cooldown: cooldown}
}

// loggerOption sets the structured logger.
type loggerOption struct {
	logger *zap.Logger
}

func (o loggerOption) apply(options *emailOptions) { options.logger = o.logger }

// WithLogger sets the structured logger. When not set, a no-op logger is
// used.
func WithLogger(logger *zap.Logger) Option {
	return loggerOption{logger: logger}
}

// tlsConfigOption injects a custom TLS configuration.
type tlsConfigOption struct {
	cfg *tls.Config
}

func (o tlsConfigOption) apply(options *emailOptions) { options.tls = o.cfg }

// withTLSConfig is an unexported option for injecting a custom TLS
// configuration, primarily for testing with self-signed certificates.
func withTLSConfig(config *tls.Config) Option {
	return tlsConfigOption{cfg: config}
}

// formatterOption injects a locale-aware Formatter used to render alert messages.
type formatterOption struct {
	f i18n.Formatter
}

func (o formatterOption) apply(options *emailOptions) { options.formatter = o.f }

// WithFormatter injects a locale-aware Formatter used to render alert
// messages. When not set, buildMessage uses the default Formatter from
// pkg/prism/channel/format backed by the built-in i18n asset bundle.
func WithFormatter(f i18n.Formatter) Option {
	return formatterOption{f: f}
}

// libraryOption injects a template Library used for template-based rendering.
type libraryOption struct {
	l template.Library
}

func (o libraryOption) apply(options *emailOptions) { options.library = o.l }

// WithLibrary injects a template Library used for template-based rendering.
// When set and alert.TemplateID is non-empty, buildMessage calls
// Library.Render instead of Formatter.Format.
func WithLibrary(l template.Library) Option {
	return libraryOption{l: l}
}

// applyDefaults replaces zero or negative Config fields with default values.
func applyDefaults(c *Config) {
	if c.Port == 0 {
		c.Port = defaultPort
	}
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
// validated after Options are applied, so Options such as WithHost may supply
// a value the Config left empty.
//
// Returns an error if the effective configuration is invalid or the retry
// backoff cannot be constructed.
func New(cfg Config, options ...Option) (*Channel, error) {
	opts := &emailOptions{cfg: cfg}
	for _, o := range options {
		o.apply(opts)
	}
	applyDefaults(&opts.cfg)

	logger := opts.logger
	if logger == nil {
		logger = zap.NewNop()
	}

	// Propagate injected Formatter and Library from options into the Config
	// so that buildMessage can access them without a separate Channel field.
	if opts.formatter != nil {
		opts.cfg.Formatter = opts.formatter
	}
	if opts.library != nil {
		opts.cfg.Library = opts.library
	}
	// When no Formatter is injected, construct a default one backed by the
	// built-in i18n bundle. This avoids re-loading the bundle on every Send
	// call and keeps renderAlert on the hot path allocation-free.
	if opts.cfg.Formatter == nil {
		opts.cfg.Formatter = buildDefaultFormatter(logger)
	}
	if err := opts.cfg.Validate(); err != nil {
		return nil, err
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
		return nil, fmt.Errorf("email: build backoff: %w", err)
	}
	r, err := retry.New(
		retry.WithMaxAttempts(opts.cfg.RetryMaxAttempts),
		retry.WithBackoff(backoff),
		retry.WithRetryable(isRetryableErr),
	)
	if err != nil {
		return nil, fmt.Errorf("email: build retry: %w", err)
	}

	breaker := circuitbreaker.New(circuitbreaker.Config{
		FailureThreshold: opts.cfg.CircuitFailureThreshold,
		Cooldown:         opts.cfg.CircuitCooldown,
	})

	// Copy the recipient slice to prevent external mutation.
	recipients := make([]string, len(opts.cfg.To))
	copy(recipients, opts.cfg.To)
	opts.cfg.To = recipients

	return &Channel{
		config: opts.cfg,
		cb:     breaker,
		retry:  r,
		logger: logger,
		tls:    opts.tls,
	}, nil
}
