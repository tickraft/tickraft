# Architecture

## Overview

Tickraft ships as a single self-contained binary that bundles the REST API, the Vue 3 single-page application, the scheduling engine, the execution engine, the collection engine, and the alerting engine. It persists state in an embedded SQLite database and has zero external runtime dependencies, so a single `tickraft start` process is enough to run the entire product.

The runtime is organised into three independent subsystems — **scheduler**, **executor**, and **collector** — that never import each other. All cross-module communication flows through a strongly typed event bus, which keeps each subsystem independently replaceable and testable.

## Layered architecture

![Layered architecture](./diagrams/layered-architecture.svg)

A single HTTP listener (`server.addr`, default `:6153`) serves every protocol: the JSON API under `/api/v1/*`, webhook ingestion under `/webhook/*`, the health probe at `/healthz`, and the SPA assets at `/`. There is no multi-port deployment mode in the open-source edition.

## Three-module architecture

The scheduler, executor, and collector are deliberately decoupled. They share no Go packages, call no methods on each other, and coordinate exclusively by publishing typed events.

### scheduler — pure scheduling engine

The scheduler is responsible only for deciding *when* a task should run:

- **Task metadata** — registration, updates, deletion, and an in-memory cache of task definitions.
- **Triggering** — a hierarchical time wheel combined with cron expressions computes the next firing time and publishes a `TaskTriggered` event when it is due.
- **Sharding** — a shard manager decides whether the current node owns a given task, so multiple instances can split work without duplicate execution.
- **Dependency tracking** — downstream tasks only fire after their upstream dependencies complete successfully.
- **Event-driven triggers** — the scheduler subscribes to `TaskCompleted` to update dependency state and to `StatusChange` to trigger event-driven tasks.

The scheduler never imports the executor package; it has no knowledge of *how* a task is executed.

### executor — task execution engine

The executor subscribes to `TaskTriggered`, looks up the right executor implementation, runs it, and publishes a `TaskCompleted` receipt.

- **Worker pool** — a bounded semaphore caps concurrent executions (default 100); when saturated the work degrades to inline execution rather than spawning unbounded goroutines.
- **Retry** — retry count and interval are read from the task metadata and applied transparently.
- **Status inference** — the result of each execution is mapped to a resource status (`Normal` / `Abnormal`).
- **Executor registry** — executors register by name and declare a capability bitmask: write actions (`local` commands, `webhook` notification callbacks), read-only probes (`icmp`, `tcp`), and the dual-mode `http` (`CapProbe | CapExec`) that both probes endpoints and runs as a scheduled task action. Task creation rejects types without a write capability and active monitor points reject types without probe capability, both with an immediate 400.
- **Operation kinds and record routing** — every execution carries an operation (`probe` or `execute`). The finished record is handed to a routing store wired at the assembly layer: `execute` records persist to the task execution log (`sys_schedule_log`), `probe` records to the telemetry probe record table (`sys_probe_record`). Neither domain package knows about the other's storage.

### collector — data collection engine

The collector ingests externally reported data and is fully decoupled from the scheduler — it subscribes to no scheduler events.

- **Listener SPI** — passive receivers model every ingestion channel (webhook, syslog, SNMP trap, MQTT, …) as a `Listener`.
- **Active probing** — monitor points schedule probes as synthetic tasks through the scheduler/executor pipeline. Each result is stored as a structured probe record — one row per probe holding status, latency, status code, output, and error — and refreshes the point's runtime status column. A probe record is the result of an operation the runtime performed; it is distinct from the logs and metrics listeners ingest from external reporters, which are observations about an asset.
- **Validator** — inbound reports are checked for structural correctness, asset existence, tenant ownership, and size limits.
- **Aggregator** — metrics are bucketed into fixed tumbling windows and reduced to avg / max / min / count / sum statistics.
- **Persistence** — metrics and logs are batched into the stores.
- **Built-in HTTPListener** — an out-of-the-box HTTP endpoint with HMAC-SHA256 signature or asset-key authentication.

## Event bus

The event bus (`pkg/event`) is the only communication channel between the three modules. It offers strongly typed publish/subscribe via generics, so event payloads are checked at compile time.

| Event             | Publisher  | Subscriber        | Purpose                                  |
|-------------------|------------|-------------------|------------------------------------------|
| `TaskTriggered`   | scheduler  | executor          | A task is due; the executor should run it. |
| `TaskCompleted`   | executor   | scheduler         | Execution finished; update dependencies.   |
| `StatusChange`    | collector  | scheduler (optional) | A resource changed state; fire event-driven tasks. |

![Event bus flow](./diagrams/event-bus.svg)

Execution lifecycle events carry an `operation` field (`probe` or `execute`); consumers treat an empty value as `execute` for compatibility with events published before the field existed.

The collector never subscribes to scheduler events, which guarantees the collection engine can run in isolation.

## Data flows

1. **Schedule → execute** — the time wheel fires → `TaskTriggered` published → executor runner consumes → executor runs (with retry) → status inferred → `TaskCompleted` published → scheduler updates dependencies → execution record persisted.
2. **Ingest → persist** — external report → listener receives → validator checks → processor determines status → state manager detects change → aggregator windows the metrics → persistence batch-writes metrics and logs.
3. **Event-driven** — collector emits `StatusChange` → scheduler subscribes → triggers an associated event-driven task → flows into the schedule → execute path.
4. **Active probe → record** — monitor point fires → synthetic probe task published → executor runs the prober → the `probe` record is routed to `sys_probe_record` and the point's runtime status is refreshed. Task executions follow the same pipeline but persist to `sys_schedule_log`; the operation kind decides the destination.

## Common components

- **config** (`pkg/config`) — loads YAML, interpolates environment variables (`${VAR}` / `${VAR:-default}`), and validates the file before startup.
- **pool** (`pkg/pool`) — a unified goroutine pool manager. Every concurrent task in the system (executors, notifications, maintenance loops, listeners) submits through it; bare `go` statements are forbidden.
- **db** (`pkg/db`) — the storage abstraction over SQLite. Business modules read and write through this layer rather than issuing raw SQL.
- **prism / alerting** (`pkg/prism`) — the alerting engine subscribes to alert events, matches them against rules, and dispatches notifications through pluggable channels (the open-source edition ships a webhook channel). Alert rules live in `pkg/prism/alert`, channels in `pkg/prism/channel`, remediation in `pkg/prism/remediation`.
- **auth** (`pkg/auth`) — JWT issuance, token blacklist, bcrypt password hashing, and the built-in admin user.

## Repository layering: `pkg/` vs `internal/`

The open-source repository is the kernel; downstream editions import it and must never modify it. The layering rules below keep the kernel complete on its own while letting editions differ without forking.

| Rule | Statement |
|------|-----------|
| L-01 | Code used by both this repository and a downstream edition lives in `pkg/`. |
| L-02 | Code used by only one edition lives in that edition's `internal/`. |
| L-03 | Edition differences are injected through options, interfaces, or decorators — implementations are never copied between repositories. |
| L-04 | Dependencies run one way: `internal/` → `pkg/`. A `pkg/` package importing `internal/` is a build error and fails CI. |
| L-05 | Domain packages are self-contained: model, store, engine, service contract, and default implementation live together (`pkg/task`, `pkg/telemetry`, `pkg/system`, `pkg/prism/*`). Services do not get a separate top-level package per layer. |
| L-06 | `pkg/api` is pure transport — server, TLS, middleware, HTTP handlers, and the route composition root (`pkg/api/router`). Domain packages must not import `pkg/api`. |

In this repository `internal/` holds edition assembly only — `cli` (entry wiring), `service` (startup composition), `quota` (quota defaults), and `web` (SPA embed). All business logic lives in `pkg/`. When a package's only consumer is a downstream edition, it does not belong in this repository at all (see the relocation precedent in [Dead-code disposition](#dead-code-disposition)).

## Composition root and injection seams

Route registration lives in the shared composition root, `pkg/api/router`. `RegisterRoutes(server, jwtMgr, authService, assetKeyGetter, opts...)` builds the middleware chain and binds every handler:

- **Required services** — auth, task, alert, system, and telemetry services plus the JWT middleware are validated in one place (`validateRouteConfig` in `pkg/api/handler`); a missing piece fails startup with a single descriptive error instead of per-route nil panics.
- **Optional surfaces** — every other component (channel, remediation, certificates, websocket, i18n, telemetry report handler, …) is a `RegisterOption`; the root nil-guards each one and degrades gracefully.
- **Edition seams** — `WithAPIKeyAuth()` opts a deployment into API-key authentication alongside JWT (omitted, the chain is JWT-only); `WithUserRevoker(...)` hooks token revocation into the password-change flow; an edition passes method values or adapters as plain functions.

Downstream editions never copy the router. Their `internal/api` only carries edition-specific routes (plugins, licensing, rate limiting) and feeds kernel services — wrapped in decorators — into `RegisterRoutes` as options.

Domain packages follow the same seam discipline:

- `pkg/task/service` accepts a nil engine — a node without the scheduling role runs as pure task CRUD.
- `pkg/prism/alert/service` reloads rules through a minimal `reloader` interface instead of depending on a concrete engine type.
- Cross-cutting edition behaviors attach as decorators around the shared service (for example, syncing data changes), never as forked copies.

## Adding a package — decision tree

1. Will both this repository and a downstream edition use it? → `pkg/`.
2. Only this edition? → `internal/`.
3. Shared behavior with an edition-specific variant? → base implementation in `pkg/` plus an option or interface seam; the variant injects through it (L-03).
4. Never create a mirror copy of a `pkg/` implementation inside a downstream repository — extend it or decorate it.
5. Before creating a new package, check whether an existing one should absorb it. Single-file micro-packages are merged away, not accumulated (precedents: `pkg/api/hlogzap` merged into `pkg/api`, `pkg/auth/password` merged into `pkg/auth`, `pkg/prism/channel/format` inlined at its single consumer).

## Dead-code disposition

When code turns up with no references, apply the steps in order:

1. **Feature comparison** — is the same capability already implemented elsewhere, more completely? If an equivalent, stronger implementation exists and is wired in, delete the weaker one. (Precedent: the old `internal/auth` login flow versus `pkg/auth.Service.Login`, which adds rate limiting, disable checks, and failure tracking.)
2. **Product surface** — does any edition actually expose the feature? No surface in either edition plus equivalent coverage elsewhere means delete. (Precedent: self-registration had no entry point in any edition.)
3. **Incomplete but needed** — if the feature is unfinished yet the product needs it, complete the implementation instead of deleting it. Reference the existing implementations in both repositories as the specification.
4. **Uncertain** — record the item in the audit document's pending-decision list and leave the code in place.

Relocation — not deletion — applies to live code that merely sits on the wrong side of the layering line: `pkg/console` moved to its only consumer's `internal/` tree rather than being removed.

## Persistence model

The open-source edition persists all state in a single SQLite file. Every business table carries a `tenant_id` column that enables row-level isolation; even though the open-source edition is single-tenant by default, the column is present so downstream extensions can enable multi-tenancy without a schema migration. Database schema is managed by GORM `AutoMigrate` at startup — there is no hand-written migration SQL to maintain.

Execution results live in two domain-owned tables: `sys_schedule_log` for task executions (`execute` operations) and `sys_probe_record` for monitor-point probes (`probe` operations). Passive collection data stays in `sys_collect_metric` and `sys_collect_log`.

## Related documents

- [Deployment](./deployment.md) — binary, Docker, and development deployment.
- [Configuration](./configuration.md) — every configuration field explained.
- [Getting started](./getting-started.md) — from zero to first task in five minutes.
- [User guide](./user-guide.md) — walkthrough of every screen in the web UI.
- [Extension guide](./extension-guide.md) — how to add executors, listeners, channels, and API plugins.
- [Module boundaries](./module-boundary.md) — the rules that keep the three modules decoupled.
- [OpenAPI specification](./api/openapi.yaml) — REST API paths and schemas.
