// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package alert

import (
	"context"
	"errors"
	"testing"

	"github.com/tickraft/tickraft/pkg/asset"
	"github.com/tickraft/tickraft/pkg/errdefs"
	"github.com/tickraft/tickraft/pkg/types"
)

// fakeAssetStore is an asset.Getter whose GetByID resolves from an
// in-memory map.
type fakeAssetStore struct {
	byID map[int64]*asset.Asset
}

func (f fakeAssetStore) GetByID(_ context.Context, id int64) (*asset.Asset, error) {
	if a, ok := f.byID[id]; ok {
		return a, nil
	}
	return nil, errdefs.ErrNotFound
}

// failingAssetStore simulates an asset backend outage: GetByID errors.
type failingAssetStore struct{}

func (failingAssetStore) GetByID(_ context.Context, _ int64) (*asset.Asset, error) {
	return nil, errors.New("asset backend unavailable")
}

// metricEvent builds a metric alert event for matcher tests.
func metricEvent(assetID, tenantID int64, metrics map[string]float64) Event {
	return Event{
		Type:     TypeMetric,
		AssetID:  assetID,
		TenantID: tenantID,
		Violations: []Violation{{
			Kind:     ViolationKindMetric,
			Severity: "critical",
			Source:   "10.0.0.1",
			Metric:   &MetricContext{Name: "cpu", Value: 95, Metrics: metrics},
		}},
	}
}

// TestAlertMatcherDefaultAllow pins the default-allow contract: with no
// rules loaded the matcher forwards every alert so the rule engine never
// silently drops alerts simply because no rules exist.
func TestAlertMatcherDefaultAllow(t *testing.T) {
	engine := NewEngine(nil) // nil logger is replaced with a no-op
	m := NewAlertMatcher(engine, nil)
	result := m.Match(context.Background(), metricEvent(1, 1, nil))
	if !result.Forward {
		t.Error("Forward with empty rule set = false, want true (default-allow)")
	}
	if result.Violations != nil {
		t.Errorf("Violations with empty rule set = %v, want nil", result.Violations)
	}
}

// TestAlertMatcherFiltersDispatch verifies the pre-filter role: an alert
// is forwarded only when at least one rule matches.
func TestAlertMatcherFiltersDispatch(t *testing.T) {
	engine := loadRules(t,
		Rule{ID: 1, Expression: `metrics["cpu"] > 90`, Enabled: true},
	)
	m := NewAlertMatcher(engine, nil)

	if !m.Match(context.Background(), metricEvent(1, 1, map[string]float64{"cpu": 95})).Forward {
		t.Error("Forward(cpu=95) = false, want true (rule matched)")
	}
	if m.Match(context.Background(), metricEvent(1, 1, map[string]float64{"cpu": 50})).Forward {
		t.Error("Forward(cpu=50) = true, want false (no rule matched)")
	}
}

// TestAlertMatcherEnrichment verifies the asset store enrichment of the
// evaluation environment: rules can reference asset.name/type/tags when
// the asset resolves, and degrade to empty values when it does not.
func TestAlertMatcherEnrichment(t *testing.T) {
	engine := loadRules(t,
		Rule{ID: 1, Expression: `asset.name == "web-1" && asset.tags["env"] == "prod"`, Enabled: true},
	)
	store := fakeAssetStore{byID: map[int64]*asset.Asset{
		7: {ID: 7, Name: "web-1", AssetType: types.AssetType("host"), Metadata: `{"env":"prod"}`},
	}}
	m := NewAlertMatcher(engine, store)

	if !m.Match(context.Background(), metricEvent(7, 1, nil)).Forward {
		t.Error("Forward with enriched asset = false, want true")
	}

	// Unknown asset id: enrichment fails non-fatally, the rule compares
	// against empty name/tags and does not match.
	if m.Match(context.Background(), metricEvent(999, 1, nil)).Forward {
		t.Error("Forward with unknown asset = true, want false (empty asset fields)")
	}
}

// TestAlertMatcherStoreErrorNonFatal verifies that an asset store outage
// degrades to an un-enriched environment instead of failing matching.
func TestAlertMatcherStoreErrorNonFatal(t *testing.T) {
	engine := loadRules(t,
		Rule{ID: 1, Expression: `metrics["cpu"] > 90`, Enabled: true},
	)
	m := NewAlertMatcher(engine, failingAssetStore{})

	if !m.Match(context.Background(), metricEvent(7, 1, map[string]float64{"cpu": 95})).Forward {
		t.Error("Forward during asset store outage = false, want true (enrichment is best-effort)")
	}
}

// TestAlertMatcherViolations verifies the structured-violation outcome
// of the single evaluation pass, including the projection from the
// event's primary violation into the env (severity/source preserved on
// the output).
func TestAlertMatcherViolations(t *testing.T) {
	engine := loadRules(t,
		Rule{ID: 1, Expression: `metrics["cpu"] > 90`, Enabled: true},
	)
	m := NewAlertMatcher(engine, nil)

	result := m.Match(context.Background(), metricEvent(7, 1, map[string]float64{"cpu": 95}))
	if !result.Forward {
		t.Fatal("Forward = false, want true (rule matched)")
	}
	if len(result.Violations) != 1 {
		t.Fatalf("Violations = %d, want 1", len(result.Violations))
	}
	v := result.Violations[0]
	if v.Severity != "critical" {
		t.Errorf("Severity = %q, want critical (projected from event)", v.Severity)
	}
	if v.Source != "10.0.0.1" {
		t.Errorf("Source = %q, want 10.0.0.1 (projected from event)", v.Source)
	}
	if v.Metric == nil || v.Metric.Name != "cpu" || v.Metric.Value != 95 {
		t.Errorf("Metric = %+v, want cpu=95", v.Metric)
	}
}
