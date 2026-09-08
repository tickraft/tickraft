// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package channel

import (
	"database/sql/driver"
	"fmt"
	"time"

	"github.com/bytedance/sonic"
)

// Channel is the GORM persistence model for a notification channel
// configuration (sys_prism_channel). The Config field stores a JSON-encoded
// object whose shape is defined by the channel type; sensitive fields
// within that JSON are encrypted with AES-256-GCM before the row is
// persisted and masked in API responses.
type Channel struct {
	// ID is the unique identifier of the channel configuration.
	ID int64 `gorm:"primaryKey;autoIncrement" json:"id"`
	// TenantID is the tenant that owns the configuration. The open-source
	// build is single-tenant and always stores 0; multi-tenant editions
	// scope every query by the tenant resolved from the request context.
	TenantID int64 `gorm:"index;default:0" json:"tenant_id"`
	// Name is the human-readable name of the channel configuration.
	Name string `gorm:"size:255" json:"name"`
	// Type is the channel type (webhook, email, dingtalk, discord, feishu,
	// slack, teams, telegram, wecom, plus types registered via
	// RegisterType).
	Type string `gorm:"size:64;index" json:"type"`
	// Config is a JSON-encoded configuration object. At rest the sensitive
	// fields are encrypted; in-memory representations returned by the
	// store contain decrypted plaintext.
	Config string `gorm:"type:text" json:"config,omitempty"`
	// Enabled indicates whether the channel is active. It is a pointer so
	// that the GORM column default (true) is applied only when the caller
	// leaves the field unset (nil); an explicit false is persisted as
	// false instead of being collapsed to the zero value and replaced by
	// the column default.
	Enabled *bool `gorm:"default:true;index" json:"enabled"`
	// LastUsedAt records the last time the channel successfully delivered
	// a notification. A nil value means the channel has never been used.
	LastUsedAt *time.Time `gorm:"column:last_used_at" json:"last_used_at,omitempty"`
	// CreatedAt is the timestamp when the configuration was created.
	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
	// UpdatedAt is the timestamp when the configuration was last updated.
	UpdatedAt time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

// TableName overrides the default GORM table name for Channel.
func (Channel) TableName() string { return "sys_prism_channel" }

// DeliveryRecord is the GORM model for a single alert delivery attempt,
// persisted to the sys_prism_delivery table.
type DeliveryRecord struct {
	// ID is the unique identifier of the delivery record.
	ID int64 `gorm:"primaryKey;autoIncrement" json:"id"`
	// TenantID is the tenant that owns the delivery record. A value of 0
	// represents a system-level record.
	TenantID int64 `gorm:"index;default:0" json:"tenant_id"`
	// ChannelID is the sys_prism_channel row of the channel that attempted
	// delivery. Zero for environment-built channels.
	ChannelID int64 `gorm:"index;default:0" json:"channel_id"`
	// ChannelName is the display name of the channel configuration that
	// attempted delivery.
	ChannelName string `gorm:"size:128;index" json:"channel_name"`
	// ChannelType is the category of channel (for example "slack").
	ChannelType string `gorm:"size:64" json:"channel_type"`
	// AlertType is the alert category.
	AlertType string `gorm:"size:64" json:"alert_type"`
	// AlertTitle is the rendered alert headline at delivery time. It may
	// be empty for records produced without an injected formatter.
	AlertTitle string `gorm:"size:255;default:''" json:"alert_title"`
	// EventID is the alert event's stable tracking identifier, used to
	// correlate a delivery with the originating dispatch.
	EventID string `gorm:"size:64;default:'';index" json:"event_id"`
	// Status is the delivery outcome: "success" or "failed". It reflects
	// the latest attempt: a manual retry overwrites it.
	Status string `gorm:"size:32;index" json:"status"`
	// Error is the error message returned by the channel on failure.
	Error string `gorm:"type:text" json:"error,omitempty"`
	// ResponseCode is the HTTP status code of the channel response when
	// the channel error exposes one; zero otherwise.
	ResponseCode int `gorm:"default:0" json:"response_code"`
	// DurationMs is the wall-clock duration of the latest attempt.
	DurationMs int64 `gorm:"default:0" json:"duration_ms"`
	// RequestPayload is the serialized alert event that was delivered. It
	// backs the detail drawer and the manual retry replay.
	RequestPayload string `gorm:"type:text" json:"request_payload,omitempty"`
	// Attempts is the per-attempt history: the original send is entry 0
	// and each manual retry appends an entry.
	Attempts Attempts `gorm:"type:text" json:"attempts"`
	// SentAt is the time at which the delivery attempt was recorded.
	SentAt time.Time `gorm:"index" json:"sent_at"`
}

// TableName overrides the default GORM table name for DeliveryRecord.
func (DeliveryRecord) TableName() string { return "sys_prism_delivery" }

// Attempt records a single delivery attempt: the original send is attempt
// 0 and each manual retry appends the next entry.
type Attempt struct {
	// Time is when the attempt started.
	Time time.Time `json:"time"`
	// N is the attempt number; 0 is the original delivery.
	N int `json:"n"`
	// Result is the outcome: "success" or "failed".
	Result string `json:"result"`
	// Error is the channel error message on failure.
	Error string `json:"error,omitempty"`
	// DurationMs is the wall-clock duration of the attempt.
	DurationMs int64 `json:"duration_ms"`
}

// Attempts is a JSON-serialized list of delivery attempts stored in a
// single text column. An empty list serializes as "[]" so the column never
// holds null.
type Attempts []Attempt

// Value implements driver.Valuer by serializing the attempts to JSON.
func (a Attempts) Value() (driver.Value, error) {
	if len(a) == 0 {
		return "[]", nil
	}
	data, err := sonic.Marshal(a)
	if err != nil {
		return nil, err
	}
	return string(data), nil
}

// Scan implements sql.Scanner by parsing the stored JSON into the list.
func (a *Attempts) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*a = nil
		return nil
	case string:
		if v == "" {
			*a = nil
			return nil
		}
		return sonic.UnmarshalString(v, a)
	case []byte:
		if len(v) == 0 {
			*a = nil
			return nil
		}
		return sonic.Unmarshal(v, a)
	default:
		return fmt.Errorf("channel: unsupported attempts column type %T", src)
	}
}
