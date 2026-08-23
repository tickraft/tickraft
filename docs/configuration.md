# Configuration

Tickraft is configured by a single YAML file. The `start` command loads it once at startup; every subsystem (API server, scheduler, executor, collector, alerting engine) reads the sections it needs from the same file.

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

The runtime runs in single-port mode: the REST API, the SPA, and the webhook listener all share `server.addr`. No separate ports are bound.

| Field                  | Type     | Default   | Description                                                        |
|------------------------|----------|-----------|--------------------------------------------------------------------|
| `addr`                 | string   | `:6153`   | HTTP listen address for the API, SPA, and health probes.           |
| `enable_cors`          | bool     | `true`    | Enable the CORS middleware.                                        |
| `enable_access_log`    | bool     | `true`    | Enable the access-log middleware.                                  |
| `max_header_bytes`     | int      | `1048576` | Maximum size of request headers in bytes.                          |
| `read_timeout`         | duration | `10s`     | Maximum duration for reading the entire request. `0s` = no timeout.|
| `write_timeout`        | duration | `30s`     | Maximum duration before timing out writes. `0s` = no timeout.      |
| `maintenance_interval` | duration | `5m`      | Interval between background maintenance sweeps (e.g. cleaning expired token blacklist entries). |

### `worker` — worker runtime

The worker is a unified deployment mode. It always starts the scheduler, the executor, and the collector together in-process; no role parameter is needed and no extra ports are bound.

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
| `channel_config`| string   | `""`    | Opaque notification-channel configuration (path or inline JSON/YAML). |

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

| Prefix        | Service             | Authentication          |
|---------------|---------------------|-------------------------|
| `/api/v1/*`   | JSON API            | JWT middleware          |
| `/webhook/*`  | Telemetry ingestion | `X-Tickraft-Asset-Key`  |
| `/healthz`    | Health probe        | None (whitelisted)      |
| `/`           | SPA static assets   | None                    |

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
