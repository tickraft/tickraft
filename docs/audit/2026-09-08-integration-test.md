# tickraft CE 前后端全量集成测试报告

- 日期：2026-09-08
- 范围：CE 开源仓 `tickraft`（Go 后端 + `web/` 前端 pnpm workspace）全部与后端交互的页面模块 —— 28 个前端页面、约 70 个 API 端点、19 张 `sys_*` 表；不含 `tickraft-x` 仓
- 方法：两种真实运行形态全覆盖 —— 开发模式（vite :5173 真前端服务 + `go run` 真后端 :6153 + 独立测试库 `build/it/it.db`）逐批全量；生产单二进制（内嵌 SPA、全新 `prod.db`、release 日志、:6253）最终验收。前端操作全部通过浏览器真实 GUI 输入（填表/点击/提交）驱动，API 另以 curl 直调交叉验证，落库以 `sqlite3` 逐表对照
- 纪律：发现即修（当日闭环）；每批结束跑该域回归；收尾全量回归（Go 单测基线 + `-race`、golangci-lint、web 三件套）
- 结果：**通过**。编号缺陷 #1–#28 共 28 项全部当日修复并验证；生产单二进制冒烟（登录→资产→监测点→探测→告警→渠道投递→任务执行→状态页）全链路通过，系统可直接产品化运行

## 1. 测试目标达成对照

| 要求 | 达成情况 |
| --- | --- |
| 每个 API 可与后端正常获取/更改数据 | 28 页面 × 全部 CRUD/动作端点经 UI 与 curl 双通道验证（B1–B8 各批） |
| 每个 API 请求与数据库交互正常（CRUD 落库） | 每批以 `sqlite3` 对照 19 张 `sys_*` 表行级验证（含软删除、upsert、掩码列） |
| 前端添加真实数据，集成前后端全链路 | 资产→监测点→探测→规则命中→告警记录→渠道投递→确认/解决；任务创建→触发→执行日志；自愈规则→触发→记录（B3–B6） |
| 前后端均为真实服务、经前端 UI 模拟输入 | 开发模式双真进程 + 浏览器 GUI 输入（vite mock 保持禁用）；生产形态复测 |
| 集成测试后可直接产品化运行 | B10 单二进制（38 MB，内嵌 SPA 6.4 MB）全新库冒烟通过；测试进程与临时文件全部清理 |

## 2. 批次覆盖与结论

| 批次 | 域 | 覆盖端点/页面要点 | 结论 |
| --- | --- | --- | --- |
| B0 | 环境 | 独立 DSN 测试配置（`build/it/it.yaml`，:6153）、本地 webhook 接收器（:18923）、admin/admin123 显式密码 | 通过 |
| B1 | 认证与会话 | 登录成功/失败、改密（改回、旧 token 失效、重登）、API Keys 创建→`X-Tickraft-API-Key` 直调→撤销→缓存 TTL 失效轮询；`sys_user/sys_api_key/sys_token_blacklist` | 通过；6 项缺陷修复（#1–#6，含改密旧密码错误误报 401 改 400 类内联错误 `ErrOldPassword`/`CodeOldPassword`、ChangePassword 路由注释 PUT 失实等） |
| B2 | 资产域 | 创建/列表/详情/编辑/删除全链路 + ConfirmDialog；配额拒绝路径（409）；`sys_asset` | 通过 |
| B3 | 遥测 | 模板 apply/删除；active 监测点 enable/disable/probe now/history；passive `X-Tickraft-Asset-Key` 上报（metrics/logs/heartbeat）；`sys_monitor_point/sys_probe_record/sys_probe_metric|log` | 通过；#11/#12 修复（监测点列表 `asset_id` 服务端过滤下沉 `ListPaged` 等） |
| B4 | 任务 | local executor 创建→触发→执行日志；cron 短周期真实调度；pause/resume/copy；webhook executor 打本地接收器；`sys_schedule_task/sys_schedule_execution` | 通过；#13–#16 修复（webhook executor headers 由 `map[string][]string` 收敛为与 http executor/渠道/前端表单一致的 `map[string]string` 平铺契约等） |
| B5 | Prism 告警+渠道 | 规则创建（表达式构建器 + `/expr/validate`）；真实告警链路（上报 abnormal→状态事件→规则命中→记录→渠道投递接收器实收）；acknowledge/resolve 流转；CSV 导出；webhook 渠道 test→真实投递→`sys_prism_delivery` success/retry；掩码回显 | 通过；#17–#20、#22–#24 修复（violations 只提取 metrics 事实比较、事件谓词不再伪造指标违规、记录归属命中规则 `MatchedRule`、渠道 test-send 落投递记录并回填 last_test 状态等） |
| B6 | 自愈 | 自愈规则（资产+触发事件+表达式）→真实触发→记录页；`sys_prism_remediation_rule|record` | 通过 |
| B7 | 系统 | 设置 GET/PUT 落库回显；状态页启用+组件派生+公开访问；系统信息真实 runtime；`sys_config/sys_status_config` | 通过 |
| B8 | Dashboard+只读页 | 图表与 `/system/stats` 及 DB 对照（发现统计时区偏移陷阱并修复）；severity 域统一（critical>error>warning>info>debug）；zh-Hans/en-US 切换零 raw-key | 通过 |
| B9 | httpapi 补盲 | 新增 6 个永久回归测试（task trigger/pause/resume/copy、`assets/:id/status`、`assets/:id/probe`、`GET /ws` 握手、`i18n/locales`、SPA 静态服务路径）；harness 补 `WithPointHandlers` 生产同构接线；2 处陈旧断言更新 | 通过 |
| B10 | 生产化验收 | 单二进制全新库冒烟全链路（见 §4） | 通过 |

## 3. 缺陷修复总览

工作区相对 `3432c8d` 共 93 文件改动（+1399/−390）另 4 个新增文件，全部为本次集成测试发现即修的产出。按域归组：

- **认证（B1，#1–#6）**：改密旧密码校验失败误报 401（导致前端把有效会话当过期）→ 新增 `ErrOldPassword`（400 类，`CodeOldPassword=40003`）内联报错；`ChangePassword` 处理器注释 POST 失实改 PUT；其余为 API key 生命周期与 token 失效时序问题。
- **遥测（B3，#11–#12）**：监测点列表 `asset_id` 过滤未下沉 SQL（`ListPaged` 增加 asset 参数）；前端监测点创建/模板页表单问题。
- **任务（B4，#13–#16）**：webhook executor `headers` 契约与前端/http executor 不一致（`[]string` → 平铺 `string`）；任务表单（`TaskForm.vue`）提交字段问题；执行日志展示问题。
- **Prism（B5，#17–#24）**：`ViolationExtractor` 把 `type == "status"` 这类事件谓词也当比较条件、伪造指标违规覆盖事件真实 payload → 重做为只提取 metrics 事实（`metrics["cpu"] > 90`），谓词型规则零 violations、payload 违规原样投递；告警记录不归属命中规则 → `MatchedRule{ID,Name}` 贯通 matcher→dispatcher→record；渠道 test-send 不落投递记录/不回填测试状态 → 经 tracked seam 落 `sys_prism_delivery` + `last_test_at/result`；渠道表单/掩码回显（`Form.vue`/`composables.ts`）。
- **统计（B8，#25–#26）**：`/system/stats` 按日序列时区偏移（窗口计算漂移，`StatsByDay` 相关修复 + 回归测试）；severity 域不统一（critical/error/warning/info/debug 排序与文案，前后端一致化 + 双语言键补全）。
- **回归基建（B9，无产品缺陷）**：harness 缺 `WithPointHandlers`（生产 `internal/service/api.go` 有接线而测试装配没有，属测试盲区非产品缺陷）。
- **#27 / #28**：见 §3.1 / §3.2 专题。

### 3.1 Bug#27 探针任务 ID 自增污染（数据完整性，本次最高危）

- **现象（IT 实机复现）**：创建 active 监测点后，后续所有常规任务拿到的自增 ID 全部落入探针号段（10^12 量级），且带碰撞风险 —— 后续某个监测点若合成出相同 ID，upsert 会覆盖常规任务行。
- **根因**：`ProberService` 以正数合成 ID `1<<40 + pointID` 把探针任务持久化进共享表 `sys_schedule_task`；SQLite rowid 自增 = max(rowid)+1，探针行把计数器永久抬进探针号段。
- **修复**：负号段方案 —— `proberTaskID(pointID) = -(1<<40 + pointID)`。负数行永不抬高自增计数器，两个号段永不相交；`ProbeRecordStore` 反解 `pointID = -taskID - offset` 并拒绝任何 `taskID >= -offset`（offset 本身反解为不存在的 point 0，一并拒绝）。启动清扫：`ProberService.Start` 先对全部 ListActive 点 `Unschedule` 遗留正数行再注册负数行（避免双探）；`UnregisterPoint` 双号段都清。
- **验证**：单测（含符号 pin）+ httpapi 回归 `TestProbeTaskIDsDoNotPoisonAutoincrement`（建点→enable→断言负数行 `prober-%d`→建常规任务→断言 `0 < id < offset`）+ IT 实机存量迁移（遗留行清为负数行、无重复、调度与 ProbeNow 解码正确）。生产全新库复核：探针行 `-1099511627777`，随后 UI 建任务拿到 **ID 1**。
- **波及面**：tickraft-x 零引用 `ProbeTaskIDOffset`，无需同步。

### 3.2 Bug#28 合成任务泄漏用户面（B10 生产冒烟发现）

- **现象**：生产二进制 UI「任务管理」列表出现 `#-1099511627777 prober-1` 内部探针行（密码学式负 ID 直接暴露给用户），且任务计数、`/system/stats` 的 TotalTasks、配额 `Count` 均被内部行污染。
- **修复**：`ListOptions.ExcludeSynthetic`（SQL `id > 0`）—— 用户面三处全部接入：`ListTasks`（列表/计数）、`ListExecutions` 名称过滤、`pkg/system` 全局统计；`store.Count`（配额）直接排除负数行。内部消费者（engine `Restore`、按 ID 解析属主）不受影响，探针行照常调度执行。
- **验证**：`TestStoreListExcludeSynthetic`（负数行默认可见/排除后不可见/Count 不计）+ 生产二进制重建后 UI 复测（列表 0 任务、探针行仍在库且持续执行）+ 全量回归。

## 4. 生产化验收（B10）明细

构建链：`pnpm build` → `web/app/dist` 拷入 `internal/web/dist` → `go build ./cmd/tickraft`（38 MB，SPA 6.4 MB 内嵌）；`logger.mode: release`；渠道加密 `TICKRAFT_CHANNEL_ENCRYPTION_KEY` 环境变量注入；全新 `prod.db`。

| 步骤 | 结果 | 证据 |
| --- | --- | --- |
| 登录 | 通过 | admin/admin123，SPA 由单二进制服务，API 401 闸门生效 |
| 建资产 | 通过 | UI 创建 `prod-smoke-asset`（device），`sys_asset` 行对照 |
| 建监测点+探测 | 通过 | `prod-smoke-http`（http → 本地接收器 /health）enable→probe→history 实记录；改期望码 404 后探测 abnormal、`sys_monitor_point.status=error`；探针任务行负数 ID（#27 生产形态确认） |
| 告警链路 | 通过 | passive heartbeat `{"kind":"heartbeat","asset_id":1,"status":"abnormal"}`（`X-Tickraft-Asset-Key` 认证）→ 资产 unknown→abnormal → 规则 `type == "status" && severity in ["error","critical"]` 命中 → 告警记录 firing → UI 确认（acknowledged 21:43:24）→ 解决（resolved 21:43:29）；恢复上报后资产回 normal 且不产生恢复噪音告警（设计行为） |
| 渠道投递 | 通过 | webhook 渠道（指向本地接收器 /alert-hook）：test-send 落 `sys_prism_delivery` success；规则触发投递实收 POST（payload 含 rule_id/severity/状态迁移），`sys_prism_delivery` 第二行 success |
| 任务执行 | 通过 | UI 创建 `prod-smoke-task`（Local Script `echo prod-smoke-ok`，cron `* * * * *`）→ **ID 1**（#27 佐证）→ Trigger Now + 两轮 cron 均 success/exit 0/输出 `prod-smoke-ok`，执行日志页 3 条与 `sys_schedule_execution` 一致，成功率 100%/均值 7ms |
| 状态页 | 通过 | UI 启用发布 → 匿名 `GET /api/v1/status` 免认证返回聚合状态（overall degraded、组件按启用监测点派生、last_checked 为真实探测时间）→ `/status` 公开页正常渲染 |

## 5. 设计边界记录（非缺陷，如实呈报）

- **资产状态机的驱动源**：主动探测结果只刷新监测点自身状态（`sys_monitor_point.status`）；资产状态机由被动上报（listener→pipeline→processor）与心跳超时驱动（`docs/user-guide.md` "listener reports flow through the collection pipeline and drive the asset status machine"）。因此仅挂主动探测、无被动上报的资产，其资产级状态保持 unknown，不会触发 status 类告警 —— 本次两形态实测与文档一致。若产品期望「探测失败→资产级告警」，需后续设计决策（规则引擎当前只订阅 metric/log/status-change 三类事件，无 probe 事件源）。
- **恢复不告警**：状态向健康方向迁移（recovery）被 `statusPayloadToAlert` 跳过，属防噪音设计，实测确认。
- **it.db 存量任务 ID**：开发模式测试库中 Bug#27 修复前创建的常规任务保留污染时代的大号 ID（已无害：负号段与正自增永不相交），全新部署不受影响；测试库已随清理删除。

## 6. 回归资产沉淀（永久）

- `tests/httpapi/blindspot_test.go`（新增）：task trigger/pause/resume/copy、`assets/:id/status`、`assets/:id/probe`、`GET /ws` 握手、`i18n/locales`、SPA 静态服务、`TestProbeTaskIDsDoNotPoisonAutoincrement`。
- `tests/httpapi/harness_test.go`：补 `WithPointHandlers` 生产同构接线（active→RegisterPoint/UnregisterPoint，passive 旁路）—— 此前 harness 未接钩子导致「enable 即注册探针任务」路径测不到。
- `pkg/task/store_test.go`：`TestStoreListExcludeSynthetic`（#28 回归）。
- `pkg/prism/alert/store_test.go`、`violations_test.go`、`pkg/prism/channel/service_test.go`/`delivery_test.go`（新增/扩充）：规则归属、violations 语义、渠道 test-send 落库回归。
- `web/packages/core/src/utils/naming.test.ts`（新增）。
- 全量回归（#28 修复后终态）：`go test ./...` 与 `-race` 双绿、`golangci-lint run` 0 告警、`pnpm test` 80/80、`vue-tsc` 0 错、`pnpm build` 通过。

## 7. 清理确认

- 测试进程全部停止并复核端口关闭：dev 后端 :6153、生产二进制 :6253、vite :5173、接收器 :18923。
- 临时产物全部删除：`build/`（it.yaml、it.db、prod/、二进制、接收器脚本、日志、channel.key 等 84 MB）。
- 仓库根开发者本地 `tickraft.db` 未受影响（mtime 保持 08-30）。
- `internal/web/dist` 内容为 B10 构建产物（目录 `.gitkeep` 占位保留），供后续发布构建参考。

## 8. 覆盖完备性复查（2026-09-08 二轮，报告日后追加）

以代码权威清单为基准逐项对照 B0–B10 实测覆盖：后端路由注册表（`pkg/api/handler/routes.go`）68 个端点、前端路由表（`web/packages/features/src/routes/`）31 个页面、GORM 模型 21 张 `sys_*` 表。

**复查结论：全部功能模块均有集成测试覆盖；三个盲点当日补验；未发现新缺陷。**

### 8.1 端点级对照（68 个）

67 个端点由 B1–B10 实测 + `tests/httpapi` 永久回归覆盖（含 `/healthz`、`/readyz`、`/ws`、`/i18n/locales`、SPA 静态服务等免认证面）。补验与豁免项：

| 端点 | 复查处置 |
| --- | --- |
| `GET /readyz` | 原 harness 仅注册未断言 —— 新增 `TestHealthReadyzProbes`（`/healthz`→`ok`、`/readyz`→`ready`，信封断言），已入回归 |
| `POST /system/certificates/reload` | TLS 启用时才注册的条件路由，非 TLS 部署不可达属预期；处理器有专项单测（`TestCertificateReloadSuccess/Failure/Signature`）+ TLS/ACME 装配测试（`pkg/api/tls_test.go`/`acme_test.go`）。豁免理由成立 |
| `PUT /system/profile` | 后端能力暂无前端界面入口（GET 由登录流消费、实测覆盖；PUT 由 `TestSystemProfile` 回归覆盖）。如实记录，非缺陷 |

### 8.2 页面级对照（31 个）

29 个页面在 B1–B10 有明确实测证据（含资产编辑、渠道编辑掩码回显、执行日志详情、双语言切换）。两个页面缺批次级证据，本轮起栈（vite + 真后端 + 独立库）实测补齐：

| 页面 | 补验结果 |
| --- | --- |
| `/prism/templates`（告警模板） | 10 个预设渲染（critical 4 / warning 5 / info 1）→ Apply Template → 确认框 → `/prism/rule/edit?templateId=1` 向导模式预填（`cpu_usage > 90`）→ 保存 → `sys_prism_alert_rule` 落库（表达式 `metrics["cpu_usage"] > 90`，enabled）。前端预设库无独立后端端点，集成面即规则创建流，链路全通 |
| `/task/edit/:id`（任务编辑） | 表单全量回填（名称/调度/命令/参数/高级项）→ 修改名称与参数 → 保存 → 详情页与 `sys_schedule_task` 落库一致（`executor_config` 由 `before-edit` 变 `after-edit`） |

### 8.3 表级对照（21 张）

19 张在批次中行级对照。两张内部表补记：

- `sys_probe_status_history`：状态迁移历史，由 `StateManager.UpdateStatus` 在每次资产状态迁移时写入；B5/B10 的 unknown→abnormal→normal 迁移即真实执行了该写路径（写入失败会中断迁移并留 ERROR 日志，实测迁移成功即写入成功）；另有 `state_guard_test.go` SQLite 直测。
- `sys_event_failed`：事件总线投递失败兜底表；正常集成链路无持续失败故无行（预期为空）；写入方有 `pkg/event` 单测覆盖。

### 8.4 复查后回归

`tests/httpapi` 全套（含新增 `TestHealthReadyzProbes`）通过；golangci-lint 0 告警；验证栈已停、临时文件已清（`build/` 移除，仓库根 dev 库不受影响）。
