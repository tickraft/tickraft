# Model Layering Design

> Status: **design review draft**. This document is the authoritative design for merging and refactoring the data models of the API layer and the persistence layer; implementation begins only after full confirmation.
> Scope: `pkg/prism/{alert,channel,remediation}`, `pkg/telemetry`, `pkg/task`, `pkg/asset`, `pkg/api/handler/*`, `internal/api/service/*`, plus **synchronized updates in tickraft-x** (Chapter 7).

> **Implementation status (updated 2026-08-26)**: this design has been implemented. Paths cited in sections such as the "current API layer / converter inventory" are **design-time snapshots** kept as decision records and no longer correspond one-to-one to the as-built tree. The service layer was later pushed down further into domain packages; the final path mapping:
> `internal/api/service/{scheduler,prism,telemetry,system}` → `pkg/task`, `pkg/prism/{alert,channel,remediation}`, `pkg/telemetry`, `pkg/system` (as of 2026-08-27 the service subpackages have been flattened into the domain roots; read the old paths below against this mapping);
> SPI+DTO moved along with them from `pkg/api/handler/<domain>/types.go` into the packages above. For the as-built architecture see `docs/architecture.md`.
> Premise: neither repo has been released. Tier 1/2/4 have been implemented and delivered (2026-08); **Section 5.4 Tier 3 (the full task-domain design) was added on 2026-08-24 and awaits a separate implementation batch after review confirmation**; the proposed wire changes are in 6.4. The only breaking wire change in the delivered batches is `fired_at` → `triggered_at` (6.1); all other entity response fields are unchanged.

---

## Table of Contents

1. [Background and Problem](#1-background-and-problem)
2. [Current-State Inventory: Model Comparison and Tier Classification](#2-current-state-inventory-model-comparison-and-tier-classification)
3. [Target Layering Conventions](#3-target-layering-conventions)
4. [Tolerant JSON Serializer Design (Final)](#4-tolerant-json-serializer-design-final)
5. [Tier-by-Tier Optimization Plan](#5-tier-by-tier-optimization-plan)
6. [Wire Change List](#6-wire-change-list)
7. [Two-Repo Implementation Order](#7-two-repo-implementation-order)
8. [Test Plan and Acceptance](#8-test-plan-and-acceptance)
9. [Risks and Non-Goals](#9-risks-and-non-goals)

---

## 1. Background and Problem

### 1.1 Current State: Two to Three Handwritten Models per Entity + Handwritten Bidirectional Conversion

The current API layer (`pkg/api/handler/*/types.go`) and the persistence layer (`pkg/*/model.go`) each define their own struct per entity, stitched together by handwritten field copies in the `internal/api/service/*` adapter layer. A full-repo inventory found:

- The CE repo has roughly **25 named conversion functions and 450–500 lines of handwritten mapping code** (details in 2.1).
- tickraft-x cannot import CE's `internal/`, so it **duplicated the entire mapping logic** (`tickraft-x/internal/api/service_alert_prism.go:172-247`, `service_task.go:427`, `service_telemetry.go`). Maintaining the same semantics twice is currently the single largest consistency risk.
- The only genuine three-layer model in the repo is task: handler `Task` → domain `pkg/task.Task` → GORM `ScheduleTask`. Its converter (`pkg/api/handler/task/converter.go`, 274 lines) shuttles restricted fields back and forth across two round trips through the metadata map and reserved config keys, silently swallows parse failures and returns zero values (`converter.go:192-214`), and the `"enabled"` key is an implicit storage contract (`converter.go:42-50`, whose own comment admits renaming it would orphan data).

### 1.2 Verified Costs

| Cost | Evidence |
|---|---|
| Historical bug class: PUT wiping fields (D-01/D-02) | Root cause is exactly "DTO carries no runtime-state fields + full-column overwrite"; even after the fix, every Update path still has to manually restore `ID/CreatedAt` in place (`internal/api/service/prism/alert.go:100-101`, `channel.go:91-92`, and `mergeTaskOntoPoint` at `telemetry.go:350`) |
| Adding one DB field requires synchronized changes in 4–6 places | DB model, handler model, converters in both directions, (the copy in the x repo), OpenAPI schema |
| OpenAPI spec drift | The `AlertRule` schema in `docs/api/openapi.yaml` (:2030-2109) is still the pre-refactor metric/operator/threshold model with no `expression/priority/group_id/metadata`; and the spec uses camelCase (:2073 `createdAt`) while the actual wire is snake_case |
| The documented rationale for the separation does not hold | `pkg/api/handler/doc.go:37` claims handler-local types let "handlers be unit-tested without a database"; but the converter itself imports the `pkg/task` model package (`converter.go:14`), and the model structs are pure data types with no dependency on a database connection |
| Silent field loss | `executionToHandler` drops `run_id/trigger_type/skip_reason/metrics` already present in the DB; `recordModelToHandler` renames `TriggeredAt`→`FiredAt` and drops `CreatedAt` (`alert.go:256-273`) |

### 1.3 Conclusion

The duplication falls into two classes: **ceremonial duplication with identical shapes** (should be merged), and **genuine layering where the shapes truly differ** (should be kept but with the implementation fixed). Neither a blanket "merge everything into the DB model" nor a blanket "keep all DTOs" is right; the bare binding of `asset.Asset` (see 2.4) proves in parallel that merging without guardrails is just as harmful.

---

## 2. Current-State Inventory: Model Comparison and Tier Classification

### 2.1 Conversion Function Landscape (CE)

| Location | Functions | Size |
|---|---|---|
| `internal/api/service/prism/alert.go` | `ruleModelToHandler`:194, `ruleHandlerToModel`:213, `decodeRuleMetadata`:227, `encodeRuleMetadata`:241, `recordModelToHandler`:256 | ~120 lines |
| `internal/api/service/prism/channel.go` | `channelModelToHandler`:152, `channelHandlerToModel`:167 | ~25 lines |
| `internal/api/service/prism/remediation.go` | `remediationRecordToHandler`:206, `remediationModelToHandler`:225, `remediationHandlerToModel`:251 | ~60 lines |
| `internal/api/service/telemetry/telemetry.go` | `pointToTask`:306, `taskToPoint`:332, `mergeTaskOntoPoint`:350, `configToString`:364 | ~70 lines |
| `pkg/api/handler/task/converter.go` | `DomainTaskToHandler`:70, `HandlerToDomainTask`:104 plus 5 helpers | ~274 lines |
| `internal/api/service/scheduler/task.go` | `executionToHandler`:453, `scheduleToMetadata`:397 | ~35 lines |
| `internal/api/service/taskconv/adapter.go` | pure delegation shells :19, :25 | 9 lines |
| `internal/api/router/router.go` | `serviceAdapter` :48-172, `userToProfile`:162 | ~120 lines |
| `pkg/task/model.go` / `store.go` | `ToTask`:60, `ToExecution`:132, `ExecutionToModel`:158, `taskToModel`:177 | ~125 lines |
| `internal/api/service/system/system.go` | Get/UpdateConfig manual mappings :104-134 | ~30 lines |

### 2.2 Tier A — Ceremonial Duplication (one-to-one fields, zero semantic transformation)

| Entity | Handler side | Persistence side | Evidence |
|---|---|---|---|
| remediation Rule | `pkg/api/handler/remediation/types.go:19` (15 fields) | `pkg/prism/remediation/model.go:52` | **The worst duplication in the repo**: the field names, types, and json tags of all 15 shared fields are verbatim identical; the DB model already carries the same json tags. The only difference is that the DB side has `TenantID/Metadata/DeletedAt` extra (the handler never serializes those three) |
| remediation Record | types.go:63 (12 fields) | `pkg/prism/remediation/record.go:37` | One-to-one; DB has `UpdatedAt` extra (it is dropped) |
| channel | `pkg/api/handler/channel/types.go:16` (8 fields) | `pkg/prism/channel/model.go:22` | One-to-one, same names and types (including `Config string` as a JSON string on both sides); DB has `DeletedAt` extra |
| system Config | `pkg/api/handler/system/types.go:13` (3 fields) | `internal/api/service/system/system.go:30` | 3/3 pure copies |
| auth tokenData | `pkg/api/handler/auth/handler.go:40` | `TokenPair` (same package, types.go:18) | **Same-layer duplication**: 5/5 fields and json tags verbatim identical |

### 2.3 Tier B — Small Shape Differences (resolvable within the model)

| Entity | Difference | Resolution |
|---|---|---|
| alert Rule | handler `Metadata map[string]string` (types.go:21) vs DB `Metadata string` (model.go:44); the DB model has no json tags; DB has `TenantID/DeletedAt` extra | json tags + tolerant serializer (Chapter 4) + internal columns `json:"-"` |
| alert Record | renames `TriggeredAt`→`FiredAt`; the handler drops `CreatedAt`; empty severity is backfilled by the converter as `"warning"` (alert.go:256-260) | use the DB model directly; the `triggered_at` rename (approved); the severity default moves into `AfterFind` |
| telemetry monitor point | handler `Config map[string]any` (types.go:50) vs `MonitorPoint.Config string` (model.go from :118); DB has `TenantID/Status/Interval/Timeout` extra and already carries json tags | Config goes through the serializer; the four internal/derived columns get `json:"-"` to keep the wire unchanged |

### 2.4 Tier C/D — Genuine Layering and the Counter-Example

- **task (Tier C; the 2026-08-24 re-review overturned the initial verdict)**: the initial verdict held that the domain-layer semantics were real (`Timeout time.Duration`, state-machine translation — storage `normal/abnormal/triggered` ↔ API `success/failed/running`, `pkg/task/model.go:92-123`) and that the layering should be kept. Deeper investigation (5.4.1) showed the domain structs have no behavior methods and the engine actually consumes keys of the metadata string bag rather than the rich fields, so "the layering should be kept" does not hold — the final decision is to converge the three layers into a single dual-tag model (5.4); the pain point the initial verdict flagged (handler-only fields being stuffed into the metadata map and round-tripped, see 1.1) disappears with the merge.
- **asset (Tier D, the counter-example)**: `TenantID` in `pkg/asset/model.go:16` carries `json:"tenant_id"` (:27) and is exposed directly on the API; the handler binds directly to that model via `BindAndValidate`, so clients can submit internal fields. This proves merging must have guardrails (`json:"-"` + column whitelist) and cannot run bare.
- **Incidental finding**: `apiKeyData` in the auth domain (handler.go:72) is a template of a **legitimate DTO** — the create response needs `raw_key`, while the `user.APIKey` model must never carry the plaintext secret. It is the positive proof of the "keep a DTO only for genuine divergence" convention.

### 2.5 Pre-existing Inconsistent Precedents

The codebase already mixes three styles: `alert.Record`, `remediation.Rule`, and `asset.Asset` are single models with dual json+gorm tags; alert Rule is a pure gorm model + handler DTO; `user.APIKey` is exposed directly through the Service interface (types.go:46-49). This design unifies everything into one convention (Chapter 3).

---

## 3. Target Layering Conventions

The following conventions go into the model section of `docs/module-boundary.md` (updated in sync during implementation):

1. **Single dual-tag model by default**: an entity has exactly one struct carrying both json and gorm tags. A separate DTO is allowed only when the API shape and the storage shape **genuinely diverge** (e.g. a create response containing a secret the model must never hold, or a cross-table aggregate view); merge back once the divergence is gone.
2. **Internal columns are always `json:"-"`**: `tenant_id`, `deleted_at`, internal blobs, runtime-state counters and the like are neither emitted nor bindable. hertz/gin JSON binding ignores `json:"-"` fields, which prevents mass-assignment by construction.
3. **Write paths must go through the store column whitelist**: the `Select(updateColumns).Updates(...)` pattern (precedents: `pkg/prism/alert/store.go:66-86`, `pkg/prism/remediation/store.go:162-163`). Writable columns are defined by the store, independent of whether models are merged — this is the mechanism that keeps clients from tampering with runtime-state fields such as `status/last_run_at/consecutive_failures` after the merge.
4. **json tags stay snake_case**: the frontend converts both directions via humps (`web/packages/core/src/utils/naming.ts`, `request.ts:129-137`); merging models never touches the wire format.
5. **Conversion functions that must exist live in `pkg/api/handler/*`** (the location tickraft-x can import); putting them under `internal/` is forbidden — this removes the root cause of the x repo's copy-paste.

---

## 4. Tolerant JSON Serializer Design (Final)

### 4.1 Decision

For fields shaped as string (DB column) ↔ map (API shape), adopt the **custom tolerant serializer, single-field approach**: the field is declared as a map type tagged `serializer:tolerantjson`, and the serializer transparently encodes/decodes on read and write. Do not adopt the "dual field + hook" approach (`Metadata string` + `MetadataMap map`) — that keeps two synonymous representations on the model surface, which is exactly the pain point this effort eliminates.

### 4.2 Beneficiary Fields

| Model field | Current shape | Target shape |
|---|---|---|
| `alert.Rule.Metadata` | `string` (model.go:44) | `map[string]string` |
| `telemetry.MonitorPoint.Config` | `string` (model.go:118 onward) | `map[string]any` |

channel's `Config` is a JSON string on both sides (handler types.go:20); it does **not** go through the serializer and stays as is.

### 4.3 Why the Built-in `serializer:json` Cannot Be Used

The built-in JSON serializer **returns an error on corrupted JSON, failing the entire row query**. The contract of the existing `MetadataMap()` (model.go:57-72) and `decodeRuleMetadata` (alert.go:227-236) is **tolerant degradation**: a bad payload decodes to nil and never blocks rule loading. Switching to the built-in serializer would turn historical dirty data from "degrade" into "down". The custom serializer moves that contract unchanged into Scan.

### 4.4 Implementation (with two implementation-time revisions)

Two revisions were finalized during implementation relative to the original design:

1. **It lives in `pkg/db/jsonmap.go`, not the originally planned `pkg/types/jsonmap.go`**. `pkg/types` has a test-pinned "standard-library-only imports" constraint, while the serializer needs `gorm.io/gorm/schema`; putting it there would break the leaf property. `pkg/db` is semantically the "database facilities" package, and model packages use a blank import (`_ "github.com/tickraft/tickraft/pkg/db"`) to guarantee registration happens before any schema parsing.
2. **JSON encoding/decoding uses `github.com/bytedance/sonic`** (fully API-compatible with `encoding/json`). The serializer sits on the hot path of metadata reads and writes (rule loading, monitor-point bulk queries), where sonic has a clear codec performance advantage on amd64/arm64; it is also an existing dependency (already used in the x repo), so it adds no supply-chain surface.

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
		return nil // "{}" and empty map normalize to nil
	}
	field.ReflectValueOf(ctx, dst).Set(value)
	return nil
}

func (tolerantJSON) Value(_ context.Context, _ *schema.Field, _ reflect.Value, fieldValue any) (any, error) {
	rv := reflect.ValueOf(fieldValue)
	if rv.Kind() != reflect.Map || rv.IsNil() || rv.Len() == 0 {
		return "", nil // nil/empty map write empty string
	}
	raw, err := sonic.Marshal(fieldValue)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func init() { schema.RegisterSerializer("tolerantjson", tolerantJSON{}) }
```

Example field declaration:

```go
Metadata map[string]string `gorm:"column:metadata;type:text;serializer:tolerantjson" json:"metadata,omitempty"`
```

### 4.5 Semantics Comparison Table

| Column content | Scan result | Consistency with current `decodeRuleMetadata` |
|---|---|---|
| NULL / `""` | nil | consistent |
| `null` | nil | consistent |
| `{}` / empty object | nil | consistent |
| corrupted JSON | nil (no error) | consistent |
| valid object | decoded map | consistent |
| write when the field is nil/empty map | `""` (never writes the `"null"` literal to pollute the column) | consistent with `encodeRuleMetadata` |

### 4.6 Registration Ordering and Upfront Verification

- Model packages blank-import `pkg/db` → its `init()` runs before the schema parsing of any `gorm.Open`/`AutoMigrate`/first query (already attached at the top of `pkg/prism/alert/model.go`).
- Upfront verification (done during implementation): the `SerializerInterface.Scan`/`Value` signatures and the registration ordering in gorm v1.31.2's `schema/serializer.go`; `Value` goes through `SerializerValuerInterface` (it receives the already-extracted fieldValue, not a reflect.Value), and the implementation was written accordingly.
- The native map update path of `Updates(map[string]any{...})` **does not go through the serializer** — verified that existing such paths (`alert/store.go`) never touch metadata/config columns; re-verified with grep during implementation.

### 4.7 Consumer Migration Checklist

| Current consumer | Disposition |
|---|---|
| `alert.Rule.MetadataMap()` (model.go:60) | Delete. **Verified to have no production callers** (definition site only), so the migration surface is zero |
| `pkg/prism/alert/register.go:88` `marshalSpecMetadata(spec.Metadata)` | change to `Metadata: spec.Metadata` directly and delete the marshal helper |
| `internal/api/service/prism/alert.go:202,220` decode/encode | deleted along with the converters |
| `internal/api/service/telemetry/telemetry.go:320-325,344,359,364-373` | deleted along with `pointToTask/taskToPoint/mergeTaskOntoPoint/configToString` |
| `pkg/prism/alert/env.go:93` `res.Metadata` | **unrelated** (asset-side field), untouched |

---

## 5. Tier-by-Tier Optimization Plan

### 5.1 Tier 1 — Pure Merge (zero wire changes)

| Entity | Action |
|---|---|
| remediation Rule/Record | delete the two handler `types.go` structs; the Service interface and implementations switch to the `pkg/prism/remediation` models. Model tag adjustments: `TenantID`, `Metadata`, `DeletedAt` → `json:"-"` (none of the three is on the wire today, so the tag adjustment is a zero change); `Record.UpdatedAt` → `json:"-"`. Tamper protection for runtime-state fields remains the existing column whitelist's job (store.go:162) |
| channel | delete the handler `Channel`; add json tags to `pkg/prism/channel.Record` (8 fields, with `config` kept a string); `DeletedAt` → `json:"-"`. The reverse conversion today copies only 4 writable fields (channel.go:167-174); after the merge the store's update whitelist takes over |
| system Config | delete the handler `Config`; add json tags to `systemConfig` (3 fields) |
| auth tokenData | delete `handler.go:40-46`; endpoints return `TokenPair` directly (types.go:18) |

Net effect: CE deletes ~200 lines of conversion code, and the x repo's corresponding copies simultaneously lose their reason to exist.

### 5.2 Tier 2 — Merge After Resolving Differences

**alert.Rule**:
- Add json tags to the model (10 fields, verbatim-aligned with the current handler Rule tags); `TenantID/DeletedAt` → `json:"-"`; `Metadata` becomes map + `serializer:tolerantjson` (4.4).
- Delete `ruleModelToHandler/ruleHandlerToModel/decodeRuleMetadata/encodeRuleMetadata`; the Service returns models directly.
- Update paths no longer manually restore `ID/CreatedAt` (the column whitelist already guarantees it); `ruleUpdateColumns` unchanged.

**alert.Record**:
- Use the DB model directly. Wire changes in 6.1 (`fired_at`→`triggered_at`; `created_at` newly exposed).
- The empty-severity `"warning"` default logic (alert.go:256-260) moves into the model's `AfterFind`: when `Severity == ""` set `"warning"`, aligning with the column default:'warning' semantics so legacy empty rows self-heal.

**telemetry MonitorPoint**:
- Delete the handler `Task` (the `Telemetry` ingestion body and the handler-local view structs stay — they are genuine DTOs).
- Model tag adjustments: `TenantID/Status/Interval/Timeout` → `json:"-"` (none is on the task-CRUD wire today, keeping the change zero; exposing `interval/timeout` later would be an incremental API decision reviewed separately); `Config` becomes map + serializer.
- Delete `pointToTask/taskToPoint/mergeTaskOntoPoint/configToString`. The manual preservation of `Status/Interval/Timeout` in `mergeTaskOntoPoint` is taken over by the telemetry store's update column whitelist (if that store has no whitelist at implementation time, add it first, then merge).

### 5.3 Tier 4 — Guardrail Backfill (fixing the asset counter-example)

- `asset.Asset`: `TenantID` → `json:"-"` (removes both the wire exposure and the binding surface); `Metadata` stays string + existing tags (changing it to map is a wire change beyond this batch's scope, listed as a later option).
- The `json:"-"` on `user.User/APIKey` sensitive fields is already correct; untouched.

### 5.4 Tier 3 — the task Domain (full design; implemented, as-built record in 5.4.8)

> This section was upgraded from "direction proposal" to full design on 2026-08-24. **The initial verdict in 2.4 Tier C ("the domain-layer semantics are real, the layering should be kept") was overturned after evidence-based re-review**, reasons in 5.4.1. Three key decisions (D1 vocabulary unification / D2 timeout promotion / D3 retry-field revival) were finalized per the recommendation because the review queries went unanswered; together with the D4 wire-key rename they are flagged in 5.4.3 — **confirm these first during review**.

#### 5.4.1 Current-State Re-review: Three-Layer Model, Two Conversion Legs, Four Verified Pain Points

task is the repo's only genuine three-layer model, with two legs of handwritten conversion:

```
handler wire   pkg/api/handler/task/types.go   Task(:13-32) / Execution(:44-61)
    ↕  converter.go 274 lines: DomainTaskToHandler(:70) / HandlerToDomainTask(:104) / DomainExecutionToHandler(:171) + 5 helpers
domain struct  pkg/task/model.go               Task(:126-164) / Execution(:169-210) — pure data, no gorm tags, no behavior methods
    ↕  ToTask(model.go:60) / taskToModel(store.go:177) / ToExecution(:132) / ExecutionToModel(:158)
GORM model     pkg/task/model.go               ScheduleTask(sys_schedule_task,:21-54) / ScheduleLog(sys_schedule_log,:100-129)
```

Re-review evidence that overturned the initial verdict:

| "Domain semantics" per the initial verdict | Re-review conclusion |
|---|---|
| `Timeout time.Duration` rich semantics | Only a type difference: the DB column is already seconds (model.go:34); a one-line method solves it (`time.Duration(t.TimeoutSeconds) * time.Second`) |
| State-machine translation | Only a vocabulary mapping: `ToLifecycleStatus` (types.go:95-106) is a 12-line pure function, and `unknown`→`failed` is a lossy collapse |
| The domain layer carries scheduling semantics | What the engine consumes are keys of the metadata string bag: `extractScheduleConfig` reads `schedule_type/cron_expr/interval` (schedule.go:24-43), Pause/Resume writes `metadata["enabled"]` (manager.go:333,385) — the domain struct's rich fields are not on the engine's main parsing path at all |

Verified pain points (all disappear after the merge, evidence pinned):

1. **Dead columns reset every time**: the `max_retries/retry_interval` columns are on the upsert whitelist (store.go:161-162) but `taskToModel` never assigns them — **every Save writes both columns back to 0**; the runner's retry reads (`executor/lifecycle.go:296-317` reading metadata keys `max_retries/retry_interval`) never take effect because no writer exists, so the wire's `retry_policy` field spins idle.
2. **Wire timestamps are metadata strings**: Task's `created_at/updated_at` are written into metadata strings by the converter at creation (converter.go:147-152) and read back verbatim (:231-240); `ToTask` never reads back the DB's real timestamp columns (model.go:60-83), so the two time sources can drift.
3. **Lossy collapse and swallowed errors**: `unknown`→`failed` (ToLifecycleStatus); Config sonic conversions in both directions silently swallow failures (converter.go:85-90,136); `enabled` parse failure silently sets false (:226-230).
4. **Reserved-key smuggling in config**: the five non-executor config keys `tenant_id/asset_id/timeout/priority/depends_on` hide inside the wire config map; the extract/strip machinery (converter.go:58-64,119-140,194-217) is the bulk of the converter's complexity, and "timeout is a reserved key" is an implicit contract.

#### 5.4.2 Goal: Converge into a Single Dual-Tag Model

`pkg/task.Task` doubles as the GORM model (table name still `sys_schedule_task`, absorbing all ScheduleTask columns); `pkg/task.Execution` absorbs ScheduleLog (table name still `sys_schedule_log`, with the `Error` field keeping the column name via `gorm:"column:error_msg"` and the wire key still `error`). `pkg/api/handler/task` keeps only: the Service interface (signatures switch to `*task.Task/*task.Execution`, staying importable by x), route handlers binding the model directly (gin binding ignores `json:"-"`, preventing mass-assignment), `Filter/ExecutionFilter/ExecutionStats` (query parameters and cross-row aggregates, not model copies), and `copyTaskRequest`.

**Task field table**:

| Field | Column | wire(json) | Versus current state |
|---|---|---|---|
| ID/Group/Tags/RunID/RetryPolicy/Concurrency | existing | unchanged | tags stay a comma-separated string + Go-side splitting (the store already does this today) |
| Name | existing (varchar255) | `name` | promoted from a metadata key to a field |
| Description | **new column** (AutoMigrate) | `description` | new column; **no backfill** (both repos unreleased by decision; dev databases rebuilt) |
| ExecutorType | existing (executor_type) | `executor_type` | field renamed (formerly ExecutorName); wire key `executor`→`executor_type` (D4) |
| Schedule | **new column** (varchar) | `schedule` | the single source of truth; the three columns `schedule_type/cron_expr/interval` are **deleted**, with parsing consolidated into `ParseSchedule` (D5) |
| Enabled | existing | `enabled` | promoted from a metadata key; Pause/Resume now write the column |
| TimeoutSeconds | existing (timeout, int64 seconds, default 30) | `timeout` | exposed at top level; method `Timeout() time.Duration`; the reserved-config-key mechanism is deleted (D2) |
| MaxRetries / RetryInterval | existing (currently dead columns) | `max_retries/retry_interval` (seconds, optional) | revived (D3); the runner reads explicit fields instead |
| Config | executor_config (text) | `config` (map, tagged tolerantjson) | pure executor config, no more reserved-key smuggling |
| CreatedAt / UpdatedAt | existing | `created_at/updated_at` | read straight from the real columns (fixes pain point 2) |
| TenantID/AssetID/Priority/DependsOn | existing | `json:"-"` | internal columns (D6) |
| Metadata | existing (text) | `json:"-"`, map + tolerantjson | repurposed as the extension-key bag (D6) |

**Execution field table**:

| Field | Column | wire(json) | Versus current state |
|---|---|---|---|
| ID/TaskID/StatusCode/Output/Duration(ms)/RetryCount/StartedAt | existing | unchanged | |
| TaskName | none (`gorm:"-"`) | `task_name,omitempty` | assigned by a service-layer join (as today) |
| ExecutorType | executor_type | `executor_type` | rename alignment (formerly ExecutorName) |
| Status | status | `success/failed/running/unknown` | **vocabulary unified to the API side** (D1); delete ToLifecycleStatus/ToStoredStatus |
| Error | error_msg (column name kept) | `error` | delete the ErrorMsg↔Error rename conversion |
| FinishedAt | finished_at | `finished_at,omitempty`, `*time.Time` | nullable pointer (D7); the zero value no longer falsely reports year zero |
| TenantID/AssetID/Operation/RunID/TriggerType/SkipReason/Metrics | existing | `json:"-"` | not exposed for now; incremental exposure reviewed separately |

#### 5.4.3 Design Decisions (D1–D3 were finalized per the recommendation because the queries went unanswered — review focus)

**D1 Unify the status vocabulary on the API side (the storage column stores `success/failed/running` directly)**
- Current state: storage `normal/abnormal/triggered/unknown` (types.go:64-75, whose comment admits the values are deliberately equal to `pkg/types.AssetStatus` and "must not be mixed" — relying on equal values is itself coupling); API side `success/failed/running`; both CE and x webs already speak the API vocabulary (the CE mock header comment says "status limited to success/failed/running"), so **zero web changes**.
- Decision: the storage column switches to the API vocabulary; `unknown` remains a legal stored value emitted verbatim instead of collapsing to `failed` (a slight behavior change); delete ToLifecycleStatus/ToStoredStatus and the handler query-parameter translation (handler.go:242).
- Go change surface (mechanical replacement): CE Status constants, dependency gating (events.go:42-59 switches to comparing StatusSuccess), Stats SQL literals (`'normal'/'abnormal'` in store.go:379-412); the x repo's health queries (health/store.go:51,87) and remediation persistence reads (the task_status_processor external reporting has since been funneled into the kernel `task.ApplyTaskReport` call, so there is no longer an independent normal/abnormal write surface).
- Alternative: keep the dual vocabularies; ToLifecycleStatus survives as the only remaining mapping (~12 lines), minimizing x changes, but the vocabulary split becomes permanent and "removing conversions" is discounted.

**D2 Timeout promoted to a top-level field**
- Top-level wire `timeout` (int seconds); delete the entire reserved-config-key mechanism (converter.go:58-64,119-140,194-217 plus the parseInt64FromAny/parseIntFromAny/parseDurationFromAny helpers).
- The per-executor timeout inputs in the CE web TaskForm (http 10/tcp 5/icmp 3/local 60 seconds) were already extracted via the reserved key into a task-level timeout and stripped from the executor config — the form collapses into one task-level input (the i18n key `task.create.timeout` already exists), and `buildExecutorConfig` no longer writes timeout.

**D3 max_retries/retry_interval revived as real fields**
- Wire exposes `max_retries` (int) / `retry_interval` (int seconds), optional with omitempty; the runner's buildRetry (executor/lifecycle.go:296-317) reads explicit event-payload fields instead (see 5.4.4), deleting the metadata-key reads; `retry_policy` gains real meaning thereby, and the fields the x web creation form expects fall into place.
- Alternative: delete the columns and the reads wholesale (consistent with the 2026-08-24 compatibility-code cleanup batch), removing retry entirely — in that case the wire's `retry_policy` field is deleted too.

**D4 Wire key `executor` → `executor_type`**
- Unifies with the Execution side and the DB column name; ~10 mechanical renames in CE web (types/task.d.ts, TaskForm, create/edit payloads, List/Detail, mock, httpapi task_test); the x web task api never matched the real backend anyway (the mock-driven extended form POSTs a camelCase extended shape that the backend would reject), so aligning its form is a separate project.

**D5 Schedule as the single-column source of truth**
- The `Schedule string` column is the single source of truth; engine parsing is consolidated into `pkg/task.ParseSchedule(schedule string) (ScheduleType, cronExpr string, interval time.Duration, err error)` — formed by merging the classification logic of today's scheduleToMetadata (scheduler/task.go:400-414: empty→event / Go duration→interval / everything else→cron) with parseSchedule (schedule.go:48-78); service create/update and the engine's Register/Restore (persistence.go:28-115) all call it.
- The three columns `schedule_type/cron_expr/interval` are deleted; telemetry `pointToProbeTask` (prober_service.go:191-228) sets the Schedule string and Metadata extension keys (monitor_point_id/expression) directly.

**D6 Metadata repurposed as the extension-key bag**: `map[string]string` + tolerantjson (reusing the Chapter 4 serializer), `json:"-"`. The remaining legal key list goes into a model comment: `monitor_point_id/expression` (telemetry probing). `tenant_id/asset_id/priority/depends_on` stay off the wire — under the current single-tenant setup tenant_id is always 0 and the other three have no frontend consumers; exposing them is an incremental API decision reviewed separately.

**D7 FinishedAt as a nullable pointer**: the column is nullable and the wire omitempty matches today; write sides (the runner, TriggerTask placeholder rows, the x processor) switch to taking addresses.

**Quirks kept (recorded, unchanged; separate review if ever touched)**: task IDs are allocated by a service-layer atomic counter (scheduler/task.go:378-395, because engine registration needs the ID first), not taken from DB auto-increment; engine persistence setTask/deleteTask failures are log-and-continue (persistence.go:130-160).

#### 5.4.4 CE Change Points (implementation checklist)

| Layer | Action |
|---|---|
| Conversion deletion | all of converter.go (274 lines) + converter_test.go; ToTask/taskToModel/ToExecution/ExecutionToModel in model.go; scheduleToMetadata in scheduler/task.go; the ScheduleTask/ScheduleLog structs fold into Task/Execution |
| Engine | Register/Update/Restore read fields (all through ParseSchedule); Pause/Resume write and persist the Enabled column (replacing metadata["enabled"]); the trigger event payload (pkg/event) carries TimeoutSeconds/MaxRetries/RetryInterval explicitly (replacing today's bare nanosecond int, events.go:127), with the Metadata bag carrying only extension keys; dependency-gating vocabulary replacement (events.go:42-59) |
| runner | buildRetry reads the payload's explicit fields; ApplyJudgment keeps reading expression from the metadata bag (lifecycle.go:128) |
| store | the model is the storage, conversions drop to zero; the Save whitelist adds the description/schedule/timeout/max_retries/retry_interval/enabled/name columns (still guarding created_at/deleted_at); Stats SQL vocabulary replacement; ExecutionRecordStore's ns→ms conversion (store.go:473) is kept |
| handler/service | the Service interface signatures switch to models; create/update bind the Task model directly (the required-name validation stays handwritten), guards kept (zeroing/restoring ID/CreatedAt, scheduler/task.go:135-139,172-175); UpdateTask no longer round-trips through the handler to preserve CreatedAt (:174); ListExecutions' task_name filter becomes SQL (name is a column now, replacing the in-memory names map :266); the execution query-parameter vocabulary translation is deleted |
| telemetry | pointToProbeTask sets fields instead (Schedule/Enabled/TimeoutSeconds/Metadata extension keys) |
| web | executor→executor_type rename; TaskForm timeout collapses to one task-level input; mock synced; zero status-vocabulary changes |
| OpenAPI | the task section is rewritten: add pause/resume paths; fix the executions sub-resource paths (`/tasks/{id}/executions`); list filters (group/tags/task_name/executor/status); TaskCreateRequest gains group/tags/retry_policy/concurrency/timeout/max_retries/retry_interval; the config example switches to numeric seconds and drops reserved keys; the status enum drops the `pending` ghost value; the schedule description drops the unsupported `@every` |

#### 5.4.5 Same-Batch Changes in tickraft-x

| Location | Action |
|---|---|
| internal/api/service_task.go | rewritten to use models directly (same pattern as service_alert_prism): delete scheduleToMetadataLocal(:406-420), lifecycleToStoredStatus, and the fetch-all-then-filter-in-memory paths (:217-239,258-281,329-350) — ExecutionStore.Query natively supports status SQL filtering; task_name filtering becomes SQL; Pause/Resume switch to the field (:185-215) |
| worker | bridge.go(:286-309) reads the Enabled/Schedule fields; task_status_processor's external branch persists via the kernel `task.ApplyTaskReport`, moving with the kernel model fields |
| Type names | all ScheduleTask/ScheduleLog references replaced with Task/Execution (worker start.go's AutoMigrate, health, remediation, quota counting, syncer) |
| syncer_decorator | signatures follow the Service interface's model switch |
| x web | the task api never matched the real backend (mock-driven development); form alignment is recorded as a later x-side item and does not block this batch |

#### 5.4.6 Test Plan

- **Delete**: all of converter_test.go.
- **Modify**: httpapi task_test's `executor` key→`executor_type` plus a new task-level timeout assertion on the create payload; scheduler/task_test's ScheduleToMetadata table tests become ParseSchedule table tests; model_test's ToTask metadata-parse assertions become direct model reads.
- **Add**: ParseSchedule table tests (empty / Go duration / cron / `@daily`); vocabulary-unification regression (unknown emitted verbatim); Save-whitelist regression (max_retries no longer reset to 0); the "PUT does not modify runtime-state fields" guardrail assertion (extending Chapter 8).
- **Unchanged**: the pagination envelope, the remaining tests/httpapi snake_case assertions, and the store-layer D-01/D-02 style regressions.

#### 5.4.7 Implementation Order and Acceptance Gates

CE goes first (model → engine → store → service → handler → web → OpenAPI) → x in the same batch (5.4.5). Acceptance: CE `go test ./...` 100% green + golangci-lint zero warnings + red-line scan; x builds and tests fully green across its three build-tag sets (see that repo's Makefile) + lint zero warnings; grep assertions: no entity conversion functions named `*ToHandler/*ToModel/*ToTask` remain under `pkg/api/handler/task` or `pkg/task`, and zero remnants of `ToLifecycleStatus/ToStoredStatus/MetadataKeyEnabled`.

#### 5.4.8 Implementation Record (as-built, delivered 2026-08-24)

The design has been implemented as decided above, and both repos passed all acceptance gates (CE full test suite + lint + red-line; x build/test across the three build-tag sets + lint zero warnings). Deviations from the design draft and landing details:

- **Location of the vocabulary bridge**: the only bridge from the asset vocabulary to the storage vocabulary is `task.ExecutionStatusFromAsset` (pkg/task/model.go:66; normal→success, abnormal→failed, everything else→unknown). The executor Result and completion events still use the asset vocabulary (AssetStatus) and are never persisted; `sys_schedule_log.status` stores only the API vocabulary. The x repo's remediation skip records keep the x-specific `skipped` marker (in neither vocabulary; list queries filter it out via `status IN (success, failed)`, with the semantics carried by the skip_reason column); the x grpc external channel's `trigger_type=external` is likewise an x-specific value and is kept.
- **Landed shape of schedule parsing**: the design draft's "ParseSchedule consolidation" landed as `task.ClassifySchedule` (exported, for validation/classification, pkg/task/schedule.go:20) plus the package-private `parseSchedule` (for engine population). The semantics match the design: `""` is event-driven, a Go duration is a fixed interval, everything else parses as cron; the `once` type was deleted (the x repo's once unit tests went with it).
- **Event payload field renames** (pkg/event.ExecutionPayload): `Timeout` (bare nanosecond int) → `TimeoutSeconds`, added `MaxRetries`/`RetryIntervalSeconds`, `Config` changed from map to a sonic-serialized string, and `Action` carries `Operation.String()` (the x repo's previously hardcoded "triggered" aligned as well). The runner's retry has had a real data source since then; the retry chain is revived.
- **Enabled-gating fix**: the x repo bridge's `isTaskEnabled` changed from "missing metadata counts as enabled" to reading the `Enabled` column directly — the implicit default-on-when-metadata-is-absent semantics disappeared along with the field promotion, and tasks not explicitly enabled are no longer scheduled.
- **GORM default-tag zero-value substitution bug**: caught during implementation — on zero-value inserts, GORM substitutes the column default for columns carrying a `default` tag, flipping the semantics (`Enabled=false` stored as `true`, and `Concurrency=0` (unlimited) stored as `1`). Fix: drop the default tag from those two columns and have the service layer assign all defaults explicitly (timeout keeps its column default of 30 because the service-layer create already assigns 30 explicitly, making the zero-value path unreachable). The x repo's store unit tests cover this in sync.
- **x distributed-mode differences**: x's `service_task.go` has no engine (Worker nodes run the scheduling); Pause/Resume write the Enabled column directly and push via the Syncer; Create/Update preserve the `json:"-"` internal fields (TenantID/AssetID/Priority/DependsOn/Metadata/Operation) and keep the current value when timeout<=0, aligning with the CE engine version's behavior; list filtering goes through `ExecutionStore.Query` native SQL (status/executor_type/task_name), and the in-memory filter functions are all deleted.
- **CE web**: the TaskForm timeout collapsed into three task-level inputs, timeout/max_retries/retry_interval (seconds); per-executor configs no longer carry a timeout key; list/detail/mock (14 seed rows) fully migrated to executor_type.
- **OpenAPI** (docs/api/openapi.yaml): executions fixed to the real sub-resource path `/tasks/{id}/executions(/{execId})`; pause/resume paths and 409 added; list gains group/tags/status/executor_type/task_name filter parameters; the copy response code 201→200 (the actual envelope is always 200); the status enum drops `pending`; the schedule description drops `@every`/`@daily` in favor of the cron / Go duration / "" three-way form; the config example drops the timeout reserved key.

---

## 6. Wire Change List

### 6.1 Breaking Changes (approved, this one only)

`alert.Record`: `fired_at` → `triggered_at`.

- Blast radius: mechanical `firedAt` → `triggeredAt` renames across 5 CE web files (`web/packages/features/src/api/prism.ts`, `mock/prism.ts`, `views/prism/record/list/List.vue`, `views/prism/record/detail/Detail.vue`, `views/dashboard/overview/index/Index.vue`).
- Implementation erratum: the design-time survey claimed "tickraft-x web has no references", but verification during implementation found **3 in x web** (`web/src/api/prism/record.ts`, `web/src/mock/prism-record.ts`, `web/src/views/prism/record/list/List.vue`, plus a dangling `common.firedAt` i18n reference), all renamed in the same batch to `triggeredAt`/`common.alertTriggeredAt`; the CE backend tests (tests/httpapi) have no assertion on that key.

### 6.2 Incremental Exposure (backward compatible)

The `alert.Record` response gains `created_at` (the model already carries the `json:"created_at"` tag).

### 6.3 Unchanged Items

The response field sets and names for remediation, channel, system, auth, and telemetry task CRUD are all unchanged; the `metadata` field's wire shape stays "decoded object" as in the handler era (the serializer does it inside the model now, invisible to clients); the pagination envelope (`items/total/page/size`) is untouched.

### 6.4 Tier 3 (task Domain) Changes (implemented; as-built in 5.4.8)

- **Breaking**: the Task wire key `executor` → `executor_type` (unified with the Execution side and the column name, 5.4.3-D4); the `config.timeout` reserved-key semantics are deleted (from now on a timeout inside config is ordinary executor config; the task timeout uses the top-level `timeout` field, D2); the execution status `unknown` no longer collapses to `failed` for display (emitted verbatim, D1).
- **Incremental exposure (optional fields)**: Task gains top-level `timeout` (seconds), `max_retries`, and `retry_interval` (seconds).
- **Storage vocabulary replacement** (transparent to API callers): `sys_schedule_log.status` now stores `success/failed/running/unknown` (formerly `normal/abnormal/triggered`); both repos are unreleased, so no data migration — dev databases are rebuilt.
- **Unchanged**: the rest of Task's fields (`name/description/schedule/enabled/config shape/group/tags/run_id/retry_policy/concurrency/created_at/updated_at`), the rest of Execution's fields, and the pagination envelope all stay.

---

## 7. Two-Repo Implementation Order

### 7.1 CE First (dependency-ordered within one batch)

1. `pkg/db/jsonmap.go` (4.4, sonic implementation) + gorm interface verification (4.6).
2. Model side: tag and serializer changes for alert Rule/Record, MonitorPoint, channel Record, remediation Rule/Record, and asset (pure model layer; engine and store follow via compilation).
3. Service layer: each `Service` interface signature switches from handler DTOs to model types; delete the converters and the `taskconv` delegation shells.
4. Handler layer: delete the duplicate structs in `pkg/api/handler/{alert,channel,remediation,system}` and auth `tokenData`; rewrite the "Handler types" section of `doc.go` for the new convention.
5. Direct metadata assignment in `register.go`; the `AfterFind` severity default.
6. OpenAPI: rewrite the `AlertRule/AlertRecord` schemas to the current models (fixing the camelCase/snake_case mixing along the way) and add the missing fields to the `Task` schema.
7. web: `firedAt` → `triggeredAt` rename + contract tests.

### 7.2 tickraft-x Follows in the Same Batch

1. Delete all of `prismRuleModelToHandler/prismRuleHandlerToModel/prismRecordModelToHandler` in `internal/api/service_alert_prism.go`; the Service implementation returns CE models directly.
2. The local conversions `executionToHandlerLocal` (`service_task.go:427`) and those in `service_telemetry.go` follow the CE interface signature switch; `syncer_decorator.go` adapts. Implementation revision: per the Chapter 3 convention, the execution conversion was hoisted into `pkg/api/handler/task.DomainExecutionToHandler` (including status normalization and full-field mapping, fixing the x local copy's drift where ExecutorType/StatusCode/Duration/RetryCount were missing); both CE's `internal/api/service/scheduler` and x's `service_task.go` call it now, with tests moved along.
3. Embedded-model check: `governance.Record` embeds `alert.Record` (`internal/prism/governance/model.go:241`) and inherits the `triggered_at` rename automatically; compile-time verification found no `fired_at` remnants in x-side backend consumers, and the 3 x web references are covered by the 6.1 implementation erratum.
4. The x-specific extended build tags (see that repo's Makefile) need a separate build-and-test run after this batch lands.

### 7.3 Acceptance Gates

- CE: `go test ./...` 100% green (including the full tests/httpapi suite); golangci-lint zero warnings (Tier 1 baseline).
- x: regular build green + extended-tag build green + the full x test suite.
- grep assertion: no entity conversion functions named `*ToHandler/*ToModel` remain under `internal/api/service` (after the taskconv deletion).

---

## 8. Test Plan and Acceptance

| Category | Action |
|---|---|
| Add | unit tests for `pkg/db/jsonmap.go`: every case in the 4.5 semantics table (NULL/""/null/{}/bad JSON/valid/write empty string both ways) |
| Keep | the store-layer D-01/D-02 regressions (`pkg/prism/alert/store_test.go:91-100`, `pkg/prism/remediation/store_test.go:107-144`) — they test column-whitelist behavior, orthogonal to the model merge, and must keep passing |
| Delete | converter unit tests such as `internal/api/service/prism/alert_test.go` (including the full-field PUT regression comment at :465 — that protection semantics is now carried by the column whitelist) |
| Unchanged | the snake_case key assertions in tests/httpapi (`prism_test.go:53,126,145`); the seeding-path `prismalert.Record` literals (prism_test.go:85-93) get compile-time fixes as field types change |
| Update | web contract tests and the 5 `firedAt`-renamed files |
| Manual | walk the full rule create → trigger → record query flow once (verifying the serializer persists valid JSON with no `"null"` literal in the column) |

---

## 9. Risks and Non-Goals

**Risks and Mitigations**

| Risk | Mitigation |
|---|---|
| serializer registered after schema parsing (init ordering) | the 4.6 upfront verification + the first implementation action |
| historical columns containing the `"null"` literal | Scan tolerantly normalizes to nil; covered by the 4.5 table |
| clients submitting runtime-state fields (status etc.) after the merge | `json:"-"` + the store column whitelist as dual insurance (conventions 2/3); httpapi gains a "PUT does not change status" regression assertion |
| unexpected wire changes from the x repo's embedded models | the 7.2.3 check; x web verified to have no `fired_at` references |
| the merge introduces a regression if the telemetry store has no update whitelist | 5.2 precondition: add the whitelist first, then merge |

**Non-Goals**

- No change to the pagination contract (page/size, PageData envelope).
- No change to channel.Config's wire shape (stays a JSON string).
- No change to asset.Metadata's wire shape (string → map belongs to a later optional batch).
- The task-domain schema redesign has a complete design (5.4, 2026-08-24); the code lands in a separate batch after review confirmation and is not mixed with the delivered Tier 1/2/4 batches.
- No code generation of any kind (oapi-codegen etc.); OpenAPI stays hand-maintained.
