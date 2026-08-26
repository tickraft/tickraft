# Module boundaries

This document captures the rules that keep the scheduler, executor, and collector decoupled and that govern how a downstream repository may extend the kernel.

## Three-module decoupling rules

The scheduler, executor, and collector are independent subsystems. They never import each other and they never call methods on each other. All cross-module communication flows through the event bus.

| Rule | Forbidden | Reason |
|------|-----------|--------|
| M-01 | scheduler → executor import | The scheduler only publishes `TaskTriggered`; direct calls would couple deployment units. |
| M-02 | executor → scheduler import | The executor only publishes `TaskCompleted`; direct calls would create a cycle. |
| M-03 | collector → scheduler import | The collector is fully decoupled; it may only optionally publish `StatusChange`. |
| M-04 | collector → executor import | Collection and execution are separate concerns with no direct communication. |
| M-05 | scheduler → collector import | The scheduler is unaware of the collector's existence. |

## Communication contract

| Direction            | Event             | Publisher  | Subscriber        |
|----------------------|-------------------|------------|-------------------|
| scheduler → executor | `TaskTriggered`   | scheduler  | executor          |
| executor → scheduler | `TaskCompleted`   | executor   | scheduler         |
| collector → scheduler | `StatusChange`   | collector  | scheduler (optional) |

The collector subscribes to no scheduler event, which guarantees it can run in isolation.

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

## HTTP Handler 归属：分层双轨

全工作区 HTTP handler 归属遵循 Go `pkg/`（公共库）与 `internal/`（私有应用）的原生语义分层：

### pkg/ 公共库层 → 集中式

- 所有 HTTP handler 统一在 `pkg/api/handler/`；中间件统一在 `pkg/api/middleware/`；路由组合根统一在 `pkg/api/router`（`RegisterRoutes` + `RegisterOption` 选项集）。
- 业务包（`pkg/auth`、`pkg/task`、`pkg/prism/*`、`pkg/asset`、`pkg/executor` 等）禁止 import `cloudwego/hertz`、`net/http`。
- 理由：`pkg/` 被跨仓导入（atlas / tickraft-x 均 import tickraft/pkg/*），必须传输层无关、可独立单测。

### internal/ 应用层 → 分布式 package-by-feature

- 每个业务包内 `handler.go` + `routes.go` 高内聚，handler 直接调用同包 Service。
- 版次特有的路由（插件、许可等）留在各仓 `internal/`，通过 `RegisterOption` 注入共享组合根；本仓 `internal/` 只做装配（cli / service / quota / web），不设独立 router。
- 理由：`internal/` 外部不可导入，高内聚 > 传输层解耦；这是 atlas / atrium / arcadia / axiom / tickraft-x 的既成惯例。

分层判定（两仓都用→pkg、仅单仓→internal、版次差异→注入缝）与新增包决策树、死代码处置流程见 [Architecture](./architecture.md)。

## Related documents

- [Architecture](./architecture.md) — layered architecture and three-module design.
- [Extension guide](./extension-guide.md) — every SPI extension point and how to register it.
