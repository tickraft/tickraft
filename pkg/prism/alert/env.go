// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package alert

import (
	"github.com/tickraft/tickraft/pkg/asset"
	"github.com/tickraft/tickraft/pkg/expr"
)

// AlertEnv is the single evaluation environment of the alert rule
// engine. Top-level scalars are tagged struct fields so unknown names
// and type errors surface at compile time; the asset domain object is
// an expr.DomainMap so `asset.id` and `asset["id"]` are equivalent
// (field-set contract: id, name, type, tags).
//
// Metrics is always a non-nil map: on non-metric events it is empty,
// so a metric rule such as metrics["cpu"] > 90 naturally evaluates to
// false instead of failing.
//
//nolint:revive // AlertEnv is the design-doc contract name, kept for cross-repo symmetry with tickraft-x
type AlertEnv struct {
	// Type is the alert category: "metric", "log", or "status".
	Type string `expr:"type"`
	// Severity is the primary violation's unified severity
	// (info / warning / error / critical).
	Severity string `expr:"severity"`
	// Source is the alert origin: IP for log alerts, probe point
	// identifier for probes.
	Source string `expr:"source"`
	// Keyword is the matched log keyword on log alerts.
	Keyword string `expr:"keyword"`
	// Content is the matched log line on log alerts.
	Content string `expr:"content"`
	// Metrics holds the related metric values on metric alerts.
	Metrics map[string]float64 `expr:"metrics"`
	// Asset describes the asset that triggered the alert.
	Asset expr.DomainMap `expr:"asset"`
}

// ExampleEnv returns a fully populated sample of AlertEnv. It is the
// baseline for entry validation (expr.Validate) and the /expr/validate
// endpoint: every documented field is present with a representative
// value.
func ExampleEnv() AlertEnv {
	return AlertEnv{
		Type:     "metric",
		Severity: "critical",
		Source:   "10.0.0.1",
		Keyword:  "fatal",
		Content:  "fatal: out of memory",
		Metrics:  map[string]float64{"cpu": 91.5},
		Asset: expr.DomainMap{
			"id":   int64(42),
			"name": "web-1",
			"type": "host",
			"tags": map[string]string{"env": "prod"},
		},
	}
}

// buildAlertEnv projects an alert.Event into AlertEnv, enriching the
// asset domain from res when the lookup succeeded. The asset id always
// comes from the event so it is present even without enrichment.
func buildAlertEnv(evt Event, res *asset.Asset) AlertEnv {
	env := AlertEnv{
		Type:    string(evt.Type),
		Metrics: map[string]float64{},
		Asset: expr.DomainMap{
			"id":   evt.AssetID,
			"name": "",
			"type": "",
			"tags": map[string]string{},
		},
	}
	if primary, ok := evt.primaryViolation(); ok {
		env.Severity = primary.Severity
		env.Source = primary.Source
		if primary.Metric != nil && primary.Metric.Metrics != nil {
			env.Metrics = primary.Metric.Metrics
		}
		if primary.Log != nil {
			env.Keyword = primary.Log.Keyword
			env.Content = primary.Log.Content
		}
	}
	if res != nil {
		env.Asset["name"] = res.Name
		env.Asset["type"] = string(res.AssetType)
		env.Asset["tags"] = asset.TagsFromMetadata(res.Metadata)
	}
	return env
}
