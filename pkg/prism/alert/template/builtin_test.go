// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package template

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/tickraft/tickraft/pkg/i18n"
	"github.com/tickraft/tickraft/pkg/prism/alert"
	"go.uber.org/zap"
)

func TestNewBuiltinLibrary_LoadsAllTemplates(t *testing.T) {
	l := NewBuiltinLibrary(zap.NewNop())
	list := l.List()
	if len(list) != 10 {
		t.Fatalf("expected 10 builtin templates, got %d: %+v", len(list), list)
	}

	expectedIDs := []string{
		"certificate_expiring",
		"connection_pool_exhausted",
		"cpu_high",
		"custom_metric_threshold",
		"disk_full",
		"http_error_rate_high",
		"log_keyword_matched",
		"memory_high",
		"network_unreachable",
		"service_down",
	}
	for i, want := range expectedIDs {
		if list[i].ID != want {
			t.Errorf("List[%d].ID = %q, want %q", i, list[i].ID, want)
		}
	}
}

func TestBuiltinLibrary_AllTemplatesValid(t *testing.T) {
	l := NewBuiltinLibrary(zap.NewNop())
	for _, tpl := range l.List() {
		if err := Validate(tpl); err != nil {
			t.Errorf("builtin template %q failed validation: %v", tpl.ID, err)
		}
	}
}

func TestBuiltinLibrary_RenderCpuHigh_EN_Concise(t *testing.T) {
	l := NewBuiltinLibrary(zap.NewNop())
	r := NewRenderer(l, nil, zap.NewNop())

	alert := alert.Event{
		Type:       alert.TypeMetric,
		AssetID:    42,
		Timestamp:  time.Date(2026, 7, 5, 12, 30, 0, 0, time.UTC),
		Violations: []alert.Violation{{Kind: alert.ViolationKindMetric, Metric: &alert.MetricContext{Name: "cpu_usage", Value: 92.5, Threshold: 80.0}}},
	}

	msg, err := r.Render(context.Background(), alert, RenderOptions{
		TemplateID: "cpu_high",
		Locale:     "en-US",
		Style:      StyleConcise,
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(msg.Title, "cpu_usage") {
		t.Errorf("en-US concise title should contain metric name: %q", msg.Title)
	}
}

func TestBuiltinLibrary_RenderCpuHigh_ZH_Detailed(t *testing.T) {
	l := NewBuiltinLibrary(zap.NewNop())
	r := NewRenderer(l, nil, zap.NewNop())

	alert := alert.Event{
		Type:       alert.TypeMetric,
		AssetID:    42,
		Timestamp:  time.Date(2026, 7, 5, 12, 30, 0, 0, time.UTC),
		Violations: []alert.Violation{{Kind: alert.ViolationKindMetric, Metric: &alert.MetricContext{Name: "cpu_usage", Value: 92.5, Threshold: 80.0}}},
	}

	msg, err := r.Render(context.Background(), alert, RenderOptions{
		TemplateID: "cpu_high",
		Locale:     "zh-Hans",
		Style:      StyleDetailed,
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(msg.Title, "cpu_usage") {
		t.Errorf("zh-Hans detailed title should contain metric name: %q", msg.Title)
	}
	if !strings.Contains(msg.Title, "告警") {
		t.Errorf("zh-Hans title should contain 告警: %q", msg.Title)
	}
}

func TestBuiltinLibrary_RenderMemoryHigh(t *testing.T) {
	l := NewBuiltinLibrary(zap.NewNop())
	r := NewRenderer(l, nil, zap.NewNop())

	alert := alert.Event{
		Type:       alert.TypeMetric,
		AssetID:    1,
		Timestamp:  time.Now(),
		Violations: []alert.Violation{{Kind: alert.ViolationKindMetric, Metric: &alert.MetricContext{Name: "memory_usage", Value: 95.0, Threshold: 85.0}}},
	}
	msg, err := r.Render(context.Background(), alert, RenderOptions{
		TemplateID: "memory_high",
		Locale:     "en-US",
		Style:      StyleDetailed,
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(msg.Title, "memory_usage") {
		t.Errorf("title should contain metric name: %q", msg.Title)
	}
}

func TestBuiltinLibrary_RenderDiskFull(t *testing.T) {
	l := NewBuiltinLibrary(zap.NewNop())
	r := NewRenderer(l, nil, zap.NewNop())

	alert := alert.Event{
		Type:       alert.TypeMetric,
		AssetID:    1,
		Timestamp:  time.Now(),
		Violations: []alert.Violation{{Kind: alert.ViolationKindMetric, Metric: &alert.MetricContext{Name: "disk_usage", Value: 98.0, Threshold: 90.0}}},
	}
	msg, err := r.Render(context.Background(), alert, RenderOptions{
		TemplateID: "disk_full",
		Locale:     "zh-Hans",
		Style:      StyleConcise,
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(msg.Title, "disk_usage") {
		t.Errorf("title should contain metric name: %q", msg.Title)
	}
}

func TestBuiltinLibrary_RenderServiceDown(t *testing.T) {
	l := NewBuiltinLibrary(zap.NewNop())
	r := NewRenderer(l, nil, zap.NewNop())

	alert := alert.Event{
		Type:      alert.TypeLog,
		AssetID:   1,
		Timestamp: time.Now(),
	}
	msg, err := r.Render(context.Background(), alert, RenderOptions{
		TemplateID: "service_down",
		Locale:     "en-US",
		Style:      StyleDetailed,
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(msg.Description, "not responding") {
		t.Errorf("description should mention not responding: %q", msg.Description)
	}
}

func TestBuiltinLibrary_RenderLogKeywordMatched(t *testing.T) {
	l := NewBuiltinLibrary(zap.NewNop())
	r := NewRenderer(l, nil, zap.NewNop())

	alert := alert.Event{
		Type:       alert.TypeLog,
		AssetID:    10,
		Timestamp:  time.Now(),
		Violations: []alert.Violation{{Kind: alert.ViolationKindLog, Severity: "error", Log: &alert.LogContext{Keyword: "OOM", Content: "out of memory"}, Source: "10.0.0.1"}},
	}
	msg, err := r.Render(context.Background(), alert, RenderOptions{
		TemplateID: "log_keyword_matched",
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

func TestBuiltinLibrary_RenderNetworkUnreachable(t *testing.T) {
	l := NewBuiltinLibrary(zap.NewNop())
	r := NewRenderer(l, nil, zap.NewNop())

	alert := alert.Event{
		Type:      alert.TypeLog,
		AssetID:   1,
		Timestamp: time.Now(),
	}
	msg, err := r.Render(context.Background(), alert, RenderOptions{
		TemplateID: "network_unreachable",
		Locale:     "zh-Hans",
		Style:      StyleDetailed,
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(msg.Title, "不可达") {
		t.Errorf("zh-Hans title should contain 不可达: %q", msg.Title)
	}
}

func TestBuiltinLibrary_RenderHttpErrorRateHigh(t *testing.T) {
	l := NewBuiltinLibrary(zap.NewNop())
	r := NewRenderer(l, nil, zap.NewNop())

	alert := alert.Event{
		Type:       alert.TypeMetric,
		AssetID:    1,
		Timestamp:  time.Now(),
		Violations: []alert.Violation{{Kind: alert.ViolationKindMetric, Metric: &alert.MetricContext{Name: "http_5xx_rate", Value: 15.0, Threshold: 5.0}}},
	}
	msg, err := r.Render(context.Background(), alert, RenderOptions{
		TemplateID: "http_error_rate_high",
		Locale:     "en-US",
		Style:      StyleDetailed,
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(msg.Title, "http_5xx_rate") {
		t.Errorf("title should contain metric name: %q", msg.Title)
	}
}

func TestBuiltinLibrary_RenderConnectionPoolExhausted(t *testing.T) {
	l := NewBuiltinLibrary(zap.NewNop())
	r := NewRenderer(l, nil, zap.NewNop())

	alert := alert.Event{
		Type:       alert.TypeMetric,
		AssetID:    1,
		Timestamp:  time.Now(),
		Violations: []alert.Violation{{Kind: alert.ViolationKindMetric, Metric: &alert.MetricContext{Name: "conn_pool_usage", Value: 99.0, Threshold: 85.0}}},
	}
	msg, err := r.Render(context.Background(), alert, RenderOptions{
		TemplateID: "connection_pool_exhausted",
		Locale:     "en-US",
		Style:      StyleConcise,
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(msg.Title, "conn_pool_usage") {
		t.Errorf("title should contain metric name: %q", msg.Title)
	}
}

func TestBuiltinLibrary_RenderCertificateExpiring(t *testing.T) {
	l := NewBuiltinLibrary(zap.NewNop())
	r := NewRenderer(l, nil, zap.NewNop())

	alert := alert.Event{
		Type:      alert.TypeLog,
		AssetID:   1,
		Timestamp: time.Now(),
	}
	msg, err := r.Render(context.Background(), alert, RenderOptions{
		TemplateID: "certificate_expiring",
		Locale:     "zh-Hans",
		Style:      StyleDetailed,
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(msg.Title, "证书") {
		t.Errorf("zh-Hans title should contain 证书: %q", msg.Title)
	}
}

func TestBuiltinLibrary_RenderCustomMetricThreshold(t *testing.T) {
	l := NewBuiltinLibrary(zap.NewNop())
	r := NewRenderer(l, nil, zap.NewNop())

	alert := alert.Event{
		Type:       alert.TypeMetric,
		AssetID:    1,
		Timestamp:  time.Now(),
		Violations: []alert.Violation{{Kind: alert.ViolationKindMetric, Metric: &alert.MetricContext{Name: "queue_depth", Value: 5000.0, Threshold: 1000.0}}},
	}
	msg, err := r.Render(context.Background(), alert, RenderOptions{
		TemplateID: "custom_metric_threshold",
		Locale:     "en-US",
		Style:      StyleTechnical,
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(msg.Title, "queue_depth") {
		t.Errorf("title should contain metric name: %q", msg.Title)
	}
}

func TestBuiltinLibrary_RenderAllTemplates(t *testing.T) {
	// Smoke test: render every builtin template with en-US/detailed to ensure
	// none of them error out.
	l := NewBuiltinLibrary(zap.NewNop())
	r := NewRenderer(l, nil, zap.NewNop())

	alert := alert.Event{
		Type:       alert.TypeMetric,
		AssetID:    1,
		Timestamp:  time.Now(),
		Violations: []alert.Violation{{Kind: alert.ViolationKindMetric, Metric: &alert.MetricContext{Name: "test_metric", Value: 90.0, Threshold: 80.0}, Severity: "error", Log: &alert.LogContext{Keyword: "test", Content: "test content"}, Source: "10.0.0.1"}},
	}

	for _, tpl := range l.List() {
		msg, err := r.Render(context.Background(), alert, RenderOptions{
			TemplateID: tpl.ID,
			Locale:     "en-US",
			Style:      StyleDetailed,
		})
		if err != nil {
			t.Errorf("Render %s: %v", tpl.ID, err)
			continue
		}
		if msg.Title == "" {
			t.Errorf("template %s produced empty title", tpl.ID)
		}
	}
}

func TestBuiltinLibrary_RenderAllTemplates_ZH(t *testing.T) {
	// Smoke test: render every builtin template with zh-Hans/detailed.
	l := NewBuiltinLibrary(zap.NewNop())
	r := NewRenderer(l, nil, zap.NewNop())

	alert := alert.Event{
		Type:       alert.TypeMetric,
		AssetID:    1,
		Timestamp:  time.Now(),
		Violations: []alert.Violation{{Kind: alert.ViolationKindMetric, Metric: &alert.MetricContext{Name: "test_metric", Value: 90.0, Threshold: 80.0}, Severity: "error", Log: &alert.LogContext{Keyword: "test", Content: "test content"}, Source: "10.0.0.1"}},
	}

	for _, tpl := range l.List() {
		msg, err := r.Render(context.Background(), alert, RenderOptions{
			TemplateID: tpl.ID,
			Locale:     "zh-Hans",
			Style:      StyleDetailed,
		})
		if err != nil {
			t.Errorf("Render %s zh-Hans: %v", tpl.ID, err)
			continue
		}
		if msg.Title == "" {
			t.Errorf("template %s zh-Hans produced empty title", tpl.ID)
		}
		if msg.Direction != i18n.LTR {
			t.Errorf("template %s zh-Hans direction = %q, want ltr", tpl.ID, msg.Direction)
		}
	}
}

func TestBuiltinLibrary_RenderAllStyles(t *testing.T) {
	// Smoke test: render cpu_high with all three styles.
	l := NewBuiltinLibrary(zap.NewNop())
	r := NewRenderer(l, nil, zap.NewNop())

	alert := alert.Event{
		Type:       alert.TypeMetric,
		AssetID:    1,
		Timestamp:  time.Now(),
		Violations: []alert.Violation{{Kind: alert.ViolationKindMetric, Metric: &alert.MetricContext{Name: "cpu_usage", Value: 92.5, Threshold: 80.0}}},
	}

	styles := []string{StyleConcise, StyleDetailed, StyleTechnical}
	for _, style := range styles {
		msg, err := r.Render(context.Background(), alert, RenderOptions{
			TemplateID: "cpu_high",
			Locale:     "en-US",
			Style:      style,
		})
		if err != nil {
			t.Errorf("Render cpu_high style=%s: %v", style, err)
			continue
		}
		if msg.Title == "" {
			t.Errorf("cpu_high style=%s produced empty title", style)
		}
	}
}
