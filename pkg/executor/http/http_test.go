// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package http

import (
	"context"
	"io"
	nethttp "net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tickraft/tickraft/pkg/executor"
	"github.com/tickraft/tickraft/pkg/types"
)

func TestName(t *testing.T) {
	p := New()
	if got := p.Name(); got != "http" {
		t.Errorf("Name: got %q, want %q", got, "http")
	}
}

// TestCapabilities pins the dual-mode declaration: the executor probes
// (OpProbe) and executes task actions (OpExecute).
func TestCapabilities(t *testing.T) {
	p := New()
	want := executor.CapProbe | executor.CapExec
	if got := p.Capabilities(); got != want {
		t.Errorf("Capabilities: got %v, want %v", got, want)
	}
	if !executor.IsDualMode(p.Capabilities()) {
		t.Error("Capabilities: executor should report as dual-mode")
	}
}

// TestRegistryAcceptsBothOperations verifies the capability gate from the
// consumer side: LookupWithOp must resolve the executor for both operations.
func TestRegistryAcceptsBothOperations(t *testing.T) {
	reg := executor.NewRegistry()
	if err := reg.Register(New()); err != nil {
		t.Fatalf("register: %v", err)
	}
	for _, op := range []executor.Operation{executor.OpProbe, executor.OpExecute} {
		if _, err := reg.LookupWithOp("http", op); err != nil {
			t.Errorf("LookupWithOp(%s): %v", op, err)
		}
	}
}

// TestExecuteMissingAddress verifies that Execute returns an abnormal
// result when the executor config does not carry an address (URL).
func TestExecuteMissingAddress(t *testing.T) {
	p := New()
	result, err := p.Execute(context.Background(), executor.ExecutionRequest{
		ExecutorName: "http",
		Config:       `{}`,
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.Status != types.AssetStatusAbnormal {
		t.Errorf("Status: got %q, want %q", result.Status, types.AssetStatusAbnormal)
	}
}

// TestExecuteInvalidConfig verifies that Execute returns a parse error
// when the executor config is not valid JSON.
func TestExecuteInvalidConfig(t *testing.T) {
	p := New()
	_, err := p.Execute(context.Background(), executor.ExecutionRequest{
		ExecutorName: "http",
		Config:       `{invalid`,
	})
	if err == nil {
		t.Fatal("Execute with invalid config: expected error, got nil")
	}
}

// TestExecuteTaskModePost verifies the OpExecute path end to end: a POST
// request with method/headers/body from the task config succeeds and reports
// metrics.
func TestExecuteTaskModePost(t *testing.T) {
	var gotMethod, gotBody string
	var gotHeader string
	srv := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		gotMethod = r.Method
		gotHeader = r.Header.Get("X-Token")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(nethttp.StatusOK)
	}))
	defer srv.Close()

	p := New()
	cfg := `{"address":"` + srv.URL + `","method":"POST","headers":{"X-Token":"secret"},"body":"{\"a\":1}"}`
	result, err := p.Execute(context.Background(), executor.ExecutionRequest{
		ExecutorName: "http",
		Operation:    executor.OpExecute,
		Config:       cfg,
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.Status != types.AssetStatusNormal {
		t.Errorf("Status: got %q, want %q (%s)", result.Status, types.AssetStatusNormal, result.ErrorMsg)
	}
	if result.StatusCode != nethttp.StatusOK {
		t.Errorf("StatusCode: got %d, want %d", result.StatusCode, nethttp.StatusOK)
	}
	if gotMethod != "POST" || gotBody != `{"a":1}` || gotHeader != "secret" {
		t.Errorf("request echo: method=%q body=%q header=%q", gotMethod, gotBody, gotHeader)
	}
	if result.Metrics["status_code"] != float64(nethttp.StatusOK) {
		t.Errorf("Metrics status_code: got %v, want 200", result.Metrics["status_code"])
	}
	if _, ok := result.Metrics["rtt_ms"]; !ok {
		t.Error("Metrics rtt_ms missing")
	}
}

// TestExecuteRespectsContextTimeout verifies that timeout control belongs to
// the caller's context: a slow endpoint under a short deadline fails fast
// instead of running to the client's defensive ceiling.
func TestExecuteRespectsContextTimeout(t *testing.T) {
	srv := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(nethttp.StatusOK)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	p := New()
	start := time.Now()
	result, err := p.Execute(ctx, executor.ExecutionRequest{
		ExecutorName: "http",
		Config:       `{"address":"` + srv.URL + `"}`,
	})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.Status != types.AssetStatusAbnormal {
		t.Errorf("Status: got %q, want %q", result.Status, types.AssetStatusAbnormal)
	}
	if elapsed >= time.Second {
		t.Errorf("elapsed %s: context deadline did not bound the request", elapsed)
	}
}
