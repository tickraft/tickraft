// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package jwt implements JWT signing, parsing, validation, refresh-token
// rotation, and token-revocation blacklists for tickraft authentication.
package jwt

import (
	"context"
	"sync"
	"time"
)

// defaultCleanupInterval is the default period at which the background
// goroutine scans for and removes expired blacklist entries.
const defaultCleanupInterval = 5 * time.Minute

// MemoryBlacklist is an in-process JWT blacklist checker suitable for
// single-process deployments that do not share an external store. Revoked
// JTIs are kept in an in-memory map keyed by JTI with their token expiry as
// the value; entries past their expiry are eligible for cleanup.
//
// Because state is local to the process, revocations performed on one
// instance are NOT visible to other instances. For HA deployments with
// multiple instances, use a distributed-store-backed blacklist
// instead.
type MemoryBlacklist struct {
	mu      sync.RWMutex
	entries map[string]time.Time // jti -> token expiry
	// cleanupInterval is how often the background goroutine removes expired
	// entries. Defaults to defaultCleanupInterval when zero.
	cleanupInterval time.Duration
	cancel          context.CancelFunc
}

// NewMemoryBlacklist creates a new in-process blacklist. The returned
// blacklist is ready to use; call StartCleanup to launch the background
// reaper and Close to stop it.
func NewMemoryBlacklist() *MemoryBlacklist {
	return &MemoryBlacklist{
		entries:         make(map[string]time.Time),
		cleanupInterval: defaultCleanupInterval,
	}
}

// Revoke adds jti to the blacklist with the given expiry. Once a JTI is
// revoked, IsRevoked reports true until the entry is cleaned up after expiry.
// Revoke is safe for concurrent use.
func (m *MemoryBlacklist) Revoke(jti string, expiry time.Time) {
	if jti == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries[jti] = expiry
}

// IsRevoked reports whether jti is currently in the blacklist. Expired
// entries are treated as not revoked (the token would already be invalid).
// IsRevoked is safe for concurrent use.
func (m *MemoryBlacklist) IsRevoked(jti string) bool {
	if jti == "" {
		return false
	}
	m.mu.RLock()
	expiry, ok := m.entries[jti]
	m.mu.RUnlock()
	if !ok {
		return false
	}
	// An expired entry is logically gone; let cleanup reclaim it.
	return !time.Now().After(expiry)
}

// Cleanup removes all entries whose expiry is in the past. It is invoked
// periodically by the background goroutine started via StartCleanup and may
// also be called directly (e.g. in tests).
func (m *MemoryBlacklist) Cleanup() {
	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	for jti, expiry := range m.entries {
		if now.After(expiry) {
			delete(m.entries, jti)
		}
	}
}

// Checker returns a BlacklistChecker func backed by this blacklist. The
// returned func never returns a non-nil error (in-memory lookups cannot
// fail), making it directly usable as the blacklistChecker argument to
// jwt.New.
func (m *MemoryBlacklist) Checker() BlacklistChecker {
	return func(jti string) (bool, error) {
		return m.IsRevoked(jti), nil
	}
}

// StartCleanup launches a background goroutine that periodically calls
// Cleanup until ctx is canceled or Close is called. The interval defaults to
// the blacklist's cleanupInterval (set via SetCleanupInterval). It is safe
// to call multiple times; each call replaces the previous goroutine.
func (m *MemoryBlacklist) StartCleanup(ctx context.Context) {
	// Stop any previously started goroutine before starting a new one.
	m.StopCleanup()

	interval := m.cleanupInterval
	if interval <= 0 {
		interval = defaultCleanupInterval
	}

	cctx, cancel := context.WithCancel(ctx)
	m.mu.Lock()
	m.cancel = cancel
	m.mu.Unlock()

	go m.runCleanup(cctx, interval)
}

// SetCleanupInterval sets the period between background cleanup sweeps. It
// must be called before StartCleanup to take effect. A non-positive value
// resets to the default interval.
func (m *MemoryBlacklist) SetCleanupInterval(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if d <= 0 {
		d = defaultCleanupInterval
	}
	m.cleanupInterval = d
}

// StopCleanup stops the background cleanup goroutine. It is safe to call
// when no goroutine is running.
func (m *MemoryBlacklist) StopCleanup() {
	m.mu.Lock()
	cancel := m.cancel
	m.cancel = nil
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// Close stops the background cleanup goroutine, releasing resources. It is
// safe to call multiple times.
func (m *MemoryBlacklist) Close() {
	m.StopCleanup()
}

// runCleanup is the background goroutine loop. It exits when ctx is canceled.
func (m *MemoryBlacklist) runCleanup(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.Cleanup()
		}
	}
}

// Compile-time assertion that *MemoryBlacklist can serve as a BlacklistChecker
// provider via its Checker method.
var _ = (*MemoryBlacklist)(nil).Checker
