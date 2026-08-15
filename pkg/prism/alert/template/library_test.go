// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package template

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/tickraft/tickraft/pkg/i18n"
	"github.com/tickraft/tickraft/pkg/prism/alert"
	"go.uber.org/zap"
)

func sampleTemplate() Template {
	return Template{
		ID:          "cpu_high",
		Name:        "CPU Usage High",
		Description: "Fires when CPU usage exceeds a threshold",
		AlertType:   AlertTypeMetric,
		Variables:   []string{"metric_name", "current_value", "threshold", "asset_name"},
		Translations: map[string]map[string]string{
			"en-US": {
				"title.concise":         "Alert: {{.metric_name}}",
				"title.detailed":        "CPU alert: {{.metric_name}} at {{.current_value}}",
				"title.technical":       "ALERT[metric] {{.metric_name}}={{.current_value}}",
				"description.concise":   "{{.metric_name}} = {{.current_value}}",
				"description.detailed":  "Asset {{.asset_name}} CPU usage is {{.current_value}}, exceeding threshold {{.threshold}}.",
				"description.technical": "metric={{.metric_name}} value={{.current_value}} threshold={{.threshold}} op={{.operator}}",
			},
			"zh-Hans": {
				"title.concise":         "告警：{{.metric_name}}",
				"title.detailed":        "CPU 告警：{{.metric_name}} 达到 {{.current_value}}",
				"title.technical":       "ALERT[metric] {{.metric_name}}={{.current_value}}",
				"description.concise":   "{{.metric_name}} = {{.current_value}}",
				"description.detailed":  "资产 {{.asset_name}} CPU 使用率 {{.current_value}}，超过阈值 {{.threshold}}。",
				"description.technical": "metric={{.metric_name}} value={{.current_value}} threshold={{.threshold}} op={{.operator}}",
			},
		},
		Styles:       []string{StyleConcise, StyleDetailed, StyleTechnical},
		ChannelHints: []string{"email", "sms", "im"},
	}
}

func TestLibrary_RegisterAndGet(t *testing.T) {
	l := NewLibrary(zap.NewNop())
	tpl := sampleTemplate()
	l.Register(tpl)

	got, err := l.Get("cpu_high")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != "cpu_high" {
		t.Errorf("Get returned wrong ID: %q", got.ID)
	}
}

func TestLibrary_GetNotFound(t *testing.T) {
	l := NewLibrary(zap.NewNop())
	_, err := l.Get("nonexistent")
	if !errors.Is(err, ErrTemplateNotFound) {
		t.Errorf("Get non-existent should return ErrTemplateNotFound, got %v", err)
	}
}

func TestLibrary_RegisterReplacesExisting(t *testing.T) {
	l := NewLibrary(zap.NewNop())
	tpl := sampleTemplate()
	l.Register(tpl)

	tpl.Name = "Updated Name"
	l.Register(tpl)

	got, err := l.Get("cpu_high")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "Updated Name" {
		t.Errorf("Register should replace existing, got Name %q", got.Name)
	}
}

func TestLibrary_ListSorted(t *testing.T) {
	l := NewLibrary(zap.NewNop())
	tpl1 := sampleTemplate()
	tpl1.ID = "zeta"
	tpl2 := sampleTemplate()
	tpl2.ID = "alpha"
	l.Register(tpl1)
	l.Register(tpl2)

	list := l.List()
	if len(list) != 2 {
		t.Fatalf("List returned %d items, want 2", len(list))
	}
	if list[0].ID != "alpha" {
		t.Errorf("List should be sorted by ID; first = %q", list[0].ID)
	}
}

func TestLibrary_ListReturnsCopy(t *testing.T) {
	l := NewLibrary(zap.NewNop())
	l.Register(sampleTemplate())
	list := l.List()
	list[0].Name = "Mutated"
	got, _ := l.Get("cpu_high")
	if got.Name == "Mutated" {
		t.Error("List should return a copy; mutating it affected the library")
	}
}

func TestNewBuiltinLibrary_EachCallIsIndependent(t *testing.T) {
	// Each NewBuiltinLibrary call must return an independent Library instance
	// so that callers can register custom templates without leaking state to
	// other callers.
	l1 := NewBuiltinLibrary(zap.NewNop())
	l2 := NewBuiltinLibrary(zap.NewNop())
	if len(l1.List()) != len(l2.List()) {
		t.Fatalf("NewBuiltinLibrary should be idempotent: l1 has %d templates, l2 has %d", len(l1.List()), len(l2.List()))
	}
	// Mutate l1 with a brand-new template ID; l2 must remain unaffected.
	custom := sampleTemplate()
	custom.ID = "zzz_custom_override"
	custom.Name = "Custom Override"
	l1.Register(custom)
	if _, err := l2.Get("zzz_custom_override"); !errors.Is(err, ErrTemplateNotFound) {
		t.Errorf("l2 should not see custom template registered in l1: err=%v", err)
	}
}

func TestValidate_ValidTemplate(t *testing.T) {
	if err := Validate(sampleTemplate()); err != nil {
		t.Errorf("Validate returned error for valid template: %v", err)
	}
}

func TestValidate_EmptyID(t *testing.T) {
	tpl := sampleTemplate()
	tpl.ID = ""
	if err := Validate(tpl); err == nil {
		t.Error("Validate should reject empty ID")
	}
}

func TestValidate_InvalidID(t *testing.T) {
	tpl := sampleTemplate()
	tpl.ID = "CPU-HIGH!"
	if err := Validate(tpl); err == nil {
		t.Error("Validate should reject invalid ID format")
	}
}

func TestValidate_EmptyName(t *testing.T) {
	tpl := sampleTemplate()
	tpl.Name = ""
	if err := Validate(tpl); err == nil {
		t.Error("Validate should reject empty Name")
	}
}

func TestValidate_InvalidAlertType(t *testing.T) {
	tpl := sampleTemplate()
	tpl.AlertType = "unknown"
	if err := Validate(tpl); err == nil {
		t.Error("Validate should reject invalid AlertType")
	}
}

func TestValidate_InvalidVariableName(t *testing.T) {
	tpl := sampleTemplate()
	tpl.Variables = []string{"Metric-Name"}
	if err := Validate(tpl); err == nil {
		t.Error("Validate should reject invalid variable name")
	}
}

func TestValidate_EmptyStyles(t *testing.T) {
	tpl := sampleTemplate()
	tpl.Styles = nil
	if err := Validate(tpl); err == nil {
		t.Error("Validate should reject empty Styles")
	}
}

func TestValidate_InvalidStyle(t *testing.T) {
	tpl := sampleTemplate()
	tpl.Styles = []string{"verbose"}
	if err := Validate(tpl); err == nil {
		t.Error("Validate should reject invalid Style")
	}
}

func TestValidate_MissingDefaultLocale(t *testing.T) {
	tpl := sampleTemplate()
	// The default locale is zh-Hans (see i18n.DefaultLocale); removing it
	// must cause Validate to reject the template.
	delete(tpl.Translations, i18n.DefaultLocale)
	if err := Validate(tpl); err == nil {
		t.Error("Validate should reject missing default locale translation")
	}
}

func TestValidate_MissingStyleKey(t *testing.T) {
	tpl := sampleTemplate()
	delete(tpl.Translations["en-US"], "title.concise")
	if err := Validate(tpl); err == nil {
		t.Error("Validate should reject missing style key in translation")
	}
}

func TestRenderer_RenderMetricEnglish(t *testing.T) {
	lib := NewLibrary(zap.NewNop())
	lib.Register(sampleTemplate())
	r := NewRenderer(lib, nil, zap.NewNop())

	alert := alert.Event{
		Type:       alert.TypeMetric,
		AssetID:    42,
		Timestamp:  time.Date(2026, 7, 5, 12, 30, 0, 0, time.UTC),
		Violations: []alert.Violation{{Kind: alert.ViolationKindMetric, Metric: &alert.MetricContext{Name: "cpu_usage", Value: 92.5, Threshold: 80.0}}},
	}

	msg, err := r.Render(context.Background(), alert, RenderOptions{
		TemplateID:      "cpu_high",
		Locale:          "en-US",
		Style:           StyleDetailed,
		FrontendBaseURL: "https://app.example.com",
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(msg.Title, "cpu_usage") {
		t.Errorf("title should contain metric name: %q", msg.Title)
	}
	if !strings.Contains(msg.Title, "92.50") {
		t.Errorf("title should contain current value: %q", msg.Title)
	}
	if !strings.Contains(msg.Description, "92.50") {
		t.Errorf("description should contain value: %q", msg.Description)
	}
	if !strings.Contains(msg.Description, "80.00") {
		t.Errorf("description should contain threshold: %q", msg.Description)
	}
	if msg.Direction != i18n.LTR {
		t.Errorf("en-US direction = %q, want ltr", msg.Direction)
	}
	if msg.AssetLink == "" {
		t.Error("resource link should be non-empty with FrontendBaseURL")
	}
}

func TestRenderer_RenderMetricChinese(t *testing.T) {
	lib := NewLibrary(zap.NewNop())
	lib.Register(sampleTemplate())
	r := NewRenderer(lib, nil, zap.NewNop())

	alert := alert.Event{
		Type:       alert.TypeMetric,
		AssetID:    42,
		Timestamp:  time.Date(2026, 7, 5, 12, 30, 0, 0, time.UTC),
		Violations: []alert.Violation{{Kind: alert.ViolationKindMetric, Metric: &alert.MetricContext{Name: "cpu_usage", Value: 92.5, Threshold: 80.0}}},
	}

	msg, err := r.Render(context.Background(), alert, RenderOptions{
		TemplateID: "cpu_high",
		Locale:     "zh-Hans",
		Style:      StyleConcise,
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(msg.Title, "cpu_usage") {
		t.Errorf("zh-Hans title should contain metric name: %q", msg.Title)
	}
	if !strings.Contains(msg.Title, "告警") {
		t.Errorf("zh-Hans title should contain 告警: %q", msg.Title)
	}
}

func TestRenderer_RenderLocaleFallback(t *testing.T) {
	lib := NewLibrary(zap.NewNop())
	lib.Register(sampleTemplate())
	r := NewRenderer(lib, nil, zap.NewNop())

	alert := alert.Event{
		Type:       alert.TypeMetric,
		AssetID:    1,
		Timestamp:  time.Now(),
		Violations: []alert.Violation{{Kind: alert.ViolationKindMetric, Metric: &alert.MetricContext{Name: "cpu", Value: 90, Threshold: 80}}},
	}

	// ja is not in the template; should fall back to en-US.
	msg, err := r.Render(context.Background(), alert, RenderOptions{
		TemplateID: "cpu_high",
		Locale:     "ja",
		Style:      StyleDetailed,
	})
	if err != nil {
		t.Fatalf("Render with fallback locale: %v", err)
	}
	if msg.Title == "" {
		t.Error("fallback title should be non-empty")
	}
}

func TestRenderer_RenderLanguageOnlyFallback(t *testing.T) {
	lib := NewLibrary(zap.NewNop())
	// Register a template with only "zh" (not "zh-Hans").
	tpl := sampleTemplate()
	tpl.Translations = map[string]map[string]string{
		"en-US": tpl.Translations["en-US"],
		"zh": {
			"title.concise":         "告警：{{.metric_name}}",
			"title.detailed":        "告警：{{.metric_name}} = {{.current_value}}",
			"title.technical":       "ALERT[metric] {{.metric_name}}={{.current_value}}",
			"description.concise":   "{{.metric_name}} = {{.current_value}}",
			"description.detailed":  "资产 {{.asset_name}} 告警",
			"description.technical": "metric={{.metric_name}}",
		},
	}
	lib.Register(tpl)
	r := NewRenderer(lib, nil, zap.NewNop())

	alert := alert.Event{
		Type:       alert.TypeMetric,
		AssetID:    1,
		Timestamp:  time.Now(),
		Violations: []alert.Violation{{Kind: alert.ViolationKindMetric, Metric: &alert.MetricContext{Name: "cpu", Value: 90, Threshold: 80}}},
	}

	// zh-Hans should fall back to zh.
	msg, err := r.Render(context.Background(), alert, RenderOptions{
		TemplateID: "cpu_high",
		Locale:     "zh-Hans",
		Style:      StyleDetailed,
	})
	if err != nil {
		t.Fatalf("Render with language-only fallback: %v", err)
	}
	if !strings.Contains(msg.Title, "告警") {
		t.Errorf("zh-Hans should fall back to zh: %q", msg.Title)
	}
}

func TestRenderer_RenderTemplateNotFound(t *testing.T) {
	lib := NewLibrary(zap.NewNop())
	r := NewRenderer(lib, nil, zap.NewNop())
	alert := alert.Event{
		Type:      alert.TypeMetric,
		AssetID:   1,
		Timestamp: time.Now(),
	}
	_, err := r.Render(context.Background(), alert, RenderOptions{TemplateID: "nonexistent"})
	if !errors.Is(err, ErrTemplateNotFound) {
		t.Errorf("Render with unknown template should return ErrTemplateNotFound, got %v", err)
	}
}

func TestRenderer_RenderTranslationMissing(t *testing.T) {
	lib := NewLibrary(zap.NewNop())
	tpl := sampleTemplate()
	// Remove all translations.
	tpl.Translations = nil
	lib.Register(tpl)
	r := NewRenderer(lib, nil, zap.NewNop())
	alert := alert.Event{
		Type:      alert.TypeMetric,
		AssetID:   1,
		Timestamp: time.Now(),
	}
	_, err := r.Render(context.Background(), alert, RenderOptions{TemplateID: "cpu_high"})
	if !errors.Is(err, ErrTranslationMissing) {
		t.Errorf("Render with no translations should return ErrTranslationMissing, got %v", err)
	}
}

func TestRenderer_RenderMissingStyleKey(t *testing.T) {
	lib := NewLibrary(zap.NewNop())
	tpl := sampleTemplate()
	delete(tpl.Translations["en-US"], "title.concise")
	lib.Register(tpl)
	r := NewRenderer(lib, nil, zap.NewNop())
	alert := alert.Event{
		Type:      alert.TypeMetric,
		AssetID:   1,
		Timestamp: time.Now(),
	}
	_, err := r.Render(context.Background(), alert, RenderOptions{
		TemplateID: "cpu_high",
		Locale:     "en-US",
		Style:      StyleConcise,
	})
	if !errors.Is(err, ErrTranslationMissing) {
		t.Errorf("Render with missing style key should return ErrTranslationMissing, got %v", err)
	}
}

func TestRenderer_RenderDefaultOptions(t *testing.T) {
	lib := NewLibrary(zap.NewNop())
	lib.Register(sampleTemplate())
	r := NewRenderer(lib, nil, zap.NewNop())
	alert := alert.Event{
		Type:       alert.TypeMetric,
		AssetID:    1,
		Timestamp:  time.Now(),
		Violations: []alert.Violation{{Kind: alert.ViolationKindMetric, Metric: &alert.MetricContext{Name: "cpu", Value: 90, Threshold: 80}}},
	}
	// Empty Locale and Style should default to i18n.DefaultLocale and "detailed".
	msg, err := r.Render(context.Background(), alert, RenderOptions{TemplateID: "cpu_high"})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if msg.Title == "" {
		t.Error("default title should be non-empty")
	}
}

func TestRenderer_RenderWithRegistry(t *testing.T) {
	// Load the i18n Registry with built-in resources for level/field labels.
	loader := i18n.NewLoader(zap.NewNop())
	reg := i18n.NewRegistry(zap.NewNop())
	if err := loader.LoadToRegistry(i18n.EmbeddedFS(), reg); err != nil {
		t.Fatalf("LoadToRegistry: %v", err)
	}

	lib := NewLibrary(zap.NewNop())
	lib.Register(sampleTemplate())
	r := NewRenderer(lib, reg, zap.NewNop())

	alert := alert.Event{
		Type:       alert.TypeMetric,
		AssetID:    42,
		Timestamp:  time.Date(2026, 7, 5, 12, 30, 0, 0, time.UTC),
		Violations: []alert.Violation{{Kind: alert.ViolationKindMetric, Metric: &alert.MetricContext{Name: "cpu_usage", Value: 92.5, Threshold: 80.0}}},
	}
	msg, err := r.Render(context.Background(), alert, RenderOptions{
		TemplateID: "cpu_high",
		Locale:     "en-US",
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if msg.Level == "" {
		t.Error("level should be resolved from registry")
	}
	if len(msg.Fields) == 0 {
		t.Error("fields should be populated from registry")
	}
}

func TestRenderer_RenderRTLDirection(t *testing.T) {
	lib := NewLibrary(zap.NewNop())
	tpl := sampleTemplate()
	tpl.Translations["ar"] = map[string]string{
		"title.concise":         "تنبيه: {{.metric_name}}",
		"title.detailed":        "تنبيه: {{.metric_name}} = {{.current_value}}",
		"title.technical":       "ALERT[metric] {{.metric_name}}",
		"description.concise":   "{{.metric_name}} = {{.current_value}}",
		"description.detailed":  "تنبيه",
		"description.technical": "metric={{.metric_name}}",
	}
	lib.Register(tpl)
	r := NewRenderer(lib, nil, zap.NewNop())

	alert := alert.Event{
		Type:       alert.TypeMetric,
		AssetID:    1,
		Timestamp:  time.Now(),
		Violations: []alert.Violation{{Kind: alert.ViolationKindMetric, Metric: &alert.MetricContext{Name: "cpu", Value: 90, Threshold: 80}}},
	}
	msg, err := r.Render(context.Background(), alert, RenderOptions{
		TemplateID: "cpu_high",
		Locale:     "ar",
		Style:      StyleConcise,
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if msg.Direction != i18n.RTL {
		t.Errorf("ar direction = %q, want rtl", msg.Direction)
	}
}

func TestRenderer_RenderLogAlert(t *testing.T) {
	lib := NewLibrary(zap.NewNop())
	tpl := Template{
		ID:          "log_keyword",
		Name:        "Log Keyword Matched",
		Description: "Fires when a log keyword is matched",
		AlertType:   AlertTypeLog,
		Variables:   []string{"keyword", "content", "source_ip"},
		Translations: map[string]map[string]string{
			"en-US": {
				"title.concise":         "Log alert: {{.keyword}}",
				"title.detailed":        "Log keyword \"{{.keyword}}\" matched",
				"title.technical":       "ALERT[log] keyword={{.keyword}}",
				"description.concise":   "Keyword {{.keyword}} in log",
				"description.detailed":  "Log entry from {{.source_ip}} matched keyword \"{{.keyword}}\": {{.content}}",
				"description.technical": "keyword={{.keyword}} source={{.source_ip}}",
			},
		},
		Styles: []string{StyleConcise, StyleDetailed, StyleTechnical},
	}
	lib.Register(tpl)
	r := NewRenderer(lib, nil, zap.NewNop())

	alert := alert.Event{
		Type:       alert.TypeLog,
		AssetID:    10,
		Timestamp:  time.Now(),
		Violations: []alert.Violation{{Kind: alert.ViolationKindLog, Severity: "error", Log: &alert.LogContext{Keyword: "OOM", Content: "out of memory"}, Source: "10.0.0.1"}},
	}
	msg, err := r.Render(context.Background(), alert, RenderOptions{
		TemplateID: "log_keyword",
		Locale:     "en-US",
		Style:      StyleDetailed,
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(msg.Title, "OOM") {
		t.Errorf("title should contain keyword: %q", msg.Title)
	}
	if !strings.Contains(msg.Description, "10.0.0.1") {
		t.Errorf("description should contain source IP: %q", msg.Description)
	}
}

func TestRenderer_RenderEmptyFrontendBaseURL(t *testing.T) {
	lib := NewLibrary(zap.NewNop())
	lib.Register(sampleTemplate())
	r := NewRenderer(lib, nil, zap.NewNop())
	alert := alert.Event{
		Type:       alert.TypeMetric,
		AssetID:    1,
		Timestamp:  time.Now(),
		Violations: []alert.Violation{{Kind: alert.ViolationKindMetric, Metric: &alert.MetricContext{Name: "cpu", Value: 90, Threshold: 80}}},
	}
	msg, err := r.Render(context.Background(), alert, RenderOptions{
		TemplateID: "cpu_high",
		Locale:     "en-US",
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if msg.AssetLink != "" {
		t.Errorf("empty FrontendBaseURL should produce empty AssetLink, got %q", msg.AssetLink)
	}
}

func TestRenderer_NilLibrary(t *testing.T) {
	r := NewRenderer(nil, nil, zap.NewNop())
	alert := alert.Event{
		Type:      alert.TypeMetric,
		AssetID:   1,
		Timestamp: time.Now(),
	}
	_, err := r.Render(context.Background(), alert, RenderOptions{TemplateID: "cpu_high"})
	if !errors.Is(err, ErrTemplateNotFound) {
		t.Errorf("Render with nil library should return ErrTemplateNotFound, got %v", err)
	}
}

func TestIsValidAlertType(t *testing.T) {
	if !IsValidAlertType(AlertTypeMetric) {
		t.Error("AlertTypeMetric should be valid")
	}
	if !IsValidAlertType(AlertTypeLog) {
		t.Error("AlertTypeLog should be valid")
	}
	if !IsValidAlertType(AlertTypeGeneric) {
		t.Error("AlertTypeGeneric should be valid")
	}
	if IsValidAlertType("unknown") {
		t.Error("unknown should be invalid")
	}
}

func TestIsValidStyle(t *testing.T) {
	if !IsValidStyle(StyleConcise) {
		t.Error("StyleConcise should be valid")
	}
	if !IsValidStyle(StyleDetailed) {
		t.Error("StyleDetailed should be valid")
	}
	if !IsValidStyle(StyleTechnical) {
		t.Error("StyleTechnical should be valid")
	}
	if IsValidStyle("verbose") {
		t.Error("verbose should be invalid")
	}
}
