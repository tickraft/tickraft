// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see details in LICENSE.

package remediation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/tickraft/tickraft/pkg/db"
)

// setupRemediationStore opens an in-memory SQLite database and migrates the
// remediation tables, returning the store and a cleanup function.
func setupRemediationStore(t *testing.T) (store *Store, cleanup func()) {
	t.Helper()
	ctx := context.Background()
	gdb, err := db.Open(ctx, db.Config{Driver: "sqlite3", Addr: ":memory:"})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	store = NewStore(gdb)
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	cleanup = func() {
		if sqlDB, e := gdb.DB(); e == nil {
			_ = sqlDB.Close()
		}
	}
	return store, cleanup
}

// mustCreateRule inserts a rule, failing the test on error. The rule must
// be fully built before the call: fields mutated afterwards do not reach
// the database.
func mustCreateRule(t *testing.T, store *Store, rule *Rule) *Rule {
	t.Helper()
	if err := store.Create(context.Background(), rule); err != nil {
		t.Fatalf("create rule %q: %v", rule.Name, err)
	}
	return rule
}

// mustGetRule fetches a rule by ID, failing the test on error.
func mustGetRule(t *testing.T, store *Store, id int64) *Rule {
	t.Helper()
	got, err := store.GetByID(context.Background(), id)
	if err != nil {
		t.Fatalf("get rule %d: %v", id, err)
	}
	return got
}

// metricRule builds a minimal valid enabled metric-trigger rule.
func metricRule(name string) *Rule {
	return &Rule{
		Name:                    name,
		TriggerEventType:        string(TriggerMetric),
		ExecutorType:            "local",
		ExecutorConfig:          `{"command":"true"}`,
		Cooldown:                60,
		CircuitBreakerThreshold: 3,
		Enabled:                 true,
		Status:                  string(StatusActive),
	}
}

// TestStoreCreatePersistsExplicitZeroValues pins the GORM default-tag fix:
// rules created disabled, or with zero cooldown / disabled circuit breaker,
// must persist those zeros instead of being swallowed by column defaults.
func TestStoreCreatePersistsExplicitZeroValues(t *testing.T) {
	store, cleanup := setupRemediationStore(t)
	defer cleanup()

	r := mustCreateRule(t, store, &Rule{
		Name:                    "zeros",
		TriggerEventType:        string(TriggerMetric),
		ExecutorType:            "local",
		Cooldown:                0,
		CircuitBreakerThreshold: 0,
		Enabled:                 false,
		Status:                  string(StatusActive),
	})

	got := mustGetRule(t, store, r.ID)
	if got.Enabled {
		t.Error("Enabled=false was persisted as true")
	}
	if got.Cooldown != 0 {
		t.Errorf("Cooldown = %d, want 0", got.Cooldown)
	}
	if got.CircuitBreakerThreshold != 0 {
		t.Errorf("CircuitBreakerThreshold = %d, want 0", got.CircuitBreakerThreshold)
	}
	if got.Status != string(StatusActive) {
		t.Errorf("Status = %q, want active", got.Status)
	}
}

// TestStoreUpdateColumnLevel pins the PUT contract: user-editable columns
// are applied while runtime state (status, consecutive_failures,
// last_run_at) and lifecycle fields (tenant_id, created_at) survive a
// stale DTO (fix for D-01).
func TestStoreUpdateColumnLevel(t *testing.T) {
	store, cleanup := setupRemediationStore(t)
	defer cleanup()
	ctx := context.Background()

	r := mustCreateRule(t, store, metricRule("original"))
	paused := time.Now().Add(-time.Minute).Truncate(time.Second)
	if err := store.UpdateRuleStatus(ctx, r.ID, string(StatusPaused)); err != nil {
		t.Fatalf("pause: %v", err)
	}
	if err := store.UpdateLastRun(ctx, r.ID, paused); err != nil {
		t.Fatalf("last run: %v", err)
	}
	if err := store.RecordExecutionOutcome(ctx, r.ID, false); err != nil {
		t.Fatalf("outcome: %v", err)
	}
	original := mustGetRule(t, store, r.ID)

	// A stale DTO: renamed, disabled, zeroed cooldown, wrong tenant and
	// created_at, plus runtime-state fields that must be ignored.
	dto := &Rule{
		ID:                      r.ID,
		TenantID:                999,
		Name:                    "renamed",
		Description:             "updated",
		TriggerEventType:        string(TriggerLog),
		Expression:              `content contains "oom"`,
		ExecutorType:            "webhook",
		ExecutorConfig:          `{"url":"http://example.test"}`,
		Cooldown:                0,
		CircuitBreakerThreshold: 5,
		Enabled:                 false,
		Status:                  string(StatusActive),              // must NOT be applied
		ConsecutiveFailures:     77,                                // must NOT be applied
		CreatedAt:               original.CreatedAt.Add(time.Hour), // must NOT be applied
	}
	if err := store.Update(ctx, dto); err != nil {
		t.Fatalf("update: %v", err)
	}

	got := mustGetRule(t, store, r.ID)
	// Editable columns applied.
	if got.Name != "renamed" || got.Description != "updated" {
		t.Errorf("name/description not applied: %+v", got)
	}
	if got.TriggerEventType != string(TriggerLog) || got.Expression != `content contains "oom"` {
		t.Errorf("trigger/expression not applied: %+v", got)
	}
	if got.ExecutorType != "webhook" || got.ExecutorConfig != `{"url":"http://example.test"}` {
		t.Errorf("executor fields not applied: %+v", got)
	}
	if got.Cooldown != 0 || got.CircuitBreakerThreshold != 5 || got.Enabled {
		t.Errorf("cooldown/threshold/enabled not applied: %+v", got)
	}
	// Runtime state and lifecycle fields untouched.
	if got.Status != string(StatusPaused) {
		t.Errorf("Status = %q, want paused (runtime state must survive PUT)", got.Status)
	}
	if got.ConsecutiveFailures != 1 {
		t.Errorf("ConsecutiveFailures = %d, want 1 (counter must survive PUT)", got.ConsecutiveFailures)
	}
	if got.LastRunAt == nil || !got.LastRunAt.Equal(paused) {
		t.Errorf("LastRunAt = %v, want %v (must survive PUT)", got.LastRunAt, paused)
	}
	if got.TenantID != original.TenantID {
		t.Errorf("TenantID = %d, want %d", got.TenantID, original.TenantID)
	}
	if !got.CreatedAt.Equal(original.CreatedAt) {
		t.Errorf("CreatedAt changed: %v -> %v", original.CreatedAt, got.CreatedAt)
	}
}

// TestStoreUpdateMissing pins the not-found contract of Update.
func TestStoreUpdateMissing(t *testing.T) {
	store, cleanup := setupRemediationStore(t)
	defer cleanup()

	err := store.Update(context.Background(), &Rule{ID: 424242, Name: "ghost"})
	if !errors.Is(err, ErrRuleNotFound) {
		t.Errorf("Update(missing) err = %v, want ErrRuleNotFound", err)
	}
}

// TestStoreRecordExecutionOutcome pins the atomic circuit-breaker SQL:
// failure increments, the row's own threshold pauses it, success resets
// the counter, and a disabled threshold never pauses.
func TestStoreRecordExecutionOutcome(t *testing.T) {
	store, cleanup := setupRemediationStore(t)
	defer cleanup()
	ctx := context.Background()

	r := mustCreateRule(t, store, metricRule("breaker")) // threshold 3

	// Two failures: counter climbs, still active.
	for i := 1; i <= 2; i++ {
		if err := store.RecordExecutionOutcome(ctx, r.ID, false); err != nil {
			t.Fatalf("failure %d: %v", i, err)
		}
		got := mustGetRule(t, store, r.ID)
		if got.ConsecutiveFailures != i {
			t.Errorf("after failure %d: ConsecutiveFailures = %d", i, got.ConsecutiveFailures)
		}
		if got.Status != string(StatusActive) {
			t.Errorf("after failure %d: Status = %q, want active", i, got.Status)
		}
	}

	// Success resets the counter without changing status.
	if err := store.RecordExecutionOutcome(ctx, r.ID, true); err != nil {
		t.Fatalf("success: %v", err)
	}
	if got := mustGetRule(t, store, r.ID); got.ConsecutiveFailures != 0 {
		t.Errorf("after success: ConsecutiveFailures = %d, want 0", got.ConsecutiveFailures)
	}

	// Three consecutive failures trip the breaker and pause the rule.
	for range 3 {
		if err := store.RecordExecutionOutcome(ctx, r.ID, false); err != nil {
			t.Fatalf("trip failure: %v", err)
		}
	}
	got := mustGetRule(t, store, r.ID)
	if got.ConsecutiveFailures != 3 {
		t.Errorf("after trip: ConsecutiveFailures = %d, want 3", got.ConsecutiveFailures)
	}
	if got.Status != string(StatusPaused) {
		t.Errorf("after trip: Status = %q, want paused", got.Status)
	}

	// A disabled threshold never pauses, even at high counts.
	noBreaker := metricRule("no-breaker")
	noBreaker.CircuitBreakerThreshold = 0
	nb := mustCreateRule(t, store, noBreaker)
	for range 5 {
		if err := store.RecordExecutionOutcome(ctx, nb.ID, false); err != nil {
			t.Fatalf("no-breaker failure: %v", err)
		}
	}
	got = mustGetRule(t, store, nb.ID)
	if got.ConsecutiveFailures != 5 {
		t.Errorf("no-breaker: ConsecutiveFailures = %d, want 5", got.ConsecutiveFailures)
	}
	if got.Status != string(StatusActive) {
		t.Errorf("no-breaker: Status = %q, want active (threshold 0 disables the breaker)", got.Status)
	}
}

// TestStoreMigrateDropsLegacyTables verifies that Migrate removes the
// orphaned pre-rename tables (sys_remediation_rule / sys_remediation_record)
// and is idempotent.
func TestStoreMigrateDropsLegacyTables(t *testing.T) {
	store, cleanup := setupRemediationStore(t)
	defer cleanup()
	ctx := context.Background()

	legacyTables := []string{"sys_remediation_rule", "sys_remediation_record"}
	for _, table := range legacyTables {
		stmt := "CREATE TABLE " + table + " (id INTEGER PRIMARY KEY)"
		if err := store.dbc.WithContext(ctx).Exec(stmt).Error; err != nil {
			t.Fatalf("create legacy table %s: %v", table, err)
		}
	}

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	tableExists := func(name string) bool {
		var count int64
		if err := store.dbc.WithContext(ctx).
			Raw("SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?", name).
			Scan(&count).Error; err != nil {
			t.Fatalf("query sqlite_master for %s: %v", name, err)
		}
		return count > 0
	}
	for _, table := range legacyTables {
		if tableExists(table) {
			t.Errorf("legacy table %s still present after migrate", table)
		}
	}
	if !tableExists("sys_prism_remediation_rule") {
		t.Error("sys_prism_remediation_rule missing after migrate")
	}

	// A second run must succeed: the DROPs are IF EXISTS.
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
}

// TestStoreGetRules pins the engine-facing query: enabled active rules
// only, trigger filter, tenant scope, and global (asset_id = 0) rules
// returned alongside asset-scoped ones.
func TestStoreGetRules(t *testing.T) {
	store, cleanup := setupRemediationStore(t)
	defer cleanup()
	ctx := context.Background()

	// metricRule builds the common seed shape: an enabled, active local
	// rule on the metric trigger with the given name and asset scope.
	metricRule := func(name string, assetID int64) *Rule {
		return &Rule{
			Name: name, AssetID: assetID,
			TriggerEventType: string(TriggerMetric), ExecutorType: "local",
			Enabled: true, Status: string(StatusActive),
		}
	}
	seed := []*Rule{
		metricRule("scoped", 7),
		metricRule("global", 0),
		metricRule("other-asset", 99),
		{
			Name: "disabled", AssetID: 7, Enabled: false,
			TriggerEventType: string(TriggerMetric), ExecutorType: "local", Status: string(StatusActive),
		},
		{
			Name: "log-rule", AssetID: 7,
			TriggerEventType: string(TriggerLog), ExecutorType: "local", Enabled: true, Status: string(StatusActive),
		},
		{
			Name: "other-tenant", TenantID: 55, AssetID: 7,
			TriggerEventType: string(TriggerMetric), ExecutorType: "local", Enabled: true, Status: string(StatusActive),
		},
		{
			Name: "paused", AssetID: 7,
			TriggerEventType: string(TriggerMetric), ExecutorType: "local", Enabled: true, Status: string(StatusPaused),
		},
	}
	for _, r := range seed {
		mustCreateRule(t, store, r)
	}

	// An asset-7 metric event: scoped + global + paused rules — the SQL
	// filters on enabled/trigger/tenant/asset; the paused-status gate is
	// applied in the Manager's decision loop.
	rules, err := store.GetRules(ctx, 0, 7, string(TriggerMetric))
	if err != nil {
		t.Fatalf("get rules: %v", err)
	}
	names := map[string]bool{}
	for _, r := range rules {
		names[r.Name] = true
	}
	if len(rules) != 3 || !names["scoped"] || !names["global"] || !names["paused"] {
		t.Errorf("GetRules returned %v, want scoped+global+paused", names)
	}

	// A global event (asset 0) sees only global rules.
	rules, err = store.GetRules(ctx, 0, 0, string(TriggerMetric))
	if err != nil {
		t.Fatalf("get rules global: %v", err)
	}
	if len(rules) != 1 || rules[0].Name != "global" {
		t.Errorf("GetRules(asset 0) returned %d rules, want global only", len(rules))
	}

	// Other tenants are isolated.
	rules, err = store.GetRules(ctx, 1, 7, string(TriggerMetric))
	if err != nil {
		t.Fatalf("get rules tenant 1: %v", err)
	}
	if len(rules) != 0 {
		t.Errorf("GetRules(tenant 1) returned %d rules, want 0", len(rules))
	}
}
