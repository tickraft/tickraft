// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package rule

import (
	"context"
	"fmt"
	"strconv"

	"github.com/tickraft/tickraft/pkg/asset"
	"github.com/tickraft/tickraft/pkg/event"
	"github.com/tickraft/tickraft/pkg/prism/alert"
	"github.com/tickraft/tickraft/pkg/task"
	"github.com/tickraft/tickraft/pkg/telemetry"
	"github.com/tickraft/tickraft/pkg/types"
	"go.uber.org/zap"
)

// Compile-time assertions that the adapters satisfy the
// interfaces they extend. Failures surface at build time
// rather than at registration time.
var (
	_ telemetry.Processor    = (*ProbeMatcher)(nil)
	_ alert.Matcher          = (*MetricMatcher)(nil)
	_ alert.ViolationMatcher = (*MetricMatcher)(nil)
)

// TaskMatcher adapts the engine's task-scene evaluation to the
// scheduler domain. It is constructed once and invoked per task
// dispatch decision.
type TaskMatcher struct {
	engine *Engine
}

// NewTaskMatcher creates a TaskMatcher backed by the supplied engine.
func NewTaskMatcher(engine *Engine) *TaskMatcher {
	return &TaskMatcher{engine: engine}
}

// Match projects the scheduler task, asset, and tags into a
// TaskMatchEnv and returns the IDs of the matching task-scene rules.
// A nil tags map is tolerated: expr-lang field access on a nil map
// yields the zero value of the value type.
//
// The projected TaskView is mirrored into the env's Event alias so
// rule authors can reference the task as either "task" or "event".
func (m *TaskMatcher) Match(
	ctx context.Context,
	tk task.Task,
	res asset.Asset,
	tags map[string]string,
) ([]int64, error) {
	taskView := toTaskView(tk)
	env := TaskMatchEnv{
		Task:  taskView,
		Event: taskView,
		Asset: toAssetView(res),
		Tags:  tags,
	}
	return m.engine.MatchTask(ctx, env), nil
}

// ProbeMatcher adapts the engine's probe-scene evaluation to the
// telemetry.Processor interface. It is registered as a generic
// processor (Type returns "") so it does not collide with the
// default Device processor or other type-specific processors.
type ProbeMatcher struct {
	engine *Engine
	store  asset.Store
	bus    event.Bus
	logger *zap.Logger
}

// NewProbeMatcher creates a ProbeMatcher. A nil logger is replaced
// with a no-op logger so callers never need to nil-check.
func NewProbeMatcher(
	engine *Engine,
	store asset.Store,
	bus event.Bus,
	logger *zap.Logger,
) *ProbeMatcher {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &ProbeMatcher{
		engine: engine,
		store:  store,
		bus:    bus,
		logger: logger,
	}
}

// Type returns the empty asset type, marking the ProbeMatcher as a
// generic processor that participates in every report dispatch
// regardless of asset type.
func (m *ProbeMatcher) Type() types.AssetType {
	return ""
}

// Process applies probe-scene rules to the report and emits an
// AlertContext for each matching rule set. The Result view is left
// zero-valued because the telemetry.Processor contract only carries
// the Report; the Asset view is populated via the asset
// store when available.
func (m *ProbeMatcher) Process(
	ctx context.Context,
	report *telemetry.Telemetry,
) (*telemetry.ProcessResult, error) {
	if report == nil {
		return nil, fmt.Errorf("probe_matcher: report is nil")
	}

	reportView := toReportView(*report)
	env := ProbeMatchEnv{
		Report: reportView,
		Event:  reportView,
	}
	if m.store != nil {
		if res, err := m.store.GetByID(ctx, report.AssetID); err == nil && res != nil {
			env.Asset = toAssetView(*res)
		} else if err != nil {
			m.logger.Debug("probe matcher asset lookup failed",
				zap.Int64("asset_id", report.AssetID),
				zap.Error(err))
		}
	}

	matched := m.engine.MatchProbe(ctx, env)
	alerts := buildProbeAlerts(matched)
	reason := fmt.Sprintf("probe_matcher: evaluated probe rules, matched=%d", len(matched))

	return &telemetry.ProcessResult{
		CurrStatus: types.AssetStatusNormal,
		Reason:     reason,
		Alerts:     alerts,
	}, nil
}

// OnTimeout delegates to telemetry.MarkOffline so timeout semantics
// stay consistent with the other processors
// (MetricThreshold, LogAlert).
func (m *ProbeMatcher) OnTimeout(ctx context.Context, assetID int64) error {
	return telemetry.MarkOffline(
		ctx, m.store, m.bus, m.logger,
		assetID, m.Type(),
		"probe_matcher: asset timeout",
	)
}

// buildProbeAlerts converts the matched rule ID slice into a single
// AlertContext describing the match. The IDs are also projected into
// the Metadata map so downstream consumers can correlate back to the
// originating rules without parsing the message.
func buildProbeAlerts(matched []int64) []telemetry.AlertContext {
	if len(matched) == 0 {
		return nil
	}
	metadata := make(map[string]string, len(matched)+1)
	for _, id := range matched {
		idStr := strconv.FormatInt(id, 10)
		metadata["rule_id_"+idStr] = "true"
	}
	metadata["rule_ids"] = strconv.FormatInt(int64(len(matched)), 10)

	return []telemetry.AlertContext{{
		Level:    "warning",
		Title:    "Probe Rule Matched",
		Message:  fmt.Sprintf("probe_matcher: %d rule(s) matched, ids=[%s]", len(matched), joinInt64(matched, ",")),
		Metadata: metadata,
	}}
}

// joinInt64 concatenates the string form of each int64 in ids with
// sep. It avoids pulling in strings.Join for a single call site and
// keeps the matcher self-contained.
func joinInt64(ids []int64, sep string) string {
	if len(ids) == 0 {
		return ""
	}
	out := strconv.FormatInt(ids[0], 10)
	for _, id := range ids[1:] {
		out += sep + strconv.FormatInt(id, 10)
	}
	return out
}

// MetricMatcher adapts the engine's metric-scene evaluation to the
// alert.Matcher interface, acting as a pre-filter for alert dispatch.
// Injected into alert.Engine.AddRule, it returns true (forward)
// when at least one metric-scene rule matches, false (suppress)
// otherwise. An empty metric-scene rule set returns true so the
// matcher never blocks alerts when no rules are configured.
type MetricMatcher struct {
	engine *Engine
	store  asset.Store
}

// NewMetricMatcher creates a MetricMatcher backed by the supplied
// engine and optional asset store. The store is used to enrich
// the evaluation environment with the asset associated with the
// alert; a nil store leaves the Asset view zero-valued.
func NewMetricMatcher(engine *Engine, store asset.Store) *MetricMatcher {
	return &MetricMatcher{engine: engine, store: store}
}

// Match implements alert.Matcher. It projects the alert into a
// MetricMatchEnv and returns true when at least one metric-scene rule
// matches. When the metric scene has no loaded rules, Match returns
// true so default-allow alert dispatch semantics are preserved.
//
// Match uses the fast MatchMetric path (rule IDs only) so the
// alert engine dispatch path does not pay the AST-parse
// cost of violation extraction. Callers that need structured
// violations should use MatchWithViolations instead.
func (m *MetricMatcher) Match(ctx context.Context, evt alert.Event) bool {
	alertView := toAlertView(evt)
	env := MetricMatchEnv{
		Alert: alertView,
		Event: alertView,
	}
	if m.store != nil {
		if res, err := m.store.GetByID(ctx, evt.AssetID); err == nil && res != nil {
			env.Asset = toAssetView(*res)
		}
	}
	matched := m.engine.MatchMetric(ctx, env)
	// Default-allow semantics: when no metric-scene rules are loaded
	// the matcher forwards every alert so the rule engine never
	// silently drops alerts simply because no rules are configured.
	// When rules exist, the alert is forwarded only if at least one
	// rule matches (per design doc chapter 7.6).
	if !m.engine.HasMetricRules() {
		return true
	}
	return len(matched) > 0
}

// MatchWithViolations implements alert.ViolationMatcher. It evaluates the
// metric-scene rules and returns all violations for the matched
// comparison sub-conditions, so a compound rule such as
// `alert.metrics["cpu"] > 90 && alert.metrics["mem"] > 85`
// contributes one Violation per matched condition.
//
// When no metric-scene rules are loaded, MatchWithViolations returns nil
// (default-allow): the alert is forwarded by Match without structured
// violations.
func (m *MetricMatcher) MatchWithViolations(ctx context.Context, evt alert.Event) []alert.Violation {
	alertView := toAlertView(evt)
	env := MetricMatchEnv{
		Alert: alertView,
		Event: alertView,
	}
	if m.store != nil {
		if res, err := m.store.GetByID(ctx, evt.AssetID); err == nil && res != nil {
			env.Asset = toAssetView(*res)
		}
	}
	if !m.engine.HasMetricRules() {
		return nil
	}
	return m.engine.MatchMetricWithViolations(ctx, env)
}
