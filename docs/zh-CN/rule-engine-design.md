# 规则引擎统一设计（Rule Engine Design）

> 本中文文档仅供参考，请以英文文档为准。
> Chinese translation is for reference only; the English documentation is authoritative.

> **路径注记（2026-08-26，2026-08-27 更新）**：文中 `internal/api/service/...` 为设计时路径快照；这些实现现位于 `pkg/prism/{alert,channel,remediation}`、`pkg/telemetry`、`pkg/task`、`pkg/system`（服务层下沉域包后，service 子包已于 2026-08-27 扁平化入各域根，SPI 在各域 `ports.go`、实现在 `service.go`）。历史发现编号与行号保留原貌。

> 状态：**设计评审稿**。本文档是规则机制重构的权威设计，经完全确认后才进入代码实施。
> 适用范围：`pkg/expr`（新）、`pkg/prism/alert`（由 `pkg/prism/rule` 迁移并合并为单包）、`pkg/prism/remediation`、`pkg/executor`、相关 API 与前端，以及 **tickraft-x 的同步更新**（第 12 章）。
> 前提：tickraft 与 tickraft-x 均未发布，**两仓都不考虑任何兼容性**，直接按最优架构变更。

---

## 目录

1. [背景与问题](#1-背景与问题)
2. [目标架构](#2-目标架构)
3. [表达式语言规范](#3-表达式语言规范)
4. [求值环境（ENV）设计](#4-求值环境env设计)
5. [函数策略](#5-函数策略)
6. [三个消费面设计](#6-三个消费面设计)
7. [持久化设计](#7-持久化设计)
8. [API 与校验变更](#8-api-与校验变更)
9. [前端变更](#9-前端变更)
10. [缺陷修复映射表](#10-缺陷修复映射表)
11. [测试计划与实施顺序](#11-测试计划与实施顺序)
12. [tickraft-x 同步设计](#12-tickraft-x-同步设计)
13. [需求覆盖评估](#13-需求覆盖评估)

---

## 1. 背景与问题

### 1.1 现状：三套各自为政的"规则"机制

| 机制 | 位置 | 求值引擎 | 持久化 |
|---|---|---|---|
| 告警规则 | `pkg/prism/rule` | expr-lang 场景化编译器（自带沙箱、自定义函数、比较数上限） | `sys_prism_rule`（scene + expression） |
| 自愈规则 | `pkg/prism/remediation` | 包内私有 `compileCondition`：**复制粘贴**的平行沙箱（白名单注释自述 "mirrors the rule engine's builtinWhitelist"） | `sys_prism_remediation_rule`（trigger_event_type + condition_expr） |
| 执行器状态判定 | `pkg/executor/*` | **无表达式**，各执行器硬编码 | 无表（http 有 `expect_status` 配置，webhook 没有） |

两套 expr-lang 沙箱已经语义漂移：告警侧有 4 个自定义函数与比较数上限 3，自愈侧都没有；自愈侧求值环境（扁平 `EventContext`）无资产富化，告警侧有。执行器侧 webhook 硬编码 2xx 而 http 支持 `expect_status`；local 执行器的**退出码数值被直接丢弃**（`Result` 无对应字段）。

### 1.2 已核实的缺陷清单

分级：P0 = 功能错误（规则静默失效），P1 = 高危潜伏，P2 = 并发/资源风险，P3 = 小问题。

| 编号 | 级别 | 缺陷 | 位置 |
|---|---|---|---|
| D-01 | P0 | PUT 自愈规则清空 `status/metadata/last_run_at`：DTO 转换器不携带运行态字段，`Store.Update` 用 `Save` 全列覆盖 → status 清空后 `handle()` 的 `Status != "active"` 检查**永久静默跳过该规则**，同时熔断计数清零、冷却重置 | `internal/api/service/prism/remediation.go` / `pkg/prism/remediation/store.go` |
| D-02 | P1 | PUT 告警规则清空 `tenant_id/group_id/metadata`（同样的 Save 全列覆盖模式） | `internal/api/service/prism/alert.go` / `pkg/prism/rule/store.go` |
| D-03 | P1 | `prism.eval_interval` 死配置：配置校验强制非零，但装配层从未传入 `rule.Config.EvalInterval`，轮询重载循环不启动 → 绕过 API 直接改 DB 的规则变更永不生效 | `internal/service/prism.go` / `pkg/prism/rule/register.go` |
| D-04 | P1 | `scene=remediation` 死通路：编译器视其为合法场景、API 可创建、引擎会加载，但 `MatchRemediation` 无任何生产调用方——**用户创建后静默无效** | `pkg/prism/rule`（SceneRemediation 全链路） |
| D-05 | P1 | task/probe 场景空转：`TaskMatcher`/`ProbeMatcher` 在 CE 与 tickraft-x 两仓库均无生产接线 | `pkg/prism/rule/matcher.go` |
| D-06 | P1 | 自愈 `condition_expr` 无入口校验：坏表达式运行时编译失败 → warn + 返回 false，规则静默永不触发 | `internal/api/service/prism/remediation.go` |
| D-07 | P2 | `matchCache` 无界增长：按 `ruleID:expr` 只增不减，改表达式/删规则后旧编译产物永不回收 | `pkg/prism/remediation/manager.go` |
| D-08 | P2 | 熔断计数读-改-写竞争：基于快照修改 metadata JSON 整体写回，同规则并发执行丢更新 | `pkg/prism/remediation/manager.go` |
| D-09 | P2 | 幂等门控 TOCTOU：`checkGates` 检查与 `dispatch` 置位之间有窗口，安全性仅依赖 channelBus 单消费者实现细节 | `pkg/prism/remediation/manager.go` |
| D-10 | P2 | payload 转换 `strconv.ParseInt` 全部忽略错误：非法 asset_id 落 0 后命中所有全局规则 | `pkg/prism/remediation/manager.go` |
| D-11 | P2 | 多租户缺口：引擎 `Reload` 固定 tenant 0 跨租户加载，求值不按事件租户过滤 | `pkg/prism/rule/engine.go` |
| D-12 | P2 | Store 校验器与引擎编译配置可能不一致：`migrateStores` 用默认编译器，忽略 `cfg.RuleConfig.CompilerConfig` | `pkg/prism/config.go` |
| D-13 | P3 | 自定义 `regex()` 每次调用重新编译正则（本设计将整体移除该函数） | `pkg/prism/rule/functions.go` |
| D-14 | P3 | Violation 提取丢失 `severity/source`：Dispatch 用提取结果整体替换 `evt.Violations` 后排序/渲染缺信息 | `pkg/prism/rule/violations.go` |
| D-15 | P3 | 自愈 `ExecutionRequest` 从不设置 Timeout，永远落到执行器默认值 | `pkg/prism/remediation/manager.go` |
| D-16 | P3 | webhook 执行器无 `expect_status`，与 http 语义不一致 | `pkg/executor/webhook/webhook.go` |
| D-17 | P3 | 残留中文调试注释 | `pkg/prism/remediation/manager.go:116` |

### 1.3 结构性问题

- **A-1 平行实现**：同一沙箱策略两处维护、已经漂移（见 1.1）。
- **A-2 场景机制空转**：`sys_prism_rule` 名义上是"四场景通用规则表"，实际只有 metric（告警）场景接线。API 路径就是 `/prism/alert/rules`，服务层是 `AlertService`，前端页面是"告警规则"——**名实不符**。
- **A-3 执行判定不可配置**：健康检查返回 200 但 body 为降级状态、脚本 exit 0 但输出含错误等真实场景无法表达。

---

## 2. 目标架构

### 2.1 分层：一个内核，三个领域面

```
pkg/expr                        ← 新包：领域无关的表达式求值内核
  compiler.go   沙箱编译（MaxNodes/AsBool/Env 类型检查）
  cache.go      有界 LRU 程序缓存（并发安全）
  errors.go     哨兵错误
        │ 被委托
  ┌─────┼──────────────────┬────────────────────────┐
  ▼     ▼                  ▼                        ▼
pkg/prism/alert       pkg/prism/remediation   pkg/executor(runner)
告警域单包：事件模型    自愈：门控+动作派发        执行结果判定
+规则引擎+规则存储
AlertEnv 求值环境       RemediationEnv 求值环境   ExecutionEnv 求值环境
sys_prism_alert_rule   sys_prism_remediation_rule  执行器配置 JSON 的可选 expression 键
                       expression = 触发条件        (sys_monitor_point.config /
                                                   remediation executor_config)
```

职责边界原则：

- **公共的**："表达式怎么编译、怎么求值、怎么缓存"——`pkg/expr`。
- **领域的**："什么时候求值、命中后做什么"——三个消费面各自实现：
  - 告警 = 事件流过滤 + Violation 提取；
  - 自愈 = 事件触发 + 幂等/冷却/熔断门控 + 动作派发；
  - 执行判定 = 单次执行返回后、重试决策前的同步判定。

**不做大一统规则引擎**：三者求值时机与生命周期完全不同（自愈规则还带必须新鲜的运行态：冷却/熔断/in-flight），强行统一编排层会造出上帝对象。

**每个消费面恰好一个求值环境**（第 4 章），场景（scene）机制整体取消。

### 2.2 `pkg/prism/rule` 定位终结：合入 `pkg/prism/alert`（单包、单模型、单次求值）

`rule` 包最初的定位是"提供给其他需要规则的包使用的通用规则引擎"。本设计落地后该定位不复存在：

- 通用能力（编译/求值/缓存）已下沉 `pkg/expr`；
- 已核实该包在 CE 内的全部真实消费方均为告警链路或装配点：`pkg/prism/config.go`、`pkg/prism/engine.go`、`internal/service/prism.go`、`internal/service/migrate.go`、`internal/api/service/prism/alert.go`（`pkg/prism/alert/model.go` 与 `pkg/prism/remediation/doc.go` 仅注释提及，无 import）；
- 场景机制删除后，包内只剩告警域逻辑（`AlertEnv`、`AlertMatcher`、Violation 提取、`sys_prism_alert_rule` 存储）。

实施分两步：先按子包形态迁至 `pkg/prism/alert/rule`（迁移期过渡），最终**整体并入 `pkg/prism/alert`**。迁移期的嵌套子包与可选接口（`ViolationMatcher` 等）只是过渡形态，最终形态完成三件事收敛：

- **单包**：编译器、引擎、规则存储、匹配器、Violation 提取与告警事件模型、派发契约同属一个 `alert` 包，不再有嵌套子包，原 `alert/rule → alert` 的依赖方向问题随之消解；remediation 与 executor 依旧不依赖告警包。
- **单模型**：持久化模型（原 `rule.Record`）与运行时视图（原 `rule.Rule`）合一为 `alert.Rule`——GORM 标签内联（表 `sys_prism_alert_rule` 不变），`Metadata` 保持 string 列、按需经 `Rule.MetadataMap()` 解码（坏 JSON 返回 nil，容忍历史脏行），`Spec`（配置文件静态规则）保留且 `Register` 直接构造 `Rule`（静态规则用负数 ID）。
- **单次求值**：`alert.Matcher` 为单方法契约 `Match(ctx, evt) MatchResult`，`MatchResult{Forward bool; Violations []Violation}`；引擎收敛为 `Engine.Evaluate(ctx, tenantID, env) (matchedIDs []int64, violations []Violation)`——一次快照、一次循环，每条规则的表达式程序恰好运行一次，Violation 仅为命中规则构建；无规则时默认放行的语义保留（见 6.1.3）。

连带变更：

- Go import 直接引用 `pkg/prism/alert`（CE 上述 5 处；tickraft-x 见 12.1）；
- 包内核心符号（`Rule`/`Store`/`Engine`/`Config`/`Register`/`NewCompiler`/`ErrRuleNotFound` 等）名不变，消费方只改 import 行；
- 原"remediation 复用 `rule.AssetEnv`"的设想取消：资产域改由各消费面自带私有构造（4.4），避免 remediation 依赖告警包。

### 2.3 `pkg/expr` 与 expr-lang 的包名冲突

两者包名均为 `expr`。约定：

- `pkg/expr` 内部文件引用 `github.com/expr-lang/expr` 及其子包时统一别名 `exprlang`；
- `pkg/expr` 对外返回自有不透明类型 `expr.Program`，**消费方不需要 import expr-lang**（唯一例外：`pkg/prism/alert/violations.go` 的 AST 遍历属领域逻辑，直接引用 expr-lang 的 `ast/parser`，同样用 `exprlang` 别名）。

### 2.4 依赖版本

维持 `github.com/expr-lang/expr v1.17.8`。已核实：v1.17.8 内置函数共 71 个，**无正则函数**——正则匹配由文法级 `matches` 运算符提供（与 `contains`/`startsWith`/`endsWith` 同族，见 5.1），因此无需升级依赖，也无需自定义正则函数。

### 2.5 tickraft-x 定位

tickraft 是 tickraft-x 的**基础设施**：x 通过 `replace => ../tickraft` 直接复用 CE 的 `pkg/*` 内核，且 x 侧存在自己的平行规则业务（自建 remediation 引擎、worker 规则分析器、结构化前端编辑器）。本次重构**必须同步更新和支持 x 的业务逻辑**，而不是事后 rebase——完整方案见第 12 章。

tickraft-x 当前对 `pkg/prism/rule` 的引用面（7 个文件，随 2.2 的包合并统一改 import 到 `pkg/prism/alert`，核心符号 `Rule`（单一模型，原 `Record` 并入）/`Store`/`Engine`/`Config`/`Register`/`NewCompiler`/`CompilerConfig`/`ErrRuleNotFound` 全部保留）：gRPC 规则拉取、API 规则 CRUD（CE 服务的本地拷贝）、Prism 编排（`BuildRuleConfig`）、分布式 Redis 同步、Worker 规则源。受影响的 schema 与隐式约定详见 12.1。

---

## 3. 表达式语言规范

### 3.1 命名规范（三级）

1. **单词优先，顶层小写**：`severity`、`metrics`、`body`、`error`、`code`、`duration`、`trigger`、`threshold`、`level`、`keyword`、`content`、`source`、`type`。
2. **系列字段归入一层名词域**：
   - `asset.id`、`asset.name`、`asset.type`、`asset.tags`
   - `metric.name`、`metric.value`
   - `status.previous`、`status.current`（全称，不用缩写 prev/curr）
3. **引用深度上限**：两级属性 + 一层索引——`asset.tags["env"]`、`metrics["cpu"]` 是合法上限。
4. **域对象双引用**：名词域对象（`asset`/`metric`/`status`）在实现上以 map 承载（见 4.4），`.field` 与 `["field"]` 两种引用方式等价——`asset.id` ≡ `asset["id"]`；文档示例统一用点号风格。

禁止事项：

- 禁止 camelCase **变量名**；**函数名**遵循 expr-lang 标准拼写——小写函数为主，`startsWith`/`endsWith`/`fromJSON` 等标准 camelCase 名称可用，但文档示例一律用小写等价形式（如用 `matches "^prefix"` 代替 `startsWith`）；
- 禁止三层及以上属性链；
- 禁止同义别名（现状 `alert.xxx`/`event.xxx` 双前缀全部取消，每个变量只有一个名字）；
- 禁止在表达式中引用 `tenant_id` 等内部字段（租户过滤是引擎内部行为，见 6.1.5）。

### 3.2 字面量与运算符

- 字面量：整数、浮点数、双引号字符串、`true/false`、数组 `[...]`、映射 `{"k": v}`、`nil`。
- 算术：`+ - * / %`（字符串 `+` 为拼接）。
- 比较：`== != > >= < <=`。
- 逻辑：`&& || !`。
- 成员：`in`（数组/映射键）。
- 字符串运算符：`matches`（正则）、`contains`（子串）、`startsWith`/`endsWith`（前后缀）。
- 索引：`metrics["cpu"]`、`asset.tags["env"]`；域对象字段双引用，`asset["id"]` 与 `asset.id` 等价。
- 时间：`now()` 当前时间、`duration("5m")` 时长字面量（配合比较使用）。

### 3.3 沙箱约束（最小约束集）

对 expr-lang 能力面的取舍结论：**不设内置函数白名单、不设比较数上限，仅保留三条结构性约束**。

| 约束 | 值 | 说明 |
|---|---|---|
| `MaxNodes` | 1000 | AST 节点数上限，防超长表达式拖慢编译与求值 |
| 结果类型 | 强制 bool | `AsBool` 编译期断言，非布尔表达式编译失败 |
| Env 类型检查 | 开启 | 编译期校验顶层变量存在性与类型（分层细节见 4.4） |

评估依据：

- **取消白名单（原 `DisableAllBuiltins` + 启用清单）**：表达式作者必然是持有配置权限的管理员，不是不可信输入；expr-lang v1.17 全部 71 个内置均为纯计算函数，无 I/O、无副作用；白名单的历史教训是维护成本与两仓漂移（A-1），收益只是"语言面收敛"。完全放开后用户可直接对照 expr-lang 官方文档使用全部标准能力，本项目只为"推荐风格"维护文档（5.2）。
- **取消比较数上限（原值 3）**：该上限只限制 `> >= < <=` 出现次数，无安全价值（表达式复杂度已由 MaxNodes 控制），却让 `a > 1 && b > 2 && c > 3 && d > 4` 这类合理表达式被拒，是用户困惑源。tickraft-x 已用 `MaxComparisons: -1` 放开并稳定运行，本次两仓统一取消（12.5）。
- **保留三条**：分别防误粘贴超长文本、保证判定语义（表达式必须是谓词）、把变量拼写错误挡在入口校验。

### 3.4 统一结果码 `code` 与时间单位

- `code`：http/webhook 执行器 = HTTP 状态码；local = 进程退出码（`Result` 新增 `ExitCode` 字段承载）；tcp/icmp = 0（连通性失败由默认语义或 `error`/`metrics` 判定）。
- 时间单位统一为**毫秒**（`duration`、`duration_ms` 语境），前端提示与本文档一致标注。

### 3.5 字段命名约定（列名/API 字段）

| 位置 | 名称 | 语义 |
|---|---|---|
| `sys_prism_alert_rule.expression` | `expression` | 告警规则表达式（何时产生告警） |
| `sys_prism_remediation_rule.expression` | `expression` | 自愈触发条件（何时触发自愈；原 `condition_expr` 改名统一） |
| 执行器配置 JSON 可选键 | `"expression"` | 执行结果判定（执行怎样算成功；空/缺省 = 协议默认语义）——随执行器配置走：`sys_prism_remediation_rule.executor_config` 与 `sys_monitor_point.config` 内 |
| Metadata 传递 key | `"expression"` | 执行链上的透传键 |
| API / 前端字段 | `expression` | 与列名/键名一致 |

字段命名由此全局统一：**列级只有一种表达式字段 `expression`，语义随所在表而定（告警 / 自愈触发）；执行判定不是独立列，是执行器配置 JSON 的可选键**——自愈动作调用执行器，动作的成败标准属于"怎么执行"的配置，而非"何时触发"的规则。

### 3.6 错误语义

编译错误分类（入口校验统一返回 HTTP 400，响应体携带 expr-lang 原始错误信息）：

1. 语法错误；
2. 未知变量（顶层变量由编译期 Env 检查拦截；域对象内部字段如 `asset.naem` 由入口校验的样例求值拦截，见 8.4）；
3. 类型不匹配（编译期 Env 类型检查）；
4. 结果非布尔；
5. 超过节点数上限。

运行时求值失败（罕见，编译期已挡住类型问题）：

- 告警引擎：warn 日志 + 跳过该规则（本次事件视为不匹配）；
- 自愈条件：warn 日志 + 返回 false（本次事件不触发）；
- 执行判定：warn 日志 + **回落协议默认语义**（不判失败——表达式 bug 不应引发告警风暴）。

### 3.7 匹配对象命名契约（tickraft 与 tickraft-x 一致）

规则匹配对象（求值环境）的命名在两仓统一遵守：

1. **类型命名**：`{Domain}Env`——`AlertEnv`（告警）、`RemediationEnv`（自愈）、`ExecutionEnv`（执行判定）。现状 CE remediation 的 `EventContext` 命名不一致，**改名为 `RemediationEnv`**（x 侧同名的 `EventContext` 一并改名，见 12.4）。
2. **ENV 是数据契约，不追求 Go 类型跨仓复用**：契约的权威定义是本文档第 4 章的变量表（字段名、类型、语义）；CE 与 x 各自定义自己的结构体，x 可定义超集（扩展字段），但**已有字段的名称与类型不得偏离契约**。`pkg/expr` 的编译器 Env 参数为 `any`，两侧结构体均可直接使用。
3. **扩展登记制**：x 新增匹配变量必须遵循本章命名规范（单词小写 / 一层名词域 / 两级属性上限），并在本文档 4.5 扩展表登记后实施。
4. **枚举值统一**：触发类型规范值为 `metric` / `log` / `status_change`（x 独有的 `fault_event` 保留为 x 扩展值；x 现状的 `metric_alert`/`log_alert` 改名为 `metric`/`log`）；资产状态沿用 `normal`/`abnormal`/`unknown`；结果码与时间单位规范见 3.4。
5. **域对象双访问与全称命名**：名词域对象（`asset`/`metric`/`status`）以 map 承载，`asset.id` 与 `asset["id"]` 等价；状态迁移字段全称命名为 `status.previous` / `status.current`（不用缩写）。

---

## 4. 求值环境（ENV）设计

场景机制取消后，每个消费面恰好一个 ENV。实现形态为**顶层 struct + 域对象 map**（详见 4.4）：顶层 struct 承载编译期变量检查；域对象（`asset`/`metric`/`status`）以 map 承载，支持 `.field` 与 `["field"]` 双引用。枚举值一律用 plain `string`/`int`/`float64` 存放，规避 expr-lang 命名类型与字面量比较报 "mismatched types"（现状已踩过此坑）。

### 4.1 告警规则 ENV（`AlertEnv`，定义于 `pkg/prism/alert`）

| 变量 | 类型 | 说明 | 示例 |
|---|---|---|---|
| `type` | string | 告警类别：`metric` / `log` / `status` | `type == "log"` |
| `severity` | string | 主 Violation 严重度：`info` / `warning` / `critical` | `severity == "critical"` |
| `source` | string | 告警来源（日志为来源 IP，探测为探测点标识） | `source == "10.0.0.1"` |
| `keyword` | string | 日志类：命中的关键词 | `keyword matches "fatal\|panic"` |
| `content` | string | 日志类：命中的日志行 | `content matches "timeout\|refused"` |
| `metrics` | map[string]float | 指标类：相关指标值 | `metrics["cpu"] > 90` |
| `asset.id` | int | 触发告警的资产 ID | `asset.id == 42` |
| `asset.name` | string | 资产名称 | `asset.name == "web-1"` |
| `asset.type` | string | 资产类型 | `asset.type == "host"` |
| `asset.tags` | map[string]string | 资产标签（由资产元数据投影） | `asset.tags["env"] == "prod"` |

相对现状（`MetricMatchEnv`）的变更：删除 `alert.`/`event.` 双前缀与 `timestamp`/`asset_id`/`tenant_id`（判定无用，租户过滤转内部）；`asset` 域新增 `tags`（原仅 task 场景有）并删除 `asset.status`/`asset.tenant_id`。

### 4.2 自愈条件 ENV（`RemediationEnv`，定义于 `pkg/prism/remediation`；原 `EventContext` 改名）

| 变量 | 类型 | 说明 | 示例 |
|---|---|---|---|
| `trigger` | string | 触发类型：`metric` / `log` / `status_change`（x 扩展 `fault_event`，见 4.5；原 `type` 改名，避免歧义） | `trigger == "metric"` |
| `level` | string | 日志类：日志级别 | `level == "error"` |
| `keyword` | string | 日志类：命中关键词 | `keyword matches "oom\|killed"` |
| `content` | string | 日志类：日志内容 | `content contains "OutOfMemory"` |
| `source` | string | 事件来源（原 `source_ip` 改名，与告警 ENV 对齐） | `source matches "^10\.0\."` |
| `threshold` | float | 指标类：阈值 | `threshold >= 90` |
| `metric.name` | string | 指标类：指标名 | `metric.name == "cpu"` |
| `metric.value` | float | 指标类：观测值 | `metric.value > 95` |
| `status.previous` | string | 状态类：迁移前状态 | `status.previous == "normal"` |
| `status.current` | string | 状态类：迁移后状态 | `status.current == "abnormal"` |
| `asset.id` | int | 资产 ID（原 `asset_id`/`asset_key` 中的 ID） | `asset.id == 42` |
| `asset.key` | string | 租户内资产唯一键（原 `asset_key`；仅 status_change 触发携带，其余为空串） | `asset.key == "web-1"` |
| `asset.name` | string | 资产名称（**新增富化**，见 6.2.1） | `asset.name == "web-1"` |
| `asset.type` | string | 资产类型（**新增富化**） | `asset.type == "host"` |
| `asset.tags` | map[string]string | 资产标签（**新增富化**） | `asset.tags["env"] == "prod"` |

空表达式（`expression == ""`）匹配该触发类型的所有事件（现状语义保留）。

### 4.3 执行判定 ENV（`ExecutionEnv`，定义于 `pkg/executor`）

| 变量 | 类型 | 说明 | 示例 |
|---|---|---|---|
| `code` | int | 统一结果码（见 3.4） | `code == 200` |
| `body` | string | 响应体/输出（已按执行器上限截断） | `body matches "\"status\":\"ok\""` |
| `error` | string | 执行错误信息（成功为空串） | `error == ""` |
| `duration` | float | 执行耗时，毫秒 | `duration < 500` |
| `metrics` | map[string]float | 执行器产出的指标 | `metrics["rtt_ms"] < 100` |

### 4.4 Go 类型定义草案

```go
// pkg/prism/alert —— 顶层 struct 保证编译期变量检查
type AlertEnv struct {
    Type     string             `expr:"type"`
    Severity string             `expr:"severity"`
    Source   string             `expr:"source"`
    Keyword  string             `expr:"keyword"`
    Content  string             `expr:"content"`
    Metrics  map[string]float64 `expr:"metrics"`
    Asset    map[string]any     `expr:"asset"` // 域对象 map：asset.id ≡ asset["id"]
}

// 各消费面自带的资产域构造（私有；字段集以 4.1/4.2 变量表为契约）
func buildAssetEnv(a asset.Asset) map[string]any {
    return map[string]any{
        "id":   a.ID,
        "name": a.Name,
        "type": string(a.AssetType),
        "tags": a.Tags, // map[string]string，支持 asset.tags["env"]
    }
}

// pkg/prism/remediation
type RemediationEnv struct {
    Trigger   string         `expr:"trigger"`
    Level     string         `expr:"level"`
    Keyword   string         `expr:"keyword"`
    Content   string         `expr:"content"`
    Source    string         `expr:"source"`
    Threshold float64        `expr:"threshold"`
    Metric    map[string]any `expr:"metric"` // {name, value}
    Status    map[string]any `expr:"status"` // {previous, current}
    Asset     map[string]any `expr:"asset"`  // {id, key, name, type, tags}
}

// pkg/executor
type ExecutionEnv struct {
    Code     int                `expr:"code"`
    Body     string             `expr:"body"`
    Error    string             `expr:"error"`
    Duration float64            `expr:"duration"`
    Metrics  map[string]float64 `expr:"metrics"`
}
```

实现要点：

- **分层类型检查**：顶层标量用 struct + expr tag，未知顶层变量与顶层类型错误在**编译期**暴露；域对象用 `map[string]any` 换取双引用能力，代价是域内字段拼写错误（`asset.naem`）编译期不可见——由入口校验的第二道"样例求值"拦截（8.4），运行期求值失败语义兜底（3.6）。
- **域构造各自私有**：`buildAssetEnv` 在 alert 与 remediation 各有一份（几行 map 字面量），不跨包导出共享类型。领域间唯一契约是第 4 章变量表（防漂移机制见 12.7），避免 remediation 依赖告警包（2.2）。
- **样例构造导出**：每个 ENV 定义处导出 `ExampleEnv()`（字段齐全的合法样例），供入口校验样例求值（8.4）与 x 契约测试（12.7）使用。

### 4.5 x 扩展变量登记表（tickraft-x 专属）

x 的自愈触发类型比 CE 多一种（`fault_event`，故障事件），其 `RemediationEnv` 为 CE 契约的超集。扩展变量按 3.7 登记制登记如下（命名已按规范从 x 现状的 `event.xxx` 扁平前缀迁移）：

| 变量 | 类型 | 说明 | x 现状名 |
|---|---|---|---|
| `fault.type` | string | 故障事件类型（`fault_event` 触发携带） | `fault_type` |
| `client.id` | string | 上报客户端标识（`fault_event` 触发携带） | `client_id` |
| `payload` | map[string]any | 原始事件负载（各触发均可携带；值类型混合，比较时注意类型） | `payload` |

CE 侧不定义这些字段（CE 无对应事件源）；x 侧结构体在 4.4 契约字段基础上追加。**x 侧对契约内字段的改名对照**：`event.type → trigger`、`event.metric_name/value → metric.name/value`、`event.prev_status/curr_status → status.previous/status.current`、`event.source_ip → source`、`event.asset_id → asset.id`。

---

## 5. 函数策略

### 5.1 原则：自定义函数为零

**expr 标准已支持的一律不自定义**。逐项裁决：

| 原自定义函数 | 标准等价写法 | 裁决 |
|---|---|---|
| `regex(p, s)` | `s matches "pattern"`（文法级运算符，沙箱下可用） | 取消 |
| `containsany(s, list)` | `s matches "a\|b\|c"`（正则交替） | 取消 |
| `inrange(v, min, max)` | `v >= min && v <= max`（比较符组合） | 取消 |
| `ago(d)` | `xxx > now() - duration("5m")` | 取消 |
| `startswith` / `endswith` | `s startsWith "err"` / `s matches "^err"` | 取消 |

依据（已核实）：v1.17.8 内置函数表（71 个）无正则函数；`matches`/`contains`/`startsWith`/`endsWith` 是 expr-lang 的**中缀运算符**而非内置函数，属文法级能力，任何编译配置下都可用（原 `DisableAllBuiltins` 沙箱不影响运算符，CE 仓库测试 `pkg/prism/rule/compiler_test.go:190` 与 `integration_test.go:129` 已佐证）。本设计取消白名单后更无限制，`matches` 保留一条回归单测即可。

### 5.2 内置函数：完全放开（不设白名单）

结论：**不启用 `DisableAllBuiltins`，expr-lang v1.17 全部标准内置函数可用**（取舍依据见 3.3）。要点：

- 用户可直接使用 expr-lang 官方文档的全部能力（集合谓词、聚合、字符串处理、类型转换、JSON 处理等）；本项目不为内置函数另写文档，只维护推荐风格；
- 推荐风格：优先小写函数（`len`/`map`/`trim`/`split`/`filter`…）；camelCase 标准内置（`startsWith`/`fromJSON`…）可用，但文档示例不主动使用（3.1）；
- 原白名单的"死条目"问题（`contains`/`startsWith`/`endsWith` 实为运算符，`EnableBuiltin` 对其无效）随白名单机制整体删除而消失；
- "白名单外内置被拒"不再是一个错误类别（3.6 相应缩减）。

### 5.3 新增自定义函数的准入门槛（登记制）

同时满足两条才允许新增：

1. expr 标准能力（运算符 + 全部标准内置，见 5.2）确实无法表达；
2. 存在真实必须的场景（非便利性）。

每次新增须在本文档登记：函数名（小写单词）、签名、语义、必要性论证。

### 5.4 性能注记

`matches` 运算符每次求值即时编译正则（expr-lang VM 行为；现有自定义 `regex()` 同样无缓存，**无回退**）。若将来高频规则下成为热点，按 5.3 门槛评估"带 pattern 缓存的 matches 等价实现"，属预留决策。

---

## 6. 三个消费面设计

### 6.1 告警规则引擎（纯告警化）

#### 6.1.1 包路径、表与模型

- 包最终落位 **`pkg/prism/alert`**（先经 `pkg/prism/alert/rule` 子包过渡，最终合并为单包，依据与连带变更见 2.2）；
- 表改名 `sys_prism_rule` → `sys_prism_alert_rule`，**删除 `scene` 列**（DDL 见 7.1）；
- 持久化模型与运行时视图合一为单一 `Rule` 模型（无 `Scene` 字段）；`Spec`（静态规则）同样删除 `Scene` 字段。

#### 6.1.2 删除清单（场景机制整体消失）

- `Scene` 枚举及四个常量（`SceneTask`/`SceneProbe`/`SceneMetric`/`SceneRemediation`）；
- `TaskView`/`ResultView`/`ReportView`/`AlertView`/`RemediationView` 及全部投影函数（由新的 `AlertEnv` 构造替代）；
- `TaskMatchEnv`/`ProbeMatchEnv`/`MetricMatchEnv`/`RemediationMatchEnv`；
- `TaskMatcher`/`ProbeMatcher`；`MetricMatcher` 改名 **`AlertMatcher`**（唯一保留的匹配器）；
- 引擎四场景分组（`taskRules`/`probeRules`/`metricRules`/`remediationRules`）塌缩为**单一规则集**；
- API 方法收敛：`HasMetricRules → HasRules`；引擎双方法（`Match`/`MatchWithViolations`）与匹配器可选接口（`ViolationMatcher`/`NamedMatcher`）最终合一为单次求值契约——`Matcher.Match(ctx, evt) MatchResult` 与 `Engine.Evaluate`（见 2.2）；
- `MatchRemediation`/`HasRemediationRules`/`ErrRuleInvalidScene` 及相关测试。

#### 6.1.3 求值流（不变的部分）

事件驱动语义保持：telemetry 事件 → `prism.Engine.Dispatch` → 治理 guard 链 → `AlertMatcher.Match`（构造 `AlertEnv`，含资产富化，单次资产查询）→ `Engine.Evaluate` 单次循环求值（一次快照、锁外 `expr.Run`，每条规则程序恰好运行一次）→ 命中规则经 `ViolationExtractor` 产出结构化 Violation → 告警记录持久化 + 渠道派发。**无规则时默认放行**的语义保留（`HasRules` 为 false 时 `Match` 返回 `Forward: true`）。

#### 6.1.4 Violation 提取修复

`buildViolation` 补齐 `Severity`/`Source` 字段透传（修复 D-14），Dispatch 替换 `evt.Violations` 后排序与渠道渲染不再丢信息。

#### 6.1.5 租户过滤（修复 D-11）

- 规则加载保留 `tenant_id`；
- 求值前过滤：`rule.TenantID == 0 || rule.TenantID == evt.TenantID`（tenant 0 = 全局规则）；
- `ListEnabled(tenantID)` 语义不变，供扩展运行时按租户加载。

#### 6.1.6 热加载接线（修复 D-03、D-12）

- `internal/service/prism.go` 构造 `rule.Config` 时传入 `EvalInterval: cfg.Prism.EvalInterval`（配置键 `prism.eval_interval`，默认 30s），轮询 Reload 循环启动，DB 侧变更最终生效；
- `migrateStores` 改用 `cfg.RuleConfig.CompilerConfig` 构建编译器，消除写路径与引擎的配置不一致。

### 6.2 自愈（remediation）

#### 6.2.1 条件求值收敛

- 删除 `remediationBuiltinWhitelist`/`compileCondition`/`matchCache`，改用 `pkg/expr`：
  - 编译：`expr.NewCompiler()`（最小约束集，见 3.3）+ `RemediationEnv`（原 `EventContext` 改名）作为 Env 类型；
  - 缓存：`pkg/expr` 的 LRU `ProgramCache`（容量默认 512，键 = Env 类型名 + 表达式串），替换无界 `matchCache`（修复 D-07）；
- **资产富化**：`Manager` 注入 asset store（新增依赖），构造 `RemediationEnv` 时补齐 `asset.name/type/tags`（`asset.id/key` 仍来自事件 payload）。

#### 6.2.2 门控与熔断

- 门控链保持：幂等（in-flight）/ 冷却（`last_run_at`）/ 熔断（连续失败计数）；
- **熔断计数原子化**（修复 D-08）：`consecutive_failures` 从 `metadata` JSON 提为独立列，失败时执行原子 SQL `UPDATE sys_prism_remediation_rule SET consecutive_failures = consecutive_failures + 1 WHERE id = ?`，成功时置 0；达到阈值时置 `status = paused`；
- **幂等门控收紧**（缓解 D-09）：置位 in-flight 的动作提前到规则查询之后、门控检查之前（先占位后检查），消除检查-置位窗口。

#### 6.2.3 执行结果判定接入

- **不在 `sys_prism_remediation_rule` 上设执行判定列**：动作成败标准是执行配置的一部分，放在 `executor_config` JSON 的可选 `expression` 键（3.5）；
- `executorOperator` 构造 `executor.ExecutionRequest` 时从 `executor_config` 解析该键，写入 `Metadata["expression"]`；
- 执行成败由 executor 层统一判定（见 6.3），`Success` 结果回流熔断计数——**用户定义的"成功"标准直接驱动熔断器**；
- `ExecutionRequest` 补 Timeout 传递（修复 D-15）。

#### 6.2.4 入口校验（修复 D-06）

`validateRule` 增加两道校验，失败返回 400：

1. 触发条件 `expression` 非空时，以 `RemediationEnv` 为 Env 编译 + 样例求值；
2. `executor_config` JSON 内 `expression` 键非空时，以 `ExecutionEnv` 为 Env 同样处理。

### 6.3 执行器执行判定

#### 6.3.1 数据结构

- `executor.Result` 新增 `ExitCode int` 字段；`pool.go` 的 `reset()` 同步清空；
- `local` 执行器写入退出码（`exec.ExitError` 的 `ExitCode()`；命令不存在/被信号杀死等无码场景写 -1）；
- webhook 执行器补 `expect_status` 配置（对齐 http，修复 D-16）。

#### 6.3.2 判定位置与语义

判定在**唯一汇聚点** `runner.doExecute`（`pkg/executor/lifecycle.go`）实现，执行器返回结果后、重试判定**之前**：

```text
result = executor.Execute(req)
if exprStr := req.Metadata["expression"]; exprStr != "" {
    env := buildExecutionEnv(result)          // code/body/error/duration/metrics
    ok, err := exprCache.Eval(exprStr, env)   // LRU 缓存的编译产物
    if err != nil {
        warn("expression eval failed, fallback to protocol default")
        // 保持 result.Status 不变（协议默认语义）
    } else if ok {
        result.Status = normal
    } else {
        result.Status = abnormal
        result.ErrorMsg = appendNote(result.ErrorMsg, "expression judged failure")
    }
}
if result.Status != normal { retry... }        // 现有重试逻辑，用户语义同样触发重试
```

语义规则：

- **空表达式 = 协议默认语义**（各执行器现状逻辑，零配置用户无感知）；
- 非空：`true` = 成功（normal），`false` = 失败（abnormal + ErrorMsg 附注）；
- 运行时求值失败：warn + 回落协议默认（不判失败，见 3.6）；
- 编译错误已被入口校验拦截（monitor point / remediation rule CRUD 时），`doExecute` 只处理运行时错误；
- 求值在 Result 归还对象池**之前**完成（Result 池化生命周期约束）。

#### 6.3.3 配置传递链

执行判定表达式统一存放在**执行器配置 JSON 的可选 `expression` 键**，由构造 `ExecutionRequest` 的装配方提取后经 Metadata 透传：

```text
sys_monitor_point.config 的 "expression" 键
  → telemetry/prober_service.pointToProbeTask 从 config JSON 提取，塞 task.Metadata["expression"]
    → task 触发发布 TypeExecutionTriggered（event.WithMetadata）
      → runner.dispatch 组装 ExecutionRequest.Metadata（现有通道，与 max_retries 同模式）
        → doExecute 读取并判定

sys_prism_remediation_rule.executor_config 的 "expression" 键
  → Manager.dispatch → executorOperator 从 executor_config JSON 提取，写入 Metadata["expression"]
      → 同上
```

装配方提取而非 runner 解析 executor 专属 JSON：`expression` 虽是跨执行器的通用键，但配置解析职责留在装配层，runner 只读 Metadata（与 `max_retries` 同模式）。

计划任务（`sys_schedule_task`）本次不开放 `expression`：任务语义下退出码天然直觉，留作后续按需启用（在本文档登记即可）。

---

## 7. 持久化设计

### 7.1 表结构变更总览

规则表**按性质分表**（不合并）：纯匹配规则一张表（告警），匹配+动作+运行态一张表（自愈），执行判定是检查点的私有配置（随检查定义走列），不建新表。

| 表 | 变更 |
|---|---|
| `sys_prism_rule` | **改名 `sys_prism_alert_rule`**，删 `scene` 列 |
| `sys_prism_record` | **改名 `sys_prism_alert_record`**（对称命名） |
| `sys_prism_remediation_rule` | `condition_expr` 列**改名 `expression`**（触发条件语义统一，见 3.5）；+ `consecutive_failures` 列；`metadata` JSON 中的计数字段废弃；执行判定走 `executor_config` JSON 可选键，无列变更 |
| `sys_monitor_point` | **无列变更**：执行判定为 `config` JSON 的可选 `expression` 键 |
| `sys_remediation_rule` / `sys_remediation_record` | 遗留孤儿表，**显式 DROP** |

### 7.2 DDL 草案（SQLite 方言；实际由 GORM AutoMigrate 生成，此处为契约说明）

```sql
-- 改名 + 删列（未发布：直接以新表名建表，无迁移路径）
CREATE TABLE sys_prism_alert_rule (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  tenant_id   INTEGER NOT NULL DEFAULT 0,
  name        VARCHAR(255) NOT NULL,
  description TEXT,
  expression  TEXT NOT NULL,
  enabled     BOOLEAN NOT NULL DEFAULT 1,
  priority    INTEGER NOT NULL DEFAULT 0,
  group_id    INTEGER,                -- 资源组可见性（可空索引）
  metadata    TEXT,                   -- JSON
  created_at  DATETIME,
  updated_at  DATETIME,
  deleted_at  DATETIME
);
CREATE INDEX idx_alert_rule_tenant  ON sys_prism_alert_rule(tenant_id);
CREATE INDEX idx_alert_rule_enabled ON sys_prism_alert_rule(enabled);
CREATE INDEX idx_alert_rule_group   ON sys_prism_alert_rule(group_id);
CREATE INDEX idx_alert_rule_deleted ON sys_prism_alert_rule(deleted_at);

-- 自愈规则：触发条件列改名 + 熔断计数列（未发布：AutoMigrate 直接按新结构建表，
-- 此处 RENAME 仅作语义说明）
ALTER TABLE sys_prism_remediation_rule RENAME COLUMN condition_expr TO expression;
ALTER TABLE sys_prism_remediation_rule ADD COLUMN consecutive_failures INTEGER NOT NULL DEFAULT 0;

-- 监控点：无 DDL 变更（执行判定为 config JSON 可选键 "expression"）

-- 孤儿表清理
DROP TABLE IF EXISTS sys_remediation_rule;
DROP TABLE IF EXISTS sys_remediation_record;
```

### 7.3 Update 语义修复（D-01、D-02 根因）

两个 Store 的 `Update` 从 `Save`（全列覆盖）改为**列级 `Select` 更新**，只更新用户可编辑列：

- `sys_prism_alert_rule`：`name, description, expression, enabled, priority, group_id, metadata, updated_at`；
- `sys_prism_remediation_rule`：`name, description, asset_id, trigger_event_type, expression, executor_type, executor_config, cooldown, circuit_breaker_threshold, enabled, updated_at`；
- 运行态列（`status`/`last_run_at`/`consecutive_failures`）与 `tenant_id`/`created_at` **永不经 API Update 触碰**；
- 服务层 DTO 同步改为全字段携带（GET 返回什么，PUT 就接受什么），双重保险。

---

## 8. API 与校验变更

### 8.1 告警规则 API（`/api/v1/prism/alert/rules`）

- 请求/响应 DTO **删除 `scene` 字段**（场景概念整体消失）；
- `expression` 仍经 `Store` 写路径预编译校验（Env 换为 `AlertEnv`）；
- CRUD 后触发的引擎 `Reload` 保持现状。

### 8.2 自愈规则 API（`/api/v1/prism/remediation/rules`）

- 请求/响应 DTO：`condition_expr` 字段**改名 `expression`**（触发条件）；`executor_config` JSON 携带可选 `expression` 键（执行判定，前端在执行器配置区提供输入，见 9.5）；
- `validateRule` 双道校验（6.2.4），示例错误体：

```json
// POST /api/v1/prism/remediation/rules —— 表达式编译失败
{
  "code": 40000,
  "message": "invalid expression: unexpected token \")\" (1:12)",
  "request_id": "..."
}
```

错误码沿用 `errdefs.CodeBadRequest`；`message` 携带 expr-lang 原始编译错误（含行列位置），前端直接展示。

### 8.3 监控点 API

- 监控点创建/更新：从 `config` JSON 提取 `expression` 键校验（Env 为 `ExecutionEnv`，编译 + 样例求值），坏表达式 400；无该键则行为不变；
- 其余字段不变。

### 8.4 通用表达式校验端点（前端实时校验 / 试跑）

```json
POST /api/v1/expr/validate
{ "env": "alert", "expression": "metrics[\"cpu\"] > 90" }

200 → { "valid": true }
400 → { "code": 40000, "message": "unknown name asset.naem (1:1)", "request_id": "..." }
```

- `env` 取值：`alert` / `remediation` / `execution`，分别以对应 ENV 为基准；
- 校验内容 = **编译**（3.6 全部错误类别）+ **样例求值**（以该 ENV 的 `ExampleEnv()` 求值一次，拦截域对象内部字段的拼写与类型错误——4.4 分层检查的补位）；无副作用、不落库；
- 前端向导/专家模式的失焦与提交前校验统一走该端点（9.4）；
- 各 CRUD 的服务端入口校验独立保留（防绕过前端直调 API）；两端校验逻辑同源（`pkg/expr` 的 `Validate(env, expression)`），tickraft-x 的 DryRun 亦复用同一内核（12.4）。

---

## 9. 前端规则编辑设计（双模式）

### 9.1 双模式总则

三个表达式入口（告警规则、自愈触发条件、执行判定）统一提供两种编辑模式，**生成的 `expression` 字符串是唯一事实源**：

- **向导模式（默认）**：以"条件行"交互式构建，最终生成一个表达式；适合不熟悉 expr-lang 的用户；
- **专家模式**：textarea（等宽字体）直接书写表达式；适合熟悉 expr-lang 规范的用户（完整能力见 3.3/5.2）。

模式切换规则：

- 向导 → 专家：把当前条件行编译为表达式填入 textarea，可继续手改；
- 专家 → 向导：尝试把现有表达式**反向解析**为条件行；仅支持向导可生成的子集（9.2），解析失败则提示"当前表达式超出向导能力"，留在专家模式（不阻塞保存）；
- 手改后保存的规则，再次编辑时自动回显在专家模式。

### 9.2 向导与表达式的双向映射

条件行模型（前端组件内部状态，不落库）：

```text
行 := 变量 × 运算符 × 值
变量 := 顶层变量 | 域对象.字段 | metrics["键"] | asset.tags["键"]
组合 := 行 ((AND | OR) 行)*，支持行级 NOT
```

生成映射（向导 → 表达式）：

| 向导元素 | 生成片段 |
|---|---|
| 组合逻辑 AND / OR | `&&` / `\|\|` |
| 行级取反 | `!(...)` |
| 字符串等于 / 不等于 | `==` / `!=` |
| 数值比较 | `>` `>=` `<` `<=` |
| 正则 / 包含 / 前缀 / 后缀 | `matches` / `contains` / `startsWith` / `endsWith`（字符串值自动加引号并转义） |
| `metrics` / `asset.tags` 键索引 | `metrics["cpu"]`、`asset.tags["env"]`（键为文本输入框） |
| 空值判断 | `error == ""`、`keyword != ""` |

反向解析（表达式 → 条件行）实现为受限的递归下降解析器，只接受上表可生成的子集；这是**纯前端模块**（`web/app/utils/expr-builder/`），不进后端。x 的结构化编辑器（`EditFull`）即向导模式的 x 变体，复用同一映射（12.6）。

### 9.3 变量目录（选择器数据源）

- 前端维护**变量目录常量**（变量路径、类型、一句话说明、所属 env），与本文档第 4 章变量表同步维护（同一 PR 内更新）；tickraft-x 在目录中追加 4.5 扩展变量；
- 运算符下拉按变量类型过滤：string → `== != matches contains startsWith endsWith in`；number → `== != > >= < <=`；map（`metrics`/`tags`）→ 先选键，再按值类型给出运算符；
- 目录常量同时驱动专家模式的**自动补全与悬浮提示**（基于现有组件依赖选轻量方案，实施时定）。

### 9.4 实时校验接入

- 失焦与提交前调用 `POST /api/v1/expr/validate`（8.4），错误信息（含行列）内联展示在编辑器下方；
- 向导模式结构性合法（行完整、值类型匹配）时不调后端；专家模式必调。

### 9.5 三个入口的页面级差异

| 入口 | env 目录 | 占位示例 |
|---|---|---|
| 告警规则编辑（`views/prism/rule/edit`） | `alert` | `metrics["cpu"] > 90` |
| 自愈规则编辑（`views/prism/remediation/rule/edit`） | `remediation`（触发条件 `expression`） | `metric.name == "cpu" && metric.value > 95` |
| 自愈执行器配置区 / 监控点编辑 | `execution`（配置 JSON 的 `expression` 键） | `code == 200 && duration < 500` |

告警规则编辑同时删除 scene 下拉；自愈编辑的旧 `conditionExpr` 字段改名 `expression`；执行判定输入位于执行器配置表单内（local/http/webhook 表单已存在，追加可选行）。

### 9.6 i18n（`zh-Hans` / `en` 同步）

- 删除：scene 相关全部 key、`conditionExpr` 相关 key；
- 新增：双模式切换（向导/专家）、条件行占位（变量/运算符/值）、"超出向导能力"提示、变量目录说明、校验错误前缀；
- 更新：三入口示例文案。

---

## 10. 缺陷修复映射表

| 缺陷 | 级别 | 对应设计条目 |
|---|---|---|
| D-01 PUT 清空自愈运行态 | P0 | 7.3 列级更新 + DTO 全字段 |
| D-02 PUT 清空告警租户/组/元数据 | P1 | 7.3 |
| D-03 eval_interval 死配置 | P1 | 6.1.6 |
| D-04 scene=remediation 死通路 | P1 | 6.1.2 整体删除 |
| D-05 task/probe 场景空转 | P1 | 6.1.2 删除；probe 需求由执行器配置 JSON 的 `expression` 键承接（6.3） |
| D-06 自愈条件无入口校验 | P1 | 6.2.4 |
| D-07 matchCache 无界 | P2 | 6.2.1 LRU 替换 |
| D-08 熔断读-改-写竞争 | P2 | 6.2.2 原子列 |
| D-09 幂等 TOCTOU | P2 | 6.2.2 先占位后检查 |
| D-10 ParseInt 忽略错误 | P2 | 6.2.1 payload 转换错误处理（非法 asset_id 不落 0，事件丢弃 + warn） |
| D-11 多租户缺口 | P2 | 6.1.5 |
| D-12 Store 编译器配置不一致 | P2 | 6.1.6 |
| D-13 regex 重复编译 | P3 | 5.1 函数整体取消（`matches` 运算符替代） |
| D-14 Violation 丢 severity/source | P3 | 6.1.4 |
| D-15 ExecutionRequest 无 Timeout | P3 | 6.2.3 |
| D-16 webhook 无 expect_status | P3 | 6.3.1 |
| D-17 调试注释残留 | P3 | 实施时顺手清理 |
| A-1 平行实现 | 结构 | 第 2 章（pkg/expr 内核） |
| A-2 场景机制空转/名实不符 | 结构 | 6.1（表改名 + 纯告警化） |
| A-3 执行判定不可配置 | 结构 | 6.3（可选 expression） |

---

## 11. 测试计划与实施顺序

### 11.1 测试计划

**pkg/expr 单测**

- 约束集：MaxNodes=1000 越界拒绝；AsBool 非布尔编译失败；标准内置不做限制（抽样验证 `matches`/`contains` 运算符与 `fromJSON` 等内置可用）；
- `Validate`（编译 + 样例求值）：未知顶层变量、域对象字段拼写错误（`asset.naem`）、类型不匹配、非布尔结果均被拦截；
- LRU 缓存：命中复用同指针、容量淘汰、并发安全（`-race`）。

**告警规则引擎**

- 新 ENV 求值：表 4.1 每变量的正反例；
- 无规则默认放行；坏规则 warn 跳过不影响其他规则；
- 租户过滤：全局规则（tenant 0）与租户规则各自命中；
- Violation 提取保留 severity/source；
- 热加载：CRUD 触发 Reload、轮询 Reload。

**自愈**

- 条件求值委托内核后的行为回归（空表达式全匹配、编译失败 false）；
- 入口校验：坏触发 `expression` 与坏 `executor_config.expression` 均返回 400；
- 熔断原子计数：并发失败场景下计数不丢（`-race` + 并发用例）；
- PUT 后 `status`/`last_run_at`/`consecutive_failures` 不变（D-01 回归）；
- 执行判定联动：`expression` 判 false 时 Record 状态 failed 且熔断计数 +1。

**执行器**

- `Result.ExitCode`：local 正常/非零退出/命令不存在三态；`pool.reset` 清空；
- 判定语义：空表达式默认、true/false 覆盖、求值失败回落、重试联动（判 false 触发重试）；
- `metadata` 传递链：monitor point `config` JSON 的 `expression` 键 → 执行判定生效（端到端）；remediation `executor_config` 同理；
- webhook `expect_status` 生效。

**tests/httpapi**

- 告警规则 CRUD 无 scene 字段、坏表达式 400；
- 自愈规则 CRUD：触发条件字段为 `expression`、`executor_config` 内执行判定键校验、PUT 后运行态字段不变；
- 监控点 CRUD：`config` 内 `expression` 键校验（坏表达式 400）；
- `/expr/validate` 三种 env 的正反例。

**前端**

- vitest：告警规则编辑无 scene；双模式切换与生成映射（向导 → 表达式快照用例）；反向解析的接受/拒绝用例；校验端点错误内联展示；
- 构建通过。

**端到端冒烟**

1. 创建告警规则 `metrics["cpu"] > 90` → 制造指标越限 → 告警产生且 Violation 带 severity；
2. 创建自愈规则（触发 `metric.name == "cpu" && metric.value > 95`，`executor_config` 内 `"expression": "code == 0"`）→ 触发 → 执行记录 completed；
3. 监控点 `config` 配 `"expression": "code == 200 && body matches \"status\":\"ok\""` → 200+异常 body 判失败、触发重试。

### 11.2 实施顺序

1. `pkg/expr` 内核 + 单测（含 `Validate` 编译 + 样例求值）；
2. `pkg/prism/rule` 迁移 + 纯告警化（ENV 替换、场景删除、表改名、租户过滤、热加载接线、Update 修复；经 `pkg/prism/alert/rule` 过渡后并入 `pkg/prism/alert` 单包，见 2.2）；
3. `pkg/prism/remediation`（条件求值收敛、资产富化、熔断原子化、`condition_expr` → `expression` 改名、入口校验、Update 修复）；
4. `pkg/executor`（ExitCode、判定汇聚点、webhook expect_status、传递链）；
5. 迁移清理（表改名/删列/DROP 孤儿表；`tickraft migrate` CLI 路径补齐 channel/remediation 覆盖）；
6. 前端双模式编辑器 + `/expr/validate` 接入 + i18n；
7. tickraft-x 同步更新（按第 12 章清单，与 CE 步骤 2-6 并行或紧随其后，同批合入）；
8. 全量 `go build ./...` / `go vet` / `go test ./...` / 前端构建 / 冒烟。

### 11.3 明确不做的事

- 不做大一统规则引擎；
- 不合并两张规则表；
- 计划任务不开放 `expression`（登记制，需要时再启）；
- telemetry 被动链路的 Device 处理器阈值硬编码不动（主动检查已可用 `expression` 表达同类语义，被动链路对齐留待后续）；
- 不引入任何自定义函数（见第 5 章准入门槛）。

---

## 12. tickraft-x 同步设计

tickraft 是 tickraft-x 的基础设施。本次重构**不是 CE 单仓变更**：x 侧存在对 CE 规则机制的直接引用、隐式约定依赖，以及一处**同构复制的平行模型**（x 的 remediation `Rule` 复制了 CE 模型字段而非嵌入，同表名 `sys_prism_remediation_rule`、触发枚举不同、自带 `EventContext` 求值环境）。本章给出 x 的同步方案，与 CE 步骤同批实施。

### 12.1 x 现状受影响面盘点

| x 侧位置 | 角色 | 受影响点 |
|---|---|---|
| `internal/api/service_alert_prism.go` | 告警规则 CRUD（CE AlertService 的本地拷贝，因 x 不能 import CE internal/） | Create 强校验 `Scene` 非空（L68）；DTO 映射含 Scene（L163-186）——删 scene 列后必须同步 |
| `internal/grpc/alert_service.go` | gRPC 规则版本化拉取（Worker/Prism 远程源） | proto `AlertRule` 消息未携带 `Expression`（注释待补）——补字段后 worker 侧才能完整投影 |
| `internal/service/worker/strategy_standalone.go` / `strategy_distributed.go` | Worker 规则源（DB 轮询 / gRPC+PubSub） | `recordsToRules` 投影丢弃表达式；"Metric 空=日志规则、Description 当关键字"的**隐式 scene 约定** |
| `internal/service/worker/analyzer.go` | Worker 告警分析器 | `matchMetricRules`/`matchLogRules` 结构化匹配与隐式约定耦合（见 12.3） |
| `internal/service/server/syncer.go` / `distributed.go` | 分布式 Server：Redis Hash 全量/增量同步、gRPC 注册 | Record JSON 序列化自动跟随 schema；`AutoMigrate(&rule.Record{})` 跟随表改名 |
| `internal/middleware/group_interceptor.go` | 资源组隔离拦截器 | 硬编码表名 `sys_prism_rule` → 改 `sys_prism_alert_rule` |
| `internal/service/prism/prism.go`（`BuildRuleConfig`） | Prism 角色规则引擎装配 | 依赖 `rule.Config/CompilerConfig`；import 路径随 2.2 迁移；比较数权益条款取消（12.5） |
| `internal/prism/remediation/` | **x 自建自愈引擎**（自己的 `EventContext`/`compileCondition`/`buildExprEnv`/DryRun） | 全部收敛到 CE 内核与契约（12.4） |
| `web/src/api/rule-full.ts` + `views/prism/rule/edit/EditFull.vue` | x 前端结构化条件编辑器（severity/logic/conditions） | 与 CE `expression` 模型 schema 不匹配——按 CE 双模式设计统一（9.1、12.6） |

### 12.2 告警规则链路同步

1. **CRUD 拷贝更新**（`service_alert_prism.go`）：删除 Scene 校验与 DTO 映射中的 Scene 字段；`prismRuleHandlerToModel` 补 TenantID/GroupID/Metadata 携带（对齐 CE 侧 D-02 修复后的全字段 DTO）；构造时传入真实 engine 使 CRUD 后 `Reload` 生效（现状传 nil 为 no-op，或改用 ConfigBus 通知 Prism 角色）。
2. **gRPC proto**：`AlertRule` 消息补 `string expression = n;`（版本化拉取已就绪，仅缺字段）；`computeAlertRuleVersion` 不变。
3. **syncer/拦截器**：`GroupInterceptor` 表名清单改 `sys_prism_alert_rule`；Redis Hash 与 Pub/Sub 通道无 schema 依赖，自动跟随。
4. **迁移顺序**：x 的 `runAllMigrations` 先跑 CE 核心表 AutoMigrate 再跑 x Feature Migrate（现有注释明确该顺序），CE 表改名后 x 侧无需额外处理。

### 12.3 Worker 规则分析器收敛（删除隐式 scene 约定）

原状：worker 曾把 `rule.Record` 投影为本地 `Rule` 视图（`recordsToRules` 仅携带 ID/Name/Description/Enabled/UpdatedAt），analyzer 靠 **"Metric 字段为空即日志规则 + Description 当关键字"** 区分场景——这是对 scene 的隐式耦合，且丢失了表达式。

最终形态：Worker 复用 CE 的匹配引擎，本地投影与双路径匹配全部删除：

- standalone/distributed 规则源直接持有单一模型 `alert.Rule`（分布式侧 gRPC proto 全字段传输；standalone 侧仅做启用过滤，元数据按需经 `Rule.MetadataMap()` 解码）；
- analyzer 对事件构造 `AlertEnv` 后经 CE 引擎 `Evaluate` 单次求值——**无需任何场景区分字段**：表达式引用的变量天然区分场景（log 事件的 `metrics` 为空 map、`content` 有值，metric 规则自然不命中），与 CE 引擎同一语义；
- worker 本地 `Rule` 视图（strategy.go 的 Metric/Condition/Threshold 零值字段）、`recordsToRules`/`parseRuleMetadata` 本地投影与 `matchMetricRules`/`matchLogRules` 双路径均已删除。

### 12.4 x 自愈引擎收敛（平行实现消除）

x 的 `internal/prism/remediation` 是 CE 同名模块的**同构复制 + 扩展**（复制模型与 `EventContext`，另加 VerifyProbe 修复后验证、DryRun 在线试跑、更多 executor 类型、故障事件触发、严格租户隔离）。同步方案：

1. **模型改嵌入**：x 的 `Rule` 改为嵌入 CE `pkg/prism/remediation.Rule` + x 特有列（`verify_probe`/`verify_enabled`），消除字段复制——这正是 CE remediation doc.go 声明的扩展方式（"Extended editions embed this model...sharing the same table"），x 此前未按此实现。CE 侧的 `condition_expr` → `expression` 改名与 `consecutive_failures` 新列由嵌入自动继承（x 删除自己的 `ConditionExpr` 定义）。
2. **求值环境对齐**：删除 x 的 `EventContext`，改为 4.2 契约的 `RemediationEnv` 超集（+4.5 扩展字段 `fault.type`/`client.id`/`payload`）；字段改名对照见 4.5。
3. **编译求值收敛**：`compileCondition`/`evaluateCondition`/`buildExprEnv` 与 DryRun（handler.go L767-800）全部改用 CE `pkg/expr`；迁移后 **x 对 expr-lang 的直接 import 归零**（现状仅 manager.go 与 handler.go 两处）。
4. **触发枚举统一**：`metric_alert → metric`、`log_alert → log`；`status_change` 不变；`fault_event` 保留为 x 扩展值（3.7）。
5. **熔断计数迁移**：x 现用 `metadata.consecutive_triggers`，随 CE 的 `consecutive_failures` 独立列一并迁移（原子 SQL，见 6.2.2）。
6. **x 特有能力保留**：VerifyProbe 修复后验证（验证探测的配置 JSON 同样支持可选 `expression` 键，判定走同一 `ExecutionEnv` 通道，与 6.3 同机制）、DryRun（换内核后能力不变）、executor 扩展类型（ssh/mysql/redis/snmp/k8s/mqtt——x 侧实现，`Metadata["expression"]` 判定通道对其同样生效）、规则级严格租户隔离、AI AlertLevel 分级（`internal/executor/ai/classify.go`，与布尔判定互补不冲突）。

### 12.5 编译配置统一（原比较数权益随约束取消而消失）

比较数上限在本设计中整体取消（3.3），x 的 `CompilerConfig{MaxComparisons: -1}` 差异化权益不复存在，`BuildRuleConfig` 简化为默认配置——**两仓编译语义完全一致**（内核唯一、约束集唯一、内置全开）。x 的差异化能力后移到更合适的层面：ENV 扩展变量（4.5）、执行器类型扩展、VerifyProbe/DryRun。

`EvalInterval`（5 分钟兜底轮询）与 `ReloadSubscriber`（ConfigBus 实时通知）保留——x 已正确接线，CE 侧 D-03 修复即对齐该用法。

### 12.6 x 前端同步

1. **结构化编辑器与 CE 双模式统一**：`rule-full.ts` 的 `AlertRuleFullPayload{logic, conditions[...]}` 即 CE 9.1 向导模式的 x 变体——提交时按 9.2 映射表编译为规范表达式存入 `expression` 列，`expression` 成为唯一事实源，消除前后端 schema 不一致；`duration` 窗口字段的映射语义是遗留决策点（13.3），首版可不映射并暂时隐藏；反向解析复用 CE 的 `expr-builder` 前端模块（CE → x 单向拷贝或提为共享 web 包，实施时定）。
2. **remediation 前端**：`api/prism/remediation.ts` 的接口类型对齐后端字段（`trigger_event_type`、`expression`（触发条件，原 `conditionExpr` 改名）、`executor_config`（含可选 `expression` 执行判定键）），移除 mock 驱动的旧 schema；DryRun 入口保留（校验内核与 `/expr/validate` 同源，8.4）。
3. i18n 与 CE 同步增删（scene 相关 key 删除、双模式与 expression 相关 key 新增）。

### 12.7 ENV 契约防漂移机制

两仓结构体独立定义（3.7 第 2 条），一致性由**测试守护**：

- x 侧增加契约一致性单测：以 CE `pkg/prism/remediation.ExampleEnv()` 运行时展开的**变量路径集合**（`trigger`、`asset.id`、`status.current`…）为基准，断言 x 的 `RemediationEnv` 样例展开路径 ⊇ 基准（map 承载的域对象同样可运行时展开键集），x 超集字段对照 4.5 登记表；
- CE 侧契约变更必须同步更新本文档第 4 章与 4.5 登记表，x 侧测试随之更新——文档是唯一权威。

### 12.8 x 同步验收清单

- [ ] `service_alert_prism.go` 无 Scene 依赖，CRUD 全字段携带；
- [ ] proto `AlertRule` 携带 `expression`，分布式 Worker 规则投影完整；
- [ ] worker analyzer 无隐式 scene 约定，统一走 `AlertEnv` + `pkg/expr`；
- [ ] `GroupInterceptor` 表名已改；
- [ ] x 的 remediation 模型嵌入 CE 模型，`EventContext` 已删除，求值走 `pkg/expr`；
- [ ] 触发条件字段为 `expression`，`condition_expr` 已删除；
- [ ] 触发枚举值为 `metric`/`log`/`status_change`/`fault_event`；
- [ ] x 对 expr-lang 直接 import 为 0；
- [ ] 契约一致性单测通过（路径集合对比）；
- [ ] DryRun/VerifyProbe 行为回归通过；
- [ ] x 前端结构化编辑器与 CE 双模式统一（提交为 `expression`），remediation 前端 schema 对齐。

---

## 13. 需求覆盖评估（tickraft + tickraft-x）

对两仓当前与既定的规则类需求逐项核对本设计的覆盖情况。

### 13.1 tickraft（CE）

| 需求 | 承接设计 | 状态 |
|---|---|---|
| 指标/日志/状态三类告警事件的规则过滤 | 4.1 AlertEnv + 6.1 | 满足 |
| 规则热加载（API 触发 + DB 轮询兜底） | 6.1.6 | 满足 |
| 自愈触发（类型 + 条件 + 资产富化） | 4.2 + 6.2.1 | 满足 |
| 自愈门控（幂等/冷却/熔断）与并发安全 | 6.2.2 | 满足 |
| 执行成败可配置（HTTP 状态/退出码/body 内容/耗时） | 3.4 + 6.3 | 满足 |
| 防误配（坏表达式入口拦截、域字段拼写拦截） | 3.6 + 8.4 | 满足 |
| 熟悉 expr-lang 用户使用完整标准能力 | 3.3 + 5.2（内置全开） | 满足 |
| 不熟悉 expr 用户的交互式建规则 | 9.1 向导模式 | 满足 |
| 多租户（CE 单租户运行，字段与过滤预留） | 6.1.5 | 满足（预留） |
| 求值性能（程序缓存、锁外求值） | 2.1 + 6.2.1 | 满足 |
| 计划任务退出码判定 | 6.3.3 尾注 | 明确不做（登记制） |
| 被动上报链路的阈值处理 | 11.3 | 明确不做（后续对齐） |

### 13.2 tickraft-x

| 需求 | 承接设计 | 状态 |
|---|---|---|
| 分布式规则同步完整性（expression 传输） | 12.2 proto 补字段 | 满足 |
| Worker 告警分析与 CE 同语义 | 12.3 | 满足 |
| 故障事件自愈（fault_event 触发与扩展变量） | 4.5 扩展登记 + 12.4 | 满足 |
| 修复后验证（VerifyProbe） | 12.4 第 6 条 | 满足 |
| 在线试跑（DryRun） | 12.4（换 `pkg/expr` 内核，与 8.4 同源） | 满足 |
| 结构化前端编辑 | 9.1 向导模式 + 12.6 | 满足 |
| 严格租户隔离 | 12.4 第 6 条保留 | 满足 |
| AI AlertLevel 分级与布尔判定并存 | 12.4 第 6 条 | 满足（互补） |
| 扩展执行器类型（ssh/mysql/redis/snmp/k8s/mqtt）的成败判定 | `Metadata["expression"]` 通道对所有执行器生效（6.3.3） | 满足 |
| 比较数放开权益 | 3.3 取消上限，两仓统一 | 满足（权益转为通用能力） |

### 13.3 评估结论与遗留决策点

结论：**两仓当前已知需求全部覆盖**；未覆盖项均为"明确不做/预留"并已登记（11.3 与上表尾行），无架构级缺口。

遗留决策点（实施时定，不影响架构）：

1. 向导反向解析器的子集范围（9.2）——首版可只支持单层 AND/OR 组合；
2. `EditFull` 的 `duration` 窗口字段向表达式的映射语义（12.6）——首版可不映射并在前端隐藏；
3. `expr-builder` 前端模块在两仓间的共享方式（拷贝或共享包）。
