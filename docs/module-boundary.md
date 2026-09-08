# Module boundaries

This document captures the rules that keep the scheduler, executor, and telemetry decoupled and that govern how a downstream repository may extend the kernel.

## Three-module decoupling rules

The scheduler, executor, and telemetry are independent subsystems. They never import each other and they never call methods on each other. All cross-module communication flows through the event bus.

| Rule | Forbidden | Reason |
|------|-----------|--------|
| M-01 | scheduler → executor import | The scheduler only publishes `TaskTriggered`; direct calls would couple deployment units. |
| M-02 | executor → scheduler import | The executor only publishes `TaskCompleted`; direct calls would create a cycle. |
| M-03 | telemetry → scheduler import | The telemetry module is fully decoupled; it may only optionally publish `StatusChange`. |
| M-04 | telemetry → executor import | Telemetry and execution are separate concerns with no direct communication. |
| M-05 | scheduler → telemetry import | The scheduler is unaware of the telemetry module. |

## Communication contract

| Direction            | Event             | Publisher  | Subscriber        |
|----------------------|-------------------|------------|-------------------|
| scheduler → executor | `TaskTriggered`   | scheduler  | executor          |
| executor → scheduler | `TaskCompleted`   | executor   | scheduler         |
| telemetry → scheduler | `StatusChange`   | telemetry  | scheduler (optional) |

The telemetry module subscribes to no scheduler event, which guarantees it can run in isolation.

## Layering principles

### `pkg/` is the public implementation layer

`pkg/` is the only programming surface the kernel exposes. It holds every SPI interface, every shared data type, every core engine implementation, every GORM model, every registry, and every sentinel error. A downstream repository imports directly from `pkg/` — there are no bridge variables or wrapper types.

### `cmd/` is the binary entry layer

`cmd/tickraft` assembles the CLI: argument parsing, subcommand dispatch, dependency injection, and service startup ordering. It defines no public SPI and no business logic. It must not be imported by `pkg/` or by tests.

### Dependency direction

- `cmd/` → `pkg/` — one-way, the entry layer uses the public packages.
- `tests/` → `pkg/` — one-way, integration tests exercise the public API.
- `pkg/` → `cmd/` or `pkg/` → `tests/` — **forbidden**. The public layer never depends on the entry or test layers.
- downstream → `pkg/` — allowed, the downstream repository imports public types and registers SPI implementations.
- downstream → `cmd/` or `tests/` — **forbidden**. Code that a downstream repository needs must live in `pkg/`.

### Extension model

A downstream repository imports public types from `pkg/` and registers its implementations through the SPI registries documented in the [Extension guide](./extension-guide.md). It must not modify kernel source files. When the kernel does not find a registered implementation, it falls back to an open-source default so the kernel always runs standalone.

## HTTP handler ownership: a two-track layering

HTTP handler ownership follows the native Go semantics of `pkg/` (public library) versus `internal/` (private application):

### `pkg/` public layer → centralised

- All shared HTTP handlers live under `pkg/api/handler/`, middleware under `pkg/api/middleware/`, and the routing composition root in `pkg/api/router` (`RegisterRoutes` plus the `RegisterOption` option set).
- Business packages (`pkg/auth`, `pkg/task`, `pkg/prism/*`, `pkg/asset`, `pkg/executor`, …) must not import `cloudwego/hertz` or `net/http`.
- Exemption: referencing only the `http.Status*` status-code constants of `net/http` (for the `errdefs.ServiceError` error mapping) is not transport coupling and is allowed; any other use (`http.Request`, `http.ResponseWriter`, `http.Client`, …) remains forbidden.
- Rationale: `pkg/` is imported across repositories (downstream repositories import `tickraft/pkg/*`), so it must stay transport-agnostic and independently unit-testable; status-code constants are pure values that introduce no transport-type dependency.

### `internal/` application layer → distributed package-by-feature

- Each application package keeps `handler.go` + `routes.go` cohesive, with handlers calling the same package's service directly.
- Edition-specific routes (plugins, licensing, …) stay in each repository's `internal/` tree and are injected into the shared composition root via `RegisterOption`; this repository's `internal/` only assembles (cli / service / quota / web) and defines no separate router.
- Rationale: `internal/` cannot be imported externally, so cohesion outweighs transport decoupling.

For the layering decision rule (both repositories → `pkg/`, one repository → `internal/`, edition differences → an injection seam), the new-package decision tree, and the dead-code disposal flow, see [Architecture](./architecture.md).

## Related documents

- [Architecture](./architecture.md) — layered architecture and three-module design.
- [Extension guide](./extension-guide.md) — every SPI extension point and how to register it.
