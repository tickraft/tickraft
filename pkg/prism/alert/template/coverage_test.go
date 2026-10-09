// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package template

import (
	"context"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/i18n"
	"github.com/tickraft/tickraft/pkg/prism/alert"
	"github.com/tickraft/tickraft/pkg/prism/alert/template/builtin"
)

// TestEmbeddedFS_ReturnsNonEmptyFS verifies builtin.EmbeddedFS returns a
// readable filesystem containing the TOML template files.
func TestEmbeddedFS_ReturnsNonEmptyFS(t *testing.T) {
	fsys := builtin.EmbeddedFS()
	entries, err := fsys.ReadDir(".")
	if err != nil {
		t.Fatalf("ReadDir(.): %v", err)
	}
	if len(entries) == 0 {
		t.Error("EmbeddedFS should contain at least the templates/ directory")
	}
	// Verify the templates directory exists.
	var hasTemplatesDir bool
	for _, e := range entries {
		if e.Name() == "templates" && e.IsDir() {
			hasTemplatesDir = true
		}
	}
	if !hasTemplatesDir {
		t.Error("EmbeddedFS should contain a templates/ directory")
	}
}

// TestLoadBuiltinTemplates_NilLogger verifies the loader does not panic
// with a nil logger and still returns all embedded templates.
func TestLoadBuiltinTemplates_NilLogger(t *testing.T) {
	templates := loadBuiltinTemplates(nil)
	if len(templates) != 10 {
		t.Errorf("expected 10 templates with nil logger, got %d", len(templates))
	}
}

// TestInferMetricLevel_SeverityBased covers the severity-based branches
// of inferMetricLevel. The function maps violation Severity to a level:
// critical/fatal/error → "critical", everything else → "warning".
func TestInferMetricLevel_SeverityBased(t *testing.T) {
	cases := []struct {
		name     string
		severity string
		want     string
	}{
		{"critical severity", "critical", "critical"},
		{"fatal severity", "fatal", "critical"},
		{"error severity", "error", "critical"},
		{"warning severity", "warning", "warning"},
		{"info severity", "info", "warning"},
		{"empty severity defaults to warning", "", "warning"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			evt := alert.Event{
				Violations: []alert.Violation{{
					Kind:     alert.ViolationKindMetric,
					Severity: tc.severity,
					Metric:   &alert.MetricContext{Value: 90, Threshold: 80},
				}},
			}
			if got := inferMetricLevel(evt); got != tc.want {
				t.Errorf("inferMetricLevel(severity=%q) = %q, want %q",
					tc.severity, got, tc.want)
			}
		})
	}
}

// TestFormatRelatedResources_NonEmpty covers the non-empty branch of
// formatRelatedResources.
func TestFormatRelatedResources_NonEmpty(t *testing.T) {
	resources := map[string]float64{
		"cpu":    90.5,
		"memory": 75.2,
	}
	result := formatRelatedResources(resources)
	if strings.Contains(result, "none") {
		t.Errorf("non-empty resources should not contain 'none': %q", result)
	}
	if !strings.Contains(result, "cpu=90.50") || !strings.Contains(result, "memory=75.20") {
		t.Errorf("result should contain formatted resources: %q", result)
	}
}

// TestFormatTimestamp_NilTranslator covers the nil-translator branch of
// formatTimestamp.
func TestFormatTimestamp_NilTranslator(t *testing.T) {
	evt := alert.Event{
		Timestamp: time.Date(2026, 7, 5, 12, 30, 0, 0, time.UTC),
	}
	got := formatTimestamp(evt, nil)
	if got != "2026-07-05 12:30:00" {
		t.Errorf("formatTimestamp with nil translator = %q, want %q", got, "2026-07-05 12:30:00")
	}
}

// TestBuildFields_NilTranslator covers the nil-translator branch of
// buildFields.
func TestBuildFields_NilTranslator(t *testing.T) {
	evt := alert.Event{
		Type:      alert.TypeMetric,
		AssetID:   1,
		Timestamp: time.Now(),
		Violations: []alert.Violation{{
			Kind:   alert.ViolationKindMetric,
			Metric: &alert.MetricContext{Name: "cpu", Value: 90, Threshold: 80},
		}},
	}
	fields := buildFields(evt, nil)
	if _, ok := fields["Metric"]; !ok {
		t.Errorf("nil translator should use raw 'Metric' label, got keys: %v", fields)
	}
}

// TestNewRenderer_NilLibrary verifies NewRenderer does not panic with nil
// library and returns a usable Renderer.
func TestNewRenderer_NilLibrary(t *testing.T) {
	r := NewRenderer(nil, nil, zap.NewNop())
	if r == nil {
		t.Fatal("NewRenderer should not return nil")
	}
}

// TestNewLibrary_NilLogger verifies NewLibrary does not panic with nil
// logger.
func TestNewLibrary_NilLogger(t *testing.T) {
	l := NewLibrary(nil)
	if l == nil {
		t.Fatal("NewLibrary should not return nil")
	}
	if _, err := l.Get("nonexistent"); err == nil {
		t.Error("Get on empty library should return error")
	}
}

// TestBuiltinLibrary_RendererWithFullRegistry verifies rendering a builtin
// template using a fully-loaded i18n Registry produces localized field
// labels.
func TestBuiltinLibrary_RendererWithFullRegistry(t *testing.T) {
	loader := i18n.NewLoader(zap.NewNop())
	registry := i18n.NewRegistry(zap.NewNop())
	if err := loader.LoadToRegistry(i18n.EmbeddedFS(), registry); err != nil {
		t.Fatalf("LoadToRegistry: %v", err)
	}
	lib := NewBuiltinLibrary(zap.NewNop())
	r := NewRenderer(lib, registry, zap.NewNop())

	evt := alert.Event{
		Type:      alert.TypeMetric,
		AssetID:   1,
		TenantID:  1,
		Timestamp: time.Date(2026, 7, 5, 12, 30, 0, 0, time.UTC),
		Violations: []alert.Violation{{
			Kind: alert.ViolationKindMetric,
			Metric: &alert.MetricContext{
				Name:      "cpu_usage",
				Value:     92.5,
				Threshold: 80.0,
				Metrics:   map[string]float64{"memory": 70.5},
			},
		}},
	}

	msg, err := r.Render(context.Background(), evt, RenderOptions{
		TemplateID:      "cpu_high",
		Locale:          "zh-Hans",
		Style:           StyleDetailed,
		FrontendBaseURL: "https://app.example.com",
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if msg.Title == "" {
		t.Error("title should be non-empty")
	}
	if len(msg.Fields) == 0 {
		t.Error("fields should be populated")
	}
}

// panickingLibrary is a Library implementation whose Get method always
// panics, used to verify the renderer's panic isolation.
type panickingLibrary struct{}

func (panickingLibrary) Register(Template)            {}
func (panickingLibrary) Get(string) (Template, error) { panic("boom from panickingLibrary") }
func (panickingLibrary) List() []Template             { return nil }

// TestRenderer_RenderRecoversFromLibraryPanic verifies that a panicking
// custom Library implementation does not crash the renderer; the panic is
// recovered and surfaced as a wrapped error.
func TestRenderer_RenderRecoversFromLibraryPanic(t *testing.T) {
	r := NewRenderer(panickingLibrary{}, nil, zap.NewNop())
	_, err := r.Render(context.Background(), alert.Event{
		Type:      alert.TypeMetric,
		AssetID:   1,
		Timestamp: time.Now(),
	}, RenderOptions{TemplateID: "cpu_high"})
	if err == nil {
		t.Fatal("expected error from panicking library, got nil")
	}
}
