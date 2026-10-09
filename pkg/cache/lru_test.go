// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package cache

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestCacheHit(t *testing.T) {
	c := NewLRU(1024, 5*time.Minute)
	c.Set(context.Background(), "user:1", []byte("alice"))

	val, ok := c.Get(context.Background(), "user:1")
	if !ok {
		t.Fatal("expected cache hit, got miss")
	}
	if string(val) != "alice" {
		t.Fatalf("expected alice, got %s", val)
	}
}

func TestCacheMiss(t *testing.T) {
	c := NewLRU(1024, 5*time.Minute)

	_, ok := c.Get(context.Background(), "nonexistent")
	if ok {
		t.Fatal("expected cache miss, got hit")
	}
}

func TestCacheTTLExpiration(t *testing.T) {
	c := NewLRU(1024, 5*time.Minute)
	c.SetWithTTL(context.Background(), "session:abc", []byte("token123"), 50*time.Millisecond)

	val, ok := c.Get(context.Background(), "session:abc")
	if !ok {
		t.Fatal("expected cache hit before TTL expiration")
	}
	if string(val) != "token123" {
		t.Fatalf("expected token123, got %s", val)
	}

	time.Sleep(100 * time.Millisecond)

	_, ok = c.Get(context.Background(), "session:abc")
	if ok {
		t.Fatal("expected cache miss after TTL expiration")
	}
}

func TestCacheLRUEviction(t *testing.T) {
	c := NewLRU(3, 5*time.Minute)

	c.Set(context.Background(), "user:1", []byte("alice"))
	c.Set(context.Background(), "user:2", []byte("bob"))
	c.Set(context.Background(), "user:3", []byte("charlie"))

	// Cache is full (3 entries). Adding a 4th should evict the LRU entry (user:1).
	c.Set(context.Background(), "user:4", []byte("diana"))

	_, ok := c.Get(context.Background(), "user:1")
	if ok {
		t.Fatal("expected user:1 to be evicted")
	}

	// The remaining entries should still be present.
	if val, ok := c.Get(context.Background(), "user:2"); !ok || string(val) != "bob" {
		t.Fatalf("expected user:2=bob, got %s, ok=%v", val, ok)
	}
	if val, ok := c.Get(context.Background(), "user:3"); !ok || string(val) != "charlie" {
		t.Fatalf("expected user:3=charlie, got %s, ok=%v", val, ok)
	}
	if val, ok := c.Get(context.Background(), "user:4"); !ok || string(val) != "diana" {
		t.Fatalf("expected user:4=diana, got %s, ok=%v", val, ok)
	}
}

func TestCacheLRUEvictionUpdatesOnGet(t *testing.T) {
	c := NewLRU(3, 5*time.Minute)

	c.Set(context.Background(), "user:1", []byte("alice"))
	c.Set(context.Background(), "user:2", []byte("bob"))
	c.Set(context.Background(), "user:3", []byte("charlie"))

	// Access user:1 so it becomes most recently used.
	_, _ = c.Get(context.Background(), "user:1")

	// Adding a 4th entry should now evict user:2 (the LRU).
	c.Set(context.Background(), "user:4", []byte("diana"))

	_, ok := c.Get(context.Background(), "user:2")
	if ok {
		t.Fatal("expected user:2 to be evicted after user:1 was accessed")
	}

	if val, ok := c.Get(context.Background(), "user:1"); !ok || string(val) != "alice" {
		t.Fatalf("expected user:1=alice to survive, got %s, ok=%v", val, ok)
	}
}

func TestCacheDelete(t *testing.T) {
	c := NewLRU(1024, 5*time.Minute)
	c.Set(context.Background(), "user:1", []byte("alice"))

	c.Delete(context.Background(), "user:1")

	_, ok := c.Get(context.Background(), "user:1")
	if ok {
		t.Fatal("expected cache miss after delete")
	}
}

func TestCacheDeleteNonExistent(t *testing.T) {
	c := NewLRU(1024, 5*time.Minute)

	// Deleting a non-existent key should be a no-op.
	c.Delete(context.Background(), "nonexistent")
}

func TestCacheClear(t *testing.T) {
	c := NewLRU(1024, 5*time.Minute)
	c.Set(context.Background(), "user:1", []byte("alice"))
	c.Set(context.Background(), "user:2", []byte("bob"))
	c.Set(context.Background(), "tenant:1", []byte("acme"))

	c.Clear(context.Background())

	for _, key := range []string{"user:1", "user:2", "tenant:1"} {
		if _, ok := c.Get(context.Background(), key); ok {
			t.Fatalf("expected cache miss for %s after clear", key)
		}
	}
}

func TestCacheConcurrentAccess(t *testing.T) {
	c := NewLRU(1024, 5*time.Minute)
	var wg sync.WaitGroup

	for i := range 100 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			key := "key:" + string(rune('a'+n%26))
			c.Set(context.Background(), key, []byte{byte(n)})
			c.Get(context.Background(), key)
			c.Delete(context.Background(), key)
		}(i)
	}

	wg.Wait()
}

func TestCacheSetUpdatesExistingKey(t *testing.T) {
	c := NewLRU(1024, 5*time.Minute)
	c.Set(context.Background(), "user:1", []byte("alice"))
	c.Set(context.Background(), "user:1", []byte("bob"))

	val, ok := c.Get(context.Background(), "user:1")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if string(val) != "bob" {
		t.Fatalf("expected bob, got %s", val)
	}
}

func TestCacheDefaultConstructor(t *testing.T) {
	// Zero values should fall back to defaults.
	c := NewLRU(0, 0)
	if c.maxEntries != 1024 {
		t.Fatalf("expected default maxEntries=1024, got %d", c.maxEntries)
	}
	if c.defaultTTL != 5*time.Minute {
		t.Fatalf("expected default TTL=5m, got %v", c.defaultTTL)
	}
}

func TestCacheStructuredKeyFormats(t *testing.T) {
	c := NewLRU(1024, 5*time.Minute)

	// {type}:{id} format
	c.Set(context.Background(), "user:1", []byte("alice"))
	val, ok := c.Get(context.Background(), "user:1")
	if !ok || string(val) != "alice" {
		t.Fatalf("expected user:1=alice, got %s, ok=%v", val, ok)
	}

	// {type}:{field}:{value} format
	c.Set(context.Background(), "user:email:alice@example.com", []byte("alice"))
	val, ok = c.Get(context.Background(), "user:email:alice@example.com")
	if !ok || string(val) != "alice" {
		t.Fatalf("expected user:email:alice@example.com=alice, got %s, ok=%v", val, ok)
	}
}

func TestLRUCache_Has(t *testing.T) {
	c := NewLRU(1024, 5*time.Minute)

	// Key does not exist.
	if c.Has(context.Background(), "missing") {
		t.Fatal("expected Has to return false for missing key")
	}

	// Key exists.
	c.Set(context.Background(), "user:1", []byte("alice"))
	if !c.Has(context.Background(), "user:1") {
		t.Fatal("expected Has to return true for existing key")
	}

	// Key expired.
	c.SetWithTTL(context.Background(), "session:abc", []byte("token"), 50*time.Millisecond)
	if !c.Has(context.Background(), "session:abc") {
		t.Fatal("expected Has to return true before expiration")
	}
	time.Sleep(100 * time.Millisecond)
	if c.Has(context.Background(), "session:abc") {
		t.Fatal("expected Has to return false after expiration")
	}
}

func TestLRUCache_GetWithTTL(t *testing.T) {
	c := NewLRU(1024, 5*time.Minute)

	// Missing key.
	_, _, ok := c.GetWithTTL(context.Background(), "missing")
	if ok {
		t.Fatal("expected GetWithTTL to return false for missing key")
	}

	// Existing key with TTL.
	c.SetWithTTL(context.Background(), "session:abc", []byte("token"), 5*time.Second)
	val, ttl, ok := c.GetWithTTL(context.Background(), "session:abc")
	if !ok {
		t.Fatal("expected GetWithTTL to return true for existing key")
	}
	if string(val) != "token" {
		t.Fatalf("expected token, got %s", val)
	}
	if ttl <= 0 || ttl > 5*time.Second {
		t.Fatalf("expected TTL between 0 and 5s, got %v", ttl)
	}

	// Expired key.
	c.SetWithTTL(context.Background(), "session:expired", []byte("gone"), 50*time.Millisecond)
	time.Sleep(100 * time.Millisecond)
	_, _, ok = c.GetWithTTL(context.Background(), "session:expired")
	if ok {
		t.Fatal("expected GetWithTTL to return false for expired key")
	}
}

func TestLRUCache_DeleteByPrefix(t *testing.T) {
	c := NewLRU(1024, 5*time.Minute)

	c.Set(context.Background(), "user:1", []byte("alice"))
	c.Set(context.Background(), "user:2", []byte("bob"))
	c.Set(context.Background(), "tenant:1", []byte("acme"))
	c.Set(context.Background(), "tenant:2", []byte("beta"))

	c.DeleteByPrefix(context.Background(), "user:")

	// user keys should be gone.
	if _, ok := c.Get(context.Background(), "user:1"); ok {
		t.Fatal("expected user:1 to be deleted")
	}
	if _, ok := c.Get(context.Background(), "user:2"); ok {
		t.Fatal("expected user:2 to be deleted")
	}

	// tenant keys should remain.
	if val, ok := c.Get(context.Background(), "tenant:1"); !ok || string(val) != "acme" {
		t.Fatalf("expected tenant:1=acme, got %s, ok=%v", val, ok)
	}
	if val, ok := c.Get(context.Background(), "tenant:2"); !ok || string(val) != "beta" {
		t.Fatalf("expected tenant:2=beta, got %s, ok=%v", val, ok)
	}
}

func TestLRUCache_Size(t *testing.T) {
	c := NewLRU(1024, 5*time.Minute)

	if s := c.Size(context.Background()); s != 0 {
		t.Fatalf("expected size 0, got %d", s)
	}

	c.Set(context.Background(), "user:1", []byte("alice"))
	c.Set(context.Background(), "user:2", []byte("bob"))

	if s := c.Size(context.Background()); s != 2 {
		t.Fatalf("expected size 2, got %d", s)
	}

	c.Delete(context.Background(), "user:1")

	if s := c.Size(context.Background()); s != 1 {
		t.Fatalf("expected size 1, got %d", s)
	}

	c.Clear(context.Background())

	if s := c.Size(context.Background()); s != 0 {
		t.Fatalf("expected size 0 after clear, got %d", s)
	}
}

func TestLRUCache_Close(t *testing.T) {
	c := NewLRU(1024, 5*time.Minute)

	if err := c.Close(context.Background()); err != nil {
		t.Fatalf("expected Close to return nil, got %v", err)
	}
}
