# Extension guide

This guide is for developers who build a downstream repository on top of the Tickraft kernel. It describes every Service Provider Interface (SPI) exposed by the `pkg/` layer and how to register a custom implementation at startup without modifying kernel source code.

## Dual-repository architecture

Tickraft follows an "open kernel + downstream extension" layout:

- **tickraft (kernel repository)** — module path `github.com/tickraft/tickraft`. Provides the generic kernel and the complete runnable open-source product. It has no dependency on any downstream repository and compiles standalone.
- **downstream repository** — built on top of the kernel. It only adds incremental capabilities; it never re-implements kernel logic and never edits kernel source files.

### One-way dependency rule

Only the downstream repository may depend on the kernel. The kernel must never import, reference, or sense any downstream code, configuration, or data structure. Concretely:

- The downstream repository imports only public packages under the kernel's `pkg/` directory.
- Both repositories are independent Go modules. Cross-repository relative paths and code copy-for-reuse are forbidden.
- The downstream repository must not edit any file under the kernel's `pkg/` to implement a feature.

### SPI injection philosophy

Every extension capability is expressed as an interface defined in the kernel's `pkg/` layer. The downstream repository implements the interface and registers the implementation during its `main()` initialisation sequence. The kernel holds only the interface reference and calls back into the implementation at runtime.

| Characteristic        | Description                                                                 |
|-----------------------|-----------------------------------------------------------------------------|
| Interface in kernel   | The SPI interface and registration entry live in a kernel `pkg/` package.   |
| Register at startup   | The downstream `main()` calls the registration function before the server starts. |
| Kernel is unaware     | The kernel never imports downstream code; it only invokes the interface.    |
| Graceful degradation  | Without an injected implementation the kernel falls back to an open-source default (usually in-memory or deny-all), so the kernel runs standalone. |

## SPI panorama

| # | Extension point | Kernel package   | Registration entry                              | Purpose                                        |
|---|-----------------|------------------|-------------------------------------------------|------------------------------------------------|
| 1 | Executor        | `pkg/executor`   | `Registry.Register`                             | Custom task executors (e.g. SSH, MySQL).       |
| 2 | Channel         | `pkg/prism/channel` | `channel.Register`                           | Custom alert notification channels.            |
| 3 | Telemetry       | `pkg/telemetry`  | `ListenerRegistry.Register` / `ProcessorRegistry.Register` | Passive listeners and data processors. |
| 4 | API plugin      | `pkg/api`        | `Server.RegisterPlugin`                         | Custom routes, middleware, lifecycle hooks.    |
| 5 | CLI subcommand  | `pkg/cli`        | `cli.RegisterCmd`                               | Custom CLI subcommands.                        |
| 6 | Storage driver  | `pkg/db`         | `db.Register`                                   | Custom database drivers.                       |

> Auth extensions (SSO providers, permission validators, tenant resolvers) are also exposed as SPIs in `pkg/auth` for downstream repositories that need multi-tenant or SSO capabilities. The open-source edition ships a single-tenant, single-admin default.

---

## Executor extension

Inject a custom task executor so the scheduler can trigger extended task types. The open-source edition ships `local`, `webhook`, `http`, `tcp`, `icmp`, `udp`, and `dns` executors.

**Kernel package**: `pkg/executor` · **Registration**: `Registry.Register(&MyExecutor{})`

Implement the `Executor` interface (`Type()`, `Category()`, `Role()`, `Execute()`) and register before the runner starts. `Registry.Register` returns an error on duplicate `Type()`.

```go
package myssh

type Executor struct{}

func (e *Executor) Type() string                { return "ssh" }
func (e *Executor) Role() executor.Role         { return executor.RoleActuator }
func (e *Executor) Execute(ctx context.Context, req executor.ExecutionRequest) (*executor.Result, error) {
    // parse req.ExecutorConfig, run the SSH command, return the result
    return &executor.Result{Status: 0, Body: output}, nil
}
```

```go
// in main()
execRegistry.Register(&myssh.Executor{})
```

---

## Channel extension

Inject a custom alert notification channel. The open-source edition ships a `webhook` channel.

**Kernel package**: `pkg/prism/channel` · **Registration**: `channel.Register("slack", factory)`

Implement a `ChannelFactory` that reads a `channel.Config` and returns an `alert.Channel` (`Name()`, `Send()`). The type name is case-insensitive; a later registration overrides an earlier one, so a downstream repository can replace the built-in `webhook`. Register before `LoadChannels` is called.

```go
func factory(cfg channel.Config) (alert.Channel, error) {
    return &SlackChannel{webhookURL: cfg.URL}, nil
}
```

```go
// in main(), before LoadChannels
channel.Register("slack", factory)
```

---

## Telemetry extension

Inject passive listeners (e.g. Syslog, SNMP trap, MQTT) and data processors. The open-source edition ships an HTTP listener and device/task processors.

**Kernel package**: `pkg/telemetry` · **Registration**: `ListenerRegistry.Register` / `ProcessorRegistry.Register`

A `Listener` (`Type()`, `Start()`, `Stop()`) receives external data and forwards it to the ingestion callback. A `Processor` (`Type()`, `Process()`, `OnTimeout()`) handles a specific asset type. Duplicate `Type()` registrations return an error. The registries are constructed by the telemetry manager and injected into the downstream repository.

```go
type SyslogListener struct{}
func (l *SyslogListener) Type() string { return "syslog" }
func (l *SyslogListener) Start(ctx context.Context, ingest func(context.Context, *telemetry.Telemetry)) error {
    // start syslog server, call ingest(ctx, &report) for each message
    return nil
}
```

```go
// in main()
listenerRegistry.Register(&SyslogListener{})
```

---

## API plugin extension

Inject custom HTTP routes, global middleware, and start/stop lifecycle hooks. Useful for mounting additional business endpoints.

**Kernel package**: `pkg/api` · **Registration**: `server.RegisterPlugin(&MyPlugin{})`

Implement the `Plugin` interface (`Name()`, `RegisterRoutes()`, `Middlewares()`, `OnStart()`, `OnStop()`). Plugin middleware runs after the built-in middleware chain. `OnStart` failure aborts startup; `OnStop` failure is logged but does not block shutdown. Register before `Server.Start()`.

```go
type Plugin struct{}
func (p *Plugin) Name() string { return "my-plugin" }
func (p *Plugin) RegisterRoutes(root *pkgapi.RouterGroup) {
    g := root.Group("/api/v1/my")
    g.GET("/status", p.status)
}
```

```go
// in main()
server.RegisterPlugin(&Plugin{})
```

---

## CLI subcommand extension

Inject custom CLI subcommands so the downstream binary can mount extended commands alongside the open-source ones.

**Kernel package**: `pkg/cli` · **Registration**: `cli.RegisterCmd(cmd)`

Build a `*cobra.Command` and register it before the root command is constructed. The kernel root command calls `cli.GetRegisteredCmds()` to aggregate every registered subcommand. Prefer calling a registration function explicitly from `main()` rather than relying on `init()` side effects.

```go
func NewActivateCmd() *cobra.Command {
    return &cobra.Command{
        Use:   "activate <key>",
        Short: "Activate a license key",
        Args:  cobra.ExactArgs(1),
        RunE: func(cmd *cobra.Command, args []string) error { /* ... */ return nil },
    }
}
```

```go
// in main(), before root command construction
cli.RegisterCmd(NewActivateCmd())
```

---

## Storage driver extension

Inject a custom database driver so the downstream repository can use a backend the kernel does not ship. The open-source edition ships a SQLite driver.

**Kernel package**: `pkg/db` · **Registration**: `db.Register("oracle", opener)`

Implement an `Opener` that returns a `*gorm.DB`. Duplicate driver names return an error. Register before `db.Open` is called. The kernel's `AutoMigrate` only migrates core models; the downstream repository is responsible for migrating its own extension models.

```go
func Opener(ctx context.Context, cfg db.Config) (*gorm.DB, error) {
    return gorm.Open(oracle.Open(cfg.DSN), &gorm.Config{})
}
```

```go
// in main(), before db.Open
db.Register("oracle", Opener)
```

---

## Composition order

All SPI registrations must complete before the server starts. The recommended order, shown in the downstream `main()`:

1. **Storage driver** — so `db.Open` can resolve the driver.
2. **Auth extensions** — so the authz service and permission middleware observe downstream providers when the API starts.
3. **Channel factories** — so `LoadChannels` finds them in the config-loading phase.
4. **Executors** — so the runner can dispatch extended task types.
5. **Telemetry listeners / processors** — so the telemetry manager can start them.
6. **CLI subcommands** — before the root command is constructed.
7. **API plugins** — before `Server.Start()` so routes and hooks are wired.

> The kernel's `main.go` is the authoritative composition example; this guide only summarises the ordering constraints.

## Related documents

- [Architecture](./architecture.md) — layered architecture and three-module design.
- [Module boundaries](./module-boundary.md) — the rules that keep modules decoupled.
- [Configuration](./configuration.md) — every configuration field explained.
- [OpenAPI specification](./api/openapi.yaml) — REST API paths and schemas.
