// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package slack

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/i18n"
	"github.com/tickraft/tickraft/pkg/prism/alert"
	"github.com/tickraft/tickraft/pkg/prism/channel"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// sampleMetricAlert returns a representative metric Event used
// across tests.
func sampleMetricAlert() alert.Event {
	return alert.Event{
		Type:     alert.TypeMetric,
		AssetID:  42,
		TenantID: 7,
		Timestamp: time.Unix(1700000000,
			0).UTC(),
		Violations: []alert.Violation{
			{
				Kind: alert.ViolationKindMetric,
				Metric: &alert.MetricContext{
					Name:      "cpu_usage",
					Value:     95.5,
					Threshold: 90.0,
					Metrics: map[string]float64{
						"node-1": 88.0,
						"node-2": 91.0,
					},
				},
			},
		},
	}
}

// sampleLogAlert returns a representative log Event used across
// tests.
func sampleLogAlert() alert.Event {
	return alert.Event{
		Type:     alert.TypeLog,
		AssetID:  11,
		TenantID: 3,
		Timestamp: time.Unix(1700000100,
			0).UTC(),
		Violations: []alert.Violation{
			{
				Kind:     alert.ViolationKindLog,
				Severity: "error",
				Source:   "10.0.0.5",
				Log: &alert.LogContext{
					Keyword: "panic",
					Content: "runtime error: nil pointer dereference",
				},
			},
		},
	}
}

// asSendError extracts a *channel.SendError from err, failing the test if
// err is not a SendError.
func asSendError(t *testing.T, err error) *channel.SendError {
	t.Helper()
	if err == nil {
		t.Fatal("expected non-nil error")
	}
	var se *channel.SendError
	if !errors.As(err, &se) {
		t.Fatalf("expected *channel.SendError, got %T: %v", err, err)
	}
	return se
}

// newTestFormatter builds a Formatter backed by the built-in i18n resource
// bundle for use in tests. The returned Formatter renders locale-aware alert
// messages using the embedded resource files.
func newTestFormatter(t *testing.T) i18n.Formatter {
	t.Helper()
	logger := zap.NewNop()
	registry := i18n.NewRegistry(logger)
	loader := i18n.NewLoader(logger)
	if err := loader.LoadToRegistry(i18n.EmbeddedFS(), registry); err != nil {
		t.Fatalf("load builtin i18n resources: %v", err)
	}
	return i18n.NewDefaultFormatter(registry, logger)
}

// mustNew creates a Channel against the given TLS test server, failing
// the test on construction error. The server's TLS client is injected so
// the self-signed certificate is trusted. A default test Formatter is
// injected so formatPayload produces locale-aware content.
func mustNew(t *testing.T, srv *httptest.Server, opts ...Option) *Channel {
	t.Helper()
	allOpts := append([]Option{WithHTTPClient(srv.Client()), WithFormatter(newTestFormatter(t))}, opts...)
	ch, err := New(Config{WebhookURL: srv.URL}, allOpts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return ch
}

// okHandler returns an http.HandlerFunc that writes the Slack success
// body "ok" with a 200 status.
func okHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(okBody))
	}
}

// decodePayload unmarshals a Slack payload JSON body for inspection.
func decodePayload(t *testing.T, body []byte) payload {
	t.Helper()
	var p payload
	if err := json.Unmarshal(body, &p); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	return p
}

// hasField reports whether any field text contains substr.
func hasField(fields []textObject, substr string) bool {
	for _, f := range fields {
		if strings.Contains(f.Text, substr) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Config.Validate
// ---------------------------------------------------------------------------

// TestValidateEmptyURL verifies that an empty WebhookURL fails
// validation.
func TestValidateEmptyURL(t *testing.T) {
	if err := (Config{}).Validate(); err == nil {
		t.Fatal("expected error for empty WebhookURL")
	}
}

// TestValidateNonHTTPSURL verifies that non-https URLs fail validation.
func TestValidateNonHTTPSURL(t *testing.T) {
	for _, u := range []string{"ftp://example.com", "http://example.com", "example.com", "mailto:x@y"} {
		if err := (Config{WebhookURL: u}).Validate(); err == nil {
			t.Errorf("expected error for URL %q", u)
		}
	}
}

// TestValidateValidHTTPSURL verifies that https URLs pass validation.
func TestValidateValidHTTPSURL(t *testing.T) {
	for _, u := range []string{"https://example.com", "https://hooks.slack.com/services/T/B/X"} {
		if err := (Config{WebhookURL: u}).Validate(); err != nil {
			t.Errorf("unexpected error for URL %q: %v", u, err)
		}
	}
}

// ---------------------------------------------------------------------------
// New: construction & defaults
// ---------------------------------------------------------------------------

// TestNewInvalidConfig verifies that New rejects an invalid Config.
func TestNewInvalidConfig(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("expected error for empty Config")
	}
	if _, err := New(Config{WebhookURL: "http://x"}); err == nil {
		t.Fatal("expected error for non-https URL")
	}
}

// TestNewDefaults verifies that New applies default values when Config
// fields are zero.
func TestNewDefaults(t *testing.T) {
	ch, err := New(Config{WebhookURL: "https://example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if ch.cfg.RetryMaxAttempts != defaultRetryMaxAttempts {
		t.Errorf("RetryMaxAttempts: got %d, want %d", ch.cfg.RetryMaxAttempts, defaultRetryMaxAttempts)
	}
	if ch.cfg.RetryBaseInterval != defaultRetryBase {
		t.Errorf("RetryBaseInterval: got %v, want %v", ch.cfg.RetryBaseInterval, defaultRetryBase)
	}
	if ch.cfg.CircuitFailureThreshold != defaultCircuitThreshold {
		t.Errorf("CircuitFailureThreshold: got %d, want %d", ch.cfg.CircuitFailureThreshold, defaultCircuitThreshold)
	}
	if ch.cfg.CircuitCooldown != defaultCircuitCooldown {
		t.Errorf("CircuitCooldown: got %v, want %v", ch.cfg.CircuitCooldown, defaultCircuitCooldown)
	}
	if ch.httpClient == nil {
		t.Error("httpClient should be non-nil")
	}
	// The pooled client now comes from the httpclient builder with
	// RegionGlobal, so the region-aware 15s default applies instead of the
	// legacy package defaultTimeout (10s).
	if want := 15 * time.Second; ch.httpClient.Timeout != want {
		t.Errorf("httpClient.Timeout: got %v, want %v", ch.httpClient.Timeout, want)
	}
	if ch.circuit == nil || ch.retry == nil || ch.logger == nil {
		t.Error("circuit, retry, logger should all be non-nil")
	}
}

// TestNewWithOptions verifies that Options override Config fields.
func TestNewWithOptions(t *testing.T) {
	ch, err := New(
		Config{},
		WithWebhookURL("https://example.com"),
		WithChannel("#alerts"),
		WithRetry(10, 2*time.Second),
		WithCircuitBreaker(3, 15*time.Second),
	)
	if err != nil {
		t.Fatal(err)
	}
	if ch.cfg.WebhookURL != "https://example.com" {
		t.Errorf("WebhookURL: got %q", ch.cfg.WebhookURL)
	}
	if ch.cfg.Channel != "#alerts" {
		t.Errorf("Channel: got %q", ch.cfg.Channel)
	}
	if ch.cfg.RetryMaxAttempts != 10 {
		t.Errorf("RetryMaxAttempts: got %d", ch.cfg.RetryMaxAttempts)
	}
	if ch.cfg.RetryBaseInterval != 2*time.Second {
		t.Errorf("RetryBaseInterval: got %v", ch.cfg.RetryBaseInterval)
	}
	if ch.cfg.CircuitFailureThreshold != 3 {
		t.Errorf("CircuitFailureThreshold: got %d", ch.cfg.CircuitFailureThreshold)
	}
	if ch.cfg.CircuitCooldown != 15*time.Second {
		t.Errorf("CircuitCooldown: got %v", ch.cfg.CircuitCooldown)
	}
}

// TestNewWithHTTPClient verifies that an injected client is used and its
// zero timeout is set to the default.
//
// The bare &http.Client{} instances below are test fixtures used to verify
// the WithHTTPClient injection contract; production code should use
// httpx.NewPoolClient for connection pooling.
func TestNewWithHTTPClient(t *testing.T) {
	custom := &http.Client{}
	ch, err := New(Config{WebhookURL: "https://example.com"}, WithHTTPClient(custom))
	if err != nil {
		t.Fatal(err)
	}
	if ch.httpClient != custom {
		t.Error("injected client not used")
	}
	if ch.httpClient.Timeout != defaultTimeout {
		t.Errorf("httpClient.Timeout: got %v, want %v", ch.httpClient.Timeout, defaultTimeout)
	}
}

// TestNewWithHTTPClientTimeoutPreserved verifies that an injected client
// with a non-zero timeout keeps its timeout.
func TestNewWithHTTPClientTimeoutPreserved(t *testing.T) {
	custom := &http.Client{Timeout: 7 * time.Second}
	ch, err := New(Config{WebhookURL: "https://example.com"}, WithHTTPClient(custom))
	if err != nil {
		t.Fatal(err)
	}
	if ch.httpClient.Timeout != 7*time.Second {
		t.Errorf("httpClient.Timeout: got %v, want 7s", ch.httpClient.Timeout)
	}
}

// TestNewWithLogger verifies that an injected logger is used.
func TestNewWithLogger(t *testing.T) {
	logger := zap.NewExample()
	ch, err := New(Config{WebhookURL: "https://example.com"}, WithLogger(logger))
	if err != nil {
		t.Fatal(err)
	}
	if ch.logger != logger {
		t.Error("injected logger not used")
	}
}

// ---------------------------------------------------------------------------
// Name
// ---------------------------------------------------------------------------

// TestName verifies the channel name.
func TestName(t *testing.T) {
	ch, err := New(Config{WebhookURL: "https://example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if got := ch.Name(); got != "slack" {
		t.Errorf("Name(): got %q, want %q", got, "slack")
	}
}

// ---------------------------------------------------------------------------
// Send: success & retry
// ---------------------------------------------------------------------------

// TestSendSuccess verifies that a 200 "ok" response completes the send
// and the server receives a Slack-compatible JSON payload.
func TestSendSuccess(t *testing.T) {
	var received []byte
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type: got %q, want application/json", ct)
		}
		body, readErr := io.ReadAll(r.Body)
		if readErr != nil {
			t.Errorf("read body: %v", readErr)
		}
		received = body
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(okBody))
	}))
	defer srv.Close()

	ch := mustNew(t, srv, WithRetry(3, time.Millisecond))
	if err := ch.Send(context.Background(), sampleMetricAlert()); err != nil {
		t.Fatalf("Send: %v", err)
	}
	p := decodePayload(t, received)
	if p.Text == "" {
		t.Error("payload text should be non-empty")
	}
	if len(p.Blocks) < 2 {
		t.Fatalf("expected at least 2 blocks, got %d", len(p.Blocks))
	}
	if p.Blocks[0].Type != "header" {
		t.Errorf("first block type: got %q, want header", p.Blocks[0].Type)
	}
	if p.Blocks[1].Type != "section" {
		t.Errorf("second block type: got %q, want section", p.Blocks[1].Type)
	}
}

// TestSendRetryThenSuccess verifies that a 5xx on the first attempt is
// retried and the second attempt succeeds.
func TestSendRetryThenSuccess(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) < 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(okBody))
	}))
	defer srv.Close()

	ch := mustNew(t, srv, WithRetry(3, time.Millisecond))
	if err := ch.Send(context.Background(), sampleMetricAlert()); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("server calls: got %d, want 2", got)
	}
}

// TestSendRetryExhausted verifies that repeated 5xx responses exhaust
// retries and return a retryable SendError.
func TestSendRetryExhausted(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	ch := mustNew(t, srv, WithRetry(3, time.Millisecond))
	se := asSendError(t, ch.Send(context.Background(), sampleMetricAlert()))
	if !se.Retryable {
		t.Error("Retryable: got false, want true")
	}
	if se.ChannelName != "slack" {
		t.Errorf("ChannelName: got %q, want %q", se.ChannelName, "slack")
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("server calls: got %d, want 3", got)
	}
}

// TestSend4xxNoRetry verifies that a 4xx response is not retried and
// returns a non-retryable SendError.
func TestSend4xxNoRetry(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	ch := mustNew(t, srv, WithRetry(3, time.Millisecond))
	se := asSendError(t, ch.Send(context.Background(), sampleMetricAlert()))
	if se.Retryable {
		t.Error("Retryable: got true, want false")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("server calls: got %d, want 1 (no retry)", got)
	}
}

// TestSendNonOKBodyNoRetry verifies that a 2xx response with a non-"ok"
// body is treated as an API error and is not retried.
func TestSendNonOKBodyNoRetry(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("invalid_payload"))
	}))
	defer srv.Close()

	ch := mustNew(t, srv, WithRetry(3, time.Millisecond))
	se := asSendError(t, ch.Send(context.Background(), sampleMetricAlert()))
	if se.Retryable {
		t.Error("Retryable: got true, want false for non-ok body")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("server calls: got %d, want 1 (no retry)", got)
	}
}

// TestSendOKBodyWithWhitespace verifies that an "ok" body with
// surrounding whitespace is accepted as success.
func TestSendOKBodyWithWhitespace(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("  ok\n"))
	}))
	defer srv.Close()

	ch := mustNew(t, srv, WithRetry(3, time.Millisecond))
	if err := ch.Send(context.Background(), sampleMetricAlert()); err != nil {
		t.Fatalf("Send: %v", err)
	}
}

// TestSendNetworkError verifies that a network error is retryable and
// ultimately fails with a retryable SendError.
func TestSendNetworkError(t *testing.T) {
	srv := httptest.NewTLSServer(okHandler())
	client := srv.Client()
	addr := srv.URL
	srv.Close() // close the listener so connections are refused

	ch, err := New(Config{WebhookURL: addr}, WithHTTPClient(client), WithRetry(2, time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	se := asSendError(t, ch.Send(context.Background(), sampleMetricAlert()))
	if !se.Retryable {
		t.Error("Retryable: got false, want true for network error")
	}
}

// ---------------------------------------------------------------------------
// Send: empty URL guard
// ---------------------------------------------------------------------------

// TestSendEmptyURL verifies that Send returns a non-retryable error when
// the channel was constructed without a WebhookURL (defensive guard for
// direct construction).
func TestSendEmptyURL(t *testing.T) {
	ch := &Channel{}
	se := asSendError(t, ch.Send(context.Background(), sampleMetricAlert()))
	if se.Retryable {
		t.Error("Retryable: got true, want false")
	}
}

// ---------------------------------------------------------------------------
// Circuit breaker integration
// ---------------------------------------------------------------------------

// TestCircuitOpen verifies that after enough consecutive failures the
// breaker opens and Send returns ErrCircuitOpen without hitting the
// server.
func TestCircuitOpen(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	ch := mustNew(t, srv,
		WithRetry(1, time.Millisecond),
		WithCircuitBreaker(2, time.Hour),
	)

	// Two failed sends open the breaker (threshold 2).
	for i := range 2 {
		if err := ch.Send(context.Background(), sampleMetricAlert()); err == nil {
			t.Fatalf("send #%d: expected error", i+1)
		}
	}
	callsAtOpen := calls.Load()

	// Third send is short-circuited by the open breaker.
	err := ch.Send(context.Background(), sampleMetricAlert())
	if !errors.Is(err, channel.ErrCircuitOpen) {
		t.Fatalf("expected ErrCircuitOpen, got %v", err)
	}
	// No additional server calls should have been made.
	if got := calls.Load(); got != callsAtOpen {
		t.Errorf("server calls during open: got %d, want %d", got, callsAtOpen)
	}
}

// TestCircuitCooldownHalfOpenSuccess verifies that after the cooldown
// elapses the breaker transitions to half-open, a successful send closes
// it, and subsequent sends proceed normally.
func TestCircuitCooldownHalfOpenSuccess(t *testing.T) {
	var fail atomic.Bool
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if fail.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(okBody))
	}))
	defer srv.Close()

	ch := mustNew(t, srv,
		WithRetry(1, time.Millisecond),
		WithCircuitBreaker(2, 50*time.Millisecond),
	)

	// Open the breaker with two failures.
	fail.Store(true)
	for range 2 {
		if err := ch.Send(context.Background(), sampleMetricAlert()); err == nil {
			t.Fatal("expected failure")
		}
	}

	// Breaker is now open.
	if err := ch.Send(context.Background(), sampleMetricAlert()); !errors.Is(err, channel.ErrCircuitOpen) {
		t.Fatalf("expected ErrCircuitOpen, got %v", err)
	}

	// Wait for cooldown, then make the server succeed.
	time.Sleep(70 * time.Millisecond)
	fail.Store(false)

	// The next send is admitted as a half-open probe and succeeds,
	// closing the breaker.
	if err := ch.Send(context.Background(), sampleMetricAlert()); err != nil {
		t.Fatalf("half-open send should succeed, got %v", err)
	}

	// Subsequent sends should succeed normally without circuit errors.
	if err := ch.Send(context.Background(), sampleMetricAlert()); err != nil {
		t.Fatalf("post-recovery send should succeed, got %v", err)
	}
}

// TestCircuitResetsOnSuccess verifies that an intervening success resets
// the failure counter so the breaker does not open on stale failures.
func TestCircuitResetsOnSuccess(t *testing.T) {
	var fail atomic.Bool
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if fail.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(okBody))
	}))
	defer srv.Close()

	ch := mustNew(t, srv,
		WithRetry(1, time.Millisecond),
		WithCircuitBreaker(3, time.Hour),
	)

	// Two failures (below threshold 3).
	fail.Store(true)
	for range 2 {
		_ = ch.Send(context.Background(), sampleMetricAlert())
	}

	// Success resets the counter.
	fail.Store(false)
	if err := ch.Send(context.Background(), sampleMetricAlert()); err != nil {
		t.Fatalf("success send: %v", err)
	}

	// Two more failures should not open the breaker (counter was reset).
	fail.Store(true)
	for range 2 {
		_ = ch.Send(context.Background(), sampleMetricAlert())
	}

	// Next send should be admitted (breaker still closed), not
	// ErrCircuitOpen.
	err := ch.Send(context.Background(), sampleMetricAlert())
	if errors.Is(err, channel.ErrCircuitOpen) {
		t.Fatal("breaker should still be closed after reset + 2 failures")
	}
}

// ---------------------------------------------------------------------------
// formatPayload
// ---------------------------------------------------------------------------

// TestFormatPayloadMetric verifies the payload structure for a metric
// alert, including the header, summary, and metric-specific fields.
func TestFormatPayloadMetric(t *testing.T) {
	ch, err := New(Config{WebhookURL: "https://example.com"}, WithFormatter(newTestFormatter(t)))
	if err != nil {
		t.Fatal(err)
	}
	body, fmtErr := ch.formatPayload(context.Background(), sampleMetricAlert())
	if fmtErr != nil {
		t.Fatalf("formatPayload: %v", fmtErr)
	}
	p := decodePayload(t, body)

	if !strings.Contains(p.Text, "cpu_usage") {
		t.Errorf("text should contain metric name, got %q", p.Text)
	}
	if len(p.Blocks) < 2 {
		t.Fatalf("expected at least 2 blocks, got %d", len(p.Blocks))
	}
	if p.Blocks[0].Type != "header" {
		t.Errorf("header block type: got %q", p.Blocks[0].Type)
	}
	if p.Blocks[0].Text == nil || !strings.Contains(p.Blocks[0].Text.Text, "cpu_usage") {
		t.Errorf("header text should contain metric name, got %+v", p.Blocks[0].Text)
	}
	fields := p.Blocks[1].Fields
	if !hasField(fields, "cpu_usage") {
		t.Error("fields should contain metric name")
	}
	if !hasField(fields, "95.5") {
		t.Error("fields should contain metric value")
	}
	if !hasField(fields, "90") {
		t.Error("fields should contain threshold")
	}
}

// TestFormatPayloadLog verifies the payload structure for a log alert,
// including level, keyword, source IP, and content fields.
func TestFormatPayloadLog(t *testing.T) {
	ch, err := New(Config{WebhookURL: "https://example.com"}, WithFormatter(newTestFormatter(t)))
	if err != nil {
		t.Fatal(err)
	}
	body, fmtErr := ch.formatPayload(context.Background(), sampleLogAlert())
	if fmtErr != nil {
		t.Fatalf("formatPayload: %v", fmtErr)
	}
	p := decodePayload(t, body)

	if !strings.Contains(p.Text, "nil pointer") {
		t.Errorf("text should contain alert content, got %q", p.Text)
	}
	fields := p.Blocks[1].Fields
	if !hasField(fields, "error") {
		t.Error("fields should contain level")
	}
	if !hasField(fields, "panic") {
		t.Error("fields should contain keyword")
	}
	if !hasField(fields, "10.0.0.5") {
		t.Error("fields should contain source IP")
	}
	if !hasField(fields, "nil pointer") {
		t.Error("fields should contain content")
	}
}

// TestFormatPayloadLogMinimal verifies that a log alert without source IP
// or content omits those fields.
func TestFormatPayloadLogMinimal(t *testing.T) {
	ch, err := New(Config{WebhookURL: "https://example.com"}, WithFormatter(newTestFormatter(t)))
	if err != nil {
		t.Fatal(err)
	}
	evt := alert.Event{
		Type:     alert.TypeLog,
		AssetID:  1,
		TenantID: 1,
		Timestamp: time.Unix(1700000000,
			0).UTC(),
		Violations: []alert.Violation{
			{
				Kind:     alert.ViolationKindLog,
				Severity: "warn",
				Log: &alert.LogContext{
					Keyword: "timeout",
				},
			},
		},
	}
	body, fmtErr := ch.formatPayload(context.Background(), evt)
	if fmtErr != nil {
		t.Fatalf("formatPayload: %v", fmtErr)
	}
	p := decodePayload(t, body)
	fields := p.Blocks[1].Fields
	if hasField(fields, "Source IP") {
		t.Error("fields should not contain Source IP")
	}
	if hasField(fields, "Content") {
		t.Error("fields should not contain Content")
	}
}

// TestFormatPayloadDefaultType verifies that an unknown alert type uses
// the default summary and header branches.
func TestFormatPayloadDefaultType(t *testing.T) {
	ch, err := New(Config{WebhookURL: "https://example.com"}, WithFormatter(newTestFormatter(t)))
	if err != nil {
		t.Fatal(err)
	}
	evt := alert.Event{
		Type:      "custom",
		AssetID:   99,
		TenantID:  1,
		Timestamp: time.Unix(1700000000, 0).UTC(),
	}
	body, fmtErr := ch.formatPayload(context.Background(), evt)
	if fmtErr != nil {
		t.Fatalf("formatPayload: %v", fmtErr)
	}
	p := decodePayload(t, body)
	if !strings.Contains(p.Text, "custom") {
		t.Errorf("text should contain type, got %q", p.Text)
	}
	if p.Blocks[0].Text == nil || !strings.Contains(p.Blocks[0].Text.Text, "custom") {
		t.Errorf("header should contain type, got %+v", p.Blocks[0].Text)
	}
}

// TestFormatPayloadWithChannel verifies that the channel field is set in
// the payload when configured.
func TestFormatPayloadWithChannel(t *testing.T) {
	ch, err := New(Config{WebhookURL: "https://example.com"}, WithChannel("#ops"), WithFormatter(newTestFormatter(t)))
	if err != nil {
		t.Fatal(err)
	}
	body, fmtErr := ch.formatPayload(context.Background(), sampleMetricAlert())
	if fmtErr != nil {
		t.Fatalf("formatPayload: %v", fmtErr)
	}
	p := decodePayload(t, body)
	if p.Channel != "#ops" {
		t.Errorf("Channel: got %q, want #ops", p.Channel)
	}
}

// TestFormatPayloadNoChannel verifies that the channel field is omitted
// when not configured.
func TestFormatPayloadNoChannel(t *testing.T) {
	ch, err := New(Config{WebhookURL: "https://example.com"}, WithFormatter(newTestFormatter(t)))
	if err != nil {
		t.Fatal(err)
	}
	body, fmtErr := ch.formatPayload(context.Background(), sampleMetricAlert())
	if fmtErr != nil {
		t.Fatalf("formatPayload: %v", fmtErr)
	}
	p := decodePayload(t, body)
	if p.Channel != "" {
		t.Errorf("Channel: got %q, want empty", p.Channel)
	}
}

// ---------------------------------------------------------------------------
// httpError & isRetryableSendErr
// ---------------------------------------------------------------------------

// TestHTTPErrorErrorString verifies the Error() output for both the
// status-code and network-error branches.
func TestHTTPErrorErrorString(t *testing.T) {
	withStatus := &httpError{statusCode: 503, err: errors.New("upstream down")}
	if got := withStatus.Error(); got != "slack: status 503: upstream down" {
		t.Errorf("Error(): got %q", got)
	}
	networkErr := &httpError{statusCode: 0, err: errors.New("connection refused")}
	if got := networkErr.Error(); got != "slack: connection refused" {
		t.Errorf("Error(): got %q", got)
	}
}

// TestHTTPErrorUnwrap verifies that Unwrap exposes the inner error.
func TestHTTPErrorUnwrap(t *testing.T) {
	inner := errors.New("boom")
	e := &httpError{statusCode: 500, err: inner}
	if !errors.Is(e, inner) {
		t.Error("errors.Is should find inner error via Unwrap")
	}
}

// TestIsRetryableSendErr verifies the retry predicate classification.
func TestIsRetryableSendErr(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"5xx", &httpError{statusCode: 500, err: errors.New("s")}, true},
		{"503", &httpError{statusCode: 503, err: errors.New("s")}, true},
		{"network", &httpError{statusCode: 0, err: errors.New("s")}, true},
		{"4xx", &httpError{statusCode: 400, err: errors.New("s")}, false},
		{"404", &httpError{statusCode: 404, err: errors.New("s")}, false},
		{"2xx non-ok", &httpError{statusCode: 200, err: errors.New("s")}, false},
		{"plain", errors.New("plain"), false},
	}
	for _, tc := range tests {
		if got := isRetryableSendErr(tc.err); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// isHTTPSURL
// ---------------------------------------------------------------------------

// TestIsHTTPSURL verifies the URL scheme check helper.
func TestIsHTTPSURL(t *testing.T) {
	for _, s := range []string{"https://x", "HTTPS://X", "https://hooks.slack.com/services/T/B/X"} {
		if !isHTTPSURL(s) {
			t.Errorf("isHTTPSURL(%q): got false, want true", s)
		}
	}
	for _, s := range []string{"http://x", "ftp://x", "x", ""} {
		if isHTTPSURL(s) {
			t.Errorf("isHTTPSURL(%q): got true, want false", s)
		}
	}
}

// ---------------------------------------------------------------------------
// applyDefaults
// ---------------------------------------------------------------------------

// TestApplyDefaults verifies that zero/negative fields are replaced.
func TestApplyDefaults(t *testing.T) {
	c := Config{}
	applyDefaults(&c)
	if c.RetryMaxAttempts != defaultRetryMaxAttempts {
		t.Errorf("RetryMaxAttempts: got %d", c.RetryMaxAttempts)
	}
	if c.RetryBaseInterval != defaultRetryBase {
		t.Errorf("RetryBaseInterval: got %v", c.RetryBaseInterval)
	}
	if c.CircuitFailureThreshold != defaultCircuitThreshold {
		t.Errorf("CircuitFailureThreshold: got %d", c.CircuitFailureThreshold)
	}
	if c.CircuitCooldown != defaultCircuitCooldown {
		t.Errorf("CircuitCooldown: got %v", c.CircuitCooldown)
	}

	// Positive values are preserved.
	c2 := Config{
		RetryMaxAttempts:        7,
		RetryBaseInterval:       2 * time.Second,
		CircuitFailureThreshold: 9,
		CircuitCooldown:         3 * time.Second,
	}
	applyDefaults(&c2)
	if c2.RetryMaxAttempts != 7 || c2.RetryBaseInterval != 2*time.Second ||
		c2.CircuitFailureThreshold != 9 || c2.CircuitCooldown != 3*time.Second {
		t.Errorf("positive values not preserved: %+v", c2)
	}
}

// ---------------------------------------------------------------------------
// alert.Channel interface conformance
// ---------------------------------------------------------------------------

// TestImplementsPrismChannel verifies that *Channel satisfies the
// alert.Channel interface at runtime.
func TestImplementsPrismChannel(t *testing.T) {
	var _ alert.Channel = (*Channel)(nil)
	ch, err := New(Config{WebhookURL: "https://example.com"})
	if err != nil {
		t.Fatal(err)
	}
	var asChannel alert.Channel = ch
	if asChannel.Name() != "slack" {
		t.Errorf("Name via interface: got %q", asChannel.Name())
	}
}
