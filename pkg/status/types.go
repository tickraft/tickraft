// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package status

import (
	"errors"
	"time"
)

// Public component status vocabulary of the status page. The values are
// wire identifiers shared with the frontend and rendered as-is.
const (
	// StatusOperational marks a component whose monitors all probe normal.
	StatusOperational = "operational"
	// StatusDegraded marks a monitor point whose runtime state is error or
	// whose latest probe reports an abnormal result.
	StatusDegraded = "degraded"
	// StatusPartialOutage marks a component with a mix of healthy and
	// failing monitors.
	StatusPartialOutage = "partial_outage"
	// StatusMajorOutage marks a component whose monitors all fail.
	StatusMajorOutage = "major_outage"
	// StatusUnknown marks a component without any probe data (inactive
	// points, passive points without a derived state, or an empty
	// component map).
	StatusUnknown = "unknown"
)

// ErrDisabled is returned by Service.PublicView when the status page is
// not enabled in the configuration. The HTTP layer maps it to 404 so an
// unpublished page is indistinguishable from a missing route.
var ErrDisabled = errors.New("status: page disabled")

// ErrValidation marks a configuration payload that fails validation. The
// message is safe to return to the client verbatim.
type ErrValidation struct{ Msg string }

func (e *ErrValidation) Error() string { return "status: " + e.Msg }

// Component maps a status page entry onto monitor points. PointIDs
// references monitor point IDs; when a component has no IDs it renders as
// StatusUnknown (a placeholder entry the operator has not wired up yet).
type Component struct {
	// Name is the display name of the component.
	Name string `json:"name"`
	// Description is an optional human-readable description.
	Description string `json:"description,omitempty"`
	// PointIDs lists the monitor points aggregated into this component.
	PointIDs []int64 `json:"point_ids,omitempty"`
}

// Config is the status page configuration (the wire shape of the
// sys_status_config singleton row).
type Config struct {
	// Title is the page title.
	Title string `json:"title"`
	// Description is an optional page subtitle.
	Description string `json:"description,omitempty"`
	// Enabled controls whether the public page is served. It defaults to
	// false so the page stays private until an operator publishes it.
	Enabled bool `json:"enabled"`
	// Components maps page entries onto monitor points. When empty the
	// service derives one component per enabled monitor point.
	Components []Component `json:"components,omitempty"`
}

// ComponentStatus is the rendered state of one component in the public
// view.
type ComponentStatus struct {
	// Name is the display name of the component.
	Name string `json:"name"`
	// Description is the optional component description.
	Description string `json:"description,omitempty"`
	// Status is the component status vocabulary value.
	Status string `json:"status"`
	// MonitorCount is the number of monitor points aggregated into the
	// component (0 for placeholder components).
	MonitorCount int `json:"monitor_count"`
	// LastChecked is the most recent probe time across the component's
	// monitor points (zero when no probe exists).
	LastChecked time.Time `json:"last_checked,omitempty"`
}

// PublicView is the rendered status page served by GET /api/v1/status.
type PublicView struct {
	// Title is the configured page title.
	Title string `json:"title"`
	// Description is the configured page subtitle.
	Description string `json:"description,omitempty"`
	// Overall is the worst non-unknown component status.
	Overall string `json:"overall"`
	// Components lists the rendered component states in configuration
	// order.
	Components []ComponentStatus `json:"components"`
	// UpdatedAt is the render time of this view.
	UpdatedAt time.Time `json:"updated_at"`
}
