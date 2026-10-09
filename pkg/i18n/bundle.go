// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package i18n

import (
	"maps"
	"sync"
)

// Bundle is an immutable store of message keys for a single locale.
//
// Implementations must be safe for concurrent read access. The MessageMap
// implementation provided in this package is the canonical in-memory bundle
// used by the Registry; alternative implementations may wrap remote stores or
// embedded resources.
type Bundle interface {
	// Lookup returns the message for the given key and a boolean indicating
	// whether the key was present. When ok is false, the returned string is
	// empty and the caller is responsible for fallback handling.
	Lookup(key string) (value string, ok bool)
	// Keys returns the set of keys contained in the bundle. The order is
	// unspecified and callers must not rely on it. The returned slice is a
	// copy and may be modified by the caller.
	Keys() []string
	// Locale returns the locale tag this bundle belongs to (e.g. "en-US",
	// "zh-Hans").
	Locale() string
}

// MessageMap is the default in-memory Bundle implementation backed by a
// map[string]string protected by a sync.RWMutex. The zero value is not a
// valid bundle; use NewMessageMap to construct one.
type MessageMap struct {
	tag    string
	mu     sync.RWMutex
	values map[string]string
}

// NewMessageMap creates a MessageMap for the given locale tag pre-populated
// with the provided key/value pairs. The input map is copied so the caller
// may freely mutate it after construction.
func NewMessageMap(tag string, values map[string]string) *MessageMap {
	cp := make(map[string]string, len(values))
	maps.Copy(cp, values)
	return &MessageMap{tag: tag, values: cp}
}

// Set adds or replaces a single key. This is primarily used by the hot-reload
// path to incrementally update a bundle without rebuilding it from scratch.
func (m *MessageMap) Set(key, value string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.values[key] = value
}

// Merge copies all key/value pairs from src into the receiver, overwriting
// existing keys. It is used by the loader to merge extended resource packs
// onto the base packs at startup.
func (m *MessageMap) Merge(src Bundle) {
	if src == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, k := range src.Keys() {
		if v, ok := src.Lookup(k); ok {
			m.values[k] = v
		}
	}
}

// Lookup implements Bundle.
func (m *MessageMap) Lookup(key string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.values[key]
	return v, ok
}

// Keys implements Bundle.
func (m *MessageMap) Keys() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]string, 0, len(m.values))
	for k := range m.values {
		out = append(out, k)
	}
	return out
}

// Locale implements Bundle.
func (m *MessageMap) Locale() string {
	return m.tag
}

// emptyBundle is a Bundle with no keys, used as a sentinel by the Registry
// when a locale is registered but has no resources loaded yet.
type emptyBundle struct{ tag string }

func (e emptyBundle) Lookup(string) (string, bool) { return "", false }
func (e emptyBundle) Keys() []string               { return nil }
func (e emptyBundle) Locale() string               { return e.tag }
