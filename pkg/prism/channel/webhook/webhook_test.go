// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package webhook

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

	"github.com/tickraft/tickraft/pkg/prism/alert"
	"github.com/tickraft/tickraft/pkg/prism/channel"
	"go.uber.org/zap"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// sampleAlert returns a representative Event used across tests.
func sampleAlert() alert.Event {
	return alert.Event{
		Type:      alert.TypeMetric,
		AssetID:   42,
		TenantID:  7,
		Timestamp: time.Unix(1700000000, 0).UTC(),
		Violations: []alert.Violation{{
			Kind:     alert.ViolationKindMetric,
			Metric:   &alert.MetricContext{Name: "cpu_usage", Value: 95.5, Threshold: 90.0},
			Severity: "critical",
		}},
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

// ---------------------------------------------------------------------------
// Config.Validate
// ---------------------------------------------------------------------------

// TestValidateEmptyURL verifies that an empty URL fails validation.
func TestValidateEmptyURL(t *testing.T) {
	if err := (Config{}).Validate(); err == nil {
		t.Fatal("expected error for empty URL")
	}
}

// TestValidateNonHTTPURL verifies that a non-http(s) URL fails validation.
func TestValidateNonHTTPURL(t *testing.T) {
	for _, url := range []string{"ftp://example.com", "example.com", "mailto:x@y"} {
		if err := (Config{URL: url}).Validate(); err == nil {
			t.Errorf("expected error for URL %q", url)
		}
	}
}

// TestValidateValidURL verifies that http and https URLs pass validation.
func TestValidateValidURL(t *testing.T) {
	for _, url := range []string{"http://example.com", "https://example.com/hook"} {
		if err := (Config{URL: url}).Validate(); err != nil {
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
	if _, err := New(Config{URL: "ftp://x"}); err == nil {
		t.Fatal("expected error for non-http URL")
	}
}

// TestNewDefaults verifies that New applies default values when Config
// fields are zero.
func TestNewDefaults(t *testing.T) {
	ch, err := New(Config{URL: "http://example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if ch.cfg.Timeout != defaultTimeout {
		t.Errorf("Timeout: got %v, want %v", ch.cfg.Timeout, defaultTimeout)
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
	if ch.client == nil {
		t.Error("client should be non-nil")
	}
	if ch.client.Timeout != defaultTimeout {
		t.Errorf("client.Timeout: got %v, want %v", ch.client.Timeout, defaultTimeout)
	}
	if ch.breaker == nil || ch.retry == nil || ch.logger == nil {
		t.Error("breaker, retry, logger should all be non-nil")
	}
}

// TestNewWithOptions verifies that Options override Config fields.
func TestNewWithOptions(t *testing.T) {
	hdrs := map[string]string{"Authorization": "Bearer x"}
	ch, err := New(
		Config{},
		WithURL("http://example.com"),
		WithTimeout(5*time.Second),
		WithHeaders(hdrs),
		WithRetry(10, 2*time.Second),
		WithCircuitBreaker(3, 15*time.Second),
	)
	if err != nil {
		t.Fatal(err)
	}
	if ch.cfg.URL != "http://example.com" {
		t.Errorf("URL: got %q", ch.cfg.URL)
	}
	if ch.cfg.Timeout != 5*time.Second {
		t.Errorf("Timeout: got %v", ch.cfg.Timeout)
	}
	if ch.headers["Authorization"] != "Bearer x" {
		t.Errorf("headers: got %v", ch.headers)
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
// The bare &http.Client{} below is a test fixture intentionally used to
// verify the WithHTTPClient injection contract; production code should use
// httpx.NewPoolClient for connection pooling.
func TestNewWithHTTPClient(t *testing.T) {
	custom := &http.Client{Timeout: 7 * time.Second}
	ch, err := New(Config{URL: "http://example.com"}, WithHTTPClient(custom))
	if err != nil {
		t.Fatal(err)
	}
	if ch.client != custom {
		t.Error("injected client not used")
	}
	if ch.client.Timeout != 7*time.Second {
		t.Errorf("client.Timeout: got %v, want 7s", ch.client.Timeout)
	}
}

// TestNewHTTPClientTimeoutSet verifies that an injected client without a
// timeout gets the configured timeout.
func TestNewHTTPClientTimeoutSet(t *testing.T) {
	custom := &http.Client{}
	ch, err := New(
		Config{URL: "http://example.com"},
		WithHTTPClient(custom),
		WithTimeout(5*time.Second),
	)
	if err != nil {
		t.Fatal(err)
	}
	if ch.client.Timeout != 5*time.Second {
		t.Errorf("client.Timeout: got %v, want 5s", ch.client.Timeout)
	}
}

// TestNewWithLogger verifies that an injected logger is used.
func TestNewWithLogger(t *testing.T) {
	logger := zap.NewExample()
	ch, err := New(Config{URL: "http://example.com"}, WithLogger(logger))
	if err != nil {
		t.Fatal(err)
	}
	if ch.logger != logger {
		t.Error("injected logger not used")
	}
}

// TestNewHeadersCopied verifies that New copies the headers map so later
// mutations to the caller's map do not affect the channel.
func TestNewHeadersCopied(t *testing.T) {
	hdrs := map[string]string{"X-Key": "v1"}
	ch, err := New(Config{URL: "http://example.com"}, WithHeaders(hdrs))
	if err != nil {
		t.Fatal(err)
	}
	hdrs["X-Key"] = "mutated"
	if ch.headers["X-Key"] != "v1" {
		t.Errorf("headers should be copied, got %q", ch.headers["X-Key"])
	}
}

// ---------------------------------------------------------------------------
// Name
// ---------------------------------------------------------------------------

// TestName verifies the channel name.
func TestName(t *testing.T) {
	ch, err := New(Config{URL: "http://example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if got := ch.Name(); got != "webhook" {
		t.Errorf("Name(): got %q, want %q", got, "webhook")
	}
}

// ---------------------------------------------------------------------------
// Send: success & retry
// ---------------------------------------------------------------------------

// TestSendSuccess verifies a 2xx response completes the send and the
// server receives the correct JSON payload.
func TestSendSuccess(t *testing.T) {
	var received alert.Event
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type: got %q, want application/json", ct)
		}
		body, readErr := io.ReadAll(r.Body)
		if readErr != nil {
			t.Errorf("read body: %v", readErr)
		}
		if jsonErr := json.Unmarshal(body, &received); jsonErr != nil {
			t.Errorf("unmarshal: %v", jsonErr)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ch, err := New(Config{URL: srv.URL}, WithRetry(3, time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	if err := ch.Send(context.Background(), sampleAlert()); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if received.AssetID != 42 || len(received.Violations) == 0 || received.Violations[0].Metric == nil || received.Violations[0].Metric.Name != "cpu_usage" {
		t.Errorf("received alert mismatch: %+v", received)
	}
}

// TestSendRetryThenSuccess verifies that a 5xx on the first attempt is
// retried and the second attempt succeeds.
func TestSendRetryThenSuccess(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) < 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ch, err := New(Config{URL: srv.URL}, WithRetry(3, time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	ch, err := New(Config{URL: srv.URL}, WithRetry(3, time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	se := asSendError(t, ch.Send(context.Background(), sampleAlert()))
	if !se.Retryable {
		t.Error("Retryable: got false, want true")
	}
	if se.ChannelName != "webhook" {
		t.Errorf("ChannelName: got %q, want %q", se.ChannelName, "webhook")
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("server calls: got %d, want 3", got)
	}
}

// TestSend4xxNoRetry verifies that a 4xx response is not retried and
// returns a non-retryable SendError.
func TestSend4xxNoRetry(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	ch, err := New(Config{URL: srv.URL}, WithRetry(3, time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	se := asSendError(t, ch.Send(context.Background(), sampleAlert()))
	if se.Retryable {
		t.Error("Retryable: got true, want false")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("server calls: got %d, want 1 (no retry)", got)
	}
}

// TestSendCustomHeaders verifies that custom headers reach the server.
func TestSendCustomHeaders(t *testing.T) {
	var gotAuth, gotCustom string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotCustom = r.Header.Get("X-Tickraft-Source")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ch, err := New(Config{URL: srv.URL}, WithHeaders(map[string]string{
		"Authorization":     "Bearer abc",
		"X-Tickraft-Source": "prism",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if err := ch.Send(context.Background(), sampleAlert()); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer abc" {
		t.Errorf("Authorization: got %q", gotAuth)
	}
	if gotCustom != "prism" {
		t.Errorf("X-Tickraft-Source: got %q", gotCustom)
	}
}

// TestSendContentTypeOverride verifies that a custom Content-Type header
// overrides the default application/json.
func TestSendContentTypeOverride(t *testing.T) {
	var gotCT string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCT = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ch, err := New(Config{URL: srv.URL}, WithHeaders(map[string]string{
		"Content-Type": "application/cloudevents+json",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if err := ch.Send(context.Background(), sampleAlert()); err != nil {
		t.Fatal(err)
	}
	if gotCT != "application/cloudevents+json" {
		t.Errorf("Content-Type: got %q, want override", gotCT)
	}
}

// TestSendNetworkError verifies that a network error is retryable and
// ultimately fails with a retryable SendError.
func TestSendNetworkError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	addr := srv.URL
	srv.Close() // close the listener so connections are refused

	ch, err := New(Config{URL: addr}, WithRetry(2, time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	se := asSendError(t, ch.Send(context.Background(), sampleAlert()))
	if !se.Retryable {
		t.Error("Retryable: got false, want true for network error")
	}
}

// ---------------------------------------------------------------------------
// Send: empty URL guard
// ---------------------------------------------------------------------------

// TestSendEmptyURL verifies that Send returns an error when the channel
// was constructed without a URL (defensive guard for direct construction).
func TestSendEmptyURL(t *testing.T) {
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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	ch, err := New(Config{URL: srv.URL},
		WithRetry(1, time.Millisecond),
		WithCircuitBreaker(2, time.Hour),
	)
	if err != nil {
		t.Fatal(err)
	}

	// Two failed sends open the breaker (threshold 2).
	for i := 0; i < 2; i++ {
		if err := ch.Send(context.Background(), sampleAlert()); err == nil {
			t.Fatalf("send #%d: expected error", i+1)
		}
	}
	callsAtOpen := calls.Load()

	// Third send is short-circuited by the open breaker.
	err = ch.Send(context.Background(), sampleAlert())
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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if fail.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ch, err := New(Config{URL: srv.URL},
		WithRetry(1, time.Millisecond),
		WithCircuitBreaker(2, 50*time.Millisecond),
	)
	if err != nil {
		t.Fatal(err)
	}

	// Open the breaker with two failures.
	fail.Store(true)
	for i := 0; i < 2; i++ {
		if err := ch.Send(context.Background(), sampleAlert()); err == nil {
			t.Fatal("expected failure")
		}
	}

	// Breaker is now open.
	if err := ch.Send(context.Background(), sampleAlert()); !errors.Is(err, channel.ErrCircuitOpen) {
		t.Fatalf("expected ErrCircuitOpen, got %v", err)
	}

	// Wait for cooldown, then make the server succeed.
	time.Sleep(70 * time.Millisecond)
	fail.Store(false)

	// The next send is admitted as a half-open probe and succeeds,
	// closing the breaker.
	if err := ch.Send(context.Background(), sampleAlert()); err != nil {
		t.Fatalf("half-open send should succeed, got %v", err)
	}

	// Subsequent sends should succeed normally without circuit errors.
	if err := ch.Send(context.Background(), sampleAlert()); err != nil {
		t.Fatalf("post-recovery send should succeed, got %v", err)
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
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ch, err := New(Config{URL: srv.URL},
		WithRetry(1, time.Millisecond),
		WithCircuitBreaker(3, time.Hour),
	)
	if err != nil {
		t.Fatal(err)
	}

	// Two failures (below threshold 3).
	fail.Store(true)
	for i := 0; i < 2; i++ {
		_ = ch.Send(context.Background(), sampleAlert())
	}

	// Success resets the counter.
	fail.Store(false)
	if err := ch.Send(context.Background(), sampleAlert()); err != nil {
		t.Fatalf("success send: %v", err)
	}

	// Two more failures should not open the breaker (counter was reset).
	fail.Store(true)
	for i := 0; i < 2; i++ {
		_ = ch.Send(context.Background(), sampleAlert())
	}

	// Next send should be admitted (breaker still closed), not ErrCircuitOpen.
	err = ch.Send(context.Background(), sampleAlert())
	if errors.Is(err, channel.ErrCircuitOpen) {
		t.Fatal("breaker should still be closed after reset + 2 failures")
	}
}

// ---------------------------------------------------------------------------
// httpError & isRetryableSendErr
// ---------------------------------------------------------------------------

// TestHTTPErrorErrorString verifies the Error() output for both the
// status-code and network-error branches.
func TestHTTPErrorErrorString(t *testing.T) {
	withStatus := &httpError{statusCode: 503, err: errors.New("upstream down")}
	if got := withStatus.Error(); got != "webhook: status 503: upstream down" {
		t.Errorf("Error(): got %q", got)
	}
	networkErr := &httpError{statusCode: 0, err: errors.New("connection refused")}
	if got := networkErr.Error(); got != "webhook: connection refused" {
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
// isHTTPURL
// ---------------------------------------------------------------------------

// TestIsHTTPURL verifies the URL scheme check helper.
func TestIsHTTPURL(t *testing.T) {
	for _, s := range []string{"http://x", "https://x", "HTTP://X"} {
		if !isHTTPURL(s) {
			t.Errorf("isHTTPURL(%q): got false, want true", s)
		}
	}
	for _, s := range []string{"ftp://x", "x", ""} {
		if isHTTPURL(s) {
			t.Errorf("isHTTPURL(%q): got true, want false", s)
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
	if c.Timeout != defaultTimeout {
		t.Errorf("Timeout: got %v", c.Timeout)
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
		Timeout:                 1 * time.Second,
		RetryMaxAttempts:        7,
		RetryBaseInterval:       2 * time.Second,
		CircuitFailureThreshold: 9,
		CircuitCooldown:         3 * time.Second,
	}
	applyDefaults(&c2)
	if c2.Timeout != 1*time.Second || c2.RetryMaxAttempts != 7 ||
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
func TestImplementsAlertChannel(t *testing.T) {
	var _ alert.Channel = (*Channel)(nil)
	ch, err := New(Config{URL: "http://example.com"})
	if err != nil {
		t.Fatal(err)
	}
	var asChannel alert.Channel = ch
	if asChannel.Name() != "webhook" {
		t.Errorf("Name via interface: got %q", asChannel.Name())
	}
}
