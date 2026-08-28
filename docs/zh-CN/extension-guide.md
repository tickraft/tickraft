# 扩展指南

> 本中文文档仅供参考，请以英文文档为准。
> Chinese translation is for reference only; the English documentation is authoritative.

本指南面向基于 Tickraft 内核构建下游仓库的开发者。它描述了 `pkg/` 层暴露的每一个 Service Provider Interface (SPI)，以及如何在启动时注册自定义实现而无需修改内核源码。

## 双仓架构

Tickraft 采用"开源内核 + 下游扩展"的布局：

- **tickraft（内核仓库）** — 模块路径 `github.com/tickraft/tickraft`。提供通用内核与完整可运行的开源产品。它不依赖任何下游仓库，可独立编译。
- **下游仓库** — 基于内核构建。仅做增量扩展，不重复实现内核逻辑，也不修改内核源文件。

### 单向依赖原则

仅下游仓库可依赖内核。内核绝不导入、引用或感知任何下游代码、配置或数据结构。具体而言：

- 下游仓库仅导入内核 `pkg/` 目录下的公开包。
- 两个仓库均为独立的 Go module。禁止跨仓相对路径引用，禁止复制对方代码修改复用。
- 下游仓库不得修改内核 `pkg/` 下的任何文件来实现功能。

### SPI 注入理念

每一项扩展能力都表达为内核 `pkg/` 层定义的接口。下游仓库实现该接口，并在其 `main()` 初始化序列中注册实现。内核仅持有接口引用，在运行时回调到实现中。

| 特性 | 描述 |
|-----------------------|-----------------------------------------------------------------------------|
| Interface in kernel   | SPI 接口与注册入口位于内核的某个 `pkg/` 包中。   |
| Register at startup   | 下游 `main()` 在服务启动前调用注册函数。 |
| Kernel is unaware     | 内核绝不导入下游代码，仅调用接口。    |
| Graceful degradation  | 未注入实现时，内核回退到开源默认实现（通常为内存实现或全部拒绝），因此内核可独立运行。 |

## SPI 全景

| # | Extension point | Kernel package   | Registration entry                              | Purpose                                        |
|---|-----------------|------------------|-------------------------------------------------|------------------------------------------------|
| 1 | Executor        | `pkg/executor`   | `Registry.Register`                             | 自定义任务 executor（例如 SSH、MySQL）。       |
| 2 | Channel         | `pkg/prism/channel` | `channel.Register`                           | 自定义告警通知渠道。                            |
| 3 | Telemetry       | `pkg/telemetry`  | `ListenerRegistry.Register` / `ProcessorRegistry.Register` | 被动 listener 与数据 processor。 |
| 4 | API plugin      | `pkg/api`        | `Server.RegisterPlugin`                         | 自定义路由、中间件、生命周期钩子。              |
| 5 | CLI subcommand  | `pkg/cli`        | `cli.RegisterCmd`                               | 自定义 CLI 子命令。                             |
| 6 | Storage driver  | `pkg/db`         | `db.Register`                                   | 自定义数据库驱动。                              |

> 鉴权扩展（SSO provider、权限校验器、租户解析器）同样作为 SPI 暴露在 `pkg/auth` 中，供需要多租户或 SSO 能力的下游仓库使用。开源版默认提供单租户、单管理员实现。

---

## Executor 扩展

注入自定义任务 executor，使 scheduler 能够触发扩展的任务类型。开源版内置 `local`、`webhook`、`http`、`tcp`、`icmp`、`udp` 与 `dns` executor。

**内核包**：`pkg/executor` · **注册方式**：`Registry.Register(&MyExecutor{})`

实现 `Executor` 接口（`Type()`、`Category()`、`Role()`、`Execute()`），并在 runner 启动前注册。当 `Type()` 重复时，`Registry.Register` 会返回错误。

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

## Channel 扩展

注入自定义告警通知 channel。开源版内置 `webhook` channel。

**内核包**：`pkg/prism/channel` · **注册方式**：`channel.Register("slack", factory)`

实现一个 `ChannelFactory`，读取 `channel.Config` 并返回 `alert.Channel`（`Name()`、`Send()`）。类型名不区分大小写；后注册的会覆盖先注册的，因此下游仓库可以替换内置的 `webhook`。需在 `LoadChannels` 调用前注册。

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

## Telemetry 扩展

注入被动 listener（例如 Syslog、SNMP trap、MQTT）与数据 processor。开源版内置 HTTP listener 与设备/任务 processor。

**内核包**：`pkg/telemetry` · **注册方式**：`ListenerRegistry.Register` / `ProcessorRegistry.Register`

`Listener`（`Type()`、`Start()`、`Stop()`）接收外部数据并转发给接收回调。`Processor`（`Type()`、`Process()`、`OnTimeout()`）处理特定的资产类型。`Type()` 重复注册会返回错误。registry 由 telemetry manager 构造并注入到下游仓库。

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

## API plugin 扩展

注入自定义 HTTP 路由、全局中间件以及启动/停止生命周期钩子。适用于挂载额外的业务端点。

**内核包**：`pkg/api` · **注册方式**：`server.RegisterPlugin(&MyPlugin{})`

实现 `Plugin` 接口（`Name()`、`RegisterRoutes()`、`Middlewares()`、`OnStart()`、`OnStop()`）。plugin 中间件在内置中间件链之后执行。`OnStart` 失败会中止启动；`OnStop` 失败仅记录日志，不阻塞关闭。需在 `Server.Start()` 之前注册。

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

## CLI 子命令扩展

注入自定义 CLI 子命令，使下游二进制可在开源子命令旁挂载扩展命令。

**内核包**：`pkg/cli` · **注册方式**：`cli.RegisterCmd(cmd)`

构建一个 `*cobra.Command`，并在根命令构造之前注册。内核根命令会调用 `cli.GetRegisteredCmds()` 聚合所有已注册的子命令。建议在 `main()` 中显式调用注册函数，而非依赖 `init()` 的副作用。

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

## 存储驱动扩展

注入自定义数据库驱动，使下游仓库可使用内核未提供的后端。开源版内置 SQLite 驱动。

**内核包**：`pkg/db` · **注册方式**：`db.Register("oracle", opener)`

实现一个返回 `*gorm.DB` 的 `Opener`。驱动名重复会返回错误。需在 `db.Open` 调用前注册。内核的 `AutoMigrate` 仅迁移核心模型；下游仓库需自行迁移其扩展模型。

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

## 组装顺序

所有 SPI 注册必须在服务启动前完成。推荐的顺序如下，以下游 `main()` 为例：

1. **存储驱动** — 使 `db.Open` 能够解析到该驱动。
2. **鉴权扩展** — 使鉴权服务与权限中间件在 API 启动时能感知到下游 provider。
3. **Channel factory** — 使 `LoadChannels` 在配置加载阶段能找到它们。
4. **Executor** — 使 runner 能够调度扩展的任务类型。
5. **Telemetry listener / processor** — 使 telemetry manager 能够启动它们。
6. **CLI 子命令** — 在根命令构造之前。
7. **API plugin** — 在 `Server.Start()` 之前，以便路由与钩子就位。

> 内核的 `main.go` 是权威的组装示例；本指南仅概述顺序约束。

## 相关文档

- [架构](./architecture.md) — 分层架构与三模块设计。
- [模块边界](./module-boundary.md) — 保持模块解耦的规则。
- [配置](./configuration.md) — 每个配置字段的详细说明。
- [OpenAPI 规范](../api/openapi.yaml) — REST API 路径与 schema。
