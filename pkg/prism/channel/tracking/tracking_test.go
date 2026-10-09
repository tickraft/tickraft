// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package tracking

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/bytedance/sonic"

	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/prism/alert"

	"go.uber.org/zap/zaptest/observer"

	"github.com/tickraft/tickraft/pkg/prism/channel/format"
)

// ---------------------------------------------------------------------------
// Test doubles
// ---------------------------------------------------------------------------

// fakeChannel is a minimal alert.Channel implementation whose Send
// result is fully controllable from a test.
type fakeChannel struct {
	name string
	err  error
}

func (f *fakeChannel) Name() string { return f.name }

func (f *fakeChannel) Send(_ context.Context, _ alert.Event) error {
	return f.err
}

// mockStore is a DeliveryRecordStore that captures every recorded
// DeliveryRecord. Setting err causes Record to return that error
// instead of nil, simulating a store failure.
type mockStore struct {
	records []DeliveryRecord
	err     error
}

func newMockStore() *mockStore {
	return &mockStore{}
}

func (m *mockStore) Record(_ context.Context, rec DeliveryRecord) error {
	m.records = append(m.records, rec)
	return m.err
}

// slackIdentity is the channel identity threaded through every test
// construction in this file.
var slackIdentity = Identity{ChannelID: 7, ChannelName: "slack", ChannelType: "slack"}

// sampleAlert returns a representative alert used across tests.
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

// ---------------------------------------------------------------------------
// New
// ---------------------------------------------------------------------------

// TestNew_NilLogger asserts that constructing the decorator with a nil
// logger does not panic and yields a usable channel.
func TestNew_NilLogger(t *testing.T) {
	t.Parallel()

	store := newMockStore()
	ch := New(&fakeChannel{name: "slack"}, store, nil, slackIdentity, format.RenderOptions{})
	if ch == nil {
		t.Fatal("New returned nil")
	}
	// Driving the decorator confirms the no-op logger is wired up and
	// does not panic on the Warn path when the store fails.
	store.err = errors.New("disk full")
	if err := ch.Send(context.Background(), sampleAlert()); err != nil {
		t.Fatalf("Send: got %v, want nil", err)
	}
}

// TestNew_WithLogger asserts that a non-nil logger is retained and used.
func TestNew_WithLogger(t *testing.T) {
	t.Parallel()

	core, recorded := observer.New(zap.WarnLevel)
	logger := zap.New(core)

	store := newMockStore()
	store.err = errors.New("disk full")
	ch := New(&fakeChannel{name: "slack"}, store, logger, slackIdentity, format.RenderOptions{})

	_ = ch.Send(context.Background(), sampleAlert())

	if observed := recorded.FilterMessage("failed to record delivery").All(); len(observed) != 1 {
		t.Fatalf("warning log: got %d entries, want 1", len(observed))
	}
}

// ---------------------------------------------------------------------------
// Name
// ---------------------------------------------------------------------------

// TestName_Passthrough asserts that Name delegates to the wrapped
// channel.
func TestName_Passthrough(t *testing.T) {
	t.Parallel()

	const want = "wecom"
	ch := New(&fakeChannel{name: want}, newMockStore(), nil, slackIdentity, format.RenderOptions{})
	if got := ch.Name(); got != want {
		t.Errorf("Name: got %q, want %q", got, want)
	}
}

// ---------------------------------------------------------------------------
// Send: success path
// ---------------------------------------------------------------------------

// TestSend_Success asserts that when the wrapped channel succeeds the
// decorator records a StatusSuccess record and returns nil.
func TestSend_Success(t *testing.T) {
	t.Parallel()

	store := newMockStore()
	ch := New(&fakeChannel{name: "slack", err: nil}, store, nil, slackIdentity, format.RenderOptions{})

	evt := sampleAlert()
	if err := ch.Send(context.Background(), evt); err != nil {
		t.Fatalf("Send: got %v, want nil", err)
	}

	if len(store.records) != 1 {
		t.Fatalf("recorded: got %d, want 1", len(store.records))
	}
	rec := store.records[0]
	if rec.ChannelName != "slack" {
		t.Errorf("ChannelName: got %q, want %q", rec.ChannelName, "slack")
	}
	if rec.AlertType != string(evt.Type) {
		t.Errorf("AlertType: got %q, want %q", rec.AlertType, string(evt.Type))
	}
	if rec.Status != StatusSuccess {
		t.Errorf("Status: got %q, want %q", rec.Status, StatusSuccess)
	}
	if rec.Error != "" {
		t.Errorf("Error: got %q, want empty", rec.Error)
	}
	if rec.SentAt.IsZero() {
		t.Error("SentAt: got zero, want non-zero")
	}
}

// ---------------------------------------------------------------------------
// Send: failure path
// ---------------------------------------------------------------------------

// TestSend_Failure asserts that when the wrapped channel fails the
// decorator records a StatusFailed record carrying the error message and
// returns the same error.
func TestSend_Failure(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("webhook unreachable")
	store := newMockStore()
	ch := New(&fakeChannel{name: "slack", err: wantErr}, store, nil, slackIdentity, format.RenderOptions{})

	evt := sampleAlert()
	err := ch.Send(context.Background(), evt)
	if !errors.Is(err, wantErr) {
		t.Fatalf("Send: got %v, want %v", err, wantErr)
	}

	if len(store.records) != 1 {
		t.Fatalf("recorded: got %d, want 1", len(store.records))
	}
	rec := store.records[0]
	if rec.Status != StatusFailed {
		t.Errorf("Status: got %q, want %q", rec.Status, StatusFailed)
	}
	if rec.Error != wantErr.Error() {
		t.Errorf("Error: got %q, want %q", rec.Error, wantErr.Error())
	}
}

// ---------------------------------------------------------------------------
// Send: store failure
// ---------------------------------------------------------------------------

// TestSend_StoreFailure asserts that a store error is logged but never
// propagated: Send still returns the wrapped channel's result.
func TestSend_StoreFailure(t *testing.T) {
	t.Parallel()

	storeErr := errors.New("disk full")

	t.Run("inner_success_store_fails", func(t *testing.T) {
		t.Parallel()

		core, recorded := observer.New(zap.WarnLevel)
		store := newMockStore()
		store.err = storeErr
		ch := New(&fakeChannel{name: "slack", err: nil}, store, zap.New(core), slackIdentity, format.RenderOptions{})

		if err := ch.Send(context.Background(), sampleAlert()); err != nil {
			t.Fatalf("Send: got %v, want nil", err)
		}
		if got := recorded.FilterMessage("failed to record delivery").Len(); got != 1 {
			t.Errorf("warning logs: got %d, want 1", got)
		}
	})

	t.Run("inner_failure_store_fails", func(t *testing.T) {
		t.Parallel()

		core, recorded := observer.New(zap.WarnLevel)
		wantErr := errors.New("webhook unreachable")
		store := newMockStore()
		store.err = storeErr
		ch := New(&fakeChannel{name: "slack", err: wantErr}, store, zap.New(core),
			slackIdentity, format.RenderOptions{})

		err := ch.Send(context.Background(), sampleAlert())
		if !errors.Is(err, wantErr) {
			t.Fatalf("Send: got %v, want %v", err, wantErr)
		}
		// The store failure must not mask the inner error: the warning
		// is emitted but the returned error is still the inner one.
		if got := recorded.FilterMessage("failed to record delivery").Len(); got != 1 {
			t.Errorf("warning logs: got %d, want 1", got)
		}
	})
}

// ---------------------------------------------------------------------------
// Interface conformance
// ---------------------------------------------------------------------------

// TestChannel_ImplementsPrismChannel is a compile-time guard mirrored
// as a test so regressions surface in go test too.
func TestChannel_ImplementsPrismChannel(t *testing.T) {
	t.Parallel()

	var _ alert.Channel = (*Channel)(nil)
}

// ---------------------------------------------------------------------------
// Identity and response-code propagation
// ---------------------------------------------------------------------------

// TestSend_IdentityThreading asserts the record carries the injected
// identity rather than the inner channel's type name.
func TestSend_IdentityThreading(t *testing.T) {
	t.Parallel()

	store := newMockStore()
	id := Identity{ChannelID: 42, ChannelName: "ops-room", ChannelType: "feishu"}
	ch := New(&fakeChannel{name: "feishu"}, store, nil, id, format.RenderOptions{})

	if err := ch.Send(context.Background(), sampleAlert()); err != nil {
		t.Fatalf("Send: %v", err)
	}
	rec := store.records[0]
	if rec.ChannelID != 42 {
		t.Errorf("ChannelID: got %d, want 42", rec.ChannelID)
	}
	if rec.ChannelName != "ops-room" {
		t.Errorf("ChannelName: got %q, want ops-room", rec.ChannelName)
	}
	if rec.ChannelType != "feishu" {
		t.Errorf("ChannelType: got %q, want feishu", rec.ChannelType)
	}
}

// statusCodeError is a test double exposing an HTTP status code the way
// the channel adapters' internal error types do.
type statusCodeError struct {
	code int
}

func (e statusCodeError) Error() string   { return "status 503" }
func (e statusCodeError) StatusCode() int { return e.code }

// TestResponseCodeOf asserts the response-code extraction rules: nil,
// plain errors, and carrier errors.
func TestResponseCodeOf(t *testing.T) {
	t.Parallel()

	if got := ResponseCodeOf(nil); got != 0 {
		t.Errorf("nil: got %d, want 0", got)
	}
	if got := ResponseCodeOf(errors.New("network down")); got != 0 {
		t.Errorf("plain error: got %d, want 0", got)
	}
	if got := ResponseCodeOf(statusCodeError{code: 503}); got != 503 {
		t.Errorf("carrier: got %d, want 503", got)
	}
	// Wrapped carrier errors are unwrapped via errors.As.
	wrapped := fmt.Errorf("send failed: %w", statusCodeError{code: 429})
	if got := ResponseCodeOf(wrapped); got != 429 {
		t.Errorf("wrapped carrier: got %d, want 429", got)
	}
}

// TestSend_ResponseCodeRecorded asserts the failed record carries the
// status code exposed by the channel error.
func TestSend_ResponseCodeRecorded(t *testing.T) {
	t.Parallel()

	store := newMockStore()
	ch := New(&fakeChannel{name: "slack", err: statusCodeError{code: 503}},
		store, nil, slackIdentity, format.RenderOptions{})

	_ = ch.Send(context.Background(), sampleAlert())
	rec := store.records[0]
	if rec.ResponseCode != 503 {
		t.Errorf("ResponseCode: got %d, want 503", rec.ResponseCode)
	}
}

// TestMarshalEvent_RoundTrip asserts the persisted event payload can be
// unmarshaled back into an equivalent alert event for replay.
func TestMarshalEvent_RoundTrip(t *testing.T) {
	t.Parallel()

	evt := sampleAlert()
	evt.EventID = "evt-123"
	raw, err := MarshalEvent(evt)
	if err != nil {
		t.Fatalf("MarshalEvent: %v", err)
	}

	var back alert.Event
	if err := sonic.Unmarshal(raw, &back); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if back.EventID != evt.EventID || back.Type != evt.Type || back.TenantID != evt.TenantID {
		t.Errorf("round trip mismatch: %+v", back)
	}
	if len(back.Violations) != 1 || back.Violations[0].Metric.Name != "cpu_usage" {
		t.Errorf("violations mismatch: %+v", back.Violations)
	}
}
