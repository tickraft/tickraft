// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package channel

import (
	"strings"
	"sync"

	"github.com/tickraft/tickraft/pkg/prism/alert"
)

// Factory constructs an alert.Channel from the given Config.
//
// This is the SPI type implemented by downstream repositories to inject
// custom channel implementations. A factory is associated with a channel
// type name via Register and is invoked by LoadChannels when the
// corresponding type is encountered in a channel config file.
//
// Factories must be safe for concurrent use; LoadChannels may invoke the
// same factory from multiple goroutines. Implementations should perform
// all heavyweight construction (HTTP clients, SMTP connections, breaker
// wiring) here so that the returned Channel is ready to Send.
type Factory func(cfg Config) (alert.Channel, error)

var (
	// registryMu guards registry. A write lock is held during Register;
	// a read lock is held during lookupFactory. The lock is never held
	// while invoking a factory, so a slow factory cannot block
	// registration of other types.
	registryMu sync.RWMutex
	// registry maps lowercased channel type names to their factories.
	registry = make(map[string]Factory)
)

// Register registers a Factory for the given channel type name.
//
// This is the SPI entry point for downstream repositories to inject
// custom channel implementations at program startup. The typeName is
// normalized to lowercase and matched case-insensitively against the
// Type field of channel config entries. Callers may pass any casing
// (e.g. "Webhook", "WEBHOOK", "webhook") — all are stored under the
// same canonical lowercase key.
//
// Register is safe for concurrent calls. If a factory is already
// registered for typeName, it is overwritten; the last registration
// wins. This allows downstream repositories to override built-in
// channels by registering a factory for "webhook" or "email".
//
// Register must be called during process initialization (typically from
// an init() function or an explicit setup phase) before LoadChannels is
// invoked; mutating the registry concurrently with LoadChannels is safe
// but may produce non-deterministic results for in-flight loads.
func Register(name string, factory Factory) {
	name = strings.ToLower(name)
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[name] = factory
}

// LookupFactory returns the Factory registered for typeName, or
// nil if no factory is registered. The lookup is case-sensitive; callers
// should normalize typeName (e.g. via strings.ToLower) before calling.
//
// The returned factory is a copy of the map value, so it remains valid
// after the read lock is released. Callers may invoke the factory
// without holding registryMu.
func LookupFactory(name string) Factory {
	name = strings.ToLower(name)
	registryMu.RLock()
	defer registryMu.RUnlock()
	return registry[name]
}

// ResetRegistryForTest clears all registered factories. It is intended
// for test isolation only and must never be called from production code.
func ResetRegistryForTest() {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry = make(map[string]Factory)
}
