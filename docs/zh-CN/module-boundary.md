# 模块边界

> 本中文文档仅供参考，请以英文文档为准。
> Chinese translation is for reference only; the English documentation is authoritative.

本文档记录了保持 scheduler、executor 与 telemetry 解耦的规则，以及下游仓库扩展内核的准则。

## 三模块解耦规则

scheduler、executor 与 telemetry 是相互独立的子系统。它们彼此从不互相导入，也从不调用彼此的方法。所有跨模块通信都通过事件总线流转。

| 规则 | 禁止事项 | 原因 |
|------|-----------|--------|
| M-01 | scheduler → executor 导入 | scheduler 仅发布 `TaskTriggered`；直接调用会耦合部署单元。 |
| M-02 | executor → scheduler 导入 | executor 仅发布 `TaskCompleted`；直接调用会形成环。 |
| M-03 | telemetry → scheduler 导入 | telemetry 完全解耦；它只能可选地发布 `StatusChange`。 |
| M-04 | telemetry → executor 导入 | 采集与执行是不同关注点，无直接通信。 |
| M-05 | scheduler → telemetry 导入 | scheduler 不感知 telemetry 的存在。 |

## 通信契约

| 方向                 | 事件              | 发布者     | 订阅者                |
|----------------------|-------------------|------------|-------------------|
| scheduler → executor | `TaskTriggered`   | scheduler  | executor          |
| executor → scheduler | `TaskCompleted`   | executor   | scheduler         |
| telemetry → scheduler | `StatusChange`   | telemetry  | scheduler（可选） |

telemetry 不订阅任何 scheduler 事件，这保证了它可以独立运行。

## 分层原则

### `pkg/` 是公开实现层

`pkg/` 是内核对外暴露的唯一编程面。它持有每个 SPI 接口、每个共享数据类型、每个核心引擎实现、每个 GORM 模型、每个注册表与每个哨兵错误。下游仓库直接从 `pkg/` 导入——不存在桥接变量或包装类型。

### `cmd/` 是二进制入口层

`cmd/tickraft` 装配 CLI：参数解析、子命令分发、依赖注入与服务启动顺序。它不定义任何公开 SPI，也不包含业务逻辑。它不得被 `pkg/` 或测试导入。

### 依赖方向

- `cmd/` → `pkg/` —— 单向，入口层使用公开包。
- `tests/` → `pkg/` —— 单向，集成测试驱动公开 API。
- `pkg/` → `cmd/` 或 `pkg/` → `tests/` —— **禁止**。公开层绝不依赖入口层或测试层。
- 下游 → `pkg/` —— 允许，下游仓库导入公开类型并注册 SPI 实现。
- 下游 → `cmd/` 或 `tests/` —— **禁止**。下游仓库所需的代码必须位于 `pkg/`。

### 扩展模型

下游仓库从 `pkg/` 导入公开类型，并通过 [扩展指南](./extension-guide.md) 中记录的 SPI 注册表注册其实现。它不得修改内核源文件。当内核找不到已注册的实现时，会回退到开源默认实现，因此内核始终可以独立运行。

## HTTP Handler 归属：分层双轨

全工作区 HTTP handler 归属遵循 Go `pkg/`（公共库）与 `internal/`（私有应用）的原生语义分层：

### pkg/ 公共库层 → 集中式

- 所有 HTTP handler 统一在 `pkg/api/handler/`；中间件统一在 `pkg/api/middleware/`；路由组合根统一在 `pkg/api/router`（`RegisterRoutes` + `RegisterOption` 选项集）。
- 业务包（`pkg/auth`、`pkg/task`、`pkg/prism/*`、`pkg/asset`、`pkg/executor` 等）禁止 import `cloudwego/hertz`、`net/http`。
- 豁免：仅引用 `net/http` 的 `http.Status*` 状态码常量（用于 `errdefs.ServiceError` 构造错误映射）不算传输层耦合，允许导入；除此之外的任何使用（`http.Request`/`http.ResponseWriter`/`http.Client` 等）仍被禁止。
- 理由：`pkg/` 被跨仓导入（下游仓库均 import tickraft/pkg/*），必须传输层无关、可独立单测；状态码常量是纯量值，不引入对传输类型的依赖。

### internal/ 应用层 → 分布式 package-by-feature

- 每个业务包内 `handler.go` + `routes.go` 高内聚，handler 直接调用同包 Service。
- 版次特有的路由（插件、许可等）留在各仓 `internal/`，通过 `RegisterOption` 注入共享组合根；本仓 `internal/` 只做装配（cli / service / quota / web），不设独立 router。
- 理由：`internal/` 外部不可导入，高内聚 > 传输层解耦；这是各下游仓库的既成惯例。

分层判定（两仓都用→pkg、仅单仓→internal、版次差异→注入缝）与新增包决策树、死代码处置流程见 [Architecture](./architecture.md)。


## 相关文档

- [架构设计](./architecture.md) —— 分层架构与三模块设计。
- [扩展指南](./extension-guide.md) —— 每个 SPI 扩展点及其注册方式。
