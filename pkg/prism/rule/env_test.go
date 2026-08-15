// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package rule

import (
	"testing"
	"time"

	"github.com/expr-lang/expr"
	"github.com/tickraft/tickraft/pkg/asset"
	"github.com/tickraft/tickraft/pkg/executor"
	a "github.com/tickraft/tickraft/pkg/prism/alert"
	"github.com/tickraft/tickraft/pkg/task"
	"github.com/tickraft/tickraft/pkg/telemetry"
	"github.com/tickraft/tickraft/pkg/types"
)

// compileAndRun is a test helper that compiles an expression through the rule
// Compiler for the given scene and evaluates it against env. It fails the test
// on any compile or runtime error and returns the boolean result.
func compileAndRun(t *testing.T, scene Scene, expression string, env any) bool {
	t.Helper()
	prog, err := NewCompiler().Compile(scene, expression)
	if err != nil {
		t.Fatalf("Compile scene=%s expr=%q: %v", scene, expression, err)
	}
	out, err := expr.Run(prog, env)
	if err != nil {
		t.Fatalf("Run scene=%s expr=%q: %v", scene, expression, err)
	}
	got, ok := out.(bool)
	if !ok {
		t.Fatalf("Run scene=%s expr=%q: expected bool, got %T (%v)", scene, expression, out, out)
	}
	return got
}

// TestEnv_TaskMatchEnv_FieldAccess exercises TaskMatchEnv field access: nested
// struct fields, map indexing, and the asset sub-view.
func TestEnv_TaskMatchEnv_FieldAccess(t *testing.T) {
	env := TaskMatchEnv{
		Task: TaskView{
			ID:           7,
			TenantID:     1,
			AssetID:      42,
			ExecutorType: "ssh",
			Priority:     9,
			Timeout:      30 * time.Second,
			Metadata:     map[string]string{"owner": "ops"},
		},
		Asset: AssetView{
			ID:       42,
			TenantID: 1,
			Type:     string(types.AssetTypeHost),
			Name:     "my-host",
			Status:   string(types.AssetStatusNormal),
		},
		Tags: map[string]string{"region": "cn-east-1"},
	}

	cases := []struct {
		name string
		expr string
		want bool
	}{
		{"executor type equals", `task.executor_type == "ssh"`, true},
		{"executor type not equals", `task.executor_type == "http"`, false},
		{"tag map hit", `tags["region"] == "cn-east-1"`, true},
		{"tag map miss yields empty", `tags["missing"] == ""`, true},
		{"asset name equals", `asset.name == "my-host"`, true},
		{"asset typed-string field via len", `len(asset.type) > 0`, true},
		{"priority comparison", `task.priority > 5`, true},
		{"metadata map access", `task.metadata["owner"] == "ops"`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := compileAndRun(t, SceneTask, tc.expr, env); got != tc.want {
				t.Errorf("expr=%q: got %v, want %v", tc.expr, got, tc.want)
			}
		})
	}
}

// TestEnv_ProbeMatchEnv_FieldAccess exercises ProbeMatchEnv field access across
// the result, report, and asset sub-views.
func TestEnv_ProbeMatchEnv_FieldAccess(t *testing.T) {
	env := ProbeMatchEnv{
		Result: ResultView{
			Status:     string(types.AssetStatusAbnormal),
			StatusCode: 500,
			Body:       "internal error",
			ErrorMsg:   "boom",
			Duration:   120 * time.Millisecond,
			Metrics:    map[string]float64{"cpu": 92.5},
		},
		Report: ReportView{
			AssetID:     42,
			TenantID:    1,
			AssetType:   string(types.AssetTypeHost),
			SourceType:  "syslog",
			RemoteAddr:  "10.0.0.1",
			CollectedAt: time.Now(),
			Metrics:     map[string]float64{"mem": 70},
			LogContent:  "disk error occurred",
			LogLevel:    "ERROR",
			Status:      string(types.AssetStatusAbnormal),
		},
		Asset: AssetView{ID: 42, Name: "my-host"},
	}

	cases := []struct {
		name string
		expr string
		want bool
	}{
		{"status code threshold", `result.status_code >= 300`, true},
		{"log content contains operator", `report.log_content contains "error"`, true},
		{"log content not contains", `report.log_content contains "warn"`, false},
		{"result metric map", `result.metrics["cpu"] > 80`, true},
		{"report source type", `report.source_type == "syslog"`, true},
		{"report log level", `report.log_level == "ERROR"`, true},
		{"asset name", `asset.name == "my-host"`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := compileAndRun(t, SceneProbe, tc.expr, env); got != tc.want {
				t.Errorf("expr=%q: got %v, want %v", tc.expr, got, tc.want)
			}
		})
	}
}

// TestEnv_MetricMatchEnv_FieldAccess exercises MetricMatchEnv field access
// across the alert and asset sub-views. The MetricView type was removed; rule
// expressions now reference alert.metrics[<name>] and alert.severity directly.
func TestEnv_MetricMatchEnv_FieldAccess(t *testing.T) {
	env := MetricMatchEnv{
		Alert: AlertView{
			Type:      string(a.TypeMetric),
			AssetID:   42,
			TenantID:  1,
			Timestamp: time.Now(),
			Severity:  "critical",
			Metrics:   map[string]float64{"cpu": 92},
			Keyword:   "panic",
			Content:   "panic: nil pointer",
			Source:    "10.0.0.2",
		},
		Asset: AssetView{ID: 42, Name: "my-host"},
	}

	cases := []struct {
		name string
		expr string
		want bool
	}{
		{"alert severity equals", `alert.severity == "critical"`, true},
		{"alert metrics map", `alert.metrics["cpu"] > 80`, true},
		{"alert keyword", `alert.keyword == "panic"`, true},
		// alert.type is stored as a plain string in the view so it can
		// be compared directly to a string literal.
		{"alert type equals", `alert.type == "metric"`, true},
		{"alert type not equals", `alert.type == "log"`, false},
		{"alert source", `alert.source == "10.0.0.2"`, true},
		{"alert content contains", `alert.content contains "nil"`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := compileAndRun(t, SceneMetric, tc.expr, env); got != tc.want {
				t.Errorf("expr=%q: got %v, want %v", tc.expr, got, tc.want)
			}
		})
	}
}

// TestEnv_EventAlias_FieldAccess verifies that the "event" alias,
// added so rule authors can write the intuitive `event.type == "..."`
// form, resolves and evaluates identically to the scene-specific
// handle. This regression-tests the bug where creating an alert rule
// with `event.type == "task.failed"` failed with
// "unknown name event (1:1)".
func TestEnv_EventAlias_FieldAccess(t *testing.T) {
	env := MetricMatchEnv{
		Alert: AlertView{
			Type:     string(a.TypeMetric),
			Severity: "critical",
			Metrics:  map[string]float64{"cpu": 92},
			Keyword:  "panic",
			Source:   "10.0.0.2",
		},
		Event: AlertView{
			Type:     string(a.TypeMetric),
			Severity: "critical",
			Metrics:  map[string]float64{"cpu": 92},
			Keyword:  "panic",
			Source:   "10.0.0.2",
		},
	}

	cases := []struct {
		name string
		expr string
		want bool
	}{
		{"event severity equals", `event.severity == "critical"`, true},
		{"event metrics map", `event.metrics["cpu"] > 80`, true},
		{"event keyword", `event.keyword == "panic"`, true},
		{"event source", `event.source == "10.0.0.2"`, true},
		// Direct string-literal equality on event.type — this is the
		// exact shape of the reported bug (`event.type == "task.failed"`).
		{"event type equals", `event.type == "metric"`, true},
		{"event type not equals", `event.type == "task.failed"`, false},
		// alias and canonical handle must agree.
		{"event equals alert severity", `event.severity == alert.severity`, true},
		{"event equals alert type", `event.type == alert.type`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := compileAndRun(t, SceneMetric, tc.expr, env); got != tc.want {
				t.Errorf("expr=%q: got %v, want %v", tc.expr, got, tc.want)
			}
		})
	}
}

// TestEnv_EventAlias_CompilesAcrossScenes verifies that the "event"
// alias is a recognized top-level name in every scene, so a rule
// author who writes `event.<field>` never hits "unknown name event"
// regardless of scene. The exact sub-fields available differ per
// scene; here we only assert the name itself compiles.
func TestEnv_EventAlias_CompilesAcrossScenes(t *testing.T) {
	cases := []struct {
		name  string
		scene Scene
		expr  string
	}{
		{"task scene", SceneTask, `event.priority > 5`},
		{"probe scene", SceneProbe, `len(event.source_type) >= 0`},
		{"metric scene", SceneMetric, `len(event.severity) >= 0`},
		{"remediation scene", SceneRemediation, `event.metric_value >= 0`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewCompiler().Compile(tc.scene, tc.expr); err != nil {
				t.Errorf("Compile scene=%s expr=%q: %v", tc.scene, tc.expr, err)
			}
		})
	}
}

// TestEnv_RemediationMatchEnv_FieldAccess exercises RemediationMatchEnv field
// access across the remediation and asset sub-views.
func TestEnv_RemediationMatchEnv_FieldAccess(t *testing.T) {
	env := RemediationMatchEnv{
		Remediation: RemediationView{
			Type:        string(a.TypeMetric),
			AssetID:     42,
			MetricName:  "cpu_usage",
			MetricValue: 92,
			Threshold:   80,
			Severity:    "critical",
			Keyword:     "panic",
			Content:     "panic: nil pointer",
			SourceIP:    "10.0.0.2",
			PrevStatus:  "normal",
			CurrStatus:  "abnormal",
		},
		Asset: AssetView{ID: 42, Name: "my-host"},
	}

	cases := []struct {
		name string
		expr string
		want bool
	}{
		{"remediation metric value", `remediation.metric_value > 80`, true},
		{"remediation threshold", `remediation.threshold == 80`, true},
		{"remediation metric name", `remediation.metric_name == "cpu_usage"`, true},
		{"remediation severity", `remediation.severity == "critical"`, true},
		{"remediation prev status", `remediation.prev_status == "normal"`, true},
		{"remediation curr status", `remediation.curr_status == "abnormal"`, true},
		{"asset name", `asset.name == "my-host"`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := compileAndRun(t, SceneRemediation, tc.expr, env); got != tc.want {
				t.Errorf("expr=%q: got %v, want %v", tc.expr, got, tc.want)
			}
		})
	}
}

// TestEnv_CustomFunction_Regex verifies the regex(pattern, target) function.
func TestEnv_CustomFunction_Regex(t *testing.T) {
	cases := []struct {
		name string
		expr string
		want bool
	}{
		{"match", `regex("^ERR[0-9]{4}", "ERR1234")`, true},
		{"no match", `regex("^ERR[0-9]{4}", "WARN1234")`, false},
		{"literal substring", `regex("error", "an error occurred")`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := compileAndRun(t, SceneMetric, tc.expr, MetricMatchEnv{}); got != tc.want {
				t.Errorf("expr=%q: got %v, want %v", tc.expr, got, tc.want)
			}
		})
	}
}

// TestEnv_CustomFunction_ContainsAny verifies the containsAny(haystack,
// needles) function. A dedicated env carrying a []string field is used because
// expr-lang types array literals as []interface{}, which is not assignable to
// the function's []string parameter.
func TestEnv_CustomFunction_ContainsAny(t *testing.T) {
	type ce struct {
		Haystack string   `expr:"haystack"`
		Needles  []string `expr:"needles"`
	}
	prog, err := expr.Compile(
		"containsAny(haystack, needles)",
		expr.AsBool(),
		expr.Env(ce{}),
		containsAnyFn,
	)
	if err != nil {
		t.Fatalf("Compile containsAny: %v", err)
	}

	cases := []struct {
		name string
		env  ce
		want bool
	}{
		{"hit first needle", ce{Haystack: "error occurred", Needles: []string{"error", "fatal"}}, true},
		{"hit second needle", ce{Haystack: "fatal crash", Needles: []string{"error", "fatal"}}, true},
		{"no needle present", ce{Haystack: "all good", Needles: []string{"error", "fatal"}}, false},
		{"empty needles", ce{Haystack: "error", Needles: []string{}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := expr.Run(prog, tc.env)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if got := out.(bool); got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// TestEnv_CustomFunction_InRange verifies the inRange(value, min, max) function.
func TestEnv_CustomFunction_InRange(t *testing.T) {
	cases := []struct {
		name  string
		value float64
		want  bool
	}{
		{"within", 50, true},
		{"lower bound", 0, true},
		{"upper bound", 100, true},
		{"above", 150, false},
		{"below", -1, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := MetricMatchEnv{Alert: AlertView{Metrics: map[string]float64{"cpu": tc.value}}}
			if got := compileAndRun(t, SceneMetric, `inRange(alert.metrics["cpu"], 0, 100)`, env); got != tc.want {
				t.Errorf("value=%v: got %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}

// TestEnv_CustomFunction_Ago verifies the ago(duration) function returns a time
// approximately duration before now, and that now() > ago(d) holds.
func TestEnv_CustomFunction_Ago(t *testing.T) {
	// Direct compilation (without AsBool) to inspect the returned time value.
	prog, err := expr.Compile(`ago("5m")`, agoFn)
	if err != nil {
		t.Fatalf("Compile ago: %v", err)
	}
	before := time.Now()
	out, err := expr.Run(prog, nil)
	if err != nil {
		t.Fatalf("Run ago: %v", err)
	}
	got, ok := out.(time.Time)
	if !ok {
		t.Fatalf("expected time.Time, got %T", out)
	}
	want := before.Add(-5 * time.Minute)
	// Allow a generous tolerance to keep the test deterministic under load.
	if diff := got.Sub(want); diff > time.Second || diff < -time.Second {
		t.Errorf("ago(\"5m\") = %v, want ~%v (diff=%v)", got, want, diff)
	}

	// ago yields a past instant, so now() > ago(d) is true.
	if !compileAndRun(t, SceneMetric, `now() > ago("5m")`, MetricMatchEnv{}) {
		t.Error(`expected now() > ago("5m") to be true`)
	}
}

// TestEnv_OptionalChainAndNilCoalescing verifies the ?. (optional chain) and
// ?? (nil coalescing) operators compile and evaluate against the env.
func TestEnv_OptionalChainAndNilCoalescing(t *testing.T) {
	env := TaskMatchEnv{
		Tags:  map[string]string{"region": "cn-east-1"},
		Asset: AssetView{Name: "my-host"},
	}
	cases := []struct {
		name string
		expr string
		want bool
	}{
		{"optional chain on map", `tags?.region == "cn-east-1"`, true},
		{"optional chain on struct", `asset?.name == "my-host"`, true},
		{"nil coalescing present key", `(tags["region"] ?? "unknown") == "cn-east-1"`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := compileAndRun(t, SceneTask, tc.expr, env); got != tc.want {
				t.Errorf("expr=%q: got %v, want %v", tc.expr, got, tc.want)
			}
		})
	}
}

// TestEnv_ToTaskView verifies the toTaskView projection from task.Task.
func TestEnv_ToTaskView(t *testing.T) {
	tk := task.Task{
		ID:           7,
		TenantID:     1,
		AssetID:      42,
		ExecutorName: "ssh",
		Priority:     9,
		Timeout:      30 * time.Second,
		Metadata:     map[string]string{"owner": "ops"},
	}
	got := toTaskView(tk)
	if got.ID != tk.ID || got.TenantID != tk.TenantID || got.AssetID != tk.AssetID {
		t.Errorf("identity fields mismatch: %+v", got)
	}
	if got.ExecutorType != tk.ExecutorName || got.Priority != tk.Priority || got.Timeout != tk.Timeout {
		t.Errorf("scalar fields mismatch: %+v", got)
	}
	if got.Metadata["owner"] != "ops" {
		t.Errorf("metadata mismatch: %+v", got.Metadata)
	}
}

// TestEnv_ToAssetView verifies the toAssetView projection.
func TestEnv_ToAssetView(t *testing.T) {
	res := asset.Asset{
		ID:        42,
		TenantID:  1,
		AssetType: types.AssetTypeHost,
		Name:      "my-host",
		Status:    types.AssetStatusNormal,
	}
	got := toAssetView(res)
	if got.ID != res.ID || got.TenantID != res.TenantID {
		t.Errorf("identity mismatch: %+v", got)
	}
	if got.Type != string(res.AssetType) || got.Name != res.Name || got.Status != string(res.Status) {
		t.Errorf("scalar mismatch: %+v", got)
	}
}

// TestEnv_ToResultView verifies the toResultView projection from executor.Result.
func TestEnv_ToResultView(t *testing.T) {
	result := executor.Result{
		Status:     types.AssetStatusAbnormal,
		StatusCode: 500,
		Body:       "boom",
		ErrorMsg:   "timeout",
		Duration:   2 * time.Second,
		Metrics:    map[string]float64{"cpu": 90},
	}
	got := toResultView(result)
	if got.Status != string(result.Status) || got.StatusCode != result.StatusCode || got.Body != result.Body {
		t.Errorf("fields mismatch: %+v", got)
	}
	if got.ErrorMsg != result.ErrorMsg || got.Duration != result.Duration {
		t.Errorf("fields mismatch: %+v", got)
	}
	if got.Metrics["cpu"] != 90 {
		t.Errorf("metrics mismatch: %+v", got.Metrics)
	}
}

// TestEnv_ToReportView verifies the toReportView projection from
// telemetry.Telemetry.
func TestEnv_ToReportView(t *testing.T) {
	report := telemetry.Telemetry{
		AssetID:     42,
		TenantID:    1,
		AssetType:   types.AssetTypeHost,
		SourceType:  "syslog",
		RemoteAddr:  "10.0.0.1",
		CollectedAt: time.Unix(1000, 0),
		Metrics:     map[string]float64{"mem": 70},
		LogContent:  "error",
		LogLevel:    "ERROR",
		Status:      types.AssetStatusAbnormal,
	}
	got := toReportView(report)
	if got.AssetID != report.AssetID || got.TenantID != report.TenantID {
		t.Errorf("identity mismatch: %+v", got)
	}
	if got.AssetType != string(report.AssetType) || got.SourceType != report.SourceType {
		t.Errorf("type fields mismatch: %+v", got)
	}
	if got.RemoteAddr != report.RemoteAddr || got.LogContent != report.LogContent || got.LogLevel != report.LogLevel {
		t.Errorf("text fields mismatch: %+v", got)
	}
	if got.Status != string(report.Status) || !got.CollectedAt.Equal(report.CollectedAt) {
		t.Errorf("status/time mismatch: %+v", got)
	}
	if got.Metrics["mem"] != 70 {
		t.Errorf("metrics mismatch: %+v", got.Metrics)
	}
}

// TestEnv_ToAlertView verifies the toAlertView projection from alert.Event.
// The metric/severity/keyword/content/source fields are projected from the
// event's primary violation (the most severe one) so a single struct exposes
// the fields rule expressions care about.
func TestEnv_ToAlertView(t *testing.T) {
	evt := a.Event{
		Type:      a.TypeMetric,
		AssetID:   42,
		TenantID:  1,
		Timestamp: time.Unix(2000, 0),
		Violations: []a.Violation{{
			Kind:     a.ViolationKindMetric,
			Severity: "critical",
			Source:   "10.0.0.2",
			Metric: &a.MetricContext{
				Name:      "cpu_usage",
				Value:     92,
				Threshold: 80,
				Metrics:   map[string]float64{"cpu": 92},
			},
			Log: &a.LogContext{
				Keyword: "panic",
				Content: "panic: nil",
			},
		}},
	}
	got := toAlertView(evt)
	if got.Type != string(evt.Type) || got.AssetID != evt.AssetID || got.TenantID != evt.TenantID {
		t.Errorf("identity mismatch: %+v", got)
	}
	if !got.Timestamp.Equal(evt.Timestamp) {
		t.Errorf("timestamp mismatch: %v vs %v", got.Timestamp, evt.Timestamp)
	}
	if got.Severity != "critical" {
		t.Errorf("Severity = %q, want %q", got.Severity, "critical")
	}
	if got.Metrics["cpu"] != 92 {
		t.Errorf("Metrics mismatch: %+v", got.Metrics)
	}
	if got.Keyword != "panic" || got.Content != "panic: nil" || got.Source != "10.0.0.2" {
		t.Errorf("text fields mismatch: %+v", got)
	}
}

// TestEnv_ToAlertView_NoViolations verifies that an event with no violations
// yields a zero-value AlertView for the projected fields while still carrying
// the event identity (type, asset, tenant, timestamp).
func TestEnv_ToAlertView_NoViolations(t *testing.T) {
	evt := a.Event{
		Type:      a.TypeLog,
		AssetID:   7,
		TenantID:  1,
		Timestamp: time.Unix(3000, 0),
	}
	got := toAlertView(evt)
	if got.Type != string(evt.Type) || got.AssetID != evt.AssetID || got.TenantID != evt.TenantID {
		t.Errorf("identity mismatch: %+v", got)
	}
	if got.Severity != "" {
		t.Errorf("Severity = %q, want empty", got.Severity)
	}
	if got.Metrics != nil {
		t.Errorf("Metrics = %+v, want nil", got.Metrics)
	}
}

// TestToFloat64 verifies the toFloat64 numeric coercion helper across all
// supported numeric kinds and the default (unsupported) fallback.
func TestToFloat64(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want float64
		ok   bool
	}{
		{"float64", float64(3.14), 3.14, true},
		{"float32", float32(2.5), 2.5, true},
		{"int", int(7), 7, true},
		{"int64", int64(8), 8, true},
		{"int32", int32(9), 9, true},
		{"string", "x", 0, false},
		{"uint unsupported", uint(1), 0, false},
		{"nil unsupported", nil, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := toFloat64(tc.in)
			if ok != tc.ok {
				t.Errorf("toFloat64(%v) ok = %v, want %v", tc.in, ok, tc.ok)
			}
			if got != tc.want {
				t.Errorf("toFloat64(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}
