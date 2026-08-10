// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package auth

import (
	"sync"
	"time"
)

// This file exposes unexported symbols for use by external test packages
// (package auth_test). It is only compiled during `go test` and does not
// affect the production binary.

// Exported constants for testing.
const (
	MaxLoginFails     = maxLoginFails
	LockoutDuration   = lockoutDuration
	CleanupInterval   = cleanupInterval
	FailEntryTTLConst = failEntryTTL
)

// LoginFailsEntry is an exported mirror of the unexported loginFailRecord
// struct, used to construct entries from external test packages.
type LoginFailsEntry struct {
	Count        int
	LockedUntil  time.Time
	LastFailedAt time.Time
}

// NewServiceForCleanupTest constructs a Service with only the fields needed
// for testing the login-fail cleanup logic. The store fields are left nil
// because cleanup tests do not exercise the persistence layer.
func NewServiceForCleanupTest() *Service {
	return &Service{
		loginFails:      make(map[string]*loginFailRecord),
		cleanupInterval: cleanupInterval,
	}
}

// Mu returns the internal mutex for tests that need to synchronize
// concurrent access to the loginFails map.
func (s *Service) Mu() *sync.Mutex { return &s.mu }

// SetLoginFail inserts or replaces a loginFailRecord entry using the
// exported LoginFailsEntry representation.
func (s *Service) SetLoginFail(username string, e LoginFailsEntry) {
	s.loginFails[username] = &loginFailRecord{
		count:        e.Count,
		lockedUntil:  e.LockedUntil,
		lastFailedAt: e.LastFailedAt,
	}
}

// HasLoginFail reports whether a loginFailRecord entry exists for username.
// It locks internally so it is safe to call while the cleanup goroutine is
// running (e.g. from the poll loop in TestCleanupGoroutine).
func (s *Service) HasLoginFail(username string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.loginFails[username]
	return ok
}

// SetCleanupInterval overrides the cleanup interval. Used by tests that
// need a shorter interval to verify the goroutine fires promptly.
func (s *Service) SetCleanupInterval(d time.Duration) { s.cleanupInterval = d }

// CleanupExpiredFails exposes the unexported cleanupExpiredFails method.
func (s *Service) CleanupExpiredFails() { s.cleanupExpiredFails() }

// StartCleanupLoop exposes the unexported startCleanupLoop method.
func (s *Service) StartCleanupLoop() { s.startCleanupLoop() }

// ValidateUsername exposes the unexported validateUsername function.
func ValidateUsername(username string) error { return validateUsername(username) }

// ValidatePassword exposes the unexported validatePassword function.
func ValidatePassword(pwd string) error { return validatePassword(pwd) }
