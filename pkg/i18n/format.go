// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package i18n

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/prism/alert"
	"github.com/tickraft/tickraft/pkg/types"
)

// Style is the formatting style applied to an alert message. It controls the
// verbosity and structure of the rendered output.
type Style string

const (
	// StyleConcise produces a short one-line summary suitable for SMS and
	// instant-message channels.
	StyleConcise Style = "concise"
	// StyleDetailed produces a full sentence description suitable for email
	// and ticket systems.
	StyleDetailed Style = "detailed"
	// StyleTechnical produces a key=value structured format suitable for
	// machine consumption and log aggregation systems.
	StyleTechnical Style = "technical"
)

// FormatOptions controls how an Event is rendered by a Formatter.
type FormatOptions struct {
	// Locale is the recipient's preferred locale (e.g. "zh-Hans"). When
	// empty, the default locale "zh-Hans" is used.
	Locale string
	// Style is the formatting style. When empty, StyleDetailed is used.
	Style Style
	// Timezone is the location used to format the alert timestamp. When
	// nil, the alert's original timestamp is rendered without conversion.
	Timezone *string
	// FrontendBaseURL is the base URL for constructing asset links. When
	// empty, FormattedMessage.AssetLink is an empty string.
	FrontendBaseURL string
}

// FormattedMessage is the rendered output of an Event through a
// Formatter. Channels consume this struct to produce channel-specific
// payloads (email HTML, IM card, SMS text, etc.).
type FormattedMessage struct {
	// Title is the short headline of the alert (e.g. "CPU Alert").
	Title string
	// Level is the human-readable severity label (e.g. "Critical").
	Level string
	// Description is the full body text of the alert.
	Description string
	// Timestamp is the formatted timestamp string in the recipient's locale
	// format.
	Timestamp string
	// Fields is the structured key/value pairs extracted from the alert,
	// suitable for rendering as a definition list or table.
	Fields map[string]string
	// AssetLink is the URL to the asset detail page in the frontend.
	// Empty when FrontendBaseURL is not configured.
	AssetLink string
	// Direction is the text direction for the recipient's locale ("ltr" or
	// "rtl"). Channels use this to set HTML dir attributes or IM card
	// layout.
	Direction Direction
}

// Formatter renders an Event into a locale-aware FormattedMessage.
// Implementations must be safe for concurrent use.
type Formatter interface {
	// Format produces a FormattedMessage for the given alert and options.
	// The opts.Locale field controls the output language; opts.Style
	// controls the verbosity. Format must not panic on missing keys; it
	// falls back to the default locale and finally to the key name.
	Format(ctx context.Context, evt alert.Event, opts FormatOptions) FormattedMessage
}

// defaultFormatter is the canonical Formatter implementation. It looks up
// style-specific keys in the Registry and performs text/template-style
// variable substitution on the rendered strings.
type defaultFormatter struct {
	registry Registry
	logger   *zap.Logger
}

// NewDefaultFormatter creates a Formatter backed by the given Registry. The
// logger is used to record missing-key warnings; pass a no-op logger when
// observability is not required.
func NewDefaultFormatter(r Registry, logger *zap.Logger) Formatter {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &defaultFormatter{registry: r, logger: logger}
}

// Format implements Formatter. It selects the style-specific key for the
// alert type, resolves the key through the Registry (with fallback), and
// substitutes variables from the alert event.
func (f *defaultFormatter) Format(ctx context.Context, evt alert.Event, opts FormatOptions) FormattedMessage {
	if opts.Style == "" {
		opts.Style = StyleDetailed
	}
	locale := opts.Locale
	if locale == "" {
		locale = DefaultLocale
	}
	tr := f.registry.Resolve(locale)
	dir := Parse(locale).Direction()

	alertType := string(evt.Type)
	vars := buildTemplateVars(evt)

	titleKey := fmt.Sprintf("alert.%s.title.%s", alertType, opts.Style)
	descKey := fmt.Sprintf("alert.%s.description.%s", alertType, opts.Style)

	title := ResolveKey(tr, titleKey, f.logger)
	desc := ResolveKey(tr, descKey, f.logger)

	title = RenderTemplate(title, vars, f.logger)
	desc = RenderTemplate(desc, vars, f.logger)

	level := f.resolveLevel(tr, evt)
	timestamp := formatTimestamp(evt, tr, opts)
	fields := buildFields(evt, tr)
	assetLink := buildAssetLink(evt, opts)

	return FormattedMessage{
		Title:       title,
		Level:       level,
		Description: desc,
		Timestamp:   timestamp,
		Fields:      fields,
		AssetLink:   assetLink,
		Direction:   dir,
	}
}

// resolveLevel returns the localized level label for the alert. Metric alerts
// infer the level from the threshold and operator; log alerts use the log
// level directly.
func (f *defaultFormatter) resolveLevel(tr Translator, evt alert.Event) string {
	var v alert.Violation
	if p := alert.PrimaryViolation(evt); p != nil {
		v = *p
	}
	var raw string
	switch evt.Type {
	case alert.TypeLog:
		raw = strings.ToLower(v.Severity)
	case alert.TypeMetric:
		raw = inferMetricLevel(evt)
	default:
		// Heartbeat and status alerts carry no severity of their own.
		raw = "info"
	}
	if raw == "" {
		raw = "info"
	}
	key := "level." + raw
	return ResolveKey(tr, key, f.logger)
}

// inferMetricLevel maps metric threshold violations to a severity label.
// It returns the highest Severity found across all violations, falling
// back to "warning" when no severity is set. The severity is stamped on
// each Violation at dispatch time (inherited from the rule or the metric
// payload), so the formatter no longer re-derives it from the operator
// and threshold.
func inferMetricLevel(evt alert.Event) string {
	hasCritical := false
	hasWarning := false
	for _, v := range evt.Violations {
		switch strings.ToLower(v.Severity) {
		case string(types.SeverityCritical), "fatal":
			hasCritical = true
		case "error":
			hasCritical = true
		default:
			hasWarning = true
		}
	}
	if hasCritical {
		return string(types.SeverityCritical)
	}
	if hasWarning {
		return "warning"
	}
	return "warning"
}

// formatTimestamp renders the alert timestamp in the recipient's locale
// format. When opts.Timezone is non-empty, the timestamp is converted to
// that timezone before formatting.
func formatTimestamp(evt alert.Event, tr Translator, _ FormatOptions) string {
	fmtStr := ResolveKey(tr, "time.format", nil)
	if fmtStr == "" || fmtStr == "time.format" {
		fmtStr = "2006-01-02 15:04:05"
	}
	return evt.Timestamp.Format(fmtStr)
}

// buildFields constructs the structured key/value pairs for the alert,
// using localized field labels from the Registry.
func buildFields(evt alert.Event, tr Translator) map[string]string {
	fields := make(map[string]string)
	var v alert.Violation
	if p := alert.PrimaryViolation(evt); p != nil {
		v = *p
	}
	fields[ResolveKey(tr, "field.timestamp", nil)] = evt.Timestamp.Format(time.RFC3339)
	fields[ResolveKey(tr, "field.resource", nil)] = fmt.Sprintf("%d", evt.AssetID)

	switch evt.Type {
	case alert.TypeMetric:
		if v.Metric != nil {
			fields[ResolveKey(tr, "field.metric_name", nil)] = v.Metric.Name
			fields[ResolveKey(tr, "field.metric_value", nil)] = fmt.Sprintf("%.2f", v.Metric.Value)
			fields[ResolveKey(tr, "field.threshold", nil)] = fmt.Sprintf("%.2f", v.Metric.Threshold)
		}
	case alert.TypeLog:
		fields[ResolveKey(tr, "field.level", nil)] = v.Severity
		if v.Log != nil {
			fields[ResolveKey(tr, "field.keyword", nil)] = v.Log.Keyword
			fields[ResolveKey(tr, "field.content", nil)] = v.Log.Content
		}
		fields[ResolveKey(tr, "field.source_ip", nil)] = v.Source
	case alert.TypeHeartbeat, alert.TypeStatus:
		// No type-specific fields for heartbeat and status alerts.
	}
	if len(evt.Violations) > 1 {
		fields[ResolveKey(tr, "field.violations_count", nil)] = fmt.Sprintf("%d", len(evt.Violations))
	}
	return fields
}

// buildAssetLink constructs the frontend URL to the asset detail page.
// Returns an empty string when FrontendBaseURL is not configured.
func buildAssetLink(evt alert.Event, opts FormatOptions) string {
	if opts.FrontendBaseURL == "" {
		return ""
	}
	base := strings.TrimRight(opts.FrontendBaseURL, "/")
	return fmt.Sprintf("%s/resources/%d", base, evt.AssetID)
}

// buildTemplateVars constructs the template variable map for text/template
// substitution. Variables are derived from the alert event fields.
// Single-value fields (metric_name, current_value, etc.) are derived from
// the primary (most severe) violation for convenience. The full violations
// list is always available via the "violations" key for multi-violation
// template support.
func buildTemplateVars(evt alert.Event) map[string]any {
	vars := make(map[string]any)
	vars["alert_type"] = string(evt.Type)
	vars["asset_id"] = evt.AssetID
	vars["tenant_id"] = evt.TenantID
	vars["timestamp"] = evt.Timestamp.Format(time.RFC3339)

	// Derive single-value convenience fields from the primary violation.
	if primary := alert.PrimaryViolation(evt); primary != nil {
		vars["severity"] = primary.Severity
		vars["source_ip"] = primary.Source
		if primary.Metric != nil {
			vars["metric_name"] = primary.Metric.Name
			vars["current_value"] = fmt.Sprintf("%.2f", primary.Metric.Value)
			vars["threshold"] = fmt.Sprintf("%.2f", primary.Metric.Threshold)
			vars["metrics"] = formatRelatedResources(primary.Metric.Metrics)
		}
		if primary.Log != nil {
			vars["keyword"] = primary.Log.Keyword
			vars["content"] = primary.Log.Content
		}
		if primary.Status != nil {
			vars["prev_status"] = primary.Status.PrevStatus
			vars["curr_status"] = primary.Status.CurrStatus
		}
	}

	vars["asset_name"] = fmt.Sprintf("asset-%d", evt.AssetID)

	violationList := make([]map[string]any, 0, len(evt.Violations))
	for _, viol := range evt.Violations {
		entry := map[string]any{
			"kind":     viol.Kind,
			"severity": viol.Severity,
			"source":   viol.Source,
			"message":  viol.Message,
		}
		if viol.Metric != nil {
			entry["metric"] = viol.Metric.Name
			entry["value"] = fmt.Sprintf("%.2f", viol.Metric.Value)
			entry["threshold"] = fmt.Sprintf("%.2f", viol.Metric.Threshold)
			entry["metrics"] = formatRelatedResources(viol.Metric.Metrics)
		}
		if viol.Log != nil {
			entry["keyword"] = viol.Log.Keyword
			entry["content"] = viol.Log.Content
		}
		if viol.Status != nil {
			entry["prev_status"] = viol.Status.PrevStatus
			entry["curr_status"] = viol.Status.CurrStatus
		}
		violationList = append(violationList, entry)
	}
	vars["violations"] = violationList

	return vars
}

// formatRelatedResources renders the related resources map as a
// comma-separated "key=value" string.
func formatRelatedResources(resources map[string]float64) string {
	if len(resources) == 0 {
		return "none"
	}
	parts := make([]string, 0, len(resources))
	for k, v := range resources {
		parts = append(parts, fmt.Sprintf("%s=%.2f", k, v))
	}
	return strings.Join(parts, ", ")
}
