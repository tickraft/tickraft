// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see details in LICENSE.

package httpapi

import (
	"net/http"
	"strings"
	"testing"
)

// exprValidateBody is the request payload of POST /api/v1/expr/validate.
type exprValidateBody struct {
	Env        string `json:"env"`
	Expression string `json:"expression"`
}

// TestExprValidate covers the expression validation endpoint shared by
// the rule editors: the three env contracts, their representative
// positive and negative expressions, and the empty-expression semantics
// (valid for the optional envs, rejected for alert rules).
func TestExprValidate(t *testing.T) {
	hs := newHarness(t)
	token := hs.login(adminUsername, adminPassword)

	valid := []struct {
		env        string
		expression string
	}{
		{"alert", `metrics["cpu"] > 90`},
		{"alert", `severity == "critical" && asset.tags["env"] == "prod"`},
		{"alert", `content contains "OOM" || keyword matches "oom|killed"`},
		{"remediation", `metric.value > threshold && trigger == "metric"`},
		{"remediation", `status.previous == "normal" && status.current == "abnormal"`},
		{"remediation", `asset.name == "web-1" || source matches "^10\\."`},
		{"execution", `code == 200 && duration < 500`},
		{"execution", `error == "" && metrics["rtt_ms"] < 100`},
		{"execution", `body contains "\"status\":\"ok\""`},
	}
	for _, tc := range valid {
		status, env := hs.do("POST", "/api/v1/expr/validate",
			exprValidateBody{Env: tc.env, Expression: tc.expression}, token)
		var data struct {
			Valid bool `json:"valid"`
		}
		hs.mustOK(status, env, "expr/validate "+tc.env, &data)
		if !data.Valid {
			t.Errorf("env=%s expression=%q: expected valid, got invalid", tc.env, tc.expression)
		}
	}

	invalid := []struct {
		env        string
		expression string
		messageSub string
	}{
		{"alert", `asset.naem == "web-1"`, "unknown field asset.naem"},
		{"alert", `metrics["cpu"] >`, ""},
		{"alert", `unknownvar > 1`, ""},
		{"alert", `metrics["cpu"] == "hot"`, ""},
		{"remediation", `metric.value > threshold &&`, ""},
		{"remediation", `status.curr == "abnormal"`, "unknown field status.curr"},
		{"execution", `code == "200"`, ""},
	}
	for _, tc := range invalid {
		status, env := hs.do("POST", "/api/v1/expr/validate",
			exprValidateBody{Env: tc.env, Expression: tc.expression}, token)
		if status != http.StatusBadRequest {
			t.Errorf("env=%s expression=%q: expected 400, got %d (code=%d, %s)",
				tc.env, tc.expression, status, env.Code, env.Message)
			continue
		}
		if env.Code != 40000 {
			t.Errorf("env=%s expression=%q: expected code 40000, got %d",
				tc.env, tc.expression, env.Code)
		}
		if tc.messageSub != "" && !strings.Contains(env.Message, tc.messageSub) {
			t.Errorf("env=%s expression=%q: message %q does not contain %q",
				tc.env, tc.expression, env.Message, tc.messageSub)
		}
	}

	// Unknown env is rejected.
	status, env := hs.do("POST", "/api/v1/expr/validate",
		exprValidateBody{Env: "nosuch", Expression: "1 == 1"}, token)
	if status != http.StatusBadRequest || !strings.Contains(env.Message, "env must be one of") {
		t.Fatalf("unknown env: expected 400 with env message, got %d (code=%d, %s)",
			status, env.Code, env.Message)
	}

	// Empty expression: valid for the optional envs (default semantics),
	// rejected for alert rules (expression is required there).
	for _, envName := range []string{"remediation", "execution"} {
		status, env := hs.do("POST", "/api/v1/expr/validate",
			exprValidateBody{Env: envName, Expression: ""}, token)
		var data struct {
			Valid bool `json:"valid"`
		}
		hs.mustOK(status, env, "expr/validate empty "+envName, &data)
		if !data.Valid {
			t.Errorf("env=%s empty expression: expected valid (optional field), got invalid", envName)
		}
	}
	status, _ = hs.do("POST", "/api/v1/expr/validate",
		exprValidateBody{Env: "alert", Expression: ""}, token)
	if status != http.StatusBadRequest {
		t.Errorf("alert empty expression: expected 400, got %d", status)
	}

	// The endpoint requires authentication.
	status, _ = hs.do("POST", "/api/v1/expr/validate",
		exprValidateBody{Env: "alert", Expression: "1 == 1"}, "")
	if status != http.StatusUnauthorized {
		t.Errorf("unauthenticated request: expected 401, got %d", status)
	}
}
