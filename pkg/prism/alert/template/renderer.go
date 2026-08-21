// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package template

import (
	"context"
	"fmt"
	"strings"

	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/i18n"
	"github.com/tickraft/tickraft/pkg/prism/alert"
	"github.com/tickraft/tickraft/pkg/types"
)

// renderer is the default Renderer implementation. It looks up templates in
// a Library, selects the translation for the requested locale (with fallback
// to i18n.DefaultLocale), and substitutes variables using text/template via
// the i18n package's renderTemplate helper.
type renderer struct {
	library  Library
	registry i18n.Registry
	logger   *zap.Logger
}

// NewRenderer creates the default Renderer backed by the given Library and
// i18n Registry. The Registry is used to resolve level labels, field labels,
// and the time format for the requested locale. A nil logger is replaced
// with a no-op logger.
func NewRenderer(lib Library, r i18n.Registry, logger *zap.Logger) Renderer {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &renderer{library: lib, registry: r, logger: logger}
}

// Render implements Renderer. It performs the following steps:
//
//  1. Look up the template by opts.TemplateID.
//  2. Resolve the locale (opts.Locale → default i18n.DefaultLocale), with
//     fallback to the default locale when the requested locale has no
//     translation.
//  3. Select the style (opts.Style → default StyleDetailed).
//  4. Look up the "title.<style>" and "description.<style>" template strings
//     in the resolved translation.
//  5. Build the template variable map from the alert event.
//  6. Render the title and description via text/template substitution.
//  7. Resolve the level label, timestamp format, and field labels from the
//     i18n Registry (with fallback).
//  8. Construct the resource link from opts.FrontendBaseURL.
//
// The whole call is wrapped with panic recovery so a buggy custom Library or
// Registry SPI implementation cannot crash the caller; a panic is recovered,
// logged at error level, and surfaced as a wrapped error.
func (r *renderer) Render(
	ctx context.Context,
	evt alert.Event,
	opts RenderOptions,
) (msg i18n.FormattedMessage, err error) {
	defer func() {
		if p := recover(); p != nil {
			r.logger.Error("renderer Render panicked",
				zap.String("template_id", opts.TemplateID),
				zap.String("locale", opts.Locale),
				zap.String("style", opts.Style),
				zap.Any("panic", p),
			)
			err = fmt.Errorf("template: renderer panicked: %v", p)
		}
	}()

	if r.library == nil {
		return i18n.FormattedMessage{}, ErrTemplateNotFound
	}

	tmpl, err := r.library.Get(opts.TemplateID)
	if err != nil {
		return i18n.FormattedMessage{}, err
	}

	locale := opts.Locale
	if locale == "" {
		locale = i18n.DefaultLocale
	}
	style := opts.Style
	if style == "" {
		style = StyleDetailed
	}

	translation, resolvedLocale := resolveTranslation(tmpl, locale)
	if translation == nil {
		return i18n.FormattedMessage{}, fmt.Errorf("%w: %s for template %s", ErrTranslationMissing, locale, tmpl.ID)
	}

	titleKey := "title." + style
	descKey := "description." + style
	titleTmpl, ok := translation[titleKey]
	if !ok {
		return i18n.FormattedMessage{}, fmt.Errorf("%w: missing key %s in locale %s",
			ErrTranslationMissing, titleKey, resolvedLocale)
	}
	descTmpl, ok := translation[descKey]
	if !ok {
		return i18n.FormattedMessage{}, fmt.Errorf("%w: missing key %s in locale %s",
			ErrTranslationMissing, descKey, resolvedLocale)
	}

	vars := buildTemplateVars(evt)
	title := i18n.RenderTemplate(titleTmpl, vars, r.logger)
	description := i18n.RenderTemplate(descTmpl, vars, r.logger)

	dir := i18n.Parse(resolvedLocale).Direction()

	var tr i18n.Translator
	if r.registry != nil {
		tr = r.registry.Resolve(resolvedLocale)
	}

	level := resolveLevel(tr, evt, r.logger)
	timestamp := formatTimestamp(evt, tr)
	fields := buildFields(evt, tr)
	assetLink := buildAssetLink(evt, opts)

	return i18n.FormattedMessage{
		Title:       title,
		Level:       level,
		Description: description,
		Timestamp:   timestamp,
		Fields:      fields,
		AssetLink:   assetLink,
		Direction:   dir,
	}, nil
}

// resolveTranslation finds the best-matching translation for locale. It
// tries the exact locale first, then the language-only variant (e.g. "zh"
// for "zh-Hans"), then i18n.DefaultLocale. Returns nil when no translation is
// available.
func resolveTranslation(t Template, locale string) (translation map[string]string, matchedLocale string) {
	if tr, ok := t.Translations[locale]; ok {
		return tr, locale
	}
	// Try language-only fallback: "zh-Hans" -> "zh".
	if idx := strings.Index(locale, "-"); idx > 0 {
		lang := locale[:idx]
		if tr, ok := t.Translations[lang]; ok {
			return tr, lang
		}
	}
	// Fall back to default locale.
	if tr, ok := t.Translations[i18n.DefaultLocale]; ok {
		return tr, i18n.DefaultLocale
	}
	return nil, ""
}

// resolveLevel returns the localized level label for the alert. Metric
// alerts use the Severity field from the violations; log alerts use the
// log level directly. Falls back to the raw label when the i18n
// Registry is nil or the key is missing.
func resolveLevel(tr i18n.Translator, evt alert.Event, logger *zap.Logger) string {
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
	if tr == nil {
		return raw
	}
	return i18n.ResolveKey(tr, "level."+raw, logger)
}

// inferMetricLevel maps metric threshold violations to a severity label.
// It returns the highest Severity found across all violations, falling
// back to "warning" when no severity is set.
func inferMetricLevel(evt alert.Event) string {
	hasCritical := false
	hasWarning := false
	for _, v := range evt.Violations {
		switch strings.ToLower(v.Severity) {
		case string(types.SeverityCritical), "fatal":
			hasCritical = true
		case string(types.SeverityError):
			hasCritical = true
		default:
			hasWarning = true
		}
	}
	if hasCritical {
		return string(types.SeverityCritical)
	}
	if hasWarning {
		return string(types.SeverityWarning)
	}
	return string(types.SeverityWarning)
}

// formatTimestamp renders the alert timestamp using the locale-specific
// format from the i18n Registry. Falls back to RFC3339 when the Registry is
// nil or the key is missing.
func formatTimestamp(evt alert.Event, tr i18n.Translator) string {
	if tr == nil {
		return evt.Timestamp.Format("2006-01-02 15:04:05")
	}
	fmtStr := i18n.ResolveKey(tr, "time.format", nil)
	if fmtStr == "" || fmtStr == "time.format" {
		fmtStr = "2006-01-02 15:04:05"
	}
	return evt.Timestamp.Format(fmtStr)
}

// buildFields constructs the structured key/value pairs for the alert,
// using localized field labels from the i18n Registry. Falls back to
// raw field names when the Registry is nil.
func buildFields(evt alert.Event, tr i18n.Translator) map[string]string {
	fields := make(map[string]string)
	var v alert.Violation
	if p := alert.PrimaryViolation(evt); p != nil {
		v = *p
	}
	timestampLabel := "Timestamp"
	resourceLabel := "Asset"
	if tr != nil {
		timestampLabel = i18n.ResolveKey(tr, "field.timestamp", nil)
		resourceLabel = i18n.ResolveKey(tr, "field.resource", nil)
	}
	fields[timestampLabel] = evt.Timestamp.Format("2006-01-02T15:04:05Z07:00")
	fields[resourceLabel] = fmt.Sprintf("%d", evt.AssetID)

	switch evt.Type {
	case alert.TypeMetric:
		metricNameLabel, metricValueLabel, thresholdLabel := "Metric", "Value", "Threshold"
		if tr != nil {
			metricNameLabel = i18n.ResolveKey(tr, "field.metric_name", nil)
			metricValueLabel = i18n.ResolveKey(tr, "field.metric_value", nil)
			thresholdLabel = i18n.ResolveKey(tr, "field.threshold", nil)
		}
		if v.Metric != nil {
			fields[metricNameLabel] = v.Metric.Name
			fields[metricValueLabel] = fmt.Sprintf("%.2f", v.Metric.Value)
			fields[thresholdLabel] = fmt.Sprintf("%.2f", v.Metric.Threshold)
		}
	case alert.TypeLog:
		levelLabel, keywordLabel, contentLabel, sourceIPLabel := "Level", "Keyword", "Content", "Source IP"
		if tr != nil {
			levelLabel = i18n.ResolveKey(tr, "field.level", nil)
			keywordLabel = i18n.ResolveKey(tr, "field.keyword", nil)
			contentLabel = i18n.ResolveKey(tr, "field.content", nil)
			sourceIPLabel = i18n.ResolveKey(tr, "field.source_ip", nil)
		}
		fields[levelLabel] = v.Severity
		if v.Log != nil {
			fields[keywordLabel] = v.Log.Keyword
			fields[contentLabel] = v.Log.Content
		}
		fields[sourceIPLabel] = v.Source
	case alert.TypeHeartbeat, alert.TypeStatus:
		// No type-specific fields for heartbeat and status alerts.
	}
	if len(evt.Violations) > 1 {
		violationsCountLabel := "Violations Count"
		if tr != nil {
			violationsCountLabel = i18n.ResolveKey(tr, "field.violations_count", nil)
		}
		fields[violationsCountLabel] = fmt.Sprintf("%d", len(evt.Violations))
	}
	return fields
}

// buildAssetLink constructs the frontend URL to the asset detail page.
// Returns an empty string when FrontendBaseURL is not configured.
func buildAssetLink(evt alert.Event, opts RenderOptions) string {
	if opts.FrontendBaseURL == "" {
		return ""
	}
	base := strings.TrimRight(opts.FrontendBaseURL, "/")
	return fmt.Sprintf("%s/resources/%d", base, evt.AssetID)
}

// buildTemplateVars constructs the template variable map for text/template
// substitution. Variables are derived from the alert event fields.
func buildTemplateVars(evt alert.Event) map[string]any {
	var v alert.Violation
	if p := alert.PrimaryViolation(evt); p != nil {
		v = *p
	}
	vars := make(map[string]any)
	vars["alert_type"] = string(evt.Type)
	vars["asset_id"] = evt.AssetID
	vars["tenant_id"] = evt.TenantID
	vars["timestamp"] = evt.Timestamp.Format("2006-01-02T15:04:05Z07:00")
	vars["asset_name"] = fmt.Sprintf("asset-%d", evt.AssetID)
	vars["severity"] = v.Severity
	vars["source_ip"] = v.Source
	if v.Metric != nil {
		vars["metric_name"] = v.Metric.Name
		vars["current_value"] = fmt.Sprintf("%.2f", v.Metric.Value)
		vars["threshold"] = fmt.Sprintf("%.2f", v.Metric.Threshold)
		vars["metrics"] = formatRelatedResources(v.Metric.Metrics)
	}
	if v.Log != nil {
		vars["keyword"] = v.Log.Keyword
		vars["content"] = v.Log.Content
	}

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

// Compile-time assertion that renderer implements Renderer.
var _ Renderer = (*renderer)(nil)
