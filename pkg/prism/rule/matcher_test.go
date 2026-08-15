// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package rule

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/tickraft/tickraft/pkg/asset"
	"github.com/tickraft/tickraft/pkg/event"
	"github.com/tickraft/tickraft/pkg/pagination"
	a "github.com/tickraft/tickraft/pkg/prism/alert"
	"github.com/tickraft/tickraft/pkg/task"
	"github.com/tickraft/tickraft/pkg/telemetry"
	"github.com/tickraft/tickraft/pkg/types"
	"go.uber.org/zap"
)

// Compile-time interface assertions mirror the ones in matcher.go so a
// refactoring that breaks the contract surfaces as a test-time compile
// failure in addition to the package-level assertion.
var (
	_ telemetry.Processor = (*ProbeMatcher)(nil)
	_ a.Matcher           = (*MetricMatcher)(nil)
)

// stubAssetStore is a minimal asset.Store double. It records every
// UpdateStatus call so ProbeMatcher.OnTimeout can verify that MarkOffline
// transitioned the asset to StatusOffline. GetByID returns the configured
// asset (or the configured err) so Process can exercise both the hit and
// miss paths of the asset-enrichment branch.
type stubAssetStore struct {
	mu     sync.Mutex
	asset  *asset.Asset
	getErr error

	updatedID     int64
	updatedStatus types.AssetStatus
	updatedActive time.Time
	updateErr     error
}

func (s *stubAssetStore) CountByStatus(_ context.Context) (map[string]int64, error) {
	return map[string]int64{}, nil
}

func (s *stubAssetStore) Create(context.Context, *asset.Asset) error {
	return nil
}
func (s *stubAssetStore) Update(context.Context, *asset.Asset) error { return nil }
func (s *stubAssetStore) GetByID(_ context.Context, _ int64) (*asset.Asset, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	return s.asset, nil
}
func (s *stubAssetStore) GetByKey(context.Context, int64, string) (*asset.Asset, error) {
	return nil, errors.New("not implemented")
}
func (s *stubAssetStore) UpdateStatus(_ context.Context, id int64, status types.AssetStatus, activeAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.updateErr != nil {
		return s.updateErr
	}
	s.updatedID = id
	s.updatedStatus = status
	s.updatedActive = activeAt
	return nil
}
func (s *stubAssetStore) Migrate(context.Context) error { return nil }
func (s *stubAssetStore) List(context.Context, int, int, asset.ListFilter) ([]*asset.Asset, int64, error) {
	return nil, 0, nil
}
func (s *stubAssetStore) ListKeyset(_ context.Context, _ pagination.PageRequest) (pagination.PageResult[*asset.Asset], error) {
	return pagination.PageResult[*asset.Asset]{}, nil
}
func (s *stubAssetStore) Delete(context.Context, int64) error { return nil }
func (s *stubAssetStore) CountByType(context.Context, int64, types.AssetType) (int64, error) {
	return 0, nil
}
func (s *stubAssetStore) ExistsByKey(context.Context, string) (bool, error) { return false, nil }

// statusUpdateSnapshot copies the recorded UpdateStatus call under the lock so
// concurrent assertions do not race with a subsequent update.
func (s *stubAssetStore) statusUpdateSnapshot() (int64, types.AssetStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.updatedID, s.updatedStatus
}

// ---------------------------------------------------------------------------
// TaskMatcher
// ---------------------------------------------------------------------------

// TestTaskMatcher_Match verifies that TaskMatcher projects task.Task +
// asset.Asset + tags into a TaskMatchEnv and returns the IDs of the
// matching task-scene rules.
func TestTaskMatcher_Match(t *testing.T) {
	ctx := context.Background()
	eng := NewEngine(zap.NewNop())
	rules := []Rule{
		{ID: 1, Name: "high-priority", Scene: SceneTask, Expression: "task.priority > 5", Priority: 10, Enabled: true},
		{ID: 2, Name: "ssh-executor", Scene: SceneTask, Expression: `task.executor_type == "ssh"`, Priority: 5, Enabled: true},
		{ID: 3, Name: "tag-region", Scene: SceneTask, Expression: `tags["region"] == "cn-east-1"`, Priority: 1, Enabled: true},
	}
	if err := eng.Load(ctx, rules); err != nil {
		t.Fatalf("Load: %v", err)
	}

	matcher := NewTaskMatcher(eng)

	tk := task.Task{
		ID:           100,
		TenantID:     1,
		AssetID:      42,
		ExecutorName: "ssh",
		Priority:     8,
		Timeout:      30 * time.Second,
		Metadata:     map[string]string{"owner": "ops"},
	}
	res := asset.Asset{
		ID:        42,
		TenantID:  1,
		AssetType: types.AssetTypeHost,
		Name:      "my-host",
		Status:    types.AssetStatusNormal,
	}
	tags := map[string]string{"region": "cn-east-1"}

	matched, err := matcher.Match(ctx, tk, res, tags)
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	// All three rules match the constructed env.
	want := map[int64]bool{1: true, 2: true, 3: true}
	if len(matched) != len(want) {
		t.Fatalf("Match = %v, want %d ids", matched, len(want))
	}
	for _, id := range matched {
		if !want[id] {
			t.Errorf("unexpected match id %d", id)
		}
	}
}

// TestTaskMatcher_Match_NilTags verifies that a nil tags map is tolerated:
// expr-lang field access on a nil map yields the zero value of the value type.
func TestTaskMatcher_Match_NilTags(t *testing.T) {
	ctx := context.Background()
	eng := NewEngine(zap.NewNop())
	if err := eng.Load(ctx, []Rule{
		{ID: 1, Scene: SceneTask, Expression: `tags["region"] == ""`, Enabled: true},
	}); err != nil {
		t.Fatalf("Load: %v", err)
	}
	matcher := NewTaskMatcher(eng)

	matched, err := matcher.Match(ctx, task.Task{ID: 1}, asset.Asset{}, nil)
	if err != nil {
		t.Fatalf("Match nil tags: %v", err)
	}
	if len(matched) != 1 || matched[0] != 1 {
		t.Errorf("Match = %v, want [1] (nil map yields empty string)", matched)
	}
}

// TestTaskMatcher_Match_NoRules verifies that an engine with no task rules
// returns an empty slice (and no error) instead of panicking.
func TestTaskMatcher_Match_NoRules(t *testing.T) {
	ctx := context.Background()
	eng := NewEngine(zap.NewNop())
	matcher := NewTaskMatcher(eng)

	matched, err := matcher.Match(ctx, task.Task{ID: 1}, asset.Asset{}, nil)
	if err != nil {
		t.Fatalf("Match no rules: %v", err)
	}
	if len(matched) != 0 {
		t.Errorf("Match = %v, want empty", matched)
	}
}

// ---------------------------------------------------------------------------
// ProbeMatcher
// ---------------------------------------------------------------------------

// TestProbeMatcher_Type verifies that ProbeMatcher.Type returns the empty
// asset type, marking it as a generic processor that participates in every
// report dispatch regardless of asset type.
func TestProbeMatcher_Type(t *testing.T) {
	matcher := NewProbeMatcher(NewEngine(zap.NewNop()), nil, nil, zap.NewNop())
	if got := matcher.Type(); got != "" {
		t.Errorf("Type = %q, want empty string", got)
	}
}

// TestProbeMatcher_Process_HitAndMiss verifies that Process emits an
// AlertContext when a probe-scene rule matches the report and returns empty
// Alerts when no rule matches.
func TestProbeMatcher_Process_HitAndMiss(t *testing.T) {
	ctx := context.Background()
	eng := NewEngine(zap.NewNop())
	if err := eng.Load(ctx, []Rule{
		{ID: 10, Name: "log-error", Scene: SceneProbe, Expression: `report.log_content contains "error"`, Priority: 5, Enabled: true},
	}); err != nil {
		t.Fatalf("Load: %v", err)
	}
	matcher := NewProbeMatcher(eng, nil, nil, zap.NewNop())

	// Hit: LogContent contains "error" → 1 alert emitted.
	hitReport := &telemetry.Telemetry{
		AssetID:     200,
		TenantID:    1,
		AssetType:   types.AssetTypeHost,
		SourceType:  "syslog",
		CollectedAt: time.Now(),
		LogContent:  "disk error on /dev/sda1",
		LogLevel:    "ERROR",
	}
	hitResult, err := matcher.Process(ctx, hitReport)
	if err != nil {
		t.Fatalf("Process hit: %v", err)
	}
	if hitResult == nil {
		t.Fatal("expected non-nil ProcessResult for hit report")
	}
	if hitResult.CurrStatus != types.AssetStatusNormal {
		t.Errorf("CurrStatus = %q, want %q", hitResult.CurrStatus, types.AssetStatusNormal)
	}
	if len(hitResult.Alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(hitResult.Alerts))
	}
	alertCtx := hitResult.Alerts[0]
	if alertCtx.Level != "warning" {
		t.Errorf("alert Level = %q, want warning", alertCtx.Level)
	}
	if alertCtx.Metadata["rule_id_10"] != "true" {
		t.Errorf("expected metadata rule_id_10=true, got %v", alertCtx.Metadata)
	}
	if alertCtx.Metadata["rule_ids"] != "1" {
		t.Errorf("expected metadata rule_ids=1, got %v", alertCtx.Metadata["rule_ids"])
	}

	// Miss: clean LogContent → no alerts.
	missReport := &telemetry.Telemetry{
		AssetID:     200,
		TenantID:    1,
		AssetType:   types.AssetTypeHost,
		SourceType:  "syslog",
		CollectedAt: time.Now(),
		LogContent:  "all good, system healthy",
		LogLevel:    "INFO",
	}
	missResult, err := matcher.Process(ctx, missReport)
	if err != nil {
		t.Fatalf("Process miss: %v", err)
	}
	if missResult == nil {
		t.Fatal("expected non-nil ProcessResult for miss report")
	}
	if len(missResult.Alerts) != 0 {
		t.Errorf("expected 0 alerts for clean report, got %d", len(missResult.Alerts))
	}
}

// TestProbeMatcher_Process_NilReport verifies that Process returns an error
// when the report is nil rather than panicking.
func TestProbeMatcher_Process_NilReport(t *testing.T) {
	matcher := NewProbeMatcher(NewEngine(zap.NewNop()), nil, nil, zap.NewNop())
	result, err := matcher.Process(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil report, got nil")
	}
	if result != nil {
		t.Errorf("expected nil result for nil report, got %+v", result)
	}
}

// TestProbeMatcher_Process_AssetEnrichment verifies that when an asset
// store is configured, Process populates the Asset view via GetByID so
// rules referencing Asset.* fields can match.
func TestProbeMatcher_Process_AssetEnrichment(t *testing.T) {
	ctx := context.Background()
	eng := NewEngine(zap.NewNop())
	if err := eng.Load(ctx, []Rule{
		{ID: 1, Name: "host-name-rule", Scene: SceneProbe, Expression: `asset.name == "enriched-host"`, Enabled: true},
	}); err != nil {
		t.Fatalf("Load: %v", err)
	}

	store := &stubAssetStore{asset: &asset.Asset{
		ID:        200,
		TenantID:  1,
		AssetType: types.AssetTypeHost,
		Name:      "enriched-host",
		Status:    types.AssetStatusNormal,
	}}
	matcher := NewProbeMatcher(eng, store, nil, zap.NewNop())

	report := &telemetry.Telemetry{AssetID: 200, TenantID: 1, CollectedAt: time.Now()}
	result, err := matcher.Process(ctx, report)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if len(result.Alerts) != 1 {
		t.Errorf("expected 1 alert with enriched asset.name, got %d", len(result.Alerts))
	}
}

// TestProbeMatcher_Process_AssetLookupError verifies that a GetByID error
// is logged but does not abort Process: the rule set still evaluates against
// the zero-valued Asset view.
func TestProbeMatcher_Process_AssetLookupError(t *testing.T) {
	ctx := context.Background()
	eng := NewEngine(zap.NewNop())
	if err := eng.Load(ctx, []Rule{
		// The rule references no Asset fields, so a missing Asset view
		// does not affect the outcome.
		{ID: 1, Name: "any-report", Scene: SceneProbe, Expression: `report.log_level == "ERROR"`, Enabled: true},
	}); err != nil {
		t.Fatalf("Load: %v", err)
	}
	store := &stubAssetStore{getErr: errors.New("db unavailable")}
	matcher := NewProbeMatcher(eng, store, nil, zap.NewNop())

	report := &telemetry.Telemetry{AssetID: 999, TenantID: 1, LogLevel: "ERROR", CollectedAt: time.Now()}
	result, err := matcher.Process(ctx, report)
	if err != nil {
		t.Fatalf("Process with failing store: %v", err)
	}
	if len(result.Alerts) != 1 {
		t.Errorf("expected 1 alert despite GetByID error, got %d", len(result.Alerts))
	}
}

// TestProbeMatcher_OnTimeout verifies that OnTimeout delegates to
// telemetry.MarkOffline: the asset store's UpdateStatus is invoked with
// types.AssetStatusOffline, and the event bus receives a StatusChange event.
func TestProbeMatcher_OnTimeout(t *testing.T) {
	ctx := context.Background()
	eng := NewEngine(zap.NewNop())

	store := &stubAssetStore{asset: &asset.Asset{
		ID:        500,
		TenantID:  1,
		AssetType: types.AssetTypeHost,
		Name:      "timed-out-host",
		Status:    types.AssetStatusNormal,
	}}
	bus := event.NewBus()
	defer bus.Close()

	var (
		busMu      sync.Mutex
		gotPayload event.StatusChangePayload
		gotCount   int
	)
	sub, err := event.Subscribe[event.StatusChangePayload](bus, event.TypeAssetStatusChanged, func(_ context.Context, e event.Event[event.StatusChangePayload]) error {
		busMu.Lock()
		defer busMu.Unlock()
		gotPayload = e.Payload
		gotCount++
		return nil
	})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer sub.Cancel()

	matcher := NewProbeMatcher(eng, store, bus, zap.NewNop())
	if err := matcher.OnTimeout(ctx, 500); err != nil {
		t.Fatalf("OnTimeout: %v", err)
	}

	// UpdateStatus called with StatusOffline.
	gotID, gotStatus := store.statusUpdateSnapshot()
	if gotID != 500 {
		t.Errorf("UpdateStatus id = %d, want 500", gotID)
	}
	if gotStatus != types.AssetStatusOffline {
		t.Errorf("UpdateStatus status = %q, want %q", gotStatus, types.AssetStatusOffline)
	}

	// Event published on the bus. The event bus is async (Task 7), so close
	// the bus to flush all in-flight events before asserting. Close is
	// idempotent and the deferred Close above is still safe.
	bus.Close()

	busMu.Lock()
	defer busMu.Unlock()
	if gotCount != 1 {
		t.Errorf("expected 1 StatusChange event, got %d", gotCount)
	}
	if gotPayload.AssetID != "500" {
		t.Errorf("payload AssetID = %q, want %q", gotPayload.AssetID, "500")
	}
	if gotPayload.PrevStatus != string(types.AssetStatusNormal) {
		t.Errorf("payload PrevStatus = %q, want %q", gotPayload.PrevStatus, types.AssetStatusNormal)
	}
	if gotPayload.CurrStatus != string(types.AssetStatusOffline) {
		t.Errorf("payload CurrStatus = %q, want %q", gotPayload.CurrStatus, types.AssetStatusOffline)
	}
}

// TestProbeMatcher_OnTimeout_NilStore verifies that OnTimeout surfaces the
// underlying MarkOffline error when no store is configured.
func TestProbeMatcher_OnTimeout_NilStore(t *testing.T) {
	matcher := NewProbeMatcher(NewEngine(zap.NewNop()), nil, nil, zap.NewNop())
	err := matcher.OnTimeout(context.Background(), 1)
	if err == nil {
		t.Fatal("expected error when store is nil, got nil")
	}
}

// TestProbeMatcher_OnTimeout_UpdateError verifies that an UpdateStatus failure
// is propagated from MarkOffline.
func TestProbeMatcher_OnTimeout_UpdateError(t *testing.T) {
	store := &stubAssetStore{
		asset:     &asset.Asset{ID: 1, Status: types.AssetStatusNormal},
		updateErr: errors.New("db write failed"),
	}
	matcher := NewProbeMatcher(NewEngine(zap.NewNop()), store, nil, zap.NewNop())
	err := matcher.OnTimeout(context.Background(), 1)
	if err == nil {
		t.Fatal("expected error from UpdateStatus failure, got nil")
	}
}

// TestProbeMatcher_NilLogger verifies that NewProbeMatcher replaces a nil
// logger with a no-op logger so callers never need to nil-check.
func TestProbeMatcher_NilLogger(t *testing.T) {
	matcher := NewProbeMatcher(NewEngine(zap.NewNop()), nil, nil, nil)
	if matcher.logger == nil {
		t.Fatal("expected non-nil logger after NewProbeMatcher(nil,...)")
	}
	// Exercise a logging code path (asset lookup failure) to ensure no
	// nil-dereference.
	ctx := context.Background()
	store := &stubAssetStore{getErr: errors.New("db unavailable")}
	matcher.store = store
	_, err := matcher.Process(ctx, &telemetry.Telemetry{AssetID: 1, CollectedAt: time.Now()})
	if err != nil {
		t.Fatalf("Process with failing store and nil logger: %v", err)
	}
}

// ---------------------------------------------------------------------------
// MetricMatcher
// ---------------------------------------------------------------------------

// metricEvent builds an alert.Event whose primary violation carries the
// supplied metric value and severity. The MetricMatcher projects the event
// into a MetricMatchEnv via toAlertView, which copies the primary violation's
// Metrics map onto AlertView.Metrics.
func metricEvent(metricName string, metricValue float64, severity string) a.Event {
	return a.Event{
		Type:      a.TypeMetric,
		AssetID:   100,
		TenantID:  1,
		Timestamp: time.Now(),
		Violations: []a.Violation{{
			Kind:     a.ViolationKindMetric,
			Severity: severity,
			Metric: &a.MetricContext{
				Name:    metricName,
				Value:   metricValue,
				Metrics: map[string]float64{metricName: metricValue},
			},
		}},
	}
}

// TestMetricMatcher_Match_HitAndMiss verifies that MetricMatcher returns true
// when at least one metric-scene rule matches and false when no rules match.
func TestMetricMatcher_Match_HitAndMiss(t *testing.T) {
	ctx := context.Background()
	eng := NewEngine(zap.NewNop())
	if err := eng.Load(ctx, []Rule{
		{ID: 1, Name: "cpu-high", Scene: SceneMetric, Expression: `alert.metrics["cpu"] > 80`, Priority: 10, Enabled: true},
	}); err != nil {
		t.Fatalf("Load: %v", err)
	}
	matcher := NewMetricMatcher(eng, nil)

	// Hit: cpu=90 > 80 → true.
	if !matcher.Match(ctx, metricEvent("cpu", 90, "critical")) {
		t.Error("expected Match=true for cpu=90, got false")
	}

	// Miss: cpu=50 < 80 → false.
	if matcher.Match(ctx, metricEvent("cpu", 50, "critical")) {
		t.Error("expected Match=false for cpu=50, got true")
	}
}

// TestMetricMatcher_Match_NoRules verifies the default-allow
// semantics of MetricMatcher when no metric-scene rules are loaded:
// Match returns true so the rule engine never silently drops alerts
// simply because no rules are configured (per design doc chapter 7.6).
func TestMetricMatcher_Match_NoRules(t *testing.T) {
	ctx := context.Background()
	eng := NewEngine(zap.NewNop())
	matcher := NewMetricMatcher(eng, nil)

	evt := metricEvent("cpu", 99, "critical")
	// Default-allow: no rules loaded → forward the alert.
	if !matcher.Match(ctx, evt) {
		t.Error("expected Match=true when no metric rules are loaded (default-allow semantics)")
	}
}

// TestMetricMatcher_Match_AssetEnrichment verifies that when an asset
// store is configured, Match populates the Asset view so rules referencing
// Asset.* fields can match.
func TestMetricMatcher_Match_AssetEnrichment(t *testing.T) {
	ctx := context.Background()
	eng := NewEngine(zap.NewNop())
	if err := eng.Load(ctx, []Rule{
		{ID: 1, Name: "host-rule", Scene: SceneMetric, Expression: `asset.name == "my-host"`, Enabled: true},
	}); err != nil {
		t.Fatalf("Load: %v", err)
	}
	store := &stubAssetStore{asset: &asset.Asset{
		ID:        100,
		TenantID:  1,
		AssetType: types.AssetTypeHost,
		Name:      "my-host",
		Status:    types.AssetStatusNormal,
	}}
	matcher := NewMetricMatcher(eng, store)

	evt := a.Event{
		Type:      a.TypeMetric,
		AssetID:   100,
		TenantID:  1,
		Timestamp: time.Now(),
	}
	if !matcher.Match(ctx, evt) {
		t.Error("expected Match=true when asset.name matches rule")
	}
}

// TestMetricMatcher_AsPrismRule verifies that MetricMatcher can be registered
// as an alert.Matcher on the open-source alert.Engine and is exposed via
// Rules().
func TestMetricMatcher_AsPrismRule(t *testing.T) {
	eng := NewEngine(zap.NewNop())
	matcher := NewMetricMatcher(eng, nil)

	target := mustNewTarget(t)
	target.AddRule(matcher)

	registered := target.Rules()
	if len(registered) != 1 {
		t.Fatalf("expected 1 rule registered, got %d", len(registered))
	}
	if _, ok := registered[0].(*MetricMatcher); !ok {
		t.Fatalf("registered rule is not *MetricMatcher, got %T", registered[0])
	}
}

// TestBuildProbeAlerts verifies the alert construction helper for the
// matched-rule-ID slice: empty input yields nil; non-empty input yields a
// single AlertContext whose Metadata records every matched rule ID.
func TestBuildProbeAlerts(t *testing.T) {
	// Empty input → nil (no alerts emitted).
	if got := buildProbeAlerts(nil); got != nil {
		t.Errorf("buildProbeAlerts(nil) = %+v, want nil", got)
	}
	if got := buildProbeAlerts([]int64{}); got != nil {
		t.Errorf("buildProbeAlerts(empty) = %+v, want nil", got)
	}

	// Non-empty input → single AlertContext with rule_id_<id> metadata.
	alerts := buildProbeAlerts([]int64{7, 42})
	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(alerts))
	}
	a := alerts[0]
	if a.Level != "warning" {
		t.Errorf("Level = %q, want warning", a.Level)
	}
	if a.Title != "Probe Rule Matched" {
		t.Errorf("Title = %q, want \"Probe Rule Matched\"", a.Title)
	}
	if a.Metadata["rule_id_7"] != "true" {
		t.Errorf("metadata rule_id_7 = %q, want true", a.Metadata["rule_id_7"])
	}
	if a.Metadata["rule_id_42"] != "true" {
		t.Errorf("metadata rule_id_42 = %q, want true", a.Metadata["rule_id_42"])
	}
	if a.Metadata["rule_ids"] != "2" {
		t.Errorf("metadata rule_ids = %q, want 2", a.Metadata["rule_ids"])
	}
}

// TestJoinInt64 verifies the joinInt64 helper used to format matched rule IDs
// in the alert message.
func TestJoinInt64(t *testing.T) {
	cases := []struct {
		name string
		ids  []int64
		sep  string
		want string
	}{
		{"empty", nil, ",", ""},
		{"single", []int64{7}, ",", "7"},
		{"multi", []int64{1, 2, 3}, ",", "1,2,3"},
		{"custom sep", []int64{1, 2}, "|", "1|2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := joinInt64(tc.ids, tc.sep); got != tc.want {
				t.Errorf("joinInt64(%v, %q) = %q, want %q", tc.ids, tc.sep, got, tc.want)
			}
		})
	}
}
