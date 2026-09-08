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

| # | Extension point | Kernel package | Registration entry | Purpose |
|---|-----------------|---------------------|-----------------------------------------------|------------------------------------------------------|
| 1 | Executor        | `pkg/executor`      | `Registry.Register`                           | 自定义任务 executor 与探测器。                      |
| 2 | Channel type    | `pkg/prism/channel` | `channel.RegisterType`                        | 自定义告警通知渠道类型。                            |
| 3 | Telemetry       | `pkg/telemetry`     | `ListenerRegistry.RegisterProtocol` / `ProcessorRegistry.Register` | 被动协议 listener 与数据 processor。 |
| 4 | API plugin      | `pkg/api`           | `Server.RegisterPlugin`                       | 自定义路由、中间件、生命周期钩子。         |
| 5 | Storage driver  | `pkg/db`            | `db.Register`                                 | 自定义数据库驱动。                            |

> 鉴权扩展（SSO provider、权限校验器、租户解析器）同样作为 SPI 暴露在 `pkg/auth` 中，供需要多租户或 SSO 能力的下游仓库使用。开源版默认提供单租户、单管理员实现。

---

## Executor 扩展

注入自定义任务 executor，使 scheduler 能够触发扩展的任务类型。开源版内置 `local`、`webhook`、`http`、`tcp`、`icmp` 与 `mqtt_probe` executor。

**内核包**：`pkg/executor` · **注册方式**：`Registry.Register(&MyExecutor{})`

实现 `Executor` 接口 —— `Name()`（唯一的类型标识）、`Capabilities()`（`CapProbe`/`CapExec` 能力位掩码；创建任务时会拒绝无写能力的类型，创建主动探测点会拒绝无探测能力的类型）、`Execute(ctx, ExecutionRequest)`。当 `Name()` 重复时，`Registry.Register` 会返回错误。

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

## Channel 扩展

注入自定义告警通知渠道类型。渠道实例是 `sys_prism_channel` 表中的行；你的类型定义该行 config JSON 到运行时发送器的构造方式。

**内核包**：`pkg/prism/channel` · **注册方式**：`channel.RegisterType("sms", info)`

提供 `TypeInfo`：`Build` 将行内 config JSON 加 `BuildOptions` 构造为 `alert.Channel`（`Name()`、`Send(ctx, Event)`）；`Validate`（可选）在 API 边界校验 config JSON；`SensitiveKeys` 声明类型专属的需静态加密并在响应中掩码的配置键；`TypeGuard`（可选）按请求上下文对类型鉴权（授权/特性钩子）。类型名不区分大小写；后注册的会覆盖先注册的，因此下游仓库可以替换内置类型。开源版装配根注册 `webhook`、`email` 与七个 IM 渠道（`dingtalk`、`discord`、`feishu`、`slack`、`teams`、`telegram`、`wecom`）。

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

## Telemetry 扩展

注入被动协议 listener（例如 Syslog、SNMP、MQTT）与数据 processor。开源版不内置任何 `ProtocolListener` 实现 —— 其被动接收走 HTTP 上报端点 —— 并内置 `device` 与 `task` 两个 processor。

**内核包**：`pkg/telemetry` · **注册方式**：`listenerRegistry.RegisterProtocol(l)` / `processorRegistry.Register(p)`

`ProtocolListener`（`Type()`、`Start(ctx, ingest)`、`Stop(ctx)`）自建协议 socket 并对每条收到的消息调用 ingest 回调；`Type` 与该 listener 匹配的被动监控点承载其配置。`Processor`（`Type() types.AssetType`、`Process(ctx, *Telemetry)`、`OnTimeout(ctx, assetID)`）处理一种资产类型的接收生命周期。两个 registry 都拒绝 `Type()` 重复注册。registry 由装配层构造并传入 telemetry 引擎；下游仓库经自己的接线获得它们。

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

## API plugin 扩展

注入自定义 HTTP 路由、全局中间件以及启动/停止生命周期钩子。适用于挂载额外的业务端点。

**内核包**：`pkg/api` · **注册方式**：`server.RegisterPlugin(&MyPlugin{})`

实现 `Plugin` 接口（`Name()`、`RegisterRoutes()`、`Middlewares()`、`OnStart()`、`OnStop()`）。plugin 中间件在内置中间件链之后执行。`OnStart` 失败会中止启动；`OnStop` 失败仅记录日志，不阻塞关闭。需在 `Server.Start()` 之前注册。

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
// in main()
server.RegisterPlugin(&Plugin{})
```

---

## 存储驱动扩展

注入自定义数据库驱动，使下游仓库可使用内核未提供的后端。开源版内置 SQLite 驱动；下游版本通过同一入口注册 MySQL 与 PostgreSQL。

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
3. **Channel 类型** — 使渠道服务加载 `sys_prism_channel` 行时能找到它们。
4. **Executor** — 使 runner 能够调度扩展的任务类型。
5. **Telemetry listener / processor** — 使 telemetry 引擎能够启动它们。
6. **API plugin** — 在 `Server.Start()` 之前，以便路由与钩子就位。

> 内核的 `internal/service` 装配层是权威的组装示例；本指南仅概述顺序约束。

## 相关文档

- [架构](./architecture.md) — 分层架构与三模块设计。
- [模块边界](./module-boundary.md) — 保持模块解耦的规则。
- [配置](./configuration.md) — 每个配置字段的详细说明。
- [OpenAPI 规范](../api/openapi.yaml) — REST API 路径与 schema。
