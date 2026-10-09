// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package alert

import (
	"context"
	"time"
)

// RecordFilter holds optional server-side filtering criteria for listing
// alert records. A zero-value filter matches all records.
type RecordFilter struct {
	// Severity filters by an exact severity match (info/warning/critical).
	Severity string
	// Status filters by an exact status match (firing/acknowledged/resolved).
	Status string
	// From restricts the result to records triggered at or after this time.
	From time.Time
	// To restricts the result to records triggered at or before this time.
	To time.Time
}

// RecordStore defines the persistence operations for alert records.
// Implementations must be safe for concurrent use.
//
// The interface lives in the alert domain because record persistence is an
// alert concern. The GORM-backed implementation lives in this package
// (store.go, see NewRecordStore).
type RecordStore interface {
	// Create inserts a new alert record. The ID and CreatedAt are populated
	// by the database on success.
	Create(ctx context.Context, m *Record) error
	// CreateBatch inserts multiple alert records in a single DB round-trip.
	// IDs and CreatedAt are populated by the database on success.
	CreateBatch(ctx context.Context, models []*Record) error
	// GetByID retrieves an alert record by its ID. Returns errdefs.ErrNotFound
	// when no record with the given ID exists.
	GetByID(ctx context.Context, id int64) (*Record, error)
	// FindByEventID returns all alert records sharing the dispatch-assigned
	// event ID, ordered by ascending ID. An empty eventID returns no rows.
	FindByEventID(ctx context.Context, eventID string) ([]*Record, error)
	// List returns a page of alert records matching the filter, ordered by
	// descending ID, plus the total count. page starts at 1; size is the
	// maximum number of items. A zero-value filter returns all records.
	List(ctx context.Context, page, size int, filter RecordFilter) ([]*Record, int64, error)
	// Acknowledge transitions the alert record identified by id to the
	// "acknowledged" status and sets acknowledged_at to the current time.
	// It returns the updated record. Returns errdefs.ErrNotFound when no
	// record with the given ID exists.
	Acknowledge(ctx context.Context, id int64) (*Record, error)
	// Resolve transitions the alert record identified by id to the
	// "resolved" status and sets resolved_at to the current time. It
	// returns the updated record. Returns errdefs.ErrNotFound when no
	// record with the given ID exists.
	Resolve(ctx context.Context, id int64) (*Record, error)
}

// Lister defines the rule listing operation the Engine needs to reload
// its rule set. It is the consumer-side port of the Store (see
// store.go): the engine depends on this narrow interface rather
// than the full persistence layer, per
type Lister interface {
	// ListEnabled returns enabled rules, ordered by priority (descending)
	// then ID (ascending). A zero tenantID returns rules across all
	// tenants; per-event tenant filtering happens at evaluation time.
	ListEnabled(ctx context.Context, tenantID int64) ([]Rule, error)
}

// Service defines the operations for managing alert rules and records.
// The wire and storage shapes are the same model: there are no Rule/Record
// DTOs. Rule and Record hold both gorm and json tags, and internal columns
// (TenantID, DeletedAt) serialize to nothing, so handlers bind and return
// the model types directly. See docs/model-layering-design.md for the
// layering contract.
type Service interface {
	// ListRules returns a page of alert rules and the total count.
	ListRules(ctx context.Context, page, size int) ([]*Rule, int64, error)
	// GetRule returns a single alert rule by ID.
	GetRule(ctx context.Context, id int64) (*Rule, error)
	// CreateRule creates a new alert rule from the given request.
	CreateRule(ctx context.Context, req *Rule) (*Rule, error)
	// UpdateRule updates an existing alert rule identified by ID.
	UpdateRule(ctx context.Context, id int64, req *Rule) (*Rule, error)
	// DeleteRule deletes an alert rule by ID.
	DeleteRule(ctx context.Context, id int64) error
	// ListRecords returns a page of alert records matching the filter and
	// the total count.
	ListRecords(ctx context.Context, page, size int, filter RecordFilter) ([]*Record, int64, error)
	// GetRecord returns a single alert record by ID.
	GetRecord(ctx context.Context, id int64) (*Record, error)
	// AcknowledgeRecord transitions the alert record identified by ID to the
	// "acknowledged" status and sets acknowledged_at to the current time.
	// It returns the updated record. Returns ErrRecordNotFound when no
	// record with the given ID exists.
	AcknowledgeRecord(ctx context.Context, id int64) (*Record, error)
	// ResolveRecord transitions the alert record identified by ID to the
	// "resolved" status and sets resolved_at to the current time. It
	// returns the updated record. Returns ErrRecordNotFound when no
	// record with the given ID exists.
	ResolveRecord(ctx context.Context, id int64) (*Record, error)
}
