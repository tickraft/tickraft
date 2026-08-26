# 数据模型分层去重设计（Model Layering Design）

> 状态：**设计评审稿**。本文档是 API 层与持久层数据模型合并重构的权威设计，经完全确认后才进入代码实施。
> 适用范围：`pkg/prism/{alert,channel,remediation}`、`pkg/telemetry`、`pkg/task`、`pkg/asset`、`pkg/api/handler/*`、`internal/api/service/*`，以及 **tickraft-x 的同步更新**（第 7 章）。

> **实施状态（2026-08-26 更新）**：本设计已实施。文中"当前 API 层/转换器盘点"等路径为**设计时快照**，保留作决策依据，不再与现状一一对应。服务层后续已再下沉域包，最终路径映射：
> `internal/api/service/{scheduler,prism,telemetry,system}` → `pkg/task/service`、`pkg/prism/{alert,channel,remediation}/service`、`pkg/telemetry/service`、`pkg/system`；
> SPI+DTO 自 `pkg/api/handler/<域>/types.go` 随迁上述各包。现状架构见 `docs/architecture.md`。
> 前提：两仓均未发布。Tier 1/2/4 已实施交付（2026-08）；**5.4 Tier 3（task 域完整设计）为 2026-08-24 新增，待评审确认后另批实施**，拟议 wire 变更见 6.4。已实施批次的唯一 wire 破坏性变更为 `fired_at` → `triggered_at`（6.1），其余实体响应字段保持不变。

---

## 目录

1. [背景与问题](#1-背景与问题)
2. [现状盘点：模型对照与分档判定](#2-现状盘点模型对照与分档判定)
3. [目标分层约定](#3-目标分层约定)
4. [容错 JSON serializer 设计（定案）](#4-容错-json-serializer-设计定案)
5. [分档优化方案](#5-分档优化方案)
6. [wire 变更清单](#6-wire-变更清单)
7. [双仓实施顺序](#7-双仓实施顺序)
8. [测试计划与验收](#8-测试计划与验收)
9. [风险与非目标](#9-风险与非目标)

---

## 1. 背景与问题

### 1.1 现状：同一实体两到三份手写模型 + 手写双向转换

当前 API 层（`pkg/api/handler/*/types.go`）与持久层（`pkg/*/model.go`）为每个实体各自定义 struct，由 `internal/api/service/*` 的适配层手写字段拷贝衔接。经全仓盘点：

- CE 仓约 **25 个命名转换函数、450–500 行手写映射代码**（明细见 2.1）。
- tickraft-x 无法 import CE 的 `internal/`，**整套复制了同一映射逻辑**（`tickraft-x/internal/api/service_alert_prism.go:172-247`、`service_task.go:427`、`service_telemetry.go`）。同一语义两份维护，是当前最大的一致性风险。
- 全仓唯一真正的三层模型是 task：handler `Task` → 领域 `pkg/task.Task` → GORM `ScheduleTask`，其转换器（`pkg/api/handler/task/converter.go`，274 行）通过 metadata map 与 config 保留键两轮往返搬运动限字段，解析失败静默吞错返回零值（`converter.go:192-214`），且 `"enabled"` 键是隐性存储契约（`converter.go:42-50` 注释自述改名会孤儿化数据）。

### 1.2 已核实的代价

| 代价 | 证据 |
|---|---|
| 历史 bug 类：PUT 清空字段（D-01/D-02）| 根因即"DTO 不携带运行态字段 + 全列覆盖"；修复后各 Update 路径仍需逐处手工恢复 `ID/CreatedAt`（`internal/api/service/prism/alert.go:100-101`、`channel.go:91-92`、`telemetry.go:350` 的 `mergeTaskOntoPoint`）|
| 新增一个 DB 字段需同步改动 4–6 处 | DB 模型、handler 模型、两个方向的转换器、（x 仓的复制品）、OpenAPI schema |
| OpenAPI 规范漂移 | `docs/api/openapi.yaml` 的 `AlertRule` schema（:2030-2109）仍是重构前的 metric/operator/threshold 旧模型，无 `expression/priority/group_id/metadata`；且规范用 camelCase（:2073 `createdAt`）而实际 wire 是 snake_case |
| 分离的文档化理由不成立 | `pkg/api/handler/doc.go:37` 称 handler-local 类型使"handler 可脱离数据库单测"；但转换器本身就 import 了 `pkg/task` 模型包（`converter.go:14`），而模型 struct 是纯数据类型、并不依赖数据库连接 |
| 静默丢字段 | `executionToHandler` 丢弃 DB 已有的 `run_id/trigger_type/skip_reason/metrics`；`recordModelToHandler` 改名 `TriggeredAt`→`FiredAt` 并丢 `CreatedAt`（`alert.go:256-273`）|

### 1.3 结论

重复分两类：**形态完全相同的仪式性重复**（应合并），与**形态确有差异的真实分层**（应保留但修正实现）。一刀切"全部合并到 DB 模型"与一刀切"全部保留 DTO"都不对；`asset.Asset` 的裸绑定（见 2.4）同时证明了没有护栏的合并同样有害。

---

## 2. 现状盘点：模型对照与分档判定

### 2.1 转换函数全景（CE）

| 位置 | 函数 | 规模 |
|---|---|---|
| `internal/api/service/prism/alert.go` | `ruleModelToHandler`:194、`ruleHandlerToModel`:213、`decodeRuleMetadata`:227、`encodeRuleMetadata`:241、`recordModelToHandler`:256 | ~120 行 |
| `internal/api/service/prism/channel.go` | `channelModelToHandler`:152、`channelHandlerToModel`:167 | ~25 行 |
| `internal/api/service/prism/remediation.go` | `remediationRecordToHandler`:206、`remediationModelToHandler`:225、`remediationHandlerToModel`:251 | ~60 行 |
| `internal/api/service/telemetry/telemetry.go` | `pointToTask`:306、`taskToPoint`:332、`mergeTaskOntoPoint`:350、`configToString`:364 | ~70 行 |
| `pkg/api/handler/task/converter.go` | `DomainTaskToHandler`:70、`HandlerToDomainTask`:104 及 5 个 helper | ~274 行 |
| `internal/api/service/scheduler/task.go` | `executionToHandler`:453、`scheduleToMetadata`:397 | ~35 行 |
| `internal/api/service/taskconv/adapter.go` | 纯委托壳 :19、:25 | 9 行 |
| `internal/api/router/router.go` | `serviceAdapter` :48-172、`userToProfile`:162 | ~120 行 |
| `pkg/task/model.go` / `store.go` | `ToTask`:60、`ToExecution`:132、`ExecutionToModel`:158、`taskToModel`:177 | ~125 行 |
| `internal/api/service/system/system.go` | Get/UpdateConfig 手工映射 :104-134 | ~30 行 |

### 2.2 分档 A —— 仪式性重复（字段一一对应，零语义变换）

| 实体 | handler 侧 | 持久层侧 | 判定证据 |
|---|---|---|---|
| remediation Rule | `pkg/api/handler/remediation/types.go:19`（15 字段）| `pkg/prism/remediation/model.go:52` | **全仓重复之最**：15 个共有字段的字段名、类型、json tag 逐字相同；DB 模型已自带相同 json tag。差异仅为 DB 多 `TenantID/Metadata/DeletedAt`（handler 从不序列化三者）|
| remediation Record | types.go:63（12 字段）| `pkg/prism/remediation/record.go:37` | 一一对应；DB 多 `UpdatedAt`（被丢弃）|
| channel | `pkg/api/handler/channel/types.go:16`（8 字段）| `pkg/prism/channel/model.go:22` | 一一对应、同名同类型（含 `Config string` 两侧同为 JSON 字符串）；DB 多 `DeletedAt` |
| system Config | `pkg/api/handler/system/types.go:13`（3 字段）| `internal/api/service/system/system.go:30` | 3/3 纯拷贝 |
| auth tokenData | `pkg/api/handler/auth/handler.go:40` | `TokenPair`（同包 types.go:18）| **同层重复**：5/5 字段与 json tag 逐字相同 |

### 2.3 分档 B —— 形态小差异（可在模型内消解）

| 实体 | 差异 | 消解方式 |
|---|---|---|
| alert Rule | handler `Metadata map[string]string`（types.go:21）vs DB `Metadata string`（model.go:44）；DB 模型无 json tag；DB 多 `TenantID/DeletedAt` | json tag + 容错 serializer（第 4 章）+ 内部列 `json:"-"` |
| alert Record | 改名 `TriggeredAt`→`FiredAt`；handler 丢 `CreatedAt`；空 severity 由转换器补 `"warning"`（alert.go:256-260）| 直接用 DB 模型；`triggered_at` 改名（已批准）；severity 默认移入 `AfterFind` |
| telemetry 监控点 | handler `Config map[string]any`（types.go:50）vs `MonitorPoint.Config string`（model.go 起 :118）；DB 多 `TenantID/Status/Interval/Timeout` 且已带 json tag | Config 走 serializer；四个内部/派生列 `json:"-"` 保持 wire 不变 |

### 2.4 分档 C/D —— 真分层与反例

- **task（分档 C，2026-08-24 复核推翻初判）**：初判认为领域层语义真实（`Timeout time.Duration`、状态机翻译——存储 `normal/abnormal/triggered` ↔ API `success/failed/running`，`pkg/task/types.go:92-123`），分层应保留。深化勘察（5.4.1）表明领域 struct 无行为方法、引擎实际消费 metadata 字符串袋的键而非富字段，"分层应保留"不成立——定案为三层收敛为单一双 tag 模型（5.4）；初判指出的病灶（handler 专属字段被塞进 metadata map 往返，见 1.1）随合并消除。
- **asset（分档 D，反例）**：`pkg/asset/asset.go:16` 的 `TenantID` 带 `json:"tenant_id"`（:27）直接暴露在 API；handler 直接 `BindAndValidate` 到该模型，客户端可提交内部字段。证明合并必须有护栏（`json:"-"` + 列白名单），不能裸奔。
- **附带发现**：auth 域的 `apiKeyData`（handler.go:72）是**合理 DTO** 的样板——create 响应需要 `raw_key`，而 `user.APIKey` 模型绝不能携带明文密钥。这是"真实分歧才保留 DTO"约定的正面例证。

### 2.5 既有的不一致先例

代码库已是三种风格混杂：`alert.Record`、`remediation.Rule`、`asset.Asset` 为 json+gorm 双 tag 单模型；alert Rule 为纯 gorm 模型 + handler DTO;`user.APIKey` 经 Service 接口直接透出（types.go:46-49）。本设计将统一为一种约定（第 3 章）。

---

## 3. 目标分层约定

以下约定写入 `docs/module-boundary.md` 的模型节（实施时同步）：

1. **默认单一双 tag 模型**：实体只有一份 struct，同时携带 json 与 gorm tag。仅当 API 形态与存储形态**真实分歧**（如 create 响应含模型不应持有的秘密字段、或跨表聚合视图）时才允许独立 DTO，分歧消除即合并。
2. **内部列一律 `json:"-"`**：`tenant_id`、`deleted_at`、内部 blob、运行态计数等不下发、不可绑定。hertz/gin 的 JSON 绑定不认 `json:"-"` 字段，天然防 mass-assignment。
3. **写路径必须走 store 列白名单**：`Select(updateColumns).Updates(...)` 模式（先例：`pkg/prism/alert/store.go:66-86`、`pkg/prism/remediation/store.go:162-163`）。可写列由 store 定义，与模型合并与否无关——这是合并后防止客户端篡改 `status/last_run_at/consecutive_failures` 等运行态字段的机制。
4. **json tag 保持 snake_case**：前端经 humps 双向转换（`web/packages/core/src/utils/naming.ts`、`request.ts:129-137`），模型合并不触碰 wire 格式。
5. **必须存在的转换函数放 `pkg/api/handler/*`**（tickraft-x 可 import 的位置），禁止放 `internal/`——杜绝 x 仓复制粘贴的根因。

---

## 4. 容错 JSON serializer 设计（定案）

### 4.1 决策

string（DB 列）↔ map（API 形态）的字段，采用**自定义容错 serializer、单字段方案**：字段声明为 map 类型，挂 `serializer:tolerantjson` tag，读写由 serializer 透明编解码。不采用"双字段 + hook"（`Metadata string` + `MetadataMap map`），那会把同义双表示保留在模型表面，正是本次要消除的病灶。

### 4.2 受益字段

| 模型字段 | 现形态 | 目标形态 |
|---|---|---|
| `alert.Rule.Metadata` | `string`（model.go:44）| `map[string]string` |
| `telemetry.MonitorPoint.Config` | `string`（model.go:118 起）| `map[string]any` |

channel 的 `Config` 两侧均为 JSON 字符串（handler types.go:20），**不走 serializer**，维持现状。

### 4.3 为什么不能用内置 `serializer:json`

内置 JSON serializer 遇损坏 JSON **返回错误，导致整行查询失败**。而现行 `MetadataMap()`（model.go:57-72）与 `decodeRuleMetadata`（alert.go:227-236）的契约是**容错降级**：坏 payload 解析为 nil，绝不阻断规则加载。若换用内置 serializer，历史脏数据会把"降级"变成"宕机"。自定义 serializer 把该契约原样移入 Scan。

### 4.4 实现（含两处实施修订）

实施时相对原设计定稿了两处修订：

1. **落位 `pkg/db/jsonmap.go`，不是原计划的 `pkg/types/jsonmap.go`**。`pkg/types` 有测试钉死的"仅标准库 import"约束，serializer 需要 `gorm.io/gorm/schema`，放进去即破坏叶子属性；`pkg/db` 语义上就是"数据库设施"包，模型侧以 blank import（`_ "github.com/tickraft/tickraft/pkg/db"`）保证注册先于任何 schema 解析。
2. **JSON 编解码用 `github.com/bytedance/sonic`**（API 与 `encoding/json` 全兼容）。serializer 位于元数据读写的热路径（规则加载、监控点批量查询），sonic 在 amd64/arm64 上有明显编解码性能优势；且为既有依赖（x 仓已在用），不新增供应链面。

```go
package db

import (
	"context"
	"reflect"

	"github.com/bytedance/sonic"
	"gorm.io/gorm/schema"
)

// tolerantJSON is a GORM serializer for JSON-shaped map fields
// (map[string]string, map[string]any) stored in text columns, declared via
// `gorm:"serializer:tolerantjson"`.
//
// It differs from the built-in json serializer in two deliberate ways,
// both preserving the historical MetadataMap/decodeRuleMetadata contract:
//
//   - Reads never fail. NULL, "", "null", "{}", and malformed blobs all
//     decode to a nil field instead of erroring, so a bad payload never
//     blocks row loading.
//   - Nil and empty maps persist as the empty string, not SQL NULL and not
//     a literal "null", matching the on-disk format previously written by
//     encodeRuleMetadata.
type tolerantJSON struct{}

func (tolerantJSON) Scan(ctx context.Context, field *schema.Field, dst reflect.Value, dbValue any) error {
	var raw []byte
	switch v := dbValue.(type) {
	case nil:
		return nil
	case []byte:
		raw = v
	case string:
		raw = []byte(v)
	default:
		var err error
		if raw, err = sonic.Marshal(v); err != nil {
			return nil // tolerant by contract
		}
	}
	if len(raw) == 0 {
		return nil
	}
	decoded := reflect.New(field.FieldType)
	if err := sonic.Unmarshal(raw, decoded.Interface()); err != nil {
		return nil // tolerant by contract
	}
	value := decoded.Elem()
	if value.Kind() == reflect.Map && value.Len() == 0 {
		return nil // "{}" 与空 map 归一为 nil
	}
	field.ReflectValueOf(ctx, dst).Set(value)
	return nil
}

func (tolerantJSON) Value(_ context.Context, _ *schema.Field, _ reflect.Value, fieldValue any) (any, error) {
	rv := reflect.ValueOf(fieldValue)
	if rv.Kind() != reflect.Map || rv.IsNil() || rv.Len() == 0 {
		return "", nil // nil/空 map 写空串
	}
	raw, err := sonic.Marshal(fieldValue)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func init() { schema.RegisterSerializer("tolerantjson", tolerantJSON{}) }
```

字段声明示例：

```go
Metadata map[string]string `gorm:"column:metadata;type:text;serializer:tolerantjson" json:"metadata,omitempty"`
```

### 4.5 语义对照表

| 列内容 | Scan 结果 | 与现行 `decodeRuleMetadata` 一致性 |
|---|---|---|
| NULL / `""` | nil | 一致 |
| `null` | nil | 一致 |
| `{}` / 空对象 | nil | 一致 |
| 损坏 JSON | nil（不报错）| 一致 |
| 合法对象 | 解码 map | 一致 |
| 字段为 nil/空 map 时写入 | `""`（不写 `"null"` 字面量污染列）| 与 `encodeRuleMetadata` 一致 |

### 4.6 注册时序与前置校验

- 模型包 blank import `pkg/db` → 其 `init()` 先于任何 `gorm.Open`/`AutoMigrate`/首次查询的 schema 解析执行（`pkg/prism/alert/model.go` 顶部已挂）。
- 前置校验（实施时已核）：gorm v1.31.2 的 `schema/serializer.go` 中 `SerializerInterface.Scan`/`Value` 签名与注册时序；`Value` 走的是 `SerializerValuerInterface`（接收已取出的 fieldValue 而非 reflect.Value），实现按此落笔。
- `Updates(map[string]any{...})` 的原生 map 更新路径**不经过 serializer**——已核实现有此类路径（`alert/store.go`）均不触碰 metadata/config 列，实施时以 grep 复验。

### 4.7 消费点迁移清单

| 现消费点 | 处置 |
|---|---|
| `alert.Rule.MetadataMap()`（model.go:60）| 删除。**经核实无任何生产调用方**（仅定义处），迁移面为零 |
| `pkg/prism/alert/register.go:88` `marshalSpecMetadata(spec.Metadata)` | 改为直接 `Metadata: spec.Metadata`，删除 marshal 辅助 |
| `internal/api/service/prism/alert.go:202,220` decode/encode | 随转换器整体删除 |
| `internal/api/service/telemetry/telemetry.go:320-325,344,359,364-373` | 随 `pointToTask/taskToPoint/mergeTaskOntoPoint/configToString` 删除 |
| `pkg/prism/alert/env.go:93` `res.Metadata` | **无关**（资产侧字段），不动 |

---

## 5. 分档优化方案

### 5.1 Tier 1 —— 纯合并（wire 零变化）

| 实体 | 动作 |
|---|---|
| remediation Rule/Record | 删 handler `types.go` 两 struct；Service 接口与实现改用 `pkg/prism/remediation` 模型。模型 tag 调整：`TenantID`、`Metadata`、`DeletedAt` → `json:"-"`（三者今日均不在 wire 上，tag 调整即零变化）；`Record.UpdatedAt` → `json:"-"`。运行态字段防篡改由既有列白名单承担（store.go:162）|
| channel | 删 handler `Channel`；`pkg/prism/channel.Record` 补 json tag（8 字段，含 `config` 维持字符串）；`DeletedAt` → `json:"-"`。反向转换今日只拷 4 个可写字段（channel.go:167-174），合并后由 store 更新白名单接管 |
| system Config | 删 handler `Config`；`systemConfig` 补 json tag（3 字段）|
| auth tokenData | 删 `handler.go:40-46`，端点直接返回 `TokenPair`（types.go:18）|

净效果：CE 删 ~200 行转换代码，x 仓对应复制品同步失去存在理由。

### 5.2 Tier 2 —— 消解差异后合并

**alert.Rule**：
- 模型加 json tag（10 字段，与现 handler Rule 的 tag 逐字对齐）；`TenantID/DeletedAt` → `json:"-"`；`Metadata` 改 map + `serializer:tolerantjson`（4.4）。
- 删 `ruleModelToHandler/ruleHandlerToModel/decodeRuleMetadata/encodeRuleMetadata`；Service 直返模型。
- Update 路径不再手工恢复 `ID/CreatedAt`（列白名单已保证）；`ruleUpdateColumns` 不变。

**alert.Record**：
- 直接用 DB 模型。wire 变更见 6.1（`fired_at`→`triggered_at`；`created_at` 增量暴露）。
- 空 severity 补默认 `"warning"` 的逻辑（alert.go:256-260）移入模型 `AfterFind`：`Severity == ""` 时置 `"warning"`，与列 default:'warning' 语义对齐，遗留空值行自愈。

**telemetry MonitorPoint**：
- 删 handler `Task`（`Telemetry` 上报体与各 handler-local 视图 struct 保留，它们是真实 DTO）。
- 模型 tag 调整：`TenantID/Status/Interval/Timeout` → `json:"-"`（今日均不在任务 CRUD wire 上，保持零变化；`interval/timeout` 未来若要暴露属增量 API 决策，另行评审）；`Config` 改 map + serializer。
- 删 `pointToTask/taskToPoint/mergeTaskOntoPoint/configToString`。`mergeTaskOntoPoint` 对 `Status/Interval/Timeout` 的手工保全由 telemetry store 的更新列白名单接管（实施时若该 store 尚无白名单，先补齐再合并）。

### 5.3 Tier 4 —— 护栏补齐（asset 反例修正）

- `asset.Asset`：`TenantID` → `json:"-"`（消除 wire 暴露与绑定面）；`Metadata` 维持 string + 现有 tag（改 map 属 wire 变更，超出本批范围，列为后续可选）。
- `user.User/APIKey` 的 `json:"-"` 敏感字段现状已正确，不动。

### 5.4 Tier 3 —— task 域（完整设计；已实施，as-built 记录见 5.4.8）

> 本节 2026-08-24 由"方向立项"升级为完整设计。**2.4 分档 C 的初判（"领域层语义真实,分层应保留"）经证据复核后推翻**，理由见 5.4.1。三项关键决策（D1 词表统一 / D2 timeout 提升 / D3 重试字段复活）在评审问询未获回答的情况下按推荐项定案，与 D4 wire 键改名一并标注于 5.4.3，**评审时重点确认**。

#### 5.4.1 现状复核：三层模型、两段转换、四组实锤病灶

task 是全仓唯一真正的三层模型，两段手写转换：

```
handler wire   pkg/api/handler/task/types.go   Task(:13-32) / Execution(:44-61)
    ↕  converter.go 274 行：DomainTaskToHandler(:70) / HandlerToDomainTask(:104) / DomainExecutionToHandler(:171) + 5 个 helper
领域 struct    pkg/task/types.go               Task(:126-164) / Execution(:169-210) —— 纯数据，无 gorm tag、无行为方法
    ↕  ToTask(model.go:60) / taskToModel(store.go:177) / ToExecution(:132) / ExecutionToModel(:158)
GORM 模型      pkg/task/model.go               ScheduleTask(sys_schedule_task,:21-54) / ScheduleLog(sys_schedule_log,:100-129)
```

初判推翻的复核证据：

| 初判认定的"领域语义" | 复核结论 |
|---|---|
| `Timeout time.Duration` 富语义 | 仅类型差：DB 列本就是秒（model.go:34），一行方法可解（`time.Duration(t.TimeoutSeconds) * time.Second`） |
| 状态机翻译 | 仅词表映射：`ToLifecycleStatus`（types.go:95-106）12 行纯函数，且 `unknown`→`failed` 有损坍缩 |
| 领域层承载调度语义 | 引擎消费的是 metadata 字符串袋的键：`extractScheduleConfig` 读 `schedule_type/cron_expr/interval`（schedule.go:24-43），Pause/Resume 写 `metadata["enabled"]`（manager.go:333,385）——领域 struct 的富字段反而不在引擎解析主路径上 |

实锤病灶（合并后全部消失，证据钉死）：

1. **死列且每次重置**：`max_retries/retry_interval` 列在 upsert 白名单（store.go:161-162）但 `taskToModel` 从不赋值——**每次 Save 都把两列写回 0**；runner 的重试读取（`executor/lifecycle.go:296-317` 读 metadata 键 `max_retries/retry_interval`）因无任何写入方而永不生效，wire 的 `retry_policy` 字段空转。
2. **wire 时间戳是 metadata 字符串**：Task 的 `created_at/updated_at` 由 converter 在创建时写入 metadata 字符串（converter.go:147-152）再原样读回（:231-240）；DB 真实时间戳列 `ToTask` 从不读回（model.go:60-83），两套时间可漂移。
3. **有损坍缩与吞错**：`unknown`→`failed`（ToLifecycleStatus）；Config 双向 sonic 转换失败静默吞错（converter.go:85-90,136）；`enabled` 解析失败静默置 false（:226-230）。
4. **config 保留键夹带**：`tenant_id/asset_id/timeout/priority/depends_on` 五个非 executor 配置键藏进 wire config map，提取/剥离机制（converter.go:58-64,119-140,194-217）是 converter 复杂度大头，"timeout 是保留键"属隐性契约。

#### 5.4.2 目标：收敛为单一双 tag 模型

`pkg/task.Task` 兼作 GORM 模型（表名仍 `sys_schedule_task`，吸收 ScheduleTask 全部列）；`pkg/task.Execution` 吸收 ScheduleLog（表名仍 `sys_schedule_log`，`Error` 字段以 `gorm:"column:error_msg"` 保留列名，wire 键仍 `error`）。`pkg/api/handler/task` 仅保留：Service 接口（签名换 `*task.Task/*task.Execution`，保持 x 可导入）、路由 handler 直接绑定模型（gin 绑定不认 `json:"-"`，防 mass-assignment）、`Filter/ExecutionFilter/ExecutionStats`（查询参数与跨行聚合，非模型副本）与 `copyTaskRequest`。

**Task 字段表**：

| 字段 | 列 | wire(json) | 相对现状 |
|---|---|---|---|
| ID/Group/Tags/RunID/RetryPolicy/Concurrency | 已有 | 不变 | tags 维持逗号串列 + Go 侧切分（现状 store 即如此） |
| Name | 已有(varchar255) | `name` | 从 metadata 键提升为字段 |
| Description | **新增列**（AutoMigrate） | `description` | 新列；**不做回填**（两仓未发布定案，开发库重建） |
| ExecutorType | 已有(executor_type) | `executor_type` | 字段更名（原 ExecutorName）；wire 键 `executor`→`executor_type`（D4） |
| Schedule | **新增列**(varchar) | `schedule` | 唯一真源；`schedule_type/cron_expr/interval` 三列**删除**，解析归并 `ParseSchedule`（D5） |
| Enabled | 已有 | `enabled` | 从 metadata 键提升；Pause/Resume 改写列 |
| TimeoutSeconds | 已有(timeout,int64 秒,default 30) | `timeout` | 顶层暴露；方法 `Timeout() time.Duration`；config 保留键机制删除（D2） |
| MaxRetries / RetryInterval | 已有（现为死列） | `max_retries/retry_interval`（秒,可选） | 复活（D3）；runner 改读显式字段 |
| Config | executor_config(text) | `config`(map,挂 tolerantjson) | 纯 executor 配置，不再夹带保留键 |
| CreatedAt / UpdatedAt | 已有 | `created_at/updated_at` | 真实列直读（修复病灶 2） |
| TenantID/AssetID/Priority/DependsOn | 已有 | `json:"-"` | 内部列（D6） |
| Metadata | 已有(text) | `json:"-"`,map + tolerantjson | 收编为扩展键袋（D6） |

**Execution 字段表**：

| 字段 | 列 | wire(json) | 相对现状 |
|---|---|---|---|
| ID/TaskID/StatusCode/Output/Duration(ms)/RetryCount/StartedAt | 已有 | 不变 | |
| TaskName | 无(`gorm:"-"`) | `task_name,omitempty` | service 层 join 赋值（现状如此） |
| ExecutorType | executor_type | `executor_type` | 更名对齐（原 ExecutorName） |
| Status | status | `success/failed/running/unknown` | **词表统一到 API 侧**（D1），删 ToLifecycleStatus/ToStoredStatus |
| Error | error_msg（列名保留） | `error` | 删 ErrorMsg↔Error 改名转换 |
| FinishedAt | finished_at | `finished_at,omitempty`,`*time.Time` | 可空指针（D7），零值不再谎报零年 |
| TenantID/AssetID/Operation/RunID/TriggerType/SkipReason/Metrics | 已有 | `json:"-"` | 暂不暴露，增量暴露另评审 |

#### 5.4.3 设计决策（D1–D3 为问询未获回答、按推荐定案项，评审重点）

**D1 状态词表统一为 API 侧（存储列直接存 `success/failed/running`）**
- 现状：存储 `normal/abnormal/triggered/unknown`（types.go:64-75，注释自述与 `pkg/types.AssetStatus` 刻意同值且"不得混用"——同值依赖本身就是耦合）；API 侧 `success/failed/running`；CE/x web 全部已说 API 词表（CE mock 头注释即 "status limited to success/failed/running"），**web 零改动**。
- 定案：存储列改存 API 词表；`unknown` 保留为合法存储值原样输出，不再坍缩为 `failed`（行为微变）；删 ToLifecycleStatus/ToStoredStatus 及 handler 查询参数翻译（handler.go:242）。
- Go 改动面（机械替换）：CE Status 常量、依赖门控（events.go:42-59 改比较 StatusSuccess）、Stats SQL 字面量（store.go:379-412 的 `'normal'/'abnormal'`）；x 仓 task_status_processor 的 normal/abnormal 写入（task_status_processor.go:447-535）、health 查询（health/store.go:51,87）、remediation 持久化读取。
- 备选：保留双词表，ToLifecycleStatus 成为唯一幸存映射（~12 行），x 改动最小，但词表分裂永久化、"去掉转换"打折。

**D2 timeout 提升为顶层字段**
- wire 顶层 `timeout`（int 秒）；删除 config 保留键机制全套（converter.go:58-64,119-140,194-217 及 parseInt64FromAny/parseIntFromAny/parseDurationFromAny helper）。
- CE web TaskForm 各 executor 配置内的超时输入（http 10/tcp 5/icmp 3/local 60 秒）本就经保留键提取为任务级超时、并从 executor config 中剥离——表单归一为一个任务级输入（i18n 键 `task.create.timeout` 已存在），`buildExecutorConfig` 不再写 timeout。

**D3 max_retries/retry_interval 复活为真实字段**
- wire 暴露 `max_retries`(int)/`retry_interval`(int 秒)，omitempty 可选；runner 的 buildRetry（executor/lifecycle.go:296-317）改读事件 payload 显式字段（见 5.4.4），删除 metadata 键读取；`retry_policy` 由此获得实义；x web 创建表单期待的字段也就位。
- 备选：连列带读取全删（与 2026-08-24 兼容代码清理批一致），重试功能整体移除——若取此案，wire 的 `retry_policy` 字段一并删除。

**D4 wire 键 `executor` → `executor_type`**
- 与 Execution 侧及 DB 列名统一；CE web 约 10 处机械改名（types/task.d.ts、TaskForm、create/edit 构参、List/Detail、mock、httpapi task_test）；x web task api 本就与真实后端不匹配（mock 驱动的扩展版表单直发 camelCase 扩展形状会被后端拒收），其表单对齐另立项。

**D5 schedule 单列真源**
- `Schedule string` 列为唯一真源；引擎解析归并为 `pkg/task.ParseSchedule(schedule string) (ScheduleType, cronExpr string, interval time.Duration, err error)`——由现 scheduleToMetadata（scheduler/task.go:400-414）的分类逻辑（空→event/Go duration→interval/其余→cron）与 parseSchedule（schedule.go:48-78）合并而成；service 创建/更新与引擎 Register/Restore（persistence.go:28-115）统一调用。
- `schedule_type/cron_expr/interval` 三列删除；telemetry `pointToProbeTask`（prober_service.go:191-228）改直接设 Schedule 字符串与 Metadata 扩展键（monitor_point_id/expression）。

**D6 Metadata 收编为扩展键袋**：`map[string]string` + tolerantjson（第 4 章 serializer 复用），`json:"-"`。剩余合法键清单写入模型注释：`monitor_point_id/expression`（telemetry 探测）、x 仓运行态键（task_status_processor 写入的 `status`）。`tenant_id/asset_id/priority/depends_on` 不暴露 wire——单租户现状下 tenant_id 恒 0，其余三者无前端消费方，暴露属增量 API 决策另评审。

**D7 FinishedAt 可空指针**：列可空，wire omitempty 与现状一致；写侧（runner、TriggerTask 占位行、x processor）改取址。

**维持的 quirk（记录不改，另立评审）**：任务 ID 由 service 层原子计数器分配（scheduler/task.go:378-395，引擎注册需先有 ID），非 DB auto-increment 直取；引擎持久化 setTask/deleteTask 失败 log-and-continue（persistence.go:130-160）。

#### 5.4.4 CE 改造点（实施清单）

| 层 | 动作 |
|---|---|
| 转换删除 | converter.go 全文件（274 行）+ converter_test.go；model.go 的 ToTask/taskToModel/ToExecution/ExecutionToModel；scheduler/task.go 的 scheduleToMetadata；ScheduleTask/ScheduleLog 两 struct 并入 Task/Execution |
| 引擎 | Register/Update/Restore 改读字段（统一走 ParseSchedule）；Pause/Resume 改写 Enabled 列并持久化（替代 metadata["enabled"]）；trigger 事件 payload（pkg/event）显式携带 TimeoutSeconds/MaxRetries/RetryInterval（替换现裸纳秒 int，events.go:127），Metadata 袋仅附扩展键；依赖门控词表替换（events.go:42-59） |
| runner | buildRetry 改读 payload 显式字段；ApplyJudgment 的 expression 继续从 metadata 袋读（lifecycle.go:128） |
| store | 模型即存储，转换归零；Save 白名单补 description/schedule/timeout/max_retries/retry_interval/enabled/name 列（仍护 created_at/deleted_at）；Stats SQL 词表替换；ExecutionRecordStore 的 ns→ms 换算（store.go:473）保留 |
| handler/service | Service 接口签名换模型；创建/更新直接绑定 Task 模型（name 必填校验维持手写），guards 保留（ID/CreatedAt 置零与恢复，scheduler/task.go:135-139,172-175）；UpdateTask 不再经 handler 往返保全 CreatedAt（:174）；ListExecutions 的 task_name 过滤改 SQL（name 已是列，替代内存 names map :266）；execution 查询参数词表翻译删除 |
| telemetry | pointToProbeTask 改设字段（Schedule/Enabled/TimeoutSeconds/Metadata 扩展键） |
| web | executor→executor_type 改名；TaskForm 超时归一为任务级输入；mock 同步；状态词表零改动 |
| OpenAPI | task 段重写：补 pause/resume 路径；修正 executions 子资源路径（`/tasks/{id}/executions`）；列表 filters（group/tags/task_name/executor/status）；TaskCreateRequest 补 group/tags/retry_policy/concurrency/timeout/max_retries/retry_interval；config 示例改数值秒并去保留键；状态枚举去 `pending` 幽灵值；schedule 描述去不支持的 `@every` |

#### 5.4.5 tickraft-x 同批改造

| 位置 | 动作 |
|---|---|
| internal/api/service_task.go | 重写为直连模型（同 service_alert_prism 模式）：删 scheduleToMetadataLocal(:406-420)、lifecycleToStoredStatus 及全量拉取后内存过滤（:217-239,258-281,329-350）——ExecutionStore.Query 原生支持 status SQL 过滤；task_name 过滤改 SQL；Pause/Resume 改字段（:185-215） |
| worker | bridge.go(:286-309) 改读 Enabled/Schedule 字段；task_status_processor 改写字段与列（含词表替换） |
| 类型名 | ScheduleTask/ScheduleLog 引用点全替换为 Task/Execution（worker start.go 的 AutoMigrate、health、remediation、配额计数、syncer） |
| syncer_decorator | 签名随 Service 接口换模型 |
| x web | task api 与真实后端本就不匹配（mock 驱动开发），表单对齐记为 x 侧后续项，不阻塞本批 |

#### 5.4.6 测试计划

- **删**：converter_test.go 全部。
- **改**：httpapi task_test 的 `executor` 键→`executor_type`、创建 payload 增任务级 timeout 断言；scheduler/task_test 的 ScheduleToMetadata 表测改 ParseSchedule 表测；model_test 的 ToTask metadata 解析断言改模型直读。
- **增**：ParseSchedule 表测（空/Go duration/cron/`@daily`）；词表统一回归（unknown 原样输出）；Save 白名单回归（max_retries 不再被重置为 0）；"PUT 不改运行态字段"护栏断言（ extends 第 8 章）。
- **不变**：分页 envelope、tests/httpapi 其余 snake_case 断言、store 层 D-01/D-02 类回归。

#### 5.4.7 实施顺序与验收门

CE 先行（模型 → 引擎 → store → service → handler → web → OpenAPI）→ x 同批（5.4.5）。验收：CE `go test ./...` 100% 绿 + golangci-lint 零告警 + 红线扫描；x 三套构建标签（见该仓 Makefile）构建与测试全绿 + lint 零告警；grep 断言：`pkg/api/handler/task` 与 `pkg/task` 下不再存在 `*ToHandler/*ToModel/*ToTask` 命名的实体转换函数，`ToLifecycleStatus/ToStoredStatus/MetadataKeyEnabled` 零残留。

#### 5.4.8 实施记录（as-built，2026-08-24 交付）

设计已按上述定案实施完毕，两仓验收门全部通过（CE 全量测试 + lint + 红线；x 三套构建标签构建/测试 + lint 零告警）。与设计稿的偏差与落地细节：

- **词表桥位置**：资产词表→存储词表的唯一桥接为 `task.ExecutionStatusFromAsset`（pkg/task/types.go:67，normal→success、abnormal→failed、其余→unknown）。executor Result 与完成事件仍用资产词表（AssetStatus），不落库；`sys_schedule_log.status` 只存 API 词表。x 仓 remediation 的 skip 记录保留 x 专属 `skipped` 标记（不在任一词表内，列表查询以 `status IN (success, failed)` 过滤不展示，语义由 skip_reason 列承载）；x grpc 外部通道的 `trigger_type=external` 同为 x 专属值，保留。
- **schedule 解析落地形态**：设计稿的 "ParseSchedule 归并" 落地为 `task.ClassifySchedule`（导出，校验/分类用，pkg/task/schedule.go:20）+ 包内私有 `parseSchedule`（引擎装填用）。语义与设计一致：`""` 为事件驱动、Go duration 为定间隔、其余按 cron 解析；`once` 类型删除（x 仓 once 单测随删）。
- **事件 payload 字段更名**（pkg/event.ExecutionPayload）：`Timeout`（裸纳秒 int）→ `TimeoutSeconds`，新增 `MaxRetries`/`RetryIntervalSeconds`，`Config` 由 map 改为 sonic 序列化字符串，`Action` 携带 `Operation.String()`（x 仓原硬编码 "triggered" 一并对齐）。runner 重试自此有真实数据来源，retry 链路复活。
- **enabled 门控修正**：x 仓 bridge 的 `isTaskEnabled` 由 "metadata 缺省视为启用" 改为直读 `Enabled` 列——metadata 缺省默认开启的隐式语义随字段化一并消除，未显式启用的任务不再被调度。
- **GORM default 标签零值替换 bug**：实施中抓到——带 `default` 标签的列在零值插入时会被 GORM 替换为列默认值，导致语义翻转（`Enabled=false` 存成 `true`、`Concurrency=0`（无限）存成 `1`）。修复：这两个列去掉 default 标签，默认值全部由服务层显式赋值（timeout 仍保留列默认 30，因服务层 create 已显式赋 30，零值路径不可达）。x 仓 store 单测同步覆盖。
- **x 分布式模式差异**：x `service_task.go` 无引擎（Worker 节点跑调度），Pause/Resume 直写 Enabled 列并经 Syncer 推送；Create/Update 保全 `json:"-"` 内部字段（TenantID/AssetID/Priority/DependsOn/Metadata/Operation）、timeout<=0 时沿用现值，与 CE 引擎版行为对齐；列表过滤走 `ExecutionStore.Query` 原生 SQL（status/executor_type/task_name），内存过滤函数全删。
- **CE web**：TaskForm 超时归一为任务级 timeout/max_retries/retry_interval 三输入（秒），各 executor config 不再携带 timeout 键；列表/详情/mock（14 条种子）全量 executor_type 化。
- **OpenAPI**（docs/api/openapi.yaml）：executions 修正为真实子资源路径 `/tasks/{id}/executions(/{execId})`；补 pause/resume 路径与 409；列表补 group/tags/status/executor_type/task_name 过滤参数；copy 响应码 201→200（实际 envelope 恒 200）；状态枚举去 `pending`；schedule 描述去 `@every`/`@daily`，改为 cron/Go duration/""三态；config 示例去 timeout 保留键。

---

## 6. wire 变更清单

### 6.1 破坏性变更（已批准，仅此一处）

`alert.Record`：`fired_at` → `triggered_at`。

- 影响面：CE web 5 个文件（`web/packages/features/src/api/prism.ts`、`mock/prism.ts`、`views/prism/record/list/List.vue`、`views/prism/record/detail/Detail.vue`、`views/dashboard/overview/index/Index.vue`）机械改名 `firedAt` → `triggeredAt`。
- 实施勘误：设计期勘误为"tickraft-x web 无引用"，实施时核实 **x web 有 3 处**（`web/src/api/prism/record.ts`、`web/src/mock/prism-record.ts`、`web/src/views/prism/record/list/List.vue`，另含悬空的 `common.firedAt` i18n 引用），已随本批同步改名 `triggeredAt`/`common.alertTriggeredAt`；CE 后端测试（tests/httpapi）无该键断言。

### 6.2 增量暴露（向后兼容）

`alert.Record` 响应新增 `created_at`（模型本就带 `json:"created_at"` tag）。

### 6.3 不变项

remediation、channel、system、auth、telemetry 任务 CRUD 的响应字段集合与命名均不变；`metadata` 字段 wire 形态从 handler 时代的"解码后对象"维持为"解码后对象"（serializer 在模型内完成，客户端无感知）；分页 envelope（`items/total/page/size`）不动。

### 6.4 Tier 3（task 域）变更（已实施，as-built 见 5.4.8）

- **破坏性**：Task wire 键 `executor` → `executor_type`（与 Execution 侧及列名统一，5.4.3-D4）；`config.timeout` 保留键语义删除（此后 config 内的 timeout 一律视为普通 executor 配置，任务超时走顶层 `timeout` 字段，D2）；执行状态 `unknown` 不再坍缩显示为 `failed`（原样输出，D1）。
- **增量暴露（可选字段）**：Task 新增顶层 `timeout`（秒）、`max_retries`、`retry_interval`（秒）。
- **存储词表换血**（对 API 调用方透明）：`sys_schedule_log.status` 改存 `success/failed/running/unknown`（原 `normal/abnormal/triggered`）；两仓未发布，不做数据迁移，开发库重建。
- **不变**：Task 其余字段（`name/description/schedule/enabled/config 形状/group/tags/run_id/retry_policy/concurrency/created_at/updated_at`）、Execution 其余字段、分页 envelope 均维持。

---

## 7. 双仓实施顺序

### 7.1 CE 先行（一个批次内按依赖排序）

1. `pkg/db/jsonmap.go`（4.4，sonic 实现）+ gorm 接口复核（4.6）。
2. 模型侧：alert Rule/Record、MonitorPoint、channel Record、remediation Rule/Record、asset 的 tag 与 serializer 变更（纯模型层，引擎与 store 编译随动）。
3. Service 层：各 `Service` 接口签名从 handler DTO 切换为模型类型；删除转换器与 `taskconv` 委托壳。
4. Handler 层：删除 `pkg/api/handler/{alert,channel,remediation,system}` 的重复 struct 与 auth `tokenData`；`doc.go` 的 "Handler types" 节改写为新约定。
5. `register.go` metadata 直赋；`AfterFind` severity 默认。
6. OpenAPI：重写 `AlertRule/AlertRecord` schema 至现行模型（顺带修正 camelCase/snake_case 混用），`Task` schema 补缺失字段。
7. web：`firedAt` → `triggeredAt` 改名 + 契约测试。

### 7.2 tickraft-x 同批跟进

1. 删除 `internal/api/service_alert_prism.go` 全部 `prismRuleModelToHandler/prismRuleHandlerToModel/prismRecordModelToHandler`，Service 实现直返 CE 模型。
2. `service_task.go:427` `executionToHandlerLocal` 与 `service_telemetry.go` 的本地转换随 CE 接口签名切换；`syncer_decorator.go` 适配。实施修订：execution 转换按第 3 章约定上收为 `pkg/api/handler/task.DomainExecutionToHandler`（含状态归一化与全字段映射，修正 x 本地副本缺 ExecutorType/StatusCode/Duration/RetryCount 的漂移），CE `internal/api/service/scheduler` 与 x `service_task.go` 均改调它，测试随迁。
3. 内嵌模型核对：`governance.Record` 内嵌 `alert.Record`（`internal/prism/governance/model.go:241`）自动继承 `triggered_at` 改名；x 侧后端消费点经编译核实无 `fired_at` 残留，x web 的 3 处引用见 6.1 实施勘误。
4. x 特有的扩展构建标签（见该仓 Makefile）需在本批合入后单独跑构建与测试验证。

### 7.3 验收门

- CE：`go test ./...` 100% 绿（含 tests/httpapi 全量）；golangci-lint 零告警（Tier 1 基线）。
- x：常规构建绿 + 扩展标签构建绿 + x 测试全量。
- grep 断言：`internal/api/service` 下不再存在 `*ToHandler/*ToModel` 命名的实体转换函数（taskconv 删除后）。

---

## 8. 测试计划与验收

| 类别 | 动作 |
|---|---|
| 新增 | `pkg/db/jsonmap.go` 单测：4.5 语义对照表全 case（NULL/""/null/{}/坏 JSON/合法/双向写空串）|
| 保留 | store 层 D-01/D-02 回归（`pkg/prism/alert/store_test.go:91-100`、`pkg/prism/remediation/store_test.go:107-144`）——它们测的是列白名单行为，与模型合并正交，必须继续通过 |
| 删除 | `internal/api/service/prism/alert_test.go` 等转换器单测（含 :465 的全字段 PUT 回归注释——该保护语义已由列白名单承接）|
| 不变 | tests/httpapi 的 snake_case key 断言（`prism_test.go:53,126,145`）；播种路径 `prismalert.Record` 字面量（prism_test.go:85-93）随字段类型编译修正 |
| 更新 | web 契约测试与 5 个 `firedAt` 改名文件 |
| 手工 | 完整走一遍规则创建→触发→记录查询（验证 serializer 落盘为合法 JSON、列内无 `"null"` 字面量）|

---

## 9. 风险与非目标

**风险与缓解**

| 风险 | 缓解 |
|---|---|
| serializer 注册晚于 schema 解析（init 顺序）| 4.6 前置校验 + 实施第一批动作 |
| 历史列中存在 `"null"` 字面量 | Scan 容错归一为 nil；4.5 表已覆盖 |
| 合并后客户端提交运行态字段（status 等）| `json:"-"` + store 列白名单双保险（约定 2/3）；httpapi 增一条"PUT 不改 status"的回归断言 |
| x 仓内嵌模型 wire 意外变化 | 7.2.3 核对；x web 已验证无 `fired_at` 引用 |
| telemetry store 若无更新白名单则合并引入回归 | 5.2 前置：先补白名单再合并 |

**非目标**

- 不改分页契约（page/size、PageData envelope）。
- 不动 channel.Config 的 wire 形态（维持 JSON 字符串）。
- 不动 asset.Metadata 的 wire 形态（string → map 属后续可选批次）。
- task 域 schema 重设计已完成完整设计（5.4，2026-08-24），代码待评审确认后另批实施，不与已交付的 Tier 1/2/4 批次混合。
- 不引入任何代码生成（oapi-codegen 等）；OpenAPI 仍手写维护。
