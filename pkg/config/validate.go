// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package config

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/user"
)

// Supported log levels.
var supportedLogLevels = []string{"debug", "info", "warn", "error"}

// Supported logging modes.
var supportedLogModes = []string{"debug", "release"}

// Validate checks the Config for required fields, valid enum values, parseable
// durations, valid listen addresses, and non-negative pool sizes. It returns a
// descriptive error for the first problem found, or nil if the configuration
// is valid.
//
// Duration fields are parsed during YAML unmarshaling via the Duration type,
// so an unparseable duration is reported as a load error before Validate runs.
// Validate additionally checks that semantically required durations are
// positive.
func (c *Config) Validate() error {
	if err := c.Database.validate(); err != nil {
		return err
	}
	if err := c.Auth.validate(); err != nil {
		return err
	}
	if err := c.Logger.validate(); err != nil {
		return err
	}
	if err := c.Server.validate(); err != nil {
		return err
	}
	if err := c.Worker.validate(); err != nil {
		return err
	}
	if err := c.Prism.validate(); err != nil {
		return err
	}
	if err := c.I18n.validate(); err != nil {
		return err
	}
	return nil
}

func (c *DatabaseConfig) validate() error {
	// When DSN is non-empty, it takes precedence; the embedded db.Config
	// fields are ignored. No further validation is needed here because
	// db.Parse will reject invalid DSNs at startup.
	if c.DSN != "" {
		return nil
	}
	// DSN is empty — validate the direct db.Config fields.
	if c.Driver == "" {
		return fmt.Errorf("config: database.dsn or database.driver is required")
	}
	if c.Addr == "" {
		return fmt.Errorf("config: database.addr is required when dsn is empty")
	}
	if strings.Contains(c.Addr, ":memory:") {
		return fmt.Errorf("config: memory databases are not supported")
	}
	return nil
}

func (c *AuthConfig) validate() error {
	if c.JWTSecret == "" {
		return fmt.Errorf("config: auth.jwt_secret is required (set it directly or use ${TICKRAFT_JWT_SECRET} env var interpolation in your config file)")
	}
	if len(c.JWTSecret) < 32 {
		return fmt.Errorf("config: auth.jwt_secret must be at least 32 bytes, got %d", len(c.JWTSecret))
	}
	if c.TokenTTL.IsZero() {
		return fmt.Errorf("config: auth.token_ttl must be greater than zero")
	}
	// Validate the admin username with the canonical rule shared with
	// pkg/user.ValidateUsername and pkg/db.EnsureAdminUser. Failing fast at
	// config load avoids the "initialized but cannot log in" bug where a
	// custom admin_username passes EnsureAdminUser but is rejected by the
	// login validator (e.g. hyphens, dots, or length < 3).
	if err := user.ValidateUsername(c.AdminUsername); err != nil {
		return fmt.Errorf("config: invalid auth.admin_username %q: %w", c.AdminUsername, err)
	}
	return nil
}

func (c *LoggerConfig) validate() error {
	if err := validateEnum("logger.level", c.Level, supportedLogLevels); err != nil {
		return err
	}
	if err := validateEnum("logger.mode", c.Mode, supportedLogModes); err != nil {
		return err
	}
	// RetentionDays is optional: a non-positive value is normalized to the
	// default instead of failing validation, applying the default value. A
	// warn-level log surfaces the fallback so operators can pin an explicit
	// value.
	if c.RetentionDays <= 0 {
		zap.L().Warn("logger.retention_days is not set or non-positive; using default 30 days",
			zap.Int("got", c.RetentionDays),
		)
		c.RetentionDays = 30
	}
	return nil
}

func (c *ServerConfig) validate() error {
	if err := validateListenAddr("server.addr", c.Addr); err != nil {
		return err
	}
	if c.MaxHeaderBytes < 0 {
		return fmt.Errorf("config: server.max_header_bytes must be >= 0, got %d", c.MaxHeaderBytes)
	}
	// TLS validation: when TLS is enabled without ACME, both a certificate
	// and a key file are required. The deeper crypto-level validation
	// (file parses, key matches cert, min version is accepted) is deferred
	// to pkg/api.ServerConfig.Validate and pkg/api.buildTLSConfig so the
	// config layer does not depend on crypto/tls.
	if c.TLSEnabled && !c.ACME.Enabled {
		if c.TLSCertFile == "" || c.TLSKeyFile == "" {
			return fmt.Errorf("config: server.tls_cert_file and server.tls_key_file are required when tls_enabled is true and acme.enabled is false")
		}
	}
	if c.ACME.Enabled {
		if c.ACME.Email == "" {
			return fmt.Errorf("config: server.acme.email is required when acme.enabled is true")
		}
		// DirectoryURL is intentionally not validated for non-emptiness here:
		// when left empty it defaults to the Let's Encrypt production endpoint
		// (pkg/api.DefaultACMEDirectoryURL) via pkg/api.ServerConfig.SetDefaults
		// during API server construction.
	}
	return nil
}

func (c *WorkerConfig) validate() error {
	if c.Concurrence < 0 {
		return fmt.Errorf("config: worker.concurrence must be >= 0, got %d", c.Concurrence)
	}
	return nil
}

func (c *PrismConfig) validate() error {
	if c.EvalInterval.IsZero() {
		return fmt.Errorf("config: prism.eval_interval must be greater than zero")
	}
	if c.Concurrence < 0 {
		return fmt.Errorf("config: prism.concurrence must be >= 0, got %d", c.Concurrence)
	}
	return nil
}

// validate ensures I18nConfig holds a valid default locale and that the
// default locale is included in supported_locales (when supported_locales is
// non-empty). Locale tags are validated for BCP 47 shape (non-empty, ASCII
// letters/digits/hyphens only). The canonical default locale "zh-Hans" is
// always accepted; callers may register additional locales.
func (c *I18nConfig) validate() error {
	if c.DefaultLocale == "" {
		return fmt.Errorf("config: i18n.default_locale is required")
	}
	if !isValidLocaleTag(c.DefaultLocale) {
		return fmt.Errorf("config: invalid i18n.default_locale %q", c.DefaultLocale)
	}
	// When supported_locales is empty, validation is skipped: SetDefaults
	// populates the builtin set, but a YAML file that explicitly sets an
	// empty list is treated as "use builtin" to avoid surprising operators.
	for _, loc := range c.SupportedLocales {
		if !isValidLocaleTag(loc) {
			return fmt.Errorf("config: invalid i18n.supported_locales entry %q", loc)
		}
	}
	// The default locale must be reachable from supported_locales so the
	// frontend language switcher always offers the fallback. Language-only
	// matches are accepted (e.g. default "zh-Hans" is covered by "zh") to
	// align with the Registry fallback chain.
	if len(c.SupportedLocales) > 0 {
		if !localeListContains(c.SupportedLocales, c.DefaultLocale) {
			return fmt.Errorf("config: i18n.default_locale %q must be listed in i18n.supported_locales", c.DefaultLocale)
		}
	}
	return nil
}

// isValidLocaleTag reports whether s is a syntactically plausible BCP 47 tag:
// non-empty, ASCII letters, digits, and hyphens only. Full BCP 47 validation
// is deferred to pkg/i18n.Parse at runtime; this guard rejects obviously
// malformed values early during config validation.
func isValidLocaleTag(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-':
		default:
			return false
		}
	}
	return true
}

// localeListContains reports whether list contains tag, accepting
// language-only matches (e.g. "zh" covers "zh-Hans") to mirror the Registry
// fallback chain semantics.
func localeListContains(list []string, tag string) bool {
	lang := strings.ToLower(strings.SplitN(tag, "-", 2)[0])
	for _, entry := range list {
		if entry == tag {
			return true
		}
		entryLang := strings.ToLower(strings.SplitN(entry, "-", 2)[0])
		if entryLang == lang {
			return true
		}
	}
	return false
}

// validateEnum reports whether value is one of the allowed strings. The field
// path is used to build a descriptive error message.
func validateEnum(field, value string, allowed []string) error {
	for _, a := range allowed {
		if value == a {
			return nil
		}
	}
	return fmt.Errorf("config: invalid %s %q, must be one of: %s", field, value, strings.Join(allowed, ", "))
}

// validateListenAddr validates that addr is a valid "host:port" listen address
// with a numeric port in the range 1-65535. The field path is used to build a
// descriptive error message.
func validateListenAddr(field, addr string) error {
	if addr == "" {
		return fmt.Errorf("config: %s is required", field)
	}
	_, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("config: invalid %s %q: %w", field, addr, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return fmt.Errorf("config: invalid port %q in %s %q: %w", portStr, field, addr, err)
	}
	if port < 1 || port > 65535 {
		return fmt.Errorf("config: port %d out of range (1-65535) in %s %q", port, field, addr)
	}
	return nil
}
