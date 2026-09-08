// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package cache provides concrete caching implementations with TTL support.
//
// The package ships LRUCache, an in-memory LRU cache with per-entry TTL
// expiration. It is safe for concurrent use.
//
// Use LRUCache when:
//   - Cache data is purely a performance optimization and can be lost on restart.
//   - Low latency and high throughput are required.
//   - The working set fits comfortably in memory.
//
// # Usage
//
// Create an LRU cache and use it:
//
//	c := cache.NewLRU(1024, 5*time.Minute)
//	c.Set("key", []byte("value"))
//	val, ok := c.Get("key")
package cache
