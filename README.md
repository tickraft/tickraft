# Tickraft

**Lightweight all-in-one job scheduler, uptime monitor & alerting tool — in a single self-hosted binary.**

[![License: AGPL v3](https://img.shields.io/badge/License-AGPL_v3-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go&logoColor=white)](https://golang.org)
[![Build Status](https://github.com/tickraft/tickraft/actions/workflows/ci.yaml/badge.svg)](https://github.com/tickraft/tickraft/actions/workflows/ci.yaml)
[![GitHub stars](https://img.shields.io/github/stars/tickraft/tickraft)](https://github.com/tickraft/tickraft/stargazers)
[![GitHub contributors](https://img.shields.io/github/contributors/tickraft/tickraft)](https://github.com/tickraft/tickraft/graphs/contributors)
[![GitHub issues](https://img.shields.io/github/issues/tickraft/tickraft)](https://github.com/tickraft/tickraft/issues)
[![PRs Welcome](https://img.shields.io/badge/PRs-welcome-brightgreen.svg)](CONTRIBUTING.md)

**English** | [简体中文](./README.zh-CN.md)

![Dashboard](docs/screenshots/dashboard.png)

## Why Tickraft?

Small teams usually glue together crontab, a separate uptime checker, a pile of alert scripts, and a spreadsheet of assets. Tickraft replaces that stack with **one self-contained Go binary**: the REST API, the Vue 3 web UI, the scheduling engine, the execution engine, the telemetry pipeline, and the alerting engine all run in a single process on a single port.

- **One binary, zero moving parts** — embedded SQLite, embedded frontend, no external database, message broker, or web server required
- **The full ops loop in one place** — schedule → execute → monitor → alert → self-heal
- **Kernel-first architecture** — every engine is a reusable package with SPI extension points (`pkg/` in Go, `web/packages/` in TypeScript)
- **Bilingual UI** — English and 简体中文 out of the box
- **Featherweight** — a single port (`:6153`), a single YAML config with env-var interpolation, runs anywhere from a Raspberry Pi to a VM

This repository is the open-source edition of Tickraft, licensed under an AGPLv3 + Commercial dual license model (see [License](#license)).

## ✨ Features

### ⏰ Flexible scheduling

- Schedules by **cron expression** (5/6-field, `@every`, `TZ=` timezone prefix), **fixed interval**, **one-shot**, or **event trigger**
- Hierarchical time-wheel engine with sharded task ownership and bounded worker pools
- Retry policies, manual triggering, pause/resume, copy-as-template, tags and groups
- Task dependencies to gate execution on upstream completion, plus full execution history with stats

### 🔁 Built-in executors

- **Local** — run shell commands or scripts
- **Webhook** — call external HTTP endpoints
- **HTTP / TCP / ICMP probers** — availability and latency checks

### 📡 Active & passive monitoring

- Active probing (ICMP ping, TCP port, HTTP) on any schedule, with built-in templates (`icmp-ping`, `http-homepage`, `https-api`, `tcp-database`)
- Passive ingestion endpoint `POST /api/v1/telemetry` for agents and scripts, authenticated by HMAC-SHA256 signature or asset key
- Tumbling-window aggregation (avg / max / min / count / sum) before persistence; metrics, logs, and heartbeats all supported
- Monitor-point status history and trend charts in the UI

### 🚨 Alerting (Prism)

- Rule engine powered by [expr-lang](https://expr-lang.org/) with sandboxed evaluation (node/comparison limits)
- Alert lifecycle management — trigger, acknowledge, resolve — with queryable records
- Alert de-duplication governance to suppress storms
- Notification channels: **email** (TLS none/implicit/STARTTLS, auth PLAIN/LOGIN/CRAM-MD5) and **webhook** (Slack, Telegram, DingTalk, Feishu — anything that accepts a POST); more channels plug in via the channel SPI
- Templated notification content

### 🩹 Self-healing remediation

- Remediation rules select an operator from the matching alert: run a **local** script, call a **webhook**, or hit an **HTTP** endpoint
- Safety rails built in: idempotency keys, cooldown windows, and a circuit breaker that auto-pauses runaway remediations

### 📦 Asset management

- Six asset types: task, device, host, port, website, service
- Four-state lifecycle (normal / abnormal / offline / unknown) with status-change history for auditing
- One-click manual probe per asset

### 🔐 Security

- JWT access/refresh tokens with revocation, API keys (hashed, revocable, cached)
- TOTP multi-factor authentication, must-change-password flow
- Role-based access control: Admin / Developer / Visitor
- TLS with hot reload, plus ACME (Let's Encrypt) HTTP-01 automation and a `cert selfsign` command

### 🧰 Everything else

- WebSocket endpoint streaming system events in real time
- ~80 REST endpoints under `/api/v1`, with an OpenAPI description in [docs/api/openapi.yaml](docs/api/openapi.yaml)
- Strongly typed in-process event bus (20+ event types) with failed-event persistence for replay
- i18n API serving the embedded locale bundles
- Written in Go 1.26 (Hertz, GORM, expr-lang, cobra, zap) with a Vue 3 + TypeScript + Element Plus + ECharts frontend

## 🚀 Quick Start

From source (the only option until the first tagged release ships prebuilt binaries):

```bash
# Prerequisites: Go 1.26+, Node.js 22+, pnpm 9+
git clone https://github.com/tickraft/tickraft.git
cd tickraft
make build

# Start with built-in development defaults (SQLite + development JWT secret)
./bin/tickraft start
```

Then open <http://localhost:6153> and log in as `admin`. The password is read from the environment variable `TICKRAFT_ADMIN_PASSWORD`; if unset, a random one is generated and logged once at startup.

For a managed configuration:

```bash
cp configs/config.example.yaml config.yaml
# set values directly, or export env vars:
#   TICKRAFT_JWT_SECRET      token signing secret
#   TICKRAFT_ADMIN_PASSWORD  initial admin password
#   TICKRAFT_DB_DSN          e.g. sqlite:///app/data/tickraft.db
./bin/tickraft config validate -c config.yaml
./bin/tickraft start -c config.yaml
```

Prebuilt binaries for Linux / macOS / Windows (amd64 / arm64) and an official container image will be attached to [Releases](https://github.com/tickraft/tickraft/releases) for every tagged version.

## 🐳 Docker

Build the image yourself for now — the official image arrives with the first tagged release:

```bash
docker build -t tickraft-ce .
docker run -d --name tickraft -p 6153:6153 \
  -v tickraft-data:/app/data \
  -v $(pwd)/config.yaml:/app/config.yaml \
  -e TICKRAFT_JWT_SECRET="your-secret-key" \
  -e TICKRAFT_ADMIN_PASSWORD="your-admin-password" \
  -e TICKRAFT_DB_DSN=sqlite:///app/data/tickraft.db \
  tickraft-ce start --config /app/config.yaml
```

## 🖥️ Screenshots

| Dashboard | Scheduled tasks | Execution logs |
|:---:|:---:|:---:|
| [![Dashboard](docs/screenshots/dashboard.png)](docs/screenshots/dashboard.png) | [![Tasks](docs/screenshots/scheduler-task-list.png)](docs/screenshots/scheduler-task-list.png) | [![Logs](docs/screenshots/scheduler-log-list.png)](docs/screenshots/scheduler-log-list.png) |

| Assets | Monitor points | Alert records |
|:---:|:---:|:---:|
| [![Assets](docs/screenshots/collector-asset-list.png)](docs/screenshots/collector-asset-list.png) | [![Probers](docs/screenshots/collector-prober-list.png)](docs/screenshots/collector-prober-list.png) | [![Alerts](docs/screenshots/prism-record-list.png)](docs/screenshots/prism-record-list.png) |

| Alert rule editor | Remediation |
|:---:|:---:|
| [![Rule editor](docs/screenshots/prism-rule-edit.png)](docs/screenshots/prism-rule-edit.png) | [![Remediation](docs/screenshots/prism-remediation-list.png)](docs/screenshots/prism-remediation-list.png) |

The full gallery with every screen is in the [user guide](docs/user-guide.md).

## 🏗️ Architecture

![Layered architecture](docs/diagrams/layered-architecture.svg)

Tickraft is a set of independent engines communicating only through a strongly typed event bus:

| Module | Packages | Responsibility |
|---|---|---|
| Scheduler | `pkg/scheduler`, `pkg/cron`, `pkg/task`, `pkg/timewheel` | Task metadata, time-wheel triggering, sharded dispatch, dependencies |
| Executor | `pkg/executor` | Event-driven execution, retries, execution records, capability model |
| Telemetry | `pkg/telemetry` | Probing and passive ingestion, validation, windowed aggregation, persistence |
| Prism | `pkg/prism` | Alert rule evaluation, notification channels, remediation dispatch |
| Foundation | `pkg/event`, `pkg/pool`, `pkg/auth`, `pkg/api`, `pkg/db`, … | Event bus, goroutine pools, auth/RBAC, HTTP middleware, storage SPI |

Every engine exposes an SPI — executor registry, notification-channel factory, telemetry processors, HTTP API plugins (`pkg/api.Plugin`), storage drivers — so downstream editions and forks extend the kernel without touching it. The rules are written down in [docs/module-boundary.md](docs/module-boundary.md) and [docs/extension-guide.md](docs/extension-guide.md).

## 📚 Documentation

| Document | Contents |
|---|---|
| [Getting started](docs/getting-started.md) | From zero to your first scheduled task in 8 steps |
| [User guide](docs/user-guide.md) | Walkthrough of every screen, with screenshots |
| [Configuration](docs/configuration.md) | Every config field, env-var interpolation, edition quotas |
| [Architecture](docs/architecture.md) | Engines, data flows, persistence model |
| [Deployment](docs/deployment.md) | Cross-compilation, Docker, system requirements |
| [Extension guide](docs/extension-guide.md) | SPI-based extension of executors, channels, API plugins |
| [Module boundary](docs/module-boundary.md) | Dependency rules between `pkg/`, `cmd/`, and `internal/` |
| [REST API](docs/api/openapi.yaml) | OpenAPI description of `/api/v1` |

A Chinese reference translation of the docs lives under [docs/zh-CN/](docs/zh-CN/README.md).

> **Edition note:** the open-source edition ships with soft quotas (e.g. 20 assets, 20 monitor points, 20 scheduled tasks — full list in [docs/configuration.md](docs/configuration.md)). They are compile-time defaults, not hard limits; recompiling lifts them.

## 🛠️ Development

```bash
# Frontend (Vue 3 + Vite dev server on :5173, proxies /api to :6153)
cd web && pnpm install && pnpm dev

# Backend
go run ./cmd/tickraft start
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for the full development environment, code standards, and the PR workflow. Backend checks: `make lint test`; frontend checks: `pnpm -C web lint test type-check`.

## 🤝 Contributing

Contributions are welcome! Please read [CONTRIBUTING.md](CONTRIBUTING.md) first — contributors sign the [CLA](CLA.md) before their first PR, commits follow [Conventional Commits](https://www.conventionalcommits.org/), and every PR passes lint, tests, and the license-header check in CI.

Found a bug or have an idea? [Open an issue](https://github.com/tickraft/tickraft/issues) or start a [discussion](https://github.com/tickraft/tickraft/discussions). Security reports follow [SECURITY.md](SECURITY.md).

## ⭐ Show Your Support

If Tickraft saves you from crontab archaeology, please give it a star — it helps others find the project.

## License

This repository is licensed under a **AGPLv3 + Commercial dual license** model. Users may choose either license:

- **AGPLv3 (default)**: for open-source users. Derivative works and network services (SaaS) must open-source all code under AGPLv3. See [LICENSES/AGPLv3.txt](LICENSES/AGPLv3.txt).
- **Commercial license**: for commercial users (enterprise private deployment, SaaS providers). Signing the commercial license agreement exempts you from all AGPLv3 obligations, allowing closed-source distribution and SaaS offerings. See [LICENSES/COMMERCIAL.txt](LICENSES/COMMERCIAL.txt).

For license selection guidance and the full statement, see [LICENSE](LICENSE). For commercial license inquiries, contact licensing@tickraft.com.

Tickraft is maintained by the Auzeka Labs team.
