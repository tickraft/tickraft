// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package prism

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/tickraft/tickraft/pkg/api/handler"
	"github.com/tickraft/tickraft/pkg/api/handler/alert"
	"github.com/tickraft/tickraft/pkg/db"
	"github.com/tickraft/tickraft/pkg/errdefs"
	prismalert "github.com/tickraft/tickraft/pkg/prism/alert"
	"github.com/tickraft/tickraft/pkg/prism/rule"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// ctx is a reusable background context for service-layer tests.
var ctx = context.Background()

// closeUnderlyingDB closes the underlying *sql.DB of a gorm.DB so that test
// fixtures release their in-memory SQLite database cleanly.
func closeUnderlyingDB(t *testing.T, dbc *gorm.DB) {
	t.Helper()
	sqlDB, err := dbc.DB()
	if err != nil {
		t.Fatalf("get underlying sql.DB: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close sql.DB: %v", err)
	}
}

// assertErrorCoder verifies that err is non-nil, matches the expected sentinel
// via errors.Is, and reports the expected HTTP status and business code
// through the errdefs.ErrorCoder interface.
func assertErrorCoder(t *testing.T, err error, sentinel error, wantStatus, wantCode int) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error matching %v, got nil", sentinel)
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected errors.Is(err, %v) = true, got err=%v", sentinel, err)
	}
	var ec errdefs.ErrorCoder
	if !errors.As(err, &ec) {
		t.Fatalf("expected err to implement errdefs.ErrorCoder, got %T", err)
	}
	if ec.HTTPStatus() != wantStatus {
		t.Errorf("HTTPStatus = %d, want %d", ec.HTTPStatus(), wantStatus)
	}
	if ec.Code() != wantCode {
		t.Errorf("Code = %d, want %d", ec.Code(), wantCode)
	}
}

// assertServiceErrorStatus verifies that err is non-nil, implements
// errdefs.ErrorCoder, and reports the expected HTTP status and business code.
// Unlike assertErrorCoder, it does not require a sentinel error match, which
// is needed for validation cases that create a fresh serviceError rather than
// returning a known sentinel.
func assertServiceErrorStatus(t *testing.T, err error, wantStatus, wantCode int) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	var ec errdefs.ErrorCoder
	if !errors.As(err, &ec) {
		t.Fatalf("expected err to implement errdefs.ErrorCoder, got %T", err)
	}
	if ec.HTTPStatus() != wantStatus {
		t.Errorf("HTTPStatus = %d, want %d", ec.HTTPStatus(), wantStatus)
	}
	if ec.Code() != wantCode {
		t.Errorf("Code = %d, want %d", ec.Code(), wantCode)
	}
}

// setupPrismService creates an AlertService backed by real GORM rule and
// record stores using an in-memory SQLite database. A non-nil rule.Engine is
// passed so the Reload path after CRUD operations is exercised; rule-to-engine
// matching is covered by the integration tests in tests/integration/.
func setupPrismService(t *testing.T) (*AlertService, *rule.Store, prismalert.RecordStore, func()) {
	t.Helper()
	gdb, err := db.Open(ctx, db.Config{Driver: "sqlite3", Addr: ":memory:"})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	ruleStore := rule.NewStore(gdb, rule.NewCompiler())
	if err := ruleStore.Migrate(ctx); err != nil {
		closeUnderlyingDB(t, gdb)
		t.Fatalf("migrate rule table: %v", err)
	}
	if err := prismalert.Migrate(ctx, gdb); err != nil {
		closeUnderlyingDB(t, gdb)
		t.Fatalf("auto migrate: %v", err)
	}
	recordStore := prismalert.NewRecordStore(gdb)
	ruleEng := rule.NewEngine(zap.NewNop())
	svc := NewAlertService(ruleStore, recordStore, ruleEng)
	cleanup := func() { closeUnderlyingDB(t, gdb) }
	return svc, ruleStore, recordStore, cleanup
}

// --- violationToRecord ---

// TestViolationToRecord covers the record-building helper that derives the
// rule name, severity, value, and message from a violation. This replaces the
// former lookupRule/buildViolationMessage helpers whose logic was merged into
// violationToRecord.
func TestViolationToRecord(t *testing.T) {
	t.Run("metric violation derives rule name, value, and default severity", func(t *testing.T) {
		v := prismalert.Violation{
			Kind:   prismalert.ViolationKindMetric,
			Metric: &prismalert.MetricContext{Name: "cpu_usage", Value: 95.0, Threshold: 90.0},
		}
		rec := prismalert.ViolationToRecord(v, time.Now())
		if rec.RuleID != 0 {
			t.Errorf("RuleID = %d, want 0", rec.RuleID)
		}
		if rec.RuleName != "cpu_usage" {
			t.Errorf("RuleName = %q, want %q", rec.RuleName, "cpu_usage")
		}
		if rec.Severity != "warning" {
			t.Errorf("Severity = %q, want %q (default)", rec.Severity, "warning")
		}
		if rec.Value != 95.0 {
			t.Errorf("Value = %v, want 95.0", rec.Value)
		}
		if rec.Status != "firing" {
			t.Errorf("Status = %q, want %q", rec.Status, "firing")
		}
	})

	t.Run("empty violation yields defaults", func(t *testing.T) {
		rec := prismalert.ViolationToRecord(prismalert.Violation{}, time.Now())
		if rec.RuleID != 0 || rec.RuleName != "" || rec.Severity != "warning" {
			t.Errorf("empty violation should yield defaults, got id=%d name=%q severity=%q",
				rec.RuleID, rec.RuleName, rec.Severity)
		}
	})

	t.Run("preserves provided severity and source rule name", func(t *testing.T) {
		v := prismalert.Violation{
			Severity: "critical",
			Source:   "cpu_usage",
		}
		rec := prismalert.ViolationToRecord(v, time.Now())
		if rec.Severity != "critical" {
			t.Errorf("Severity = %q, want %q", rec.Severity, "critical")
		}
		if rec.RuleName != "cpu_usage" {
			t.Errorf("RuleName = %q, want %q", rec.RuleName, "cpu_usage")
		}
	})

	t.Run("log violation derives rule name from keyword", func(t *testing.T) {
		v := prismalert.Violation{
			Severity: "error",
			Log:      &prismalert.LogContext{Keyword: "panic"},
		}
		rec := prismalert.ViolationToRecord(v, time.Now())
		if rec.RuleName != "panic" {
			t.Errorf("RuleName = %q, want %q", rec.RuleName, "panic")
		}
	})

	t.Run("default message contains rule name and kind", func(t *testing.T) {
		v := prismalert.Violation{
			Kind:   prismalert.ViolationKindMetric,
			Metric: &prismalert.MetricContext{Name: "cpu_usage"},
		}
		rec := prismalert.ViolationToRecord(v, time.Now())
		if !strings.Contains(rec.Message, "cpu_usage") {
			t.Errorf("expected message to contain rule name, got %q", rec.Message)
		}
		if !strings.Contains(rec.Message, string(prismalert.ViolationKindMetric)) {
			t.Errorf("expected message to contain kind, got %q", rec.Message)
		}
	})
}

// --- RecordAlert ---

func TestRecordAlert(t *testing.T) {
	t.Run("persists record with metric as rule name", func(t *testing.T) {
		_, _, recordStore, cleanup := setupPrismService(t)
		defer cleanup()

		evt := prismalert.Event{
			Type:       prismalert.TypeMetric,
			AssetID:    7,
			TenantID:   1,
			Timestamp:  time.Now(),
			Violations: []prismalert.Violation{{Kind: prismalert.ViolationKindMetric, Metric: &prismalert.MetricContext{Name: "cpu_usage", Value: 95.0, Threshold: 90.0}}},
		}
		if err := prismalert.RecordAlert(ctx, recordStore, evt); err != nil {
			t.Fatalf("RecordAlert: %v", err)
		}

		records, total, err := recordStore.List(ctx, 1, 10, prismalert.RecordFilter{})
		if err != nil {
			t.Fatalf("list records: %v", err)
		}
		if total != 1 || len(records) != 1 {
			t.Fatalf("expected 1 record, got total=%d len=%d", total, len(records))
		}
		got := records[0]
		// violationToRecord derives rule name from the metric name and
		// defaults severity to "warning" since rule metadata is not
		// surfaced through the OnAlert callback.
		if got.RuleID != 0 {
			t.Errorf("RuleID = %d, want 0", got.RuleID)
		}
		if got.RuleName != "cpu_usage" {
			t.Errorf("RuleName = %q, want %q", got.RuleName, "cpu_usage")
		}
		if got.Severity != "warning" {
			t.Errorf("Severity = %q, want %q", got.Severity, "warning")
		}
		if got.Value != 95.0 {
			t.Errorf("Value = %v, want 95.0", got.Value)
		}
		if got.Status != "firing" {
			t.Errorf("Status = %q, want %q", got.Status, "firing")
		}
		if got.Message == "" {
			t.Error("Message should not be empty")
		}
		if got.TriggeredAt.IsZero() {
			t.Error("TriggeredAt should be set")
		}
	})

	t.Run("uses zero RuleID for unmatched metric", func(t *testing.T) {
		_, _, recordStore, cleanup := setupPrismService(t)
		defer cleanup()

		evt := prismalert.Event{
			Type:       prismalert.TypeMetric,
			AssetID:    1,
			TenantID:   1,
			Timestamp:  time.Now(),
			Violations: []prismalert.Violation{{Kind: prismalert.ViolationKindMetric, Metric: &prismalert.MetricContext{Name: "nonexistent"}}},
		}
		if err := prismalert.RecordAlert(ctx, recordStore, evt); err != nil {
			t.Fatalf("RecordAlert: %v", err)
		}

		records, _, err := recordStore.List(ctx, 1, 10, prismalert.RecordFilter{})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(records) != 1 {
			t.Fatalf("expected 1 record, got %d", len(records))
		}
		if records[0].RuleID != 0 {
			t.Errorf("RuleID = %d, want 0 for unmatched rule", records[0].RuleID)
		}
		if records[0].RuleName != "nonexistent" {
			t.Errorf("RuleName = %q, want %q", records[0].RuleName, "nonexistent")
		}
	})

	t.Run("no-op when recordStore is nil", func(t *testing.T) {
		evt := prismalert.Event{
			Type:       prismalert.TypeMetric,
			Violations: []prismalert.Violation{{Kind: prismalert.ViolationKindMetric, Metric: &prismalert.MetricContext{Name: "cpu_usage"}}},
		}
		if err := prismalert.RecordAlert(ctx, nil, evt); err != nil {
			t.Errorf("RecordAlert with nil recordStore should return nil, got %v", err)
		}
	})

	t.Run("uses now when timestamp is zero", func(t *testing.T) {
		_, _, recordStore, cleanup := setupPrismService(t)
		defer cleanup()

		before := time.Now()
		evt := prismalert.Event{
			Type:       prismalert.TypeMetric,
			Violations: []prismalert.Violation{{Kind: prismalert.ViolationKindMetric, Metric: &prismalert.MetricContext{Name: "cpu_usage"}}},
			// Timestamp left zero — RecordAlert should populate it with time.Now().
		}
		if err := prismalert.RecordAlert(ctx, recordStore, evt); err != nil {
			t.Fatalf("RecordAlert: %v", err)
		}
		records, _, err := recordStore.List(ctx, 1, 10, prismalert.RecordFilter{})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(records) != 1 {
			t.Fatalf("expected 1 record, got %d", len(records))
		}
		if records[0].TriggeredAt.Before(before) {
			t.Errorf("TriggeredAt = %v, want >= %v", records[0].TriggeredAt, before)
		}
	})
}

// --- AlertService CRUD ---

func TestPrismAlertServiceCRUD(t *testing.T) {
	svc, _, _, cleanup := setupPrismService(t)
	defer cleanup()

	var ruleID int64

	t.Run("Create nil request returns ErrInvalidRequest", func(t *testing.T) {
		_, err := svc.CreateRule(ctx, nil)
		assertErrorCoder(t, err, handler.ErrInvalidRequest, http.StatusBadRequest, errdefs.CodeBadRequest)
	})

	t.Run("Create with empty name returns 400", func(t *testing.T) {
		_, err := svc.CreateRule(ctx, &alert.Rule{Scene: "metric", Expression: "alert.metrics[\"cpu\"] > 90"})
		assertServiceErrorStatus(t, err, http.StatusBadRequest, errdefs.CodeBadRequest)
	})

	t.Run("Create with empty scene returns 400", func(t *testing.T) {
		_, err := svc.CreateRule(ctx, &alert.Rule{Name: "rule", Expression: "alert.metrics[\"cpu\"] > 90"})
		assertServiceErrorStatus(t, err, http.StatusBadRequest, errdefs.CodeBadRequest)
	})

	t.Run("Create with empty expression returns 400", func(t *testing.T) {
		_, err := svc.CreateRule(ctx, &alert.Rule{Name: "rule", Scene: "metric"})
		assertServiceErrorStatus(t, err, http.StatusBadRequest, errdefs.CodeBadRequest)
	})

	t.Run("Create persists rule and assigns ID", func(t *testing.T) {
		created, err := svc.CreateRule(ctx, &alert.Rule{
			Name:       "cpu-high",
			Scene:      "metric",
			Expression: `alert.metrics["cpu"] > 90`,
			Priority:   10,
			Enabled:    true,
		})
		if err != nil {
			t.Fatalf("CreateRule: %v", err)
		}
		if created.ID == 0 {
			t.Fatal("expected non-zero ID after Create")
		}
		ruleID = created.ID
		if created.Scene != "metric" {
			t.Errorf("Scene = %q, want %q", created.Scene, "metric")
		}
		if created.Expression != `alert.metrics["cpu"] > 90` {
			t.Errorf("Expression = %q, want %q", created.Expression, `alert.metrics["cpu"] > 90`)
		}
		if created.Priority != 10 {
			t.Errorf("Priority = %d, want %d", created.Priority, 10)
		}
	})

	t.Run("Get returns the created rule", func(t *testing.T) {
		got, err := svc.GetRule(ctx, ruleID)
		if err != nil {
			t.Fatalf("GetRule: %v", err)
		}
		if got.ID != ruleID {
			t.Errorf("ID = %d, want %d", got.ID, ruleID)
		}
		if got.Name != "cpu-high" {
			t.Errorf("Name = %q, want %q", got.Name, "cpu-high")
		}
	})

	t.Run("Get non-existent returns ErrRuleNotFound", func(t *testing.T) {
		_, err := svc.GetRule(ctx, 99999)
		assertErrorCoder(t, err, handler.ErrRuleNotFound, http.StatusNotFound, errdefs.CodeNotFound)
	})

	t.Run("List returns at least one rule", func(t *testing.T) {
		rules, total, err := svc.ListRules(ctx, 1, 10)
		if err != nil {
			t.Fatalf("ListRules: %v", err)
		}
		if total < 1 {
			t.Errorf("total = %d, want >= 1", total)
		}
		if len(rules) == 0 {
			t.Fatal("expected at least 1 rule")
		}
	})

	t.Run("Update nil request returns ErrInvalidRequest", func(t *testing.T) {
		_, err := svc.UpdateRule(ctx, ruleID, nil)
		assertErrorCoder(t, err, handler.ErrInvalidRequest, http.StatusBadRequest, errdefs.CodeBadRequest)
	})

	t.Run("Update non-existent returns ErrRuleNotFound", func(t *testing.T) {
		_, err := svc.UpdateRule(ctx, 99999, &alert.Rule{Name: "x", Scene: "metric", Expression: "true"})
		assertErrorCoder(t, err, handler.ErrRuleNotFound, http.StatusNotFound, errdefs.CodeNotFound)
	})

	t.Run("Update persists new name", func(t *testing.T) {
		updated, err := svc.UpdateRule(ctx, ruleID, &alert.Rule{
			Name:       "cpu-critical",
			Scene:      "metric",
			Expression: `alert.metrics["cpu"] > 95`,
			Priority:   20,
			Enabled:    true,
		})
		if err != nil {
			t.Fatalf("UpdateRule: %v", err)
		}
		if updated.Name != "cpu-critical" {
			t.Errorf("Name = %q, want %q", updated.Name, "cpu-critical")
		}
		if updated.Expression != `alert.metrics["cpu"] > 95` {
			t.Errorf("Expression = %q, want %q", updated.Expression, `alert.metrics["cpu"] > 95`)
		}
		if updated.Priority != 20 {
			t.Errorf("Priority = %d, want %d", updated.Priority, 20)
		}
	})

	t.Run("Delete removes the rule", func(t *testing.T) {
		if err := svc.DeleteRule(ctx, ruleID); err != nil {
			t.Fatalf("DeleteRule: %v", err)
		}
		_, err := svc.GetRule(ctx, ruleID)
		assertErrorCoder(t, err, handler.ErrRuleNotFound, http.StatusNotFound, errdefs.CodeNotFound)
	})

	t.Run("Delete non-existent returns ErrRuleNotFound", func(t *testing.T) {
		err := svc.DeleteRule(ctx, 99999)
		assertErrorCoder(t, err, handler.ErrRuleNotFound, http.StatusNotFound, errdefs.CodeNotFound)
	})
}
