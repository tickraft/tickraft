// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package config

import (
	"time"

	"github.com/tickraft/tickraft/pkg/db"
)

// Config is the top-level configuration for the tickraft single-process deployment.
// The runtime runs the API server, worker engines, and prism together in one process.
// Config is loaded from a YAML file viaLoad or LoadFromBytes and validated via Validate.
//
// SetDefaults should be called on a zero-value Config before unmarshaling so that fields
// absent from the YAML file retain their default values. Fields present in the YAML file
// overwrite the defaults during unmarshaling.
type Config struct {
	// Server configures the HTTP API server.
	Server ServerConfig `yaml:"server" json:"server"`
	// Worker configures the worker process runtime parameters.
	Worker WorkerConfig `yaml:"worker" json:"worker"`
	// Prism configures the alert evaluation engine.
	Prism PrismConfig `yaml:"prism" json:"prism"`
	// Database configures the backend database connection.
	Database DatabaseConfig `yaml:"database" json:"database"`
	// Auth configures JWT token signing and lifetime.
	Auth AuthConfig `yaml:"auth" json:"auth"`
	// Logger configures the logger level and mode.
	Logger LoggerConfig `yaml:"logger" json:"logger"`
	// I18n configures the internationalization settings.
	I18n I18nConfig `yaml:"i18n" json:"i18n"`
}

// ServerConfig configures the HTTP API server.
type ServerConfig struct {
	// Addr is the HTTP listen address for the REST API, SPA, and health
	// probes (default ":6153"). The runtime runs in single-port
	// mode: the API, frontend assets, and webhook listener are all served
	// from this one address.
	Addr string `yaml:"addr" json:"addr"`
	// EnableCORS enables CORS middleware (default true).
	EnableCORS bool `yaml:"enable_cors" json:"enable_cors"`
	// EnableAccessLog enables access log middleware (default true).
	EnableAccessLog bool `yaml:"enable_access_log" json:"enable_access_log"`
	// MaxHeaderBytes is the maximum size of request headers (default 1048576).
	MaxHeaderBytes int `yaml:"max_header_bytes" json:"max_header_bytes"`
	// ReadTimeout is the maximum duration for reading the entire request.
	// A zero value means no timeout.
	ReadTimeout Duration `yaml:"read_timeout" json:"read_timeout"`
	// WriteTimeout is the maximum duration before timing out writes of the
	// response. A zero value means no timeout.
	WriteTimeout Duration `yaml:"write_timeout" json:"write_timeout"`
	// MaintenanceInterval is the interval between background maintenance
	// sweeps such as cleaning expired token blacklist entries
	// (default "5m").
	MaintenanceInterval Duration `yaml:"maintenance_interval" json:"maintenance_interval"`

	// TLSEnabled toggles TLS termination for the HTTP server. When true the
	// server serves HTTPS on Addr; when false plain HTTP is served. The
	// schema mirrors pkg/api.ServerConfig so the start command can forward
	// these fields without translation.
	TLSEnabled bool `yaml:"tls_enabled" json:"tls_enabled"`
	// TLSCertFile is the PEM-encoded server certificate path. Required when
	// TLSEnabled is true and ACME is disabled.
	TLSCertFile string `yaml:"tls_cert_file" json:"tls_cert_file"`
	// TLSKeyFile is the PEM-encoded server private key path. Required when
	// TLSEnabled is true and ACME is disabled.
	TLSKeyFile string `yaml:"tls_key_file" json:"tls_key_file"`
	// TLSMinVersion is the minimum TLS version: "1.2" or "1.3". Defaults to
	// "1.2" when empty (see pkg/api.DefaultTLSMinVersion).
	TLSMinVersion string `yaml:"tls_min_version" json:"tls_min_version"`
	// TLSCipherSuites is the cipher-suite whitelist. When empty the server
	// applies pkg/api.DefaultTLSCipherSuites.
	TLSCipherSuites []string `yaml:"tls_cipher_suites" json:"tls_cipher_suites"`
	// TLSClientCAFile is the PEM-encoded client CA certificate path used for
	// mutual TLS. Empty means no client certificate verification.
	TLSClientCAFile string `yaml:"tls_client_ca_file" json:"tls_client_ca_file"`
	// TLSClientAuth is the client authentication mode (see
	// pkg/api.ServerConfig.TLSClientAuth for the accepted values).
	TLSClientAuth string `yaml:"tls_client_auth" json:"tls_client_auth"`

	// ACME configures automatic certificate issuance via the ACME protocol.
	// When ACME.Enabled is true the server obtains certificates from an ACME
	// directory (e.g. Let's Encrypt) instead of relying on statically
	// configured TLSCertFile/TLSKeyFile.
	ACME ACMEConfig `yaml:"acme" json:"acme"`
}

// ACMEConfig configures automatic certificate issuance via the ACME protocol.
// The schema mirrors pkg/api.ACMEConfig so the start command can forward
// these fields without translation; the actual ACME implementation lives in
// pkg/api/acme.go (HTTP-01 challenge) and is extended by downstream editions
// via the extension interface for DNS-01.
type ACMEConfig struct {
	// Enabled toggles ACME automatic certificate issuance and renewal.
	Enabled bool `yaml:"enabled" json:"enabled"`
	// DirectoryURL is the ACME directory URL. Defaults to the Let's Encrypt
	// production endpoint when empty (see pkg/api.DefaultACMEDirectoryURL).
	DirectoryURL string `yaml:"directory_url" json:"directory_url"`
	// Email is the ACME registration email. Required when Enabled is true.
	Email string `yaml:"email" json:"email"`
	// ChallengeType is the ACME challenge type: "http-01" or "dns-01".
	// Defaults to "http-01" when empty (see pkg/api.DefaultACMEChallengeType).
	ChallengeType string `yaml:"challenge_type" json:"challenge_type"`
	// Domains is the list of domain names for which the manager should
	// obtain certificates via the ACME protocol. At least one domain is
	// required when Enabled is true and the manager is started by the
	// server (see cmd/tickraft/start.go).
	Domains []string `yaml:"domains" json:"domains"`
}

// WorkerConfig configures the worker process. The Worker is a unified
// deployment that always starts the Scheduler, Executor, and Collector
// modules together; role-based filtering is no longer supported. The worker
// runs in-process alongside the API server in standalone mode and does not
// bind any additional listen ports.
type WorkerConfig struct {
	// Concurrence controls the maximum number of tasks executed concurrently
	// by the executor pool.
	//   - Value range: 0 or positive integer.
	//   - Default: 0 (auto-sized to CPU*2 at startup).
	//   - Effective: startup; changes require a restart.
	//   - Notes: a value of 0 lets the runtime auto-size the pool based on
	//     GOMAXPROCS. Negative values are rejected by Validate.
	Concurrence int `yaml:"concurrence" json:"concurrence"`
	// ProbeTimeout is the default timeout for prober executors (HTTP, TCP,
	// ICMP) when no per-probe timeout is explicitly set.
	//   - Value range: any positive Go duration string (e.g. "5s", "30s").
	//   - Default: "5s".
	//   - Effective: startup; changes require a restart.
	//   - Notes: zero means no timeout (not recommended for production).
	ProbeTimeout Duration `yaml:"probe_timeout" json:"probe_timeout"`
}

// PrismConfig configures the alert evaluation engine. The prism engine runs
// in-process alongside the API server in standalone mode and does not bind
// any additional listen ports.
type PrismConfig struct {
	// EvalInterval is the interval between alert rule evaluation passes.
	//   - Value range: any positive Go duration string.
	//   - Default: "30s".
	//   - Effective: startup; changes require a restart.
	//   - Notes: must be greater than zero; a zero value is rejected by
	//     Validate. Shorter intervals increase CPU usage but reduce alert
	//     latency.
	EvalInterval Duration `yaml:"eval_interval" json:"eval_interval"`
	// Concurrence controls the goroutine pool size for sending notifications
	// across all configured channels.
	//   - Value range: 0 or positive integer.
	//   - Default: 8.
	//   - Effective: startup; changes require a restart.
	//   - Notes: a value of 0 disables the pool (synchronous dispatch).
	//     Negative values are rejected by Validate.
	Concurrence int `yaml:"concurrence" json:"concurrence"`
}

// DatabaseConfig configures the backend database connection. The database
// can be configured in two mutually exclusive ways:
//
//   - DSN: a single connection string in the format
//     "driver://user:password@host:port/database?param1=value1&param2=value2".
//     When DSN is non-empty it takes precedence; the embedded db.Config fields
//     are ignored.
//   - Direct fields: set the embedded db.Config fields (driver, addr,
//     credential, params) individually in YAML. This path is used when DSN is
//     empty. It is useful when credentials are injected via a secret manager
//     and the operator prefers structured fields over a single DSN string.
//
// The runtime supports only SQLite3. Extended storage extensions
// register additional drivers (MySQL, PostgreSQL) via the db.Register SPI.
//
// Example YAML (DSN path):
//
//	database:
//	  dsn: "sqlite:///var/lib/tickraft/tickraft.db?journal_mode=WAL"
//
// Example YAML (direct fields path):
//
//	database:
//	  driver: sqlite3
//	  addr: /var/lib/tickraft/tickraft.db
//	  params:
//	    journal_mode: WAL
//	    busy_timeout: "5000"
type DatabaseConfig struct {
	// DSN is the data source name. When non-empty, it takes precedence over
	// the embedded db.Config fields and is parsed via db.Parse at startup.
	//   - Value range: a valid DSN string for a registered driver.
	//   - Default: "" (empty; falls back to direct db.Config fields).
	//   - Effective: startup; changes require a restart.
	//   - Notes: the runtime recognizes "sqlite://", "sqlite3://",
	//     and bare file paths. Memory databases (":memory:") are not supported.
	//     Use environment variable interpolation for credentials, e.g.
	//     ${TICKRAFT_DB_DSN}, to avoid hardcoding secrets.
	DSN string `yaml:"dsn" json:"dsn"`

	// db.Config is embedded inline so its fields (driver, addr, credential,
	// params) can be set directly in YAML when DSN is empty. When DSN is
	// non-empty, these fields are ignored.
	//   - driver: database driver name (e.g. "sqlite3"). Required when DSN
	//     is empty.
	//   - addr: database location. For SQLite it is a file path; for
	//     network databases it is "host:port". Required when DSN is empty.
	//   - credential: username/password for network databases. Left empty
	//     for SQLite. Use env var interpolation for secrets.
	//   - params: driver-specific parameters (e.g. journal_mode, busy_timeout
	//     for SQLite; database name for network databases).
	//   - Notes: memory databases (":memory:") are not supported.
	db.Config `yaml:",inline" json:",inline"`
}

// ResolveDBConfig resolves DatabaseConfig into a db.Config ready for db.Open.
// If DSN is non-empty, it is parsed via db.Parse and the result is returned.
// If DSN is empty, the embedded db.Config fields are returned as-is. This
// allows callers to configure the database either via a single DSN string or
// via structured fields without special-casing the resolution logic.
func (c *DatabaseConfig) ResolveDBConfig() (db.Config, error) {
	if c.DSN != "" {
		return db.Parse(c.DSN)
	}
	return c.Config, nil
}

// AuthConfig configures JWT token signing and lifetime.
type AuthConfig struct {
	// JWTSecret is the secret used to sign JWT tokens. Required. Use
	// environment variable interpolation, e.g. ${TICKRAFT_JWT_SECRET}.
	JWTSecret string `yaml:"jwt_secret" json:"jwt_secret"`
	// TokenTTL is the lifetime of issued JWT tokens (default "24h").
	TokenTTL Duration `yaml:"token_ttl" json:"token_ttl"`
	// AdminUsername is the built-in admin username (default "admin").
	AdminUsername string `yaml:"admin_username" json:"admin_username"`
	// AdminPassword is the built-in admin password. When empty, a random
	// password is generated and logged once at startup.
	AdminPassword string `yaml:"admin_password" json:"admin_password"`
}

// LoggerConfig configures the application logger.
type LoggerConfig struct {
	// Level controls the minimum severity of log entries emitted.
	//   - Value range: "debug", "info", "warn", "error".
	//   - Default: "info".
	//   - Effective: startup; changes require a restart.
	//   - Notes: invalid values are rejected by Validate.
	Level string `yaml:"level" json:"level"`
	// Mode controls the logger output format and development features.
	//   - Value range: "debug" (console-friendly, stack traces on) or
	//     "release" (JSON, production-hardened).
	//   - Default: "debug".
	//   - Effective: startup; changes require a restart.
	//   - Notes: the --log-mode CLI flag overrides this value at startup.
	Mode string `yaml:"mode" json:"mode"`
	// RetentionDays is the number of days to retain log files before they
	// are rotated out and deleted.
	//   - Value range: positive integer.
	//   - Default: 30.
	//   - Effective: startup; changes require a restart.
	//   - Notes: a non-positive value is normalized to the default (30 days)
	//     during validation. A warn-level log surfaces the fallback.
	RetentionDays int `yaml:"retention_days" json:"retention_days"`
}

// I18nConfig configures the internationalization runtime. The
// kernel ships builtin locale bundles for zh-Hans (default) and en-US; extended
// editions merge additional locales (zh-Hant, en-GB, ar, ja, de, fr, es, ru,
// ko) via Registry.Register at startup.
//
// DefaultLocale is the fallback locale used when no exact match is found in
// the Registry; it must match pkg/i18n.DefaultLocale ("zh-Hans").
// SupportedLocales declares the locales the deployment advertises via
// GET /api/v1/i18n/locales. The default is ["zh-Hans", "en-US"] so
// the language switcher only offers locales with shipped asset bundles;
// operators may extend the list when extended locale packs are registered.
type I18nConfig struct {
	// DefaultLocale is the fallback locale tag (default "zh-Hans"). It must be
	// a valid BCP 47 tag and is normalized to standard casing at load time.
	DefaultLocale string `yaml:"default_locale" json:"default_locale"`
	// SupportedLocales lists the locale tags advertised to frontend clients.
	// When empty, the builtin set (zh-Hans, en-US) is used. Tags must be valid
	// BCP 47; duplicates and the default locale are tolerated.
	SupportedLocales []string `yaml:"supported_locales" json:"supported_locales"`
}

// SetDefaults populates the Config with default values. It must be called on a
// zero-value Config before unmarshaling so that fields absent from the YAML
// file retain their defaults; fields present in the YAML file overwrite the
// defaults during unmarshaling.
func (c *Config) SetDefaults() {
	c.Server.SetDefaults()
	c.Worker.SetDefaults()
	c.Prism.SetDefaults()
	c.Database.SetDefaults()
	c.Auth.SetDefaults()
	c.Logger.SetDefaults()
	c.I18n.SetDefaults()
}

// SetDefaults populates ServerConfig with default values.
func (c *ServerConfig) SetDefaults() {
	c.Addr = ":6153"
	c.EnableCORS = true
	c.EnableAccessLog = true
	c.MaxHeaderBytes = 1048576
	c.MaintenanceInterval = Duration(5 * time.Minute)
	// ReadTimeout and WriteTimeout default to zero (no timeout).
}

// SetDefaults populates WorkerConfig with default values.
func (c *WorkerConfig) SetDefaults() {
	c.ProbeTimeout = Duration(5 * time.Second)
	// Concurrence defaults to 0 (auto).
}

// SetDefaults populates PrismConfig with default values.
func (c *PrismConfig) SetDefaults() {
	c.EvalInterval = Duration(30 * time.Second)
	c.Concurrence = 8
}

// SetDefaults populates DatabaseConfig with default values.
func (c *DatabaseConfig) SetDefaults() {
	// DSN has no default; it is a required field.
}

// SetDefaults populates AuthConfig with default values.
func (c *AuthConfig) SetDefaults() {
	c.TokenTTL = Duration(24 * time.Hour)
	c.AdminUsername = "admin"
	// JWTSecret has no default; it is a required field.
	// AdminPassword defaults to empty (random password generated at startup).
}

// SetDefaults populates LoggerConfig with default values.
func (c *LoggerConfig) SetDefaults() {
	c.Level = "info"
	c.Mode = "debug"
	c.RetentionDays = 30
}

// SetDefaults populates I18nConfig with default values. The default locale
// matches pkg/i18n.DefaultLocale ("zh-Hans"); supported_locales matches the
// builtin locale set shipped with the kernel (zh-Hans, en-US) so the
// frontend language switcher only offers locales backed by asset bundles.
// callers may extend this list at startup after registering
// additional locale packs.
func (c *I18nConfig) SetDefaults() {
	c.DefaultLocale = "zh-Hans"
	c.SupportedLocales = []string{"zh-Hans", "en-US"}
}
