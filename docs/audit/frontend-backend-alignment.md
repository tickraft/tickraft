# 前后端对齐审计:定时任务 / 主动探测 / 被动探测(Frontend-Backend Alignment Audit)

> 状态:**批次 1–4 已全部交付(2026-08-26),验证全绿**。本文档记录 2026-08-25 对 `pkg/executor`、`pkg/telemetry`、`pkg/task` 三域后端与 `web/` 前端(定时任务、主动探测、被动探测)的联合评估结论、用户决策与实施批次。
> 关联文档:`docs/model-layering-design.md`(Tier 3 task 域)、`docs/rule-engine-design.md`(§6.3 判定表达式链路)、三域后端评估(本文 §2)。

---

## 1. 评估范围与方法

- 后端:`pkg/executor`(执行器 SPI 与五个内置实现)、`pkg/telemetry`(被动管线 + ProberService)、`pkg/task`(调度任务域)、`pkg/event`(事件总线消费模型)。
- 前端:`web/packages/features/src` 的 task 与 telemetry 模块(views/api/routes/types/i18n/mock)、`web/packages/core`(request/WS)、api-contract 测试。
- 方法:逐端点/逐字段契约比对(以代码为准,不以旧模块文档为准),关键结论逐一在源码定位复核。

## 2. 后端三域评估结论(摘要)

架构与设计文档一致:三模块经事件总线解耦、operation 分流记录(probe → `sys_probe_record`,execute → `sys_schedule_log`)、先落库后发完成事件、能力位预检、Tier 3 单模型均正确落地。发现的问题按严重度:

| 编号 | 级别 | 问题 | 位置 |
|---|---|---|---|
| B1 | P0 | `trigger()` 硬编码 `TriggerType="schedule"`;手动触发另插永不更新的 running 占位行(双行) | `pkg/task/events.go:135`、`internal/api/service/scheduler/task.go:250` |
| A1 | P0 | 被动资产超时离线检测全链路死代码:`Collector.RegisterAsset` 零调用,timeout wheel 空转,`MarkOffline` 永不触发 | `pkg/telemetry/telemetry.go:25`、`state.go:62` |
| C1/C2 | P1 | 探针任务未设 Concurrency(0=无限制)可自我重叠;事件路径绕过并发闸 | `prober_service.go:212`、`task/events.go:101` |
| C3 | P1 | 超时来源不统一:tcp/icmp/webhook/local 各持内部超时层,与"ctx 单一来源"意图相悖 | 各执行器实现 |
| D1 | P1 | 逐探针事务写放大:status 不变也 UPDATE | `probe_record.go:135` |
| D2 | 决策待定 | 事件总线 per-Type 串行消费 + runner CallerRuns 内联 → 队头阻塞风险 | `channel_bus.go:271`、`runner.go:409` |
| B3 | P1 | exit code 断流:local 统一的退出码不进记录/事件 | `executor/types.go` |
| E | P2 | 死代码:`task.listTasks`、`ExecutionStore.List`、`SkipReason/Metrics` 列、scheduler once/immediate 原语、event 虚常量等 | 见 §6 |

## 3. 前端评估结论

### 3.1 契约总况

- 任务域前端 wire 契约已对齐 Tier 3(`executor_type`、单一 `schedule`、`timeout/max_retries/retry_interval`),无 `once`/`schedule_type`/`cron_expr` 发送侧残留(表单态字段在提交前剥离)。
- 前端调用的所有端点均真实存在;后端存在少量前端未用端点(template 单查/建改、assets probe 等)。
- 扩展版前端(tickraft-x/web)任务与遥测模块仍停留在 pre-Tier3 旧契约(`once/executeAt/executorConfig`、`GET /tasks/logs`),需随批次 4 迁移。

### 3.2 功能性缺陷(必修)

1. **探针历史/日志状态词汇错配**:后端返回资产词汇 `normal/abnormal/offline/unknown`,前端只认 `success/error` → 历史状态全灰;`logLevelType` default 分支返回 success → **探测失败(abnormal)显示绿色**(`views/telemetry/monitor/detail/Detail.vue:128-135、491`)。
2. **任务详情页统计是全局数据冒充单任务数据**:`getExecutionStats()` 无 taskID(后端也不支持),却展示为该任务的 Total/成功率/平均耗时。
3. **Probe Now 是假按钮**:后端 `ProbeMonitor` 仅返回已存状态(`pkg/api/handler/telemetry/handler.go:325`),前端却提示"探测成功"。
4. **模板分类过滤失效**:前端选项 `probe/monitor`,后端真实分类 `web/network/database/security` → 选择即查空。
5. **被动点 history 的 Status 列被复用为指标名**(`handler.go:317`),语义混乱。
6. **udp 是幽灵探测项**:后端 `ceProberTypes` 硬编码含 udp,但运行时无 udp 执行器 → 创建必 400;前端亦无 udp 表单。
7. **webhook per-point "Auth Method/Signing Secret" 是死设置**:listener 只读服务端全局 `WithSecret`,点配置中的 secret 无人消费;且无任何 UI 告知上报端点与认证方式。

### 3.3 展示缺口(有真实数据未展示)

- `MonitorStatus.last_probe_at`/`latency_ms` 后端已返回,前端未展示。
- 监控点 `status` 列在列表页缺失(仅 enabled 开关)。
- summary chips 只统计当前页行数却当全局计数。
- 资产选择器固定 `size=100`,超出不可选。

### 3.4 死代码清单(前端)

- WS 层:`useWebSocket`(core)零调用;`types/event.d.ts` 事件词汇与后端**零交集**(如 `asset.status_change` vs 实际 `asset.status_changed`);vite `/ws` proxy 无消费者。
- i18n 死键:task.json 约 100/285 键、telemetry.json 约 349/479 键(`telemetry.prober.*`/`telemetry.listener.*` 为已删旧页面化石)。
- 死 API 导出:`createTemplate/updateTemplate/getTemplate/getBuiltinTemplates/getChannel/probeAsset/updateProfile/healthCheck`;telemetry.ts 与 asset.ts 重复的 `getAssets`。
- `ExecutorType` 死枚举成员(telnet/snmp/notify)。
- 后端 WS 转发的 22 个事件类型中 13 个永不发布(`task.*`、`execution.started/progressed`、`alert.*`、`asset.fault_detected`)。

### 3.5 伪造 UI(用户决策:补全真实实现,不删不造假)

- 日志详情 "Worker Node: —" 恒空、"Worker Pickup ~1ms" 假时间线、Environment 永久占位 tab。
- "timeout" 状态卡片/分支:后端永不产生(超时记为 failed)→ 本轮使后端真实区分 timeout。
- dashboard "Task Execution Trend" 图永远空(数据源从未接入)。

## 4. 用户决策记录(2026-08-25)

| 决策点 | 结论 |
|---|---|
| WS 实时层(两端死代码) | **暂不动**,仅本文档记录现状 |
| i18n 死键 + 死 API 导出 + 后端 udp 目录项 | **批准删除** |
| 伪造 UI | **不删除,补全为真实实现**(不造假数据),扩展版相关功能同步更新 |
| Probe Now | **后端实现真探测** |
| webhook per-point secret | **后端实现 per-point secret** |

## 5. 实施批次

- **批次 1(P0 正确性)**:trigger_type 真实来源 + 删占位行 + wire 放开;被动离线检测接线;timeout 状态真实化;stats 支持 `task_id`;前端状态词汇映射、模板分类、history metric 字段。
- **批次 2(功能补全)**:真探测;per-point secret;执行上下文真实化(node/triggered_at/exit_code);stats `days` 趋势序列;monitors summary + status 列 + 资产远程搜索 + 上报引导;执行器枚举真实化(probers 从注册表派生、新增 `GET /executors`);P1 项(Concurrency=1、事件路径并发闸、超时单一来源收敛、条件更新、RecordStore ctx、http 连接复用)。
- **批次 3(死代码清理,仅批准项)**:§3.4 所列(保留上报引导要复用的 `telemetry.listener.webhook.*` 词汇);后端 `ceProberTypes` 随注册表派生整体消失。
- **批次 4(扩展版同步)**:tickraft-x 后端联动(签名变更同步、per-point secret/真探测/stats 扩展核对、udp 保留);x 前端任务/遥测模块契约迁移。

## 6. 遗留与待决策

- **D2 事件总线队头阻塞**:per-Type 串行 + CallerRuns 内联,worker 饱和时阻塞同类型全部事件。候选:(a) runner 订阅方只投内部有界队列、满则丢弃+计数;(b) 总线支持 per-subscription 并发;(c) 维持现状。本轮不动,待单独决策。
- **WS task.\* 生命周期事件**:补发布 vs 修剪死订阅,随 WS 层决策一并处理。
- 后端其余死代码(§2 E 项:task 域死函数、scheduler once/immediate 原语、event 虚常量)未在本轮批准范围,留待后续减法批次。
- **x 四类 mock-fed 页面待后端**:tickraft-x 的 metric 仪表盘 / log 检索 / probe 记录 / telemetry resources 页面仍由 dev-mock 供给(见 §7.5),CE 与 x 均无对应后端;正式启用前需先设计并实现数据面端点。
- **x 旧 telemetry service 删除已批准**:`tickraft-x/internal/api/service_telemetry.go` 零调用(被共享 telemetry 服务替代),随服务层下沉批次(§7.7)一并删除,决策见架构优化计划(2026-08-26)。
- **内核路由无 404 catch-all(两端共有)**:未知 URL 整页空白(布局挂在路由记录上,无匹配即无渲染)。是否补 catch-all/重定向路由待决策。

## 7. 实施记录(随批追加)

### 7.1 批次 1 附加发现与修复

- **GORM 列默认值吞掉 `enabled=false`**(生产级 bug):`monitor_points.enabled` 带 `default:true` 标签,INSERT 零值 bool 被 GORM 替换为默认值 → 创建禁用监控点实际落库为启用,且与离线检测注册状态不一致。修复:去掉列 default(task 域 `Enabled` 同款先例,`pkg/telemetry/model.go`)。
- timeout 状态贯通:runner 判定 `context.DeadlineExceeded` → `ExecutionRecord.TimedOut`(与 Status 正交)→ adapter 存储时翻译为执行词表 `timeout`;`GET /tasks/stats` 增 `task_id` 过滤。
- 被动离线检测接线:`Engine.Start` 引导存量资产注册(`registerPassiveTimeouts`),CRUD 钩子经 `SyncAssetObservation` 按 mode 分流;首报自动注册与 `MarkOffline` 去重沿用既有实现。
- Probe Now 真探测:`ProberService.ProbeNow` 经 `task.Manager.Schedule` 立即点火(含启动竞态 re-register 重试);service 校验 active+enabled,handler 返回 202;前端 toast 语义改为"已触发"并延迟轮询刷新。
- 被动点 history 增显式 `metric` 字段,Status 列不再复用为指标名。

### 7.2 webhook 认证模型(per-point secret 落地后)

统一端点 `POST /api/v1/telemetry` 的认证链(`pkg/telemetry/http/listener.go`),凭证优先级:

1. **per-point HMAC**:passive 点 `config.secret`(`authType` 为 `hmac` 或缺省)注册进内存 `SecretRegistry`(启动 `LoadSecrets` 自 MonitorStore 加载,点 CRUD 钩子 `SetPoint`/`RemovePoint` 增量维护,secret 变更即替换、禁用/删除即摘除)。请求携带 `X-Tickraft-Signature`(hex HMAC-SHA256 of raw body)时先遍历注册表验签;命中者将上报**绑定到所属点的资产**——请求可省略资产身份,显式 `asset_id` 指向其他资产则 403。
2. **全局 HMAC 后备**:per-point 未命中时按 listener 级 `WithSecret` 验签(原有行为)。配置了全局 secret 时未签名请求一律 401。
3. **asset-key**:无签名且未配全局 secret 时,按 `asset_id`(或 `asset_key`+`tenant_id`)解析资产,不存在则 404(原有行为)。

约束:呈现的签名匹配不到任何凭证必拒(空钥 HMAC 可被任意计算,`verifySignature` 对空 secret 直接失败);无资产绑定的点其 secret 等同全局信任级;`authType: "asset-key"` 的点 secret 不注册,走 asset-key 路径。表单 secret/authType 字段自本批起为真实生效配置。

### 7.3 批次 2 补充记录:表单配置词表与执行器枚举

- **TaskForm 配置词表 bug(生产级)**:表单原按旧词汇组键发送 `url/host/interpreter/source/headers(字符串)`,而执行器请求结构体实际键为 `address/command/args/headers(map)` → 创建的 http/tcp/icmp/local 任务配置后端全部解析为零值。修复:`buildExecutorConfig` 按执行器请求结构体标签组键;local 的 args 以单行编辑、提交时拆为字符串数组;headers 以 "key: value" 多行编辑、提交时组为对象(`views/task/task/components/TaskForm.vue`)。
- **humps 递归约束(契约约定)**:请求拦截器对整个 body 递归 decamelize(`web/packages/core/src/utils/naming.ts`),嵌套 `config` 内的复合驼峰键(如 `timeoutMs`)上线即变 `timeout_ms`。前端 config 键必须使用转换稳定键:单词键(`address`/`command`)或与后端 JSON 标签一致的书写。
- **执行器枚举真实化**:新增 `GET /executors`(CapExec 能力位);probers 元数据改注册表派生。TaskForm 执行器卡片动态加载注册表(名称回退 curated i18n),注册表未知类型回退 raw JSON config 编辑;日志筛选下拉同步动态化。
- **超时单一来源收敛落地**:tcp/icmp/webhook/local 执行器内部超时层降级为 `deadline.Fallback` —— 仅当调用方 ctx 无 deadline 时兜底;任务 `TimeoutSeconds` 由 runner 生命周期统一设置 ctx deadline,执行器不再可能缩短已配置的任务超时(`pkg/executor/tcp/tcp.go:146` 为代表)。

### 7.4 批次 3 清理记录(仅批准项)

- 前端 i18n 死键:telemetry.json 删 ~349 键,顶层只剩 `asset/listener(仅 webhook)/monitor/prober` 真实词汇;task.json 删 ~100 键。`telemetry.listener.webhook.*` 上报引导词汇按决策保留并在批次 2 复用。
- 死 API 导出 8 个(createTemplate/updateTemplate/getTemplate/getBuiltinTemplates/getChannel/probeAsset/updateProfile/healthCheck)、telemetry.ts 与 asset.ts 重复的 `getAssets`、`ExecutorType` 死成员(telnet/snmp/notify)。
- 后端 udp 幽灵项:`ceProberTypes` 硬编码随注册表派生整体消失。
- WS 层(两端)按决策未动,现状仍在 §3.4/§6。

### 7.5 批次 4 扩展版同步记录(tickraft-x)

**task 模块契约迁移**
- createFull 表单删除无后端通道的 pre-Tier3 字段(once/executeAt/priority/asset 绑定/alert 配置等,经批准);提交组键对齐 Tier 3 契约(`executor_type`/单一 `schedule`/`timeout` 等)。
- ExecutorPicker 分区呈现:开源执行器动态枚举(openCards,注册表加载 + 静态兜底),扩展执行器独立分区(tierLocked,FeatureConstants 门控)。
- 执行日志与统计走 CE 真实端点(`GET /tasks/:id/executions`、`GET /tasks/stats?task_id=&days=`),不再调旧的 `GET /tasks/logs`。

**telemetry 模块对齐 CE 新契约**
- prober 列表页改真实 `GET /telemetry/monitors?mode=active` 契约:分页、启停(`PUT :id/enable|disable`)、立即探测(`POST :id/probe` 202 后延迟刷新行状态)、删除;伪造的 `/telemetry/probers` 分页端点弃用。
- listener 总览页改真实 `GET /telemetry/listeners` 注册表卡片 + 被动上报引导(上报端点 `/api/v1/telemetry`、per-point HMAC / asset-key 认证、可复制 curl 示例),复用内核 `telemetry.listener.webhook.*` 词汇;原各 listener 假配置表单/假测试连接/假开关删除。
- `api/telemetry.ts` 删 getListeners/testListener/getProbers/deleteProber 假端点及配套类型;**metric / log / probe-record / resource 四类保留 dev-mock —— 后端待建**(见 §6)。
- mock 层:`mock/task.ts` 删除;`mock/telemetry.ts` 删 listeners / listeners/:type/test / probers / probers/:id 条目,保留 metrics/logs/probes/resources。

**x i18n 基建修复(迁移中发现的真实缺陷,zh-Hans/en-US 合并路径)**
- `mergeTask` 原浅合并:x 的 task.task 块会整体替换内核 list/create/detail → 改为 task.task.* 一级深合并。
- prism / telemetry 命名空间同理:新增 `mergeSubkeys` 对撞键深合并(x telemetry 的 listener/prober 块曾整体覆盖内核 `listener.webhook.*` 与 prober 词汇)。
- x `prism.remediation.list` 与内核"自愈记录"词汇语义冲突 → 改名 `rules`(视图引用同步);补 `task.list.pause/resume`、`task.remediation.title`、`telemetry.prober.list.title`、`telemetry.listener.overview.title` 等路由/菜单键(×10 locale);listenerFull 死键 30 个/语言清理(保留 intro/configEmpty/managePoints)。

**link 共享删除风险(后续约束)**:x 经 pnpm link 消费 CE features 包,CE 侧删除导出会直接破坏 x 编译(本轮 `getListeners` 类型导出即为 x 需求新增)。后续 CE 减法批次删除任何导出前,必须先 grep tickraft-x 侧引用。

**验证(批次 4 收口,2026-08-26)**:双仓 `go build ./... && go test ./...` 全绿;golangci CE 默认 / x 默认 / x saas 标签均 0 告警;x web vue-tsc + eslint 0 错误;CE web vitest 67/67;tests/httpapi 新增断言 —— stats `task_id`(精确作用域)与 `days=3`(零填充日序列、结合 task_id 精确计数)及非法参数严格 400,probe 202 真探测 e2e(harness 按 `internal/service` 生产同款接线 `WithProbeTrigger`,断言探测记录落 telemetry 域且 sys_schedule_log 零残留、禁用点 400)。

### 7.6 x 后端接线与启动回归修复(2026-08-25)

**启动回归(生产级,x 全 server 模式)**:CE 将 telemetrySvc 纳入内核路由校验必填项(`pkg/api/handler/routes.go` validateRouteConfig),x 装配从未注入 → `tickraft-x start standalone` 启动即失败("required services not injected: telemetry service");x 测试经 stub 注入掩蔽,未能暴露。修复:CE `internal/api/service/telemetry` 整体提升为 `pkg/api/service/telemetry`(双仓依赖规则:x 仅消费 CE pkg/;该服务依赖本就全在 pkg 层),x 装配层注入完整内核 telemetry 服务,未授权 x 至此方可启动。

**接线结构(x 落点)**:
- `platform.Runtime` 新增 `ProberSvc` / `TelemetryCollector` / `ExecutorRegistry` 句柄,由 worker `StartEngines` 在 API server 起动前发布(standalone = 进内进程引擎;distributed server-only = nil)。
- worker engines:探测记录走 `probeOnlyRecordStore` —— 仅 `OpProbe` 落 `sys_probe_record` 域,`OpExecute` 留给 x 自有 result-writer(双写 `sys_schedule_log` 风险);ProberService + MonitorStore 接入 collector,`Start` 引导存量 active 点。
- 路由装配:`router.go` 透传 WithTelemetryService / ReportHandler / DataStores / ProbeRecords / ExecutorRegistry;`server/api.go` 构造模板库(pro 全集)、per-point secret registry(`LoadSecrets` 启动加载)、点 CRUD 钩子(被动 SetPoint/RemovePoint + SyncAssetObservation;active RegisterPoint/UnregisterPoint)、`WithProbeTrigger` + `WithExecutorValidator`(LookupWithOp OpProbe);collector 句柄非空时挂 webhook report handler(per-point HMAC 验签 + 审计)与 metric/log 数据面(bridge 结构性满足内核接口)。
- server-only distributed 模式句柄为 nil:telemetry 服务退化为 store-backed,Probe Now 返回 503 "prober service is not running",无 report handler —— 探测/上报归属 worker/collector 节点,语义正确。

**运行须知(x standalone 冒烟发现)**:sqlite 存储需 `-tags sqlite_json` 构建(mattn 默认构建缺 json1);未加载 license 不会播种 admin 用户(仅 `tenant.BootstrapFromLicense` 播种),冒烟需以 kernel `password.Hash` 手工 bcrypt 种子。

**冒烟结果(未授权模式)**:healthz 200;API 契约 curl 全量验证(monitors CRUD、probe 202 + 记录落库、enable/disable 门控 400、listeners=webhook、probers=http/icmp/tcp 即 FeatureGuard CE 集、summary);浏览器 5174:prober/list 真表格 + Probe Now toast + 后端 4 次 dispatch/6 条记录;listener/overview-full 真注册表卡片 + 上报引导(per-point secret curl 示例)完整渲染。附修:该页此前无任何菜单入口(仅 URL 可达),已在 x 扩展菜单补 `syslog_listener` 门控条目。

### 7.7 服务层下沉域包(批次 0+A,2026-08-26)

**结构变更(纯迁移,零行为变更)**
- 四个服务实现包 `pkg/api/service/{scheduler,prism,telemetry,system}` 解散;SPI+契约 DTO 自 `pkg/api/handler/<域>/types.go` 随实现迁入域包:`pkg/task/service`(scheduler 更名 task,消除与 `pkg/scheduler` 调度内核重名)、`pkg/prism/{alert,channel,remediation}/service`、`pkg/telemetry/service`、`pkg/system`(新建,Config+Service+实现)。
- handler/<域> 只留 HTTP 绑定;实现命名统一 `<域>Service`/`New<域>Service`,接口统一 `Service`。
- 服务级错误词表(ServiceError+七个哨兵+InnermostMessage)自 `pkg/api/handler/errors.go` 迁 `pkg/errdefs`(域包不反向依赖 api 层的前提);telemetry 两个哨兵随 SPI 入 `pkg/telemetry/service`。
- `pkg/api/service` 目录删除;双仓 ~25 文件 import 更新(x 副本暂存活,批次 B 删)。

**验证**:CE `go build/vet/test ./...` 全绿、golangci 0 告警、tests/httpapi 全量(-count=1,34.7s)通过;x build/vet/test 全绿、golangci default+saas 双标签 0 告警。已知遗留反向依赖一处(`pkg/telemetry/http` → `pkg/api/httputil`),随 httputil 收敛批次处理。
