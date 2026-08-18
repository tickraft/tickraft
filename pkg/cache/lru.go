// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package cache

import (
	"container/list"
	"context"
	"strings"
	"sync"
	"time"
)

// lruEntry holds a cached value along with its expiration time.
type lruEntry struct {
	key      string
	value    []byte
	expireAt time.Time
}

// LRUCache provides an in-memory LRU cache with per-entry TTL expiration.
// It is safe for concurrent use via sync.RWMutex.
//
// Cache keys follow the format "{type}:{id}" or "{type}:{field}:{value}"
// to allow structured key lookups.
type LRUCache struct {
	mu         sync.RWMutex
	items      map[string]*list.Element
	evictList  *list.List
	maxEntries int
	defaultTTL time.Duration
}

// NewLRU creates an LRU with the given maximum entry count and default TTL.
// When maxEntries is reached, the least recently used entry is evicted.
// The defaultTTL is applied to entries set via Set; SetWithTTL overrides it.
// If maxEntries <= 0, it defaults to 1024; if defaultTTL <= 0, it defaults to 5 minutes.
func NewLRU(maxEntries int, defaultTTL time.Duration) *LRUCache {
	if maxEntries <= 0 {
		maxEntries = 1024
	}
	if defaultTTL <= 0 {
		defaultTTL = 5 * time.Minute
	}
	return &LRUCache{
		items:      make(map[string]*list.Element),
		evictList:  list.New(),
		maxEntries: maxEntries,
		defaultTTL: defaultTTL,
	}
}

// Get retrieves a cached value by key. If the key exists and has not expired,
// the value is returned and the entry is promoted to the front of the LRU list.
// If the key is missing or expired, it is removed and (nil, false) is returned.
//
// The context is accepted to satisfy the [Cache] contract; the in-memory
// implementation is non-blocking and does not consult it.
func (c *LRUCache) Get(_ context.Context, key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	elem, ok := c.items[key]
	if !ok {
		return nil, false
	}

	entry := entryOf(elem)
	if time.Now().After(entry.expireAt) {
		c.removeElement(elem)
		return nil, false
	}

	c.evictList.MoveToFront(elem)
	return entry.value, true
}

// Set stores a value with the cache's default TTL. If the key already exists,
// its value and expiration are updated and it is promoted in the LRU list.
// If the cache is full, the least recently used entry is evicted first.
//
// The context is accepted to satisfy the [Cache] contract; the in-memory
// implementation is non-blocking and does not consult it.
func (c *LRUCache) Set(ctx context.Context, key string, value []byte) {
	c.SetWithTTL(ctx, key, value, c.defaultTTL)
}

// SetWithTTL stores a value with an explicit TTL. If the key already exists,
// its value and expiration are updated and it is promoted in the LRU list.
// If the cache is full, the least recently used entry is evicted first.
//
// The context is accepted to satisfy the [Cache] contract; the in-memory
// implementation is non-blocking and does not consult it.
func (c *LRUCache) SetWithTTL(_ context.Context, key string, value []byte, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.items[key]; ok {
		c.evictList.MoveToFront(elem)
		entry := entryOf(elem)
		entry.value = value
		entry.expireAt = time.Now().Add(ttl)
		return
	}

	entry := &lruEntry{
		key:      key,
		value:    value,
		expireAt: time.Now().Add(ttl),
	}
	elem := c.evictList.PushFront(entry)
	c.items[key] = elem

	if c.evictList.Len() > c.maxEntries {
		oldest := c.evictList.Back()
		if oldest != nil {
			c.removeElement(oldest)
		}
	}
}

// Delete removes a key from the cache. It is a no-op if the key does not exist.
//
// The context is accepted to satisfy the [Cache] contract; the in-memory
// implementation is non-blocking and does not consult it.
func (c *LRUCache) Delete(_ context.Context, key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.items[key]; ok {
		c.removeElement(elem)
	}
}

// Clear removes all entries from the cache.
//
// The context is accepted to satisfy the [Cache] contract; the in-memory
// implementation is non-blocking and does not consult it.
func (c *LRUCache) Clear(_ context.Context) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.items = make(map[string]*list.Element)
	c.evictList.Init()
}

// Has checks whether a key exists without retrieving its value.
// It does not promote the entry in the LRU list.
//
// The context is accepted to satisfy the [Cache] contract; the in-memory
// implementation is non-blocking and does not consult it.
func (c *LRUCache) Has(_ context.Context, key string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	elem, ok := c.items[key]
	if !ok {
		return false
	}

	entry := entryOf(elem)
	return !time.Now().After(entry.expireAt)
}

// GetWithTTL retrieves a cached value and its remaining TTL.
// If the key is missing or expired, it returns nil, 0, false.
//
// The context is accepted to satisfy the [Cache] contract; the in-memory
// implementation is non-blocking and does not consult it.
func (c *LRUCache) GetWithTTL(_ context.Context, key string) ([]byte, time.Duration, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	elem, ok := c.items[key]
	if !ok {
		return nil, 0, false
	}

	entry := entryOf(elem)
	now := time.Now()
	if now.After(entry.expireAt) {
		c.removeElement(elem)
		return nil, 0, false
	}

	c.evictList.MoveToFront(elem)
	remaining := entry.expireAt.Sub(now)
	return entry.value, remaining, true
}

// DeleteByPrefix removes all entries whose keys start with the given prefix.
//
// The context is accepted to satisfy the [Cache] contract; the in-memory
// implementation is non-blocking and does not consult it.
func (c *LRUCache) DeleteByPrefix(_ context.Context, prefix string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	var toRemove []*list.Element
	for key, elem := range c.items {
		if strings.HasPrefix(key, prefix) {
			toRemove = append(toRemove, elem)
		}
	}
	for _, elem := range toRemove {
		c.removeElement(elem)
	}
}

// Size returns the number of entries currently in the cache.
//
// The context is accepted to satisfy the [Cache] contract; the in-memory
// implementation is non-blocking and does not consult it.
func (c *LRUCache) Size(_ context.Context) int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return len(c.items)
}

// Close releases any resources held by the cache.
// For LRU this is a no-op since all data is in memory. The context is
// accepted to satisfy the [Cache] contract and is not consulted.
func (c *LRUCache) Close(_ context.Context) error {
	return nil
}

// removeElement removes a list element from both the eviction list and the
// items map. The caller must hold c.mu.
func (c *LRUCache) removeElement(elem *list.Element) {
	c.evictList.Remove(elem)
	entry := entryOf(elem)
	delete(c.items, entry.key)
}

// entryOf returns the lruEntry stored in a list element. The type assertion
// is safe because only *lruEntry values are ever pushed into evictList.
func entryOf(elem *list.Element) *lruEntry {
	return elem.Value.(*lruEntry) //nolint:errcheck // invariant: only *lruEntry is stored
}
