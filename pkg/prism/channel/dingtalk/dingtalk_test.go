// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package dingtalk

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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

// sampleAlert returns a representative metric Event used across tests.
func sampleAlert() alert.Event {
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

// sampleLogAlert returns a log-type Event for testing log formatting.
func sampleLogAlert() alert.Event {
	return alert.Event{
		Type:     alert.TypeLog,
		AssetID:  10,
		TenantID: 3,
		Timestamp: time.Unix(1700000000,
			0).UTC(),
		Violations: []alert.Violation{
			{
				Kind:     alert.ViolationKindLog,
				Severity: "error",
				Source:   "10.0.0.1",
				Log: &alert.LogContext{
					Keyword: "panic",
					Content: "goroutine panic: nil pointer",
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

// newTestClient returns an HTTP client that skips TLS verification, for
// use with httptest.NewTLSServer.
//
// The bare &http.Client{} here is a test fixture for TLS-protected test
// servers; production code should use httpx.NewPoolClient for connection
// pooling.
func newTestClient() *http.Client {
	return &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
}

// writeDingTalkOK writes a successful DingTalk API response.
func writeDingTalkOK(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
}

// writeDingTalkAPIError writes a DingTalk API error response.
func writeDingTalkAPIError(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"errcode":310000,"errmsg":"sign not match"}`))
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

// newTestChannel creates a Channel pointing at srv with sensible test
// retry/circuit settings. A default test Formatter is injected so
// formatTextMessage/formatMarkdownMessage produce locale-aware content.
func newTestChannel(t *testing.T, srv *httptest.Server, opts ...Option) *Channel {
	t.Helper()
	defaultOpts := []Option{
		WithHTTPClient(newTestClient()),
		WithRetry(3, time.Millisecond),
		WithFormatter(newTestFormatter(t)),
	}
	allOpts := make([]Option, 0, len(defaultOpts)+len(opts))
	allOpts = append(allOpts, defaultOpts...)
	allOpts = append(allOpts, opts...)
	ch, err := New(Config{WebhookURL: srv.URL}, allOpts...)
	if err != nil {
		t.Fatal(err)
	}
	return ch
}

// ---------------------------------------------------------------------------
// Config.Validate
// ---------------------------------------------------------------------------

// TestValidateEmptyWebhookURL verifies that an empty URL fails validation.
func TestValidateEmptyWebhookURL(t *testing.T) {
	if err := (Config{}).Validate(); err == nil {
		t.Fatal("expected error for empty webhook url")
	}
}

// TestValidateNonHTTPSURL verifies that non-https URLs fail validation.
func TestValidateNonHTTPSURL(t *testing.T) {
	for _, u := range []string{"http://example.com", "ftp://example.com", "example.com"} {
		if err := (Config{WebhookURL: u}).Validate(); err == nil {
			t.Errorf("expected error for URL %q", u)
		}
	}
}

// TestValidateValidConfig verifies that valid configs pass validation.
func TestValidateValidConfig(t *testing.T) {
	if err := (Config{WebhookURL: "https://oapi.dingtalk.com/hook", Secret: "SEC"}).Validate(); err != nil {
		t.Errorf("unexpected error with secret: %v", err)
	}
	if err := (Config{WebhookURL: "https://oapi.dingtalk.com/hook"}).Validate(); err != nil {
		t.Errorf("unexpected error without secret: %v", err)
	}
}

// TestValidateBaseURLScheme verifies the stored base_url (consumed by the
// deployment's Stream-mode client, not the outbound webhook) accepts http
// and https but rejects other schemes.
func TestValidateBaseURLScheme(t *testing.T) {
	base := Config{WebhookURL: "https://oapi.dingtalk.com/hook"}
	for _, u := range []string{"http://api.dingtalk.intranet.example", "https://api.dingtalk.intranet.example"} {
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

// TestBaseURLJSONRoundTrip verifies the stored base_url survives the config
// JSON round trip the channel management UI performs, alongside the
// Stream-mode credentials it travels with.
func TestBaseURLJSONRoundTrip(t *testing.T) {
	raw := `{"webhook_url":"https://oapi.dingtalk.com/hook","app_key":"ak","app_secret":"as",` +
		`"base_url":"https://api.dingtalk.intranet.example"}`
	var cfg Config
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if cfg.BaseURL != "https://api.dingtalk.intranet.example" {
		t.Errorf("base url: got %q", cfg.BaseURL)
	}
	if cfg.AppKey != "ak" || cfg.AppSecret != "as" {
		t.Errorf("stream credentials: got %q/%q", cfg.AppKey, cfg.AppSecret)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("validate: %v", err)
	}
}

// ---------------------------------------------------------------------------
// New: construction & defaults
// ---------------------------------------------------------------------------

// TestNewInvalidConfig verifies that New rejects an invalid Config.
func TestNewInvalidConfig(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("expected error for empty config")
	}
	if _, err := New(Config{WebhookURL: "http://example.com"}); err == nil {
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

// TestNewWithOptions verifies that Options override Config fields.
func TestNewWithOptions(t *testing.T) {
	ch, err := New(
		Config{},
		WithWebhookURL("https://example.com"),
		WithSecret("SEC"),
		WithMessageType(MessageTypeMarkdown),
		WithRetry(10, 2*time.Second),
		WithCircuitBreaker(3, 15*time.Second),
	)
	if err != nil {
		t.Fatal(err)
	}
	if ch.cfg.WebhookURL != "https://example.com" {
		t.Errorf("WebhookURL: got %q", ch.cfg.WebhookURL)
	}
	if ch.cfg.Secret != "SEC" {
		t.Errorf("Secret: got %q", ch.cfg.Secret)
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

// TestNewWithHTTPClient verifies that an injected client is used as-is.
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
	if got := ch.Name(); got != "dingtalk" {
		t.Errorf("Name(): got %q, want %q", got, "dingtalk")
	}
}

// ---------------------------------------------------------------------------
// sign
// ---------------------------------------------------------------------------

// TestSign verifies the HMAC-SHA256 signature computation by comparing
// against an independent implementation.
func TestSign(t *testing.T) {
	timestamp := int64(1577808000000)
	secret := "SECtest123456"

	stringToSign := fmt.Sprintf("%d\n%s", timestamp, secret)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(stringToSign))
	expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	ch := &Channel{cfg: Config{Secret: secret}}
	got, err := ch.sign(timestamp, secret)
	if err != nil {
		t.Fatal(err)
	}
	if got != expected {
		t.Errorf("sign: got %q, want %q", got, expected)
	}
}

// ---------------------------------------------------------------------------
// buildURL
// ---------------------------------------------------------------------------

// TestBuildURLNoSecret verifies that without a secret the URL is returned
// as-is.
func TestBuildURLNoSecret(t *testing.T) {
	original := "https://oapi.dingtalk.com/robot/send?access_token=xxx"
	ch := &Channel{cfg: Config{WebhookURL: original}}
	got, err := ch.buildURL(1577808000000)
	if err != nil {
		t.Fatal(err)
	}
	if got != original {
		t.Errorf("buildURL without secret: got %q, want %q", got, original)
	}
}

// TestBuildURLWithSecret verifies that with a secret the timestamp and
// sign query parameters are appended.
func TestBuildURLWithSecret(t *testing.T) {
	ch := &Channel{cfg: Config{
		WebhookURL: "https://oapi.dingtalk.com/robot/send?access_token=xxx",
		Secret:     "SECtest",
	}}
	got, err := ch.buildURL(1577808000000)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	if u.Query().Get("timestamp") != "1577808000000" {
		t.Errorf("timestamp: got %q, want %q", u.Query().Get("timestamp"), "1577808000000")
	}
	if u.Query().Get("sign") == "" {
		t.Error("sign should be non-empty")
	}
	if u.Query().Get("access_token") != "xxx" {
		t.Errorf("access_token should be preserved: got %q", u.Query().Get("access_token"))
	}
}

// ---------------------------------------------------------------------------
// Send: success & retry
// ---------------------------------------------------------------------------

// TestSendTextSuccess verifies a text message send succeeds and the server
// receives the correct JSON payload.
func TestSendTextSuccess(t *testing.T) {
	var received textMessage
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type: got %q, want application/json", ct)
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &received); err != nil {
			t.Errorf("unmarshal: %v", err)
		}
		writeDingTalkOK(w)
	}))
	defer srv.Close()

	ch := newTestChannel(t, srv)
	if err := ch.Send(context.Background(), sampleAlert()); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if received.MsgType != MessageTypeText {
		t.Errorf("MsgType: got %q, want %q", received.MsgType, MessageTypeText)
	}
	if !strings.Contains(received.Text.Content, "cpu_usage") {
		t.Errorf("text content missing metric name: %q", received.Text.Content)
	}
}

// TestSendMarkdownSuccess verifies a markdown message send succeeds.
func TestSendMarkdownSuccess(t *testing.T) {
	var received markdownMessage
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &received)
		writeDingTalkOK(w)
	}))
	defer srv.Close()

	ch := newTestChannel(t, srv, WithMessageType(MessageTypeMarkdown))
	if err := ch.Send(context.Background(), sampleAlert()); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if received.MsgType != MessageTypeMarkdown {
		t.Errorf("MsgType: got %q, want %q", received.MsgType, MessageTypeMarkdown)
	}
	if !strings.Contains(received.Markdown.Text, "cpu_usage") {
		t.Errorf("markdown text missing metric name: %q", received.Markdown.Text)
	}
}

// TestSendNoSecret verifies that without a secret no timestamp or sign
// query parameters are sent.
func TestSendNoSecret(t *testing.T) {
	var gotTimestamp, gotSign string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTimestamp = r.URL.Query().Get("timestamp")
		gotSign = r.URL.Query().Get("sign")
		writeDingTalkOK(w)
	}))
	defer srv.Close()

	ch := newTestChannel(t, srv)
	if err := ch.Send(context.Background(), sampleAlert()); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if gotTimestamp != "" {
		t.Errorf("timestamp should be absent without secret, got %q", gotTimestamp)
	}
	if gotSign != "" {
		t.Errorf("sign should be absent without secret, got %q", gotSign)
	}
}

// TestSendWithSecret verifies that with a secret the timestamp and sign
// query parameters are present in the request URL.
func TestSendWithSecret(t *testing.T) {
	var gotTimestamp, gotSign string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTimestamp = r.URL.Query().Get("timestamp")
		gotSign = r.URL.Query().Get("sign")
		writeDingTalkOK(w)
	}))
	defer srv.Close()

	ch := newTestChannel(t, srv, WithSecret("SECtest"))
	if err := ch.Send(context.Background(), sampleAlert()); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if gotTimestamp == "" {
		t.Error("timestamp should be present when secret is configured")
	}
	if gotSign == "" {
		t.Error("sign should be present when secret is configured")
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
		writeDingTalkOK(w)
	}))
	defer srv.Close()

	ch := newTestChannel(t, srv)
	if err := ch.Send(context.Background(), sampleAlert()); err != nil {
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

	ch := newTestChannel(t, srv)
	se := asSendError(t, ch.Send(context.Background(), sampleAlert()))
	if !se.Retryable {
		t.Error("Retryable: got false, want true")
	}
	if se.ChannelName != "dingtalk" {
		t.Errorf("ChannelName: got %q, want %q", se.ChannelName, "dingtalk")
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

	ch := newTestChannel(t, srv)
	se := asSendError(t, ch.Send(context.Background(), sampleAlert()))
	if se.Retryable {
		t.Error("Retryable: got true, want false")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("server calls: got %d, want 1 (no retry)", got)
	}
}

// TestSendAPIErrorNoRetry verifies that a DingTalk API error (errcode != 0)
// is not retried and returns a non-retryable SendError.
func TestSendAPIErrorNoRetry(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		writeDingTalkAPIError(w)
	}))
	defer srv.Close()

	ch := newTestChannel(t, srv)
	se := asSendError(t, ch.Send(context.Background(), sampleAlert()))
	if se.Retryable {
		t.Error("Retryable: got true, want false for API error")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("server calls: got %d, want 1 (no retry for API error)", got)
	}
}

// TestSendInvalidResponseBody verifies that a non-JSON response body is
// treated as a non-retryable error.
func TestSendInvalidResponseBody(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()

	ch := newTestChannel(t, srv)
	se := asSendError(t, ch.Send(context.Background(), sampleAlert()))
	if se.Retryable {
		t.Error("Retryable: got true, want false for parse error")
	}
}

// TestSendNetworkError verifies that a network error is retryable and
// ultimately fails with a retryable SendError.
func TestSendNetworkError(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeDingTalkOK(w)
	}))
	ch := newTestChannel(t, srv, WithRetry(2, time.Millisecond))
	srv.Close() // close listener so connections are refused

	se := asSendError(t, ch.Send(context.Background(), sampleAlert()))
	if !se.Retryable {
		t.Error("Retryable: got false, want true for network error")
	}
}

// ---------------------------------------------------------------------------
// Send: empty URL guard
// ---------------------------------------------------------------------------

// TestSendEmptyWebhookURL verifies that Send returns a non-retryable
// SendError when the channel was constructed without a URL.
func TestSendEmptyWebhookURL(t *testing.T) {
	ch := &Channel{}
	se := asSendError(t, ch.Send(context.Background(), sampleAlert()))
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

	ch := newTestChannel(t, srv,
		WithRetry(1, time.Millisecond),
		WithCircuitBreaker(2, time.Hour),
	)

	// Two failed sends open the breaker (threshold 2).
	for i := range 2 {
		if err := ch.Send(context.Background(), sampleAlert()); err == nil {
			t.Fatalf("send #%d: expected error", i+1)
		}
	}
	callsAtOpen := calls.Load()

	// Third send is short-circuited by the open breaker.
	err := ch.Send(context.Background(), sampleAlert())
	if !errors.Is(err, channel.ErrCircuitOpen) {
		t.Fatalf("expected ErrCircuitOpen, got %v", err)
	}
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
		writeDingTalkOK(w)
	}))
	defer srv.Close()

	ch := newTestChannel(t, srv,
		WithRetry(1, time.Millisecond),
		WithCircuitBreaker(3, time.Hour),
	)

	// Two failures (below threshold 3).
	fail.Store(true)
	for range 2 {
		_ = ch.Send(context.Background(), sampleAlert())
	}

	// Success resets the counter.
	fail.Store(false)
	if err := ch.Send(context.Background(), sampleAlert()); err != nil {
		t.Fatalf("success send: %v", err)
	}

	// Two more failures should not open the breaker (counter was reset).
	fail.Store(true)
	for range 2 {
		_ = ch.Send(context.Background(), sampleAlert())
	}

	// Next send should be admitted (breaker still closed), not ErrCircuitOpen.
	err := ch.Send(context.Background(), sampleAlert())
	if errors.Is(err, channel.ErrCircuitOpen) {
		t.Fatal("breaker should still be closed after reset + 2 failures")
	}
}

// ---------------------------------------------------------------------------
// Message formatting
// ---------------------------------------------------------------------------

// TestFormatTextMessage verifies text message formatting for both metric
// and log alert types.
func TestFormatTextMessage(t *testing.T) {
	ch := &Channel{cfg: Config{MessageType: MessageTypeText}, formatter: newTestFormatter(t)}

	// Metric alert
	body, err := ch.formatTextMessage(context.Background(), sampleAlert())
	if err != nil {
		t.Fatal(err)
	}
	var msg textMessage
	if err := json.Unmarshal(body, &msg); err != nil {
		t.Fatal(err)
	}
	if msg.MsgType != MessageTypeText {
		t.Errorf("MsgType: got %q", msg.MsgType)
	}
	if !strings.Contains(msg.Text.Content, "cpu_usage") {
		t.Errorf("content missing metric name: %q", msg.Text.Content)
	}
	if !strings.Contains(msg.Text.Content, "95.50") {
		t.Errorf("content missing metric value: %q", msg.Text.Content)
	}

	// Log alert
	body, err = ch.formatTextMessage(context.Background(), sampleLogAlert())
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(body, &msg); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg.Text.Content, "panic") {
		t.Errorf("content missing keyword: %q", msg.Text.Content)
	}
	if !strings.Contains(msg.Text.Content, "nil pointer") {
		t.Errorf("content missing content: %q", msg.Text.Content)
	}
}

// TestFormatMarkdownMessage verifies markdown message formatting for both
// metric and log alert types.
func TestFormatMarkdownMessage(t *testing.T) {
	ch := &Channel{cfg: Config{MessageType: MessageTypeMarkdown}, formatter: newTestFormatter(t)}

	// Metric alert
	body, err := ch.formatMarkdownMessage(context.Background(), sampleAlert())
	if err != nil {
		t.Fatal(err)
	}
	var msg markdownMessage
	if err := json.Unmarshal(body, &msg); err != nil {
		t.Fatal(err)
	}
	if msg.MsgType != MessageTypeMarkdown {
		t.Errorf("MsgType: got %q", msg.MsgType)
	}
	if msg.Markdown.Title != "Tickraft Alert" {
		t.Errorf("Title: got %q", msg.Markdown.Title)
	}
	if !strings.Contains(msg.Markdown.Text, "cpu_usage") {
		t.Errorf("text missing metric name: %q", msg.Markdown.Text)
	}

	// Log alert
	body, err = ch.formatMarkdownMessage(context.Background(), sampleLogAlert())
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(body, &msg); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg.Markdown.Text, "panic") {
		t.Errorf("text missing keyword: %q", msg.Markdown.Text)
	}
	if !strings.Contains(msg.Markdown.Text, "10.0.0.1") {
		t.Errorf("text missing source IP: %q", msg.Markdown.Text)
	}
}

// ---------------------------------------------------------------------------
// httpError & isRetryableSendErr
// ---------------------------------------------------------------------------

// TestHTTPErrorErrorString verifies the Error() output for both the
// status-code and network-error branches.
func TestHTTPErrorErrorString(t *testing.T) {
	withStatus := &httpError{statusCode: 503, err: errors.New("upstream down")}
	if got := withStatus.Error(); got != "dingtalk: status 503: upstream down" {
		t.Errorf("Error(): got %q", got)
	}
	networkErr := &httpError{statusCode: 0, err: errors.New("connection refused")}
	if got := networkErr.Error(); got != "dingtalk: connection refused" {
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
	for _, s := range []string{"https://example.com", "https://oapi.dingtalk.com/hook"} {
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

// TestImplementsPrismChannel verifies that *Channel satisfies the
// alert.Channel interface at runtime.
func TestImplementsPrismChannel(t *testing.T) {
	var _ alert.Channel = (*Channel)(nil)
	ch, err := New(Config{WebhookURL: "https://example.com"})
	if err != nil {
		t.Fatal(err)
	}
	var asChannel alert.Channel = ch
	if asChannel.Name() != "dingtalk" {
		t.Errorf("Name via interface: got %q", asChannel.Name())
	}
}
