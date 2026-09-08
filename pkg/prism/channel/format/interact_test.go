// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package format

import (
	"reflect"
	"testing"

	"github.com/tickraft/tickraft/pkg/prism/alert"
)

func buttonsFor(evt alert.Event) []ActionButton {
	return []ActionButton{
		{Action: ActionAcknowledge, Label: "Acknowledge"},
		{Action: ActionResolve, Label: "Resolve"},
	}
}

// TestInteractionButtonsForDisabled verifies that a disabled policy never
// produces buttons.
func TestInteractionButtonsForDisabled(t *testing.T) {
	o := InteractionOptions{Enabled: false, Buttons: buttonsFor}
	if got := o.ButtonsFor(alert.Event{EventID: "evt-1"}); got != nil {
		t.Errorf("disabled policy: got %v, want nil", got)
	}
}

// TestInteractionButtonsForNoProvider verifies that a missing button
// provider behaves as "always no buttons".
func TestInteractionButtonsForNoProvider(t *testing.T) {
	o := InteractionOptions{Enabled: true}
	if got := o.ButtonsFor(alert.Event{EventID: "evt-1"}); got != nil {
		t.Errorf("no provider: got %v, want nil", got)
	}
}

// TestInteractionButtonsForNoEventID verifies that events without an
// EventID (whose callbacks could not locate records) produce no buttons.
func TestInteractionButtonsForNoEventID(t *testing.T) {
	o := InteractionOptions{Enabled: true, Buttons: buttonsFor}
	if got := o.ButtonsFor(alert.Event{}); got != nil {
		t.Errorf("no event id: got %v, want nil", got)
	}
}

// TestInteractionButtonsForPassesProvider verifies the provider output is
// returned verbatim for a qualifying event.
func TestInteractionButtonsForPassesProvider(t *testing.T) {
	o := InteractionOptions{Enabled: true, Buttons: buttonsFor}
	want := buttonsFor(alert.Event{})
	got := o.ButtonsFor(alert.Event{EventID: "evt-1"})
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}
