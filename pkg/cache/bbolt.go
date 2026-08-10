// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package cache

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"go.etcd.io/bbolt"
	berrors "go.etcd.io/bbolt/errors"
	"go.uber.org/zap"
)

// bboltBucket is the single bucket used to store all cache entries.
var bboltBucket = []byte("cache")

// entryHeaderSize is the number of bytes used to store the expiration
// timestamp at the beginning of each stored entry.
const entryHeaderSize = 8

// BboltCache provides a persistent cache backed by an embedded bbolt database.
// It uses disk-based storage, allowing cache data to survive process restarts.
//
// Each entry is stored as [8-byte expiration UnixNano][value bytes]. Entries
// are checked for expiration on read; expired entries return (nil, false) and
// are deleted asynchronously to avoid blocking the read path.
//
// All methods use bbolt's transaction model, which ensures concurrent safety
// at the database level. The database is opened with a 600-second timeout to
// avoid indefinite blocking on lock acquisition.
//
// Async deletion of expired entries is coordinated with Close via a mutex, a
// closed flag, and a WaitGroup: Close sets closed under the mutex (rejecting
// new async work) and then waits for in-flight deleteKey goroutines to finish
// before closing the database handle. The mutex ensures WaitGroup.Add cannot
// race with WaitGroup.Wait, which the WaitGroup contract forbids when the
// counter is zero.
type BboltCache struct {
	db         *bbolt.DB
	defaultTTL time.Duration

	// closeMu protects closed and serializes WaitGroup.Add against
	// WaitGroup.Wait. It is held only briefly (flag check + Add), never
	// across a bbolt transaction.
	closeMu sync.Mutex
	// closed is set to true by Close; subsequent deleteKey calls observe
	// it and skip registration. Guarded by closeMu.
	closed bool
	// wg tracks in-flight async deleteKey goroutines so Close can wait
	// for them before closing the database handle. Add is called under
	// closeMu after checking closed; Wait is called after setting closed,
	// satisfying the WaitGroup contract.
	wg sync.WaitGroup
}

// NewBbolt creates a persistent cache backed by a bbolt database at the given
// file path. The defaultTTL is applied to entries set via Set; SetWithTTL
// overrides it. If defaultTTL <= 0, it defaults to 5 minutes.
//
// The bbolt database is opened with a 600-second lock timeout. The cache
// bucket is created automatically if it does not exist.
func NewBbolt(path string, defaultTTL time.Duration) (*BboltCache, error) {
	if path == "" {
		return nil, errors.New("cache: bbolt path is required")
	}
	if defaultTTL <= 0 {
		defaultTTL = 5 * time.Minute
	}

	db, err := bbolt.Open(path, 0600, &bbolt.Options{Timeout: 600 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("cache: open bbolt %q: %w", path, err)
	}

	// Ensure the cache bucket exists.
	err = db.Update(func(tx *bbolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(bboltBucket)
		return err
	})
	if err != nil {
		// ignored because: bucket creation failed and we are about to return the
		// creation error to the caller; the close error is not actionable and
		// surfacing it would mask the root cause.
		_ = db.Close()
		return nil, fmt.Errorf("cache: create bbolt bucket: %w", err)
	}

	return &BboltCache{
		db:         db,
		defaultTTL: defaultTTL,
	}, nil
}

// Get retrieves a cached value by key. Returns the value and true if the key
// exists and has not expired. Returns nil and false if the key is missing or
// expired. Expired entries are deleted asynchronously to avoid blocking the
// read path.
//
// The context is not propagated into the bbolt transaction (bbolt does not
// accept a context), but it is consulted before starting the read: a already
// cancelled context returns immediately without touching the database.
func (c *BboltCache) Get(ctx context.Context, key string) ([]byte, bool) {
	if err := ctx.Err(); err != nil {
		return nil, false
	}
	var raw []byte
	err := c.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(bboltBucket)
		if bucket == nil {
			return nil
		}
		value := bucket.Get([]byte(key))
		if value == nil {
			return nil
		}
		// Copy the value because it is only valid for the duration of the
		// transaction.
		raw = make([]byte, len(value))
		copy(raw, value)
		return nil
	})
	if err != nil {
		return nil, false
	}

	if len(raw) == 0 {
		return nil, false
	}

	value, expireAt, ok := decodeEntry(raw)
	if !ok {
		return nil, false
	}

	if time.Now().After(expireAt) {
		// goroutine lifecycle: bounded — performs a single bbolt Delete transaction
		// and exits. Tracked by c.wg so Close() can drain in-flight calls; new
		// calls observe the closed flag and short-circuit.
		go c.deleteKey(context.Background(), key)
		return nil, false
	}

	return value, true
}

// Set stores a value with the cache's default TTL. If the key already exists,
// its value and expiration are overwritten.
//
// The context is consulted before the write; a cancelled context skips the
// persist. bbolt itself does not accept a context.
func (c *BboltCache) Set(ctx context.Context, key string, value []byte) {
	c.SetWithTTL(ctx, key, value, c.defaultTTL)
}

// SetWithTTL stores a value with an explicit TTL. If the key already exists,
// its value and expiration are overwritten.
//
// The context is consulted before the write; a cancelled context skips the
// persist. bbolt itself does not accept a context.
func (c *BboltCache) SetWithTTL(ctx context.Context, key string, value []byte, ttl time.Duration) {
	if err := ctx.Err(); err != nil {
		return
	}
	expireAt := time.Now().Add(ttl)
	entry := encodeEntry(value, expireAt)

	err := c.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(bboltBucket)
		if bucket == nil {
			return fmt.Errorf("cache: bucket %q not found", bboltBucket)
		}
		return bucket.Put([]byte(key), entry)
	})
	if err != nil {
		// Persisting cache entries is best-effort: the Set API has no error
		// return to preserve consistency with the in-memory LRU. Log via the
		// global zap logger so failures remain observable without changing
		// the public signature.
		zap.L().Warn("cache: persist entry failed",
			zap.String("key", key),
			zap.Error(err),
		)
	}
}

// Delete removes a cached entry by key. It is a no-op if the key does not
// exist.
//
// The context is consulted before the delete; a cancelled context skips it.
func (c *BboltCache) Delete(ctx context.Context, key string) {
	c.deleteKey(ctx, key)
}

// deleteKey removes a single key from the bucket within a write transaction.
// It is safe to call after Close: the closed flag check short-circuits new
// work, and Close waits for in-flight calls via the WaitGroup. This is
// important because Get and GetWithTTL may spawn asynchronous deleteKey calls
// for expired entries that could race with Close.
//
// The closeMu mutex is held only for the closed check and WaitGroup.Add,
// never across the bbolt transaction. This satisfies the WaitGroup contract:
// Add with a positive delta always happens-before Wait when the counter is
// zero, because Close sets closed under the same mutex before calling Wait.
func (c *BboltCache) deleteKey(ctx context.Context, key string) {
	// Isolate panics in this asynchronous path to prevent process crash.
	// Sync callers of Delete are expected to propagate panics to their own
	// recover handlers; async callers (Get/GetWithTTL) have no caller
	// frame to handle a panic, so it must be recovered here and logged via
	// the global zap logger.
	defer func() {
		if r := recover(); r != nil {
			zap.L().Error("cache: async deleteKey panicked",
				zap.String("key", key),
				zap.Any("panic", r),
			)
		}
	}()

	if err := ctx.Err(); err != nil {
		return
	}

	c.closeMu.Lock()
	if c.closed {
		c.closeMu.Unlock()
		return
	}
	c.wg.Add(1)
	c.closeMu.Unlock()
	defer c.wg.Done()

	err := c.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(bboltBucket)
		if bucket == nil {
			return nil
		}
		return bucket.Delete([]byte(key))
	})
	if err != nil {
		// Async cleanup of an expired entry: not actionable for the caller.
		// Logged via the global zap logger so a corrupted bucket is observable.
		zap.L().Warn("cache: async deleteKey failed",
			zap.String("key", key),
			zap.Error(err),
		)
	}
}

// Clear removes all cached entries by deleting and recreating the bucket.
//
// The context is consulted before the clear; a cancelled context skips it.
func (c *BboltCache) Clear(ctx context.Context) {
	if err := ctx.Err(); err != nil {
		return
	}
	err := c.db.Update(func(tx *bbolt.Tx) error {
		if err := tx.DeleteBucket(bboltBucket); err != nil && !errors.Is(err, berrors.ErrBucketNotFound) {
			return err
		}
		_, err := tx.CreateBucketIfNotExists(bboltBucket)
		return err
	})
	if err != nil {
		// Clear has no error return; log via the global zap logger so a failure
		// to reset the cache is observable by operators.
		zap.L().Warn("cache: clear failed",
			zap.Error(err),
		)
	}
}

// Has checks whether a key exists and has not expired. It does not promote
// or delete entries.
//
// The context is consulted before the read; a cancelled context returns
// false.
func (c *BboltCache) Has(ctx context.Context, key string) bool {
	if err := ctx.Err(); err != nil {
		return false
	}
	var raw []byte
	err := c.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(bboltBucket)
		if bucket == nil {
			return nil
		}
		value := bucket.Get([]byte(key))
		if value == nil {
			return nil
		}
		raw = make([]byte, len(value))
		copy(raw, value)
		return nil
	})
	if err != nil {
		return false
	}

	if len(raw) == 0 {
		return false
	}

	_, expireAt, ok := decodeEntry(raw)
	if !ok {
		return false
	}

	return !time.Now().After(expireAt)
}

// GetWithTTL retrieves a cached value and its remaining TTL. If the key is
// missing or expired, it returns nil, 0, false. Expired entries are deleted
// asynchronously to avoid blocking the read path.
//
// The context is consulted before the read; a cancelled context returns a
// miss.
func (c *BboltCache) GetWithTTL(ctx context.Context, key string) ([]byte, time.Duration, bool) {
	if err := ctx.Err(); err != nil {
		return nil, 0, false
	}
	var raw []byte
	err := c.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(bboltBucket)
		if bucket == nil {
			return nil
		}
		value := bucket.Get([]byte(key))
		if value == nil {
			return nil
		}
		raw = make([]byte, len(value))
		copy(raw, value)
		return nil
	})
	if err != nil {
		return nil, 0, false
	}

	if len(raw) == 0 {
		return nil, 0, false
	}

	value, expireAt, ok := decodeEntry(raw)
	if !ok {
		return nil, 0, false
	}

	now := time.Now()
	if now.After(expireAt) {
		// goroutine lifecycle: bounded — performs a single bbolt Delete transaction
		// and exits. Tracked by c.wg so Close() can drain in-flight calls.
		go c.deleteKey(context.Background(), key)
		return nil, 0, false
	}

	remaining := expireAt.Sub(now)
	return value, remaining, true
}

// DeleteByPrefix removes all entries whose keys start with the given prefix.
//
// The context is consulted before the delete; a cancelled context skips it.
func (c *BboltCache) DeleteByPrefix(ctx context.Context, prefix string) {
	if err := ctx.Err(); err != nil {
		return
	}
	err := c.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(bboltBucket)
		if bucket == nil {
			return nil
		}
		cursor := bucket.Cursor()
		// The cursor value is unused for prefix-based deletion; the key alone
		// determines membership.
		for k, _ := cursor.Seek([]byte(prefix)); k != nil && strings.HasPrefix(string(k), prefix); k, _ = cursor.Next() {
			if err := cursor.Delete(); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		// DeleteByPrefix has no error return; log via the global zap logger so
		// a partial failure is observable by operators.
		zap.L().Warn("cache: delete by prefix failed",
			zap.String("prefix", prefix),
			zap.Error(err),
		)
	}
}

// Size returns the number of entries currently in the cache. Note that this
// count may include entries that have expired but have not yet been cleaned
// up by a read-triggered deletion.
//
// The context is consulted before the read; a cancelled context returns 0.
func (c *BboltCache) Size(ctx context.Context) int {
	if err := ctx.Err(); err != nil {
		return 0
	}
	var count int
	err := c.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(bboltBucket)
		if bucket == nil {
			return nil
		}
		count = bucket.Stats().KeyN
		return nil
	})
	if err != nil {
		return 0
	}
	return count
}

// Close releases the bbolt database handle. After Close is called, the cache
// must not be used. Close is idempotent and safe for concurrent callers.
//
// Close first sets the closed flag under closeMu (rejecting new async
// deleteKey work), then waits for in-flight deleteKey goroutines to finish
// before closing the database handle. This avoids data races between async
// expired-entry deletions and database teardown, and satisfies the WaitGroup
// contract by ensuring no Add call can race with Wait.
//
// The context bounds the wait for in-flight async deleteKey goroutines.
// A cancelled context does not abort the database close itself (which is
// non-cancellable), but it skips the goroutine drain so teardown returns
// promptly; in-flight deletes observe the closed flag and short-circuit.
func (c *BboltCache) Close(ctx context.Context) error {
	c.closeMu.Lock()
	if c.closed {
		c.closeMu.Unlock()
		return nil
	}
	c.closed = true
	c.closeMu.Unlock()

	// Wait for in-flight async deleteKey goroutines to finish before
	// closing the database handle. New deleteKey calls observe the closed
	// flag and return without touching the database. Honor the caller's
	// context: if it is already cancelled, skip the drain (the goroutines
	// will observe the closed flag and exit on their own).
	if ctx.Err() == nil {
		c.wg.Wait()
	}
	return c.db.Close()
}

// encodeEntry serializes a value with its expiration timestamp into a single
// byte slice suitable for storage in bbolt. The first 8 bytes hold the
// expiration UnixNano timestamp (little-endian), followed by the raw value.
func encodeEntry(value []byte, expireAt time.Time) []byte {
	buf := make([]byte, entryHeaderSize+len(value))
	binary.LittleEndian.PutUint64(buf[:entryHeaderSize], uint64(expireAt.UnixNano()))
	copy(buf[entryHeaderSize:], value)
	return buf
}

// decodeEntry deserializes a stored entry into its value and expiration time.
// Returns ok=false if the data is too short to contain a valid header.
func decodeEntry(data []byte) (value []byte, expireAt time.Time, ok bool) {
	if len(data) < entryHeaderSize {
		return nil, time.Time{}, false
	}
	expireAt = time.Unix(0, int64(binary.LittleEndian.Uint64(data[:entryHeaderSize])))
	value = data[entryHeaderSize:]
	return value, expireAt, true
}
