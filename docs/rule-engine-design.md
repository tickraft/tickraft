# Rule Engine Design

> **Path note (2026-08-26, updated 2026-08-27)**: The `internal/api/service/...` paths in this document are design-time path snapshots; these implementations now live in `pkg/prism/{alert,channel,remediation}`, `pkg/telemetry`, `pkg/task`, and `pkg/system` (after the service layer moved down into domain packages, the service subpackages were flattened into each domain's root on 2026-08-27, with the SPI in each domain's `ports.go` and the implementation in `service.go`). Historical finding numbers and line numbers are preserved as-is.

> Status: **design review draft**. This document is the authoritative design for the rule mechanism revamp; implementation starts only after full confirmation.
> Scope: `pkg/expr` (new), `pkg/prism/alert` (migrated from `pkg/prism/rule` and merged into a single package), `pkg/prism/remediation`, `pkg/executor`, the related APIs and frontend, plus **the synchronized tickraft-x updates** (Chapter 12).
> Premise: neither tickraft nor tickraft-x has been released, so **neither repo takes on any compatibility obligations**; we change straight to the optimal architecture.

---

## Table of Contents

1. [Background and Problems](#1-background-and-problems)
2. [Target Architecture](#2-target-architecture)
3. [Expression Language Specification](#3-expression-language-specification)
4. [Evaluation Environment (ENV) Design](#4-evaluation-environment-env-design)
5. [Function Strategy](#5-function-strategy)
6. [Design of the Three Consumption Faces](#6-design-of-the-three-consumption-faces)
7. [Persistence Design](#7-persistence-design)
8. [API and Validation Changes](#8-api-and-validation-changes)
9. [Frontend Changes](#9-frontend-rule-editing-design-dual-mode)
10. [Defect Fix Mapping Table](#10-defect-fix-mapping-table)
11. [Test Plan and Implementation Order](#11-test-plan-and-implementation-order)
12. [tickraft-x Synchronization Design](#12-tickraft-x-synchronization-design)
13. [Requirements Coverage Assessment](#13-requirements-coverage-assessment-tickraft--tickraft-x)

---

## 1. Background and Problems

### 1.1 Current State: Three "Rule" Mechanisms Each Going Its Own Way

| Mechanism | Location | Evaluation engine | Persistence |
|---|---|---|---|
| Alert rules | `pkg/prism/rule` | expr-lang scene-aware compiler (own sandbox, custom functions, comparison-count cap) | `sys_prism_rule` (scene + expression) |
| Remediation rules | `pkg/prism/remediation` | package-private `compileCondition`: a **copy-pasted** parallel sandbox (the whitelist comment self-describes as "mirrors the rule engine's builtinWhitelist") | `sys_prism_remediation_rule` (trigger_event_type + condition_expr) |
| Executor status determination | `pkg/executor/*` | **no expressions**, hardcoded per executor | no table (http has an `expect_status` config, webhook does not) |

The two expr-lang sandboxes have already drifted semantically: the alert side has 4 custom functions and a comparison-count cap of 3, while the remediation side has neither; the remediation evaluation environment (the flat `EventContext`) has no asset enrichment, while the alert side does. On the executor side, webhook hardcodes 2xx while http supports `expect_status`; the local executor's **exit code value is simply discarded** (`Result` has no corresponding field).

### 1.2 Verified Defect List

Severity levels: P0 = functional error (rules silently fail), P1 = high-risk latent issue, P2 = concurrency/resource risk, P3 = minor issue.

| ID | Level | Defect | Location |
|---|---|---|---|
| D-01 | P0 | PUT remediation rule wipes `status/metadata/last_run_at`: the DTO converter omits runtime-state fields, and `Store.Update` uses `Save`, overwriting all columns → once status is cleared, the `Status != "active"` check in `handle()` **silently skips this rule forever**, while the circuit-breaker count zeroes out and the cooldown resets | `internal/api/service/prism/remediation.go` / `pkg/prism/remediation/store.go` |
| D-02 | P1 | PUT alert rule wipes `tenant_id/group_id/metadata` (same Save overwrites-all-columns pattern) | `internal/api/service/prism/alert.go` / `pkg/prism/rule/store.go` |
| D-03 | P1 | `prism.eval_interval` dead config: config validation forces non-zero, but the assembly layer never passes it into `rule.Config.EvalInterval`, so the polling reload loop never starts → rule changes made directly in the DB, bypassing the API, never take effect | `internal/service/prism.go` / `pkg/prism/rule/register.go` |
| D-04 | P1 | `scene=remediation` dead path: the compiler treats it as a valid scene, the API can create it, and the engine loads it, but `MatchRemediation` has no production caller — **silently ineffective once the user creates it** | `pkg/prism/rule` (the whole SceneRemediation chain) |
| D-05 | P1 | task/probe scenes idle: `TaskMatcher`/`ProbeMatcher` have no production wiring in either the CE or the tickraft-x repo | `pkg/prism/rule/matcher.go` |
| D-06 | P1 | remediation `condition_expr` has no entry-point validation: a bad expression fails to compile at runtime → warn + return false, so the rule silently never fires | `internal/api/service/prism/remediation.go` |
| D-07 | P2 | `matchCache` grows unboundedly: keyed by `ruleID:expr`, it only grows and never shrinks; after an expression change or rule deletion the old compiled artifacts are never reclaimed | `pkg/prism/remediation/manager.go` |
| D-08 | P2 | circuit-breaker counter read-modify-write race: the whole metadata JSON is modified from a snapshot and written back, so concurrent executions of the same rule lose updates | `pkg/prism/remediation/manager.go` |
| D-09 | P2 | idempotency-gating TOCTOU: there is a window between the `checkGates` check and the `dispatch` set; safety relies solely on the channelBus single-consumer implementation detail | `pkg/prism/remediation/manager.go` |
| D-10 | P2 | payload conversion ignores all `strconv.ParseInt` errors: an illegal asset_id lands on 0 and then matches every global rule | `pkg/prism/remediation/manager.go` |
| D-11 | P2 | multi-tenant gap: engine `Reload` loads across tenants with a fixed tenant 0, and evaluation does not filter by the event's tenant | `pkg/prism/rule/engine.go` |
| D-12 | P2 | store validator and engine compiler config can diverge: `migrateStores` uses the default compiler, ignoring `cfg.RuleConfig.CompilerConfig` | `pkg/prism/config.go` |
| D-13 | P3 | custom `regex()` recompiles the regex on every call (this design removes the function entirely) | `pkg/prism/rule/functions.go` |
| D-14 | P3 | Violation extraction loses `severity/source`: Dispatch replaces `evt.Violations` wholesale with the extraction result, leaving sorting/rendering without the information | `pkg/prism/rule/violations.go` |
| D-15 | P3 | remediation `ExecutionRequest` never sets Timeout and always falls through to the executor default | `pkg/prism/remediation/manager.go` |
| D-16 | P3 | webhook executor has no `expect_status`, inconsistent with http semantics | `pkg/executor/webhook/webhook.go` |
| D-17 | P3 | leftover Chinese debug comments | `pkg/prism/remediation/manager.go:116` |

### 1.3 Structural Problems

- **A-1 Parallel implementations**: the same sandbox policy maintained in two places, already drifted (see 1.1).
- **A-2 Idle scene mechanism**: `sys_prism_rule` is nominally a "four-scene universal rule table", but in practice only the metric (alert) scene is wired. The API path is `/prism/alert/rules`, the service layer is `AlertService`, and the frontend page is "alert rules" — **name and reality do not match**.
- **A-3 Non-configurable execution determination**: real-world cases — a health check returning 200 while the body indicates a degraded state, or a script exiting 0 while its output contains errors — cannot be expressed.

---

## 2. Target Architecture

### 2.1 Layering: One Kernel, Three Domain Faces

```
pkg/expr                        ← new package: domain-agnostic expression evaluation kernel
  compiler.go   sandboxed compilation (MaxNodes/AsBool/Env type checking)
  cache.go      bounded LRU program cache (concurrency-safe)
  errors.go     sentinel errors
        │ delegated to
  ┌─────┼──────────────────┬────────────────────────┐
  ▼     ▼                  ▼                        ▼
pkg/prism/alert       pkg/prism/remediation   pkg/executor(runner)
alert domain, single   remediation: gating +   execution result
package: event model + action dispatch         determination
rule engine + rule store
AlertEnv evaluation    RemediationEnv evaluation  ExecutionEnv evaluation
environment            environment               environment
sys_prism_alert_rule   sys_prism_remediation_rule  optional "expression" key in
                       expression = trigger        executor config JSON
                       condition                  (monitor_points.config /
                                                   remediation executor_config)
```

Responsibility boundary principles:

- **Common**: "how expressions are compiled, evaluated, and cached" — `pkg/expr`.
- **Domain-specific**: "when to evaluate and what to do on a match" — implemented separately by each of the three consumption faces:
  - alert = event-stream filtering + Violation extraction;
  - remediation = event triggering + idempotency/cooldown/circuit-breaker gating + action dispatch;
  - execution determination = synchronous determination after a single execution returns and before the retry decision.

**No grand-unified rule engine**: the three have completely different evaluation timing and lifecycles (remediation rules additionally carry runtime state that must stay fresh: cooldown/circuit-breaker/in-flight), and forcing a unified orchestration layer would produce a god object.

**Exactly one evaluation environment per consumption face** (Chapter 4); the scene mechanism is removed entirely.

### 2.2 The End of `pkg/prism/rule`'s Positioning: Merged into `pkg/prism/alert` (Single Package, Single Model, Single Evaluation)

The `rule` package was originally positioned as "a general-purpose rule engine for other packages that need rules". Once this design lands, that positioning no longer exists:

- The generic capabilities (compile/evaluate/cache) have moved down into `pkg/expr`;
- Verified: every real consumer of this package inside CE is on the alert chain or an assembly point: `pkg/prism/config.go`, `pkg/prism/engine.go`, `internal/service/prism.go`, `internal/service/migrate.go`, `internal/api/service/prism/alert.go` (`pkg/prism/alert/model.go` and `pkg/prism/remediation/doc.go` mention it in comments only, no import);
- Once the scene mechanism is deleted, only alert-domain logic remains in the package (`AlertEnv`, `AlertMatcher`, Violation extraction, the `sys_prism_alert_rule` store).

Implementation is in two steps: first migrate it as a subpackage to `pkg/prism/alert/rule` (a migration-period transition), then finally **merge it wholesale into `pkg/prism/alert`**. The nested subpackage and optional interfaces (`ViolationMatcher` etc.) during the migration period are transitional shapes only; the final shape completes three consolidations:

- **Single package**: compiler, engine, rule store, matcher, Violation extraction, the alert event model, and the dispatch contract all belong to one `alert` package with no nested subpackage left; the former `alert/rule → alert` dependency-direction problem dissolves accordingly, and remediation and executor still do not depend on the alert package.
- **Single model**: the persistence model (formerly `rule.Record`) and the runtime view (formerly `rule.Rule`) merge into a single `alert.Rule` — GORM tags inlined (the `sys_prism_alert_rule` table stays as-is), `Metadata` remains a string column, decoded on demand via `Rule.MetadataMap()` (bad JSON returns nil, tolerating historical dirty rows), and `Spec` (static rules from config files) is retained with `Register` constructing a `Rule` directly (static rules use negative IDs).
- **Single evaluation**: `alert.Matcher` is a single-method contract `Match(ctx, evt) MatchResult`, with `MatchResult{Forward bool; Violations []Violation}`; the engine collapses into `Engine.Evaluate(ctx, tenantID, env) (matchedIDs []int64, violations []Violation)` — one snapshot, one loop, each rule's expression program runs exactly once, and Violations are built only for matched rules; the default-allow-when-no-rules semantics is retained (see 6.1.3).

Follow-on changes:

- Go imports reference `pkg/prism/alert` directly (the 5 CE sites above; tickraft-x in 12.1);
- The package's core symbols (`Rule`/`Store`/`Engine`/`Config`/`Register`/`NewCompiler`/`ErrRuleNotFound` etc.) keep their names, so consumers only change their import lines;
- The earlier idea that "remediation reuses `rule.AssetEnv`" is dropped: each consumption face instead carries its own private asset-domain constructor (4.4), avoiding remediation depending on the alert package.

### 2.3 Package Name Conflict Between `pkg/expr` and expr-lang

Both have the package name `expr`. Conventions:

- Files inside `pkg/expr` uniformly alias `github.com/expr-lang/expr` and its subpackages as `exprlang` when referencing them;
- `pkg/expr` exposes its own opaque type `expr.Program` to the outside, so **consumers never need to import expr-lang** (the sole exception: the AST traversal in `pkg/prism/alert/violations.go` is domain logic that references expr-lang's `ast/parser` directly, also under the `exprlang` alias).

### 2.4 Dependency Version

Keep `github.com/expr-lang/expr v1.17.8`. Verified: v1.17.8 ships 71 builtin functions in total and **no regex function** — regex matching is provided by the grammar-level `matches` operator (same family as `contains`/`startsWith`/`endsWith`, see 5.1), so there is no need to upgrade the dependency or add a custom regex function.

### 2.5 tickraft-x Positioning

tickraft is tickraft-x's **infrastructure**: x reuses CE's `pkg/*` kernel directly via `replace => ../tickraft`, and the x side has its own parallel rule business (a self-built remediation engine, a worker rule analyzer, a structured frontend editor). This revamp **must update and support x's business logic in lockstep**, not rebase afterwards — the full plan is in Chapter 12.

tickraft-x's current reference surface over `pkg/prism/rule` (7 files; with 2.2's package merge they all uniformly change their imports to `pkg/prism/alert`, and the core symbols `Rule` (the single model, with the former `Record` folded in)/`Store`/`Engine`/`Config`/`Register`/`NewCompiler`/`CompilerConfig`/`ErrRuleNotFound` are all retained): gRPC rule pulling, API rule CRUD (a local copy of the CE service), Prism orchestration (`BuildRuleConfig`), distributed Redis sync, worker rule sources. See 12.1 for the affected schema and implicit conventions.

---

## 3. Expression Language Specification

### 3.1 Naming Conventions (Three Levels)

1. **Single words first, top level lowercase**: `severity`, `metrics`, `body`, `error`, `code`, `duration`, `trigger`, `threshold`, `level`, `keyword`, `content`, `source`, `type`.
2. **Group related fields under a single-level noun domain**:
   - `asset.id`, `asset.name`, `asset.type`, `asset.tags`
   - `metric.name`, `metric.value`
   - `status.previous`, `status.current` (full words, no prev/curr abbreviations)
3. **Reference depth cap**: two property levels + one index level — `asset.tags["env"]` and `metrics["cpu"]` are the legal maximum.
4. **Dual referencing for domain objects**: noun-domain objects (`asset`/`metric`/`status`) are carried as maps in the implementation (see 4.4), and the two reference styles `.field` and `["field"]` are equivalent — `asset.id` ≡ `asset["id"]`; document examples uniformly use the dot style.

Prohibited:

- No camelCase **variable names**; **function names** follow expr-lang's standard spelling — lowercase functions primarily; standard camelCase names such as `startsWith`/`endsWith`/`fromJSON` are allowed, but document examples always use lowercase equivalents (e.g. `matches "^prefix"` instead of `startsWith`);
- No property chains of three or more levels;
- No synonymous aliases (the current `alert.xxx`/`event.xxx` dual prefixes are all removed; every variable has exactly one name);
- No referencing internal fields such as `tenant_id` in expressions (tenant filtering is engine-internal behavior, see 6.1.5).

### 3.2 Literals and Operators

- Literals: integers, floats, double-quoted strings, `true/false`, arrays `[...]`, maps `{"k": v}`, `nil`.
- Arithmetic: `+ - * / %` (string `+` is concatenation).
- Comparison: `== != > >= < <=`.
- Logical: `&& || !`.
- Membership: `in` (arrays/map keys).
- String operators: `matches` (regex), `contains` (substring), `startsWith`/`endsWith` (prefix/suffix).
- Indexing: `metrics["cpu"]`, `asset.tags["env"]`; domain-object fields are dual-reference, `asset["id"]` is equivalent to `asset.id`.
- Time: `now()` for the current time, `duration("5m")` as a duration literal (used together with comparisons).

### 3.3 Sandbox Constraints (Minimal Constraint Set)

The verdict on expr-lang's capability surface: **no builtin-function whitelist, no comparison-count cap; only three structural constraints remain**.

| Constraint | Value | Description |
|---|---|---|
| `MaxNodes` | 1000 | Cap on AST node count, prevents overly long expressions from slowing compilation and evaluation |
| Result type | bool enforced | `AsBool` compile-time assertion; non-boolean expressions fail to compile |
| Env type checking | enabled | Compile-time validation of top-level variable existence and types (layering details in 4.4) |

Rationale:

- **Drop the whitelist (formerly `DisableAllBuiltins` + an enable list)**: expression authors are necessarily admins holding configuration privileges, not untrusted input; all 71 expr-lang v1.17 builtins are pure computation functions with no I/O and no side effects; the historical lesson of the whitelist is maintenance cost and drift between the two repos (A-1), while the only benefit was "a narrower language surface". Once fully opened up, users can use all standard capabilities directly against the official expr-lang documentation, and this project only maintains documentation for the "recommended style" (5.2).
- **Drop the comparison-count cap (formerly 3)**: the cap only limits how many times `> >= < <=` appear, offering no security value (expression complexity is already bounded by MaxNodes), yet it rejects reasonable expressions like `a > 1 && b > 2 && c > 3 && d > 4` and is a source of user confusion. tickraft-x already lifted it with `MaxComparisons: -1` and runs stably; this revamp removes it uniformly in both repos (12.5).
- **Keep the three**: they respectively guard against accidentally pasting overly long text, guarantee determination semantics (expressions must be predicates), and stop variable misspellings at entry-point validation.

### 3.4 Unified Result Code `code` and Time Units

- `code`: http/webhook executors = HTTP status code; local = process exit code (carried by the new `ExitCode` field on `Result`); tcp/icmp = 0 (connectivity failures are determined by default semantics or `error`/`metrics`).
- The time unit is unified to **milliseconds** (the `duration`, `duration_ms` contexts); frontend hints are labeled consistently with this document.

### 3.5 Field Naming Conventions (Column Names / API Fields)

| Location | Name | Semantics |
|---|---|---|
| `sys_prism_alert_rule.expression` | `expression` | alert rule expression (when an alert is raised) |
| `sys_prism_remediation_rule.expression` | `expression` | remediation trigger condition (when remediation fires; the former `condition_expr` renamed for uniformity) |
| Optional key in executor config JSON | `"expression"` | execution result determination (what counts as a successful execution; empty/absent = protocol default semantics) — lives with the executor config: inside `sys_prism_remediation_rule.executor_config` and `monitor_points.config` |
| Metadata pass-through key | `"expression"` | pass-through key along the execution chain |
| API / frontend field | `expression` | consistent with the column name / key name |

Field naming is thereby unified globally: **at the column level there is exactly one expression field, `expression`, whose semantics depend on the containing table (alert / remediation trigger); execution determination is not a standalone column but an optional key in the executor config JSON** — remediation actions invoke executors, and the success criterion of an action belongs to the "how to execute" configuration, not the "when to trigger" rule.

### 3.6 Error Semantics

Compile error categories (entry-point validation uniformly returns HTTP 400, with the response body carrying expr-lang's original error message):

1. Syntax errors;
2. Unknown variables (top-level variables are caught by the compile-time Env check; fields inside domain objects, such as `asset.naem`, are caught by the sample evaluation in entry-point validation, see 8.4);
3. Type mismatches (compile-time Env type checking);
4. Non-boolean results;
5. Exceeding the node-count cap.

Runtime evaluation failures (rare; type problems are already blocked at compile time):

- Alert engine: warn log + skip the rule (the event is treated as unmatched this time);
- Remediation condition: warn log + return false (the event does not trigger this time);
- Execution determination: warn log + **fall back to protocol default semantics** (not judged as failure — an expression bug should not trigger an alert storm).

### 3.7 Match-Object Naming Contract (Consistent Across tickraft and tickraft-x)

Naming of rule match objects (evaluation environments) follows these rules uniformly across both repos:

1. **Type naming**: `{Domain}Env` — `AlertEnv` (alert), `RemediationEnv` (remediation), `ExecutionEnv` (execution determination). The current CE remediation `EventContext` naming is inconsistent, so it is **renamed to `RemediationEnv`** (the identically named `EventContext` on the x side is renamed as well, see 12.4).
2. **The ENV is a data contract; cross-repo reuse of Go types is not the goal**: the authoritative definition of the contract is the variable table in Chapter 4 of this document (field names, types, semantics); CE and x each define their own structs, and x may define a superset (extension fields), but **the names and types of existing fields must not deviate from the contract**. The compiler's Env parameter in `pkg/expr` is `any`, so both sides' structs can be used directly.
3. **Registration system for extensions**: new match variables added by x must follow this chapter's naming conventions (lowercase single words / single-level noun domains / two-level property cap) and be registered in the 4.5 extension table of this document before implementation.
4. **Unified enum values**: the canonical trigger types are `metric` / `log` / `status_change` (the x-only `fault_event` is retained as an x extension value; x's current `metric_alert`/`log_alert` are renamed to `metric`/`log`); asset states keep `normal`/`abnormal`/`unknown`; result-code and time-unit conventions are in 3.4.
5. **Dual access and full-word naming for domain objects**: noun-domain objects (`asset`/`metric`/`status`) are carried as maps; `asset.id` is equivalent to `asset["id"]`; state-transition fields are named in full as `status.previous` / `status.current` (no abbreviations).

---

## 4. Evaluation Environment (ENV) Design

With the scene mechanism removed, each consumption face has exactly one ENV. The implementation shape is **top-level struct + domain-object maps** (details in 4.4): the top-level struct carries compile-time variable checking; domain objects (`asset`/`metric`/`status`) are carried as maps supporting both `.field` and `["field"]` references. Enum values are always stored as plain `string`/`int`/`float64`, avoiding expr-lang named types reporting "mismatched types" when compared against literals (a pitfall the current code has already hit).

### 4.1 Alert Rule ENV (`AlertEnv`, defined in `pkg/prism/alert`)

| Variable | Type | Description | Example |
|---|---|---|---|
| `type` | string | alert category: `metric` / `log` / `status` | `type == "log"` |
| `severity` | string | primary Violation severity: `info` / `warning` / `critical` | `severity == "critical"` |
| `source` | string | alert source (for logs the origin IP, for probes the probe point identifier) | `source == "10.0.0.1"` |
| `keyword` | string | log category: the matched keyword | `keyword matches "fatal\|panic"` |
| `content` | string | log category: the matched log line | `content matches "timeout\|refused"` |
| `metrics` | map[string]float | metric category: related metric values | `metrics["cpu"] > 90` |
| `asset.id` | int | ID of the asset that raised the alert | `asset.id == 42` |
| `asset.name` | string | asset name | `asset.name == "web-1"` |
| `asset.type` | string | asset type | `asset.type == "host"` |
| `asset.tags` | map[string]string | asset tags (projected from asset metadata) | `asset.tags["env"] == "prod"` |

Changes relative to the current state (`MetricMatchEnv`): drop the `alert.`/`event.` dual prefixes and `timestamp`/`asset_id`/`tenant_id` (useless for determination; tenant filtering becomes internal); the `asset` domain gains `tags` (previously only the task scene had it) and drops `asset.status`/`asset.tenant_id`.

### 4.2 Remediation Condition ENV (`RemediationEnv`, defined in `pkg/prism/remediation`; renamed from the former `EventContext`)

| Variable | Type | Description | Example |
|---|---|---|---|
| `trigger` | string | trigger type: `metric` / `log` / `status_change` (x extension `fault_event`, see 4.5; renamed from the former `type` to avoid ambiguity) | `trigger == "metric"` |
| `level` | string | log category: log level | `level == "error"` |
| `keyword` | string | log category: matched keyword | `keyword matches "oom\|killed"` |
| `content` | string | log category: log content | `content contains "OutOfMemory"` |
| `source` | string | event source (renamed from `source_ip`, aligned with the alert ENV) | `source matches "^10\.0\."` |
| `threshold` | float | metric category: threshold | `threshold >= 90` |
| `metric.name` | string | metric category: metric name | `metric.name == "cpu"` |
| `metric.value` | float | metric category: observed value | `metric.value > 95` |
| `status.previous` | string | status category: state before the transition | `status.previous == "normal"` |
| `status.current` | string | status category: state after the transition | `status.current == "abnormal"` |
| `asset.id` | int | asset ID (the ID from the former `asset_id`/`asset_key`) | `asset.id == 42` |
| `asset.key` | string | asset unique key within the tenant (the former `asset_key`; carried only on status_change triggers, empty string otherwise) | `asset.key == "web-1"` |
| `asset.name` | string | asset name (**new enrichment**, see 6.2.1) | `asset.name == "web-1"` |
| `asset.type` | string | asset type (**new enrichment**) | `asset.type == "host"` |
| `asset.tags` | map[string]string | asset tags (**new enrichment**) | `asset.tags["env"] == "prod"` |

An empty expression (`expression == ""`) matches all events of that trigger type (current semantics retained).

### 4.3 Execution Determination ENV (`ExecutionEnv`, defined in `pkg/executor`)

| Variable | Type | Description | Example |
|---|---|---|---|
| `code` | int | unified result code (see 3.4) | `code == 200` |
| `body` | string | response body / output (already truncated at the executor's cap) | `body matches "\"status\":\"ok\""` |
| `error` | string | execution error message (empty string on success) | `error == ""` |
| `duration` | float | execution elapsed time, milliseconds | `duration < 500` |
| `metrics` | map[string]float | metrics produced by the executor | `metrics["rtt_ms"] < 100` |

### 4.4 Draft Go Type Definitions

```go
// pkg/prism/alert — the top-level struct guarantees compile-time variable checking
type AlertEnv struct {
    Type     string             `expr:"type"`
    Severity string             `expr:"severity"`
    Source   string             `expr:"source"`
    Keyword  string             `expr:"keyword"`
    Content  string             `expr:"content"`
    Metrics  map[string]float64 `expr:"metrics"`
    Asset    map[string]any     `expr:"asset"` // domain-object map: asset.id ≡ asset["id"]
}

// Asset-domain constructor carried by each consumption face (private; the field set is governed by the 4.1/4.2 variable tables)
func buildAssetEnv(a asset.Asset) map[string]any {
    return map[string]any{
        "id":   a.ID,
        "name": a.Name,
        "type": string(a.AssetType),
        "tags": a.Tags, // map[string]string, supports asset.tags["env"]
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

Implementation notes:

- **Layered type checking**: top-level scalars use struct + expr tags, so unknown top-level variables and top-level type errors surface at **compile time**; domain objects use `map[string]any` to gain dual referencing, at the cost that misspelled fields inside a domain (`asset.naem`) are invisible at compile time — caught by the second gate of entry-point validation, "sample evaluation" (8.4), with runtime evaluation-failure semantics as the final backstop (3.6).
- **Private per-domain constructors**: `buildAssetEnv` exists separately in alert and remediation (a few lines of map literal each), with no shared type exported across packages. The only cross-domain contract is the Chapter 4 variable table (anti-drift mechanism in 12.7), avoiding remediation depending on the alert package (2.2).
- **Exported sample constructors**: each ENV definition site exports `ExampleEnv()` (a valid sample with all fields populated), used by entry-point validation's sample evaluation (8.4) and x's contract tests (12.7).

### 4.5 x Extension Variable Registry (tickraft-x Specific)

x has one more remediation trigger type than CE (`fault_event`, fault events), and its `RemediationEnv` is a superset of the CE contract. The extension variables, registered per the 3.7 registration system, are as follows (naming already migrated per the conventions from x's current flat `event.xxx` prefixes):

| Variable | Type | Description | Current x name |
|---|---|---|---|
| `fault.type` | string | fault event type (carried on `fault_event` triggers) | `fault_type` |
| `client.id` | string | reporting client identifier (carried on `fault_event` triggers) | `client_id` |
| `payload` | map[string]any | raw event payload (may be carried on any trigger; mixed value types, mind the types when comparing) | `payload` |

CE does not define these fields (CE has no corresponding event source); the x-side struct appends them on top of the 4.4 contract fields. **Rename mapping for in-contract fields on the x side**: `event.type → trigger`, `event.metric_name/value → metric.name/value`, `event.prev_status/curr_status → status.previous/status.current`, `event.source_ip → source`, `event.asset_id → asset.id`.

---

## 5. Function Strategy

### 5.1 Principle: Zero Custom Functions

**Never hand-roll what the expr standard already supports**. Item-by-item verdicts:

| Former custom function | Standard equivalent | Verdict |
|---|---|---|
| `regex(p, s)` | `s matches "pattern"` (grammar-level operator, usable under the sandbox) | dropped |
| `containsany(s, list)` | `s matches "a\|b\|c"` (regex alternation) | dropped |
| `inrange(v, min, max)` | `v >= min && v <= max` (comparison-operator composition) | dropped |
| `ago(d)` | `xxx > now() - duration("5m")` | dropped |
| `startswith` / `endswith` | `s startsWith "err"` / `s matches "^err"` | dropped |

Basis (verified): the v1.17.8 builtin function table (71 functions) has no regex function; `matches`/`contains`/`startsWith`/`endsWith` are expr-lang's **infix operators** rather than builtin functions — grammar-level capabilities available under any compiler configuration (the former `DisableAllBuiltins` sandbox did not affect operators, as evidenced by the CE repo tests `pkg/prism/rule/compiler_test.go:190` and `integration_test.go:129`). With this design's whitelist removal there is even less restriction; `matches` only needs one regression unit test.

### 5.2 Builtin Functions: Fully Open (No Whitelist)

Conclusion: **`DisableAllBuiltins` is not enabled; all standard expr-lang v1.17 builtin functions are available** (rationale in 3.3). Key points:

- Users can use everything the official expr-lang documentation offers (collection predicates, aggregation, string handling, type conversion, JSON handling, etc.) directly; this project writes no separate documentation for builtins and maintains only a recommended style;
- Recommended style: prefer lowercase functions (`len`/`map`/`trim`/`split`/`filter`…); camelCase standard builtins (`startsWith`/`fromJSON`…) are allowed, but document examples do not use them proactively (3.1);
- The former whitelist's "dead entries" problem (`contains`/`startsWith`/`endsWith` are actually operators, so `EnableBuiltin` had no effect on them) disappears along with the wholesale removal of the whitelist mechanism;
- "Builtin outside the whitelist rejected" is no longer an error category (3.6 shrinks accordingly).

### 5.3 Admission Bar for New Custom Functions (Registration System)

A new custom function is allowed only if both conditions hold:

1. expr's standard capabilities (operators + all standard builtins, see 5.2) genuinely cannot express it;
2. a real, mandatory scenario exists (not mere convenience).

Every addition must be registered in this document: function name (lowercase word), signature, semantics, and a necessity argument.

### 5.4 Performance Note

The `matches` operator compiles its regex on the fly at every evaluation (expr-lang VM behavior; the existing custom `regex()` is equally uncached, so there is **no regression**). If it ever becomes a hot spot under high-frequency rules, evaluate a "matches-equivalent implementation with a pattern cache" against the 5.3 bar — a reserved decision.

---

## 6. Design of the Three Consumption Faces

### 6.1 Alert Rule Engine (Alert-Only)

#### 6.1.1 Package Path, Table, and Model

- The package finally lands at **`pkg/prism/alert`** (transitioning through the `pkg/prism/alert/rule` subpackage first, then merging into a single package; rationale and follow-on changes in 2.2);
- The table is renamed `sys_prism_rule` → `sys_prism_alert_rule`, **dropping the `scene` column** (DDL in 7.1);
- The persistence model and the runtime view merge into a single `Rule` model (no `Scene` field); `Spec` (static rules) likewise drops the `Scene` field.

#### 6.1.2 Deletion List (the Scene Mechanism Disappears Entirely)

- The `Scene` enum and its four constants (`SceneTask`/`SceneProbe`/`SceneMetric`/`SceneRemediation`);
- `TaskView`/`ResultView`/`ReportView`/`AlertView`/`RemediationView` and all projection functions (replaced by the new `AlertEnv` construction);
- `TaskMatchEnv`/`ProbeMatchEnv`/`MetricMatchEnv`/`RemediationMatchEnv`;
- `TaskMatcher`/`ProbeMatcher`; `MetricMatcher` is renamed **`AlertMatcher`** (the only surviving matcher);
- The engine's four scene groups (`taskRules`/`probeRules`/`metricRules`/`remediationRules`) collapse into a **single rule set**;
- API method consolidation: `HasMetricRules → HasRules`; the engine's dual methods (`Match`/`MatchWithViolations`) and the matcher optional interfaces (`ViolationMatcher`/`NamedMatcher`) finally merge into the single-evaluation contract — `Matcher.Match(ctx, evt) MatchResult` and `Engine.Evaluate` (see 2.2);
- `MatchRemediation`/`HasRemediationRules`/`ErrRuleInvalidScene` and related tests.

#### 6.1.3 Evaluation Flow (the Unchanged Parts)

Event-driven semantics are preserved: telemetry event → `prism.Engine.Dispatch` → governance guard chain → `AlertMatcher.Match` (builds the `AlertEnv`, including asset enrichment with a single asset query) → `Engine.Evaluate`'s single-loop evaluation (one snapshot, `expr.Run` outside the lock, each rule program runs exactly once) → matched rules produce structured Violations via `ViolationExtractor` → alert record persistence + channel dispatch. The **default-allow-when-no-rules** semantics is retained (`Match` returns `Forward: true` when `HasRules` is false).

#### 6.1.4 Violation Extraction Fix

`buildViolation` fills in the `Severity`/`Source` field pass-through (fixes D-14); after Dispatch replaces `evt.Violations`, sorting and channel rendering no longer lose information.

#### 6.1.5 Tenant Filtering (fixes D-11)

- Rule loading keeps `tenant_id`;
- Filter before evaluation: `rule.TenantID == 0 || rule.TenantID == evt.TenantID` (tenant 0 = global rules);
- `ListEnabled(tenantID)` semantics unchanged, for extension runtimes to load per tenant.

#### 6.1.6 Hot-Reload Wiring (fixes D-03, D-12)

- When building `rule.Config`, `internal/service/prism.go` passes `EvalInterval: cfg.Prism.EvalInterval` (config key `prism.eval_interval`, default 30s); the polling Reload loop starts, and DB-side changes finally take effect;
- `migrateStores` builds its compiler with `cfg.RuleConfig.CompilerConfig` instead, eliminating the config divergence between the write path and the engine.

### 6.2 Remediation

#### 6.2.1 Condition Evaluation Consolidation

- Delete `remediationBuiltinWhitelist`/`compileCondition`/`matchCache` and switch to `pkg/expr`:
  - Compilation: `expr.NewCompiler()` (minimal constraint set, see 3.3) + `RemediationEnv` (the renamed former `EventContext`) as the Env type;
  - Caching: `pkg/expr`'s LRU `ProgramCache` (default capacity 512, key = Env type name + expression string), replacing the unbounded `matchCache` (fixes D-07);
- **Asset enrichment**: the `Manager` gets an asset store injected (a new dependency) and fills in `asset.name/type/tags` when building the `RemediationEnv` (`asset.id/key` still come from the event payload).

#### 6.2.2 Gating and Circuit Breaking

- The gating chain stays: idempotency (in-flight) / cooldown (`last_run_at`) / circuit breaker (consecutive-failure count);
- **Atomicized circuit-breaker counting** (fixes D-08): `consecutive_failures` is promoted from the `metadata` JSON to a standalone column; on failure, execute the atomic SQL `UPDATE sys_prism_remediation_rule SET consecutive_failures = consecutive_failures + 1 WHERE id = ?`, set to 0 on success; when the threshold is reached, set `status = paused`;
- **Tightened idempotency gating** (mitigates D-09): the in-flight set moves earlier — after the rule query and before the gate checks (mark first, check second) — eliminating the check-then-set window.

#### 6.2.3 Execution Result Determination Hook-In

- **No execution-determination column on `sys_prism_remediation_rule`**: an action's success criterion is part of the execution config and lives in the optional `expression` key of the `executor_config` JSON (3.5);
- When `executorOperator` builds the `executor.ExecutionRequest`, it parses that key from `executor_config` and writes it into `Metadata["expression"]`;
- Execution success/failure is determined uniformly at the executor layer (see 6.3), and the `Success` result flows back into the circuit-breaker count — **the user-defined "success" criterion directly drives the circuit breaker**;
- `ExecutionRequest` gains Timeout pass-through (fixes D-15).

#### 6.2.4 Entry-Point Validation (fixes D-06)

`validateRule` gains two validation gates that return 400 on failure:

1. When the trigger-condition `expression` is non-empty, compile + sample-evaluate against the `RemediationEnv`;
2. When the `expression` key inside the `executor_config` JSON is non-empty, do the same against the `ExecutionEnv`.

### 6.3 Executor Execution Determination

#### 6.3.1 Data Structures

- `executor.Result` gains an `ExitCode int` field; `pool.go`'s `reset()` clears it in sync;
- The `local` executor writes the exit code (`ExitCode()` of `exec.ExitError`; -1 for codeless cases such as command-not-found or killed-by-signal);
- The webhook executor gains an `expect_status` config (aligned with http, fixes D-16).

#### 6.3.2 Determination Location and Semantics

Determination is implemented at the **single convergence point** `runner.doExecute` (`pkg/executor/lifecycle.go`), after the executor returns its result and **before** the retry decision:

```text
result = executor.Execute(req)
if exprStr := req.Metadata["expression"]; exprStr != "" {
    env := buildExecutionEnv(result)          // code/body/error/duration/metrics
    ok, err := exprCache.Eval(exprStr, env)   // LRU-cached compiled artifact
    if err != nil {
        warn("expression eval failed, fallback to protocol default")
        // keep result.Status unchanged (protocol default semantics)
    } else if ok {
        result.Status = normal
    } else {
        result.Status = abnormal
        result.ErrorMsg = appendNote(result.ErrorMsg, "expression judged failure")
    }
}
if result.Status != normal { retry... }        // existing retry logic; user semantics trigger retries too
```

Semantic rules:

- **Empty expression = protocol default semantics** (each executor's current logic; invisible to zero-config users);
- Non-empty: `true` = success (normal), `false` = failure (abnormal + an ErrorMsg note);
- Runtime evaluation failure: warn + fall back to the protocol default (not judged as failure, see 3.6);
- Compile errors are already blocked by entry-point validation (at monitor point / remediation rule CRUD time); `doExecute` handles only runtime errors;
- Evaluation completes **before** the Result is returned to the object pool (a pooled-Result lifecycle constraint).

#### 6.3.3 Configuration Pass-Through Chain

Execution determination expressions uniformly live in the **optional `expression` key of the executor config JSON**, extracted by the assembly side that builds the `ExecutionRequest` and passed through via Metadata:

```text
the "expression" key of monitor_points.config
  → telemetry/prober_service.pointToProbeTask extracts it from the config JSON and stuffs it into task.Metadata["expression"]
    → the task fires TypeExecutionTriggered (event.WithMetadata)
      → runner.dispatch assembles ExecutionRequest.Metadata (existing channel, same pattern as max_retries)
        → doExecute reads it and determines

the "expression" key of sys_prism_remediation_rule.executor_config
  → Manager.dispatch → executorOperator extracts it from the executor_config JSON and writes Metadata["expression"]
      → same as above
```

The assembly side extracts it rather than the runner parsing executor-specific JSON: although `expression` is a cross-executor generic key, config-parsing responsibility stays in the assembly layer and the runner only reads Metadata (same pattern as `max_retries`).

Scheduled tasks (`sys_schedule_task`) do not open up `expression` this time: under task semantics the exit code is naturally intuitive, so this is left for later enablement on demand (just register it in this document).

---

## 7. Persistence Design

### 7.1 Table Schema Change Overview

Rule tables are **split by nature** (not merged): one table for pure matching rules (alert), one table for match + action + runtime state (remediation); execution determination is the check point's private config (carried in a column that travels with the check definition), and no new tables are created.

| Table | Change |
|---|---|
| `sys_prism_rule` | **renamed `sys_prism_alert_rule`**, drop the `scene` column |
| `sys_prism_record` | **renamed `sys_prism_alert_record`** (symmetric naming) |
| `sys_prism_remediation_rule` | `condition_expr` column **renamed `expression`** (unified trigger-condition semantics, see 3.5); + `consecutive_failures` column; the counter field in the `metadata` JSON is deprecated; execution determination goes through the optional `executor_config` JSON key, no column change |
| `monitor_points` | **no column change**: execution determination is the optional `expression` key of the `config` JSON |
| `sys_remediation_rule` / `sys_remediation_record` | legacy orphan tables, **explicitly DROPped** |

### 7.2 DDL Draft (SQLite dialect; actually generated by GORM AutoMigrate, shown here as the contract)

```sql
-- rename + drop column (unreleased: create the table directly under the new name, no migration path)
CREATE TABLE sys_prism_alert_rule (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  tenant_id   INTEGER NOT NULL DEFAULT 0,
  name        VARCHAR(255) NOT NULL,
  description TEXT,
  expression  TEXT NOT NULL,
  enabled     BOOLEAN NOT NULL DEFAULT 1,
  priority    INTEGER NOT NULL DEFAULT 0,
  group_id    INTEGER,                -- resource-group visibility (nullable index)
  metadata    TEXT,                   -- JSON
  created_at  DATETIME,
  updated_at  DATETIME,
  deleted_at  DATETIME
);
CREATE INDEX idx_alert_rule_tenant  ON sys_prism_alert_rule(tenant_id);
CREATE INDEX idx_alert_rule_enabled ON sys_prism_alert_rule(enabled);
CREATE INDEX idx_alert_rule_group   ON sys_prism_alert_rule(group_id);
CREATE INDEX idx_alert_rule_deleted ON sys_prism_alert_rule(deleted_at);

-- remediation rules: trigger-condition column rename + circuit-breaker counter column (unreleased: AutoMigrate creates the table directly under the new structure;
-- the RENAME here is for semantic illustration only)
ALTER TABLE sys_prism_remediation_rule RENAME COLUMN condition_expr TO expression;
ALTER TABLE sys_prism_remediation_rule ADD COLUMN consecutive_failures INTEGER NOT NULL DEFAULT 0;

-- monitor points: no DDL change (execution determination is the optional "expression" key of the config JSON)

-- orphan table cleanup
DROP TABLE IF EXISTS sys_remediation_rule;
DROP TABLE IF EXISTS sys_remediation_record;
```

### 7.3 Update Semantics Fix (root cause of D-01 and D-02)

Both stores' `Update` changes from `Save` (overwrites all columns) to **column-level `Select` updates**, touching only user-editable columns:

- `sys_prism_alert_rule`: `name, description, expression, enabled, priority, group_id, metadata, updated_at`;
- `sys_prism_remediation_rule`: `name, description, asset_id, trigger_event_type, expression, executor_type, executor_config, cooldown, circuit_breaker_threshold, enabled, updated_at`;
- Runtime-state columns (`status`/`last_run_at`/`consecutive_failures`) and `tenant_id`/`created_at` are **never touched by API Updates**;
- Service-layer DTOs are changed in sync to carry all fields (whatever GET returns, PUT accepts), as a double safeguard.

---

## 8. API and Validation Changes

### 8.1 Alert Rule API (`/api/v1/prism/alert/rules`)

- Request/response DTOs **drop the `scene` field** (the scene concept disappears entirely);
- `expression` still goes through pre-compilation validation on the `Store` write path (with the Env switched to `AlertEnv`);
- The engine `Reload` triggered after CRUD stays as-is.

### 8.2 Remediation Rule API (`/api/v1/prism/remediation/rules`)

- Request/response DTOs: the `condition_expr` field is **renamed `expression`** (trigger condition); the `executor_config` JSON carries the optional `expression` key (execution determination; the frontend provides the input in the executor config section, see 9.5);
- `validateRule` dual validation (6.2.4), example error body:

```json
// POST /api/v1/prism/remediation/rules — expression compilation failed
{
  "code": 40000,
  "message": "invalid expression: unexpected token \")\" (1:12)",
  "request_id": "..."
}
```

The error code stays `errdefs.CodeBadRequest`; `message` carries expr-lang's original compile error (with line and column positions), which the frontend displays directly.

### 8.3 Monitor Point API

- Monitor point create/update: extract the `expression` key from the `config` JSON for validation (Env is `ExecutionEnv`, compile + sample evaluation); a bad expression yields 400; without the key, behavior is unchanged;
- Other fields unchanged.

### 8.4 Generic Expression Validation Endpoint (frontend live validation / dry run)

```json
POST /api/v1/expr/validate
{ "env": "alert", "expression": "metrics[\"cpu\"] > 90" }

200 → { "valid": true }
400 → { "code": 40000, "message": "unknown name asset.naem (1:1)", "request_id": "..." }
```

- `env` values: `alert` / `remediation` / `execution`, each based on the corresponding ENV;
- Validation = **compilation** (all 3.6 error categories) + **sample evaluation** (evaluate once against that ENV's `ExampleEnv()`, catching spelling and type errors of fields inside domain objects — the backfill for 4.4's layered checking); no side effects, nothing persisted;
- The frontend wizard/expert modes' blur and pre-submit validation uniformly go through this endpoint (9.4);
- Each CRUD's server-side entry-point validation is kept independently (guarding against bypassing the frontend and calling the API directly); both ends' validation logic share the same source (`pkg/expr`'s `Validate(env, expression)`), and tickraft-x's DryRun reuses the same kernel (12.4).

---

## 9. Frontend Rule Editing Design (Dual Mode)

### 9.1 Dual-Mode General Rules

The three expression entry points (alert rules, remediation trigger conditions, execution determination) uniformly offer two editing modes, and **the generated `expression` string is the single source of truth**:

- **Wizard mode (default)**: build interactively from "condition rows" that ultimately generate a single expression; suited to users unfamiliar with expr-lang;
- **Expert mode**: write the expression directly in a textarea (monospace font); suited to users familiar with the expr-lang spec (full capabilities in 3.3/5.2).

Mode-switching rules:

- Wizard → Expert: compile the current condition rows into an expression and fill the textarea; hand editing can continue;
- Expert → Wizard: try to **reverse-parse** the existing expression into condition rows; only the wizard-generatable subset is supported (9.2); on parse failure, show "the current expression exceeds wizard capabilities" and stay in expert mode (saving is not blocked);
- Rules saved after hand editing automatically re-appear in expert mode when edited again.

### 9.2 Bidirectional Mapping Between the Wizard and Expressions

Condition-row model (frontend component internal state, not persisted):

```text
row := variable × operator × value
variable := top-level variable | domain-object.field | metrics["key"] | asset.tags["key"]
combination := row ((AND | OR) row)*, row-level NOT supported
```

Generation mapping (wizard → expression):

| Wizard element | Generated fragment |
|---|---|
| Combining logic AND / OR | `&&` / `\|\|` |
| Row-level negation | `!(...)` |
| String equals / not-equals | `==` / `!=` |
| Numeric comparison | `>` `>=` `<` `<=` |
| Regex / contains / prefix / suffix | `matches` / `contains` / `startsWith` / `endsWith` (string values auto-quoted and escaped) |
| `metrics` / `asset.tags` key indexing | `metrics["cpu"]`, `asset.tags["env"]` (the key is a text input) |
| Emptiness checks | `error == ""`, `keyword != ""` |

Reverse parsing (expression → condition rows) is implemented as a restricted recursive-descent parser that accepts only the subset generatable by the table above; this is a **pure frontend module** (`web/app/utils/expr-builder/`), not backend. x's structured editor (`EditFull`) is the x variant of wizard mode and reuses the same mapping (12.6).

### 9.3 Variable Catalog (Selector Data Source)

- The frontend maintains **variable catalog constants** (variable path, type, one-line description, owning env), kept in sync with this document's Chapter 4 variable tables (updated within the same PR); tickraft-x appends the 4.5 extension variables to the catalog;
- Operator dropdowns filter by variable type: string → `== != matches contains startsWith endsWith in`; number → `== != > >= < <=`; map (`metrics`/`tags`) → pick the key first, then offer operators per the value type;
- The catalog constants also drive expert mode's **autocomplete and hover hints** (pick a lightweight approach based on existing component dependencies, decided at implementation time).

### 9.4 Live Validation Integration

- On blur and before submit, call `POST /api/v1/expr/validate` (8.4); error messages (with line and column) are displayed inline below the editor;
- Wizard mode does not call the backend when structurally valid (rows complete, value types matching); expert mode always calls.

### 9.5 Page-Level Differences of the Three Entry Points

| Entry point | env catalog | Placeholder example |
|---|---|---|
| Alert rule editing (`views/prism/rule/edit`) | `alert` | `metrics["cpu"] > 90` |
| Remediation rule editing (`views/prism/remediation/rule/edit`) | `remediation` (trigger condition `expression`) | `metric.name == "cpu" && metric.value > 95` |
| Remediation executor config section / monitor point editing | `execution` (the `expression` key of the config JSON) | `code == 200 && duration < 500` |

Alert rule editing also drops the scene dropdown; the old `conditionExpr` field in remediation editing is renamed `expression`; the execution-determination input sits inside the executor config form (the local/http/webhook forms already exist; append an optional row).

### 9.6 i18n (`zh-Hans` / `en` in Sync)

- Delete: all scene-related keys, `conditionExpr`-related keys;
- Add: dual-mode switching (wizard/expert), condition-row placeholders (variable/operator/value), the "exceeds wizard capabilities" hint, variable catalog descriptions, validation error prefixes;
- Update: example copy for the three entry points.

---

## 10. Defect Fix Mapping Table

| Defect | Level | Corresponding design item |
|---|---|---|
| D-01 PUT wipes remediation runtime state | P0 | 7.3 column-level updates + full-field DTOs |
| D-02 PUT wipes alert tenant/group/metadata | P1 | 7.3 |
| D-03 eval_interval dead config | P1 | 6.1.6 |
| D-04 scene=remediation dead path | P1 | 6.1.2 wholesale deletion |
| D-05 task/probe scenes idle | P1 | 6.1.2 deletion; probe needs are taken over by the `expression` key of the executor config JSON (6.3) |
| D-06 remediation condition has no entry-point validation | P1 | 6.2.4 |
| D-07 matchCache unbounded | P2 | 6.2.1 LRU replacement |
| D-08 circuit-breaker read-modify-write race | P2 | 6.2.2 atomic column |
| D-09 idempotency TOCTOU | P2 | 6.2.2 mark first, check second |
| D-10 ParseInt ignores errors | P2 | 6.2.1 payload conversion error handling (an illegal asset_id does not land on 0; the event is dropped + warn) |
| D-11 multi-tenant gap | P2 | 6.1.5 |
| D-12 store compiler config divergence | P2 | 6.1.6 |
| D-13 regex recompilation | P3 | 5.1 function removed entirely (replaced by the `matches` operator) |
| D-14 Violation loses severity/source | P3 | 6.1.4 |
| D-15 ExecutionRequest has no Timeout | P3 | 6.2.3 |
| D-16 webhook has no expect_status | P3 | 6.3.1 |
| D-17 leftover debug comments | P3 | cleaned up in passing during implementation |
| A-1 parallel implementations | structural | Chapter 2 (the pkg/expr kernel) |
| A-2 idle scene mechanism / name-reality mismatch | structural | 6.1 (table rename + alert-only) |
| A-3 non-configurable execution determination | structural | 6.3 (optional expression) |

---

## 11. Test Plan and Implementation Order

### 11.1 Test Plan

**pkg/expr unit tests**

- Constraint set: MaxNodes=1000 out-of-range rejected; AsBool fails to compile non-boolean; standard builtins unrestricted (spot-check that the `matches`/`contains` operators and builtins like `fromJSON` work);
- `Validate` (compile + sample evaluation): unknown top-level variables, domain-object field misspellings (`asset.naem`), type mismatches, and non-boolean results are all caught;
- LRU cache: hits reuse the same pointer, capacity eviction, concurrency safety (`-race`).

**Alert rule engine**

- New ENV evaluation: positive and negative cases for every variable in Table 4.1;
- Default allow with no rules; a bad rule is skipped with a warn and does not affect other rules;
- Tenant filtering: global rules (tenant 0) and tenant rules each match;
- Violation extraction retains severity/source;
- Hot reload: CRUD-triggered Reload, polling Reload.

**Remediation**

- Behavior regression after condition evaluation delegates to the kernel (empty expression matches all, compile failure yields false);
- Entry-point validation: both a bad trigger `expression` and a bad `executor_config.expression` return 400;
- Atomic circuit-breaker counting: no lost updates under concurrent failures (`-race` + concurrent cases);
- After PUT, `status`/`last_run_at`/`consecutive_failures` are unchanged (D-01 regression);
- Execution-determination linkage: when `expression` judges false, the Record status is failed and the circuit-breaker count increments by 1.

**Executor**

- `Result.ExitCode`: the three local states — normal exit / non-zero exit / command not found; `pool.reset` clears it;
- Determination semantics: empty-expression default, true/false override, evaluation-failure fallback, retry linkage (judging false triggers a retry);
- `metadata` pass-through chain: the monitor point `config` JSON's `expression` key → execution determination takes effect (end to end); likewise for remediation `executor_config`;
- webhook `expect_status` takes effect.

**tests/httpapi**

- Alert rule CRUD has no scene field; a bad expression yields 400;
- Remediation rule CRUD: the trigger-condition field is `expression`, the execution-determination key inside `executor_config` is validated, runtime-state fields are unchanged after PUT;
- Monitor point CRUD: `expression` key validation inside `config` (a bad expression yields 400);
- `/expr/validate` positive and negative cases for the three envs.

**Frontend**

- vitest: alert rule editing has no scene; dual-mode switching and generation mapping (wizard → expression snapshot cases); reverse-parsing accept/reject cases; inline display of validation-endpoint errors;
- Build passes.

**End-to-end smoke tests**

1. Create alert rule `metrics["cpu"] > 90` → produce a metric threshold breach → an alert is raised and the Violation carries severity;
2. Create remediation rule (trigger `metric.name == "cpu" && metric.value > 95`, `"expression": "code == 0"` inside `executor_config`) → trigger it → the execution record is completed;
3. Configure the monitor point `config` with `"expression": "code == 200 && body matches \"status\":\"ok\""` → a 200 + abnormal body is judged failed and triggers a retry.

### 11.2 Implementation Order

1. `pkg/expr` kernel + unit tests (including `Validate` compile + sample evaluation);
2. `pkg/prism/rule` migration + alert-only refit (ENV replacement, scene removal, table rename, tenant filtering, hot-reload wiring, Update fix; transition through `pkg/prism/alert/rule` then merge into the single `pkg/prism/alert` package, see 2.2);
3. `pkg/prism/remediation` (condition-evaluation consolidation, asset enrichment, atomic circuit breaking, `condition_expr` → `expression` rename, entry-point validation, Update fix);
4. `pkg/executor` (ExitCode, the determination convergence point, webhook expect_status, pass-through chain);
5. Migration cleanup (table renames / column drops / orphan-table DROPs; fill in channel/remediation coverage on the `tickraft migrate` CLI path);
6. Frontend dual-mode editor + `/expr/validate` integration + i18n;
7. tickraft-x synchronized updates (per the Chapter 12 checklist, in parallel with or immediately after CE steps 2-6, merged in the same batch);
8. Full `go build ./...` / `go vet` / `go test ./...` / frontend build / smoke tests.

### 11.3 Explicit Non-Goals

- No grand-unified rule engine;
- Do not merge the two rule tables;
- Scheduled tasks do not open up `expression` (registration system; enable when needed);
- The hardcoded Device-handler thresholds on the telemetry passive path stay untouched (active checks can already express the same semantics with `expression`; aligning the passive path is left for later);
- No custom functions introduced at all (see Chapter 5's admission bar).

---

## 12. tickraft-x Synchronization Design

tickraft is tickraft-x's infrastructure. This revamp is **not a CE-only change**: the x side has direct references to CE's rule mechanism, dependencies on implicit conventions, and one **isomorphically copied parallel model** (x's remediation `Rule` copied CE's model fields instead of embedding them; same table name `sys_prism_remediation_rule`, different trigger enums, its own `EventContext` evaluation environment). This chapter gives x's synchronization plan, implemented in the same batch as the CE steps.

### 12.1 Inventory of x's Affected Surface

| x-side location | Role | Affected points |
|---|---|---|
| `internal/api/service_alert_prism.go` | alert rule CRUD (a local copy of CE's AlertService, because x cannot import CE internal/) | Create strictly validates `Scene` as non-empty (L68); DTO mapping contains Scene (L163-186) — must sync after the scene column is dropped |
| `internal/grpc/alert_service.go` | gRPC versioned rule pulling (remote source for Worker/Prism) | the proto `AlertRule` message does not carry `Expression` (comment pending) — the worker side can only project fully after the field is added |
| `internal/service/worker/strategy_standalone.go` / `strategy_distributed.go` | worker rule sources (DB polling / gRPC+PubSub) | the `recordsToRules` projection drops the expression; the **implicit scene convention** "empty Metric = log rule, Description as keyword" |
| `internal/service/worker/analyzer.go` | worker alert analyzer | `matchMetricRules`/`matchLogRules` structured matching is coupled to the implicit conventions (see 12.3) |
| `internal/service/server/syncer.go` / `distributed.go` | distributed server: Redis Hash full/incremental sync, gRPC registration | Record JSON serialization follows the schema automatically; `AutoMigrate(&rule.Record{})` follows the table rename |
| `internal/middleware/group_interceptor.go` | resource-group isolation interceptor | hardcoded table name `sys_prism_rule` → change to `sys_prism_alert_rule` |
| `internal/service/prism/prism.go` (`BuildRuleConfig`) | Prism-role rule engine assembly | depends on `rule.Config/CompilerConfig`; import path migrates per 2.2; the comparison-count entitlement clause is removed (12.5) |
| `internal/prism/remediation/` | **x's self-built remediation engine** (its own `EventContext`/`compileCondition`/`buildExprEnv`/DryRun) | everything converges onto the CE kernel and contracts (12.4) |
| `web/src/api/rule-full.ts` + `views/prism/rule/edit/EditFull.vue` | x frontend structured condition editor (severity/logic/conditions) | schema mismatch with CE's `expression` model — unified per CE's dual-mode design (9.1, 12.6) |

### 12.2 Alert Rule Chain Synchronization

1. **CRUD copy update** (`service_alert_prism.go`): delete the Scene validation and the Scene field in DTO mappings; `prismRuleHandlerToModel` fills in TenantID/GroupID/Metadata carriage (aligned with CE's post-D-02-fix full-field DTOs); pass the real engine at construction so post-CRUD `Reload` takes effect (currently nil, a no-op — or switch to ConfigBus notifications to the Prism role).
2. **gRPC proto**: add `string expression = n;` to the `AlertRule` message (versioned pulling is ready; only the field is missing); `computeAlertRuleVersion` unchanged.
3. **syncer/interceptor**: `GroupInterceptor`'s table-name list changes to `sys_prism_alert_rule`; the Redis Hash and Pub/Sub channels have no schema dependency and follow automatically.
4. **Migration order**: x's `runAllMigrations` runs CE core-table AutoMigrate first, then x Feature Migrate (the existing comment makes this order explicit); after CE's table rename, the x side needs no extra handling.

### 12.3 Worker Rule Analyzer Consolidation (Implicit Scene Convention Deleted)

As-was: the worker projected `rule.Record` into a local `Rule` view (`recordsToRules` carried only ID/Name/Description/Enabled/UpdatedAt), and the analyzer distinguished scenes via **"an empty Metric field means a log rule + Description as keyword"** — an implicit coupling to scene that also lost the expression.

Final shape: the worker reuses CE's matching engine, and the local projection and dual-path matching are deleted entirely:

- The standalone/distributed rule sources hold the single model `alert.Rule` directly (distributed side transmits all fields over the gRPC proto; standalone side only filters by enabled, decoding metadata on demand via `Rule.MetadataMap()`);
- The analyzer builds an `AlertEnv` for the event and evaluates once via the CE engine's `Evaluate` — **no scene-distinguishing field is needed at all**: the variables an expression references naturally distinguish scenes (a log event's `metrics` is an empty map and `content` has a value, so a metric rule simply does not match), same semantics as the CE engine;
- The worker's local `Rule` view (strategy.go's zero-valued Metric/Condition/Threshold fields), the `recordsToRules`/`parseRuleMetadata` local projections, and the `matchMetricRules`/`matchLogRules` dual paths are all deleted.

### 12.4 x Remediation Engine Consolidation (Parallel Implementation Eliminated)

x's `internal/prism/remediation` is an **isomorphic copy + extension** of CE's same-named module (copied model and `EventContext`, plus VerifyProbe post-fix verification, DryRun online trial runs, more executor types, fault-event triggers, strict tenant isolation). Synchronization plan:

1. **Model switches to embedding**: x's `Rule` embeds CE's `pkg/prism/remediation.Rule` + x-specific columns (`verify_probe`/`verify_enabled`), eliminating the field copying — exactly the extension approach CE's remediation doc.go declares ("Extended editions embed this model...sharing the same table"), which x had not implemented before. CE's `condition_expr` → `expression` rename and the new `consecutive_failures` column are inherited automatically through embedding (x deletes its own `ConditionExpr` definition).
2. **Evaluation environment alignment**: delete x's `EventContext` and switch to a superset of the 4.2-contract `RemediationEnv` (+ the 4.5 extension fields `fault.type`/`client.id`/`payload`); the field rename mapping is in 4.5.
3. **Compile-and-evaluate consolidation**: `compileCondition`/`evaluateCondition`/`buildExprEnv` and DryRun (handler.go L767-800) all switch to CE's `pkg/expr`; after the migration **x's direct imports of expr-lang drop to zero** (currently just two places: manager.go and handler.go).
4. **Unified trigger enums**: `metric_alert → metric`, `log_alert → log`; `status_change` unchanged; `fault_event` retained as an x extension value (3.7).
5. **Circuit-breaker counter migration**: x currently uses `metadata.consecutive_triggers`, migrated together with CE's standalone `consecutive_failures` column (atomic SQL, see 6.2.2).
6. **x-specific capabilities retained**: VerifyProbe post-fix verification (the verification probe's config JSON likewise supports the optional `expression` key, determined through the same `ExecutionEnv` channel, same mechanism as 6.3), DryRun (capability unchanged after the kernel switch), extended executor types (ssh/mysql/redis/snmp/k8s/mqtt — x-side implementations; the `Metadata["expression"]` determination channel applies to them too), rule-level strict tenant isolation, and AI AlertLevel classification (`internal/executor/ai/classify.go`, complementary to boolean determination, no conflict).

### 12.5 Compiler Configuration Unification (the former comparison-count entitlement disappears with the constraint's removal)

The comparison-count cap is removed wholesale in this design (3.3), so x's `CompilerConfig{MaxComparisons: -1}` differentiated entitlement no longer exists and `BuildRuleConfig` simplifies to the default configuration — **compilation semantics are fully identical across the two repos** (one kernel, one constraint set, all builtins open). x's differentiated capabilities move to more appropriate layers: ENV extension variables (4.5), executor type extensions, VerifyProbe/DryRun.

`EvalInterval` (5-minute fallback polling) and `ReloadSubscriber` (ConfigBus real-time notifications) are retained — x is already wired correctly, and CE's D-03 fix simply aligns with that usage.

### 12.6 x Frontend Synchronization

1. **Structured editor unified with CE's dual mode**: `rule-full.ts`'s `AlertRuleFullPayload{logic, conditions[...]}` is the x variant of CE 9.1's wizard mode — on submit it is compiled into a canonical expression per the 9.2 mapping table and stored in the `expression` column, making `expression` the single source of truth and eliminating the frontend/backend schema mismatch; the mapping semantics of the `duration` window field is a leftover decision point (13.3) and may go unmapped and temporarily hidden in the first release; reverse parsing reuses CE's `expr-builder` frontend module (one-way CE → x copy or promoted to a shared web package, decided at implementation time).
2. **Remediation frontend**: `api/prism/remediation.ts`'s interface types align with backend fields (`trigger_event_type`, `expression` (trigger condition, renamed from `conditionExpr`), `executor_config` (with the optional `expression` execution-determination key)); remove the mock-driven old schema; keep the DryRun entry point (its validation kernel shares the same source as `/expr/validate`, 8.4).
3. i18n additions/removals synced with CE (scene-related keys deleted; dual-mode and expression-related keys added).

### 12.7 ENV Contract Anti-Drift Mechanism

The two repos define their structs independently (3.7 item 2); consistency is **guarded by tests**:

- The x side adds contract-consistency unit tests: taking the **variable path set** expanded at runtime from CE's `pkg/prism/remediation.ExampleEnv()` (`trigger`, `asset.id`, `status.current`…) as the baseline, assert that the paths expanded from x's `RemediationEnv` sample ⊇ the baseline (map-carried domain objects can likewise have their key sets expanded at runtime); x superset fields are checked against the 4.5 registry;
- CE-side contract changes must update this document's Chapter 4 and the 4.5 registry in sync, and x-side tests follow — the document is the sole authority.

### 12.8 x Synchronization Acceptance Checklist

- [ ] `service_alert_prism.go` has no Scene dependency and CRUD carries all fields;
- [ ] the proto `AlertRule` carries `expression` and the distributed worker rule projection is complete;
- [ ] the worker analyzer has no implicit scene convention and uniformly goes through `AlertEnv` + `pkg/expr`;
- [ ] `GroupInterceptor`'s table names are updated;
- [ ] x's remediation model embeds the CE model, `EventContext` is deleted, and evaluation goes through `pkg/expr`;
- [ ] the trigger-condition field is `expression` and `condition_expr` is deleted;
- [ ] trigger enum values are `metric`/`log`/`status_change`/`fault_event`;
- [ ] x's direct expr-lang imports are 0;
- [ ] contract-consistency unit tests pass (path-set comparison);
- [ ] DryRun/VerifyProbe behavior regressions pass;
- [ ] x's frontend structured editor is unified with CE's dual mode (submits as `expression`), and the remediation frontend schema is aligned.

---

## 13. Requirements Coverage Assessment (tickraft + tickraft-x)

Item-by-item verification of how this design covers the current and established rule-related requirements of both repos.

### 13.1 tickraft (CE)

| Requirement | Design coverage | Status |
|---|---|---|
| Rule filtering of the three alert event kinds: metric/log/status | 4.1 AlertEnv + 6.1 | covered |
| Rule hot reload (API-triggered + DB polling fallback) | 6.1.6 | covered |
| Remediation triggering (type + condition + asset enrichment) | 4.2 + 6.2.1 | covered |
| Remediation gating (idempotency/cooldown/circuit breaker) and concurrency safety | 6.2.2 | covered |
| Configurable execution success (HTTP status / exit code / body content / elapsed time) | 3.4 + 6.3 | covered |
| Misconfiguration protection (bad expressions blocked at entry, domain-field misspellings blocked) | 3.6 + 8.4 | covered |
| expr-lang-savvy users get full standard capabilities | 3.3 + 5.2 (all builtins open) | covered |
| Interactive rule building for users unfamiliar with expr | 9.1 wizard mode | covered |
| Multi-tenancy (CE runs single-tenant; fields and filtering reserved) | 6.1.5 | covered (reserved) |
| Evaluation performance (program caching, evaluation outside locks) | 2.1 + 6.2.1 | covered |
| Scheduled-task exit-code determination | 6.3.3 closing note | explicitly not done (registration system) |
| Threshold handling on the passive reporting path | 11.3 | explicitly not done (aligned later) |

### 13.2 tickraft-x

| Requirement | Design coverage | Status |
|---|---|---|
| Distributed rule sync completeness (expression transmission) | 12.2 proto field addition | covered |
| Worker alert analysis with the same semantics as CE | 12.3 | covered |
| Fault-event remediation (fault_event trigger and extension variables) | 4.5 extension registry + 12.4 | covered |
| Post-fix verification (VerifyProbe) | 12.4 item 6 | covered |
| Online trial runs (DryRun) | 12.4 (switches to the `pkg/expr` kernel, same source as 8.4) | covered |
| Structured frontend editing | 9.1 wizard mode + 12.6 | covered |
| Strict tenant isolation | 12.4 item 6 retained | covered |
| AI AlertLevel classification coexists with boolean determination | 12.4 item 6 | covered (complementary) |
| Success/failure determination for extended executor types (ssh/mysql/redis/snmp/k8s/mqtt) | the `Metadata["expression"]` channel applies to all executors (6.3.3) | covered |
| Comparison-count lifting entitlement | 3.3 removes the cap, unified across both repos | covered (the entitlement becomes a general capability) |

### 13.3 Assessment Conclusion and Leftover Decision Points

Conclusion: **all currently known requirements of both repos are covered**; the uncovered items are all "explicitly not done / reserved" and registered (11.3 and the last table rows), with no architecture-level gap.

Leftover decision points (decided at implementation time, no architectural impact):

1. The subset scope of the wizard reverse parser (9.2) — the first release may support only single-level AND/OR combinations;
2. The mapping semantics of `EditFull`'s `duration` window field into expressions (12.6) — the first release may leave it unmapped and hide it in the frontend;
3. How the `expr-builder` frontend module is shared between the two repos (copy or shared package).
