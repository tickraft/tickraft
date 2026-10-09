// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package teams

import (
	"context"
	"crypto/tls"
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
	"github.com/tickraft/tickraft/pkg/prism/alert/template"
	"github.com/tickraft/tickraft/pkg/prism/channel"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// sampleMetricAlert returns a representative metric Event used across
// tests.
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
					Metrics:   map[string]float64{"mem_usage": 80.0},
				},
			},
		},
	}
}

// sampleLogAlert returns a representative log Event used across tests.
func sampleLogAlert() alert.Event {
	return alert.Event{
		Type:     alert.TypeLog,
		AssetID:  9,
		TenantID: 3,
		Timestamp: time.Unix(1700000001,
			0).UTC(),
		Violations: []alert.Violation{
			{
				Kind:     alert.ViolationKindLog,
				Severity: "error",
				Source:   "10.0.0.1",
				Log: &alert.LogContext{
					Keyword: "OOM",
					Content: "process killed: out of memory",
				},
			},
		},
	}
}

// newTestClient returns an HTTP client that skips TLS verification, suitable
// for use with httptest.NewTLSServer.
//
// The bare &http.Client{} here is a test fixture for TLS-protected test
// servers; production code should use httpx.NewPoolClient for connection
// pooling.
func newTestClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
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

// asMessageCard decodes the JSON body received by a test server into a
// messageCard, failing the test on decode error.
func asMessageCard(t *testing.T, body []byte) messageCard {
	t.Helper()
	var card messageCard
	if err := json.Unmarshal(body, &card); err != nil {
		t.Fatalf("unmarshal message card: %v", err)
	}
	return card
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

// ---------------------------------------------------------------------------
// Config.Validate
// ---------------------------------------------------------------------------

// TestValidateEmptyWebhookURL verifies that an empty WebhookURL fails
// validation.
func TestValidateEmptyWebhookURL(t *testing.T) {
	if err := (Config{}).Validate(); err == nil {
		t.Fatal("expected error for empty WebhookURL")
	}
}

// TestValidateNonHTTPSURL verifies that a non-https URL fails validation.
func TestValidateNonHTTPSURL(t *testing.T) {
	for _, url := range []string{"ftp://example.com", "http://example.com", "example.com", "mailto:x@y"} {
		if err := (Config{WebhookURL: url}).Validate(); err == nil {
			t.Errorf("expected error for URL %q", url)
		}
	}
}

// TestValidateValidHTTPSURL verifies that https URLs pass validation.
func TestValidateValidHTTPSURL(t *testing.T) {
	for _, url := range []string{
		"https://example.com", "HTTPS://EXAMPLE.COM/hook", "https://outlook.office.com/webhook/xxx",
	} {
		if err := (Config{WebhookURL: url}).Validate(); err != nil {
			t.Errorf("unexpected error for URL %q: %v", url, err)
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
		WithRetry(10, 2*time.Second),
		WithCircuitBreaker(3, 15*time.Second),
	)
	if err != nil {
		t.Fatal(err)
	}
	if ch.cfg.WebhookURL != "https://example.com" {
		t.Errorf("WebhookURL: got %q", ch.cfg.WebhookURL)
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

// TestNewWithHTTPClient verifies that an injected client is used as-is
// when it already has a timeout.
//
// The bare &http.Client{} instances below are test fixtures used to verify
// the WithHTTPClient injection contract; production code should use
// httpx.NewPoolClient for connection pooling.
func TestNewWithHTTPClient(t *testing.T) {
	custom := &http.Client{Timeout: 7 * time.Second}
	ch, err := New(Config{WebhookURL: "https://example.com"}, WithHTTPClient(custom))
	if err != nil {
		t.Fatal(err)
	}
	if ch.httpClient != custom {
		t.Error("injected client not used")
	}
	if ch.httpClient.Timeout != 7*time.Second {
		t.Errorf("httpClient.Timeout: got %v, want 7s", ch.httpClient.Timeout)
	}
}

// TestNewHTTPClientTimeoutSet verifies that an injected client without a
// timeout gets the default timeout.
func TestNewHTTPClientTimeoutSet(t *testing.T) {
	custom := &http.Client{}
	ch, err := New(
		Config{WebhookURL: "https://example.com"},
		WithHTTPClient(custom),
	)
	if err != nil {
		t.Fatal(err)
	}
	if ch.httpClient.Timeout != defaultTimeout {
		t.Errorf("httpClient.Timeout: got %v, want %v", ch.httpClient.Timeout, defaultTimeout)
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
	if got := ch.Name(); got != "teams" {
		t.Errorf("Name(): got %q, want %q", got, "teams")
	}
}

// ---------------------------------------------------------------------------
// Send: success & retry
// ---------------------------------------------------------------------------

// TestSendSuccess verifies a 2xx response completes the send and the
// server receives a valid MessageCard JSON payload.
func TestSendSuccess(t *testing.T) {
	var received messageCard
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type: got %q, want application/json", ct)
		}
		body, readErr := io.ReadAll(r.Body)
		if readErr != nil {
			t.Errorf("read body: %v", readErr)
		}
		received = asMessageCard(t, body)
		_, _ = w.Write([]byte("1"))
	}))
	defer srv.Close()

	ch, err := New(Config{WebhookURL: srv.URL},
		WithHTTPClient(newTestClient()),
		WithRetry(3, time.Millisecond),
		WithFormatter(newTestFormatter(t)),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := ch.Send(context.Background(), sampleMetricAlert()); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if received.Type != "MessageCard" {
		t.Errorf("Type: got %q, want MessageCard", received.Type)
	}
	if received.Context != "http://schema.org/extensions" {
		t.Errorf("Context: got %q", received.Context)
	}
	if received.Summary == "" {
		t.Error("Summary should be non-empty (required by Teams)")
	}
	if received.Title == "" {
		t.Error("Title should be non-empty")
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
		_, _ = w.Write([]byte("1"))
	}))
	defer srv.Close()

	ch, err := New(Config{WebhookURL: srv.URL},
		WithHTTPClient(newTestClient()),
		WithRetry(3, time.Millisecond),
	)
	if err != nil {
		t.Fatal(err)
	}
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

	ch, err := New(Config{WebhookURL: srv.URL},
		WithHTTPClient(newTestClient()),
		WithRetry(3, time.Millisecond),
	)
	if err != nil {
		t.Fatal(err)
	}
	se := asSendError(t, ch.Send(context.Background(), sampleMetricAlert()))
	if !se.Retryable {
		t.Error("Retryable: got false, want true")
	}
	if se.ChannelName != "teams" {
		t.Errorf("ChannelName: got %q, want %q", se.ChannelName, "teams")
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

	ch, err := New(Config{WebhookURL: srv.URL},
		WithHTTPClient(newTestClient()),
		WithRetry(3, time.Millisecond),
	)
	if err != nil {
		t.Fatal(err)
	}
	se := asSendError(t, ch.Send(context.Background(), sampleMetricAlert()))
	if se.Retryable {
		t.Error("Retryable: got true, want false")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("server calls: got %d, want 1 (no retry)", got)
	}
}

// TestSendTeamsValidationError verifies that a 2xx response whose body
// contains the Teams validation marker is treated as a non-retryable
// error.
func TestSendTeamsValidationError(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte("Summary or Text is required."))
	}))
	defer srv.Close()

	ch, err := New(Config{WebhookURL: srv.URL},
		WithHTTPClient(newTestClient()),
		WithRetry(3, time.Millisecond),
	)
	if err != nil {
		t.Fatal(err)
	}
	se := asSendError(t, ch.Send(context.Background(), sampleMetricAlert()))
	if se.Retryable {
		t.Error("Retryable: got true, want false for validation error")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("server calls: got %d, want 1 (no retry)", got)
	}
}

// TestSendNetworkError verifies that a network error is retryable and
// ultimately fails with a retryable SendError.
func TestSendNetworkError(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("1"))
	}))
	addr := srv.URL
	srv.Close() // close the listener so connections are refused

	ch, err := New(Config{WebhookURL: addr},
		WithHTTPClient(newTestClient()),
		WithRetry(2, time.Millisecond),
	)
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

// TestSendEmptyWebhookURL verifies that Send returns an error when the
// channel was constructed without a WebhookURL (defensive guard for direct
// construction).
func TestSendEmptyWebhookURL(t *testing.T) {
	ch := &Channel{}
	se := asSendError(t, ch.Send(context.Background(), sampleMetricAlert()))
	if se.Retryable {
		t.Error("Retryable: got true, want false")
	}
}

// ---------------------------------------------------------------------------
// MessageCard construction
// ---------------------------------------------------------------------------

// TestFormatMessageCardMetric verifies the MessageCard built from a metric
// alert: type, context, themeColor, title, summary, text, and sections.
// A test Formatter is injected so format.Render produces locale-aware output
// with a derived level.
func TestFormatMessageCardMetric(t *testing.T) {
	ch := &Channel{formatter: newTestFormatter(t)}
	evt := sampleMetricAlert()
	evt.Locale = "en-US"
	body, err := ch.formatMessageCard(context.Background(), evt)
	if err != nil {
		t.Fatalf("formatMessageCard: %v", err)
	}
	card := asMessageCard(t, body)

	if card.Type != "MessageCard" {
		t.Errorf("Type: got %q, want MessageCard", card.Type)
	}
	if card.Context != "http://schema.org/extensions" {
		t.Errorf("Context: got %q", card.Context)
	}
	// The Formatter infers "warning" for 95.5 < 90*2 → gold.
	if card.ThemeColor != colorWarning {
		t.Errorf("ThemeColor: got %q, want %q", card.ThemeColor, colorWarning)
	}
	if !strings.Contains(card.Title, "cpu_usage") {
		t.Errorf("Title should contain metric name, got %q", card.Title)
	}
	if !strings.Contains(card.Text, "95.50") {
		t.Errorf("Text should contain metric value, got %q", card.Text)
	}
	if len(card.Sections) != 1 {
		t.Fatalf("Sections: got %d, want 1", len(card.Sections))
	}
	facts := card.Sections[0].Facts
	if len(facts) < 5 {
		t.Errorf("Facts: got %d, want at least 5", len(facts))
	}
	// Verify a metric-specific fact is present (en-US label for field.metric_name).
	found := false
	for _, f := range facts {
		if f.Name == "Metric" && f.Value == "cpu_usage" {
			found = true
		}
	}
	if !found {
		t.Error("metric fact Metric not found")
	}
}

// TestFormatMessageCardLog verifies the MessageCard built from a log alert.
func TestFormatMessageCardLog(t *testing.T) {
	ch := &Channel{formatter: newTestFormatter(t)}
	evt := sampleLogAlert()
	evt.Locale = "en-US"
	body, err := ch.formatMessageCard(context.Background(), evt)
	if err != nil {
		t.Fatalf("formatMessageCard: %v", err)
	}
	card := asMessageCard(t, body)

	// error level → dark orange.
	if card.ThemeColor != colorError {
		t.Errorf("ThemeColor: got %q, want %q", card.ThemeColor, colorError)
	}
	if !strings.Contains(card.Title, "OOM") {
		t.Errorf("Title should contain keyword, got %q", card.Title)
	}
	if !strings.Contains(card.Text, "process killed") {
		t.Errorf("Text should contain log content, got %q", card.Text)
	}
	if len(card.Sections) != 1 {
		t.Fatalf("Sections: got %d, want 1", len(card.Sections))
	}
	// Verify a log-specific fact is present (en-US label for field.keyword).
	found := false
	for _, f := range card.Sections[0].Facts {
		if f.Name == "Keyword" && f.Value == "OOM" {
			found = true
		}
	}
	if !found {
		t.Error("log fact Keyword not found")
	}
}

// TestFormatMessageCardUnknownType verifies the fallback branch for an
// unknown alert type.
func TestFormatMessageCardUnknownType(t *testing.T) {
	ch := &Channel{formatter: newTestFormatter(t)}
	evt := alert.Event{
		Type:      "custom",
		AssetID:   1,
		TenantID:  1,
		Timestamp: time.Unix(1700000000, 0).UTC(),
		Locale:    "en-US",
	}
	body, err := ch.formatMessageCard(context.Background(), evt)
	if err != nil {
		t.Fatalf("formatMessageCard: %v", err)
	}
	card := asMessageCard(t, body)
	if !strings.Contains(card.Title, "custom") {
		t.Errorf("Title should contain type, got %q", card.Title)
	}
	if !strings.Contains(card.Text, "custom") {
		t.Errorf("Text should contain type, got %q", card.Text)
	}
}

// TestThemeColorForLevel verifies the level to color mapping.
func TestThemeColorForLevel(t *testing.T) {
	tests := []struct {
		level string
		want  string
	}{
		{"critical", colorCritical},
		{"FATAL", colorCritical},
		{"emergency", colorCritical},
		{"severe", colorCritical},
		{"error", colorError},
		{"ERR", colorError},
		{"warning", colorWarning},
		{"Warn", colorWarning},
		{"info", colorInfo},
		{"information", colorInfo},
		{"notice", colorInfo},
		{"debug", colorDebug},
		{"unknown", colorDefault},
		{"", colorDefault},
		{"  Critical  ", colorCritical},
	}
	for _, tc := range tests {
		if got := themeColorForLevel(tc.level); got != tc.want {
			t.Errorf("themeColorForLevel(%q): got %q, want %q", tc.level, got, tc.want)
		}
	}
}

// TestAlertLevel verifies the derived alert level for color mapping.
func TestAlertLevel(t *testing.T) {
	// Explicit level takes precedence.
	if got := alertLevel(alert.Event{
		Violations: []alert.Violation{
			{
				Kind:     alert.ViolationKindMetric,
				Severity: "critical",
			},
		},
	}); got != "critical" {
		t.Errorf("alertLevel with explicit level: got %q", got)
	}
	// Metric alert without level defaults to warning.
	if got := alertLevel(alert.Event{Type: alert.TypeMetric}); got != "warning" {
		t.Errorf("alertLevel metric default: got %q, want warning", got)
	}
	// Non-metric alert without level defaults to info.
	if got := alertLevel(alert.Event{Type: alert.TypeLog}); got != "info" {
		t.Errorf("alertLevel log default: got %q, want info", got)
	}
}

// TestNew_WithI18nOptions verifies that the i18n-related options are wired
// into the Channel. The injected Formatter, Library, and Registry must be
// reachable on the constructed Channel so that formatMessageCard can dispatch
// through the three-tier rendering pipeline.
func TestNew_WithI18nOptions(t *testing.T) {
	registry := i18n.NewRegistry(zap.NewNop())
	loader := i18n.NewLoader(zap.NewNop())
	if err := loader.LoadToRegistry(i18n.EmbeddedFS(), registry); err != nil {
		t.Fatalf("load builtin resources: %v", err)
	}
	formatter := i18n.NewDefaultFormatter(registry, zap.NewNop())
	lib := template.NewBuiltinLibrary(zap.NewNop())

	ch, err := New(Config{WebhookURL: "https://example.com/webhook"},
		WithFrontendBaseURL("https://front.example.com"),
		WithFormatter(formatter),
		WithLibrary(lib),
		WithRegistry(registry),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if ch.formatter == nil {
		t.Error("formatter was not injected")
	}
	if ch.library == nil {
		t.Error("library was not injected")
	}
	if ch.registry == nil {
		t.Error("registry was not injected")
	}
	if ch.cfg.FrontendBaseURL != "https://front.example.com" {
		t.Errorf("FrontendBaseURL: got %q, want %q",
			ch.cfg.FrontendBaseURL, "https://front.example.com")
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

	ch, err := New(Config{WebhookURL: srv.URL},
		WithHTTPClient(newTestClient()),
		WithRetry(1, time.Millisecond),
		WithCircuitBreaker(2, time.Hour),
	)
	if err != nil {
		t.Fatal(err)
	}

	// Two failed sends open the breaker (threshold 2).
	for i := range 2 {
		if err := ch.Send(context.Background(), sampleMetricAlert()); err == nil {
			t.Fatalf("send #%d: expected error", i+1)
		}
	}
	callsAtOpen := calls.Load()

	// Third send is short-circuited by the open breaker.
	err = ch.Send(context.Background(), sampleMetricAlert())
	if !errors.Is(err, channel.ErrCircuitOpen) {
		t.Fatalf("expected ErrCircuitOpen, got %v", err)
	}
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
		_, _ = w.Write([]byte("1"))
	}))
	defer srv.Close()

	ch, err := New(Config{WebhookURL: srv.URL},
		WithHTTPClient(newTestClient()),
		WithRetry(1, time.Millisecond),
		WithCircuitBreaker(2, 50*time.Millisecond),
	)
	if err != nil {
		t.Fatal(err)
	}

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
		_, _ = w.Write([]byte("1"))
	}))
	defer srv.Close()

	ch, err := New(Config{WebhookURL: srv.URL},
		WithHTTPClient(newTestClient()),
		WithRetry(1, time.Millisecond),
		WithCircuitBreaker(3, time.Hour),
	)
	if err != nil {
		t.Fatal(err)
	}

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

	// Next send should be admitted (breaker still closed), not ErrCircuitOpen.
	err = ch.Send(context.Background(), sampleMetricAlert())
	if errors.Is(err, channel.ErrCircuitOpen) {
		t.Fatal("breaker should still be closed after reset + 2 failures")
	}
}

// ---------------------------------------------------------------------------
// httpError & isRetryableSendErr & isTeamsErrorBody
// ---------------------------------------------------------------------------

// TestHTTPErrorErrorString verifies the Error() output for both the
// status-code and network-error branches.
func TestHTTPErrorErrorString(t *testing.T) {
	withStatus := &httpError{statusCode: 503, err: errors.New("upstream down")}
	if got := withStatus.Error(); got != "teams: status 503: upstream down" {
		t.Errorf("Error(): got %q", got)
	}
	networkErr := &httpError{statusCode: 0, err: errors.New("connection refused")}
	if got := networkErr.Error(); got != "teams: connection refused" {
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
		{"2xx-validation", &httpError{statusCode: 200, err: errors.New("Summary required")}, false},
		{"plain", errors.New("plain"), false},
		{"nil", nil, false},
	}
	for _, tc := range tests {
		if got := isRetryableSendErr(tc.err); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestIsTeamsErrorBody verifies the Teams error body detection.
func TestIsTeamsErrorBody(t *testing.T) {
	tests := []struct {
		body string
		want bool
	}{
		{"Summary or Text is required.", true},
		{"  Summary  ", true},
		{"1", false},
		{"", false},
		{"success", false},
	}
	for _, tc := range tests {
		if got := isTeamsErrorBody([]byte(tc.body)); got != tc.want {
			t.Errorf("isTeamsErrorBody(%q): got %v, want %v", tc.body, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// isHTTPSURL
// ---------------------------------------------------------------------------

// TestIsHTTPSURL verifies the URL scheme check helper.
func TestIsHTTPSURL(t *testing.T) {
	for _, s := range []string{"https://x", "HTTPS://X", "https://example.com/hook"} {
		if !isHTTPSURL(s) {
			t.Errorf("isHTTPSURL(%q): got false, want true", s)
		}
	}
	for _, s := range []string{"http://x", "ftp://x", "x", "", "://no-scheme"} {
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
	if asChannel.Name() != "teams" {
		t.Errorf("Name via interface: got %q", asChannel.Name())
	}
}
