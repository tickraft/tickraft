// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package alert

import "context"

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
	// List returns a page of alert records ordered by descending ID, plus
	// the total count. page starts at 1; size is the maximum number of items.
	List(ctx context.Context, page, size int) ([]*Record, int64, error)
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
