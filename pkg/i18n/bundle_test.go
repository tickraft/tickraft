// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package i18n

import "testing"

func TestMessageMap_LookupAndKeys(t *testing.T) {
	m := NewMessageMap("zh-Hans", map[string]string{
		"alert.metric.title": "CPU 使用率告警",
		"alert.metric.desc":  "CPU 使用率超过阈值",
	})

	if v, ok := m.Lookup("alert.metric.title"); !ok || v != "CPU 使用率告警" {
		t.Errorf("Lookup(missing key) = %q,%v, want non-empty", v, ok)
	}
	if _, ok := m.Lookup("nonexistent"); ok {
		t.Errorf("Lookup(nonexistent) should return ok=false")
	}

	keys := m.Keys()
	if len(keys) != 2 {
		t.Errorf("Keys() returned %d items, want 2", len(keys))
	}
}

func TestMessageMap_SetAddsKey(t *testing.T) {
	m := NewMessageMap("en-US", nil)
	m.Set("a", "b")
	if v, ok := m.Lookup("a"); !ok || v != "b" {
		t.Errorf("Set/Lookup round-trip failed: %q,%v", v, ok)
	}
}

func TestMessageMap_Merge(t *testing.T) {
	dst := NewMessageMap("en-US", map[string]string{"a": "1", "b": "2"})
	src := NewMessageMap("en-US", map[string]string{"b": "20", "c": "3"})
	dst.Merge(src)

	if v, _ := dst.Lookup("a"); v != "1" {
		t.Errorf("Merge should preserve existing keys: a = %q", v)
	}
	if v, _ := dst.Lookup("b"); v != "20" {
		t.Errorf("Merge should overwrite shared keys: b = %q, want 20", v)
	}
	if v, _ := dst.Lookup("c"); v != "3" {
		t.Errorf("Merge should add new keys: c = %q", v)
	}
}

func TestMessageMap_Locale(t *testing.T) {
	m := NewMessageMap("ja-JP", nil)
	if got := m.Locale(); got != "ja-JP" {
		t.Errorf("Locale() = %q, want ja-JP", got)
	}
}

func TestMessageMap_InputMapNotMutated(t *testing.T) {
	src := map[string]string{"a": "1"}
	m := NewMessageMap("en-US", src)
	m.Set("a", "2")
	if src["a"] != "1" {
		t.Errorf("NewMessageMap should copy input: src[a] = %q, want 1", src["a"])
	}
}

func TestEmptyBundle(t *testing.T) {
	e := emptyBundle{tag: "en-US"}
	if _, ok := e.Lookup("x"); ok {
		t.Errorf("emptyBundle.Lookup should always return false")
	}
	if got := e.Locale(); got != "en-US" {
		t.Errorf("emptyBundle.Locale = %q", got)
	}
	if keys := e.Keys(); keys != nil {
		t.Errorf("emptyBundle.Keys should be nil, got %v", keys)
	}
}
