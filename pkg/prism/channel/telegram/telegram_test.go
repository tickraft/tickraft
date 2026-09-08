// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package telegram

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

// testBotToken is a fake Telegram bot token used across tests. Its suffix
// is the secret part that must never leak into logs or error messages.
const testBotToken = "1234567890:ABC-DEF1234ghIkl-zyx57W2v1u123ew11"

// testChatID is the fake target chat id used across tests.
const testChatID = "-1001234567890"

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

// mustNew creates a Channel against the given (non-TLS) test server,
// failing the test on construction error. The server's client is injected,
// the APIBase is pointed at the server, the token/chat id are pre-set, and
// a default test Formatter is injected so formatMessage produces locale-aware
// content.
func mustNew(t *testing.T, srv *httptest.Server, opts ...Option) *Channel {
	t.Helper()
	allOpts := append([]Option{WithHTTPClient(srv.Client()), WithFormatter(newTestFormatter(t))}, opts...)
	ch, err := New(Config{
		BotToken: testBotToken,
		ChatID:   testChatID,
		APIBase:  srv.URL,
	}, allOpts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return ch
}

// okHandler returns an http.HandlerFunc that writes a Telegram success
// envelope with a 200 status.
func okHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":42}}`))
	}
}

// decodePayload unmarshals a Telegram payload JSON body for inspection.
func decodePayload(t *testing.T, body []byte) payload {
	t.Helper()
	var p payload
	if err := json.Unmarshal(body, &p); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	return p
}

// ---------------------------------------------------------------------------
// Config.Validate
// ---------------------------------------------------------------------------

// TestValidateEmptyBotToken verifies that an empty BotToken fails
// validation.
func TestValidateEmptyBotToken(t *testing.T) {
	if err := (Config{ChatID: "123"}).Validate(); err == nil {
		t.Fatal("expected error for empty BotToken")
	}
}

// TestValidateEmptyChatID verifies that an empty ChatID fails validation.
func TestValidateEmptyChatID(t *testing.T) {
	if err := (Config{BotToken: "123:abc"}).Validate(); err == nil {
		t.Fatal("expected error for empty ChatID")
	}
}

// TestValidateValidConfig verifies that a config with both BotToken and
// ChatID passes validation.
func TestValidateValidConfig(t *testing.T) {
	if err := (Config{BotToken: "123:abc", ChatID: "123"}).Validate(); err != nil {
		t.Errorf("unexpected error: %v", err)
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
	if _, err := New(Config{BotToken: "123:abc"}); err == nil {
		t.Fatal("expected error for missing ChatID")
	}
	if _, err := New(Config{ChatID: "123"}); err == nil {
		t.Fatal("expected error for missing BotToken")
	}
}

// TestNewDefaults verifies that New applies default values when Config
// fields are zero. Region defaults to global, which yields the 15s
// httpclient timeout matching defaultTimeout. APIBase defaults to the
// public Telegram endpoint.
func TestNewDefaults(t *testing.T) {
	ch, err := New(Config{BotToken: "123:abc", ChatID: "123"})
	if err != nil {
		t.Fatal(err)
	}
	if ch.cfg.APIBase != defaultAPIBase {
		t.Errorf("APIBase: got %q, want %q", ch.cfg.APIBase, defaultAPIBase)
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
		WithBotToken("123:abc"),
		WithChatID("-100"),
		WithFrontendBaseURL("https://app.example.com"),
		WithAPIBase("https://botapi.example.com"),
		WithProxyURL("http://proxy:8080"),
		WithRetry(10, 2*time.Second),
		WithCircuitBreaker(3, 15*time.Second),
	)
	if err != nil {
		t.Fatal(err)
	}
	if ch.cfg.BotToken != "123:abc" {
		t.Errorf("BotToken: got %q", ch.cfg.BotToken)
	}
	if ch.cfg.ChatID != "-100" {
		t.Errorf("ChatID: got %q", ch.cfg.ChatID)
	}
	if ch.cfg.FrontendBaseURL != "https://app.example.com" {
		t.Errorf("FrontendBaseURL: got %q", ch.cfg.FrontendBaseURL)
	}
	if ch.cfg.APIBase != "https://botapi.example.com" {
		t.Errorf("APIBase: got %q", ch.cfg.APIBase)
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
	ch, err := New(Config{BotToken: "123:abc", ChatID: "123"}, WithHTTPClient(custom))
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
	ch, err := New(Config{BotToken: "123:abc", ChatID: "123"}, WithHTTPClient(custom))
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
	ch, err := New(Config{BotToken: "123:abc", ChatID: "123"}, WithLogger(logger))
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
		BotToken: "123:abc",
		ChatID:   "123",
		ProxyURL: "ftp://proxy:21",
	}); err == nil {
		t.Fatal("expected error for unsupported proxy scheme")
	}
}

// ---------------------------------------------------------------------------
// Name
// ---------------------------------------------------------------------------

// TestName verifies the channel name.
func TestName(t *testing.T) {
	ch, err := New(Config{BotToken: "123:abc", ChatID: "123"})
	if err != nil {
		t.Fatal(err)
	}
	if got := ch.Name(); got != "telegram" {
		t.Errorf("Name(): got %q, want %q", got, "telegram")
	}
}

// ---------------------------------------------------------------------------
// Send: success & payload format
// ---------------------------------------------------------------------------

// TestSendSuccess verifies that a 200 ok=true response completes the send
// and the server receives a Telegram-compatible JSON payload with the
// chat_id, text, and MarkdownV2 parse_mode.
func TestSendSuccess(t *testing.T) {
	var received []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type: got %q, want application/json", ct)
		}
		body, readErr := io.ReadAll(r.Body)
		if readErr != nil {
			t.Errorf("read body: %v", readErr)
		}
		received = body
		// The path must embed the bot token.
		if !strings.Contains(r.URL.Path, "/bot"+testBotToken+"/sendMessage") {
			t.Errorf("request path missing bot token: %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":42}}`))
	}))
	defer srv.Close()

	ch := mustNew(t, srv, WithRetry(3, time.Millisecond))
	if err := ch.Send(context.Background(), sampleMetricAlert()); err != nil {
		t.Fatalf("Send: %v", err)
	}
	p := decodePayload(t, received)
	if p.ChatID != testChatID {
		t.Errorf("ChatID: got %q, want %q", p.ChatID, testChatID)
	}
	if p.Text == "" {
		t.Error("payload text should be non-empty")
	}
	if p.ParseMode != "MarkdownV2" {
		t.Errorf("ParseMode: got %q, want MarkdownV2", p.ParseMode)
	}
}

// ---------------------------------------------------------------------------
// Send: MarkdownV2 message format
// ---------------------------------------------------------------------------

// TestFormatMessageMetric verifies the MarkdownV2 text structure for a
// metric alert, including the bold title, level/time lines, escaped
// description, and the absence of an asset link when FrontendBaseURL is
// empty.
func TestFormatMessageMetric(t *testing.T) {
	ch, err := New(Config{BotToken: "123:abc", ChatID: "123"}, WithFormatter(newTestFormatter(t)))
	if err != nil {
		t.Fatal(err)
	}
	evt := sampleMetricAlert()
	evt.Locale = "en-US"
	text := ch.formatMessage(context.Background(), evt)

	// Bold title wraps the Formatter-produced title (contains metric name, escaped).
	if !strings.Contains(text, "*Alert: cpu\\_usage") {
		t.Errorf("text should contain bold escaped title with metric name, got %q", text)
	}
	// Level line carries the en-US level label (Warning, capitalized).
	if !strings.Contains(text, "*Level:* Warning") {
		t.Errorf("text should contain level line, got %q", text)
	}
	// Time line carries the RFC3339 timestamp with escaped punctuation.
	if !strings.Contains(text, "*Time:* 2023\\-11\\-14T") {
		t.Errorf("text should contain escaped timestamp, got %q", text)
	}
	// Description contains the metric name and value, escaped.
	if !strings.Contains(text, "cpu\\_usage") || !strings.Contains(text, "95\\.50") {
		t.Errorf("description should contain escaped metric name and value, got %q", text)
	}
	// No asset link when FrontendBaseURL is empty.
	if strings.Contains(text, "View Asset") {
		t.Errorf("text should not contain asset link, got %q", text)
	}
}

// TestFormatMessageLog verifies the MarkdownV2 text structure for a log
// alert, including the keyword title and the escaped description with
// source IP.
func TestFormatMessageLog(t *testing.T) {
	ch, err := New(Config{BotToken: "123:abc", ChatID: "123"}, WithFormatter(newTestFormatter(t)))
	if err != nil {
		t.Fatal(err)
	}
	evt := sampleLogAlert()
	evt.Locale = "en-US"
	text := ch.formatMessage(context.Background(), evt)
	// Bold title contains the keyword (escaped).
	if !strings.Contains(text, "*Alert: log keyword") || !strings.Contains(text, "panic") {
		t.Errorf("text should contain bold title with keyword, got %q", text)
	}
	if !strings.Contains(text, "*Level:* Error") {
		t.Errorf("text should contain level line, got %q", text)
	}
	// Description contains the content and source IP, with escaped chars.
	if !strings.Contains(text, "nil pointer dereference") {
		t.Errorf("text should contain content, got %q", text)
	}
	if !strings.Contains(text, "10\\.0\\.0\\.5") {
		t.Errorf("text should contain escaped source IP, got %q", text)
	}
}

// TestFormatMessageAssetLink verifies that an asset deep link is
// included (as a MarkdownV2 inline link) when FrontendBaseURL is set.
func TestFormatMessageAssetLink(t *testing.T) {
	ch, err := New(Config{
		BotToken:        "123:abc",
		ChatID:          "123",
		FrontendBaseURL: "https://app.example.com",
	}, WithFormatter(newTestFormatter(t)))
	if err != nil {
		t.Fatal(err)
	}
	evt := sampleMetricAlert()
	evt.Locale = "en-US"
	text := ch.formatMessage(context.Background(), evt)
	want := "[View Asset](https://app.example.com/resources/42)"
	if !strings.Contains(text, want) {
		t.Errorf("text should contain asset link %q, got %q", want, text)
	}
}

// TestSendAssetLinkEndToEnd verifies through a real HTTP exchange that
// the asset link appears in the delivered payload text when
// FrontendBaseURL is configured.
func TestSendAssetLinkEndToEnd(t *testing.T) {
	var received []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1}}`))
	}))
	defer srv.Close()

	ch := mustNew(t, srv, WithRetry(3, time.Millisecond), WithFrontendBaseURL("https://app.example.com"))
	evt := sampleMetricAlert()
	evt.Locale = "en-US"
	if err := ch.Send(context.Background(), evt); err != nil {
		t.Fatalf("Send: %v", err)
	}
	p := decodePayload(t, received)
	if !strings.Contains(p.Text, "https://app.example.com/resources/42") {
		t.Errorf("payload text should contain asset link, got %q", p.Text)
	}
}

// TestEscapeMDV2 verifies that all Telegram MarkdownV2 special characters
// are backslash-escaped and non-special characters are left intact.
func TestEscapeMDV2(t *testing.T) {
	in := "_*[]()~`>#+-=|{}.!abc 123"
	out := escapeMDV2(in)
	for _, r := range mdv2Specials {
		// Each special char must be preceded by a backslash in the output.
		if !strings.Contains(out, "\\"+string(r)) {
			t.Errorf("special char %q not escaped in %q", r, out)
		}
	}
	if !strings.Contains(out, "abc 123") {
		t.Errorf("non-special text should be intact, got %q", out)
	}
}

// TestEscapeMDV2URL verifies that only ')' and '\' are escaped inside a
// link URL, leaving URL syntax characters like '.' and '/' intact.
func TestEscapeMDV2URL(t *testing.T) {
	in := "https://app.example.com/path_(x)/y?z=1\\2"
	out := escapeMDV2URL(in)
	if !strings.Contains(out, "app.example.com/path") {
		t.Errorf("URL syntax should be intact, got %q", out)
	}
	if !strings.Contains(out, `\)`) {
		t.Errorf("')' should be escaped, got %q", out)
	}
	if !strings.Contains(out, `\\`) {
		t.Errorf("'\\' should be escaped, got %q", out)
	}
}

// ---------------------------------------------------------------------------
// Send: retry behavior
// ---------------------------------------------------------------------------

// TestSendRetryThenSuccess verifies that a 5xx on the first attempt is
// retried and the second attempt succeeds.
func TestSendRetryThenSuccess(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) < 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1}}`))
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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	ch := mustNew(t, srv, WithRetry(3, time.Millisecond))
	se := asSendError(t, ch.Send(context.Background(), sampleMetricAlert()))
	if !se.Retryable {
		t.Error("Retryable: got false, want true")
	}
	if se.ChannelName != "telegram" {
		t.Errorf("ChannelName: got %q, want telegram", se.ChannelName)
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("server calls: got %d, want 3", got)
	}
}

// TestSend4xxNoRetry verifies that a 4xx HTTP response is not retried and
// returns a non-retryable SendError.
func TestSend4xxNoRetry(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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

// TestSend429Retryable verifies that an HTTP 429 rate-limit response is
// retried and ultimately fails with a retryable SendError.
func TestSend429Retryable(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	ch := mustNew(t, srv, WithRetry(3, time.Millisecond))
	se := asSendError(t, ch.Send(context.Background(), sampleMetricAlert()))
	if !se.Retryable {
		t.Error("Retryable: got false, want true for HTTP 429")
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("server calls: got %d, want 3 (429 retries)", got)
	}
}

// TestSendOKFalseErrorCode400 verifies that a 200 response with ok=false
// and error_code=400 (non-retryable) is not retried.
func TestSendOKFalseErrorCode400(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":false,"error_code":400,"description":"Bad Request: chat not found"}`))
	}))
	defer srv.Close()

	ch := mustNew(t, srv, WithRetry(3, time.Millisecond))
	se := asSendError(t, ch.Send(context.Background(), sampleMetricAlert()))
	if se.Retryable {
		t.Error("Retryable: got true, want false for ok=false error_code=400")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("server calls: got %d, want 1 (no retry)", got)
	}
	if !strings.Contains(se.Error(), "chat not found") {
		t.Errorf("error should surface API description, got %v", se)
	}
}

// TestSendOKFalseErrorCode429 verifies that a 200 response with ok=false
// and error_code=429 (rate limit) is retryable.
func TestSendOKFalseErrorCode429(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":false,"error_code":429,"description":"Too Many Requests"}`))
	}))
	defer srv.Close()

	ch := mustNew(t, srv, WithRetry(3, time.Millisecond))
	se := asSendError(t, ch.Send(context.Background(), sampleMetricAlert()))
	if !se.Retryable {
		t.Error("Retryable: got false, want true for ok=false error_code=429")
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("server calls: got %d, want 3 (429 retries)", got)
	}
}

// TestSendOKFalseErrorCode500 verifies that a 200 response with ok=false
// and a 5xx error_code is retryable.
func TestSendOKFalseErrorCode500(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":false,"error_code":500,"description":"Internal Server Error"}`))
	}))
	defer srv.Close()

	ch := mustNew(t, srv, WithRetry(3, time.Millisecond))
	se := asSendError(t, ch.Send(context.Background(), sampleMetricAlert()))
	if !se.Retryable {
		t.Error("Retryable: got false, want true for ok=false error_code=500")
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("server calls: got %d, want 3", got)
	}
}

// TestSend500Retryable verifies that an HTTP 500 response is retryable.
func TestSend500Retryable(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	ch := mustNew(t, srv, WithRetry(3, time.Millisecond))
	se := asSendError(t, ch.Send(context.Background(), sampleMetricAlert()))
	if !se.Retryable {
		t.Error("Retryable: got false, want true for HTTP 500")
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("server calls: got %d, want 3", got)
	}
}

// TestSendNetworkError verifies that a network error is retryable and
// ultimately fails with a retryable SendError.
func TestSendNetworkError(t *testing.T) {
	srv := httptest.NewServer(okHandler())
	client := srv.Client()
	addr := srv.URL
	srv.Close() // close the listener so connections are refused

	ch, err := New(Config{BotToken: testBotToken, ChatID: testChatID, APIBase: addr},
		WithHTTPClient(client), WithRetry(2, time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	se := asSendError(t, ch.Send(context.Background(), sampleMetricAlert()))
	if !se.Retryable {
		t.Error("Retryable: got false, want true for network error")
	}
}

// ---------------------------------------------------------------------------
// Send: empty config guard
// ---------------------------------------------------------------------------

// TestSendEmptyConfig verifies that Send returns a non-retryable error when
// the channel was constructed without a BotToken/ChatID (defensive guard
// for direct construction).
func TestSendEmptyConfig(t *testing.T) {
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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if fail.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1}}`))
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
// buildPayload
// ---------------------------------------------------------------------------

// TestBuildPayload verifies that buildPayload populates chat_id, text, and
// parse_mode from the channel config and the supplied text.
func TestBuildPayload(t *testing.T) {
	ch, err := New(Config{BotToken: "123:abc", ChatID: "-100"})
	if err != nil {
		t.Fatal(err)
	}
	p := ch.buildPayload("hello")
	if p.ChatID != "-100" {
		t.Errorf("ChatID: got %q, want -100", p.ChatID)
	}
	if p.Text != "hello" {
		t.Errorf("Text: got %q, want hello", p.Text)
	}
	if p.ParseMode != "MarkdownV2" {
		t.Errorf("ParseMode: got %q, want MarkdownV2", p.ParseMode)
	}
}

// ---------------------------------------------------------------------------
// httpError & isRetryableSendErr
// ---------------------------------------------------------------------------

// TestHTTPErrorErrorString verifies the Error() output for both the
// status-code and network-error branches.
func TestHTTPErrorErrorString(t *testing.T) {
	withStatus := &httpError{statusCode: 503, err: errors.New("upstream down")}
	if got := withStatus.Error(); got != "telegram: status 503: upstream down" {
		t.Errorf("Error(): got %q", got)
	}
	networkErr := &httpError{statusCode: 0, err: errors.New("connection refused")}
	if got := networkErr.Error(); got != "telegram: connection refused" {
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

// TestIsRetryableSendErr verifies the retry predicate classification across
// the HTTP-status and Telegram error_code layers.
func TestIsRetryableSendErr(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		// Network error.
		{"network", &httpError{statusCode: 0, err: errors.New("s")}, true},
		// Non-2xx HTTP classified by status code.
		{"http 500", &httpError{statusCode: 500, err: errors.New("s")}, true},
		{"http 503", &httpError{statusCode: 503, err: errors.New("s")}, true},
		{"http 429", &httpError{statusCode: 429, err: errors.New("s")}, true},
		{"http 400", &httpError{statusCode: 400, err: errors.New("s")}, false},
		{"http 404", &httpError{statusCode: 404, err: errors.New("s")}, false},
		// 2xx HTTP with ok=false classified by error_code.
		{"2xx ec 429", &httpError{statusCode: 200, errorCode: 429, err: errors.New("s")}, true},
		{"2xx ec 500", &httpError{statusCode: 200, errorCode: 500, err: errors.New("s")}, true},
		{"2xx ec 400", &httpError{statusCode: 200, errorCode: 400, err: errors.New("s")}, false},
		{"2xx ec 0", &httpError{statusCode: 200, err: errors.New("s")}, false},
		// Non-httpError is not retryable.
		{"plain", errors.New("plain"), false},
	}
	for _, tc := range tests {
		if got := isRetryableSendErr(tc.err); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// applyDefaults
// ---------------------------------------------------------------------------

// TestApplyDefaults verifies that zero/negative fields are replaced (and
// APIBase/Region defaulted) and positive values are preserved.
func TestApplyDefaults(t *testing.T) {
	c := Config{}
	applyDefaults(&c)
	if c.APIBase != defaultAPIBase {
		t.Errorf("APIBase: got %q, want %q", c.APIBase, defaultAPIBase)
	}
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

	// Positive/explicit values are preserved.
	c2 := Config{
		APIBase:                 "https://botapi.example.com",
		Region:                  httpclient.RegionCN,
		RetryMaxAttempts:        7,
		RetryBaseInterval:       2 * time.Second,
		CircuitFailureThreshold: 9,
		CircuitCooldown:         3 * time.Second,
	}
	applyDefaults(&c2)
	if c2.APIBase != "https://botapi.example.com" || c2.Region != httpclient.RegionCN ||
		c2.RetryMaxAttempts != 7 || c2.RetryBaseInterval != 2*time.Second ||
		c2.CircuitFailureThreshold != 9 || c2.CircuitCooldown != 3*time.Second {
		t.Errorf("explicit values not preserved: %+v", c2)
	}
}

// ---------------------------------------------------------------------------
// redactToken
// ---------------------------------------------------------------------------

// TestRedactToken verifies that the token is masked to its first 8
// characters plus "***", and that short tokens are fully masked.
func TestRedactToken(t *testing.T) {
	token := "1234567890:ABC-DEF1234ghIkl-zyx57W2v1u123ew11"
	got := redactToken(token)
	if strings.Contains(got, "ABC-DEF") {
		t.Errorf("redacted token leaks secret part: %q", got)
	}
	if !strings.HasPrefix(got, "12345678") {
		t.Errorf("redacted token should keep first 8 chars: %q", got)
	}
	if !strings.HasSuffix(got, "***") {
		t.Errorf("redacted token should end with ***: %q", got)
	}

	// Short token is fully masked.
	if got := redactToken("short"); got != "***" {
		t.Errorf("short token: got %q, want ***", got)
	}
}

// TestRedactTokenEndToEnd verifies that a failed send surfaces a redacted
// token (never the raw token) in the returned error.
func TestRedactTokenEndToEnd(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("bad request"))
	}))
	defer srv.Close()

	ch := mustNew(t, srv, WithRetry(1, time.Millisecond))
	se := asSendError(t, ch.Send(context.Background(), sampleMetricAlert()))
	// The secret suffix of the test token must never appear.
	if strings.Contains(se.Error(), "ABC-DEF1234ghIkl") {
		t.Errorf("error leaks token secret: %v", se)
	}
	if !strings.Contains(se.Error(), "***") {
		t.Errorf("error should contain redacted marker: %v", se)
	}
}

// ---------------------------------------------------------------------------
// alert.Channel interface conformance
// ---------------------------------------------------------------------------

// TestImplementsPrismChannel verifies that *Channel satisfies the
// alert.Channel interface at runtime.
func TestImplementsPrismChannel(t *testing.T) {
	var _ alert.Channel = (*Channel)(nil)
	ch, err := New(Config{BotToken: "123:abc", ChatID: "123"})
	if err != nil {
		t.Fatal(err)
	}
	var asChannel alert.Channel = ch
	if asChannel.Name() != "telegram" {
		t.Errorf("Name via interface: got %q", asChannel.Name())
	}
}
