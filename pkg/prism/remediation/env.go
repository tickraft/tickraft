// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package remediation

import (
	"strconv"

	"github.com/tickraft/tickraft/pkg/asset"
	"github.com/tickraft/tickraft/pkg/event"
	"github.com/tickraft/tickraft/pkg/expr"
)

// Domain map keys of the closed field sets. Centralizing them keeps the
// contract keys greppable and prevents typo drift between
// ExampleEnv and buildRemediationEnv.
const (
	domainID   = "id"
	domainKey  = "key"
	domainName = "name"
	domainType = "type"
)

// RemediationEnv is the evaluation environment of remediation trigger
// conditions (the former EventContext). Top-level scalars are tagged
// struct fields so unknown names and type errors surface at compile
// time; the metric/status/asset domains are expr.DomainMap closed
// field sets so `metric.value` and `metric["value"]` are equivalent
// and field typos are rejected by entry validation.
//
// Field-set contracts:
//   - metric: name, value
//   - status: previous, current
//   - asset:  id, key, name, type, tags
//
// asset.id/key always come from the event payload (key is only carried
// by status_change triggers); asset.name/type/tags are enriched from
// the asset store when the lookup succeeds.
//
//nolint:revive // RemediationEnv is the design-doc contract name, kept identical to tickraft-x for cross-repo symmetry
type RemediationEnv struct {
	// Trigger is the trigger category: "metric", "log", or
	// "status_change".
	Trigger string `expr:"trigger"`
	// Level is the log severity on log triggers.
	Level string `expr:"level"`
	// Keyword is the matched log keyword on log triggers.
	Keyword string `expr:"keyword"`
	// Content is the matched log line on log triggers.
	Content string `expr:"content"`
	// Source is the event origin (log source IP).
	Source string `expr:"source"`
	// Threshold is the configured threshold on metric triggers.
	Threshold float64 `expr:"threshold"`
	// Metric holds the metric name and observed value on metric
	// triggers.
	Metric expr.DomainMap `expr:"metric"`
	// Status holds the previous and current asset status on
	// status_change triggers.
	Status expr.DomainMap `expr:"status"`
	// Asset describes the asset that triggered the event.
	Asset expr.DomainMap `expr:"asset"`
}

// ExampleEnv returns a fully populated sample of RemediationEnv. It is
// the baseline for entry validation (expr.Validate) and the
// /expr/validate endpoint.
func ExampleEnv() RemediationEnv {
	return RemediationEnv{
		Trigger:   "metric",
		Level:     "error",
		Keyword:   "oom",
		Content:   "OutOfMemoryError: Java heap space",
		Source:    "10.0.0.1",
		Threshold: 90,
		Metric:    expr.DomainMap{domainName: "cpu", "value": 95.5},
		Status:    expr.DomainMap{"previous": "normal", "current": "abnormal"},
		Asset: expr.DomainMap{
			domainID:   int64(42),
			domainKey:  "web-1",
			domainName: "web-1",
			domainType: "host",
			"tags":     map[string]string{"env": "prod"},
		},
	}
}

// ValidateExpression checks a trigger condition expression against the
// RemediationEnv contract via the kernel's compile + domain-key +
// sample-evaluation pipeline. It is the single validation source shared
// by the rule write path and the /expr/validate endpoint.
func ValidateExpression(expression string) error {
	return expr.Validate(ExampleEnv(), expression)
}

// triggerEvent is the raw event payload projection consumed by the
// Engine for rule loading, gating, records, and env construction. It
// carries plumbing fields (tenant id, per-trigger raw data) that are
// not expression variables; the expression surface is RemediationEnv.
type triggerEvent struct {
	// Trigger is the trigger category identifier.
	Trigger string
	// TenantID scopes rule loading to the event's tenant.
	TenantID int64
	// AssetID is the numeric asset identifier from the payload.
	AssetID int64
	// AssetKey is the tenant-unique asset key (status_change only).
	AssetKey string
	// Metric fields (metric trigger).
	MetricName  string
	MetricValue float64
	Threshold   float64
	// Log fields (log trigger).
	Level   string
	Keyword string
	Content string
	Source  string
	// Status fields (status_change trigger).
	PrevStatus string
	CurrStatus string
}

// buildRemediationEnv projects a triggerEvent into a RemediationEnv,
// enriching the asset domain from res when the lookup succeeded. The
// asset id/key always come from the event so they are present even
// without enrichment.
func buildRemediationEnv(te triggerEvent, res *asset.Asset) RemediationEnv {
	env := RemediationEnv{
		Trigger:   te.Trigger,
		Level:     te.Level,
		Keyword:   te.Keyword,
		Content:   te.Content,
		Source:    te.Source,
		Threshold: te.Threshold,
		Metric:    expr.DomainMap{domainName: te.MetricName, "value": te.MetricValue},
		Status:    expr.DomainMap{"previous": te.PrevStatus, "current": te.CurrStatus},
		Asset: expr.DomainMap{
			domainID:   te.AssetID,
			domainKey:  te.AssetKey,
			domainName: "",
			domainType: "",
			"tags":     map[string]string{},
		},
	}
	if res != nil {
		env.Asset[domainName] = res.Name
		env.Asset[domainType] = string(res.AssetType)
		env.Asset["tags"] = asset.TagsFromMetadata(res.Metadata)
	}
	return env
}

// parseID parses a decimal event payload identifier. The bool result
// is false for malformed values: the caller must skip the event rather
// than fall back to 0, because asset id 0 matches every global rule
// (fix for D-10).
func parseID(v string) (int64, bool) {
	id, err := strconv.ParseInt(v, 10, 64)
	if err != nil || id < 0 {
		return 0, false
	}
	return id, true
}

// metricPayloadToTrigger converts a metric-exceeded event payload into
// a triggerEvent. The bool result is false when the payload carries a
// malformed tenant or asset identifier.
func metricPayloadToTrigger(ev event.Event[event.MetricExceededPayload]) (triggerEvent, bool) {
	p := ev.Payload
	assetID, ok := parseID(p.AssetID)
	if !ok {
		return triggerEvent{}, false
	}
	tenantID, ok := parseID(p.TenantID)
	if !ok {
		return triggerEvent{}, false
	}
	return triggerEvent{
		Trigger:     string(TriggerMetric),
		AssetID:     assetID,
		TenantID:    tenantID,
		MetricName:  p.MetricName,
		MetricValue: p.MetricValue,
		Threshold:   p.Threshold,
	}, true
}

// logPayloadToTrigger converts a log-matched event payload into a
// triggerEvent. The bool result is false when the payload carries a
// malformed tenant or asset identifier.
func logPayloadToTrigger(ev event.Event[event.LogMatchedPayload]) (triggerEvent, bool) {
	p := ev.Payload
	assetID, ok := parseID(p.AssetID)
	if !ok {
		return triggerEvent{}, false
	}
	tenantID, ok := parseID(p.TenantID)
	if !ok {
		return triggerEvent{}, false
	}
	return triggerEvent{
		Trigger:  string(TriggerLog),
		AssetID:  assetID,
		TenantID: tenantID,
		Level:    p.Level,
		Keyword:  p.Keyword,
		Content:  p.Content,
		Source:   p.SourceIP,
	}, true
}

// statusPayloadToTrigger converts an asset status-change event payload
// into a triggerEvent. The bool result is false when the payload
// carries a malformed tenant or asset identifier.
func statusPayloadToTrigger(ev event.Event[event.StatusChangePayload]) (triggerEvent, bool) {
	p := ev.Payload
	assetID, ok := parseID(p.AssetID)
	if !ok {
		return triggerEvent{}, false
	}
	tenantID, ok := parseID(p.TenantID)
	if !ok {
		return triggerEvent{}, false
	}
	return triggerEvent{
		Trigger:    string(TriggerStatusChange),
		AssetID:    assetID,
		AssetKey:   p.AssetKey,
		TenantID:   tenantID,
		PrevStatus: p.PrevStatus,
		CurrStatus: p.CurrStatus,
	}, true
}
