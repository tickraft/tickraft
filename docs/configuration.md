# Configuration

Tickraft is configured by a single YAML file. The `start` command loads it once at startup; every subsystem (API server, scheduler, executor, telemetry, alerting engine) reads the sections it needs from the same file.

A complete, commented example lives at [`configs/config.example.yaml`](../configs/config.example.yaml). Copy it and edit it for your environment:

```bash
cp configs/config.example.yaml config.yaml
```

## Environment-variable interpolation

Sensitive values — database DSN, JWT secret, admin password — should be injected from the environment rather than written to disk. The config loader supports two interpolation forms:

| Form              | Behaviour                                                            |
|-------------------|----------------------------------------------------------------------|
| `${VAR}`          | Expand to the value of `VAR`. A reference to an unset variable without a default causes a load error. |
| `${VAR:-default}` | Expand to the value of `VAR`, or `default` when `VAR` is unset.      |

Example:

```yaml
database:
  dsn: ${TICKRAFT_DB_DSN}
auth:
  jwt_secret: ${TICKRAFT_JWT_SECRET:-change-me-in-production}
  admin_password: ${TICKRAFT_ADMIN_PASSWORD}
```

## Validate before startup

Check the file before launching the server — the validator applies interpolation, defaults, and field-level checks:

```bash
./bin/tickraft config validate -c config.yaml
```

## Sections

### `server` — HTTP API server

The runtime runs in single-port mode: the REST API, the SPA, and the telemetry ingestion endpoint all share `server.addr`. No separate ports are bound.

| Field                  | Type     | Default   | Description                                                        |
|------------------------|----------|-----------|--------------------------------------------------------------------|
| `addr`                 | string   | `:6153`   | HTTP listen address for the API, SPA, and health probes.           |
| `enable_cors`          | bool     | `true`    | Enable the CORS middleware.                                        |
| `enable_access_log`    | bool     | `true`    | Enable the access-log middleware.                                  |
| `max_header_bytes`     | int      | `1048576` | Maximum size of request headers in bytes.                          |
| `read_timeout`         | duration | `0s`      | Maximum duration for reading the entire request. `0s` = no timeout; set an explicit value in production (e.g. `10s`). |
| `write_timeout`        | duration | `0s`      | Maximum duration before timing out writes. `0s` = no timeout; set an explicit value in production (e.g. `30s`). |
| `maintenance_interval` | duration | `5m`      | Interval between background maintenance sweeps (e.g. cleaning expired token blacklist entries). |

#### TLS termination

When `server.tls_enabled` is `true` the server serves HTTPS on `server.addr`. Certificates come either from static PEM files or — when ACME is enabled — from the ACME manager. Static certificates are hot-reloaded: when the files change on disk the server picks up the new pair without a restart, which suits certificate rotation behind a symlink flip.

| Field                | Type       | Default | Description                                                      |
|----------------------|------------|---------|------------------------------------------------------------------|
| `tls_enabled`        | bool       | `false` | Toggle TLS termination. `false` serves plain HTTP.                |
| `tls_cert_file`      | string     | —       | PEM-encoded server certificate. Required when TLS is on and ACME is off. |
| `tls_key_file`       | string     | —       | PEM-encoded server private key. Required when TLS is on and ACME is off. |
| `tls_min_version`    | string     | `1.2`   | Minimum TLS version: `1.2` or `1.3`.                             |
| `tls_cipher_suites`  | []string   | built-in whitelist | Cipher-suite whitelist. Empty applies the built-in default set. |
| `tls_client_ca_file` | string     | —       | PEM-encoded client CA certificate for mutual TLS. Empty disables client-certificate verification. |
| `tls_client_auth`    | string     | `""`    | Client-authentication mode: `""`, `request`, `require`, `verify-if-given`, or `require-verify`. |

#### ACME (automatic certificates)

`server.acme` configures automatic issuance and renewal via the ACME protocol (e.g. Let's Encrypt) as an alternative to static PEM files. The open-source edition serves the HTTP-01 challenge on the same single port as the API; DNS-01 is provided by the extended edition through the extension interface.

| Field             | Type     | Default | Description                                                      |
|-------------------|----------|---------|------------------------------------------------------------------|
| `enabled`         | bool     | `false` | Toggle ACME issuance.                                             |
| `directory_url`   | string   | Let's Encrypt production | ACME directory URL. Point at a staging directory while testing issuance. |
| `email`           | string   | —       | Registration email. Required when enabled.                        |
| `challenge_type`  | string   | `http-01` | `http-01` or `dns-01` (extended edition).                       |
| `domains`         | []string | —       | Domains to obtain certificates for. At least one when enabled.    |

For a self-signed certificate instead of ACME, see `tickraft cert selfsign` (see the deployment guide).

### `worker` — worker runtime

The worker is a unified deployment mode. It always starts the scheduler, the executor, and the telemetry together in-process; no role parameter is needed and no extra ports are bound.

| Field           | Type     | Default | Description                                                        |
|-----------------|----------|---------|--------------------------------------------------------------------|
| `concurrence`   | int      | `0`     | Maximum number of tasks executed concurrently. `0` = auto-sized to `GOMAXPROCS*2`. |
| `probe_timeout` | duration | `5s`    | Default timeout for prober executors.                              |

### `prism` — alerting engine

The alerting engine runs in-process with no extra listen port.

| Field           | Type     | Default | Description                                                        |
|-----------------|----------|---------|--------------------------------------------------------------------|
| `eval_interval` | duration | `30s`   | Interval between alert-rule evaluations.                           |
| `concurrence`   | int      | `8`     | Goroutine pool size for sending notifications. `0` = synchronous.  |

### `database` — database connection

The open-source edition supports only SQLite. The driver is derived from the DSN scheme: `sqlite://` or `sqlite3://` (a bare file path such as `tickraft.db` is also accepted for backwards compatibility). Connection-pool tuning is done via DSN query parameters (e.g. `sqlite:///path?_max_open_conns=10&_max_idle_conns=5`), not via structured fields.

| Field | Type   | Default | Description                                  |
|-------|--------|---------|----------------------------------------------|
| `dsn` | string | —       | Data source name. Use env-var interpolation. |

```yaml
database:
  dsn: "sqlite:///app/data/tickraft.db"
```

### `auth` — JWT and the built-in admin user

| Field            | Type     | Default | Description                                                        |
|------------------|----------|---------|--------------------------------------------------------------------|
| `jwt_secret`     | string   | —       | Secret used to sign JWT tokens. **Must be set.** Use env-var interpolation. |
| `access_ttl`     | duration | `2h`    | Lifetime of issued JWT access tokens.                              |
| `refresh_ttl`    | duration | `168h`  | Lifetime of issued JWT refresh tokens.                             |
| `admin_username` | string   | `admin` | Built-in admin username.                                           |
| `admin_password` | string   | —       | Built-in admin password. When empty, a random password is generated and logged once at startup. Use env-var interpolation. |

### `logger` — logging

| Field            | Type | Default   | Description                                                        |
|------------------|------|-----------|--------------------------------------------------------------------|
| `level`          | string | `info`  | Log level: `debug`, `info`, `warn`, or `error`.                    |
| `mode`           | string | `debug` | Logging mode: `debug` (development) or `release` (production).     |
| `retention_days` | int    | `30`    | Number of days to retain log files before rotation deletes them.   |

### `i18n` — internationalization

The kernel ships builtin locale bundles for `zh-Hans` (default) and `en-US`.

| Field               | Type     | Default     | Description                                                        |
|---------------------|----------|-------------|--------------------------------------------------------------------|
| `default_locale`    | string   | `zh-Hans`   | Fallback locale used when no exact match is found. Must be a valid BCP 47 tag. |
| `supported_locales` | []string | `[zh-Hans, en-US]` | Locales advertised via `GET /api/v1/i18n/locales`. Extend when downstream locale packs are registered. |

## Single-port routing

Because every service shares `server.addr`, routes are partitioned by path prefix:

| Path                          | Service             | Authentication                    |
|-------------------------------|---------------------|-----------------------------------|
| `POST /api/v1/auth/login`     | Auth login          | None (public)                     |
| `POST /api/v1/auth/refresh`   | Auth token refresh  | None (public)                     |
| `GET /api/v1/i18n/locales`    | Locale listing      | None (public)                     |
| `POST /api/v1/telemetry`      | Telemetry ingestion | `X-Tickraft-Asset-Key`            |
| `/api/v1/*` (everything else) | JSON API            | JWT middleware                    |
| `GET /ws`                     | WebSocket push      | Query-token auth (in-handler)     |
| `GET /healthz`, `GET /readyz` | Health probes       | None (whitelisted)                |
| `/`                           | SPA static assets   | None                              |

## Open-source edition quotas

The open-source edition enforces soft quotas to keep the single-process footprint predictable. The source code can be recompiled to lift them.

| Resource              | Quota  |
|-----------------------|--------|
| Monitored assets      | 20     |
| Probers               | 20     |
| Scheduled tasks       | 20     |
| Remediation actions   | 5      |
| HTTP probe interval   | 60 s   |
| Telemetry events/day  | 100 000|

## Related documents

- [Deployment](./deployment.md) — binary, Docker, and development deployment.
- [Getting started](./getting-started.md) — from zero to first task in five minutes.
- [Architecture](./architecture.md) — layered architecture and three-module design.
- [Example file](../configs/config.example.yaml) — the fully commented reference configuration.
