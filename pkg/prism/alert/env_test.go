// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package alert

import (
	"testing"

	"github.com/tickraft/tickraft/pkg/asset"
	"github.com/tickraft/tickraft/pkg/expr"

	"github.com/tickraft/tickraft/pkg/types"
)

// TestBuildAlertEnv covers the projection from Event into AlertEnv,
// including primary-violation extraction and optional asset enrichment.
func TestBuildAlertEnv(t *testing.T) {
	evt := Event{
		Type:     TypeMetric,
		AssetID:  7,
		TenantID: 3,
		Violations: []Violation{{
			Kind:     ViolationKindMetric,
			Severity: "critical",
			Source:   "probe-1",
			Metric: &MetricContext{
				Name:    "cpu",
				Value:   95,
				Metrics: map[string]float64{"cpu": 95, "mem": 60},
			},
			Log: &LogContext{Keyword: "fatal", Content: "fatal: out of memory"},
		}},
	}

	t.Run("with asset enrichment", func(t *testing.T) {
		res := &asset.Asset{
			ID:        7,
			Name:      "web-1",
			AssetType: types.AssetType("host"),
			Metadata:  `{"env":"prod","tier":"web"}`,
		}
		env := buildAlertEnv(evt, res)
		if env.Type != "metric" {
			t.Errorf("Type = %q, want %q", env.Type, "metric")
		}
		if env.Severity != "critical" {
			t.Errorf("Severity = %q, want %q", env.Severity, "critical")
		}
		if env.Source != "probe-1" {
			t.Errorf("Source = %q, want %q", env.Source, "probe-1")
		}
		if env.Keyword != "fatal" || env.Content != "fatal: out of memory" {
			t.Errorf("log fields = %q/%q, want fatal/fatal: out of memory", env.Keyword, env.Content)
		}
		if env.Metrics["cpu"] != 95 || env.Metrics["mem"] != 60 {
			t.Errorf("Metrics = %v, want cpu=95 mem=60", env.Metrics)
		}
		if env.Asset["id"] != int64(7) {
			t.Errorf("asset.id = %v, want 7", env.Asset["id"])
		}
		if env.Asset["name"] != "web-1" {
			t.Errorf("asset.name = %v, want web-1", env.Asset["name"])
		}
		if env.Asset["type"] != "host" {
			t.Errorf("asset.type = %v, want host", env.Asset["type"])
		}
		tags, _ := env.Asset["tags"].(map[string]string)
		if tags["env"] != "prod" || tags["tier"] != "web" {
			t.Errorf("asset.tags = %v, want env=prod tier=web", tags)
		}
	})

	t.Run("without enrichment keeps event asset id and empty fields", func(t *testing.T) {
		env := buildAlertEnv(evt, nil)
		if env.Asset["id"] != int64(7) {
			t.Errorf("asset.id = %v, want 7", env.Asset["id"])
		}
		if env.Asset["name"] != "" || env.Asset["type"] != "" {
			t.Errorf("asset name/type should be empty without enrichment, got %v/%v",
				env.Asset["name"], env.Asset["type"])
		}
		tags, _ := env.Asset["tags"].(map[string]string)
		if tags == nil || len(tags) != 0 {
			t.Errorf("asset.tags should be an empty non-nil map, got %v", tags)
		}
	})

	t.Run("non-metric event yields empty non-nil metrics map", func(t *testing.T) {
		logEvt := Event{
			Type: TypeLog,
			Violations: []Violation{{
				Kind: ViolationKindLog,
				Log:  &LogContext{Keyword: "panic", Content: "panic: nil deref"},
			}},
		}
		env := buildAlertEnv(logEvt, nil)
		if env.Metrics == nil || len(env.Metrics) != 0 {
			t.Errorf("Metrics should be empty non-nil map on log events, got %v", env.Metrics)
		}
	})

	t.Run("event without violations yields zero-value scalars", func(t *testing.T) {
		env := buildAlertEnv(Event{Type: TypeStatus, AssetID: 9}, nil)
		if env.Severity != "" || env.Source != "" || env.Keyword != "" || env.Content != "" {
			t.Errorf("scalars should be empty without violations, got %+v", env)
		}
	})
}

// TestAlertEnvVariables exercises every documented AlertEnv variable
// through the Compiler so each name/type contract is pinned: a positive
// case that must evaluate true and a negative case that must evaluate
// false against the same environment.
func TestAlertEnvVariables(t *testing.T) {
	env := ExampleEnv()
	cases := []struct {
		expression string
		want       bool
	}{
		// Top-level scalars.
		{`type == "metric"`, true},
		{`type == "log"`, false},
		{`severity == "critical"`, true},
		{`severity == "info"`, false},
		{`source == "10.0.0.1"`, true},
		{`source == "10.0.0.2"`, false},
		// Log-oriented fields via string operators.
		{`keyword contains "ata"`, true},
		{`keyword contains "xyz"`, false},
		{`content matches "fatal.*memory"`, true},
		{`content startsWith "fatal"`, true},
		{`content endsWith "xyz"`, false},
		// Metrics map (open keys).
		{`metrics["cpu"] > 90`, true},
		{`metrics["cpu"] > 99`, false},
		{`metrics["nonexistent"] > 0`, false}, // missing key reads as zero
		// Asset domain (closed field set).
		{`asset.id == 42`, true},
		{`asset["id"] == 42`, true}, // dual access asset.id ≡ asset["id"]
		{`asset.name == "web-1"`, true},
		{`asset.type == "host"`, true},
		{`asset.type == "container"`, false},
		{`asset.tags["env"] == "prod"`, true},
		{`asset.tags["missing"] == ""`, true},
		// Cross-variable composition.
		{`severity == "critical" && metrics["cpu"] > 90`, true},
		{`type == "log" || metrics["cpu"] > 90`, true},
		{`type == "log" && keyword != ""`, false},
	}
	compiler := NewCompiler()
	for _, tc := range cases {
		t.Run(tc.expression, func(t *testing.T) {
			program, err := compiler.Compile(tc.expression)
			if err != nil {
				t.Fatalf("Compile(%q): %v", tc.expression, err)
			}
			got, err := expr.RunBool(program, env)
			if err != nil {
				t.Fatalf("eval %q: %v", tc.expression, err)
			}
			if got != tc.want {
				t.Errorf("eval %q = %v, want %v", tc.expression, got, tc.want)
			}
		})
	}
}

// TestCompilerValidate pins the validation entry point shared by the
// store write path: the example environment must accept canonical
// expressions and reject unknown names and domain typos.
func TestCompilerValidate(t *testing.T) {
	compiler := NewCompiler()
	valid := []string{
		`metrics["cpu"] > 90`,
		`severity == "critical"`,
		`asset.name contains "web"`,
		`keyword matches "fatal.*"`,
		`metrics["cpu"] > 90 && asset.tags["env"] == "prod"`,
	}
	for _, expression := range valid {
		if err := compiler.Validate(expression); err != nil {
			t.Errorf("Validate(%q) = %v, want nil", expression, err)
		}
	}
	invalid := map[string]string{
		"unknown top-level variable": "event.metrics[\"cpu\"] > 90",
		"unknown asset field":        "asset.naem == \"web-1\"",
		"syntax error":               "metrics[\"cpu\"] >",
	}
	for name, expression := range invalid {
		if err := compiler.Validate(expression); err == nil {
			t.Errorf("Validate(%q) (%s) = nil, want error", expression, name)
		}
	}
}
