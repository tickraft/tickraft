// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package channel

import (
	"encoding/json"
	"time"
)

// Channel business error codes (20300-20399).
const (
	CodeNotFound      = 20300
	CodeNameEmpty     = 20301
	CodeTypeInvalid   = 20302
	CodeConfigInvalid = 20303
	CodeTestFailed    = 20304
	CodeNotRetryable  = 20305
)

// CreateRequest is the request body for the create-channel endpoint.
type CreateRequest struct {
	Name    string          `json:"name"`
	Type    string          `json:"type"`
	Config  json.RawMessage `json:"config"`
	Enabled *bool           `json:"enabled"`
}

// UpdateRequest is the request body for the update-channel endpoint. All
// fields are optional; omitted fields retain their previous values.
type UpdateRequest struct {
	Name    string          `json:"name"`
	Type    string          `json:"type"`
	Config  json.RawMessage `json:"config"`
	Enabled *bool           `json:"enabled"`
}

// TestRequest is the request body for the test-channel endpoint. It
// supports two modes: referencing an already-saved channel by ID, or
// supplying an inline type+config pair.
type TestRequest struct {
	ID     *int64          `json:"id,omitempty"`
	Type   string          `json:"type,omitempty"`
	Config json.RawMessage `json:"config,omitempty"`
}

// TestResult reports the outcome of a single channel probe issued by
// TestAllChannels. DurationMs and Error together give operators the
// per-channel connectivity evidence (private-deployment network
// diagnostics).
type TestResult struct {
	// ChannelID is the probed channel configuration's id.
	ChannelID int64 `json:"channel_id"`
	// Name is the channel configuration's display name.
	Name string `json:"name"`
	// Type is the channel type (feishu, email, ...).
	Type string `json:"type"`
	// OK reports whether the probe notification was delivered.
	OK bool `json:"ok"`
	// Error is the failure reason when OK is false: either a channel
	// build failure or the send error. Empty on success.
	Error string `json:"error,omitempty"`
	// DurationMs is the wall-clock send duration in milliseconds. It is
	// zero when the channel could not be built at all.
	DurationMs int64 `json:"duration_ms"`
}

// DeliveryListParams holds the filters and pagination parameters for
// listing delivery records.
type DeliveryListParams struct {
	// ChannelID restricts results to the given channel configuration.
	// Zero lists records across all channels.
	ChannelID int64
	// Status restricts results to the given delivery status.
	Status string
	// AlertTitle restricts results with a case-insensitive substring
	// match on the alert title.
	AlertTitle string
	// StartTime restricts results to records sent at or after this time.
	StartTime *time.Time
	// EndTime restricts results to records sent at or before this time.
	EndTime *time.Time
	// Page is the 1-based page number.
	Page int
	// Size is the number of records per page.
	Size int
	// Cursor is the opaque next-page token for keyset (cursor-based)
	// pagination. When the caller invokes the keyset list method it is
	// forwarded as PageRequest.Cursor; an empty value means the first
	// page. It is ignored by the offset-mode List method.
	Cursor string
}

// Option is the compact projection returned by the options
// endpoint, used to populate filter dropdowns without loading full
// configurations.
type Option struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Enabled bool   `json:"enabled"`
}
