// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package feishu

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
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/i18n"
	"github.com/tickraft/tickraft/pkg/prism/alert"
	"github.com/tickraft/tickraft/pkg/prism/channel"
	"github.com/tickraft/tickraft/pkg/prism/channel/format"

	"github.com/tickraft/tickraft/pkg/prism/channel/httpclient"
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
				Kind: alert.ViolationKindMetric,
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

// writeFeishuOK writes a successful Feishu API response.
func writeFeishuOK(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"code":0,"msg":"ok"}`))
}

// writeFeishuAPIError writes a Feishu API error response.
func writeFeishuAPIError(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"code":19021,"msg":"sign not match"}`))
}

// expectedSign computes the Feishu signature independently so tests can
// verify the channel's sign output.
func expectedSign(t *testing.T, timestamp int64, secret string) string {
	t.Helper()
	stringToSign := fmt.Sprintf("%d\n%s", timestamp, secret)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(stringToSign))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// newTestRegistry builds an i18n Registry loaded from the embedded
// resource bundle, for tests that resolve locale-aware strings through the
// channel itself (e.g. the plain-notification intranet hint).
func newTestRegistry(t *testing.T) i18n.Registry {
	t.Helper()
	logger := zap.NewNop()
	registry := i18n.NewRegistry(logger)
	loader := i18n.NewLoader(logger)
	if err := loader.LoadToRegistry(i18n.EmbeddedFS(), registry); err != nil {
		t.Fatalf("load builtin i18n resources: %v", err)
	}
	return registry
}

// newTestFormatter builds a Formatter backed by the built-in i18n resource
// bundle for use in tests. The returned Formatter renders locale-aware alert
// messages using the embedded resource files.
func newTestFormatter(t *testing.T) i18n.Formatter {
	t.Helper()
	return i18n.NewDefaultFormatter(newTestRegistry(t), zap.NewNop())
}

// newTestChannel creates a Channel pointing at srv with sensible test
// retry/circuit settings. A default test Formatter is injected so
// formatMessage produces locale-aware content.
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

// receivedBody captures the fields a Feishu webhook receives so tests can
// assert on the message type, signing fields, and payload fragments.
type receivedBody struct {
	MsgType   MessageType     `json:"msg_type"`
	Card      json.RawMessage `json:"card"`
	Content   json.RawMessage `json:"content"`
	Timestamp string          `json:"timestamp"`
	Sign      string          `json:"sign"`
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
	if err := (Config{WebhookURL: "https://open.feishu.cn/hook", Secret: "SEC"}).Validate(); err != nil {
		t.Errorf("unexpected error with secret: %v", err)
	}
	if err := (Config{WebhookURL: "https://open.feishu.cn/hook"}).Validate(); err != nil {
		t.Errorf("unexpected error without secret: %v", err)
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
	if ch.cfg.MessageType != MessageTypeInteractive {
		t.Errorf("MessageType: got %q, want %q", ch.cfg.MessageType, MessageTypeInteractive)
	}
	if ch.cfg.Region != httpclient.RegionCN {
		t.Errorf("Region: got %q, want %q", ch.cfg.Region, httpclient.RegionCN)
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
	// httpclient applies the RegionCN default (10s) when Timeout is zero.
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
		WithMessageType(MessageTypeText),
		WithFrontendBaseURL("https://app.example.com"),
		WithProxyURL("http://proxy:8080"),
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
	if ch.cfg.MessageType != MessageTypeText {
		t.Errorf("MessageType: got %q", ch.cfg.MessageType)
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

// TestNewInvalidProxy verifies that an unsupported proxy scheme causes New
// to fail with the httpclient builder error.
func TestNewInvalidProxy(t *testing.T) {
	if _, err := New(Config{WebhookURL: "https://example.com", ProxyURL: "ftp://proxy"}); err == nil {
		t.Fatal("expected error for unsupported proxy scheme")
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
	if got := ch.Name(); got != "feishu" {
		t.Errorf("Name(): got %q, want %q", got, "feishu")
	}
}

// ---------------------------------------------------------------------------
// sign & injectSign
// ---------------------------------------------------------------------------

// TestSign verifies the HMAC-SHA256 signature computation by comparing
// against an independent implementation.
func TestSign(t *testing.T) {
	timestamp := int64(1700000000)
	secret := "SECtest123456"
	expected := expectedSign(t, timestamp, secret)

	ch := &Channel{cfg: Config{Secret: secret}}
	got, err := ch.sign(timestamp, secret)
	if err != nil {
		t.Fatal(err)
	}
	if got != expected {
		t.Errorf("sign: got %q, want %q", got, expected)
	}
}

// TestInjectSign verifies that timestamp and sign are added to the body
// while existing fields are preserved.
func TestInjectSign(t *testing.T) {
	base := []byte(`{"msg_type":"text","content":{"text":"hi"}}`)
	out, err := injectSign(base, 1700000000, "sig123")
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	if string(m["timestamp"]) != `"1700000000"` {
		t.Errorf("timestamp: got %s, want %q", m["timestamp"], `"1700000000"`)
	}
	if string(m["sign"]) != `"sig123"` {
		t.Errorf("sign: got %s, want %q", m["sign"], `"sig123"`)
	}
	if string(m["msg_type"]) != `"text"` {
		t.Errorf("msg_type not preserved: got %s", m["msg_type"])
	}
	var c textContent
	if err := json.Unmarshal(m["content"], &c); err != nil {
		t.Fatalf("content not preserved: %v", err)
	}
	if c.Text != "hi" {
		t.Errorf("content text not preserved: got %q", c.Text)
	}
}

// TestInjectSignInvalidJSON verifies that injectSign fails on a malformed
// body.
func TestInjectSignInvalidJSON(t *testing.T) {
	if _, err := injectSign([]byte(`{not json`), 1, "s"); err == nil {
		t.Fatal("expected error for malformed body")
	}
}

// ---------------------------------------------------------------------------
// Send: success & signing
// ---------------------------------------------------------------------------

// TestSendInteractiveSuccess verifies a default interactive card send
// succeeds and the server receives the correct JSON payload.
func TestSendInteractiveSuccess(t *testing.T) {
	var got receivedBody
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type: got %q, want application/json", ct)
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &got); err != nil {
			t.Errorf("unmarshal: %v", err)
		}
		writeFeishuOK(w)
	}))
	defer srv.Close()

	ch := newTestChannel(t, srv)
	if err := ch.Send(context.Background(), sampleAlert()); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got.MsgType != MessageTypeInteractive {
		t.Errorf("MsgType: got %q, want %q", got.MsgType, MessageTypeInteractive)
	}
	if len(got.Card) == 0 {
		t.Error("card should be present in interactive message")
	}
}

// TestSendTextSuccess verifies a text message send succeeds and the
// content contains alert details.
func TestSendTextSuccess(t *testing.T) {
	var got receivedBody
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		writeFeishuOK(w)
	}))
	defer srv.Close()

	ch := newTestChannel(t, srv, WithMessageType(MessageTypeText))
	if err := ch.Send(context.Background(), sampleAlert()); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got.MsgType != MessageTypeText {
		t.Errorf("MsgType: got %q, want %q", got.MsgType, MessageTypeText)
	}
	var content textContent
	if err := json.Unmarshal(got.Content, &content); err != nil {
		t.Fatalf("unmarshal content: %v", err)
	}
	if !strings.Contains(content.Text, "cpu_usage") {
		t.Errorf("text content missing metric name: %q", content.Text)
	}
}

// TestSendPlainNotificationOnly verifies the L0 degraded-notification
// policy: with PlainNotificationOnly the channel downgrades to a text
// message even though the config selects interactive cards, and the asset
// link carries the localized intranet-address hint so recipients know why
// the URL only resolves inside the deployment network.
func TestSendPlainNotificationOnly(t *testing.T) {
	var got receivedBody
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		writeFeishuOK(w)
	}))
	defer srv.Close()

	ch := newTestChannel(t, srv,
		WithFrontendBaseURL("https://ui.intranet.example.com"),
		WithRegistry(newTestRegistry(t)),
		WithPlainNotificationOnly(true))
	if err := ch.Send(context.Background(), sampleAlert()); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got.MsgType != MessageTypeText {
		t.Errorf("MsgType: got %q, want %q (plain mode must override interactive)", got.MsgType, MessageTypeText)
	}
	if len(got.Card) != 0 {
		t.Error("card should be absent in plain-notification mode")
	}
	var content textContent
	if err := json.Unmarshal(got.Content, &content); err != nil {
		t.Fatalf("unmarshal content: %v", err)
	}
	if !strings.Contains(content.Text, "https://ui.intranet.example.com") {
		t.Errorf("text content missing asset link: %q", content.Text)
	}
	// sampleAlert carries no Locale, so the message renders in the default
	// zh-Hans bundle and the hint must be the Chinese label.
	if !strings.Contains(content.Text, "内网地址") {
		t.Errorf("text content missing intranet-address hint: %q", content.Text)
	}
}

// TestSendNoSecret verifies that without a secret no timestamp or sign
// fields are sent.
func TestSendNoSecret(t *testing.T) {
	var got receivedBody
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		writeFeishuOK(w)
	}))
	defer srv.Close()

	ch := newTestChannel(t, srv)
	if err := ch.Send(context.Background(), sampleAlert()); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got.Timestamp != "" {
		t.Errorf("timestamp should be absent without secret, got %q", got.Timestamp)
	}
	if got.Sign != "" {
		t.Errorf("sign should be absent without secret, got %q", got.Sign)
	}
}

// TestSendWithSecret verifies that with a secret the timestamp and sign
// fields are present in the request body and the signature matches an
// independent computation. It also confirms the timestamp is in seconds.
func TestSendWithSecret(t *testing.T) {
	var got receivedBody
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		writeFeishuOK(w)
	}))
	defer srv.Close()

	const secret = "SECtest123"
	ch := newTestChannel(t, srv, WithSecret(secret))
	if err := ch.Send(context.Background(), sampleAlert()); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got.Timestamp == "" {
		t.Fatal("timestamp should be present when secret is configured")
	}
	ts, err := strconv.ParseInt(got.Timestamp, 10, 64)
	if err != nil {
		t.Fatalf("parse timestamp: %v", err)
	}
	// Seconds-since-epoch is ~1.7e9; milliseconds would be ~1.7e12.
	if ts < 1_000_000_000 {
		t.Errorf("timestamp should be seconds, got %d", ts)
	}
	if got.Sign == "" {
		t.Fatal("sign should be present when secret is configured")
	}
	if want := expectedSign(t, ts, secret); got.Sign != want {
		t.Errorf("sign: got %q, want %q", got.Sign, want)
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
		writeFeishuOK(w)
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
	if se.ChannelName != "feishu" {
		t.Errorf("ChannelName: got %q, want %q", se.ChannelName, "feishu")
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

// TestSendAPIErrorNoRetry verifies that a Feishu API error (code != 0)
// is not retried and returns a non-retryable SendError.
func TestSendAPIErrorNoRetry(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		writeFeishuAPIError(w)
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
		writeFeishuOK(w)
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
		writeFeishuOK(w)
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

// interactiveWire mirrors the robot webhook interactive payload for test
// assertions (the production type carries the card as raw JSON).
type interactiveWire struct {
	MsgType string `json:"msg_type"`
	Card    struct {
		Header struct {
			Title struct {
				Content string `json:"content"`
			} `json:"title"`
		} `json:"header"`
		Elements []struct {
			Tag  string `json:"tag"`
			Text *struct {
				Content string `json:"content"`
			} `json:"text"`
			Actions []struct {
				URL   string            `json:"url"`
				Value map[string]string `json:"value"`
			} `json:"actions"`
		} `json:"elements"`
	} `json:"card"`
}

// textWire mirrors the robot webhook text payload for test assertions.
type textWire struct {
	MsgType string `json:"msg_type"`
	Content struct {
		Text string `json:"text"`
	} `json:"content"`
}

// TestFormatInteractiveWithLink verifies the interactive card structure
// and that an asset link button is present when FrontendBaseURL is set.
func TestFormatInteractiveWithLink(t *testing.T) {
	ch := &Channel{
		cfg:       Config{MessageType: MessageTypeInteractive, FrontendBaseURL: "https://app.example.com"},
		formatter: newTestFormatter(t),
	}

	evt := sampleAlert()
	evt.Locale = "en-US"
	body, err := ch.formatMessage(context.Background(), evt)
	if err != nil {
		t.Fatal(err)
	}
	var m interactiveWire
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatal(err)
	}
	if m.MsgType != string(MessageTypeInteractive) {
		t.Errorf("MsgType: got %q", m.MsgType)
	}
	if !strings.Contains(m.Card.Header.Title.Content, "cpu_usage") {
		t.Errorf("header title should contain metric name: got %q", m.Card.Header.Title.Content)
	}
	if len(m.Card.Elements) == 0 {
		t.Fatal("expected non-empty elements")
	}
	// First element should carry the level. For a metric alert with
	// value 95.5 vs threshold 90 (operator ">") the i18n Formatter
	// classifies the level as "warning", rendered via the en-US label
	// "Warning".
	first := m.Card.Elements[0]
	if first.Tag != "div" || first.Text == nil || !strings.Contains(first.Text.Content, "Level:") {
		t.Errorf("level element: %+v", first)
	}
	if !strings.Contains(first.Text.Content, "Warning") {
		t.Errorf("level element should contain Warning level: %q", first.Text.Content)
	}

	// Locate the asset link action button.
	foundLink := false
	for _, el := range m.Card.Elements {
		if el.Tag == "action" {
			for _, a := range el.Actions {
				if strings.Contains(a.URL, "/resources/42") {
					foundLink = true
				}
			}
		}
	}
	if !foundLink {
		t.Error("asset link action button not found")
	}
}

// TestFormatInteractiveNoLink verifies that the asset link button is
// omitted when FrontendBaseURL is empty.
func TestFormatInteractiveNoLink(t *testing.T) {
	ch := &Channel{cfg: Config{MessageType: MessageTypeInteractive}, formatter: newTestFormatter(t)}

	evt := sampleLogAlert()
	evt.Locale = "en-US"
	body, err := ch.formatMessage(context.Background(), evt)
	if err != nil {
		t.Fatal(err)
	}
	var m interactiveWire
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatal(err)
	}
	if m.MsgType != string(MessageTypeInteractive) {
		t.Errorf("MsgType: got %q", m.MsgType)
	}
	if !strings.Contains(m.Card.Header.Title.Content, "panic") {
		t.Errorf("header title should contain keyword: got %q", m.Card.Header.Title.Content)
	}
	for _, el := range m.Card.Elements {
		if el.Tag == "action" {
			t.Errorf("action element should be absent without FrontendBaseURL: %+v", el)
		}
	}
}

// TestFormatText verifies text message formatting for both metric and log
// alert types.
func TestFormatText(t *testing.T) {
	ch := &Channel{cfg: Config{MessageType: MessageTypeText}, formatter: newTestFormatter(t)}

	// Metric alert.
	evt := sampleAlert()
	evt.Locale = "en-US"
	body, err := ch.formatMessage(context.Background(), evt)
	if err != nil {
		t.Fatal(err)
	}
	var m textWire
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatal(err)
	}
	if m.MsgType != string(MessageTypeText) {
		t.Errorf("MsgType: got %q", m.MsgType)
	}
	if !strings.Contains(m.Content.Text, "cpu_usage") {
		t.Errorf("content missing metric name: %q", m.Content.Text)
	}
	if !strings.Contains(m.Content.Text, "95.50") {
		t.Errorf("content missing metric value: %q", m.Content.Text)
	}

	// Log alert.
	logEvt := sampleLogAlert()
	logEvt.Locale = "en-US"
	body, err = ch.formatMessage(context.Background(), logEvt)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(m.Content.Text, "panic") {
		t.Errorf("content missing keyword: %q", m.Content.Text)
	}
	if !strings.Contains(m.Content.Text, "nil pointer") {
		t.Errorf("content missing description: %q", m.Content.Text)
	}
}

// ---------------------------------------------------------------------------
// httpError & isRetryableSendErr
// ---------------------------------------------------------------------------

// TestHTTPErrorErrorString verifies the Error() output for both the
// status-code and network-error branches.
func TestHTTPErrorErrorString(t *testing.T) {
	withStatus := &httpError{statusCode: 503, err: errors.New("upstream down")}
	if got := withStatus.Error(); got != "feishu: status 503: upstream down" {
		t.Errorf("Error(): got %q", got)
	}
	networkErr := &httpError{statusCode: 0, err: errors.New("connection refused")}
	if got := networkErr.Error(); got != "feishu: connection refused" {
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
		{"2xx api rejection", &httpError{statusCode: 200, err: errors.New("s")}, false},
		{"plain api error", errors.New("feishu api error: code 19021"), false},
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
	for _, s := range []string{"https://example.com", "HTTPS://open.feishu.cn", "https://open.feishu.cn/hook"} {
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
	if c.MessageType != MessageTypeInteractive {
		t.Errorf("MessageType: got %q", c.MessageType)
	}
	if c.Region != httpclient.RegionCN {
		t.Errorf("Region: got %q", c.Region)
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
		MessageType:             MessageTypeText,
		Region:                  httpclient.RegionGlobal,
		RetryMaxAttempts:        7,
		RetryBaseInterval:       2 * time.Second,
		CircuitFailureThreshold: 9,
		CircuitCooldown:         3 * time.Second,
	}
	applyDefaults(&c2)
	if c2.MessageType != MessageTypeText || c2.Region != httpclient.RegionGlobal ||
		c2.RetryMaxAttempts != 7 || c2.RetryBaseInterval != 2*time.Second ||
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
	if asChannel.Name() != "feishu" {
		t.Errorf("Name via interface: got %q", asChannel.Name())
	}
}

// ---------------------------------------------------------------------------
// App mode & interactive buttons
// ---------------------------------------------------------------------------

// appMessageWire mirrors the application message payload for test
// assertions.
type appMessageWire struct {
	ReceiveID string `json:"receive_id"`
	MsgType   string `json:"msg_type"`
	Content   string `json:"content"`
}

// interactionButtons is a fixed provider for the interaction tests.
func interactionButtons(alert.Event) []format.ActionButton {
	return []format.ActionButton{
		{Action: format.ActionAcknowledge, Label: "Acknowledge"},
		{Action: format.ActionResolve, Label: "Resolve"},
		{Action: format.ActionSilence, Label: "Silence"},
	}
}

// TestFormatAppModeCallbackButtons verifies that app mode with the
// interaction policy enabled renders callback buttons carrying the action
// verb and EventID, wrapped in the application message envelope.
func TestFormatAppModeCallbackButtons(t *testing.T) {
	ch := &Channel{
		cfg: Config{
			Mode:        ModeApp,
			AppID:       "cli_a",
			AppSecret:   "s",
			ChatID:      "oc_chat",
			Interaction: format.InteractionOptions{Enabled: true, Buttons: interactionButtons},
		},
		formatter: newTestFormatter(t),
	}
	evt := sampleAlert()
	evt.EventID = "evt-42"
	body, err := ch.formatMessage(context.Background(), evt)
	if err != nil {
		t.Fatal(err)
	}
	var m appMessageWire
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatal(err)
	}
	if m.ReceiveID != "oc_chat" || m.MsgType != string(MessageTypeInteractive) {
		t.Fatalf("envelope: got %+v", m)
	}
	var card struct {
		Elements []struct {
			Tag     string `json:"tag"`
			Actions []struct {
				URL   string            `json:"url"`
				Value map[string]string `json:"value"`
			} `json:"actions"`
		} `json:"elements"`
	}
	if err := json.Unmarshal([]byte(m.Content), &card); err != nil {
		t.Fatal(err)
	}
	var action *struct {
		URL   string            `json:"url"`
		Value map[string]string `json:"value"`
	}
outer:
	for i := range card.Elements {
		el := &card.Elements[i]
		if el.Tag == "action" {
			for j := range el.Actions {
				if len(el.Actions[j].Value) > 0 {
					action = &el.Actions[j]
					break outer
				}
			}
		}
	}
	if action == nil {
		t.Fatal("callback button not found")
	}
	if got := action.Value["action"]; got != format.ActionAcknowledge {
		t.Errorf("first callback action: got %q, want %q", got, format.ActionAcknowledge)
	}
	if got := action.Value["event_id"]; got != "evt-42" {
		t.Errorf("callback event_id: got %q, want evt-42", got)
	}
}

// TestFormatWebhookModeNeverRendersCallbackButtons verifies that webhook
// (custom robot) mode keeps link-only cards even when the interaction
// policy is enabled: robot cards cannot receive button callbacks.
func TestFormatWebhookModeNeverRendersCallbackButtons(t *testing.T) {
	ch := &Channel{
		cfg: Config{
			Mode:            ModeRobot,
			WebhookURL:      "https://open.feishu.cn/hook",
			FrontendBaseURL: "https://app.example.com",
			Interaction:     format.InteractionOptions{Enabled: true, Buttons: interactionButtons},
		},
		formatter: newTestFormatter(t),
	}
	evt := sampleAlert()
	evt.EventID = "evt-42"
	body, err := ch.formatMessage(context.Background(), evt)
	if err != nil {
		t.Fatal(err)
	}
	var m interactiveWire
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatal(err)
	}
	for _, el := range m.Card.Elements {
		if el.Tag != "action" {
			continue
		}
		for _, a := range el.Actions {
			if len(a.Value) > 0 {
				t.Errorf("webhook mode must not render callback buttons: %+v", a)
			}
		}
	}
}

// TestValidateAppMode covers app-mode validation branches and the empty
// mode inference.
func TestValidateAppMode(t *testing.T) {
	if err := (Config{Mode: ModeApp, AppID: "a", AppSecret: "s", ChatID: "c"}).Validate(); err != nil {
		t.Errorf("valid app config: %v", err)
	}
	for _, cfg := range []Config{
		{Mode: ModeApp},
		{Mode: ModeApp, AppID: "a"},
		{Mode: ModeApp, AppID: "a", AppSecret: "s"},
		{Mode: "bogus"},
	} {
		if err := cfg.Validate(); err == nil {
			t.Errorf("expected error for %+v", cfg)
		}
	}
	// Empty mode infers app mode from app credentials alone.
	if err := (Config{AppID: "a", AppSecret: "s", ChatID: "c"}).Validate(); err != nil {
		t.Errorf("inferred app mode: %v", err)
	}
}

// TestSendAppModeBaseURLOverride drives the full app-mode pipeline —
// tenant_access_token fetch, then im/v1/messages delivery — against an
// httptest server injected via withBaseURL, and verifies the token is
// cached across sends.
func TestSendAppModeBaseURLOverride(t *testing.T) {
	var tokenCalls, sendCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case pathTenantToken:
			tokenCalls.Add(1)
			if r.Method != http.MethodPost {
				t.Errorf("token: got method %s", r.Method)
			}
			_, _ = w.Write([]byte(`{"code":0,"tenant_access_token":"t-1","expire":3600}`))
		case pathMessageSend:
			sendCalls.Add(1)
			if got := r.Header.Get("Authorization"); got != "Bearer t-1" {
				t.Errorf("authorization: got %q", got)
			}
			if got := r.URL.Query().Get("receive_id_type"); got != "chat_id" {
				t.Errorf("receive_id_type: got %q", got)
			}
			_, _ = w.Write([]byte(`{"code":0}`))
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	ch, err := New(
		Config{Mode: ModeApp, AppID: "cli_a", AppSecret: "s", ChatID: "oc_chat"},
		withBaseURL(srv.URL),
		WithHTTPClient(srv.Client()),
		WithFormatter(newTestFormatter(t)),
	)
	if err != nil {
		t.Fatal(err)
	}
	evt := sampleAlert()
	evt.EventID = "evt-42"
	if err := ch.Send(context.Background(), evt); err != nil {
		t.Fatal(err)
	}
	if err := ch.Send(context.Background(), evt); err != nil {
		t.Fatal(err)
	}
	if tokenCalls.Load() != 1 {
		t.Errorf("token fetches: got %d, want 1 (cached)", tokenCalls.Load())
	}
	if sendCalls.Load() != 2 {
		t.Errorf("message sends: got %d, want 2", sendCalls.Load())
	}
}

// TestValidateBaseURLScheme verifies the stored base_url accepts http (a
// dedicated-edition private endpoint may be plain http inside the intranet)
// and https but rejects other schemes.
func TestValidateBaseURLScheme(t *testing.T) {
	base := Config{Mode: ModeApp, AppID: "a", AppSecret: "s", ChatID: "c"}
	for _, u := range []string{"http://feishu.intranet.example", "https://feishu.intranet.example"} {
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

// TestSendAppModeConfigBaseURL drives app mode with the endpoint supplied
// only via the stored Config.BaseURL — no test-injection option — mirroring
// a dedicated-edition deployment pointing at its private OpenAPI endpoint.
func TestSendAppModeConfigBaseURL(t *testing.T) {
	var sendCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case pathTenantToken:
			_, _ = w.Write([]byte(`{"code":0,"tenant_access_token":"t-1","expire":3600}`))
		case pathMessageSend:
			sendCalls.Add(1)
			_, _ = w.Write([]byte(`{"code":0}`))
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	ch, err := New(
		Config{Mode: ModeApp, AppID: "cli_a", AppSecret: "s", ChatID: "oc_chat", BaseURL: srv.URL},
		WithHTTPClient(srv.Client()),
		WithFormatter(newTestFormatter(t)),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := ch.Send(context.Background(), sampleAlert()); err != nil {
		t.Fatal(err)
	}
	if sendCalls.Load() != 1 {
		t.Errorf("message sends: got %d, want 1", sendCalls.Load())
	}
}
