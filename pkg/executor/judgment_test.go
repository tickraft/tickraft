// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see details in LICENSE.

package executor

import (
	"testing"

	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/types"
)

// TestConfigExpression pins the extraction of the optional "expression"
// key from executor config JSON: the assembly-side half of the judgment
// transmission chain.
func TestConfigExpression(t *testing.T) {
	cases := []struct {
		config string
		want   string
	}{
		{``, ``},
		{`{}`, ``},
		{`{"command":"true"}`, ``},
		{`{"expression":"code == 200"}`, `code == 200`},
		{`{"command":"true","expression":"code == 0"}`, `code == 0`},
		{`{"expression":""}`, ``},
		{`not-json`, ``},
	}
	for _, tc := range cases {
		if got := ConfigExpression(tc.config); got != tc.want {
			t.Errorf("ConfigExpression(%q) = %q, want %q", tc.config, got, tc.want)
		}
	}
}

// TestBuildExecutionEnvCode pins the unified code projection: HTTP status
// for protocol executors, exit code for command executors, 0 otherwise.
func TestBuildExecutionEnvCode(t *testing.T) {
	cases := []struct {
		name   string
		result *Result
		want   int
	}{
		{"nil result", nil, 0},
		{"http success", &Result{StatusCode: 200}, 200},
		{"http failure", &Result{StatusCode: 503}, 503},
		{"local nonzero exit", &Result{ExitCode: 3}, 3},
		{"local missing command", &Result{ExitCode: -1}, -1},
		{"connectivity executor", &Result{}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := buildExecutionEnv(tc.result).Code; got != tc.want {
				t.Errorf("Code = %d, want %d", got, tc.want)
			}
		})
	}
}

// TestBuildExecutionEnvFields pins the remaining env projections: body,
// error, duration (milliseconds), and metrics passthrough.
func TestBuildExecutionEnvFields(t *testing.T) {
	env := buildExecutionEnv(&Result{
		Status:   types.AssetStatusAbnormal,
		Body:     "boom",
		ErrorMsg: "timeout",
		Duration: 1500 * 1000 * 1000, // 1.5s
		Metrics:  map[string]float64{"rtt_ms": 12.5},
	})
	if env.Body != "boom" || env.Error != "timeout" {
		t.Errorf("body/error projection: %+v", env)
	}
	if env.Duration != 1500 {
		t.Errorf("Duration = %v, want 1500 (ms)", env.Duration)
	}
	if env.Metrics["rtt_ms"] != 12.5 {
		t.Errorf("Metrics = %v", env.Metrics)
	}
	if env.Code != 0 {
		t.Errorf("Code = %d, want 0", env.Code)
	}
}

// TestApplyJudgment pins the judgment semantics of rule-engine-design
// : empty expression is protocol default, true forces success,
// false forces failure with an ErrorMsg note, and a runtime evaluation
// failure falls back to the protocol default.
func TestApplyJudgment(t *testing.T) {
	t.Run("empty expression keeps protocol default", func(t *testing.T) {
		r := &Result{Status: types.AssetStatusAbnormal}
		ApplyJudgment("", r, zap.NewNop())
		if r.Status != types.AssetStatusAbnormal {
			t.Errorf("Status = %q, want unchanged abnormal", r.Status)
		}
	})
	t.Run("nil result is a no-op", func(t *testing.T) {
		ApplyJudgment("code == 0", nil, zap.NewNop())
	})
	t.Run("true forces normal", func(t *testing.T) {
		r := &Result{Status: types.AssetStatusAbnormal, ExitCode: 3}
		ApplyJudgment("code == 3", r, zap.NewNop())
		if r.Status != types.AssetStatusNormal {
			t.Errorf("Status = %q, want normal", r.Status)
		}
	})
	t.Run("false forces abnormal with note", func(t *testing.T) {
		r := &Result{Status: types.AssetStatusNormal, StatusCode: 200}
		ApplyJudgment("code != 200", r, zap.NewNop())
		if r.Status != types.AssetStatusAbnormal {
			t.Errorf("Status = %q, want abnormal", r.Status)
		}
		if r.ErrorMsg != judgmentFailureNote {
			t.Errorf("ErrorMsg = %q, want %q", r.ErrorMsg, judgmentFailureNote)
		}
	})
	t.Run("false appends to existing error", func(t *testing.T) {
		r := &Result{Status: types.AssetStatusNormal, ErrorMsg: "slow"}
		ApplyJudgment("duration > 0", r, zap.NewNop())
		want := "slow; " + judgmentFailureNote
		if r.ErrorMsg != want {
			t.Errorf("ErrorMsg = %q, want %q", r.ErrorMsg, want)
		}
	})
	t.Run("runtime eval failure keeps protocol default", func(t *testing.T) {
		r := &Result{Status: types.AssetStatusNormal, Body: "ok"}
		// "matches" with an invalid regex compiles but fails at runtime.
		ApplyJudgment(`body matches "("`, r, zap.NewNop())
		if r.Status != types.AssetStatusNormal {
			t.Errorf("Status = %q, want unchanged normal (fallback)", r.Status)
		}
		if r.ErrorMsg != "" {
			t.Errorf("ErrorMsg = %q, want empty (fallback must not annotate)", r.ErrorMsg)
		}
	})
	t.Run("nil logger is tolerated", func(t *testing.T) {
		r := &Result{Status: types.AssetStatusNormal}
		ApplyJudgment(`body matches "("`, r, nil)
		if r.Status != types.AssetStatusNormal {
			t.Errorf("Status = %q, want unchanged", r.Status)
		}
	})
}

// TestAppendJudgmentNote pins the note formatting: no leading separator
// on an empty message and no duplicate note on repeated application.
func TestAppendJudgmentNote(t *testing.T) {
	if got := appendJudgmentNote(""); got != judgmentFailureNote {
		t.Errorf("appendJudgmentNote(\"\") = %q", got)
	}
	once := appendJudgmentNote("orig")
	if got := appendJudgmentNote(once); got != once {
		t.Errorf("duplicate note not suppressed: %q", got)
	}
}
