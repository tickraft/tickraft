// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package discord

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

	"github.com/tickraft/tickraft/pkg/prism/channel/httpclient"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// sampleMetricAlert returns a representative metric Event whose value
// (95.5) is below 2x its threshold (90), so the canonical level resolves to
// "warning".
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
					},
				},
			},
		},
	}
}

// sampleCriticalMetricAlert returns a metric Event whose value (200)
// is at least 2x its threshold (90), so the canonical level resolves to
// "critical".
func sampleCriticalMetricAlert() alert.Event {
	a := sampleMetricAlert()
	a.Violations[0].Metric.Value = 200.0
	a.Violations[0].Severity = "critical"
	return a
}

// sampleLogAlert returns a representative log Event with level "error".
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

// mustNew creates a Channel against the given TLS test server, failing the
// test on construction error. The server's TLS client is injected so the
// self-signed certificate is trusted. A default test Formatter is injected
// so formatPayload produces locale-aware content.
func mustNew(t *testing.T, srv *httptest.Server, opts ...Option) *Channel {
	t.Helper()
	allOpts := append([]Option{WithHTTPClient(srv.Client()), WithFormatter(newTestFormatter(t))}, opts...)
	ch, err := New(Config{WebhookURL: srv.URL}, allOpts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return ch
}

// okHandler returns an http.HandlerFunc that writes a Discord-style 204 No
// Content success response.
func okHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}
}

// decodePayload unmarshals a Discord payload JSON body for inspection.
func decodePayload(t *testing.T, body []byte) payload {
	t.Helper()
	var p payload
	if err := json.Unmarshal(body, &p); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	return p
}

// findField returns the embed field with the given name and whether it was
// found.
func findField(fields []field, name string) (field, bool) {
	for _, f := range fields {
		if f.Name == name {
			return f, true
		}
	}
	return field{}, false
}

// ---------------------------------------------------------------------------
// Config.Validate
// ---------------------------------------------------------------------------

// TestValidateEmptyURL verifies that an empty WebhookURL fails validation.
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
	for _, u := range []string{"https://example.com", "https://discord.com/api/webhooks/1/abc"} {
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
// fields are zero. Region defaults to global, which yields the 15s
// httpclient timeout matching defaultTimeout.
func TestNewDefaults(t *testing.T) {
	ch, err := New(Config{WebhookURL: "https://discord.com/api/webhooks/1/abc"})
	if err != nil {
		t.Fatal(err)
	}
	if ch.cfg.Region != httpclient.RegionGlobal {
		t.Errorf("Region: got %q, want %q", ch.cfg.Region, httpclient.RegionGlobal)
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
	if ch.httpClient.Timeout != defaultTimeout {
		t.Errorf("httpClient.Timeout: got %v, want %v", ch.httpClient.Timeout, defaultTimeout)
	}
	if ch.circuit == nil || ch.retry == nil || ch.logger == nil {
		t.Error("circuit, retry, logger should all be non-nil")
	}
}

// TestNewWithOptions verifies that Options override Config fields. A proxy
// URL is configured to exercise the httpclient builder's proxy branch.
func TestNewWithOptions(t *testing.T) {
	ch, err := New(
		Config{},
		WithWebhookURL("https://discord.com/api/webhooks/1/abc"),
		WithUsername("AlertBot"),
		WithAvatarURL("https://cdn.example.com/a.png"),
		WithFrontendBaseURL("https://app.example.com"),
		WithProxyURL("http://proxy:8080"),
		WithRetry(10, 2*time.Second),
		WithCircuitBreaker(3, 15*time.Second),
	)
	if err != nil {
		t.Fatal(err)
	}
	if ch.cfg.WebhookURL != "https://discord.com/api/webhooks/1/abc" {
		t.Errorf("WebhookURL: got %q", ch.cfg.WebhookURL)
	}
	if ch.cfg.Username != "AlertBot" {
		t.Errorf("Username: got %q", ch.cfg.Username)
	}
	if ch.cfg.AvatarURL != "https://cdn.example.com/a.png" {
		t.Errorf("AvatarURL: got %q", ch.cfg.AvatarURL)
	}
	if ch.cfg.FrontendBaseURL != "https://app.example.com" {
		t.Errorf("FrontendBaseURL: got %q", ch.cfg.FrontendBaseURL)
	}
	if ch.cfg.ProxyURL != "http://proxy:8080" {
		t.Errorf("ProxyURL: got %q", ch.cfg.ProxyURL)
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
	if ch.httpClient.Transport == nil {
		t.Error("proxy should produce a custom transport")
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
	ch, err := New(Config{WebhookURL: "https://discord.com/api/webhooks/1/abc"}, WithHTTPClient(custom))
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
	ch, err := New(Config{WebhookURL: "https://discord.com/api/webhooks/1/abc"}, WithHTTPClient(custom))
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
	ch, err := New(Config{WebhookURL: "https://discord.com/api/webhooks/1/abc"}, WithLogger(logger))
	if err != nil {
		t.Fatal(err)
	}
	if ch.logger != logger {
		t.Error("injected logger not used")
	}
}

// TestNewInvalidProxy verifies that an unsupported proxy scheme is rejected
// by the httpclient builder and surfaced by New.
func TestNewInvalidProxy(t *testing.T) {
	if _, err := New(Config{
		WebhookURL: "https://discord.com/api/webhooks/1/abc",
		ProxyURL:   "ftp://proxy:21",
	}); err == nil {
		t.Fatal("expected error for unsupported proxy scheme")
	}
}

// ---------------------------------------------------------------------------
// Name
// ---------------------------------------------------------------------------

// TestName verifies the channel name.
func TestName(t *testing.T) {
	ch, err := New(Config{WebhookURL: "https://discord.com/api/webhooks/1/abc"})
	if err != nil {
		t.Fatal(err)
	}
	if got := ch.Name(); got != "discord" {
		t.Errorf("Name(): got %q, want %q", got, "discord")
	}
}

// ---------------------------------------------------------------------------
// Send: success & embed format
// ---------------------------------------------------------------------------

// TestSendSuccess verifies that a 2xx response completes the send and the
// server receives a Discord-compatible embed payload.
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
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	ch := mustNew(t, srv, WithRetry(3, time.Millisecond))
	evt := sampleMetricAlert()
	evt.Locale = "en-US"
	if err := ch.Send(context.Background(), evt); err != nil {
		t.Fatalf("Send: %v", err)
	}
	p := decodePayload(t, received)
	if len(p.Embeds) != 1 {
		t.Fatalf("embeds: got %d, want 1", len(p.Embeds))
	}
	emb := p.Embeds[0]
	if !strings.Contains(emb.Title, "cpu_usage") {
		t.Errorf("title should contain metric name, got %q", emb.Title)
	}
	if !strings.Contains(emb.Description, "cpu_usage") {
		t.Errorf("description should contain metric name, got %q", emb.Description)
	}
	if emb.Color != colorWarning {
		t.Errorf("color: got %d, want %d (warning)", emb.Color, colorWarning)
	}
	if emb.Timestamp != evt.Timestamp.Format(time.RFC3339) {
		t.Errorf("timestamp: got %q, want %q", emb.Timestamp, evt.Timestamp.Format(time.RFC3339))
	}
	if emb.Footer.Text != footerText {
		t.Errorf("footer: got %q, want %q", emb.Footer.Text, footerText)
	}
	if f, ok := findField(emb.Fields, "Metric"); !ok || f.Value != "cpu_usage" {
		t.Errorf("Metric field missing or wrong: %+v", f)
	}
}

// ---------------------------------------------------------------------------
// Send: retry behavior
// ---------------------------------------------------------------------------

// TestSendRetryThenSuccess verifies that a 5xx on the first attempt is
// retried and the second attempt succeeds.
func TestSendRetryThenSuccess(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) < 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
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
	if se.ChannelName != "discord" {
		t.Errorf("ChannelName: got %q, want discord", se.ChannelName)
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

// TestSend429Retryable verifies that a 429 rate-limit response is retried
// and ultimately fails with a retryable SendError.
func TestSend429Retryable(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	ch := mustNew(t, srv, WithRetry(3, time.Millisecond))
	se := asSendError(t, ch.Send(context.Background(), sampleMetricAlert()))
	if !se.Retryable {
		t.Error("Retryable: got false, want true for 429")
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("server calls: got %d, want 3 (429 retries)", got)
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
// breaker opens and Send returns ErrCircuitOpen without hitting the server.
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

// TestCircuitResetsOnSuccess verifies that an intervening success resets
// the failure counter so the breaker does not open on stale failures.
func TestCircuitResetsOnSuccess(t *testing.T) {
	var fail atomic.Bool
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if fail.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
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

// TestFormatPayloadMetric verifies the embed structure for a metric alert,
// including title, color, footer, timestamp, and metric-specific fields.
func TestFormatPayloadMetric(t *testing.T) {
	ch, err := New(Config{WebhookURL: "https://discord.com/api/webhooks/1/abc"}, WithFormatter(newTestFormatter(t)))
	if err != nil {
		t.Fatal(err)
	}
	evt := sampleMetricAlert()
	evt.Locale = "en-US"
	body, fmtErr := ch.formatPayload(context.Background(), evt)
	if fmtErr != nil {
		t.Fatalf("formatPayload: %v", fmtErr)
	}
	p := decodePayload(t, body)

	if len(p.Embeds) != 1 {
		t.Fatalf("embeds: got %d, want 1", len(p.Embeds))
	}
	emb := p.Embeds[0]
	if !strings.Contains(emb.Title, "cpu_usage") {
		t.Errorf("title should contain metric name: got %q", emb.Title)
	}
	if emb.Color != colorWarning {
		t.Errorf("color: got %d, want %d", emb.Color, colorWarning)
	}
	if emb.Footer.Text != footerText {
		t.Errorf("footer: got %q", emb.Footer.Text)
	}
	if emb.Timestamp != evt.Timestamp.Format(time.RFC3339) {
		t.Errorf("timestamp: got %q", emb.Timestamp)
	}
	f, ok := findField(emb.Fields, "Metric")
	if !ok || f.Value != "cpu_usage" {
		t.Errorf("Metric field missing or wrong: %+v", f)
	}
	f, ok = findField(emb.Fields, "Value")
	if !ok || !strings.Contains(f.Value, "95.5") {
		t.Errorf("Value field missing or wrong: %+v", f)
	}
}

// TestFormatPayloadCriticalColor verifies that a critical metric alert maps
// to the critical (red) embed color.
func TestFormatPayloadCriticalColor(t *testing.T) {
	ch, err := New(Config{WebhookURL: "https://discord.com/api/webhooks/1/abc"}, WithFormatter(newTestFormatter(t)))
	if err != nil {
		t.Fatal(err)
	}
	evt := sampleCriticalMetricAlert()
	evt.Locale = "en-US"
	body, fmtErr := ch.formatPayload(context.Background(), evt)
	if fmtErr != nil {
		t.Fatalf("formatPayload: %v", fmtErr)
	}
	p := decodePayload(t, body)
	if p.Embeds[0].Color != colorCritical {
		t.Errorf("color: got %d, want %d (critical)", p.Embeds[0].Color, colorCritical)
	}
}

// TestFormatPayloadInfoColor verifies that an unknown alert type resolves
// to the info (green) embed color.
func TestFormatPayloadInfoColor(t *testing.T) {
	ch, err := New(Config{WebhookURL: "https://discord.com/api/webhooks/1/abc"}, WithFormatter(newTestFormatter(t)))
	if err != nil {
		t.Fatal(err)
	}
	evt := alert.Event{
		Type:      "custom",
		AssetID:   99,
		TenantID:  1,
		Timestamp: time.Unix(1700000000, 0).UTC(),
		Locale:    "en-US",
	}
	body, fmtErr := ch.formatPayload(context.Background(), evt)
	if fmtErr != nil {
		t.Fatalf("formatPayload: %v", fmtErr)
	}
	p := decodePayload(t, body)
	if p.Embeds[0].Color != colorInfo {
		t.Errorf("color: got %d, want %d (info)", p.Embeds[0].Color, colorInfo)
	}
	if !strings.Contains(p.Embeds[0].Title, "custom") {
		t.Errorf("title should contain alert type: got %q", p.Embeds[0].Title)
	}
}

// TestFormatPayloadLog verifies the embed structure for a log alert,
// including keyword, source IP, and content fields. Log level "error" maps
// to the critical (red) color.
func TestFormatPayloadLog(t *testing.T) {
	ch, err := New(Config{WebhookURL: "https://discord.com/api/webhooks/1/abc"}, WithFormatter(newTestFormatter(t)))
	if err != nil {
		t.Fatal(err)
	}
	evt := sampleLogAlert()
	evt.Locale = "en-US"
	body, fmtErr := ch.formatPayload(context.Background(), evt)
	if fmtErr != nil {
		t.Fatalf("formatPayload: %v", fmtErr)
	}
	p := decodePayload(t, body)
	emb := p.Embeds[0]
	if !strings.Contains(emb.Title, "panic") {
		t.Errorf("title should contain keyword: got %q", emb.Title)
	}
	if emb.Color != colorCritical {
		t.Errorf("color: got %d, want %d (error -> critical)", emb.Color, colorCritical)
	}
	if f, ok := findField(emb.Fields, "Keyword"); !ok || f.Value != "panic" {
		t.Errorf("Keyword field missing or wrong: %+v", f)
	}
	if f, ok := findField(emb.Fields, "Source IP"); !ok || f.Value != "10.0.0.5" {
		t.Errorf("Source IP field missing or wrong: %+v", f)
	}
	if f, ok := findField(emb.Fields, "Content"); !ok || !strings.Contains(f.Value, "nil pointer") {
		t.Errorf("Content field missing or wrong: %+v", f)
	}
}

// TestFormatPayloadLogMinimal verifies that a log alert without source IP
// or content omits those empty fields.
func TestFormatPayloadLogMinimal(t *testing.T) {
	ch, err := New(Config{WebhookURL: "https://discord.com/api/webhooks/1/abc"}, WithFormatter(newTestFormatter(t)))
	if err != nil {
		t.Fatal(err)
	}
	evt := alert.Event{
		Type:     alert.TypeLog,
		AssetID:  1,
		TenantID: 1,
		Timestamp: time.Unix(1700000000,
			0).UTC(),
		Locale: "en-US",
		Violations: []alert.Violation{
			{
				Kind:     alert.ViolationKindLog,
				Severity: "warning",
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
	emb := p.Embeds[0]
	if _, ok := findField(emb.Fields, "Source IP"); ok {
		t.Error("fields should not contain empty Source IP")
	}
	if _, ok := findField(emb.Fields, "Content"); ok {
		t.Error("fields should not contain empty Content")
	}
	if emb.Color != colorWarning {
		t.Errorf("color: got %d, want %d (warning)", emb.Color, colorWarning)
	}
}

// TestFormatPayloadUsernameAvatar verifies that the username and avatar_url
// overrides are set in the payload when configured.
func TestFormatPayloadUsernameAvatar(t *testing.T) {
	ch, err := New(Config{WebhookURL: "https://discord.com/api/webhooks/1/abc"},
		WithUsername("AlertBot"),
		WithAvatarURL("https://cdn.example.com/a.png"),
		WithFormatter(newTestFormatter(t)),
	)
	if err != nil {
		t.Fatal(err)
	}
	body, fmtErr := ch.formatPayload(context.Background(), sampleMetricAlert())
	if fmtErr != nil {
		t.Fatalf("formatPayload: %v", fmtErr)
	}
	p := decodePayload(t, body)
	if p.Username != "AlertBot" {
		t.Errorf("Username: got %q, want AlertBot", p.Username)
	}
	if p.AvatarURL != "https://cdn.example.com/a.png" {
		t.Errorf("AvatarURL: got %q", p.AvatarURL)
	}
}

// TestFormatPayloadNoUsernameAvatar verifies that the username and
// avatar_url fields are omitted when not configured.
func TestFormatPayloadNoUsernameAvatar(t *testing.T) {
	ch, err := New(Config{WebhookURL: "https://discord.com/api/webhooks/1/abc"}, WithFormatter(newTestFormatter(t)))
	if err != nil {
		t.Fatal(err)
	}
	body, fmtErr := ch.formatPayload(context.Background(), sampleMetricAlert())
	if fmtErr != nil {
		t.Fatalf("formatPayload: %v", fmtErr)
	}
	p := decodePayload(t, body)
	if p.Username != "" {
		t.Errorf("Username: got %q, want empty", p.Username)
	}
	if p.AvatarURL != "" {
		t.Errorf("AvatarURL: got %q, want empty", p.AvatarURL)
	}
}

// TestFormatPayloadAssetLink verifies that an asset deep link field is
// included when FrontendBaseURL is configured.
func TestFormatPayloadAssetLink(t *testing.T) {
	ch, err := New(Config{WebhookURL: "https://discord.com/api/webhooks/1/abc"},
		WithFrontendBaseURL("https://app.example.com"),
		WithFormatter(newTestFormatter(t)),
	)
	if err != nil {
		t.Fatal(err)
	}
	body, fmtErr := ch.formatPayload(context.Background(), sampleMetricAlert())
	if fmtErr != nil {
		t.Fatalf("formatPayload: %v", fmtErr)
	}
	p := decodePayload(t, body)
	emb := p.Embeds[0]
	f, ok := findField(emb.Fields, "Asset Link")
	if !ok {
		t.Fatal("fields should contain Asset Link")
	}
	want := "https://app.example.com/resources/42"
	if f.Value != want {
		t.Errorf("Asset Link value: got %q, want %q", f.Value, want)
	}
	if f.Inline {
		t.Error("Asset Link should be non-inline")
	}
}

// TestFormatPayloadNoAssetLink verifies that no asset link field is
// present when FrontendBaseURL is empty.
func TestFormatPayloadNoAssetLink(t *testing.T) {
	ch, err := New(Config{WebhookURL: "https://discord.com/api/webhooks/1/abc"}, WithFormatter(newTestFormatter(t)))
	if err != nil {
		t.Fatal(err)
	}
	evt := sampleMetricAlert()
	evt.Locale = "en-US"
	body, fmtErr := ch.formatPayload(context.Background(), evt)
	if fmtErr != nil {
		t.Fatalf("formatPayload: %v", fmtErr)
	}
	p := decodePayload(t, body)
	if _, ok := findField(p.Embeds[0].Fields, "Asset Link"); ok {
		t.Error("fields should not contain Asset Link when FrontendBaseURL empty")
	}
}

// ---------------------------------------------------------------------------
// httpError & isRetryableSendErr
// ---------------------------------------------------------------------------

// TestHTTPErrorErrorString verifies the Error() output for both the
// status-code and network-error branches.
func TestHTTPErrorErrorString(t *testing.T) {
	withStatus := &httpError{statusCode: 503, err: errors.New("upstream down")}
	if got := withStatus.Error(); got != "discord: status 503: upstream down" {
		t.Errorf("Error(): got %q", got)
	}
	networkErr := &httpError{statusCode: 0, err: errors.New("connection refused")}
	if got := networkErr.Error(); got != "discord: connection refused" {
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

// TestIsRetryableSendErr verifies the retry predicate classification,
// including the 429 rate-limit special case.
func TestIsRetryableSendErr(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"5xx", &httpError{statusCode: 500, err: errors.New("s")}, true},
		{"503", &httpError{statusCode: 503, err: errors.New("s")}, true},
		{"429", &httpError{statusCode: 429, err: errors.New("s")}, true},
		{"network", &httpError{statusCode: 0, err: errors.New("s")}, true},
		{"4xx", &httpError{statusCode: 400, err: errors.New("s")}, false},
		{"404", &httpError{statusCode: 404, err: errors.New("s")}, false},
		{"2xx", &httpError{statusCode: 204, err: errors.New("s")}, false},
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
	for _, s := range []string{"https://x", "HTTPS://X", "https://discord.com/api/webhooks/1/abc"} {
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

// TestApplyDefaults verifies that zero/negative fields are replaced and
// positive values are preserved.
func TestApplyDefaults(t *testing.T) {
	c := Config{}
	applyDefaults(&c)
	if c.Region != httpclient.RegionGlobal {
		t.Errorf("Region: got %q, want %q", c.Region, httpclient.RegionGlobal)
	}
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
		Region:                  httpclient.RegionCN,
		RetryMaxAttempts:        7,
		RetryBaseInterval:       2 * time.Second,
		CircuitFailureThreshold: 9,
		CircuitCooldown:         3 * time.Second,
	}
	applyDefaults(&c2)
	if c2.Region != httpclient.RegionCN || c2.RetryMaxAttempts != 7 || c2.RetryBaseInterval != 2*time.Second ||
		c2.CircuitFailureThreshold != 9 || c2.CircuitCooldown != 3*time.Second {
		t.Errorf("positive values not preserved: %+v", c2)
	}
}

// ---------------------------------------------------------------------------
// redactWebhookURL
// ---------------------------------------------------------------------------

// TestRedactWebhookURL verifies that the secret token segment is masked,
// the id is retained, and degenerate inputs are reduced safely.
func TestRedactWebhookURL(t *testing.T) {
	got := redactWebhookURL("https://discord.com/api/webhooks/123456/SECRET_TOKEN")
	if strings.Contains(got, "SECRET_TOKEN") {
		t.Errorf("redacted URL leaks token: %q", got)
	}
	if !strings.Contains(got, "123456") {
		t.Errorf("redacted URL should keep the id: %q", got)
	}
	if !strings.HasSuffix(got, "***") {
		t.Errorf("redacted URL should end with ***: %q", got)
	}

	// Single-segment path: reduced to scheme://host.
	got = redactWebhookURL("https://discord.com/single")
	if got != "https://discord.com" {
		t.Errorf("single-segment: got %q, want https://discord.com", got)
	}

	// Unparseable URL.
	got = redactWebhookURL("https://[::1")
	if got != "discord:unparseable-webhook-url" {
		t.Errorf("unparseable: got %q", got)
	}
}

// TestRedactWebhookURLEndToEnd verifies that a failed send surfaces a
// redacted URL (never the raw token) in the returned error.
func TestRedactWebhookURLEndToEnd(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("bad webhook"))
	}))
	defer srv.Close()

	// Build a channel against a token-bearing URL by injecting the test
	// server's TLS client and overriding the webhook URL with a token-like
	// path appended to the server address.
	tokenURL := srv.URL + "/api/webhooks/99999/SUPER_SECRET_TOKEN"
	ch, err := New(Config{WebhookURL: tokenURL}, WithHTTPClient(srv.Client()), WithRetry(1, time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	se := asSendError(t, ch.Send(context.Background(), sampleMetricAlert()))
	if strings.Contains(se.Error(), "SUPER_SECRET_TOKEN") {
		t.Errorf("error leaks token: %v", se)
	}
	if !strings.Contains(se.Error(), "***") {
		t.Errorf("error should contain redacted marker: %v", se)
	}
}

// ---------------------------------------------------------------------------
// levelColor
// ---------------------------------------------------------------------------

// TestLevelColor verifies the level-to-color mapping including aliases and
// the default fallback.
func TestLevelColor(t *testing.T) {
	tests := []struct {
		level string
		want  int
	}{
		{"critical", colorCritical},
		{"CRITICAL", colorCritical},
		{"fatal", colorCritical},
		{"error", colorCritical},
		{"severe", colorCritical},
		{"emergency", colorCritical},
		{"warning", colorWarning},
		{"warn", colorWarning},
		{"info", colorInfo},
		{"information", colorInfo},
		{"notice", colorInfo},
		{"unknown", colorInfo},
		{"", colorInfo},
	}
	for _, tc := range tests {
		if got := levelColor(tc.level); got != tc.want {
			t.Errorf("levelColor(%q): got %d, want %d", tc.level, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// alert.Channel interface conformance
// ---------------------------------------------------------------------------

// TestImplementsPrismChannel verifies that *Channel satisfies the
// alert.Channel interface at runtime.
func TestImplementsPrismChannel(t *testing.T) {
	var _ alert.Channel = (*Channel)(nil)
	ch, err := New(Config{WebhookURL: "https://discord.com/api/webhooks/1/abc"})
	if err != nil {
		t.Fatal(err)
	}
	var asChannel alert.Channel = ch
	if asChannel.Name() != "discord" {
		t.Errorf("Name via interface: got %q", asChannel.Name())
	}
}
