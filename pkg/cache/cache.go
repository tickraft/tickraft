// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package cache

import (
	"context"
	"time"
)

// Cache is the context-aware caching contract implemented by LRUCache
// and any extended (e.g. Redis) backends.
//
// Every method accepts a [context.Context] so that callers can cancel
// in-flight work or attach deadlines. In-memory implementations
// (*LRUCache) honor cancellation only opportunistically (the operation
// is fast and non-blocking), while remote backends (Redis) propagate
// the context to the underlying store so that a cancelled caller does
// not keep the backend busy.
//
// All values are stored as []byte to ensure consistent behavior across
// implementations. Use the GetJSON/SetJSON helpers for type-safe access
// with automatic JSON serialization.
//
// Implementations must be safe for concurrent use.
//
// This interface is the canonical cache contract for the
// kernel; the extended package re-exports it as a type alias so that
// LRU and Redis implementations all satisfy a single contract.
type Cache interface {
	// Get retrieves a cached value by key. Returns the value and true
	// if found, or nil and false if not found or expired.
	Get(ctx context.Context, key string) ([]byte, bool)
	// Set stores a value with the default TTL.
	Set(ctx context.Context, key string, value []byte)
	// SetWithTTL stores a value with a custom TTL.
	SetWithTTL(ctx context.Context, key string, value []byte, ttl time.Duration)
	// Delete removes a cached entry by key.
	Delete(ctx context.Context, key string)
	// Clear removes all cached entries.
	Clear(ctx context.Context)
	// Has checks whether a key exists without retrieving its value.
	Has(ctx context.Context, key string) bool
	// GetWithTTL retrieves a cached value and its remaining TTL.
	GetWithTTL(ctx context.Context, key string) ([]byte, time.Duration, bool)
	// DeleteByPrefix removes all entries whose keys start with the
	// given prefix.
	DeleteByPrefix(ctx context.Context, prefix string)
	// Size returns the number of entries currently in the cache.
	Size(ctx context.Context) int
	// Close releases any resources held by the cache. The context
	// allows callers to bound teardown; a cancelled context does not
	// guarantee an immediate return for backends that must flush.
	Close(ctx context.Context) error
}

// Compile-time assertion that the concrete implementation satisfies
// the context-aware Cache contract.
var _ Cache = (*LRUCache)(nil)
