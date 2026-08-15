// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package timewheel

import (
	"context"
	"sync/atomic"
	"time"
)

// EntryID uniquely identifies a registered entry in the time wheel.
type EntryID int64

var nextEntryID atomic.Int64

func newEntryID() EntryID {
	return EntryID(nextEntryID.Add(1))
}

// Callback is invoked when an entry expires.
type Callback func(entryID EntryID)

// Entry represents a registered item in the time wheel.
type Entry struct {
	// ID is the unique entry identifier.
	ID EntryID
	// Callback is the function to invoke on expiration.
	Callback Callback
	// ExpireAt is the absolute time when the entry should fire.
	ExpireAt time.Time
	// Metadata holds optional user data associated with the entry.
	Metadata interface{}
}

// Wheel is the hierarchical time wheel interface.
type Wheel interface {
	// Add registers a callback to fire after the given duration.
	// Returns the entry ID for later removal or renewal.
	Add(duration time.Duration, cb Callback) EntryID

	// AddAt registers a callback to fire at the specified absolute time.
	// Returns the entry ID for later removal or renewal.
	AddAt(fireAt time.Time, cb Callback) EntryID

	// Remove removes an entry by ID. No-op if not found.
	Remove(id EntryID)

	// Renew resets the expiration timer for an entry.
	// It removes the old entry and re-adds it with the new duration.
	// Returns the new entry ID.
	Renew(id EntryID, duration time.Duration) EntryID

	// Start begins the time wheel tick loop.
	// Blocks until the context is cancelled or Stop is called.
	Start(ctx context.Context)

	// Stop gracefully stops the time wheel.
	// It cancels the internal tick loop and waits for pending callbacks.
	Stop(ctx context.Context) error
}

// New creates a new hierarchical time wheel configured by the provided
// options. The wheel has two layers: a seconds wheel (60 slots) and a
// minutes wheel (60 slots). Expired callbacks are dispatched through a
// [pool.Pool]; when no pool is injected via [WithPool] the wheel
// creates a default one sized by [WithWorkerSize] (or
// [defaultWorkerSize]) and closes it on [Stop].
//
// Returns an error if the internally-created default pool cannot be
// initialized. This path is unreachable in practice because the worker
// count is sanitized to a positive value, but the error is returned
// rather than panicking to honor the "no panic in business logic"
// rule.
func New(opts ...Option) (Wheel, error) {
	cfg := config{
		workerSize: defaultWorkerSize,
	}
	for _, o := range opts {
		o.apply(&cfg)
	}
	if cfg.workerSize <= 0 {
		cfg.workerSize = defaultWorkerSize
	}
	return newHierarchicalWheel(cfg)
}

// NewWheel creates a new hierarchical time wheel whose default pool is
// sized by workerSize. It is a convenience wrapper around [New];
// callers should prefer [New] with
// [WithWorkerSize] or [WithPool].
//
// If workerSize <= 0, a default of 100 is used.
//
// Returns an error if the internally-created default pool cannot be
// initialized (see [New] for details).
func NewWheel(workerSize int) (Wheel, error) {
	return New(WithWorkerSize(workerSize))
}
