// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package channel

import (
	"errors"
	"fmt"
	"testing"
)

// ---------------------------------------------------------------------------
// SendError: construction
// ---------------------------------------------------------------------------

// TestNewSendError verifies that NewSendError populates all fields.
func TestNewSendError(t *testing.T) {
	inner := fmt.Errorf("boom")
	se := NewSendError("webhook", true, inner)
	if se == nil {
		t.Fatal("expected non-nil SendError")
	}
	if se.ChannelName != "webhook" {
		t.Errorf("ChannelName: got %q, want %q", se.ChannelName, "webhook")
	}
	if !se.Retryable {
		t.Errorf("Retryable: got false, want true")
	}
	if se.Err != inner {
		t.Errorf("Err: got %v, want %v", se.Err, inner)
	}
}

// TestNewSendErrorWithNilErr verifies that NewSendError tolerates a nil
// inner error without panicking.
func TestNewSendErrorWithNilErr(t *testing.T) {
	se := NewSendError("email", false, nil)
	if se == nil {
		t.Fatal("expected non-nil SendError")
	}
	if se.Err != nil {
		t.Errorf("Err: got %v, want nil", se.Err)
	}
}

// ---------------------------------------------------------------------------
// SendError: Error() output
// ---------------------------------------------------------------------------

// TestSendErrorErrorString verifies the Error() output format.
func TestSendErrorErrorString(t *testing.T) {
	se := NewSendError("webhook", true, fmt.Errorf("connection refused"))
	want := "channel webhook: connection refused"
	if got := se.Error(); got != want {
		t.Errorf("Error(): got %q, want %q", got, want)
	}
}

// TestSendErrorErrorWithNilErr verifies the Error() output when the inner
// error is nil. fmt's %v verb renders a nil error as "<nil>".
func TestSendErrorErrorWithNilErr(t *testing.T) {
	se := NewSendError("email", false, nil)
	want := "channel email: <nil>"
	if got := se.Error(); got != want {
		t.Errorf("Error(): got %q, want %q", got, want)
	}
}

// ---------------------------------------------------------------------------
// SendError: Unwrap()
// ---------------------------------------------------------------------------

// TestSendErrorUnwrap verifies that Unwrap returns the inner error.
func TestSendErrorUnwrap(t *testing.T) {
	inner := fmt.Errorf("timeout")
	se := NewSendError("webhook", true, inner)
	if unwrapped := se.Unwrap(); unwrapped != inner {
		t.Errorf("Unwrap(): got %v, want %v", unwrapped, inner)
	}
}

// ---------------------------------------------------------------------------
// SendError: errors.Is / errors.As compatibility
// ---------------------------------------------------------------------------

// errSentinel is a sentinel error used to verify errors.Is traversal.
var errSentinel = errors.New("sentinel failure")

// TestSendErrorErrorsIs verifies that errors.Is traverses into SendError.
func TestSendErrorErrorsIs(t *testing.T) {
	se := NewSendError("webhook", true, errSentinel)
	if !errors.Is(se, errSentinel) {
		t.Errorf("errors.Is(se, errSentinel): got false, want true")
	}
}

// TestSendErrorErrorsIsWithUnrelated verifies that errors.Is returns false
// for an unrelated error.
func TestSendErrorErrorsIsWithUnrelated(t *testing.T) {
	se := NewSendError("webhook", true, errSentinel)
	other := errors.New("other")
	if errors.Is(se, other) {
		t.Errorf("errors.Is(se, other): got true, want false")
	}
}

// TestSendErrorErrorsAs verifies that errors.As can extract a SendError
// from a wrapped error chain.
func TestSendErrorErrorsAs(t *testing.T) {
	inner := fmt.Errorf("rate limited")
	se := NewSendError("dingtalk", true, inner)
	// Wrap once more to simulate a deeper chain.
	wrapped := fmt.Errorf("dispatch failed: %w", se)

	var target *SendError
	if !errors.As(wrapped, &target) {
		t.Fatal("errors.As: expected to find *SendError in chain")
	}
	if target.ChannelName != "dingtalk" {
		t.Errorf("ChannelName: got %q, want %q", target.ChannelName, "dingtalk")
	}
	if !target.Retryable {
		t.Errorf("Retryable: got false, want true")
	}
	if target.Err != inner {
		t.Errorf("Err: got %v, want %v", target.Err, inner)
	}
}

// TestSendErrorErrorsAsDirect verifies that errors.As works when SendError
// is the top-level error.
func TestSendErrorErrorsAsDirect(t *testing.T) {
	se := NewSendError("sms", false, fmt.Errorf("limit exceeded"))

	var target *SendError
	if !errors.As(se, &target) {
		t.Fatal("errors.As: expected to find *SendError")
	}
	if target.ChannelName != "sms" {
		t.Errorf("ChannelName: got %q, want %q", target.ChannelName, "sms")
	}
	if target.Retryable {
		t.Errorf("Retryable: got true, want false")
	}
}

// ---------------------------------------------------------------------------
// ErrCircuitOpen sentinel
// ---------------------------------------------------------------------------

// TestErrCircuitOpenIsSentinel verifies that ErrCircuitOpen is a stable
// sentinel usable with errors.Is.
func TestErrCircuitOpenIsSentinel(t *testing.T) {
	if !errors.Is(ErrCircuitOpen, ErrCircuitOpen) {
		t.Fatal("errors.Is(ErrCircuitOpen, ErrCircuitOpen): got false, want true")
	}

	wrapped := fmt.Errorf("send suppressed: %w", ErrCircuitOpen)
	if !errors.Is(wrapped, ErrCircuitOpen) {
		t.Fatal("errors.Is(wrapped, ErrCircuitOpen): got false, want true")
	}
}

// TestErrCircuitOpenInsideSendError verifies that a SendError wrapping
// ErrCircuitOpen is still detectable via errors.Is.
func TestErrCircuitOpenInsideSendError(t *testing.T) {
	se := NewSendError("webhook", false, ErrCircuitOpen)
	if !errors.Is(se, ErrCircuitOpen) {
		t.Fatal("errors.Is(se, ErrCircuitOpen): got false, want true")
	}
}
