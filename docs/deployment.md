# Deployment

## Build

### Build from source

```bash
go build -o bin/tickraft ./cmd/tickraft
```

or with the Makefile:

```bash
make build
```

The binary is written to `bin/tickraft`.

### Cross-compilation

```bash
make cross-build
```

Supported targets: `linux/amd64`, `linux/arm64`, `windows/amd64`, `darwin/amd64`, `darwin/arm64`.

## Run

The open-source edition runs only in standalone mode — a single process that serves the API, the SPA, the scheduler, the executor, the telemetry, and the alerting engine on one HTTP port.

```bash
./bin/tickraft start --config config.yaml
```

When `--config` is omitted, an built-in default configuration (SQLite, a development JWT secret) is used. It is suitable for local experiments only. The default listen address is `:6153` and can be changed via `server.addr` in the configuration file.

### Prepare the configuration file

Copy the example file and edit it for your environment:

```bash
cp configs/config.example.yaml config.yaml
```

The configuration file uses YAML and supports environment-variable interpolation so that secrets (database DSN, JWT secret, admin password) can be injected from the environment instead of being written to disk. See [Configuration](./configuration.md) for every field.

### Validate the configuration

Validate the file before starting the server — this checks environment-variable interpolation, applies defaults, and verifies every field:

```bash
./bin/tickraft config validate -c config.yaml
```

## Docker

### Build the image

```bash
make docker
```

### Run a container

Run the image you just built (same as the [README](../README.md) quick start):

```bash
docker run -d \
  --name tickraft \
  -p 6153:6153 \
  -v tickraft-data:/app/data \
  -v $(pwd)/config.yaml:/app/config.yaml \
  -e TICKRAFT_JWT_SECRET="your-secret-key" \
  -e TICKRAFT_ADMIN_PASSWORD="your-admin-password" \
  tickraft-ce:latest start --config /app/config.yaml
```

Release builds are published to `ghcr.io/tickraft/tickraft-ce` by the release pipeline; substitute that image tag to run a published build instead of a local one.

### docker-compose

```yaml
version: '3'
services:
  tickraft:
    image: tickraft-ce:latest
    command: ["start", "--config", "/app/config.yaml"]
    ports:
      - "6153:6153"
    volumes:
      - ./data:/app/data
      - ./config.yaml:/app/config.yaml
    environment:
      - TICKRAFT_JWT_SECRET=your-secret-key
      - TICKRAFT_ADMIN_PASSWORD=your-admin-password
      - TICKRAFT_DB_DSN=sqlite:///app/data/tickraft.db
    restart: unless-stopped
```

## Single-port routing

One Hertz engine serves every protocol on `server.addr`. Routes are partitioned by path prefix, so there are no port conflicts:

| Path prefix   | Service            | Authentication          | Notes                                  |
|---------------|--------------------|-------------------------|----------------------------------------|
| `/api/v1/*`   | JSON API           | JWT middleware          | Tasks, assets, alerts, system, etc.    |
| `POST /api/v1/telemetry` | Telemetry ingestion | Asset-Key header        | Unified telemetry reporting endpoint.  |
| `/healthz`    | Health probe       | None (whitelisted)      | Returns 503 when dependencies are down.|
| `/`           | SPA static assets  | None                    | Vue 3 frontend.                        |

Global middleware (RequestID → AccessLog → Recovery → CORS → TrustedProxy) runs on every path; route-group middleware is mounted per prefix.

## Ingesting Prometheus exposition format

`POST /api/v1/telemetry` also accepts the Prometheus text exposition format (Content-Type `text/plain; version=0.0.4; charset=utf-8`), so anything that can emit a `.prom` file can report metrics without a JSON wrapper. Authentication and identity follow the same model as JSON pushes:

- **Signature** (recommended): `X-Tickraft-Signature` carries the hex-encoded HMAC-SHA256 of the raw body, signed with the global secret or a per-point secret. A per-point match also binds the report to that point's asset.
- **Asset identity via query string**: `?asset_id=<id>` or `?asset_key=<key>&tenant_id=<id>` when no signature credential applies.

Series are flattened into the metric map: labels fold into the key (`http_requests_total{code="200",method="get"}`), and histograms/summaries expand into `_sum`, `_count`, `_bucket{le="..."}`, and `{quantile="..."}` series. One push counts as one event against the daily quota and is capped at 64 KiB.

```bash
curl -X POST "http://localhost:6153/api/v1/telemetry?asset_id=1" \
  -H 'Content-Type: text/plain; version=0.0.4; charset=utf-8' \
  -H "X-Tickraft-Signature: $(openssl dgst -sha256 -hmac "$SECRET" -hex < metrics.prom | cut -d' ' -f2)" \
  --data-binary @metrics.prom
```

### node_exporter textfile bridge

A one-line cron forwards every file the node_exporter textfile collector produces:

```cron
* * * * * cat /var/lib/node_exporter/textfile_collector/*.prom > /tmp/all.prom && curl -s -X POST "http://tickraft:6153/api/v1/telemetry?asset_id=1" -H 'Content-Type: text/plain; version=0.0.4' -H "X-Tickraft-Signature: $(openssl dgst -sha256 -hmac \"$SECRET\" -hex < /tmp/all.prom | cut -d' ' -f2)" --data-binary @/tmp/all.prom
```

## System requirements

- **Operating system**: Linux, macOS, or Windows.
- **Architecture**: amd64 or arm64.
- **Memory**: ≥ 30 MB idle.
- **Disk**: ≥ 1 GB recommended (driven by log and telemetry volume).
- **Database**: embedded SQLite — no external database server required.

## Development mode

### Frontend dev server

```bash
cd web
pnpm install
pnpm run dev
```

The Vite dev server runs on `http://localhost:5173` with mock data enabled, so the UI is fully interactive without a running backend.

### Backend dev

```bash
go run ./cmd/tickraft start
```

Without `--config` the built-in defaults are used, which is convenient for quick local launches.

## Related documents

- [Configuration](./configuration.md) — every configuration field explained.
- [Getting started](./getting-started.md) — from zero to first task in five minutes.
- [Architecture](./architecture.md) — layered architecture and three-module design.
- [OpenAPI specification](./api/openapi.yaml) — REST API paths and schemas.
