// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package format

import (
	"github.com/tickraft/tickraft/pkg/prism/alert"
)

// Interaction action verbs carried by card callback buttons. The verb is
// the stable cross-platform contract: channel adapters embed it in the
// button value/key, and the inbound callback dispatcher maps it back onto
// the alert lifecycle. Editions may extend the set; unknown verbs are
// ignored by the dispatcher.
const (
	// ActionAcknowledge claims the alert (firing -> acknowledged).
	ActionAcknowledge = "acknowledge"
	// ActionResolve marks the alert resolved.
	ActionResolve = "resolve"
	// ActionSilence opens a maintenance window for the alert's rule.
	ActionSilence = "silence"
)

// ActionButton is one interactive action rendered on an alert card. Action
// is the lifecycle verb (ActionAcknowledge/ActionResolve/ActionSilence or
// an edition-specific one); Label is the button text in the recipient's
// locale, chosen by the button provider.
type ActionButton struct {
	Action string
	Label  string
}

// InteractionOptions carries the deployment-wide interactive-card policy
// (M5 L1 inbound events). The kernel defines the seam only: when Enabled
// is true and Buttons is non-nil, interactive-capable channel adapters
// render callback buttons (whose values carry the action verb and the
// alert's EventID) instead of link-only cards. Deployments that keep the
// zero value render plain cards exactly as before. The seam has no
// licensing notion; editions decide whether to enable it and inject the
// button set.
type InteractionOptions struct {
	// Enabled turns on interactive card rendering. When false no adapter
	// renders callback buttons regardless of Buttons.
	Enabled bool
	// Buttons produces the action buttons for one alert event. It receives
	// the event so providers can localize labels via evt.Locale and skip
	// buttons for events that cannot act (for example an empty EventID).
	// A nil Buttons behaves as "always no buttons".
	Buttons func(evt alert.Event) []ActionButton
}

// ButtonsFor returns the action buttons for evt under the policy. It
// reports nil when interaction is disabled, the provider is absent, or
// the event carries no EventID (the callback could not locate records).
func (o InteractionOptions) ButtonsFor(evt alert.Event) []ActionButton {
	if !o.Enabled || o.Buttons == nil || evt.EventID == "" {
		return nil
	}
	return o.Buttons(evt)
}
