// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package rule

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tickraft/tickraft/pkg/asset"
	a "github.com/tickraft/tickraft/pkg/prism/alert"
	"github.com/tickraft/tickraft/pkg/types"
	"go.uber.org/zap"
)

// mockRuleTarget implements RuleTarget for tests without importing the prism
// package, avoiding an import cycle (prism imports rule for Register).
type mockRuleTarget struct {
	mu    sync.Mutex
	rules []a.Matcher
}

func (m *mockRuleTarget) AddRule(matcher a.Matcher) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rules = append(m.rules, matcher)
}

func (m *mockRuleTarget) Rules() []a.Matcher {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]a.Matcher(nil), m.rules...)
}

// mustNewTarget creates a mockRuleTarget for tests.
func mustNewTarget(t *testing.T) *mockRuleTarget {
	t.Helper()
	return &mockRuleTarget{}
}

// ---------------------------------------------------------------------------
// Config.IsEnabled / Config.logger
// ---------------------------------------------------------------------------

// TestConfig_IsEnabled verifies that IsEnabled returns true when at least one
// static rule is supplied or a Store is configured, and false for the zero
// value.
func TestConfig_IsEnabled(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
		want bool
	}{
		{"zero value", Config{}, false},
		{"only rules", Config{Rules: []Spec{{Name: "r", Scene: SceneMetric, Expression: `alert.metrics["cpu"] > 0`}}}, true},
		{"only store", Config{Store: &stubRuleStore{}}, true},
		{"rules and store", Config{
			Rules: []Spec{{Name: "r", Scene: SceneMetric, Expression: `alert.metrics["cpu"] > 0`}},
			Store: &stubRuleStore{},
		}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.cfg.IsEnabled(); got != tc.want {
				t.Errorf("IsEnabled = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestConfig_Logger_NilFallback verifies that a nil Logger falls back to a
// no-op logger, and a non-nil Logger is returned as-is.
func TestConfig_Logger_NilFallback(t *testing.T) {
	var zeroCfg Config
	if l := zeroCfg.logger(); l == nil {
		t.Error("expected non-nil logger for zero-value Config")
	}

	custom := zap.NewNop()
	cfg := Config{Logger: custom}
	if got := cfg.logger(); got != custom {
		t.Error("expected configured logger to be returned as-is")
	}
}

// ---------------------------------------------------------------------------
// Register: zero-config and nil-engine guard
// ---------------------------------------------------------------------------

// TestRegister_ZeroConfigIsNoOp verifies that a zero-value Config causes
// Register to return nil without touching the supplied prism Engine: no rule
// is added.
func TestRegister_ZeroConfigIsNoOp(t *testing.T) {
	target := mustNewTarget(t)

	if _, err := Register(context.Background(), target, Config{}); err != nil {
		t.Fatalf("Register zero config: %v", err)
	}
	if got := target.Rules(); len(got) != 0 {
		t.Errorf("expected no rules registered for zero config, got %d", len(got))
	}
}

// TestRegister_NilPrismEngineReturnsError verifies that an enabled Config
// with a nil prism Engine returns an error rather than panicking.
func TestRegister_NilPrismEngineReturnsError(t *testing.T) {
	cfg := Config{
		Rules: []Spec{{Name: "r", Scene: SceneMetric, Expression: `alert.metrics["cpu"] > 0`}},
	}
	if _, err := Register(context.Background(), nil, cfg); err == nil {
		t.Fatal("expected error for nil prism engine, got nil")
	}
}

// ---------------------------------------------------------------------------
// Register: static rules
// ---------------------------------------------------------------------------

// TestRegister_StaticRulesLoaded verifies that static rules are compiled and
// loaded with negative IDs (so they never collide with database-assigned IDs),
// and that the MetricMatcher is registered on the prism Engine.
func TestRegister_StaticRulesLoaded(t *testing.T) {
	target := mustNewTarget(t)

	cfg := Config{
		Logger: zap.NewNop(),
		Rules: []Spec{
			{Name: "cpu-high", Scene: SceneMetric, Expression: `alert.metrics["cpu"] > 80`, Priority: 10},
			{Name: "task-priority", Scene: SceneTask, Expression: "task.priority > 5", Priority: 5},
		},
	}
	if _, err := Register(context.Background(), target, cfg); err != nil {
		t.Fatalf("Register: %v", err)
	}

	// The MetricMatcher is registered as an alert.Matcher.
	rules := target.Rules()
	if len(rules) != 1 {
		t.Fatalf("expected 1 prism rule (MetricMatcher), got %d", len(rules))
	}
	mm, ok := rules[0].(*MetricMatcher)
	if !ok {
		t.Fatalf("registered rule is not *MetricMatcher, got %T", rules[0])
	}

	// The matcher's underlying engine holds both static rules with negative IDs.
	// Verify by matching: a metric value of 90 should hit the cpu-high rule.
	ctx := context.Background()
	if !mm.Match(ctx, metricEvent("cpu", 90, "critical")) {
		t.Error("expected Match=true for static rule cpu-high with cpu=90")
	}
	if mm.Match(ctx, metricEvent("cpu", 50, "critical")) {
		t.Error("expected Match=false for static rule cpu-high with cpu=50")
	}

	// Static rules receive negative IDs: verify via white-box inspection of the
	// engine's program cache.
	mm.engine.mu.RLock()
	cacheLen := len(mm.engine.programs)
	hasNegativeID := false
	for id := range mm.engine.programs {
		if id < 0 {
			hasNegativeID = true
			break
		}
	}
	mm.engine.mu.RUnlock()
	if cacheLen != 2 {
		t.Errorf("expected 2 cached programs (one per static rule), got %d", cacheLen)
	}
	if !hasNegativeID {
		t.Error("expected at least one negative ID in the program cache (static rules use negative IDs)")
	}
}

// TestRegister_StaticRuleCompileFailureIsolation verifies that an invalid
// static rule expression is logged and skipped while valid sibling rules load
// normally.
func TestRegister_StaticRuleCompileFailureIsolation(t *testing.T) {
	target := mustNewTarget(t)

	cfg := Config{
		Logger: zap.NewNop(),
		Rules: []Spec{
			{Name: "good", Scene: SceneMetric, Expression: `alert.metrics["cpu"] > 80`, Priority: 10},
			{Name: "bad", Scene: SceneMetric, Expression: `alert.metrics["cpu"] >`, Priority: 5}, // dangling operator
			{Name: "also-good", Scene: SceneMetric, Expression: `alert.metrics["cpu"] > 90`, Priority: 1},
		},
	}
	if _, err := Register(context.Background(), target, cfg); err != nil {
		t.Fatalf("Register: %v", err)
	}

	rules := target.Rules()
	if len(rules) != 1 {
		t.Fatalf("expected 1 prism rule, got %d", len(rules))
	}
	mm := rules[0].(*MetricMatcher)

	// Only the two good rules are in the cache.
	mm.engine.mu.RLock()
	cacheLen := len(mm.engine.programs)
	mm.engine.mu.RUnlock()
	if cacheLen != 2 {
		t.Errorf("expected 2 cached programs (bad rule skipped), got %d", cacheLen)
	}

	// cpu=85 matches good (>80) but not also-good (>90).
	ctx := context.Background()
	if !mm.Match(ctx, metricEvent("cpu", 85, "critical")) {
		t.Error("expected Match=true for cpu=85 (matches >80 rule)")
	}
}

// ---------------------------------------------------------------------------
// Register: dynamic Store
// ---------------------------------------------------------------------------

// TestRegister_StoreInitialReload verifies that when a Store is configured, the
// initial Reload replaces the static rule set with the store's enabled rules.
func TestRegister_StoreInitialReload(t *testing.T) {
	target := mustNewTarget(t)

	store := &stubRuleStore{rules: []Record{
		{ID: 100, TenantID: 1, Name: "dynamic", Scene: string(SceneMetric), Expression: `alert.metrics["cpu"] > 50`, Enabled: true, Priority: 5},
	}}
	cfg := Config{
		Logger: zap.NewNop(),
		Rules: []Spec{
			{Name: "static", Scene: SceneMetric, Expression: `alert.metrics["cpu"] > 999`, Priority: 1},
		},
		Store: store,
	}
	if _, err := Register(context.Background(), target, cfg); err != nil {
		t.Fatalf("Register: %v", err)
	}

	mm := target.Rules()[0].(*MetricMatcher)

	// The static rule (threshold 999) is replaced by the dynamic rule (threshold 50)
	// after the initial Reload. A value of 60 matches the dynamic rule but not the
	// static one.
	ctx := context.Background()
	if !mm.Match(ctx, metricEvent("cpu", 60, "critical")) {
		t.Error("expected Match=true for cpu=60 against dynamic rule (threshold 50)")
	}

	// The static rule (ID -1) must not be in the cache after Reload.
	mm.engine.mu.RLock()
	_, staticStillPresent := mm.engine.programs[-1]
	cacheLen := len(mm.engine.programs)
	mm.engine.mu.RUnlock()
	if staticStillPresent {
		t.Error("expected static rule to be replaced by store rules after initial Reload")
	}
	if cacheLen != 1 {
		t.Errorf("expected 1 cached program (dynamic only), got %d", cacheLen)
	}
}

// TestRegister_StoreReloadFailureNonFatal verifies that a Store ListEnabled
// failure during the initial Reload is non-fatal: Register returns nil and the
// previously loaded static rules remain in the engine.
func TestRegister_StoreReloadFailureNonFatal(t *testing.T) {
	target := mustNewTarget(t)

	store := &stubRuleStore{listErr: errStubStoreUnavailable}
	cfg := Config{
		Logger: zap.NewNop(),
		Rules: []Spec{
			{Name: "static", Scene: SceneMetric, Expression: `alert.metrics["cpu"] > 80`, Priority: 10},
		},
		Store: store,
	}
	if _, err := Register(context.Background(), target, cfg); err != nil {
		t.Fatalf("Register: %v", err)
	}

	mm := target.Rules()[0].(*MetricMatcher)

	// The static rule remains loaded despite the Store failure.
	ctx := context.Background()
	if !mm.Match(ctx, metricEvent("cpu", 90, "critical")) {
		t.Error("expected Match=true for static rule (Store failure should not unload it)")
	}
}

// errStubStoreUnavailable is a sentinel error used by the stub store to simulate
// a ListEnabled failure.
var errStubStoreUnavailable = newStubError("stub store unavailable")

// newStubError returns a new error with the given message. It is a helper so
// each call produces a distinct error value (no shared sentinel) which keeps
// tests that compare errors.Is robust against accidental identity.
func newStubError(msg string) error {
	return &stubError{msg: msg}
}

type stubError struct{ msg string }

func (e *stubError) Error() string { return e.msg }

// ---------------------------------------------------------------------------
// Register: EvalInterval reload loop
// ---------------------------------------------------------------------------

// countingStore is a stubRuleStore whose ListEnabled increments a counter on
// every call. It is used to verify that Register starts a periodic Reload
// goroutine when EvalInterval is positive.
type countingStore struct {
	stubRuleStore
	calls *int64
}

func (s *countingStore) ListEnabled(ctx context.Context, tenantID int64, scene Scene) ([]Record, error) {
	atomic.AddInt64(s.calls, 1)
	return s.stubRuleStore.ListEnabled(ctx, tenantID, scene)
}

// TestRegister_EvalIntervalStartsReloadLoop verifies that Register launches a
// background reload goroutine when cfg.Store is non-nil and cfg.EvalInterval is
// positive. The goroutine calls ListEnabled once per scene per tick, so after
// at least one tick the counter is greater than the initial Reload's count.
//
// The reload loop goroutine is cancelled via Engine.Stop so it does not
// leak past the test.
func TestRegister_EvalIntervalStartsReloadLoop(t *testing.T) {
	target := mustNewTarget(t)

	var calls int64
	store := &countingStore{
		stubRuleStore: stubRuleStore{rules: []Record{
			{ID: 1, TenantID: 1, Name: "dynamic", Scene: string(SceneMetric), Expression: `alert.metrics["cpu"] > 0`, Enabled: true, Priority: 1},
		}},
		calls: &calls,
	}
	cfg := Config{
		Logger:       zap.NewNop(),
		Store:        store,
		EvalInterval: 50 * time.Millisecond,
	}
	ruleEng, err := Register(context.Background(), target, cfg)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	defer ruleEng.Stop(context.Background())

	// Initial Reload = 4 ListEnabled calls (one per scene: task, probe,
	// metric, remediation). Wait for at least one more tick (4 more calls)
	// before asserting.
	initialCalls := atomic.LoadInt64(&calls)
	if initialCalls < 4 {
		t.Errorf("expected at least 4 ListEnabled calls from initial Reload, got %d", initialCalls)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt64(&calls) >= initialCalls+4 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	got := atomic.LoadInt64(&calls)
	if got < initialCalls+4 {
		t.Errorf("expected reload loop to fire (>= %d calls), got %d", initialCalls+4, got)
	}
}

// TestRegister_EvalIntervalZeroNoReloadLoop verifies that when EvalInterval is
// zero, Register does NOT start a reload goroutine: only the initial Reload's
// 4 ListEnabled calls occur, and the count does not grow over time.
func TestRegister_EvalIntervalZeroNoReloadLoop(t *testing.T) {
	target := mustNewTarget(t)

	var calls int64
	store := &countingStore{
		stubRuleStore: stubRuleStore{rules: []Record{
			{ID: 1, TenantID: 1, Name: "dynamic", Scene: string(SceneMetric), Expression: `alert.metrics["cpu"] > 0`, Enabled: true, Priority: 1},
		}},
		calls: &calls,
	}
	cfg := Config{
		Logger:       zap.NewNop(),
		Store:        store,
		EvalInterval: 0, // no periodic loop
	}
	if _, err := Register(context.Background(), target, cfg); err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Initial Reload = 4 calls. Wait long enough that a 50ms ticker (had it
	// been started) would have fired multiple times.
	time.Sleep(200 * time.Millisecond)
	got := atomic.LoadInt64(&calls)
	if got != 4 {
		t.Errorf("expected exactly 4 ListEnabled calls (initial Reload only), got %d", got)
	}
}

// ---------------------------------------------------------------------------
// Register: AssetStore wiring
// ---------------------------------------------------------------------------

// TestRegister_AssetStoreWired verifies that when an AssetStore is
// configured, the MetricMatcher's store field is populated so Match can enrich
// the env via GetByID.
func TestRegister_AssetStoreWired(t *testing.T) {
	target := mustNewTarget(t)

	resStore := &stubAssetStore{asset: assetAssetForRegisterPtr()}
	cfg := Config{
		Logger:     zap.NewNop(),
		AssetStore: resStore,
		Rules: []Spec{
			{Name: "by-name", Scene: SceneMetric, Expression: `asset.name == "enriched-host"`, Priority: 1},
		},
	}
	if _, err := Register(context.Background(), target, cfg); err != nil {
		t.Fatalf("Register: %v", err)
	}

	mm := target.Rules()[0].(*MetricMatcher)
	if mm.store == nil {
		t.Fatal("expected MetricMatcher.store to be wired from cfg.AssetStore")
	}
	ctx := context.Background()
	evt := a.Event{
		Type:      a.TypeMetric,
		AssetID:   100,
		TenantID:  1,
		Timestamp: time.Now(),
	}
	if !mm.Match(ctx, evt) {
		t.Error("expected Match=true when asset.name matches the rule via AssetStore enrichment")
	}
}

// assetAssetForRegisterPtr is a helper that returns a pointer to an
// asset.Asset used to seed the stubAssetStore for the AssetStore
// wiring test. It is named distinctly to avoid clashing with the asset
// package identifier inside the test file.
func assetAssetForRegisterPtr() *asset.Asset {
	return &asset.Asset{
		ID:        100,
		TenantID:  1,
		AssetType: types.AssetTypeHost,
		Name:      "enriched-host",
		Status:    types.AssetStatusNormal,
	}
}

// ---------------------------------------------------------------------------
// Register: full integration — static + store + asset store
// ---------------------------------------------------------------------------

// TestRegister_FullIntegration verifies the full Register pipeline: static
// rules are loaded, the store performs an initial Reload, the MetricMatcher is
// registered, and matching reflects the dynamic rules.
func TestRegister_FullIntegration(t *testing.T) {
	target := mustNewTarget(t)

	store := &stubRuleStore{rules: []Record{
		{ID: 1, TenantID: 1, Name: "cpu-dynamic", Scene: string(SceneMetric), Expression: `alert.metrics["cpu"] > 70`, Enabled: true, Priority: 10},
	}}
	cfg := Config{
		Logger: zap.NewNop(),
		Rules: []Spec{
			{Name: "static-fallback", Scene: SceneMetric, Expression: `alert.metrics["cpu"] > 999`, Priority: 1},
		},
		Store:      store,
		AssetStore: &stubAssetStore{},
	}
	if _, err := Register(context.Background(), target, cfg); err != nil {
		t.Fatalf("Register: %v", err)
	}

	rules := target.Rules()
	if len(rules) != 1 {
		t.Fatalf("expected 1 prism rule, got %d", len(rules))
	}
	mm := rules[0].(*MetricMatcher)

	// After the initial Reload, only the dynamic rule (threshold 70) is loaded.
	ctx := context.Background()
	if !mm.Match(ctx, metricEvent("cpu", 75, "critical")) {
		t.Error("expected Match=true for cpu=75 against dynamic rule (threshold 70)")
	}
	if mm.Match(ctx, metricEvent("cpu", 65, "critical")) {
		t.Error("expected Match=false for cpu=65 against dynamic rule (threshold 70)")
	}
}

// ---------------------------------------------------------------------------
// concurrency: Register + Match
// ---------------------------------------------------------------------------

// TestRegister_ConcurrentMatchAfterRegister verifies that the engine produced
// by Register is safe for concurrent Match invocations. Run with -race to
// detect data races.
func TestRegister_ConcurrentMatchAfterRegister(t *testing.T) {
	target := mustNewTarget(t)

	cfg := Config{
		Logger: zap.NewNop(),
		Rules: []Spec{
			{Name: "cpu", Scene: SceneMetric, Expression: `alert.metrics["cpu"] > 80`, Priority: 10},
		},
	}
	if _, err := Register(context.Background(), target, cfg); err != nil {
		t.Fatalf("Register: %v", err)
	}
	mm := target.Rules()[0].(*MetricMatcher)

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	start := make(chan struct{})
	ctx := context.Background()
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			<-start
			for i := 0; i < 20; i++ {
				_ = mm.Match(ctx, metricEvent("cpu", 90, "critical"))
			}
		}()
	}
	close(start)
	wg.Wait()
}
