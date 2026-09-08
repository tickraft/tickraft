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

| # | Extension point | Kernel package      | Registration entry                            | Purpose                                              |
|---|-----------------|---------------------|-----------------------------------------------|------------------------------------------------------|
| 1 | Executor        | `pkg/executor`      | `Registry.Register`                           | Custom task executors and probers.                  |
| 2 | Channel type    | `pkg/prism/channel` | `channel.RegisterType`                        | Custom alert notification channel types.            |
| 3 | Telemetry       | `pkg/telemetry`     | `ListenerRegistry.RegisterProtocol` / `ProcessorRegistry.Register` | Passive protocol listeners and data processors. |
| 4 | API plugin      | `pkg/api`           | `Server.RegisterPlugin`                       | Custom routes, middleware, lifecycle hooks.         |
| 5 | Storage driver  | `pkg/db`            | `db.Register`                                 | Custom database drivers.                            |

> Auth extensions (SSO providers, permission validators, tenant resolvers) are also exposed as SPIs in `pkg/auth` for downstream repositories that need multi-tenant or SSO capabilities. The open-source edition ships a single-tenant, single-admin default.

---

## Executor extension

Inject a custom task executor so the scheduler can dispatch extended task types. The open-source edition ships `local`, `webhook`, `http`, `tcp`, `icmp`, and `mqtt_probe` executors.

**Kernel package**: `pkg/executor` · **Registration**: `registry.Register(&MyExecutor{})`

Implement the `Executor` interface — `Name()` (unique type identifier), `Capabilities()` (a `CapProbe`/`CapExec` bitmask; task creation rejects types without write capability and active monitor points reject types without probe capability), and `Execute(ctx, ExecutionRequest)`. `Registry.Register` returns an error on a duplicate `Name()`.

```go
package myssh

type Executor struct{}

func (e *Executor) Name() string { return "ssh" }
func (e *Executor) Capabilities() executor.Capability {
    return executor.CapProbe | executor.CapExec
}
func (e *Executor) Execute(ctx context.Context, req executor.ExecutionRequest) (*executor.Result, error) {
    // parse req.ExecutorConfig, run the SSH command, return the result
    return &executor.Result{ /* ... */ }, nil
}
```

```go
// in main(), before the runner starts
execRegistry.Register(&myssh.Executor{})
```

---

## Channel type extension

Inject a custom alert notification channel type. Channel instances are rows in the `sys_prism_channel` table; your type defines how a row's config JSON becomes a runtime sender.

**Kernel package**: `pkg/prism/channel` · **Registration**: `channel.RegisterType("sms", info)`

Provide a `TypeInfo`: `Build` turns the row's config JSON plus `BuildOptions` into an `alert.Channel` (`Name()`, `Send(ctx, Event)`); `Validate` (optional) checks config JSON at the API boundary; `SensitiveKeys` lists type-specific config keys to encrypt at rest and mask in responses; `TypeGuard` (optional) authorizes the type per request context (the licensing/feature hook). Type names are matched case-insensitively; a later registration overwrites an earlier one, so a downstream repository can replace a built-in type. The open-source composition root registers `webhook`, `email`, and the seven instant-messaging types (`dingtalk`, `discord`, `feishu`, `slack`, `teams`, `telegram`, `wecom`).

```go
func build(configJSON string, opts channel.BuildOptions) (alert.Channel, error) {
    var cfg smsConfig
    if err := sonic.UnmarshalString(configJSON, &cfg); err != nil {
        return nil, err
    }
    return &SMSChannel{cfg: cfg}, nil
}

func validate(configJSON string) error {
    var cfg smsConfig
    return sonic.UnmarshalString(configJSON, &cfg) // plus field checks
}

// in main(), before the channel service loads rows
channel.RegisterType("sms", channel.TypeInfo{
    Build:         build,
    Validate:      validate,
    SensitiveKeys: []string{"apikey"},
})
```

---

## Telemetry extension

Inject passive protocol listeners (e.g. Syslog, SNMP, MQTT) and data processors. The open-source edition ships no `ProtocolListener` implementations — its passive ingestion is the HTTP report endpoint — plus the `device` and `task` processors.

**Kernel package**: `pkg/telemetry` · **Registration**: `listenerRegistry.RegisterProtocol(l)` / `processorRegistry.Register(p)`

A `ProtocolListener` (`Type()`, `Start(ctx, ingest)`, `Stop(ctx)`) binds its own protocol socket and invokes the ingest callback for each received message; a passive monitor point whose `Type` matches the listener's `Type()` carries that listener's configuration. A `Processor` (`Type() types.AssetType`, `Process(ctx, *Telemetry)`, `OnTimeout(ctx, assetID)`) handles one asset type's ingestion lifecycle. Both registries reject duplicate `Type()` registrations. The registries are constructed by the composition layer and passed to the telemetry engine; downstream repositories receive them through their own wiring.

```go
type SyslogListener struct{}

func (l *SyslogListener) Type() string { return "syslog" }
func (l *SyslogListener) Start(ctx context.Context, ingest func(context.Context, *telemetry.Telemetry)) error {
    // bind the syslog socket; for each received message call
    // ingest(ctx, &telemetry.Telemetry{ /* ... */ })
    return nil
}
func (l *SyslogListener) Stop(ctx context.Context) error { return nil }
```

```go
// in main(), before the telemetry engine starts
listenerRegistry.RegisterProtocol(&SyslogListener{})
```

---

## API plugin extension

Inject custom HTTP routes, global middleware, and start/stop lifecycle hooks. Useful for mounting additional business endpoints.

**Kernel package**: `pkg/api` · **Registration**: `server.RegisterPlugin(&MyPlugin{})`

Implement the `Plugin` interface (`Name()`, `RegisterRoutes(root *RouterGroup)`, `Middlewares()`, `OnStart()`, `OnStop()`). Plugin middleware runs after the built-in middleware chain. `OnStart` failure aborts startup; `OnStop` failure is logged but does not block shutdown. Register before `Server.Start()`.

```go
type Plugin struct{}

func (p *Plugin) Name() string { return "my-plugin" }
func (p *Plugin) RegisterRoutes(root *api.RouterGroup) {
    g := root.Group("/api/v1/my")
    g.GET("/status", p.status)
}
func (p *Plugin) Middlewares() []app.HandlerFunc { return nil }
func (p *Plugin) OnStart(ctx context.Context) error { return nil }
func (p *Plugin) OnStop(ctx context.Context) error  { return nil }
```

```go
// in main(), before Server.Start()
server.RegisterPlugin(&Plugin{})
```

---

## Storage driver extension

Inject a custom database driver so the downstream repository can use a backend the kernel does not ship. The open-source edition ships a SQLite driver; downstream editions register MySQL and PostgreSQL through this same entry point.

**Kernel package**: `pkg/db` · **Registration**: `db.Register("oracle", opener)`

Implement an `Opener` (`func(ctx context.Context, cfg db.Config) (*gorm.DB, error)`). Duplicate driver names return an error. Register before `db.Open` is called. The kernel's `AutoMigrate` only migrates core models; the downstream repository is responsible for migrating its own extension models.

```go
func opener(ctx context.Context, cfg db.Config) (*gorm.DB, error) {
    return gorm.Open(oracle.Open(cfg.DSN), &gorm.Config{})
}
```

```go
// in main(), before db.Open
db.Register("oracle", opener)
```

---

## Composition order

All SPI registrations must complete before the server starts. The recommended order, shown in the downstream `main()`:

1. **Storage driver** — so `db.Open` can resolve the driver.
2. **Auth extensions** — so the authz service and permission middleware observe downstream providers when the API starts.
3. **Channel types** — so the channel service finds them when it loads `sys_prism_channel` rows.
4. **Executors** — so the runner can dispatch extended task types.
5. **Telemetry listeners / processors** — so the telemetry engine can start them.
6. **API plugins** — before `Server.Start()` so routes and hooks are wired.

> The kernel's `internal/service` composition layer is the authoritative example; this guide only summarises the ordering constraints.

## Related documents

- [Architecture](./architecture.md) — layered architecture and three-module design.
- [Module boundaries](./module-boundary.md) — the rules that keep modules decoupled.
- [Configuration](./configuration.md) — every configuration field explained.
- [OpenAPI specification](./api/openapi.yaml) — REST API paths and schemas.
