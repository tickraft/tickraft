// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package template

import (
	"context"
	"errors"
	"strings"
	"testing"

	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/prism/alert"
)

// scopedSample returns a metric alert event bound to the given template ID.
func scopedSample(templateID string) alert.Event {
	return alert.Event{
		Type:       alert.TypeMetric,
		TemplateID: templateID,
		AssetID:    7,
		Violations: []alert.Violation{{
			Kind: alert.ViolationKindMetric,
			Metric: &alert.MetricContext{
				Name:      "cpu_usage",
				Value:     91.5,
				Threshold: 85,
			},
		}},
	}
}

// registerScoped registers a minimal renderable template with the given ID
// and network scope. The title template distinguishes variants.
func registerScoped(l Library, id, scope string) {
	l.Register(Template{
		ID:           id,
		Name:         id,
		AlertType:    AlertTypeMetric,
		NetworkScope: scope,
		Translations: map[string]map[string]string{
			"en-US": {
				"title.detailed":       "T:" + id,
				"description.detailed": "D:" + id,
			},
		},
		Styles: []string{StyleDetailed},
	})
}

func TestScopedTemplateID(t *testing.T) {
	cases := []struct {
		id, scope, want string
	}{
		{"cpu_high", ScopeExtranet, "cpu_high_extranet"},
		{"cpu_high", ScopeIntranet, "cpu_high_intranet"},
		{"cpu_high", ScopeBoth, "cpu_high"},
		{"cpu_high", "", "cpu_high"},
		{"cpu_high", "bogus", "cpu_high"},
	}
	for _, tc := range cases {
		if got := ScopedTemplateID(tc.id, tc.scope); got != tc.want {
			t.Errorf("ScopedTemplateID(%q, %q) = %q, want %q", tc.id, tc.scope, got, tc.want)
		}
	}
}

func TestIsValidNetworkScope(t *testing.T) {
	for _, s := range []string{"", ScopeIntranet, ScopeExtranet, ScopeBoth} {
		if !IsValidNetworkScope(s) {
			t.Errorf("IsValidNetworkScope(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"public", "INTRANET", "both "} {
		if IsValidNetworkScope(s) {
			t.Errorf("IsValidNetworkScope(%q) = true, want false", s)
		}
	}
}

func TestRenderer_ScopeVariantWins(t *testing.T) {
	l := NewLibrary(zap.NewNop())
	registerScoped(l, "cpu_high", ScopeBoth)
	registerScoped(l, "cpu_high_extranet", ScopeExtranet)

	r := NewRenderer(l, nil, zap.NewNop())
	msg, err := r.Render(context.Background(), scopedSample("cpu_high"), RenderOptions{
		TemplateID:   "cpu_high",
		Locale:       "en-US",
		NetworkScope: ScopeExtranet,
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if msg.Title != "T:cpu_high_extranet" {
		t.Errorf("extranet scope should pick the variant template, got title %q", msg.Title)
	}

	msg, err = r.Render(context.Background(), scopedSample("cpu_high"), RenderOptions{
		TemplateID:   "cpu_high",
		Locale:       "en-US",
		NetworkScope: ScopeIntranet,
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if msg.Title != "T:cpu_high" {
		t.Errorf("intranet scope without variant should pick the base template, got title %q", msg.Title)
	}
}

func TestRenderer_ScopeRejectsForeignTemplate(t *testing.T) {
	l := NewLibrary(zap.NewNop())
	registerScoped(l, "cpu_high", ScopeIntranet)

	r := NewRenderer(l, nil, zap.NewNop())
	_, err := r.Render(context.Background(), scopedSample("cpu_high"), RenderOptions{
		TemplateID:   "cpu_high",
		Locale:       "en-US",
		NetworkScope: ScopeExtranet,
	})
	if !errors.Is(err, ErrTemplateNotFound) {
		t.Fatalf("extranet render of intranet-only template should fail with ErrTemplateNotFound, got %v", err)
	}
	if !strings.Contains(err.Error(), "does not serve scope") {
		t.Errorf("error should explain the scope mismatch, got %v", err)
	}
}

func TestRenderer_NoScopePolicyAcceptsAll(t *testing.T) {
	l := NewLibrary(zap.NewNop())
	registerScoped(l, "cpu_high", ScopeExtranet)

	r := NewRenderer(l, nil, zap.NewNop())
	opts := RenderOptions{TemplateID: "cpu_high", Locale: "en-US"}
	msg, err := r.Render(context.Background(), scopedSample("cpu_high"), opts)
	if err != nil {
		t.Fatalf("Render without scope policy should accept any template, got %v", err)
	}
	if msg.Title != "T:cpu_high" {
		t.Errorf("unexpected title %q", msg.Title)
	}
}

func TestRenderer_ScopeVariantMustServeScope(t *testing.T) {
	l := NewLibrary(zap.NewNop())
	registerScoped(l, "cpu_high", ScopeBoth)
	// A variant ID that declares the opposite scope must not be selected.
	registerScoped(l, "cpu_high_extranet", ScopeIntranet)

	r := NewRenderer(l, nil, zap.NewNop())
	msg, err := r.Render(context.Background(), scopedSample("cpu_high"), RenderOptions{
		TemplateID:   "cpu_high",
		Locale:       "en-US",
		NetworkScope: ScopeExtranet,
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if msg.Title != "T:cpu_high" {
		t.Errorf("variant with mismatched scope must be skipped, got title %q", msg.Title)
	}
}
