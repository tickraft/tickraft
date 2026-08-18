// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package i18n

import (
	"context"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/prism/alert"
)

func loadBuiltinRegistry(t *testing.T) Registry {
	t.Helper()
	loader := NewLoader(zap.NewNop())
	r := NewRegistry(zap.NewNop())
	if err := loader.LoadToRegistry(EmbeddedFS(), r); err != nil {
		t.Fatalf("LoadToRegistry: %v", err)
	}
	return r
}

func TestDefaultFormatter_MetricAlertEnglish(t *testing.T) {
	r := loadBuiltinRegistry(t)
	f := NewDefaultFormatter(r, zap.NewNop())

	evt := alert.Event{
		Type:      alert.TypeMetric,
		AssetID:   42,
		TenantID:  1,
		Timestamp: time.Date(2026, 7, 5, 12, 30, 0, 0, time.UTC),
		Violations: []alert.Violation{{
			Kind:   alert.ViolationKindMetric,
			Metric: &alert.MetricContext{Name: "cpu_usage", Value: 92.5, Threshold: 80.0},
		}},
	}

	msg := f.Format(context.Background(), evt, FormatOptions{
		Locale:          "en-US",
		Style:           StyleDetailed,
		FrontendBaseURL: "https://app.example.com",
	})

	if !strings.Contains(msg.Title, "cpu_usage") {
		t.Errorf("title should contain metric name: %q", msg.Title)
	}
	if !strings.Contains(msg.Description, "92.50") {
		t.Errorf("description should contain value: %q", msg.Description)
	}
	if !strings.Contains(msg.Description, "80.00") {
		t.Errorf("description should contain threshold: %q", msg.Description)
	}
	if msg.Direction != LTR {
		t.Errorf("en-US direction = %q, want ltr", msg.Direction)
	}
	if msg.AssetLink == "" {
		t.Errorf("asset link should be non-empty with FrontendBaseURL")
	}
	if !strings.Contains(msg.AssetLink, "42") {
		t.Errorf("asset link should contain asset ID: %q", msg.AssetLink)
	}
	if msg.Level != "Warning" && msg.Level != "Critical" {
		t.Errorf("level = %q, want Warning or Critical", msg.Level)
	}
}

func TestDefaultFormatter_MetricAlertChinese(t *testing.T) {
	r := loadBuiltinRegistry(t)
	f := NewDefaultFormatter(r, zap.NewNop())

	evt := alert.Event{
		Type:      alert.TypeMetric,
		AssetID:   42,
		TenantID:  1,
		Timestamp: time.Date(2026, 7, 5, 12, 30, 0, 0, time.UTC),
		Violations: []alert.Violation{{
			Kind:   alert.ViolationKindMetric,
			Metric: &alert.MetricContext{Name: "cpu_usage", Value: 92.5, Threshold: 80.0},
		}},
	}

	msg := f.Format(context.Background(), evt, FormatOptions{
		Locale: "zh-Hans",
		Style:  StyleDetailed,
	})

	if !strings.Contains(msg.Title, "cpu_usage") {
		t.Errorf("zh-Hans title should contain metric name: %q", msg.Title)
	}
	if !strings.Contains(msg.Title, "告警") {
		t.Errorf("zh-Hans title should contain 告警: %q", msg.Title)
	}
	if msg.Direction != LTR {
		t.Errorf("zh-Hans direction = %q, want ltr", msg.Direction)
	}
}

func TestDefaultFormatter_LogAlertEnglish(t *testing.T) {
	r := loadBuiltinRegistry(t)
	f := NewDefaultFormatter(r, zap.NewNop())

	evt := alert.Event{
		Type:      alert.TypeLog,
		AssetID:   10,
		TenantID:  1,
		Timestamp: time.Date(2026, 7, 5, 12, 30, 0, 0, time.UTC),
		Violations: []alert.Violation{{
			Kind:     alert.ViolationKindLog,
			Severity: "error",
			Log:      &alert.LogContext{Keyword: "OOM", Content: "out of memory"},
			Source:   "10.0.0.1",
		}},
	}

	msg := f.Format(context.Background(), evt, FormatOptions{
		Locale: "en-US",
		Style:  StyleConcise,
	})

	if !strings.Contains(msg.Title, "OOM") {
		t.Errorf("title should contain keyword: %q", msg.Title)
	}
	if !strings.Contains(msg.Description, "OOM") {
		t.Errorf("description should contain keyword: %q", msg.Description)
	}
	if msg.Level != "Error" {
		t.Errorf("level = %q, want Error", msg.Level)
	}
}

func TestDefaultFormatter_StyleVariants(t *testing.T) {
	r := loadBuiltinRegistry(t)
	f := NewDefaultFormatter(r, zap.NewNop())

	evt := alert.Event{
		Type:      alert.TypeMetric,
		AssetID:   1,
		Timestamp: time.Now(),
		Violations: []alert.Violation{{
			Kind:   alert.ViolationKindMetric,
			Metric: &alert.MetricContext{Name: "cpu_usage", Value: 90, Threshold: 80},
		}},
	}

	concise := f.Format(context.Background(), evt, FormatOptions{Locale: "en-US", Style: StyleConcise})
	detailed := f.Format(context.Background(), evt, FormatOptions{Locale: "en-US", Style: StyleDetailed})
	technical := f.Format(context.Background(), evt, FormatOptions{Locale: "en-US", Style: StyleTechnical})

	if concise.Title == detailed.Title {
		t.Errorf("concise and detailed titles should differ")
	}
	if detailed.Title == technical.Title {
		t.Errorf("detailed and technical titles should differ")
	}
	if !strings.Contains(technical.Title, "ALERT[metric]") {
		t.Errorf("technical title should contain ALERT[metric]: %q", technical.Title)
	}
}

func TestDefaultFormatter_DefaultOptions(t *testing.T) {
	r := loadBuiltinRegistry(t)
	f := NewDefaultFormatter(r, zap.NewNop())

	evt := alert.Event{
		Type:      alert.TypeMetric,
		AssetID:   1,
		Timestamp: time.Now(),
		Violations: []alert.Violation{{
			Kind:   alert.ViolationKindMetric,
			Metric: &alert.MetricContext{Name: "cpu", Value: 90, Threshold: 80},
		}},
	}

	// Empty Locale and Style should default to "zh-Hans" (DefaultLocale) and "detailed".
	msg := f.Format(context.Background(), evt, FormatOptions{})
	if msg.Direction != LTR {
		t.Errorf("default direction = %q, want ltr", msg.Direction)
	}
	if msg.Title == "" {
		t.Errorf("default title should be non-empty")
	}
}

func TestDefaultFormatter_LocaleFallback(t *testing.T) {
	r := loadBuiltinRegistry(t)
	f := NewDefaultFormatter(r, zap.NewNop())

	evt := alert.Event{
		Type:      alert.TypeMetric,
		AssetID:   1,
		Timestamp: time.Now(),
		Violations: []alert.Violation{{
			Kind:   alert.ViolationKindMetric,
			Metric: &alert.MetricContext{Name: "cpu", Value: 90, Threshold: 80},
		}},
	}

	// fr-FR is not registered; should fall back to the default locale (zh-Hans).
	msg := f.Format(context.Background(), evt, FormatOptions{Locale: "fr-FR"})
	if msg.Title == "" {
		t.Errorf("fallback title should be non-empty")
	}
	if msg.Direction != LTR {
		t.Errorf("fallback direction = %q, want ltr", msg.Direction)
	}
}

func TestDefaultFormatter_EmptyFrontendBaseURL(t *testing.T) {
	r := loadBuiltinRegistry(t)
	f := NewDefaultFormatter(r, zap.NewNop())

	evt := alert.Event{
		Type:      alert.TypeMetric,
		AssetID:   1,
		Timestamp: time.Now(),
		Violations: []alert.Violation{{
			Kind:   alert.ViolationKindMetric,
			Metric: &alert.MetricContext{Name: "cpu", Value: 90, Threshold: 80},
		}},
	}

	msg := f.Format(context.Background(), evt, FormatOptions{
		Locale:          "en-US",
		FrontendBaseURL: "",
	})
	if msg.AssetLink != "" {
		t.Errorf("empty FrontendBaseURL should produce empty AssetLink, got %q", msg.AssetLink)
	}
}

func TestDefaultFormatter_FieldsPopulated(t *testing.T) {
	r := loadBuiltinRegistry(t)
	f := NewDefaultFormatter(r, zap.NewNop())

	evt := alert.Event{
		Type:      alert.TypeMetric,
		AssetID:   42,
		TenantID:  1,
		Timestamp: time.Now(),
		Violations: []alert.Violation{{
			Kind:   alert.ViolationKindMetric,
			Metric: &alert.MetricContext{Name: "cpu_usage", Value: 92.5, Threshold: 80.0},
		}},
	}

	msg := f.Format(context.Background(), evt, FormatOptions{Locale: "en-US"})
	if len(msg.Fields) == 0 {
		t.Errorf("fields should be populated")
	}
	if _, ok := msg.Fields["Metric"]; !ok {
		t.Errorf("fields should contain 'Metric' key, got %v", msg.Fields)
	}
}

func TestDefaultFormatter_RTLDirection(t *testing.T) {
	r := loadBuiltinRegistry(t)
	// Register an ar locale so Resolve returns a non-default bundle.
	r.Register("ar", NewMessageMap("ar", map[string]string{
		"alert.metric.title.concise":       "تنبيه: {{.metric_name}}",
		"alert.metric.description.concise": "{{.current_value}}",
	}))

	f := NewDefaultFormatter(r, zap.NewNop())
	evt := alert.Event{
		Type:      alert.TypeMetric,
		AssetID:   1,
		Timestamp: time.Now(),
		Violations: []alert.Violation{{
			Kind:   alert.ViolationKindMetric,
			Metric: &alert.MetricContext{Name: "cpu", Value: 90, Threshold: 80},
		}},
	}

	msg := f.Format(context.Background(), evt, FormatOptions{Locale: "ar", Style: StyleConcise})
	if msg.Direction != RTL {
		t.Errorf("ar direction = %q, want rtl", msg.Direction)
	}
}

func TestDefaultFormatter_NilLogger(t *testing.T) {
	r := loadBuiltinRegistry(t)
	f := NewDefaultFormatter(r, nil)
	evt := alert.Event{
		Type:      alert.TypeMetric,
		AssetID:   1,
		Timestamp: time.Now(),
		Violations: []alert.Violation{{
			Kind:   alert.ViolationKindMetric,
			Metric: &alert.MetricContext{Name: "cpu", Value: 90, Threshold: 80},
		}},
	}
	// Should not panic with nil logger.
	msg := f.Format(context.Background(), evt, FormatOptions{Locale: "en-US"})
	if msg.Title == "" {
		t.Errorf("title should be non-empty even with nil logger")
	}
}

func TestRenderTemplate_MissingVariable(t *testing.T) {
	result := RenderTemplate("hello {{.name}}", map[string]any{}, zap.NewNop())
	// text/template with missingkey=zero renders <no value> for missing keys.
	// The test verifies the function does not panic and returns a string.
	if result == "" {
		t.Errorf("renderTemplate should return non-empty string")
	}
}

func TestRenderTemplate_EmptyTemplate(t *testing.T) {
	result := RenderTemplate("", map[string]any{"a": "b"}, zap.NewNop())
	if result != "" {
		t.Errorf("empty template should return empty string, got %q", result)
	}
}

func TestRenderTemplate_NoVars(t *testing.T) {
	result := RenderTemplate("hello world", nil, zap.NewNop())
	if result != "hello world" {
		t.Errorf("template with no vars should return as-is, got %q", result)
	}
}

func TestRenderTemplate_ValidSubstitution(t *testing.T) {
	result := RenderTemplate("value={{.current_value}}", map[string]any{
		"current_value": "92.50",
	}, zap.NewNop())
	if result != "value=92.50" {
		t.Errorf("valid substitution failed: got %q, want value=92.50", result)
	}
}

func TestInferMetricLevel_Critical(t *testing.T) {
	evt := alert.Event{
		Violations: []alert.Violation{{
			Kind:     alert.ViolationKindMetric,
			Severity: "critical",
			Metric:   &alert.MetricContext{Value: 200, Threshold: 100},
		}},
	}
	if got := inferMetricLevel(evt); got != "critical" {
		t.Errorf("inferMetricLevel(severity=critical) = %q, want critical", got)
	}
}

func TestInferMetricLevel_Warning(t *testing.T) {
	evt := alert.Event{
		Violations: []alert.Violation{{
			Kind:   alert.ViolationKindMetric,
			Metric: &alert.MetricContext{Value: 110, Threshold: 100},
		}},
	}
	if got := inferMetricLevel(evt); got != "warning" {
		t.Errorf("inferMetricLevel(above threshold) = %q, want warning", got)
	}
}
