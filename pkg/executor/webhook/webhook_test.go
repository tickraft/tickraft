// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package webhook

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tickraft/tickraft/pkg/executor"
	"github.com/tickraft/tickraft/pkg/types"
)

// TestName verifies the name identifier.
func TestName(t *testing.T) {
	e := New()
	if got := e.Name(); got != "webhook" {
		t.Errorf("Name(): got %q, want %q", got, "webhook")
	}
}

// TestCapabilities verifies the capability bitmask.
func TestCapabilities(t *testing.T) {
	e := New()
	if got := e.Capabilities(); got != executor.CapNotify {
		t.Errorf("Capabilities(): got %v, want %v", got, executor.CapNotify)
	}
}

// TestExecuteSuccess verifies that Execute sends an HTTP request and
// returns the response body with a normal status.
func TestExecuteSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("Method: got %q, want %q", r.Method, http.MethodPost)
		}
		body, _ := io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	e := New()
	cfg := config{
		Method: http.MethodPost,
		URL:    srv.URL,
		Body:   `{"key":"value"}`,
	}
	cfgBytes, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	req := executor.ExecutionRequest{
		Config:  string(cfgBytes),
		AssetID: 1,
	}
	result, err := e.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: got err %v, want nil", err)
	}
	if result.Status != types.AssetStatusNormal {
		t.Errorf("Status: got %q, want %q", result.Status, types.AssetStatusNormal)
	}
	if result.StatusCode != http.StatusOK {
		t.Errorf("StatusCode: got %d, want %d", result.StatusCode, http.StatusOK)
	}
	if result.Body != `{"key":"value"}` {
		t.Errorf("Body: got %q, want %q", result.Body, `{"key":"value"}`)
	}
}

// TestExecuteEmptyConfig verifies that Execute returns an error when the
// executor config is empty.
func TestExecuteEmptyConfig(t *testing.T) {
	e := New()
	req := executor.ExecutionRequest{}
	_, err := e.Execute(context.Background(), req)
	if err == nil {
		t.Fatal("Execute: got nil err, want error for empty config")
	}
}

// TestExecuteMissingURL verifies that Execute returns an error when the
// URL field is missing.
func TestExecuteMissingURL(t *testing.T) {
	e := New()
	req := executor.ExecutionRequest{
		Config: `{}`,
	}
	_, err := e.Execute(context.Background(), req)
	if err == nil {
		t.Fatal("Execute: got nil err, want error for missing URL")
	}
}

// TestExecuteServerError verifies that a 5xx response yields
// StatusAbnormal.
func TestExecuteServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	}))
	defer srv.Close()

	e := New()
	cfg := config{
		URL: srv.URL,
	}
	cfgBytes, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	req := executor.ExecutionRequest{
		Config: string(cfgBytes),
	}
	result, err := e.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: got err %v, want nil", err)
	}
	if result.Status != types.AssetStatusAbnormal {
		t.Errorf("Status: got %q, want %q", result.Status, types.AssetStatusAbnormal)
	}
	if result.StatusCode != http.StatusInternalServerError {
		t.Errorf("StatusCode: got %d, want %d", result.StatusCode, http.StatusInternalServerError)
	}
}

// TestExecuteWithHeaders verifies that custom headers are sent with the
// request.
func TestExecuteWithHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Custom"); got != "test-value" {
			t.Errorf("Header X-Custom: got %q, want %q", got, "test-value")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	e := New()
	cfg := config{
		URL:     srv.URL,
		Method:  http.MethodGet,
		Headers: map[string][]string{"X-Custom": {"test-value"}},
	}
	cfgBytes, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	req := executor.ExecutionRequest{
		Config: string(cfgBytes),
	}
	result, err := e.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: got err %v, want nil", err)
	}
	if result.Status != types.AssetStatusNormal {
		t.Errorf("Status: got %q, want %q", result.Status, types.AssetStatusNormal)
	}
}

// TestExecuteExpectStatus pins the expect_status contract (aligned with
// the http executor): an explicit code requires an exact match — a 2xx
// response can be judged abnormal and a non-2xx response normal.
func TestExecuteExpectStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	e := New()

	// A non-2xx response matching expect_status is normal.
	cfgBytes, err := json.Marshal(config{URL: srv.URL, ExpectStatus: http.StatusServiceUnavailable})
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	result, err := e.Execute(context.Background(), executor.ExecutionRequest{Config: string(cfgBytes)})
	if err != nil {
		t.Fatalf("Execute (expect 503, got 503): %v", err)
	}
	if result.Status != types.AssetStatusNormal {
		t.Errorf("Status: got %q, want %q (exact match on expect_status)", result.Status, types.AssetStatusNormal)
	}

	// A 2xx-mismatching expect_status would be abnormal; reuse the same
	// server with an impossible expectation.
	cfgBytes, err = json.Marshal(config{URL: srv.URL, ExpectStatus: http.StatusOK})
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	result, err = e.Execute(context.Background(), executor.ExecutionRequest{Config: string(cfgBytes)})
	if err != nil {
		t.Fatalf("Execute (expect 200, got 503): %v", err)
	}
	if result.Status != types.AssetStatusAbnormal {
		t.Errorf("Status: got %q, want %q (mismatch on expect_status)", result.Status, types.AssetStatusAbnormal)
	}
}
