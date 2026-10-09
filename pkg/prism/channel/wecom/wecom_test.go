// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package wecom

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/i18n"
	"github.com/tickraft/tickraft/pkg/prism/alert"
	"github.com/tickraft/tickraft/pkg/prism/channel"
	"github.com/tickraft/tickraft/pkg/prism/channel/format"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// sampleMetricAlert returns a representative metric Event.
func sampleMetricAlert() alert.Event {
	return alert.Event{
		Type:     alert.TypeMetric,
		AssetID:  42,
		TenantID: 7,
		Timestamp: time.Unix(1700000000,
			0).UTC(),
		Violations: []alert.Violation{
			{
				Kind:     alert.ViolationKindMetric,
				Severity: "critical",
				Metric: &alert.MetricContext{
					Name:      "cpu_usage",
					Value:     95.5,
					Threshold: 90.0,
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

// newRobotChannel builds a robot-mode Channel pointing at srv. The server
// must be a TLSServer (created via httptest.NewTLSServer) since robot mode
// requires an https URL; srv.Client() is injected so the channel trusts the
// self-signed test certificate. A default test Formatter is injected so
// buildRobotPayload produces locale-aware content.
func newRobotChannel(t *testing.T, srv *httptest.Server, opts ...Option) *Channel {
	t.Helper()
	ch, err := New(
		Config{Mode: ModeRobot, RobotWebhookURL: srv.URL},
		append([]Option{WithHTTPClient(srv.Client()), WithFormatter(newTestFormatter(t))}, opts...)...,
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return ch
}

// newAppChannel builds an app-mode Channel pointing at srv. srv.Client() is
// injected so the channel trusts the self-signed test certificate when srv
// is a TLSServer. A default test Formatter is injected so buildAppPayload
// produces locale-aware content.
func newAppChannel(t *testing.T, srv *httptest.Server, opts ...Option) *Channel {
	t.Helper()
	ch, err := New(
		Config{
			Mode:    ModeApp,
			CorpID:  "corp-1",
			AgentID: 1000002,
			Secret:  "secret-1",
			ToUser:  "@all",
		},
		append([]Option{
			withBaseURL(srv.URL), WithHTTPClient(srv.Client()), WithFormatter(newTestFormatter(t)),
		}, opts...)...,
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return ch
}

// ---------------------------------------------------------------------------
// Config.Validate
// ---------------------------------------------------------------------------

// TestValidateRobotEmptyURL verifies that robot mode rejects an empty URL.
func TestValidateRobotEmptyURL(t *testing.T) {
	if err := (Config{Mode: ModeRobot}).Validate(); err == nil {
		t.Fatal("expected error for empty robot URL")
	}
}

// TestValidateRobotNonHTTPS verifies that robot mode rejects a non-https URL.
func TestValidateRobotNonHTTPS(t *testing.T) {
	for _, url := range []string{"http://example.com", "ftp://example.com", "example.com"} {
		if err := (Config{Mode: ModeRobot, RobotWebhookURL: url}).Validate(); err == nil {
			t.Errorf("expected error for URL %q", url)
		}
	}
}

// TestValidateRobotValid verifies that robot mode accepts an https URL.
func TestValidateRobotValid(t *testing.T) {
	if err := (Config{Mode: ModeRobot, RobotWebhookURL: "https://example.com/hook"}).Validate(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestValidateAppMissingFields verifies that app mode rejects missing fields.
func TestValidateAppMissingFields(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
	}{
		{"missing corp_id", Config{Mode: ModeApp, AgentID: 1, Secret: "s", ToUser: "u"}},
		{"missing agent_id", Config{Mode: ModeApp, CorpID: "c", Secret: "s", ToUser: "u"}},
		{"missing secret", Config{Mode: ModeApp, CorpID: "c", AgentID: 1, ToUser: "u"}},
		{"missing to_user", Config{Mode: ModeApp, CorpID: "c", AgentID: 1, Secret: "s"}},
	}
	for _, tc := range tests {
		if err := tc.cfg.Validate(); err == nil {
			t.Errorf("%s: expected error", tc.name)
		}
	}
}

// TestValidateAppValid verifies that app mode accepts a complete config.
func TestValidateAppValid(t *testing.T) {
	cfg := Config{Mode: ModeApp, CorpID: "c", AgentID: 1, Secret: "s", ToUser: "u"}
	if err := cfg.Validate(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestValidateBaseURLScheme verifies the stored base_url accepts http (a
// dedicated-edition private endpoint may be plain http inside the intranet)
// and https but rejects other schemes, in both modes.
func TestValidateBaseURLScheme(t *testing.T) {
	for _, base := range []Config{
		{Mode: ModeApp, CorpID: "c", AgentID: 1, Secret: "s", ToUser: "u"},
		{Mode: ModeRobot, RobotWebhookURL: "https://example.com/hook"},
	} {
		for _, u := range []string{"http://qyapi.intranet.example", "https://qyapi.intranet.example"} {
			cfg := base
			cfg.BaseURL = u
			if err := cfg.Validate(); err != nil {
				t.Errorf("base url %q: unexpected error %v", u, err)
			}
		}
		for _, u := range []string{"ftp://example.com", "example.com"} {
			cfg := base
			cfg.BaseURL = u
			if err := cfg.Validate(); err == nil {
				t.Errorf("base url %q: expected error", u)
			}
		}
	}
}

// TestValidateInvalidMode verifies that an unknown mode is rejected.
func TestValidateInvalidMode(t *testing.T) {
	if err := (Config{Mode: "bogus"}).Validate(); err == nil {
		t.Fatal("expected error for invalid mode")
	}
}

// ---------------------------------------------------------------------------
// isHTTPSURL
// ---------------------------------------------------------------------------

// TestIsHTTPSURL verifies the https scheme check.
func TestIsHTTPSURL(t *testing.T) {
	for _, s := range []string{"https://x", "HTTPS://X"} {
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
// New: construction & defaults
// ---------------------------------------------------------------------------

// TestNewInvalidConfig verifies that New rejects an invalid Config.
func TestNewInvalidConfig(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("expected error for empty Config")
	}
	if _, err := New(Config{Mode: ModeRobot, RobotWebhookURL: "http://x"}); err == nil {
		t.Fatal("expected error for non-https robot URL")
	}
}

// TestNewDefaults verifies that New applies default values.
func TestNewDefaults(t *testing.T) {
	ch, err := New(Config{Mode: ModeRobot, RobotWebhookURL: "https://example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if ch.cfg.MessageType != MessageTypeText {
		t.Errorf("MessageType: got %q, want %q", ch.cfg.MessageType, MessageTypeText)
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
	if ch.httpClient == nil || ch.retry == nil || ch.circuit == nil || ch.logger == nil {
		t.Error("httpClient, retry, circuit, logger should all be non-nil")
	}
	if ch.baseURL != defaultBaseURL {
		t.Errorf("baseURL: got %q, want %q", ch.baseURL, defaultBaseURL)
	}
}

// TestNewWithOptions verifies that Options override Config fields.
func TestNewWithOptions(t *testing.T) {
	ch, err := New(
		Config{},
		WithRobotMode(),
		WithRobotWebhookURL("https://example.com"),
		WithMessageType(MessageTypeMarkdown),
		WithRetry(10, 2*time.Second),
		WithCircuitBreaker(3, 15*time.Second),
	)
	if err != nil {
		t.Fatal(err)
	}
	if ch.cfg.Mode != ModeRobot {
		t.Errorf("Mode: got %q", ch.cfg.Mode)
	}
	if ch.cfg.RobotWebhookURL != "https://example.com" {
		t.Errorf("RobotWebhookURL: got %q", ch.cfg.RobotWebhookURL)
	}
	if ch.cfg.MessageType != MessageTypeMarkdown {
		t.Errorf("MessageType: got %q", ch.cfg.MessageType)
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

// TestNewAppModeWithOptions verifies app mode options.
func TestNewAppModeWithOptions(t *testing.T) {
	ch, err := New(
		Config{},
		WithAppMode(),
		WithCorpID("corp-1"),
		WithAgentID(1000002),
		WithSecret("secret-1"),
		WithToUser("@all"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if ch.cfg.Mode != ModeApp {
		t.Errorf("Mode: got %q", ch.cfg.Mode)
	}
	if ch.cfg.CorpID != "corp-1" || ch.cfg.AgentID != 1000002 ||
		ch.cfg.Secret != "secret-1" || ch.cfg.ToUser != "@all" {
		t.Errorf("app config mismatch: %+v", ch.cfg)
	}
}

// TestNewWithHTTPClient verifies that an injected client is used.
//
// The bare &http.Client{} below is a test fixture used to verify the
// WithHTTPClient injection contract; production code should use
// httpx.NewPoolClient for connection pooling.
func TestNewWithHTTPClient(t *testing.T) {
	custom := &http.Client{Timeout: 7 * time.Second}
	ch, err := New(Config{Mode: ModeRobot, RobotWebhookURL: "https://example.com"}, WithHTTPClient(custom))
	if err != nil {
		t.Fatal(err)
	}
	if ch.httpClient != custom {
		t.Error("injected client not used")
	}
}

// TestNewWithLogger verifies that an injected logger is used.
func TestNewWithLogger(t *testing.T) {
	logger := zap.NewExample()
	ch, err := New(Config{Mode: ModeRobot, RobotWebhookURL: "https://example.com"}, WithLogger(logger))
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
	ch, err := New(Config{Mode: ModeRobot, RobotWebhookURL: "https://example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if got := ch.Name(); got != "wecom" {
		t.Errorf("Name(): got %q, want %q", got, "wecom")
	}
}

// ---------------------------------------------------------------------------
// Robot mode: Send
// ---------------------------------------------------------------------------

// robotRequestBody captures the JSON posted to the robot webhook.
type robotRequestBody struct {
	MsgType string `json:"msgtype"`
	Text    struct {
		Content string `json:"content"`
	} `json:"text"`
	Markdown struct {
		Content string `json:"content"`
	} `json:"markdown"`
}

// TestSendRobotTextSuccess verifies a successful robot text send and that
// the server receives a text payload.
func TestSendRobotTextSuccess(t *testing.T) {
	var got robotRequestBody
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}))
	defer srv.Close()

	ch := newRobotChannel(t, srv, WithRetry(3, time.Millisecond))
	if err := ch.Send(context.Background(), sampleMetricAlert()); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got.MsgType != "text" {
		t.Errorf("MsgType: got %q, want text", got.MsgType)
	}
	if got.Text.Content == "" {
		t.Error("text content should be non-empty")
	}
}

// TestSendRobotMarkdownSuccess verifies a successful robot markdown send.
func TestSendRobotMarkdownSuccess(t *testing.T) {
	var got robotRequestBody
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}))
	defer srv.Close()

	ch := newRobotChannel(t, srv, WithMessageType(MessageTypeMarkdown), WithRetry(3, time.Millisecond))
	if err := ch.Send(context.Background(), sampleMetricAlert()); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got.MsgType != "markdown" {
		t.Errorf("MsgType: got %q, want markdown", got.MsgType)
	}
	if got.Markdown.Content == "" {
		t.Error("markdown content should be non-empty")
	}
}

// TestSendRobotRetryThenSuccess verifies that a 5xx is retried and the
// second attempt succeeds.
func TestSendRobotRetryThenSuccess(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) < 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}))
	defer srv.Close()

	ch := newRobotChannel(t, srv, WithRetry(3, time.Millisecond))
	if err := ch.Send(context.Background(), sampleMetricAlert()); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("server calls: got %d, want 2", got)
	}
}

// TestSendRobotRetryExhausted verifies that repeated 5xx exhausts retries
// and returns a retryable SendError.
func TestSendRobotRetryExhausted(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	ch := newRobotChannel(t, srv, WithRetry(3, time.Millisecond))
	se := asSendError(t, ch.Send(context.Background(), sampleMetricAlert()))
	if !se.Retryable {
		t.Error("Retryable: got false, want true")
	}
	if se.ChannelName != "wecom" {
		t.Errorf("ChannelName: got %q, want %q", se.ChannelName, "wecom")
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("server calls: got %d, want 3", got)
	}
}

// TestSendRobot4xxNoRetry verifies that a 4xx response is not retried.
func TestSendRobot4xxNoRetry(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	ch := newRobotChannel(t, srv, WithRetry(3, time.Millisecond))
	se := asSendError(t, ch.Send(context.Background(), sampleMetricAlert()))
	if se.Retryable {
		t.Error("Retryable: got true, want false")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("server calls: got %d, want 1", got)
	}
}

// TestSendRobotErrcode verifies that a non-zero errcode (non-token) is a
// non-retryable error.
func TestSendRobotErrcode(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"errcode":93000,"errmsg":"webhook inactive"}`))
	}))
	defer srv.Close()

	ch := newRobotChannel(t, srv, WithRetry(3, time.Millisecond))
	se := asSendError(t, ch.Send(context.Background(), sampleMetricAlert()))
	if se.Retryable {
		t.Error("Retryable: got true, want false for non-token errcode")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("server calls: got %d, want 1", got)
	}
}

// TestSendRobotNetworkError verifies that a network error is retryable.
func TestSendRobotNetworkError(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	srv.Close() // close the listener so connections are refused

	ch := newRobotChannel(t, srv, WithRetry(2, time.Millisecond))
	se := asSendError(t, ch.Send(context.Background(), sampleMetricAlert()))
	if !se.Retryable {
		t.Error("Retryable: got false, want true for network error")
	}
}

// ---------------------------------------------------------------------------
// App mode: Send
// ---------------------------------------------------------------------------

// appTestServer returns an httptest server that handles the gettoken and
// message/send endpoints. The tokenHandler and sendHandler callbacks
// receive the request and return the response body to write.
func appTestServer(t *testing.T, tokenHandler, sendHandler func(r *http.Request) []byte) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/cgi-bin/gettoken", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(tokenHandler(r))
	})
	mux.HandleFunc("/cgi-bin/message/send", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(sendHandler(r))
	})
	return httptest.NewTLSServer(mux)
}

// okToken returns a successful gettoken response body.
func okToken() []byte {
	return []byte(`{"errcode":0,"errmsg":"ok","access_token":"TOKEN-1","expires_in":7200}`)
}

// okSend returns a successful message/send response body.
func okSend() []byte {
	return []byte(`{"errcode":0,"errmsg":"ok"}`)
}

// TestSendAppTextSuccess verifies a successful app text send: the
// gettoken and message/send endpoints are both hit.
func TestSendAppTextSuccess(t *testing.T) {
	var tokenCalls, sendCalls atomic.Int32
	srv := appTestServer(t,
		func(_ *http.Request) []byte { tokenCalls.Add(1); return okToken() },
		func(_ *http.Request) []byte { sendCalls.Add(1); return okSend() },
	)
	defer srv.Close()

	ch := newAppChannel(t, srv, WithRetry(3, time.Millisecond))
	if err := ch.Send(context.Background(), sampleMetricAlert()); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got := tokenCalls.Load(); got != 1 {
		t.Errorf("gettoken calls: got %d, want 1", got)
	}
	if got := sendCalls.Load(); got != 1 {
		t.Errorf("send calls: got %d, want 1", got)
	}
}

// TestSendAppConfigBaseURL drives app mode with the endpoint supplied only
// via the stored Config.BaseURL — no test-injection option — mirroring a
// dedicated-edition deployment pointing at its private OpenAPI endpoint.
func TestSendAppConfigBaseURL(t *testing.T) {
	var sendCalls atomic.Int32
	srv := appTestServer(t,
		func(_ *http.Request) []byte { return okToken() },
		func(_ *http.Request) []byte { sendCalls.Add(1); return okSend() },
	)
	defer srv.Close()

	ch, err := New(
		Config{
			Mode:    ModeApp,
			CorpID:  "corp-1",
			AgentID: 1000002,
			Secret:  "secret-1",
			ToUser:  "@all",
			BaseURL: srv.URL,
		},
		WithHTTPClient(srv.Client()),
		WithFormatter(newTestFormatter(t)),
		WithRetry(3, time.Millisecond),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := ch.Send(context.Background(), sampleMetricAlert()); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got := sendCalls.Load(); got != 1 {
		t.Errorf("send calls: got %d, want 1", got)
	}
}

// TestSendAppMarkdownSuccess verifies a successful app markdown send and
// that the message/send body is a markdown payload.
func TestSendAppMarkdownSuccess(t *testing.T) {
	var got robotRequestBody
	srv := appTestServer(t,
		func(_ *http.Request) []byte { return okToken() },
		func(r *http.Request) []byte {
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &got)
			return okSend()
		},
	)
	defer srv.Close()

	ch := newAppChannel(t, srv, WithMessageType(MessageTypeMarkdown), WithRetry(3, time.Millisecond))
	if err := ch.Send(context.Background(), sampleMetricAlert()); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got.MsgType != "markdown" {
		t.Errorf("MsgType: got %q, want markdown", got.MsgType)
	}
}

// TestSendAppTokenCacheHit verifies that the second send reuses the cached
// access_token and does not call gettoken again.
func TestSendAppTokenCacheHit(t *testing.T) {
	var tokenCalls, sendCalls atomic.Int32
	srv := appTestServer(t,
		func(_ *http.Request) []byte { tokenCalls.Add(1); return okToken() },
		func(_ *http.Request) []byte { sendCalls.Add(1); return okSend() },
	)
	defer srv.Close()

	ch := newAppChannel(t, srv, WithRetry(3, time.Millisecond))
	for i := range 2 {
		if err := ch.Send(context.Background(), sampleMetricAlert()); err != nil {
			t.Fatalf("send #%d: %v", i+1, err)
		}
	}
	if got := tokenCalls.Load(); got != 1 {
		t.Errorf("gettoken calls: got %d, want 1 (cache hit)", got)
	}
	if got := sendCalls.Load(); got != 2 {
		t.Errorf("send calls: got %d, want 2", got)
	}
}

// TestSendAppTokenExpiredRefresh verifies that an expired cached token is
// refreshed on the next send.
func TestSendAppTokenExpiredRefresh(t *testing.T) {
	var tokenCalls atomic.Int32
	srv := appTestServer(t,
		func(_ *http.Request) []byte { tokenCalls.Add(1); return okToken() },
		func(_ *http.Request) []byte { return okSend() },
	)
	defer srv.Close()

	ch := newAppChannel(t, srv, WithRetry(3, time.Millisecond))
	if err := ch.Send(context.Background(), sampleMetricAlert()); err != nil {
		t.Fatalf("first send: %v", err)
	}
	if got := tokenCalls.Load(); got != 1 {
		t.Fatalf("gettoken calls after first send: got %d, want 1", got)
	}

	// Force token expiry.
	ch.tokenMu.Lock()
	ch.tokenExpireAt = time.Now().Add(-time.Hour)
	ch.tokenMu.Unlock()

	if err := ch.Send(context.Background(), sampleMetricAlert()); err != nil {
		t.Fatalf("second send: %v", err)
	}
	if got := tokenCalls.Load(); got != 2 {
		t.Errorf("gettoken calls after expired send: got %d, want 2", got)
	}
}

// TestSendAppTokenErrorRetry verifies that a token-related errcode from
// message/send invalidates the cache and the retry fetches a fresh token.
func TestSendAppTokenErrorRetry(t *testing.T) {
	var tokenCalls, sendCalls atomic.Int32
	srv := appTestServer(t,
		func(_ *http.Request) []byte { tokenCalls.Add(1); return okToken() },
		func(_ *http.Request) []byte {
			n := sendCalls.Add(1)
			if n == 1 {
				return []byte(`{"errcode":42001,"errmsg":"access_token expired"}`)
			}
			return okSend()
		},
	)
	defer srv.Close()

	ch := newAppChannel(t, srv, WithRetry(3, time.Millisecond))
	if err := ch.Send(context.Background(), sampleMetricAlert()); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got := tokenCalls.Load(); got != 2 {
		t.Errorf("gettoken calls: got %d, want 2 (refresh on retry)", got)
	}
	if got := sendCalls.Load(); got != 2 {
		t.Errorf("send calls: got %d, want 2", got)
	}
}

// TestSendAppErrcodeNonToken verifies that a non-token errcode from
// message/send is a non-retryable error.
func TestSendAppErrcodeNonToken(t *testing.T) {
	var sendCalls atomic.Int32
	srv := appTestServer(t,
		func(_ *http.Request) []byte { return okToken() },
		func(_ *http.Request) []byte {
			sendCalls.Add(1)
			return []byte(`{"errcode":81013,"errmsg":"invalid userid"}`)
		},
	)
	defer srv.Close()

	ch := newAppChannel(t, srv, WithRetry(3, time.Millisecond))
	se := asSendError(t, ch.Send(context.Background(), sampleMetricAlert()))
	if se.Retryable {
		t.Error("Retryable: got true, want false for non-token errcode")
	}
	if got := sendCalls.Load(); got != 1 {
		t.Errorf("send calls: got %d, want 1 (no retry)", got)
	}
}

// TestSendAppTokenFetchErrcode verifies that an errcode from gettoken is a
// non-retryable error.
func TestSendAppTokenFetchErrcode(t *testing.T) {
	srv := appTestServer(t,
		func(_ *http.Request) []byte {
			return []byte(`{"errcode":40001,"errmsg":"invalid corpid"}`)
		},
		func(_ *http.Request) []byte { return okSend() },
	)
	defer srv.Close()

	ch := newAppChannel(t, srv, WithRetry(3, time.Millisecond))
	se := asSendError(t, ch.Send(context.Background(), sampleMetricAlert()))
	// 40001 is not a token errcode, so non-retryable.
	if se.Retryable {
		t.Error("Retryable: got true, want false for non-token token-fetch errcode")
	}
}

// TestSendAppTokenFetch5xx verifies that a 5xx from gettoken is retried
// and the second attempt succeeds.
func TestSendAppTokenFetch5xx(t *testing.T) {
	var tokenCalls atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cgi-bin/gettoken":
			n := tokenCalls.Add(1)
			if n < 2 {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(okToken())
		case "/cgi-bin/message/send":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(okSend())
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	ch := newAppChannel(t, srv, WithRetry(3, time.Millisecond))
	if err := ch.Send(context.Background(), sampleMetricAlert()); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got := tokenCalls.Load(); got != 2 {
		t.Errorf("gettoken calls: got %d, want 2", got)
	}
}

// ---------------------------------------------------------------------------
// Send: mode not configured guard
// ---------------------------------------------------------------------------

// TestSendModeNotConfigured verifies that a directly-constructed Channel
// with no mode returns a non-retryable SendError.
func TestSendModeNotConfigured(t *testing.T) {
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

	ch := newRobotChannel(t, srv,
		WithRetry(1, time.Millisecond),
		WithCircuitBreaker(2, time.Hour),
	)

	for i := range 2 {
		if err := ch.Send(context.Background(), sampleMetricAlert()); err == nil {
			t.Fatalf("send #%d: expected error", i+1)
		}
	}
	callsAtOpen := calls.Load()

	err := ch.Send(context.Background(), sampleMetricAlert())
	if !errors.Is(err, channel.ErrCircuitOpen) {
		t.Fatalf("expected ErrCircuitOpen, got %v", err)
	}
	if got := calls.Load(); got != callsAtOpen {
		t.Errorf("server calls during open: got %d, want %d", got, callsAtOpen)
	}
}

// TestCircuitCooldownHalfOpenSuccess verifies that after the cooldown
// elapses the breaker transitions to half-open and a successful send
// closes it.
func TestCircuitCooldownHalfOpenSuccess(t *testing.T) {
	var fail atomic.Bool
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if fail.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}))
	defer srv.Close()

	ch := newRobotChannel(t, srv,
		WithRetry(1, time.Millisecond),
		WithCircuitBreaker(2, 50*time.Millisecond),
	)

	fail.Store(true)
	for range 2 {
		if err := ch.Send(context.Background(), sampleMetricAlert()); err == nil {
			t.Fatal("expected failure")
		}
	}
	if err := ch.Send(context.Background(), sampleMetricAlert()); !errors.Is(err, channel.ErrCircuitOpen) {
		t.Fatalf("expected ErrCircuitOpen, got %v", err)
	}

	time.Sleep(70 * time.Millisecond)
	fail.Store(false)

	if err := ch.Send(context.Background(), sampleMetricAlert()); err != nil {
		t.Fatalf("half-open send should succeed, got %v", err)
	}
	if err := ch.Send(context.Background(), sampleMetricAlert()); err != nil {
		t.Fatalf("post-recovery send should succeed, got %v", err)
	}
}

// TestCircuitResetsOnSuccess verifies that an intervening success resets
// the failure counter.
func TestCircuitResetsOnSuccess(t *testing.T) {
	var fail atomic.Bool
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if fail.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}))
	defer srv.Close()

	ch := newRobotChannel(t, srv,
		WithRetry(1, time.Millisecond),
		WithCircuitBreaker(3, time.Hour),
	)

	fail.Store(true)
	for range 2 {
		_ = ch.Send(context.Background(), sampleMetricAlert())
	}
	fail.Store(false)
	if err := ch.Send(context.Background(), sampleMetricAlert()); err != nil {
		t.Fatalf("success send: %v", err)
	}

	fail.Store(true)
	for range 2 {
		_ = ch.Send(context.Background(), sampleMetricAlert())
	}
	err := ch.Send(context.Background(), sampleMetricAlert())
	if errors.Is(err, channel.ErrCircuitOpen) {
		t.Fatal("breaker should still be closed after reset + 2 failures")
	}
}

// ---------------------------------------------------------------------------
// apiError & isRetryable & isTokenErrCode
// ---------------------------------------------------------------------------

// TestAPIErrorErrorString verifies the Error() output for all branches.
func TestAPIErrorErrorString(t *testing.T) {
	tests := []struct {
		name string
		e    *apiError
		want string
	}{
		{"status+errcode", &apiError{statusCode: 200, errcode: 42001, errmsg: "expired"},
			"wecom: status 200, errcode 42001: expired"},
		{"errcode only", &apiError{errcode: 40014, errmsg: "invalid"}, "wecom: errcode 40014: invalid"},
		{"status+err", &apiError{statusCode: 500, err: errors.New("down")}, "wecom: status 500: down"},
		{"status only", &apiError{statusCode: 404}, "wecom: status 404"},
		{"err only", &apiError{err: errors.New("net")}, "wecom: net"},
		{"unknown", &apiError{}, "wecom: unknown error"},
	}
	for _, tc := range tests {
		if got := tc.e.Error(); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestAPIErrorUnwrap verifies that Unwrap exposes the inner error.
func TestAPIErrorUnwrap(t *testing.T) {
	inner := errors.New("boom")
	e := &apiError{statusCode: 500, err: inner}
	if !errors.Is(e, inner) {
		t.Error("errors.Is should find inner error via Unwrap")
	}
}

// TestIsRetryable verifies the retry predicate classification.
func TestIsRetryable(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"network", &apiError{statusCode: 0, err: errors.New("net")}, true},
		{"5xx", &apiError{statusCode: 500}, true},
		{"503", &apiError{statusCode: 503}, true},
		{"4xx", &apiError{statusCode: 400}, false},
		{"404", &apiError{statusCode: 404}, false},
		{"2xx token errcode", &apiError{statusCode: 200, errcode: 42001}, true},
		{"2xx invalid token errcode", &apiError{statusCode: 200, errcode: 40014}, true},
		{"2xx non-token errcode", &apiError{statusCode: 200, errcode: 93000}, false},
		{"plain error", errors.New("plain"), false},
	}
	for _, tc := range tests {
		if got := isRetryable(tc.err); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestIsTokenErrCode verifies the token errcode classification.
func TestIsTokenErrCode(t *testing.T) {
	for _, code := range []int{errcodeInvalidToken, errcodeTokenExpired} {
		if !isTokenErrCode(code) {
			t.Errorf("isTokenErrCode(%d): got false, want true", code)
		}
	}
	for _, code := range []int{0, 1, 93000, 81013} {
		if isTokenErrCode(code) {
			t.Errorf("isTokenErrCode(%d): got true, want false", code)
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
	if c.MessageType != MessageTypeText {
		t.Errorf("MessageType: got %q", c.MessageType)
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

	c2 := Config{
		MessageType:             MessageTypeMarkdown,
		RetryMaxAttempts:        7,
		RetryBaseInterval:       2 * time.Second,
		CircuitFailureThreshold: 9,
		CircuitCooldown:         3 * time.Second,
	}
	applyDefaults(&c2)
	if c2.MessageType != MessageTypeMarkdown || c2.RetryMaxAttempts != 7 ||
		c2.RetryBaseInterval != 2*time.Second || c2.CircuitFailureThreshold != 9 ||
		c2.CircuitCooldown != 3*time.Second {
		t.Errorf("positive values not preserved: %+v", c2)
	}
}

// ---------------------------------------------------------------------------
// alert.Channel interface conformance
// ---------------------------------------------------------------------------

// TestImplementsPrismChannel verifies that *Channel satisfies
// alert.Channel at runtime.
func TestImplementsPrismChannel(t *testing.T) {
	var _ alert.Channel = (*Channel)(nil)
	ch, err := New(Config{Mode: ModeRobot, RobotWebhookURL: "https://example.com"})
	if err != nil {
		t.Fatal(err)
	}
	var asChannel alert.Channel = ch
	if asChannel.Name() != "wecom" {
		t.Errorf("Name via interface: got %q", asChannel.Name())
	}
}

// ---------------------------------------------------------------------------
// Interactive template card
// ---------------------------------------------------------------------------

// cardButtons is a fixed provider for the interaction tests.
func cardButtons(alert.Event) []format.ActionButton {
	return []format.ActionButton{
		{Action: format.ActionAcknowledge, Label: "认领"},
		{Action: format.ActionResolve, Label: "处理"},
		{Action: format.ActionSilence, Label: "静默"},
	}
}

// TestBuildAppPayloadTemplateCard verifies that app mode with the
// interaction policy enabled renders a button_interaction template card:
// msgtype template_card, task_id equal to the EventID, and button keys of
// the form "<action>:<event_id>".
func TestBuildAppPayloadTemplateCard(t *testing.T) {
	ch, err := New(
		Config{
			Mode:        ModeApp,
			CorpID:      "corp-1",
			AgentID:     1000002,
			Secret:      "secret-1",
			ToUser:      "@all",
			Interaction: format.InteractionOptions{Enabled: true, Buttons: cardButtons},
		},
		WithFormatter(newTestFormatter(t)),
	)
	if err != nil {
		t.Fatal(err)
	}
	evt := sampleMetricAlert()
	evt.EventID = "evt-7"
	body, err := ch.buildAppPayload(context.Background(), evt)
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		ToUser       string `json:"touser"`
		MsgType      string `json:"msgtype"`
		AgentID      int    `json:"agentid"`
		TemplateCard struct {
			CardType   string `json:"card_type"`
			TaskID     string `json:"task_id"`
			ButtonList []struct {
				Text string `json:"text"`
				Key  string `json:"key"`
			} `json:"button_list"`
		} `json:"template_card"`
	}
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatal(err)
	}
	if m.MsgType != "template_card" {
		t.Fatalf("msgtype: got %q, want template_card", m.MsgType)
	}
	if m.TemplateCard.CardType != "button_interaction" {
		t.Errorf("card_type: got %q", m.TemplateCard.CardType)
	}
	if m.TemplateCard.TaskID != "evt-7" {
		t.Errorf("task_id: got %q, want evt-7", m.TemplateCard.TaskID)
	}
	if len(m.TemplateCard.ButtonList) != 3 {
		t.Fatalf("buttons: got %d, want 3", len(m.TemplateCard.ButtonList))
	}
	first := m.TemplateCard.ButtonList[0]
	if first.Text != "认领" || first.Key != "acknowledge:evt-7" {
		t.Errorf("first button: got %q key %q", first.Text, first.Key)
	}
}

// TestBuildAppPayloadWithoutInteraction verifies that app mode without
// the interaction policy keeps the text/markdown payload shape.
func TestBuildAppPayloadWithoutInteraction(t *testing.T) {
	ch, err := New(
		Config{Mode: ModeApp, CorpID: "corp-1", AgentID: 2, Secret: "s", ToUser: "@all"},
		WithFormatter(newTestFormatter(t)),
	)
	if err != nil {
		t.Fatal(err)
	}
	evt := sampleMetricAlert()
	evt.EventID = "evt-7"
	body, err := ch.buildAppPayload(context.Background(), evt)
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		MsgType string `json:"msgtype"`
	}
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatal(err)
	}
	if m.MsgType == "template_card" {
		t.Error("template_card must not render without the interaction policy")
	}
}
