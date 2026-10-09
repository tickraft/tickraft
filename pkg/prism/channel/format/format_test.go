// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package format

import (
	"context"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/i18n"
	"github.com/tickraft/tickraft/pkg/prism/alert"
	"github.com/tickraft/tickraft/pkg/prism/alert/template"
)

// ---------------------------------------------------------------------------
// BuildWithOpts: locale-aware rendering
// ---------------------------------------------------------------------------

func TestBuildWithOpts_DefaultFormatter(t *testing.T) {
	evt := alert.Event{
		Type:     alert.TypeMetric,
		AssetID:  42,
		TenantID: 1,
		Timestamp: time.Unix(1700000000,
			0).UTC(),
		Violations: []alert.Violation{
			{
				Kind: alert.ViolationKindMetric,
				Metric: &alert.MetricContext{
					Name:      "cpu_usage",
					Value:     95.5,
					Threshold: 90.0,
				},
			},
		},
	}
	msg := BuildWithOpts(context.Background(), evt, i18n.FormatOptions{
		Locale:          "en-US",
		Style:           i18n.StyleDetailed,
		FrontendBaseURL: "https://example.com",
	}, nil, zap.NewNop())

	if msg.Title == "" {
		t.Error("default formatter should produce non-empty title")
	}
	if msg.Locale != "en-US" {
		t.Errorf("locale: got %q, want %q", msg.Locale, "en-US")
	}
	if msg.Direction != "ltr" {
		t.Errorf("en-US direction: got %q, want %q", msg.Direction, "ltr")
	}
	if msg.Fields == nil {
		t.Error("fields should be non-nil")
	}
}

func TestBuildWithOpts_ChineseLocale(t *testing.T) {
	evt := alert.Event{
		Type:    alert.TypeMetric,
		AssetID: 42,
		Timestamp: time.Unix(1700000000,
			0).UTC(),
		Violations: []alert.Violation{
			{
				Kind: alert.ViolationKindMetric,
				Metric: &alert.MetricContext{
					Name:      "cpu_usage",
					Value:     95.5,
					Threshold: 90.0,
				},
			},
		},
	}
	msg := BuildWithOpts(context.Background(), evt, i18n.FormatOptions{
		Locale:          "zh-Hans",
		Style:           i18n.StyleDetailed,
		FrontendBaseURL: "https://example.com",
	}, nil, zap.NewNop())

	if msg.Title == "" {
		t.Error("zh-Hans formatter should produce non-empty title")
	}
	if msg.Locale != "zh-Hans" {
		t.Errorf("locale: got %q, want %q", msg.Locale, "zh-Hans")
	}
	if msg.Direction != "ltr" {
		t.Errorf("zh-Hans direction: got %q, want %q", msg.Direction, "ltr")
	}
}

func TestBuildWithOpts_AlertLocaleFallback(t *testing.T) {
	evt := alert.Event{
		Type:      alert.TypeMetric,
		AssetID:   1,
		Timestamp: time.Now(),
		Locale:    "zh-Hans",
		Violations: []alert.Violation{
			{
				Kind: alert.ViolationKindMetric,
				Metric: &alert.MetricContext{
					Name:      "cpu",
					Value:     90,
					Threshold: 80,
				},
			},
		},
	}
	// opts.Locale is empty; should fall back to alert.Locale.
	msg := BuildWithOpts(context.Background(), evt, i18n.FormatOptions{
		Style: i18n.StyleDetailed,
	}, nil, zap.NewNop())

	if msg.Locale != "zh-Hans" {
		t.Errorf("locale: got %q, want %q (from alert.Locale)", msg.Locale, "zh-Hans")
	}
}

func TestBuildWithOpts_DefaultLocaleWhenEmpty(t *testing.T) {
	evt := alert.Event{
		Type:      alert.TypeMetric,
		AssetID:   1,
		Timestamp: time.Now(),
		Violations: []alert.Violation{
			{
				Kind: alert.ViolationKindMetric,
				Metric: &alert.MetricContext{
					Name:      "cpu",
					Value:     90,
					Threshold: 80,
				},
			},
		},
	}
	// Both opts.Locale and alert.Locale are empty; should default to i18n.DefaultLocale ("zh-Hans").
	msg := BuildWithOpts(context.Background(), evt, i18n.FormatOptions{}, nil, zap.NewNop())

	if msg.Locale != i18n.DefaultLocale {
		t.Errorf("locale: got %q, want %q (default)", msg.Locale, i18n.DefaultLocale)
	}
}

func TestBuildWithOpts_StyleVariants(t *testing.T) {
	evt := alert.Event{
		Type:      alert.TypeMetric,
		AssetID:   1,
		Timestamp: time.Now(),
		Violations: []alert.Violation{
			{
				Kind: alert.ViolationKindMetric,
				Metric: &alert.MetricContext{
					Name:      "cpu_usage",
					Value:     90,
					Threshold: 80,
				},
			},
		},
	}

	concise := BuildWithOpts(context.Background(), evt, i18n.FormatOptions{
		Locale: "en-US",
		Style:  i18n.StyleConcise,
	}, nil, zap.NewNop())
	detailed := BuildWithOpts(context.Background(), evt, i18n.FormatOptions{
		Locale: "en-US",
		Style:  i18n.StyleDetailed,
	}, nil, zap.NewNop())
	technical := BuildWithOpts(context.Background(), evt, i18n.FormatOptions{
		Locale: "en-US",
		Style:  i18n.StyleTechnical,
	}, nil, zap.NewNop())

	if concise.Title == detailed.Title {
		t.Error("concise and detailed titles should differ")
	}
	if detailed.Title == technical.Title {
		t.Error("detailed and technical titles should differ")
	}
}

func TestBuildWithOpts_RTLDirection(t *testing.T) {
	// Register a minimal ar locale so the Formatter produces RTL direction.
	loader := i18n.NewLoader(zap.NewNop())
	registry := i18n.NewRegistry(zap.NewNop())
	if err := loader.LoadToRegistry(i18n.EmbeddedFS(), registry); err != nil {
		t.Fatalf("load builtin resources: %v", err)
	}
	// Add a minimal ar bundle to trigger RTL detection.
	registry.Register("ar", i18n.NewMessageMap("ar", map[string]string{
		"alert.metric.title.concise":       "تنبيه: {{.metric_name}}",
		"alert.metric.description.concise": "{{.current_value}}",
	}))

	formatter := i18n.NewDefaultFormatter(registry, zap.NewNop())

	evt := alert.Event{
		Type:      alert.TypeMetric,
		AssetID:   1,
		Timestamp: time.Now(),
		Violations: []alert.Violation{
			{
				Kind: alert.ViolationKindMetric,
				Metric: &alert.MetricContext{
					Name:      "cpu",
					Value:     90,
					Threshold: 80,
				},
			},
		},
	}
	msg := BuildWithOpts(context.Background(), evt, i18n.FormatOptions{
		Locale: "ar",
		Style:  i18n.StyleConcise,
	}, formatter, zap.NewNop())

	if msg.Direction != "rtl" {
		t.Errorf("ar direction: got %q, want %q", msg.Direction, "rtl")
	}
}

func TestBuildWithOpts_TitleFallback(t *testing.T) {
	// Use a nil formatter so the default is constructed; the default
	// Formatter backed by the built-in bundle should always produce a
	// non-empty title for metric alerts.
	evt := alert.Event{
		Type:      alert.TypeMetric,
		AssetID:   1,
		Timestamp: time.Now(),
		Violations: []alert.Violation{
			{
				Kind: alert.ViolationKindMetric,
				Metric: &alert.MetricContext{
					Name:      "cpu_usage",
					Value:     90,
					Threshold: 80,
				},
			},
		},
	}
	msg := BuildWithOpts(context.Background(), evt, i18n.FormatOptions{
		Locale: "en-US",
	}, nil, zap.NewNop())

	if msg.Title == "" {
		t.Error("title should fall back to metric name when formatter produces empty title")
	}
	if !strings.Contains(msg.Title, "cpu") {
		t.Errorf("title should contain metric name: %q", msg.Title)
	}
}

func TestBuildWithOpts_CustomFormatter(t *testing.T) {
	custom := &mockFormatter{
		msg: i18n.FormattedMessage{
			Title:       "Custom Title",
			Description: "Custom Description",
			Level:       "critical",
			Direction:   i18n.LTR,
			Fields:      map[string]string{"custom": "value"},
		},
	}
	evt := alert.Event{
		Type:      alert.TypeMetric,
		AssetID:   1,
		Timestamp: time.Now(),
		Violations: []alert.Violation{
			{
				Kind: alert.ViolationKindMetric,
				Metric: &alert.MetricContext{
					Name:      "cpu",
					Value:     90,
					Threshold: 80,
				},
			},
		},
	}
	msg := BuildWithOpts(context.Background(), evt, i18n.FormatOptions{
		Locale: "en-US",
	}, custom, zap.NewNop())

	if msg.Title != "Custom Title" {
		t.Errorf("title: got %q, want %q", msg.Title, "Custom Title")
	}
	if msg.Description != "Custom Description" {
		t.Errorf("description: got %q, want %q", msg.Description, "Custom Description")
	}
	if msg.Fields["custom"] != "value" {
		t.Errorf("fields should contain custom key")
	}
}

// mockFormatter is a test double for i18n.Formatter.
type mockFormatter struct {
	msg i18n.FormattedMessage
}

func (m *mockFormatter) Format(_ context.Context, _ alert.Event, _ i18n.FormatOptions) i18n.FormattedMessage {
	return m.msg
}

// ---------------------------------------------------------------------------
// Render: two-tier dispatch (Template → Formatter)
// ---------------------------------------------------------------------------

func TestRender_FormatterPath(t *testing.T) {
	custom := &mockFormatter{
		msg: i18n.FormattedMessage{
			Title:       "Formatted Title",
			Description: "Formatted Description",
			Level:       "critical",
			Direction:   i18n.RTL,
			Fields:      map[string]string{"k": "v"},
		},
	}
	evt := alert.Event{
		Type:      alert.TypeMetric,
		AssetID:   1,
		Timestamp: time.Now(),
		Violations: []alert.Violation{
			{
				Kind: alert.ViolationKindMetric,
				Metric: &alert.MetricContext{
					Name:      "cpu",
					Value:     90,
					Threshold: 80,
				},
			},
		},
	}
	msg := Render(context.Background(), evt, RenderOptions{
		Formatter: custom,
		Logger:    zap.NewNop(),
	})
	if msg.Title != "Formatted Title" {
		t.Errorf("formatter title: got %q, want %q", msg.Title, "Formatted Title")
	}
	if msg.Direction != "rtl" {
		t.Errorf("formatter direction: got %q, want %q", msg.Direction, "rtl")
	}
}

func TestRender_TemplatePath(t *testing.T) {
	// Build a Library with one template and a Registry with basic resources.
	registry := i18n.NewRegistry(zap.NewNop())
	loader := i18n.NewLoader(zap.NewNop())
	if err := loader.LoadToRegistry(i18n.EmbeddedFS(), registry); err != nil {
		t.Fatalf("load i18n resources: %v", err)
	}

	lib := template.NewBuiltinLibrary(zap.NewNop())
	evt := alert.Event{
		Type:       alert.TypeMetric,
		AssetID:    1,
		Timestamp:  time.Now(),
		Locale:     "zh-Hans",
		TemplateID: "cpu_high",
		Violations: []alert.Violation{
			{
				Kind: alert.ViolationKindMetric,
				Metric: &alert.MetricContext{
					Name:      "cpu_usage",
					Value:     95.5,
					Threshold: 90.0,
				},
			},
		},
	}
	msg := Render(context.Background(), evt, RenderOptions{
		Library:  lib,
		Registry: registry,
		Logger:   zap.NewNop(),
	})
	if msg.Title == "" {
		t.Error("template path should produce non-empty title")
	}
	if msg.Locale != "zh-Hans" {
		t.Errorf("locale: got %q, want %q", msg.Locale, "zh-Hans")
	}
	if msg.Fields == nil {
		t.Error("fields should be non-nil")
	}
}

func TestRender_TemplateFallback(t *testing.T) {
	// Library is set but TemplateID is empty → should use Formatter path.
	custom := &mockFormatter{
		msg: i18n.FormattedMessage{
			Title:       "Formatter Title",
			Description: "Formatter Description",
			Level:       "warning",
			Direction:   i18n.LTR,
		},
	}
	lib := template.NewBuiltinLibrary(zap.NewNop())
	evt := alert.Event{
		Type:      alert.TypeMetric,
		AssetID:   1,
		Timestamp: time.Now(),
		// TemplateID intentionally empty.,
		Violations: []alert.Violation{
			{
				Kind: alert.ViolationKindMetric,
				Metric: &alert.MetricContext{
					Name:      "cpu",
					Value:     90,
					Threshold: 80,
				},
			},
		},
	}
	msg := Render(context.Background(), evt, RenderOptions{
		Formatter: custom,
		Library:   lib,
		Logger:    zap.NewNop(),
	})
	if msg.Title != "Formatter Title" {
		t.Errorf("should fall back to formatter: got %q, want %q", msg.Title, "Formatter Title")
	}
}

func TestRender_TemplateNotFound(t *testing.T) {
	// Library is set, TemplateID is set but doesn't exist → fallback to Formatter.
	custom := &mockFormatter{
		msg: i18n.FormattedMessage{
			Title:       "Fallback Title",
			Description: "Fallback Description",
			Level:       "warning",
			Direction:   i18n.LTR,
		},
	}
	lib := template.NewBuiltinLibrary(zap.NewNop())
	evt := alert.Event{
		Type:       alert.TypeMetric,
		AssetID:    1,
		Timestamp:  time.Now(),
		TemplateID: "nonexistent_template",
		Violations: []alert.Violation{
			{
				Kind: alert.ViolationKindMetric,
				Metric: &alert.MetricContext{
					Name:      "cpu",
					Value:     90,
					Threshold: 80,
				},
			},
		},
	}
	msg := Render(context.Background(), evt, RenderOptions{
		Formatter: custom,
		Library:   lib,
		Logger:    zap.NewNop(),
	})
	if msg.Title != "Fallback Title" {
		t.Errorf("should fall back to formatter on template error: got %q, want %q", msg.Title, "Fallback Title")
	}
}

func TestRender_FormattedToMessage_TitleFallback(t *testing.T) {
	// When the template renderer produces an empty title, formattedToMessage
	// should fall back to the alert's metric name / keyword / type.
	lib := template.NewBuiltinLibrary(zap.NewNop())
	evt := alert.Event{
		Type:       alert.TypeMetric,
		AssetID:    1,
		Timestamp:  time.Now(),
		Locale:     "en-US",
		TemplateID: "cpu_high",
		Violations: []alert.Violation{
			{
				Kind: alert.ViolationKindMetric,
				Metric: &alert.MetricContext{
					Name:      "cpu_usage",
					Value:     95.5,
					Threshold: 90.0,
				},
			},
		},
	}
	msg := Render(context.Background(), evt, RenderOptions{
		Library: lib,
		Logger:  zap.NewNop(),
	})
	// The builtin cpu_high template should produce a non-empty title.
	if msg.Title == "" {
		t.Error("title should not be empty for a valid template")
	}
}

// ---------------------------------------------------------------------------
// IntranetLinkHint: L0 plain-notification link annotation
// ---------------------------------------------------------------------------

func TestIntranetLinkHint(t *testing.T) {
	// No registry injected: the English fallback label is used.
	if got := IntranetLinkHint(nil, ""); got != "intranet address" {
		t.Errorf("nil registry: got %q, want fallback %q", got, "intranet address")
	}

	registry := i18n.NewRegistry(zap.NewNop())
	loader := i18n.NewLoader(zap.NewNop())
	if err := loader.LoadToRegistry(i18n.EmbeddedFS(), registry); err != nil {
		t.Fatalf("load builtin resources: %v", err)
	}

	if got := IntranetLinkHint(registry, "zh-Hans"); got != "内网地址" {
		t.Errorf("zh-Hans: got %q, want %q", got, "内网地址")
	}
	if got := IntranetLinkHint(registry, "en-US"); got != "intranet address" {
		t.Errorf("en-US: got %q, want %q", got, "intranet address")
	}
	// Unknown locale resolves to the default bundle rather than failing;
	// the key exists there, so any localized value (not the raw key) is
	// acceptable.
	if got := IntranetLinkHint(registry, "xx-XX"); got == "" || got == "notify.link_intranet_hint" {
		t.Errorf("unknown locale: got %q, want a resolved label", got)
	}
}
