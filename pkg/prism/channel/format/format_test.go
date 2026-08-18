// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
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
)

func loadRegistry(t *testing.T) i18n.Registry {
	t.Helper()
	loader := i18n.NewLoader(zap.NewNop())
	r := i18n.NewRegistry(zap.NewNop())
	if err := loader.LoadToRegistry(i18n.EmbeddedFS(), r); err != nil {
		t.Fatalf("LoadToRegistry: %v", err)
	}
	return r
}

func TestNewFormatter_NilLogger(t *testing.T) {
	r := loadRegistry(t)
	f := NewFormatter(r, nil)
	if f == nil {
		t.Fatal("NewFormatter returned nil")
	}
	evt := alert.Event{
		Type:      alert.TypeMetric,
		AssetID:   1,
		Timestamp: time.Now(),
		Violations: []alert.Violation{{
			Kind:   alert.ViolationKindMetric,
			Metric: &alert.MetricContext{Name: "cpu", Value: 90, Threshold: 80},
		}},
	}
	msg := f.Format(context.Background(), evt, i18n.FormatOptions{Locale: "en-US"})
	if msg.Title == "" {
		t.Error("formatter produced empty title")
	}
}

func TestNewFormatter_NilRegistry(t *testing.T) {
	// A nil registry should not panic; the formatter will fall back to
	// key names. This case is mainly for defensive programming.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("NewFormatter with nil registry panicked: %v", r)
		}
	}()
	f := NewFormatter(nil, zap.NewNop())
	if f == nil {
		t.Fatal("NewFormatter returned nil")
	}
}

func TestBuild_Defaults(t *testing.T) {
	r := loadRegistry(t)
	f := NewFormatter(r, zap.NewNop())
	evt := alert.Event{
		Type:      alert.TypeMetric,
		AssetID:   42,
		Timestamp: time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC),
		Violations: []alert.Violation{{
			Kind:   alert.ViolationKindMetric,
			Metric: &alert.MetricContext{Name: "cpu_usage", Value: 92.5, Threshold: 80.0},
		}},
	}

	msg := Build(evt, "https://app.example.com", f)
	if !strings.Contains(msg.Title, "cpu_usage") {
		t.Errorf("Build title should contain metric name: %q", msg.Title)
	}
	if msg.Direction != i18n.LTR {
		t.Errorf("Build default direction = %q, want ltr", msg.Direction)
	}
	if msg.AssetLink == "" {
		t.Error("Build should produce a asset link with frontendBaseURL")
	}
	if !strings.Contains(msg.AssetLink, "42") {
		t.Errorf("Build asset link should contain asset ID: %q", msg.AssetLink)
	}
}

func TestBuild_NilFormatter(t *testing.T) {
	// When f is nil, Build should construct a default formatter backed by
	// an empty registry. The output will contain fallback key names rather
	// than rendered text, but the call must not panic.
	evt := alert.Event{
		Type:      alert.TypeMetric,
		AssetID:   1,
		Timestamp: time.Now(),
		Violations: []alert.Violation{{
			Kind:   alert.ViolationKindMetric,
			Metric: &alert.MetricContext{Name: "cpu", Value: 90, Threshold: 80},
		}},
	}
	msg := Build(evt, "", nil)
	if msg.Title == "" {
		t.Error("Build with nil formatter should still produce a non-empty title (fallback key)")
	}
}

func TestBuildWithOpts_CustomLocale(t *testing.T) {
	r := loadRegistry(t)
	f := NewFormatter(r, zap.NewNop())
	evt := alert.Event{
		Type:      alert.TypeMetric,
		AssetID:   1,
		Timestamp: time.Now(),
		Violations: []alert.Violation{{
			Kind:   alert.ViolationKindMetric,
			Metric: &alert.MetricContext{Name: "cpu_usage", Value: 92.5, Threshold: 80.0},
		}},
	}

	msg := BuildWithOpts(evt, i18n.FormatOptions{
		Locale: "zh-Hans",
		Style:  i18n.StyleConcise,
	}, f)
	if !strings.Contains(msg.Title, "cpu_usage") {
		t.Errorf("zh-Hans concise title should contain metric name: %q", msg.Title)
	}
	if !strings.Contains(msg.Title, "告警") {
		t.Errorf("zh-Hans title should contain 告警: %q", msg.Title)
	}
}

func TestBuildWithOpts_NilFormatter(t *testing.T) {
	evt := alert.Event{
		Type:      alert.TypeLog,
		AssetID:   10,
		Timestamp: time.Now(),
		Violations: []alert.Violation{{
			Kind:     alert.ViolationKindLog,
			Severity: "error",
			Log:      &alert.LogContext{Keyword: "OOM", Content: "out of memory"},
			Source:   "10.0.0.1",
		}},
	}
	msg := BuildWithOpts(evt, i18n.FormatOptions{Locale: "en-US"}, nil)
	if msg.Title == "" {
		t.Error("BuildWithOpts with nil formatter should produce a non-empty title")
	}
}

func TestBuildWithOpts_StyleVariants(t *testing.T) {
	r := loadRegistry(t)
	f := NewFormatter(r, zap.NewNop())
	evt := alert.Event{
		Type:      alert.TypeMetric,
		AssetID:   1,
		Timestamp: time.Now(),
		Violations: []alert.Violation{{
			Kind:   alert.ViolationKindMetric,
			Metric: &alert.MetricContext{Name: "cpu", Value: 90, Threshold: 80},
		}},
	}

	concise := BuildWithOpts(evt, i18n.FormatOptions{Locale: "en-US", Style: i18n.StyleConcise}, f)
	detailed := BuildWithOpts(evt, i18n.FormatOptions{Locale: "en-US", Style: i18n.StyleDetailed}, f)
	technical := BuildWithOpts(evt, i18n.FormatOptions{Locale: "en-US", Style: i18n.StyleTechnical}, f)

	if concise.Title == detailed.Title {
		t.Error("concise and detailed titles should differ")
	}
	if detailed.Title == technical.Title {
		t.Error("detailed and technical titles should differ")
	}
}

func TestBuildWithOpts_RTLDirection(t *testing.T) {
	r := loadRegistry(t)
	// Register a minimal ar locale so Resolve returns a non-default bundle.
	r.Register("ar", i18n.NewMessageMap("ar", map[string]string{
		"alert.metric.title.concise":       "تنبيه: {{.metric_name}}",
		"alert.metric.description.concise": "{{.current_value}}",
	}))
	f := NewFormatter(r, zap.NewNop())
	evt := alert.Event{
		Type:      alert.TypeMetric,
		AssetID:   1,
		Timestamp: time.Now(),
		Violations: []alert.Violation{{
			Kind:   alert.ViolationKindMetric,
			Metric: &alert.MetricContext{Name: "cpu", Value: 90, Threshold: 80},
		}},
	}
	msg := BuildWithOpts(evt, i18n.FormatOptions{Locale: "ar", Style: i18n.StyleConcise}, f)
	if msg.Direction != i18n.RTL {
		t.Errorf("ar direction = %q, want rtl", msg.Direction)
	}
}
