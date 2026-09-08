# tickraft 双仓全面技术评估报告

- 日期：2026-08-28
- 范围：CE 开源仓 `tickraft`（Go 后端 + web/ 前端 pnpm workspace）与商业扩展仓 `tickraft-x`（后端 + web/ 前端），双仓全覆盖
- 方法：三路并行勘察（架构与完整性 / 代码质量 / 文档与集成）→ 关键严重项人工逐行复核 → 与 2026-08 既有审计记录（`docs/audit/frontend-backend-alignment.md` 等）交叉验证
- 路径约定：`CE xxx` 相对 tickraft 仓根；`X xxx` 相对 tickraft-x 仓根
- 严重程度：严重（发布阻断或核心功能失效）/ 中等（正确性、可维护性或文档可信度受损）/ 轻微（规范与打磨）
- 优先级：P0（发布前必须处理）/ P1（尽快）/ P2（择机）
- 统计：严重 8 项、中等 23 项、轻微 30+ 项；另附各维度「验证通过」清单

## 修复状态（2026-08-28 当日更新）

严重 8 项已经用户逐项批准并于当日全部修复，验证通过：

| 编号 | 修复内容 | 验证 |
| --- | --- | --- |
| C-1 | `pkg/telemetry` 新增 `superviseLoop`（recover + ctx 退出 + 1s 退避重启），process loop、聚合消费循环、`Aggregator.run` 三处全部纳入自愈；recoverPanic 失实注释一并改写 | 新增 `supervise_test.go` 两用例（panic 重启、ctx 退出不重启）；`go test ./pkg/telemetry/...` 全绿 |
| F-1 | 8 个 SaaS 视图（quota/subscription/billing/payment/pricing/upgrade/tenant-list/tenant-detail）全部接入 `api/saas.ts` 真实请求 + 加载/错误态；`api/saas.ts` 重写为唯一数据源（DTO 映射、分/元转换、404 语义）；后端无对应能力的 UI 块诚实降级（移除 PAYG 假价格表、租户 ownerEmail/usersCount/monthlyUsage、时间线/操作日志、重置密码/续费提醒等）；删除 `web/src/mock/saas.ts`；顺带修正 `internal/tenant/handler.go` 两处 PATCH→PUT 失实注释 | `vue-tsc --noEmit` 0 错；eslint/stylelint 0 告警；`go build ./...`（x）通过、`go test ./internal/tenant/...` 通过；10 个语言包新增/清理键后 JSON 校验通过 |
| D-1..D-4、D-15 | openapi.yaml：告警路径改 `/api/v1/prism/alert/*`；`msg`→`message`；header 改 `X-Tickraft-API-Key`；删除 6253 双端口描述与 server 条目、引用路径改 `pkg/telemetry/http/listener.go`；头部版本与 info.version 对齐（2.0.0 / 2026-08-28） | YAML 解析通过，42 paths 复核 |
| D-5、D-6 | x README：二进制名 `./bin/tickraft`、Node 22+/pnpm 9+、`cd web && pnpm install && pnpm dev`；x CI 前端 job 改 pnpm/action-setup v9 + node 22 + pnpm cache + `--frozen-lockfile` + type-check + build | 本地以 pnpm 重放同命令通过 |

中等（P1）23 项经用户四批次批准，已全部修复并验证（2026-08-28）：

| 批次 | 编号 | 修复内容 | 验证 |
| --- | --- | --- | --- |
| 1 | C-2..C-6、C-18 | 状态缓存先写后落库失败回滚、首状态补历史行、心跳超时竞态消除、ApplyTaskReport 乐观锁、sweeper status 索引、resp.Body 关闭等六项正确性修复 | 对应包单测新增/更新，`go test` 全绿 |
| 2 | C-12..C-15、C-17 | quota 别名删除、legacy 探针清理垫片删除、AfterFind 自愈钩子删除、x timeseries 迁移脚手架整体删除（含 migrate-tsdb CLI 与启动自动迁移）、web 调试脚本与 tsbuildinfo 清理 | 双仓 `go test ./...` 全绿 |
| 3 | D-7..D-14、A-2、D-17/N-3 | configuration.md 幻影配置/默认值/路由表修正、getting-started Node 22+/pnpm 9、web/README 版本表、openapi.yaml 补 16 路径 12 schema（58 paths 与 routes.go 程序化比对 0 缺失）、x pro-extensions.md 重写、x CI branches/run_lint/.gitignore、collector→telemetry 全量术语统一（17 文档+2 SVG+8 截图+.trae 规则）、审计文档勘误注记 | YAML 解析+路由比对通过；红线扫描重放通过 |
| 4 | F-2 | lease_k8s.go 死代码删除 + ErrK8sLeaseNotConfigured/文档注释残留清理 | `go test ./internal/license/` 通过 |
| 4 | F-6 | license 审计：生产装配（buildLicenseHandler）在 TICKRAFT_LICENSE_PATH 设置时显式注入加密 AuditLogger（license-audit.log 与许可证同目录、serial 派生密钥）；recordAudit 无 sink 时打 Warn；死代码 noopAuditLogger/NewNoopAuditLogger 删除 | `go build ./...`、`go test ./internal/license/ ./internal/api/` 全绿 |
| 4 | F-13 | x 新增 Redis 令牌桶限流器（Lua 原子脚本：双窗口填充/扣减/容量变更重置/2×窗口 TTL）；Plugin 持有限流器（原包级单例删除）；组合根 buildRateLimiter 按 `rt.Redis != nil && !rt.RedisCacheOnly` 选择（与租约基础设施同一判据），单节点保持内存版 | miniredis 10 用例（跨副本共享预算、日限额封顶分钟、容量变更重置、Redis 故障 fail-open 等）+ 既有中间件/api 测试全绿 |
| 4 | U-1 | 前端拦截器错误中文化：CE 新增 `apiErrorMessages.ts`（错误码表+精确消息表+「X is required」「invalid X」两类句式规则，zh locale 生效、en 透传）接入 request.ts 全部 4 个 reject 点；x ops 控制台同构 compact 版（含 license 域消息） | request.test.ts 新增 5 用例（14/14 通过）；全量 vitest 72/72 |
| 4 | N-1 | 双仓 143 行注释设计文档章节引用中性化（Go/TS/Vue；RFC 标准引用保留；生成文件豁免）；N-2 一并处理 | 残留扫描仅剩 RFC 引用；人工复核无残句 |

### 批次 4 与原建议的偏差说明

- **F-2**：原建议「启动时拒绝 lease.mode=k8s」——经核查**不存在任何 lease.mode 配置**（K8sConfig 是 ops 子服务选主，与许可租约无关），实际处置为按未发布减法政策删除死代码 stub。
- **F-6**：审计前提部分失实——`noopAuditLogger` 全仓零调用、`WithLicenseDeps` 生产零调用，真实缺陷是「生产端点（ImportLicense/VerifyLicense/RequestCode 等）调用 recordAudit 时因 licenseDeps 恒空而静默丢审计」。修复按批准方向（装配注入+Warn）落地；另按减法政策删除了零引用的 noop 实现。
- **F-13**：按「部署模式选择」实现时复用租约基础设施的判据（Redis 非仅缓存），未新增配置项；限流器从包级单例改为 Plugin 字段（Plugin 每进程仅构造一次，语义等价、可注入性更好）。
- **C-15（批次 2 补记）**：Operation 严格化落地为「发布方缺省」而非「消费方兼容分支」——task.Engine 的 trigger() 将空 operation 缺省为 execute，prober 经 Task.Operation 运行时字段注入 probe；消费端 runner.dispatch 严格拒绝非法值。纯严格方案会误路由全部探针记录，此为构建期发现的设计纠偏。
- **TSDB 脚手架删除（批次 2 补记）**：范围从「迁移脚手架」扩展到启动自动迁移块与整个 migrate-tsdb CLI（同属死代码路径）；Store.MigrateFrom SPI 保留（接口方法不触发死代码告警，超出批准范围不动）。

轻微（P2）24 项经用户批准「全部本轮修复」，已全部处置并验证（2026-08-28）；另 6 项（F-5/F-7/F-8/F-9/F-10/F-12）经批准保持现状，标注见后：

| 批次 | 编号 | 修复内容 | 验证 |
| --- | --- | --- | --- |
| 5 | C-7、C-8、C-11、C-16 | run handle 恒非纯数字（newRunID 十六进制）与十进制任务号的不变式以注释固化（report.go bindTaskRef 文档 + events.go 补充）；Register 在 engine.Add 失败时补偿删除已持久化任务（Update 同理）；事件发布与 executor 派发/落库点改 `context.WithoutCancel(ctx)` 派生（report/sweeper/events/lifecycle 共 7 处）；listTasks 挪入测试辅助、`ExecutionStore.List` 从接口与实现删除（`ListOptions.IDs` 承接批量查询） | `go test ./pkg/task/... ./pkg/executor/...` 全绿；新增 `TestRegisterRollsBackOnAddFailure` |
| 6 | C-10、C-16、C-19、C-21、C-22、C-23、C-25 | crypto/rand 初始化种子读取失败改 panic 拒启（附理由注释）；登出 best-effort 绑定保留但补注释（见偏差说明）；channel store 空 `var _` 断言删除；matcher 热路径资产查找加 LRU 缓存（validator 同款）；Aggregator/runner 的 Stop 协程生命周期语义注释固化（有界 drain/tail）；`pkg/event/types.go`→`model.go`、`pkg/api/types.go`→`config.go`（四件套布局归位）；`task.Manager` 接口更名 `TaskEngine` 并统一注释词汇（Options/engineOption/WithEngine/NewEngine/Stop 等残留 Service/manager 措辞一并清理）；`NewLibrary` 直接构造返回、裸断言消除 | CE `golangci-lint run` 0 issues；`go test` 全绿 |
| 7 | F-3、F-4、C-9、C-20、C-24 | start_mqtt_broker 过期 TODO 改为 standalone 模式边界说明（anonymous auth + log-only 为设计而非待办）；`validTenantTypes` 收敛为带许可约束说明的显式枚举；8 处错误响应体读取统一 `io.LimitReader(respBodyLimit)`（64KiB）；syncer 增量同步逐 ID 查询改 `ListOptions.IDs` / GORM `Find` IN 批查询；sonic 用法按全仓约定（解码 ConfigDefault、签名/指纹敏感点 ConfigStd） | x `go test ./...`（default+saas 双标签）全绿；`run_lint.sh` 双变体 0 issues |
| 8 | U-2、C-26、C-27 | tf 内联回退三副本（DefaultLayout/BlankLayout/AuthBrandPanel）删除、原回退 key 补齐进语言包；x 前端 16 个 API 文件各自声明的 `PageResult`/`ListQuery` 统一改为从 `@tickraft/core` 导入；三大组件拆分：sso Config 1107→428、license Overview 1000→258、cluster Overview 935→293（`components/` 子组件 + defineModel 双向绑定，父组件保留编排） | vue-tsc 0 错；eslint 0 错（存量 warning 与本次改动无关）；CE web vitest 72/72 |
| 9 | A-1、A-3、F-11、D-16、D-18 | module-boundary 增加「仅引用 `http.Status*` 常量」豁免（5 个域 service 的实际用法）；architecture.md Listener 章节标注版本归属（CE 仅内置 HTTP，syslog/SNMP/MQTT 为扩展版提供）；路由注册 4 处失实「内存兜底」注释纠正为「省略时不注册路由组（测试注入内存实现，生产装配恒注入 DB 版）」；两篇设计文档双语落位（根目录英文权威版 + zh-CN 镜像带参考声明 + README 双语索引）；x web/README.md 从占位符改写为真实文档（用途/Node 22+/pnpm 9/link: 兄弟仓依赖/命令/代理说明） | 红线扫描重放通过（tracked + 新文件分别扫描）；根目录文档 0 中文字符、标题/代码围栏与原文对齐（45/45、82/82） |

### P2 与原建议的偏差说明

- **C-10（登出绑定）**：原建议「改用统一 BindAndValidate」——经核实登出 body 为可选字段，`BindAndValidate` 的硬 400 会拒绝无 body 的登出请求（老客户端确实如此发送），故保留 `_ = c.Bind` best-effort 并补注释说明。rand fail-fast 项按建议落地（init panic 拒启）。
- **C-21**：以注释文档化收束——Stop 返回后协程存活是有界的 drain/tail（run 协程观测到取消的 context 即退出，无无界泄漏），改行为引入的风险大于收益。
- **C-22**：`pkg/api/types.go` 实际内容是 Plugin 配置类型（非 API 模型），更名 `config.go` 而非原建议的 `model.go`，四件套命名意图一致。
- **C-23**：接口更名 `TaskEngine` 后 revive 报 `task.TaskEngine` stutter，按全仓既定手法加定向 `//nolint:revive` 注释（`Engine` 名称为具体实现结构体持有，接口需区分）。
- **C-25**：最终形态为 `NewLibrary` 返回 `Library` 接口并直接构造实现（`NewBuiltinLibrary` 改用局部 logger），全仓不再存在裸类型断言，同时满足 revive 的 unexported-return 约束。
- **C-27**：租户 Detail 在批次 1 F-1 删除 mock UI 块后已降至 679 行（低于拆分阈值），未纳入本轮拆分；实际拆分为 sso/license/cluster 三个。
- **C-8 连带**：x `internal/service/worker/strategy_test.go` 两处使用 `ExecutionStore.List` 的测试随接口删除改为 `Query`（本轮全量 x 测试时发现的连带断裂，已修复并重跑双标签全绿）。

### 保持项标注（经批准保持现状）

- **F-5（KMS 静态密钥）**：保持——已有文档说明与 workaround，默认凭据链支持列为后续项。
- **F-7（NoopTicketClient）**：保持——诚实报错（`ErrAtlasUnavailable`、路由不注册）的有意优雅降级。
- **F-8（成员添加降级）**：复核结论——`accountCreator == nil` 生产装配恒注入、仅测试走 501 ✓；但 `emailSender`（MemberEmailSender 端口）**在任何装配中都没有实现**，注释中「CE/测试模式」表述过实：明文密码放响应体就是当前生产行为。SMTP 实现超出本轮批准范围，记录为后续项。
- **F-9（healthz/readyz 库级 stub）**：保持——库级默认恒 200，CE 生产装配已接真实 handler。
- **F-10（asset-key deny-all）**：保持——fail-closed 正向样本。
- **F-12（CE web mock 层）**：保持——无视图引用，仅 dev-only vite mockServerPlugin 消费，开关语义不变。

### 存量告警清零（2026-08-28 第二轮，经用户要求一并修复）

| 范围 | 内容 | 验证 |
| --- | --- | --- |
| CE web eslint（307 条 warning） | 306 条格式类（max-attributes-per-line 等）`--fix` 自动清零；FeatureGuard 的 required-prop-with-default 手工修（删除 `feature: ''` 死默认值）；eslint 全局 ignores 补 `**/.vite/**`（Vite 预构建缓存此前会被 `eslint .` 全量扫描误报 2528 个 errors） | 规范范围与 `eslint .` 全量范围均 0 problems；vue-tsc 0 错；vitest 72/72 |
| CE web stylelint | 复核时发现 DefaultLayout.vue 一处 N-1 中性化改写吞行损伤（`// 注释` 与 `@media (...) {` 并行，致 `@media` 被注释吞掉、出现孤立 `}` 且 reduce-motion 规则失效）——拆行修复；全仓双仓扫描确认仅此一处同类损伤 | stylelint 0 problems |
| x web eslint（7 条 warning） | OpsBadge/OpsStat 四个可选 prop 补显式 `undefined` 默认值；ops Login 两处 `v-html` 改普通插值（语言包实测为纯文本，v-html 属多余 XSS 面）；telemetry Search 日志高亮由 v-html 重构为分段渲染 + `<mark>` 组件（escapeHtml/highlightMessage 删除，内容经插值天然转义） | eslint 0 problems；vue-tsc 0 错 |
| x web stylelint（ops 子应用 136 条 error，官方脚本此前从未覆盖 ops） | `--fix` 自动清零 131 条（属性排序/空行）；手工修 5 条：OpsLayout grid 长属性改 `grid-template` 简写、element-overrides 两处 EP 内部类 `btn-prev/btn-next` 加豁免注释、Login/Tenants 两处 no-descending-specificity 规则块重排；stylelint 脚本 glob 扩展覆盖 `ops/src/**` 防回潮 | stylelint（src+ops）0 problems |

连带修复说明：x `pnpm lint` 脚本自带 `--fix`，会把 disable-next-line 注释与报错行的配对拆散（格式化强制多行属性）——Search.vue 因此放弃注释压制、彻底去除 v-html。

### 构建兜底新发现并修复：x web build 被 i18n 消息语法阻断（2026-08-28）

补跑双仓 `vite build` 兜底时发现 x web 生产构建失败（此前矩阵只有 type-check/lint/test，从未跑过 build）：`task.create.reportStatusHint`（zh-Hans/en-US task.json，8-27 任务状态双模式新增键）含字面量 `{{task_ref}}`，vue-i18n 消息编译器将 `{` 视为插值语法、嵌套大括号直接报错（error code 9）。修复：改为字面量转义 `{'{{task_ref}}'}`（渲染输出不变，仍显示 `{{task_ref}}`）。全语言包扫描确认仅此一键受影响；修复后双仓 build 均通过。教训：**前端验证矩阵必须包含 build**（type-check/lint 均不触发消息预编译）。

## 总体结论

双仓 kernel/extension 架构质量高：CE 是依赖方向严格向下的干净内核（cmd → internal 装配 → pkg SPI），x 完全通过已文档化的 SPI（`api.Plugin`、executor/channel/processor 注册表、option 注入组合根、memory/Redis 状态后端）挂钩，不修改内核源码、无向上依赖；fail-closed 默认（asset-key deny-all、MQTT 匿名认证拒启、路由校验必填服务）体现安全意识。CE 代码内零 TODO，历史 P0/P1 修复全部保持有效。

本轮发现的实质弱点集中在三处：**① CE 遥测处理循环缺乏 panic 自愈（唯一严重级代码缺陷，监控产品会静默失明）**；**② x 的 SaaS 控制台表面完整性（8 个页面渲染 mock 数据而后端已就绪）**；**③ 文档可信度——CE openapi.yaml 有 4 处与代码相反的硬错误，x 文档滞后于代码重构**。以下逐维度展开。

---

## 一、架构与方案设计评估

### 1.1 总体架构（验证通过）

- **分层与依赖方向**：CE `cmd/tickraft`（18 行薄入口）→ `internal/cli`（cobra 命令）/`internal/service`（runtime DI、worker、api 装配）→ `pkg/`（26 个公共内核包）。grep 验证 `pkg/` 零导入 `internal/`/`cmd/`；x 的 `internal/` 零导入 x 的 `cmd/`。无循环依赖风险。
- **kernel/extension 接缝**（高质量）：
  - 模块级：x `go.mod` 以 `replace github.com/tickraft/tickraft => ../tickraft` 引 CE，仅 import CE 的 `pkg/`（约 500 条），零 import CE `internal/`。CE 自身无版式构建标签（仅 windows/unix reaper）。
  - Plugin SPI：`CE pkg/api/plugin.go:15-31` 定义 `Plugin`（Name/RegisterRoutes/Middlewares/OnStart/OnStop）；`X internal/api/plugin.go:68-787` 实现（`:787` 编译期断言），注册约 130 条扩展路由。
  - Option 组合根：`CE pkg/api/router/router.go:373-412` `RegisterRoutes(..., opts...)` 17 个 `With*` 选项；x 经 `X internal/api/service_factory.go:75-77` 注入（含 Syncer 感知装饰器、分布式 Server 角色 engine-less DB 服务）。
  - 其他 SPI：executor 注册表、channel 注册表、遥测 processor 注册表、listener SPI、治理状态 memory/Redis 后端、cache LRU/Bbolt/Redis。
- **技术选型**（适当）：CE Go 1.26.4，Hertz v0.10.6 + GORM v1.31.2 + sonic v1.15.2 + expr-lang v1.17.8 + bbolt/cobra/zap/jwt-v5——小而新，契合单二进制产品。x 增补 redis/pgx/k8s controller-runtime/prometheus/mochi-mqtt/gosnmp/chromedp/wazero/lua/grpc——重但每个都有对应真实模块（operator、MQTT broker、SNMP、浏览器流执行器、WASM/Lua 编解码、配额 gRPC），版本均为当前版。

### 1.2 发现

| 编号 | 问题 | 位置 | 程度 | 建议 | 优先级 |
|---|---|---|---|---|---|
| A-1 | module-boundary 文档禁止业务包导入 `net/http`，但 5 个域 service 仍导入（仅为 `errdefs.ServiceError` 使用 `http.Status*` 常量） | `CE pkg/task/service.go:11`、`pkg/prism/alert/service.go:10`、`pkg/prism/channel/service.go:11`、`pkg/prism/remediation/service.go:11`、`pkg/telemetry/service.go:11`；规则出处 `CE docs/module-boundary.md` | 轻微 | 二选一：域层定义自己的状态码枚举，或修订文档豁免「仅状态常量」场景 | P2 |
| A-2 | 模块命名「collector」与「telemetry」漂移：README/前端菜单/pkg 均为 telemetry，`docs/architecture.md`（7/41/43/60/66/72 行）、`docs/user-guide.md`、`docs/module-boundary.md`、`docs/README.md`、截图文件名、`.trae/rules/03` 仍用 collector | 见左 | 中等 | 统一为 telemetry：文档全文替换、截图重命名、`.trae/rules/03 §1.3` 术语表更新 | P1 |
| A-3 | `docs/architecture.md` Listener 示例列出 syslog/SNMP/MQTT——CE 仅内置 HTTP listener，其余在 x | `CE docs/architecture.md` collector 章节 | 轻微 | 标注「CE 内置 HTTP；syslog/SNMP/MQTT 由扩展版提供」 | P2 |

---

## 二、功能完整性与实现质量

### 2.1 占位/stub/未完成实现全量清单

CE 非测试 Go 代码零 TODO/FIXME/XXX/HACK，无 not-implemented panic。x 仅 3 处 TODO。逐项：

| 编号 | 问题 | 位置 | 程度 | 分类 | 建议 | 优先级 |
|---|---|---|---|---|---|---|
| F-1 | **SaaS 控制台 8 个视图渲染硬编码 mock 数据**（订阅总览/租户列表/租户详情/账单/支付/配额/定价/升级），真实后端路由已注册（billing.RegisterRoutes、`/api/v1/quotas`、`/api/v1/tenants`）、`X web/src/api/saas.ts` 已有真实请求函数但视图从不调用 | `X web/src/views/saas/**`（例：`subscription/overview/Overview.vue:15,21,80,303-316` 直接渲染 `mockSubscription/mockQuotaUsage`）；`X web/src/api/saas.ts:23` | **严重** | 未完成工作（已人工复核确认） | 8 个视图接入 saas.ts 真实 API + 加载/错误态；完成后删除 `@/mock/saas` | P0 |
| F-2 | `K8sLeaseManager` 全部 5 个方法返回 "not yet implemented"，k8s 模式许可证租约不可用；选择该模式不报错、首次使用才失败 | `X internal/license/lease_k8s.go:19-81`（TODO :42） | 中等 | 遗留未实现（Phase 10），当前无调用方 | 启动时对 lease.mode=k8s 显式报错拒绝启动；实现排期后放开 | P1 |
| F-3 | 独立 MQTT broker 模式启动时未接 DeviceRegistry/TenantKeyLookup/forwarder；已缓解：未配 registry 且非显式 anonymous 时 broker 拒绝启动（fail-closed） | `X internal/cli/start_mqtt_broker.go:109-113`；`X mqtt/broker.go:512-527` | 轻微 | 半有意（standalone 模式），TODO 文案过期 | 更新 TODO 描述为「standalone 模式边界」或补齐注入路径 | P2 |
| F-4 | 租户配额常量需与 license 常量手工同步，存在漂移风险 | `X internal/tenant/store.go:40` | 轻微 | 有意备注 | 单一常量源（license 包导出，tenant 引用） | P2 |
| F-5 | AWS KMS 不支持 IRSA/EC2 instance-profile 凭据，仅静态密钥（已有文档说明与 workaround） | `X internal/credential/kms.go:196-200` | 轻微 | 已文档化的限制 | 保持；后续支持默认凭据链 | P2 |
| F-6 | 未注入审计存储时 license 域审计静默 no-op（`noopAuditLogger`）；登录审计已接 `audit.Store`，license 域可能漏审计 | `X internal/license/audit.go:321-351`、`X internal/auth/service.go:84` | 中等 | 扩展点默认值，但属静默数据丢失 | 生产装配路径显式注入审计存储；noop 路径打 Warn 日志 | P1 |
| F-7 | `NoopTicketClient` 恒返回 `ErrAtlasUnavailable`，路由不注册（handler nil）——诚实报错、无假成功 | `X internal/ticket/ports.go:45-55` | 轻微 | 有意的优雅降级 | 保持 | — |
| F-8 | `accountCreator == nil` 时 AddMember 返回 501；`emailSender == nil` 时明文密码直接放响应体（CE/测试模式，已有文档） | `X internal/tenant/handler_member.go:84-92` | 轻微 | 有意的 CE 模式降级 | 保持；确保仅测试装配出现 | P2 |
| F-9 | `/healthz`、`/readyz` 默认 stub 恒 200；CE 生产已接真实 handler | `CE pkg/api/router/router.go:276-279,288-290`；装配 `CE internal/service/api.go` | 轻微 | 有意的库级默认 | 保持 | — |
| F-10 | asset-key 遥测鉴权 getter 为 nil 时 deny-all——fail-closed，好模式 | `CE pkg/api/router/router.go:43-48` | 轻微 | 有意（正向样本） | 保持 | — |
| F-11 | channel/remediation 内存兜底 store（服务未注入时）；CE 与 x 生产装配均注入 DB 版 | `CE pkg/api/handler/route_option.go:122-127,137-142` | 轻微 | 仅测试路径 | 可加注释标明 test-only，或收紧为显式 panic | P2 |
| F-12 | CE web mock 层（7 文件）无视图引用，但被 dev-only vite mockServerPlugin 消费 | `CE web/packages/features/src/mock/*`、`CE web/app/vite.config.ts:40-46` | 轻微 | 有意开发工具 | 保持 dev-only 开关语义即可 | — |

### 2.2 内存存储 vs 持久化审计

历史项验证已修复：token 黑名单 GORM 化（`CE pkg/auth/store.go:19-20`，表 `sys_token_blacklist`；x HA 用 Redis 复合黑名单）；`sys_probe_record` 域分离真实生效（`CE internal/service/worker.go:150-168,320-340`）；执行记录 GORM 持久化。CE 16 张表 + x 约 78 张表。

逐项分类（其余内存态均为可接受的短 TTL/自愈缓存：治理去重 60s 窗口、x 治理状态 memory/Redis SPI、license 本地租约（Redis 版存在）、MFA ticket、采集器注册表、集群拓扑、cache 包 LRU/Bbolt/Redis、ACME 测试兜底、遥测状态缓存、expr 编译缓存、事件总线、i18n bundle）：

| 编号 | 问题 | 位置 | 程度 | 建议 | 优先级 |
|---|---|---|---|---|---|
| F-13 | **API 限流器仅 per-node**（sync.Map 令牌桶），无 Redis 实现；HA/SaaS 部署实际限额 = 配置值 × 副本数 | `X internal/middleware/ratelimit.go`（`internal/middleware/` 下未发现 Redis 版） | 中等 | 增加 Redis 令牌桶实现并按部署模式选择；单节点部署保持现状 | P1 |

**结论：生产路径不存在应持久化而仅内存的告警状态/黑名单/记录存储**（历史缺陷均已修复并验证）；唯一真缺陷是 F-13 限流器，最大完整性缺口是 F-1 SaaS mock 页。

---

## 三、代码质量深度分析

### 3.1 逻辑漏洞

| 编号 | 问题 | 位置 | 程度 | 建议 | 优先级 |
|---|---|---|---|---|---|
| C-1 | **遥测处理循环 panic 后永久静默停摆**：`recoverPanic` 仅记日志不重启；`Start` 被 `started` 幂等守卫挡住无法重入；之后 `Submit` 持续投递直至 1024 缓冲填满丢弃——监控产品失明且仅有 Warn 日志。同形态还有聚合消费循环与 `Aggregator.run`。注释宣称 "the engine can keep serving other telemetry streams" 与实际相反（已人工逐行复核确认） | `CE pkg/telemetry/engine.go:194-198`（process loop）、`:207-211`（aggregated consumer）、`:436-444`（recoverPanic）、`:171-173`（幂等守卫）、`:422-430`（Submit 丢弃）；`CE pkg/telemetry/aggregation.go:263-290` | **严重** | 循环体外层套 for + recover（panic 后退避重启），或 recoverPanic 内重新拉起协程；同步修正失实注释 | P0 |
| C-2 | `UpdateStatus` 先写内存缓存再落库，事务失败不回滚——缓存/DB 状态分叉，且缓存值未发 `StatusChange` 事件 | `CE pkg/telemetry/state.go:184-217`；调用方 `pkg/telemetry/pipeline.go:117-124` 仅记日志 | 中等 | 落库成功后再更新缓存；失败时保留旧值并返回错误 | P1 |
| C-3 | 资产首次上报路径不写 `StatusHistory` 基线行，且不在事务内 | `CE pkg/telemetry/state.go:168-177`（对比变更路径 :192-217） | 中等 | 首次赋值同样写 history 行并纳入事务 | P1 |
| C-4 | 心跳超时竞态可误判在线资产离线：时间轮条目已出队则回调仍执行，`MarkOffline` 仅查「是否已离线」，无「调度后是否有新上报」守卫；后果是假离线→告警/处置风暴（窗口≈轮询 1s+派发延迟） | `CE pkg/telemetry/state.go:61-91,110-146`；`pkg/telemetry/pipeline.go:260`；`pkg/telemetry/processor/offline.go:47-86` | 中等 | 回调持锁比对「最后上报时间/世代号」与条目调度时间，过期即忽略 | P1 |
| C-5 | `ApplyTaskReport` 先查后改无状态守卫：UPDATE 仅 `Where(id)`；running 上报与 sweeper `MarkTimeout`（后者有 `status=running` 守卫）竞态可把 timeout 行复活为 running、或覆写刚落库的终态行 | `CE pkg/task/report.go:86-114`；守卫对照 `pkg/task/store.go:279-294` | 中等 | UPDATE 加 `status` 条件（期望态）并检查 RowsAffected；不匹配则重读按现行状态机裁决 | P1 |
| C-6 | 潜在 nil 崩溃：校验层显式容忍 `assetStore == nil`，但状态层 `sm.store` 无条件调用——仅 `WithAssetStore` 缺省时可达；panic 被池 recover 吞掉，每条上报静默失败 | `CE pkg/telemetry/validation.go:120-122` vs `pkg/telemetry/pipeline.go:117`、`state.go:173/194/220` | 中等 | 构造时强制要求 store（fail-fast），或状态层显式判空降级 | P1 |
| C-7 | `bindTaskRef` 数字 ref 歧义当前仅靠构造安全（run handle 恒非纯数字）维持，属脆弱隐式契约 | `CE pkg/task/report.go:156`；`pkg/task/events.go:181-187` | 轻微 | 在两处补注释固化约定；或 ref 引入类型前缀 | P2 |
| C-8 | `Engine.Register` 先持久化后 `engine.Add`，Add 失败时任务已入库已入内存表——调用方按「未注册」处理会与持久态背离；`Update` 同理非原子 | `CE pkg/task/engine.go:269-289,312-315` | 轻微 | 失败路径补偿删除（或文档化该语义） | P2 |

### 3.2 异常处理 / 资源释放

| 编号 | 问题 | 位置 | 程度 | 建议 | 优先级 |
|---|---|---|---|---|---|
| C-9 | 错误响应体 `io.ReadAll` 无大小上限（内核侧 `httputil.ReadBody` 有 ProbeBodyLimit 上限，x 这些点绕开了） | `X internal/credential/vault.go:235,282`、`kms.go:254,422,501`、`X internal/auth/handler_oauth_providers.go:63,94`、`X internal/license/fingerprint.go:599` | 轻微 | 换 `io.LimitReader`（如 64KB） | P2 |
| C-10 | 偶发吞错点：`rand.Read` 失败忽略；登出可选 body `_ = c.Bind` 与全仓 `BindAndValidate` 惯例不一致 | `CE pkg/prism/dispatch.go:456`；`CE pkg/api/handler/auth/handler.go:104` | 轻微 | rand 失败 fail-fast；登出改用统一绑定工具 | P2 |
| C-11 | 事件发布点用 `context.Background()` 而非派生的 detached ctx，丢失上游 trace/元数据 | `CE pkg/task/report.go:333`、`sweeper.go:145`、`events.go:167`、`pkg/executor/lifecycle.go:398` | 轻微 | 若无生命周期原因，改 `context.WithoutCancel(ctx)` | P2 |

**验证通过（资源处理）**：全部 `resp.Body` 均 defer 关闭；30+ 处 Ticker/Timer 均停止或 Stop 内显式停；仅 2 处 `http.Client{}` 字面量且均带超时；executor 客户端统一走 `httpx.NewPoolClient` + `HardTimeout` 封顶；无裸 `http.Get/Post`；无直接 `sql.Rows`；池/总线协程 wg 跟踪且 Stop 有界。

### 3.3 冗余 / 死代码 / 未发布项目的兼容代码

| 编号 | 问题 | 位置 | 程度 | 建议 | 优先级 |
|---|---|---|---|---|---|
| C-12 | `ListenerRegistry.RegisterHTTP/ListHTTP/httpListeners` 双仓零调用（webhook listener 实际由装配层直挂 `ReportHandler`），且 `engine.go`/`telemetry.go` 多处注释仍在描述这条死接线 | `CE pkg/telemetry/listener.go:97-133`；失实注释 `pkg/telemetry/engine.go:79-80,224-225`、`pkg/telemetry/telemetry.go:110,258` | 中等 | 删除死方法与字段；修正注释为「HTTP listener 由装配层挂载」 | P1 |
| C-13 | `tagsFromMetadata` 在 CE 两个域逐字节重复（含同样的吞错语义） | `CE pkg/prism/alert/env.go:101-113`、`pkg/prism/remediation/env.go:160-172` | 中等 | 下沉 `pkg/prism` 共享包（或各自 env 包公共 helper） | P1 |
| C-14 | `classifyStoreError` 在 x 两个 handler 包逐字节重复；`parseRecordID/recordFilterScope` 家族有随新子包继续复制的趋势 | `X internal/prism/grading/handler.go:135`、`X internal/prism/codec/handler.go:128` | 中等 | 提取 `internal/prism/handlerutil` 共享 | P1 |
| C-15 | **未发布却保留的兼容垫片**（项目无任何 release，无兼容对象）：quota 类型别名（生产仅 1 处内部使用 + 1 处 x 测试）；legacy 探针行一次性清理 shim（启动时仍在跑）；事件 `operation` 空值兼容分支；`AfterAfter` 旧行自愈钩子；x 关系↔TSDB 回填迁移脚手架 | `CE pkg/quota/quota.go:92-101`、`CE internal/service/worker.go:342-353`、`CE pkg/event/payload.go:78`、`CE pkg/prism/alert/model.go:108`、`X internal/timeseries/migrate.go`（整文件） | 中等 | 全部移除：别名改直接引用新名、内部使用点改 `TypeProbeInterval`；清理 shim 删除；`operation` 空值改必填；`AfterAfter` 删除；TSDB 迁移脚手架移出生产路径（dev DB 重建即可） | P1 |
| C-16 | test-only 生产代码：`listTasks` 仅引擎测试用；`ExecutionStore.List` 双仓仅测试调用；channel store 的 `var _ = (*Store)(nil)` 空断言（断言不了任何接口） | `CE pkg/task/persistence.go:177-185`、`pkg/task/store.go:205-218` + `ports.go:92`、`pkg/prism/channel/store.go:174-177` | 轻微 | `listTasks` 挪测试或改导出测试钩子；`List` 删除或标注外部消费者；空断言改为具名接口断言或删除 | P2 |
| C-17 | CE web 根目录提交了 5 个 Playwright 调试脚本 + 构建产物 tsbuildinfo；`test-fix.cjs` 硬编码另一项目（arcadia）的绝对路径 | `CE web/app/test-column-drag-stability.cjs`、`test-deep-diag.cjs`、`test-fix.cjs:13`、`test-scroll-sync.cjs`、`test-scroll-sync2.cjs`、`tsconfig.node.tsbuildinfo` | 中等 | 全部删除（tsbuildinfo 同时补 .gitignore） | P1 |

### 3.4 性能

| 编号 | 问题 | 位置 | 程度 | 建议 | 优先级 |
|---|---|---|---|---|---|
| C-18 | sweeper 每 30s 以 `status='running'` 全表扫 `sys_schedule_log`，该列无索引，而表「无界增长、仅靠保留期清理」（表自身注释）——扫描成本随历史单调上升 | `CE pkg/task/model.go:207`（无 index tag）；`CE pkg/task/sweeper.go:90-91` | 中等 | `Status` 加索引（`index` tag + 迁移）；或 sweeper 查询限定 `updated_at > now-stale-window` 组合索引 | P1 |
| C-19 | 告警热路径每事件未缓存查资产（validator 有 LRU，matcher 的 `buildEnv` 没有） | `CE pkg/prism/alert/matcher.go:79-84` | 轻微 | 复用 validator 同款 LRU | P2 |
| C-20 | 有界低频 N+1：x syncer 增量同步逐 ID `Get/First`；启动时逐内置模板 `Count` | `X internal/service/server/syncer.go:164-165,221-223`；`CE pkg/telemetry/builtin_templates.go:115-118` | 轻微 | 改 `IN` 批查询（启动 Count 可保留） | P2 |
| C-21 | `Aggregator.Stop`/`Engine.Stop`/`runner.Stop` 在调用方 ctx 先超时时遗留等待协程（非泄漏，可自愈，但生命周期超出 Stop 返回） | `CE pkg/telemetry/aggregation.go:349-362`、`pkg/executor/runner.go:324-338` | 轻微 | 文档化或用独立 detached ctx 等待 | P2 |

**验证通过（性能/契约）**：分页契约全端强制（`ParsePaging` 严格 400、MaxSize=100、DefaultSize=20，双仓全部列表 handler 过检，无无界 Find）；`QueryExecutions` 批量名字映射避免 N+1；flush/遥测通道有界（256/1024）且满时显式丢弃告警；热路径有池化 buffer/Results、编译规则快照无锁求值。

### 3.5 风格一致性

| 编号 | 问题 | 位置 | 程度 | 建议 | 优先级 |
|---|---|---|---|---|---|
| C-22 | 四件套布局漂移：`pkg/event/types.go`、`pkg/api/types.go` 存在（文档只豁免 auth）；`pkg/telemetry/migrate.go` 独立迁移文件；`builtin_templates.go` 含 store 风格 GORM 代码 | `CE pkg/event/types.go`、`pkg/api/types.go`、`pkg/telemetry/migrate.go`、`pkg/telemetry/builtin_templates.go:118`；规则出处 `CE docs/architecture.md:98-116` | 轻微 | types 并入 model.go/doc.go；迁移并入 store.go；模板 Count 挪 store.go | P2 |
| C-23 | 命名语义：`task.Engine` 实现 `Manager` 接口（算数据的 Engine 实现管资源的 Manager 接口），且 `engine.go:52-55` 注释混用 Service/Manager/Engine 三词 | `CE pkg/task/engine.go:25-53,52-55` | 轻微 | 接口更名（如 `TaskEngine`）+ 注释词汇统一；x 侧 Manager 用法均合规 | P2 |
| C-24 | sonic 配置混用：x credential 对普通 Vault/KMS 响应用 `ConfigStd`（非字节敏感场景），同文件族 OAuth handler 却用 ConfigDefault | `X internal/credential/vault.go:245,299,311`、`kms.go:262,432,509` vs `X internal/auth/handler_oauth_providers.go:67,98` | 轻微 | 普通响应统一 ConfigDefault；仅签名/指纹处 ConfigStd | P2 |
| C-25 | 裸类型断言 `NewLibrary(logger).(*library)`——构造器返回类型一变即 panic | `CE pkg/prism/alert/template/library.go:93` | 轻微 | comma-ok 或构造器直接返回具体类型 | P2 |
| C-26 | x 前端 16+ API 文件各自重复声明 `interface PageResult/ListQuery`，未复用 `@tickraft/core` | `X web/src/api/*.ts`、`src/api/prism/*.ts` | 轻微 | 统一从 core 导入 | P2 |
| C-27 | x 前端超大组件：SSO Config.vue 1107 行、license Overview 1000、租户 Detail 989、集群 Overview 935 | `X web/src/views/system/sso/config/Config.vue` 等 | 轻微 | 按面板拆子组件 | P2 |

**验证通过（风格）**：CE 双仓 lint 零告警基线保持；命名/注释密度整体一致；x 前端正确委托共享端点到 `@tickraft/features`（无跨版本 API 包装重复）；无 console.log 残留；无注释掉的代码块；49 处 `//nolint` 全部带理由。

---

## 四、文档与实现一致性校验

### 4.1 严重（openapi.yaml 为对外 API 契约，硬错误直接误导消费者）

| 编号 | 问题 | 位置 | 程度 | 建议 | 优先级 |
|---|---|---|---|---|---|
| D-1 | 告警路径错误：spec 写 `/api/v1/alert/rules`、`/api/v1/alert/records`；后端实际注册 `/api/v1/prism/alert/*`，前端也调 prism 前缀——spec 消费者必 404 | `CE docs/api/openapi.yaml:1596,1733`；实际 `CE pkg/api/handler/routes.go:208,217`、`CE web/packages/features/src/api/prism.ts:71,112` | **严重** | 修正 spec 路径为 `/api/v1/prism/alert/*` | P0 |
| D-2 | 响应包络字段错误：spec `{code, msg, data}`；代码实际输出 `message` | `CE docs/api/openapi.yaml:19` vs `CE pkg/api/httputil/response.go:16` | **严重** | spec 全部响应体 `msg` → `message` | P0 |
| D-3 | API Key header 名错误：spec 写 `X-API-Key`；代码常量 `X-Tickraft-API-Key` | `CE docs/api/openapi.yaml`（Authentication Schemes 节）vs `CE pkg/api/httputil/headers.go:16` | **严重** | spec 更正 header 名 | P0 |
| D-4 | 描述已不存在的双端口：称 API 6153 / 遥测上报 6253 并列出第二 server 条目；代码单端口 `:6153`（6253 全仓无一处出现）；同节引用的 `pkg/collector/http/listener.go` 路径已改名 `pkg/telemetry/http/listener.go` | `CE docs/api/openapi.yaml`（info/servers 节）vs `CE pkg/config/config.go:20,52-56` | **严重** | 删除 6253 server 条目与双端口描述；修正引用路径 | P0 |
| D-5 | x 快速开始二进制名错误：README 写 `./bin/tickraft-x`；Makefile 产物为 `bin/tickraft` | `X README.md:26` vs `X Makefile:6,17` | **严重**（新用户第一条命令即失败） | README 更正为 `./bin/tickraft` | P0 |
| D-6 | x CI 前端 job 必挂：`npm ci` + node 20 跑 pnpm workspace（无 package-lock.json）；README 开发节同样写 `npm install && npm run dev` | `X .github/workflows/ci.yml`（frontend job）、`X README.md:33` vs `X web/pnpm-lock.yaml`、`X Makefile`（web-build 用 pnpm --frozen-lockfile） | **严重**（CI 常红） | CI 与 README 改 pnpm（对齐 CE 的 node 22+/pnpm 9 基线） | P0 |

### 4.2 中等

| 编号 | 问题 | 位置 | 程度 | 建议 | 优先级 |
|---|---|---|---|---|---|
| D-7 | `configuration.md` 记录不存在的配置项 `prism.channel_config`（`PrismConfig` 仅 EvalInterval/Concurrence） | `CE docs/configuration.md:71`、zh-CN `:74` vs `CE pkg/config/config.go:165-187` | 中等 | 删除该节 | P1 |
| D-8 | 超时默认值错误：文档称 read 10s/write 30s；代码默认零（无超时），example yaml 里的 10/30 是显式样例非默认 | `CE docs/configuration.md:50-51` vs `CE pkg/config/config.go` SetDefaults；`CE configs/config.example.yaml:40-42` | 中等 | 文档改为「默认 0（不限），建议显式配置」 | P1 |
| D-9 | 路由表列出不存在的 `/webhook/*` 遥测路由（实际为 `POST /api/v1/telemetry`）；且称 `/api/v1/*` 全走 JWT（login/refresh/i18n/telemetry 实际豁免） | `CE docs/configuration.md:113-122` | 中等 | 重写单端口路由表并标注豁免端点 | P1 |
| D-10 | 前置条件互相矛盾 + 快速开始产出无 UI 二进制：README node22/pnpm9、getting-started node18/pnpm8、web/README node18/pnpm8、CI node24/pnpm9；`getting-started` 第 1 步用裸 `go build`，而 `internal/web/dist` 被 gitignore（仅 .gitkeep）——产物内嵌空 SPA，浏览器登录步骤必失败；`make build` 又强制要求 node+pnpm | `CE README.md:90`、`CE docs/getting-started.md:8,16`、`CE web/README.md`、`CE .github/workflows/ci.yaml:189-193`、`CE .gitignore:41-43` | 中等 | 统一 node 22+/pnpm 9 表述；getting-started 改 `make build` 或提供「无前端构建」的明确降级说明 | P1 |
| D-11 | `web/README.md` 技术栈版本全过期（Vite 5/Vue Router 4/Pinia 2/zod 3 vs 实际 vite 8/router 5/pinia 4/zod 4） | `CE web/README.md` vs `CE web/app/package.json`、`web/packages/core/package.json` | 中等 | 更新版本表 | P1 |
| D-12 | openapi.yaml 自称「仅含实际注册路由」却缺 20 条：executors、expr/validate、channels×6、remediation×6、profile GET/PUT、assets status/probe、告警 acknowledge/resolve、/ws、i18n、/readyz；尾部 Related documentation 四链接全部不存在 | `CE docs/api/openapi.yaml` | 中等 | 补齐缺失路径；删除或修复外链 | P1 |
| D-13 | x `pro-extensions.md` 指向已删除路径（`internal/account/`、`internal/handler/sso_*.go`、`internal/handler/roaming.go`、`internal/auth/registry.go`）+ 6 个死链接（docs/modules/、docs/system-design.md 等）；另低报 executor 数量（列 8 实有 19） | `X docs/pro-extensions.md` | 中等 | 全面修订路径与链接；executor 清单补齐 | P1 |
| D-14 | x CI 分支不匹配：ci.yml 触发 [main, develop]（仓内无 develop）、license-scan 仅 [main]；`run_lint.sh` 硬编码本机绝对路径且只 lint 一个子树、产物写仓根 | `X .github/workflows/ci.yml`、`X run_lint.sh:3` | 中等 | 触发分支对齐；脚本改相对路径 + 全仓 lint + 产物 gitignore | P1 |

### 4.3 轻微

| 编号 | 问题 | 位置 | 建议 |
|---|---|---|---|
| D-15 | openapi.yaml 头部 "Version V2.1 / Updated 2026-06-29" 与 `info.version: 2.0.0` 不一致 | `CE docs/api/openapi.yaml` | 头部版本与 info 同步 |
| D-16 | zh-CN 文档缺 model-layering-design.md、rule-engine-design.md（docs/README 声明 en 权威，可接受但翻译不完整） | `CE docs/zh-CN/` | 择机补译 |
| D-17 | 既有审计文档两处失真：§7.7 记录的 `pkg/telemetry/http → pkg/api/httputil` 反向依赖已不存在；§7.4 记录 8 个「已删除死导出」中 5 个现又存在（getBuiltinTemplates/getChannel/probeAsset/updateProfile/healthCheck） | `CE docs/audit/frontend-backend-alignment.md:172,124` | 追加勘误注记（审计记录不改历史，补 addendum） |
| D-18 | x README 开发节 npm 表述（并入 D-6）；x web 无 README 前置说明 | `X README.md:33` | 随 D-6 一并修 |

**验证通过（文档一致性）**：默认端口 6153、配额数值（20 资产/20 探针/20 任务/5 处置/60s 间隔/10 万事件每日）、内置模板 4 个、执行器 5 种、通知渠道 email+webhook（含 TLS 三模式与三种 AUTH）、CLI 子命令、约 80 端点（实数 79）、20+ 事件类型（实数 27）、失败事件持久化、rule-engine-design.md 的路径翻新横幅与扁平化布局一致——均与代码吻合；2026-08 文档反超claim清理成果保持。

---

## 五、前后端集成与用户体验

### 5.1 验证通过（集成契约）

- **路由对齐**：CE 前端全部 API 模块 URL 与后端注册 1:1 对应，无「前端调用不存在端点」；后端未被前端使用的端点仅 template GET/POST/PUT 单条、`GET /prism/channels/:id`（合理保留，供外部/x 消费）。
- **humps 约定**：请求侧 snakeize（body+params）、响应侧 camelize（`CE web/packages/core/src/utils/request.ts:130-137`）；两个刻意例外均有据（refresh_token 字面量直发；EmailConfig 因 JSON 字符串内嵌无法转换而显式 snake_case，前端 `prism.ts:196-200` ↔ 后端 `pkg/prism/channel/config.go:39-44` 对齐）。契约测试锁定参数名（api-contract.test.ts）。
- **分页契约**：双侧全强制（详见 3.4 验证通过）；`PageData{items,total,page,size}` 与前端类型一致。
- **X-Tickraft-Task-Ref**：executor 派发盖章（`CE pkg/executor/internal/httputil/httputil.go:60-73`）、遥测上报回传 `task_ref`（`CE pkg/telemetry/http/listener.go:456-460`）、前端不涉（机器凭据）——闭环一致。
- **错误码**：40101/40100/40300 在 body-code 与 HTTP-status 双路径处理；码表 `CE pkg/errdefs/codes.go:12-27` 两侧一致；无裸数字码直达用户。

### 5.2 发现

| 编号 | 问题 | 位置 | 程度 | 建议 | 优先级 |
|---|---|---|---|---|---|
| U-1 | 错误消息语言混排：后端 message 全英文（handler 未走 i18n），UI 默认 zh-Hans 且直接透出原文——中文用户看到 "invalid page" 类英文串 | 后端例 `CE pkg/api/httputil/paging.go` 产出的消息；前端透出点 `CE web/packages/features/src/views/.../ChangePassword.vue:129`、`Login.vue:124`、`prism/channel/list/List.vue:124`、`ExprEditor.vue:219` | 中等 | 短期：前端拦截器对已知码映射 zh-Hans 文案；长期：后端 message 走 `pkg/i18n` 按请求语言产出 | P1 |
| U-2 | i18n 双机制并存：JSON bundle + 三处复制的 `tf(key, zhFallback, enFallback)` 内联回退（自述过渡态），部分 key 仍落到回退 | `CE web/packages/features/src/layouts/DefaultLayout.vue:165`、`BlankLayout.vue:86`、`AuthBrandPanel.vue:74` | 轻微 | 回退 key 补齐进 bundle 后删除 tf 三副本 | P2 |

### 5.3 UI/主题/响应式（验证通过为主）

- 验证通过：暗色模式经 `html[data-theme]` EP 桥接（`styles/element.scss:6-27`）；token 体系 8 文件全 `--tk-` 前缀；响应式断点 xs–xl（`useResponsive.ts:11-17`）由布局消费；EP 覆写限定 `.tk-app` 作用域；监控 active/passive 双模式与后端 mode 过滤、executor `address` 契约（Create host/url→address、Detail `config.address ?? host/url`）连贯；执行状态词表（success/failed/running/timeout）与资产状态词表（normal/abnormal/offline/unknown）分离清晰。
- 轻微：视图模板均走 `t()`，无硬编码中英文残留（验证通过）；跨浏览器未做系统性实测（本轮为静态审查，未跑多浏览器冒烟——见 §8 局限）。

---

## 六、代码精简与优化建议（未发布、无兼容包袱）

汇总移除清单（除注明外均建议直接删除，理由：项目无任何 release）：

1. **兼容垫片五件**（=C-15）：quota 别名、legacy 探针清理 shim、事件 operation 空值分支、AfterAfter 自愈、x TSDB 迁移脚手架。
2. **死代码**（=C-12/C-16/C-17）：ListenerRegistry HTTP 三成员；`listTasks`、`ExecutionStore.List`（若确认无外部消费者）；web/app 5 个调试脚本 + tsbuildinfo；CE mock 层保留（dev 工具，有消费者）。
3. **重复实现合并**（=C-13/C-14/U-2/C-26）：tagsFromMetadata、classifyStoreError 家族、tf() 三副本、x PageResult 16 副本。
4. **结构优化**：四件套漂移归位（=C-22）；超大组件拆分（=C-27）；`task.Engine`/`Manager` 接口语义收敛（=C-23）。
5. **依赖管理**：两仓 go.mod 均干净、无幽灵依赖（验证通过）；x 可关注 wazero/chromedp 等重依赖的按角色拆分构建（server 角色二进制可不链入 operator/浏览器依赖，缩短构建与镜像体积）——列为后续可选优化。

---

## 七、注释规范与质量

| 编号 | 问题 | 位置 | 程度 | 建议 | 优先级 |
|---|---|---|---|---|---|
| N-1 | 注释引用设计文档章节，违反「注释中性客观、不引特定章节」要求：CE 40 行 + x 103 行（grep `§` 计数）。代表性：headers.go 引 `00_tickraft_global.md §四.3.1`；alert/ports.go 引 `code-architecture.md §4.3.2`；task/report.go 三处引 repo 外的 `docs/modules/task.md §12`；user/doc.go、expr/doc.go、executor/judgment.go/env.go/lifecycle.go、remediation/env.go；前端 menu.ts:9、useResponsive.ts:8、useMenuFilter.ts:8、useColumnWidths.ts:13（引 `03_tickraft_frontend.md §4.3.1`）、DefaultLayout.vue、ExprEditor.vue、expr-builder/*.ts（部分引用 repo 外的 navigation-design.md）；x tenant/ports.go:1-13、asset/ports.go:4-13、license/errors.go:7、user/ports.go:26,41 等 | 详见左（完整清单可按 grep 再生成） | 中等（143 行、机械性批改） | 批量改写为中性自述（删「per §x.y / 见文档 §x」引注，保留语义）；被引文档不在 repo 内的尤其应去除指向；生成文件（quota_grpc.pb.go 等）豁免 | P1 |
| N-2 | 注释与实现相反/过时：recoverPanic 声称 "keep serving other telemetry streams"（实际该协程死亡即停摆，见 C-1）；`task/engine.go:52-55` Service/Manager/Engine 词汇漂移；listener 死接线注释（见 C-12） | `CE pkg/telemetry/engine.go:432-435`、`pkg/task/engine.go:52-55` | 中等（C-1 处随修复一并改） | 随对应修复同步改写 | P1 |
| N-3 | 既有审计文档失真两处（同 D-17，审计记录补 addendum 即可） | `CE docs/audit/frontend-backend-alignment.md:124,172` | 轻微 | 补勘误注记 | P2 |

**验证通过（注释）**：CE Go 零 TODO/FIXME；无注释掉的代码块；49 处 `//nolint` 全带理由；包级 doc 与 2026-08-27 扁平化重构一致（抽查 router/task/prism/worker）。

---

## 八、方法论与局限

- 本轮为静态审查 + 定向单测基线交叉验证；未运行多浏览器冒烟、未做长稳/混沌注入（C-1/C-4 的时序类缺陷建议修复后补压力回归）。
- 严重项 C-1、F-1 已由主评估人逐行人工复核确认；其余发现由勘察代理给出 file:line 证据、按置信度标注，报告内均已注明。
- 与 2026-08 前两轮审计对比：历史 P0/P1 全部保持修复状态（离线检测路径、探针并发、超时统一、黑名单持久化、探针记录域分离均验证有效），本轮无回归。

## 九、汇总与优先级总览

| 级别 | 数量 | 编号 |
|---|---|---|
| 严重（P0） | 8（已全部修复，见「修复状态」） | C-1（遥测循环停摆）、F-1（SaaS mock 页）、D-1..D-4（openapi 四错）、D-5（x 二进制名）、D-6（x CI 前端 job） |
| 中等（P1 为主） | 23（已全部修复，见「修复状态」批次表） | F-2、F-6、F-13、C-2..C-6、C-12..C-15、C-17、C-18、A-2、D-7..D-14、U-1、N-1、N-2 |
| 轻微（P2） | 30+ | A-1、A-3、F-3..F-5、F-7..F-12、C-7..C-11、C-16、C-19..C-27、D-15..D-18、U-2、N-3 |

建议处置顺序：**第一批** C-1（核心可用性）→ D-1..D-6（对外契约与 CI，纯文档/配置零风险）→ **第二批** F-1（x SaaS 接线，工作量最大）→ **第三批** P1 代码正确性簇（C-2..C-6、C-18）与清理簇（C-12..C-15、C-17、N-1）→ P2 择机。

---

*本报告位于 docs/audit/（CE 红线扫描豁免目录）；红线扫描与 license 扫描本地重放双仓均通过（2026-08-28）。*
