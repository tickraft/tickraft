// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package event

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/tickraft/tickraft/pkg/db/errmap"
	"gorm.io/gorm"
)

// FailedEvent is the persistence model for the event_failed_events table. It
// persists event envelopes that exhausted all delivery retries so they can
// be audited and potentially replayed.
type FailedEvent struct {
	// ID is the unique identifier of the failed event record.
	ID int64 `gorm:"primaryKey;autoIncrement" json:"id"`
	// EventID is the unique identifier from the original event envelope.
	EventID string `gorm:"size:128;index" json:"event_id"`
	// EventType is the type of the failed event.
	EventType string `gorm:"size:64;index" json:"event_type"`
	// TenantID is the tenant identifier from the original envelope.
	TenantID string `gorm:"size:64;index" json:"tenant_id"`
	// Payload is the JSON-encoded event payload.
	Payload string `gorm:"type:text" json:"payload"`
	// Error is the last error message that caused delivery to fail.
	Error string `gorm:"type:text" json:"error"`
	// Retries is the number of retry attempts before the event was persisted.
	Retries int `json:"retries"`
	// CreatedAt is the time the failed event was persisted.
	CreatedAt time.Time `gorm:"autoCreateTime;index" json:"created_at"`
}

// TableName returns the database table name for FailedEvent.
func (FailedEvent) TableName() string { return "event_failed_events" }

// failedEventStore implements FailedEventStore using a database
// connection. It persists failed event envelopes so they survive process
// restarts and can be audited or replayed by operators.
type failedEventStore struct {
	dbc *gorm.DB
}

// NewFailedEventStore creates a failedEventStore backed by the given
// database. The caller is responsible for running Migrate before first use.
func NewFailedEventStore(dbc *gorm.DB) *failedEventStore {
	return &failedEventStore{dbc: dbc}
}

// Migrate creates or updates the event_failed_events table.
func (s *failedEventStore) Migrate(ctx context.Context) error {
	if err := s.dbc.WithContext(ctx).AutoMigrate(&FailedEvent{}); err != nil {
		return fmt.Errorf("event: migrate failed_events table: %w", err)
	}
	return nil
}

// Save persists the failed event envelope and its error to the database.
// The envelope payload is JSON-encoded; unencodable payloads are stored as
// their fmt-formatted string representation so no failed event is ever lost.
func (s *failedEventStore) Save(ctx context.Context, env Envelope, err error) error {
	payloadBytes, mErr := json.Marshal(env.Payload)
	payloadStr := string(payloadBytes)
	if mErr != nil {
		payloadStr = fmt.Sprintf("%v", env.Payload)
	}

	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	}

	record := &FailedEvent{
		EventID:   env.EventID,
		EventType: string(env.Type),
		TenantID:  env.TenantID,
		Payload:   payloadStr,
		Error:     errMsg,
	}

	if createErr := s.dbc.WithContext(ctx).Create(record).Error; createErr != nil {
		return fmt.Errorf("event: save failed event: %w", errmap.MapError(createErr))
	}
	return nil
}

// Compile-time assertion that failedEventStore satisfies FailedEventStore.
var _ FailedEventStore = (*failedEventStore)(nil)
