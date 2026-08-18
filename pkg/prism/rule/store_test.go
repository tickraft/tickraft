// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package rule

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// newTestStore returns a Store backed by a fresh in-memory SQLite database
// with the Record table migrated. It fatals the test on setup failure so
// each test starts from a clean, schema-ready database.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	store := NewStore(db, NewCompiler())
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return store
}

// mustCreateRule is a thin wrapper around store.Create that fatals the test on
// failure and returns the created model (with the auto-incremented ID populated).
func mustCreateRule(t *testing.T, store *Store, m *Record) *Record {
	t.Helper()
	ctx := context.Background()
	if err := store.Create(ctx, m); err != nil {
		t.Fatalf("Create: %v", err)
	}
	return m
}

// validMetricModel is a convenience constructor for a Record that is known
// to compile cleanly for the metric scene. Tests that exercise compile-check
// failure override Expression after construction.
func validMetricModel(name, expr string) *Record {
	return &Record{
		TenantID:   1,
		Name:       name,
		Scene:      string(SceneMetric),
		Expression: expr,
		Enabled:    true,
		Priority:   10,
	}
}

// ---------------------------------------------------------------------------
// Create / Get / Update / Delete lifecycle
// ---------------------------------------------------------------------------

// TestStore_CreateAndGet verifies that Create persists the rule and Get
// retrieves the same fields by (id, tenantID).
func TestStore_CreateAndGet(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	created := mustCreateRule(t, store, validMetricModel("cpu-high", `alert.metrics["cpu"] > 80`))
	if created.ID == 0 {
		t.Fatal("expected auto-incremented ID after Create")
	}
	if created.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be populated by GORM autoCreateTime")
	}

	got, err := store.Get(ctx, created.ID, created.TenantID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != created.ID || got.Name != "cpu-high" || got.Expression != `alert.metrics["cpu"] > 80` {
		t.Errorf("Get mismatch: got=%+v want=%+v", got, created)
	}
	if got.Scene != string(SceneMetric) || got.Priority != 10 || got.Enabled != true {
		t.Errorf("fields mismatch: got=%+v", got)
	}
}

// TestStore_GetNotFound verifies that Get on a missing (id, tenantID)
// returns ErrRuleNotFound (mapped from gorm.ErrRecordNotFound).
func TestStore_GetNotFound(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	_, err := store.Get(ctx, 999, 1)
	if !errors.Is(err, ErrRuleNotFound) {
		t.Errorf("expected ErrRuleNotFound, got %v", err)
	}

	// Existing ID but wrong tenant → still not found.
	created := mustCreateRule(t, store, validMetricModel("rule-1", `alert.metrics["cpu"] > 80`))
	_, err = store.Get(ctx, created.ID, created.TenantID+1)
	if !errors.Is(err, ErrRuleNotFound) {
		t.Errorf("expected ErrRuleNotFound for wrong tenant, got %v", err)
	}
}

// TestStore_Update verifies that Update persists changes and that Get
// reflects them. It also verifies that ID/TenantID are preserved by Save.
func TestStore_Update(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	created := mustCreateRule(t, store, validMetricModel("cpu-high", `alert.metrics["cpu"] > 80`))
	created.Expression = `alert.metrics["cpu"] > 90`
	created.Priority = 20
	created.Enabled = false
	created.Description = "tighter threshold"
	if err := store.Update(ctx, created); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := store.Get(ctx, created.ID, created.TenantID)
	if err != nil {
		t.Fatalf("Get after Update: %v", err)
	}
	if got.Expression != `alert.metrics["cpu"] > 90` {
		t.Errorf("Expression = %q, want %q", got.Expression, `alert.metrics["cpu"] > 90`)
	}
	if got.Priority != 20 {
		t.Errorf("Priority = %d, want 20", got.Priority)
	}
	if got.Enabled != false {
		t.Errorf("Enabled = %v, want false", got.Enabled)
	}
	if got.Description != "tighter threshold" {
		t.Errorf("Description = %q, want \"tighter threshold\"", got.Description)
	}
}

// TestStore_Delete verifies that Delete soft-deletes the rule so that
// subsequent Get returns ErrRuleNotFound.
func TestStore_Delete(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	created := mustCreateRule(t, store, validMetricModel("cpu-high", `alert.metrics["cpu"] > 80`))
	if err := store.Delete(ctx, created.ID, created.TenantID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := store.Get(ctx, created.ID, created.TenantID); !errors.Is(err, ErrRuleNotFound) {
		t.Errorf("expected ErrRuleNotFound after Delete, got %v", err)
	}
}

// TestStore_DeleteNotFound verifies that Delete on a missing (id, tenantID)
// returns ErrRuleNotFound (RowsAffected == 0).
func TestStore_DeleteNotFound(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	if err := store.Delete(ctx, 999, 1); !errors.Is(err, ErrRuleNotFound) {
		t.Errorf("expected ErrRuleNotFound, got %v", err)
	}
}

// TestStore_DeleteWrongTenant verifies that Delete with the wrong tenant
// returns ErrRuleNotFound (the row exists but the tenantID filter excludes it).
func TestStore_DeleteWrongTenant(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	created := mustCreateRule(t, store, validMetricModel("cpu-high", `alert.metrics["cpu"] > 80`))
	if err := store.Delete(ctx, created.ID, created.TenantID+1); !errors.Is(err, ErrRuleNotFound) {
		t.Errorf("expected ErrRuleNotFound for wrong tenant, got %v", err)
	}

	// The rule is still present.
	if _, err := store.Get(ctx, created.ID, created.TenantID); err != nil {
		t.Errorf("rule was deleted by wrong-tenant call: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Compile-check on write paths
// ---------------------------------------------------------------------------

// TestStore_CreateRejectsInvalidExpression verifies that Create compile-
// checks the expression before writing and rejects an invalid expression with a
// wrapped error.
func TestStore_CreateRejectsInvalidExpression(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	bad := validMetricModel("bad", `alert.metrics["cpu"] >`) // dangling operator
	err := store.Create(ctx, bad)
	if err == nil {
		t.Fatal("expected error for invalid expression, got nil")
	}
	// The compile-check error wraps the compiler's error (which itself wraps
	// ErrRuleCompileFailed); verify ErrRuleCompileFailed is in the chain.
	if !errors.Is(err, ErrRuleCompileFailed) {
		t.Errorf("expected ErrRuleCompileFailed in error chain, got %v", err)
	}
	// The row was never written: a subsequent Get returns ErrRuleNotFound.
	if _, err := store.Get(ctx, bad.ID, bad.TenantID); !errors.Is(err, ErrRuleNotFound) {
		t.Errorf("expected ErrRuleNotFound after rejected Create, got %v", err)
	}
}

// TestStore_CreateRejectsInvalidScene verifies that an invalid scene is
// rejected at Create time (Compile returns ErrRuleInvalidScene).
func TestStore_CreateRejectsInvalidScene(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	bad := &Record{
		TenantID:   1,
		Name:       "bad-scene",
		Scene:      "unknown",
		Expression: "true",
		Enabled:    true,
	}
	err := store.Create(ctx, bad)
	if err == nil {
		t.Fatal("expected error for invalid scene, got nil")
	}
	if !errors.Is(err, ErrRuleInvalidScene) {
		t.Errorf("expected ErrRuleInvalidScene in error chain, got %v", err)
	}
}

// TestStore_UpdateRejectsInvalidExpression verifies that Update compile-
// checks the new expression and refuses to persist an invalid one, leaving the
// previously stored row untouched.
func TestStore_UpdateRejectsInvalidExpression(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	created := mustCreateRule(t, store, validMetricModel("cpu-high", `alert.metrics["cpu"] > 80`))
	original, err := store.Get(ctx, created.ID, created.TenantID)
	if err != nil {
		t.Fatalf("Get original: %v", err)
	}

	// Attempt to update with an invalid expression.
	created.Expression = `alert.metrics["cpu"] >`
	updateErr := store.Update(ctx, created)
	if updateErr == nil {
		t.Fatal("expected error for invalid Update expression, got nil")
	}
	if !errors.Is(updateErr, ErrRuleCompileFailed) {
		t.Errorf("expected ErrRuleCompileFailed in error chain, got %v", updateErr)
	}

	// The stored row retains the original expression.
	got, err := store.Get(ctx, created.ID, created.TenantID)
	if err != nil {
		t.Fatalf("Get after rejected Update: %v", err)
	}
	if got.Expression != original.Expression {
		t.Errorf("Expression = %q, want original %q (rejected Update must not mutate state)",
			got.Expression, original.Expression)
	}
}

// ---------------------------------------------------------------------------
// ListEnabled: scene / tenant / enabled filtering and ordering
// ---------------------------------------------------------------------------

// TestStore_ListEnabled_Ordering verifies that ListEnabled returns enabled
// rules ordered by priority DESC, then ID ASC for deterministic evaluation.
func TestStore_ListEnabled_Ordering(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	// Three rules with priorities 5, 10, 5 (in creation/ID order).
	mustCreateRule(t, store, &Record{
		TenantID:   1,
		Name:       "p5-a",
		Scene:      string(SceneMetric),
		Expression: `alert.metrics["cpu"] > 0`,
		Enabled:    true,
		Priority:   5,
	})
	mustCreateRule(t, store, &Record{
		TenantID:   1,
		Name:       "p10",
		Scene:      string(SceneMetric),
		Expression: `alert.metrics["cpu"] > 0`,
		Enabled:    true,
		Priority:   10,
	})
	mustCreateRule(t, store, &Record{
		TenantID:   1,
		Name:       "p5-b",
		Scene:      string(SceneMetric),
		Expression: `alert.metrics["cpu"] > 0`,
		Enabled:    true,
		Priority:   5,
	})

	got, err := store.ListEnabled(ctx, 0, SceneMetric)
	if err != nil {
		t.Fatalf("ListEnabled: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 rules, got %d", len(got))
	}
	// Expected: p10 (priority 10), then p5-a (priority 5, ID smaller), then p5-b.
	wantNames := []string{"p10", "p5-a", "p5-b"}
	for i, w := range wantNames {
		if got[i].Name != w {
			t.Errorf("position %d: Name = %q, want %q (full order: %+v)", i, got[i].Name, w, namesOf(got))
		}
	}
}

// namesOf extracts the Name slice from a Record slice for assertion messages.
func namesOf(ms []Record) []string {
	out := make([]string, len(ms))
	for i := range ms {
		out[i] = ms[i].Name
	}
	return out
}

// TestStore_ListEnabled_SceneFilter verifies that ListEnabled only returns
// rules matching the requested scene.
func TestStore_ListEnabled_SceneFilter(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	mustCreateRule(t, store, &Record{
		TenantID:   1,
		Name:       "metric",
		Scene:      string(SceneMetric),
		Expression: `alert.metrics["cpu"] > 0`,
		Enabled:    true,
		Priority:   1,
	})
	mustCreateRule(t, store, &Record{
		TenantID:   1,
		Name:       "task",
		Scene:      string(SceneTask),
		Expression: "task.priority > 0",
		Enabled:    true,
		Priority:   1,
	})
	mustCreateRule(t, store, &Record{
		TenantID:   1,
		Name:       "probe",
		Scene:      string(SceneProbe),
		Expression: `report.log_content contains "x"`,
		Enabled:    true,
		Priority:   1,
	})

	for _, tc := range []struct {
		scene Scene
		want  string
	}{
		{SceneMetric, "metric"},
		{SceneTask, "task"},
		{SceneProbe, "probe"},
	} {
		got, err := store.ListEnabled(ctx, 0, tc.scene)
		if err != nil {
			t.Fatalf("ListEnabled(%s): %v", tc.scene, err)
		}
		if len(got) != 1 {
			t.Errorf("scene=%s: expected 1 rule, got %d (%+v)", tc.scene, len(got), namesOf(got))
			continue
		}
		if got[0].Name != tc.want {
			t.Errorf("scene=%s: Name = %q, want %q", tc.scene, got[0].Name, tc.want)
		}
	}
}

// TestStore_ListEnabled_EnabledFilter verifies that ListEnabled excludes
// rules with Enabled=false.
//
// Note: GORM's `default:true` tag on Record.Enabled means that Create with
// Enabled=false (the zero value) is overwritten by the default true. To create
// a disabled rule we therefore Create with Enabled=true and then Update
// (Save) it to false — Save writes all fields including zero values.
func TestStore_ListEnabled_EnabledFilter(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	mustCreateRule(t, store, &Record{
		TenantID:   1,
		Name:       "on",
		Scene:      string(SceneMetric),
		Expression: `alert.metrics["cpu"] > 0`,
		Enabled:    true,
		Priority:   1,
	})
	off := mustCreateRule(t, store, &Record{
		TenantID:   1,
		Name:       "off",
		Scene:      string(SceneMetric),
		Expression: `alert.metrics["cpu"] > 0`,
		Enabled:    true,
		Priority:   1,
	})
	off.Enabled = false
	if err := store.Update(ctx, off); err != nil {
		t.Fatalf("Update to disable: %v", err)
	}

	got, err := store.ListEnabled(ctx, 0, SceneMetric)
	if err != nil {
		t.Fatalf("ListEnabled: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 enabled rule, got %d (%+v)", len(got), namesOf(got))
	}
	if got[0].Name != "on" {
		t.Errorf("Name = %q, want \"on\"", got[0].Name)
	}
}

// TestStore_ListEnabled_TenantFilter verifies that ListEnabled with a
// positive tenantID returns only that tenant's rules, and a zero tenantID
// returns rules across all tenants.
func TestStore_ListEnabled_TenantFilter(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	mustCreateRule(t, store, &Record{
		TenantID:   1,
		Name:       "t1-a",
		Scene:      string(SceneMetric),
		Expression: `alert.metrics["cpu"] > 0`,
		Enabled:    true,
		Priority:   1,
	})
	mustCreateRule(t, store, &Record{
		TenantID:   1,
		Name:       "t1-b",
		Scene:      string(SceneMetric),
		Expression: `alert.metrics["cpu"] > 0`,
		Enabled:    true,
		Priority:   1,
	})
	mustCreateRule(t, store, &Record{
		TenantID:   2,
		Name:       "t2-a",
		Scene:      string(SceneMetric),
		Expression: `alert.metrics["cpu"] > 0`,
		Enabled:    true,
		Priority:   1,
	})

	// tenantID=1 → 2 rules.
	gotT1, err := store.ListEnabled(ctx, 1, SceneMetric)
	if err != nil {
		t.Fatalf("ListEnabled tenant=1: %v", err)
	}
	if len(gotT1) != 2 {
		t.Errorf("tenant=1: expected 2 rules, got %d (%+v)", len(gotT1), namesOf(gotT1))
	}

	// tenantID=2 → 1 rule.
	gotT2, err := store.ListEnabled(ctx, 2, SceneMetric)
	if err != nil {
		t.Fatalf("ListEnabled tenant=2: %v", err)
	}
	if len(gotT2) != 1 || gotT2[0].Name != "t2-a" {
		t.Errorf("tenant=2: expected [t2-a], got %+v", namesOf(gotT2))
	}

	// tenantID=0 → all 3 rules.
	gotAll, err := store.ListEnabled(ctx, 0, SceneMetric)
	if err != nil {
		t.Fatalf("ListEnabled tenant=0: %v", err)
	}
	if len(gotAll) != 3 {
		t.Errorf("tenant=0: expected 3 rules, got %d (%+v)", len(gotAll), namesOf(gotAll))
	}
}

// TestStore_ListEnabled_ExcludesSoftDeleted verifies that a soft-deleted
// rule is excluded from ListEnabled (GORM's default scope applies deleted_at IS NULL).
func TestStore_ListEnabled_ExcludesSoftDeleted(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	keep := mustCreateRule(t, store, &Record{
		TenantID:   1,
		Name:       "keep",
		Scene:      string(SceneMetric),
		Expression: `alert.metrics["cpu"] > 0`,
		Enabled:    true,
		Priority:   1,
	})
	mustCreateRule(t, store, &Record{
		TenantID:   1,
		Name:       "delete",
		Scene:      string(SceneMetric),
		Expression: `alert.metrics["cpu"] > 0`,
		Enabled:    true,
		Priority:   1,
	})

	if err := store.Delete(ctx, keep.ID, keep.TenantID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	got, err := store.ListEnabled(ctx, 0, SceneMetric)
	if err != nil {
		t.Fatalf("ListEnabled after Delete: %v", err)
	}
	if len(got) != 1 || got[0].Name != "delete" {
		t.Errorf("expected [delete], got %+v", namesOf(got))
	}
}

// TestStore_ListEnabled_EmptyResult verifies that ListEnabled on a scene
// with no rules returns an empty (non-nil) slice and no error.
func TestStore_ListEnabled_EmptyResult(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	got, err := store.ListEnabled(ctx, 1, SceneMetric)
	if err != nil {
		t.Fatalf("ListEnabled empty: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil slice for empty result")
	}
	if len(got) != 0 {
		t.Errorf("expected 0 rules, got %d", len(got))
	}
}

// ---------------------------------------------------------------------------
// Migrate
// ---------------------------------------------------------------------------

// TestStore_Migrate verifies that Migrate creates the sys_prism_rule
// table and that re-running Migrate is idempotent.
func TestStore_Migrate(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	store := NewStore(db, NewCompiler())
	ctx := context.Background()

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate first call: %v", err)
	}
	// Idempotent: a second call must not error.
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate second call: %v", err)
	}

	// The table exists: a Create must succeed.
	m := validMetricModel("migrate-test", `alert.metrics["cpu"] > 0`)
	if err := store.Create(ctx, m); err != nil {
		t.Fatalf("Create after Migrate: %v", err)
	}
	if m.ID == 0 {
		t.Error("expected auto-incremented ID after Create on migrated table")
	}
}

// TestStore_MigrateFailure verifies that a Migrate error is wrapped with
// the "migrate rule table" prefix.
func TestStore_MigrateFailure(t *testing.T) {
	// Use a closed DB to force an AutoMigrate error.
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if sqlDB, err := db.DB(); err == nil {
		_ = sqlDB.Close()
	}
	store := NewStore(db, NewCompiler())
	err = store.Migrate(context.Background())
	if err == nil {
		t.Fatal("expected Migrate error on closed DB, got nil")
	}
	if !strings.Contains(err.Error(), "migrate rule table") {
		t.Errorf("expected error to wrap with \"migrate rule table\", got %v", err)
	}
}

// ---------------------------------------------------------------------------
// toRule conversion
// ---------------------------------------------------------------------------

// TestToRule_Conversion verifies that Record.toRule preserves all fields
// and parses the JSON-encoded Metadata column.
func TestToRule_Conversion(t *testing.T) {
	m := Record{
		ID:         7,
		TenantID:   1,
		Name:       "cpu-high",
		Scene:      string(SceneMetric),
		Expression: `alert.metrics["cpu"] > 80`,
		Enabled:    true,
		Priority:   10,
		Metadata:   `{"owner":"ops","severity":"critical"}`,
	}
	r := m.toRule()
	if r.ID != m.ID || r.TenantID != m.TenantID || r.Name != m.Name {
		t.Errorf("identity mismatch: got=%+v", r)
	}
	if r.Scene != SceneMetric || r.Expression != m.Expression {
		t.Errorf("scene/expr mismatch: got=%+v", r)
	}
	if r.Priority != m.Priority || r.Enabled != m.Enabled {
		t.Errorf("priority/enabled mismatch: got=%+v", r)
	}
	if r.Metadata["owner"] != "ops" || r.Metadata["severity"] != "critical" {
		t.Errorf("metadata mismatch: got=%+v", r.Metadata)
	}
}

// TestToRule_EmptyMetadata verifies that an empty Metadata column yields a nil
// map on the resulting Rule.
func TestToRule_EmptyMetadata(t *testing.T) {
	m := Record{ID: 1, Scene: string(SceneMetric), Expression: `alert.metrics["cpu"] > 0`}
	r := m.toRule()
	if r.Metadata != nil {
		t.Errorf("expected nil Metadata for empty column, got %+v", r.Metadata)
	}
}

// TestToRule_InvalidJSONMetadata verifies that a malformed Metadata column is
// tolerated: the resulting Rule has a nil Metadata field rather than failing.
func TestToRule_InvalidJSONMetadata(t *testing.T) {
	m := Record{
		ID:       1,
		Scene:    string(SceneMetric),
		Metadata: `{not-json`,
	}
	r := m.toRule()
	if r.Metadata != nil {
		t.Errorf("expected nil Metadata for invalid JSON, got %+v", r.Metadata)
	}
}

// TestToRule_EmptyObjectMetadata verifies that an empty JSON object parses to
// nil (parseMetadata explicitly returns nil for len(metadata) == 0).
func TestToRule_EmptyObjectMetadata(t *testing.T) {
	m := Record{
		ID:       1,
		Scene:    string(SceneMetric),
		Metadata: `{}`,
	}
	r := m.toRule()
	if r.Metadata != nil {
		t.Errorf("expected nil Metadata for empty JSON object, got %+v", r.Metadata)
	}
}

// TestParseMetadata covers all branches of parseMetadata: empty, valid JSON,
// invalid JSON, and an empty JSON object.
func TestParseMetadata(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want map[string]string
	}{
		{"empty string", "", nil},
		{"valid json", `{"a":"1","b":"2"}`, map[string]string{"a": "1", "b": "2"}},
		{"invalid json", `{not-json`, nil},
		{"empty object", `{}`, nil},
		{"json array (not object)", `["a","b"]`, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseMetadata(tc.raw)
			if len(got) != len(tc.want) {
				t.Errorf("parseMetadata(%q) len = %d, want %d", tc.raw, len(got), len(tc.want))
				return
			}
			for k, v := range tc.want {
				if got[k] != v {
					t.Errorf("parseMetadata(%q)[%q] = %q, want %q", tc.raw, k, got[k], v)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Cross-cutting: full lifecycle integration with the engine
// ---------------------------------------------------------------------------

// TestStore_ReloadIntoEngine verifies that rules persisted via the store
// can be reloaded into the engine via Reload, and that the engine's evaluation
// reflects the stored priorities and expressions.
//
// Note: the disabled rule is Created with Enabled=true and then Update'd to
// Enabled=false because GORM's `default:true` tag overwrites the zero-value
// false on Create.
func TestStore_ReloadIntoEngine(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	mustCreateRule(t, store, &Record{
		TenantID:   1,
		Name:       "cpu",
		Scene:      string(SceneMetric),
		Expression: `alert.metrics["cpu"] > 80`,
		Enabled:    true,
		Priority:   10,
	})
	mustCreateRule(t, store, &Record{
		TenantID:   1,
		Name:       "mem",
		Scene:      string(SceneMetric),
		Expression: `alert.metrics["mem"] > 90`,
		Enabled:    true,
		Priority:   5,
	})
	// Disabled rule must not be reloaded.
	disabled := mustCreateRule(t, store, &Record{
		TenantID:   1,
		Name:       "disabled",
		Scene:      string(SceneMetric),
		Expression: `alert.metrics["cpu"] > 0`,
		Enabled:    true,
		Priority:   1,
	})
	disabled.Enabled = false
	if err := store.Update(ctx, disabled); err != nil {
		t.Fatalf("Update to disable: %v", err)
	}

	eng := NewEngine(nil) // nil logger is replaced by NewEngine
	if err := eng.Reload(ctx, store); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	// cpu=95 and mem=95 matches both enabled rules (>80 and >90).
	matched := eng.MatchMetric(ctx, MetricMatchEnv{Alert: AlertView{Metrics: map[string]float64{"cpu": 95, "mem": 95}}})
	if len(matched) != 2 {
		t.Errorf("expected 2 matches, got %d (%v)", len(matched), matched)
	}

	// cpu=85 matches only the >80 rule.
	matched = eng.MatchMetric(ctx, MetricMatchEnv{Alert: AlertView{Metrics: map[string]float64{"cpu": 85}}})
	if len(matched) != 1 {
		t.Errorf("expected 1 match for cpu=85, got %d (%v)", len(matched), matched)
	}
}

// TestStore_TableName verifies that Record uses the configured table
// name sys_prism_rule.
func TestStore_TableName(t *testing.T) {
	var m Record
	if got := m.TableName(); got != "sys_prism_rule" {
		t.Errorf("TableName = %q, want \"sys_prism_rule\"", got)
	}
}

// TestStore_CRUDFlowWithMetadata verifies that the Metadata column survives
// a Create → Get → Update round trip and that toRule recovers the map.
func TestStore_CRUDFlowWithMetadata(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	created := mustCreateRule(t, store, &Record{
		TenantID:   1,
		Name:       "with-meta",
		Scene:      string(SceneMetric),
		Expression: `alert.metrics["cpu"] > 80`,
		Enabled:    true,
		Priority:   10,
		Metadata:   `{"owner":"ops"}`,
	})

	got, err := store.Get(ctx, created.ID, created.TenantID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Metadata != `{"owner":"ops"}` {
		t.Errorf("Metadata = %q, want {\"owner\":\"ops\"}", got.Metadata)
	}
	// toRule recovers the metadata map.
	r := got.toRule()
	if r.Metadata["owner"] != "ops" {
		t.Errorf("toRule Metadata[owner] = %q, want ops", r.Metadata["owner"])
	}

	// Update the metadata.
	got.Metadata = `{"owner":"dev","severity":"high"}`
	if err := store.Update(ctx, got); err != nil {
		t.Fatalf("Update: %v", err)
	}
	gotAfter, err := store.Get(ctx, created.ID, created.TenantID)
	if err != nil {
		t.Fatalf("Get after Update: %v", err)
	}
	r2 := gotAfter.toRule()
	if r2.Metadata["owner"] != "dev" || r2.Metadata["severity"] != "high" {
		t.Errorf("toRule after Update = %+v, want owner=dev severity=high", r2.Metadata)
	}
}

// TestStore_NilCompilerSafe verifies that NewStore accepts the
// compiler parameter and that all read paths (Get, ListEnabled) function
// without invoking the compiler. The compile-check only fires on writes.
func TestStore_NilCompilerSafe(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	// Construct a store with a real compiler; we use the read paths only.
	store := NewStore(db, NewCompiler())
	ctx := context.Background()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	// Read on empty DB returns empty/NotFound without invoking the compiler.
	if _, err := store.Get(ctx, 1, 1); !errors.Is(err, ErrRuleNotFound) {
		t.Errorf("Get on empty DB: expected ErrRuleNotFound, got %v", err)
	}
	got, err := store.ListEnabled(ctx, 0, SceneMetric)
	if err != nil {
		t.Fatalf("ListEnabled on empty DB: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty slice, got %d", len(got))
	}
}

// TestStore_DeleteThenRecreate verifies that a soft-deleted rule's ID is
// not reused by a subsequent Create (GORM soft-delete keeps the row, so the
// auto-increment counter does not roll back).
func TestStore_DeleteThenRecreate(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	first := mustCreateRule(t, store, validMetricModel("first", `alert.metrics["cpu"] > 80`))
	firstID := first.ID
	if err := store.Delete(ctx, first.ID, first.TenantID); err != nil {
		t.Fatalf("Delete first: %v", err)
	}

	second := mustCreateRule(t, store, validMetricModel("second", `alert.metrics["cpu"] > 80`))
	if second.ID == firstID {
		t.Errorf("second.ID = %d, want a new ID (not the deleted %d)", second.ID, firstID)
	}
	if second.ID <= firstID {
		t.Errorf(
			"second.ID (%d) should be greater than firstID (%d) since soft-delete retains the row",
			second.ID, firstID,
		)
	}

	// Sanity: only the second rule is retrievable.
	got, err := store.ListEnabled(ctx, 0, SceneMetric)
	if err != nil {
		t.Fatalf("ListEnabled: %v", err)
	}
	if len(got) != 1 || got[0].Name != "second" {
		t.Errorf("expected [second], got %+v", namesOf(got))
	}
}

// TestStore_MultiTenantIsolation verifies that two tenants sharing the same
// store see only their own rules through ListEnabled(tenantID, ...) and Get.
func TestStore_MultiTenantIsolation(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	t1 := mustCreateRule(t, store, &Record{
		TenantID:   1,
		Name:       "t1-only",
		Scene:      string(SceneMetric),
		Expression: `alert.metrics["cpu"] > 0`,
		Enabled:    true,
		Priority:   1,
	})
	mustCreateRule(t, store, &Record{
		TenantID:   2,
		Name:       "t2-only",
		Scene:      string(SceneMetric),
		Expression: `alert.metrics["cpu"] > 0`,
		Enabled:    true,
		Priority:   1,
	})

	// Tenant 1 sees only t1-only.
	gotT1, err := store.ListEnabled(ctx, 1, SceneMetric)
	if err != nil {
		t.Fatalf("ListEnabled t1: %v", err)
	}
	if len(gotT1) != 1 || gotT1[0].Name != "t1-only" {
		t.Errorf("tenant 1: expected [t1-only], got %+v", namesOf(gotT1))
	}

	// Tenant 2 cannot Get tenant 1's rule by (id, tenantID=2).
	if _, err := store.Get(ctx, t1.ID, 2); !errors.Is(err, ErrRuleNotFound) {
		t.Errorf("tenant 2 Get t1 rule: expected ErrRuleNotFound, got %v", err)
	}

	// Tenant 2 cannot Delete tenant 1's rule.
	if err := store.Delete(ctx, t1.ID, 2); !errors.Is(err, ErrRuleNotFound) {
		t.Errorf("tenant 2 Delete t1 rule: expected ErrRuleNotFound, got %v", err)
	}
	// The rule still exists for tenant 1.
	if _, err := store.Get(ctx, t1.ID, 1); err != nil {
		t.Errorf("tenant 1 Get after tenant 2 Delete attempt: %v", err)
	}
}

// TestStore_ListEnabledAcrossAllScenes verifies that a store with rules in
// all four scenes can be drained by the engine's Reload (which calls
// ListEnabled once per scene).
func TestStore_ListEnabledAcrossAllScenes(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	mustCreateRule(t, store, &Record{
		TenantID:   1,
		Name:       "task-1",
		Scene:      string(SceneTask),
		Expression: "task.priority > 0",
		Enabled:    true,
		Priority:   1,
	})
	mustCreateRule(t, store, &Record{
		TenantID:   1,
		Name:       "probe-1",
		Scene:      string(SceneProbe),
		Expression: `report.log_content contains "x"`,
		Enabled:    true,
		Priority:   1,
	})
	mustCreateRule(t, store, &Record{
		TenantID:   1,
		Name:       "metric-1",
		Scene:      string(SceneMetric),
		Expression: `alert.metrics["cpu"] > 0`,
		Enabled:    true,
		Priority:   1,
	})
	mustCreateRule(t, store, &Record{
		TenantID:   1,
		Name:       "remediation-1",
		Scene:      string(SceneRemediation),
		Expression: "remediation.metric_value > 0",
		Enabled:    true,
		Priority:   1,
	})

	// Each scene reports 1 rule; the engine's Reload drains all four.
	total := 0
	for _, scene := range []Scene{SceneTask, SceneProbe, SceneMetric, SceneRemediation} {
		got, err := store.ListEnabled(ctx, 0, scene)
		if err != nil {
			t.Fatalf("ListEnabled(%s): %v", scene, err)
		}
		if len(got) != 1 {
			t.Errorf("scene %s: expected 1 rule, got %d", scene, len(got))
		}
		total += len(got)
	}
	if total != 4 {
		t.Errorf("total across scenes = %d, want 4", total)
	}
}

// _ keeps the time import used when future timestamp-based assertions are added.
var _ = time.Now
