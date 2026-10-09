// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package i18n

import (
	"sync"
	"testing"

	"go.uber.org/zap"
)

func TestRegistry_DefaultLocaleAlwaysRegistered(t *testing.T) {
	r := NewRegistry(zap.NewNop())
	if !r.Has(DefaultLocale) {
		t.Errorf("default locale must always be registered")
	}
}

func TestRegistry_RegisterAndResolveExact(t *testing.T) {
	r := NewRegistry(zap.NewNop())
	r.Register("zh-Hans", NewMessageMap("zh-Hans", map[string]string{
		"alert.metric.title": "CPU 告警",
	}))

	tr := r.Resolve("zh-Hans")
	if got := tr.T("alert.metric.title"); got != "CPU 告警" {
		t.Errorf("Resolve exact T() = %q, want CPU 告警", got)
	}
	if got := tr.Locale(); got != "zh-Hans" {
		t.Errorf("Locale() = %q, want zh-Hans", got)
	}
}

func TestRegistry_ResolveLanguageFallback(t *testing.T) {
	r := NewRegistry(zap.NewNop())
	// Register only language-only "zh", request "zh-TW" (not the default).
	r.Register("zh", NewMessageMap("zh", map[string]string{
		"alert.metric.title": "中文告警",
	}))

	tr := r.Resolve("zh-TW")
	if got := tr.T("alert.metric.title"); got != "中文告警" {
		t.Errorf("Resolve language fallback T() = %q, want 中文告警", got)
	}
}

func TestRegistry_ResolveDefaultFallback(t *testing.T) {
	r := NewRegistry(zap.NewNop())
	r.Register("zh-Hans", NewMessageMap("zh-Hans", map[string]string{
		"alert.metric.title": "CPU 告警",
	}))

	// Request a locale not registered at all.
	tr := r.Resolve("fr-FR")
	if got := tr.T("alert.metric.title"); got != "CPU 告警" {
		t.Errorf("Resolve default fallback T() = %q, want CPU 告警", got)
	}
}

func TestRegistry_ResolveKeyFallback(t *testing.T) {
	r := NewRegistry(zap.NewNop())
	r.Register("zh-Hans", NewMessageMap("zh-Hans", map[string]string{
		"alert.metric.title": "CPU 告警",
	}))
	r.Register("fr", NewMessageMap("fr", map[string]string{
		// No alert.metric.title here; should fall back to zh-Hans default.
	}))

	tr := r.Resolve("fr")
	if got := tr.T("alert.metric.title"); got != "CPU 告警" {
		t.Errorf("Key fallback T() = %q, want CPU 告警", got)
	}
}

func TestRegistry_ResolveMissingKeyReturnsKey(t *testing.T) {
	r := NewRegistry(zap.NewNop())
	tr := r.Resolve("en-US")
	if got := tr.T("nonexistent.key"); got != "nonexistent.key" {
		t.Errorf("Missing key T() = %q, want the key itself", got)
	}
}

func TestRegistry_Unregister(t *testing.T) {
	r := NewRegistry(zap.NewNop())
	r.Register("ja-JP", NewMessageMap("ja-JP", map[string]string{"a": "b"}))
	if !r.Has("ja-JP") {
		t.Errorf("Has(ja-JP) should be true after Register")
	}
	r.Unregister("ja-JP")
	if r.Has("ja-JP") {
		t.Errorf("Has(ja-JP) should be false after Unregister")
	}
}

func TestRegistry_UnregisterDefaultIsNoop(t *testing.T) {
	r := NewRegistry(zap.NewNop())
	r.Unregister(DefaultLocale)
	if !r.Has(DefaultLocale) {
		t.Errorf("default locale must remain registered after Unregister")
	}
}

func TestRegistry_RegisterNilBundleIgnored(t *testing.T) {
	r := NewRegistry(zap.NewNop())
	r.Register("ja-JP", nil)
	if r.Has("ja-JP") {
		t.Errorf("nil bundle should not be registered")
	}
}

func TestRegistry_ReRegisterReplacesBundle(t *testing.T) {
	r := NewRegistry(zap.NewNop())
	r.Register("zh-Hans", NewMessageMap("zh-Hans", map[string]string{"a": "1"}))
	r.Register("zh-Hans", NewMessageMap("zh-Hans", map[string]string{"a": "2"}))
	tr := r.Resolve("zh-Hans")
	if got := tr.T("a"); got != "2" {
		t.Errorf("Re-register should replace bundle: T() = %q, want 2", got)
	}
}

func TestRegistry_ListReturnsAllLocales(t *testing.T) {
	r := NewRegistry(zap.NewNop())
	r.Register("en-US", NewMessageMap("en-US", map[string]string{}))
	r.Register("zh-Hans", NewMessageMap("zh-Hans", map[string]string{
		"_meta.native_name": displayZhHans,
	}))
	r.Register("ar", NewMessageMap("ar", map[string]string{}))

	infos := r.List()
	tags := make(map[string]bool, len(infos))
	for _, info := range infos {
		tags[info.Tag] = true
	}
	if !tags["en-US"] || !tags["zh-Hans"] || !tags["ar"] {
		t.Errorf("List() missing expected locales, got %v", tags)
	}

	for _, info := range infos {
		if info.Tag == "zh-Hans" && info.NativeName != displayZhHans {
			t.Errorf("zh-Hans native name = %q, want 简体中文", info.NativeName)
		}
		if info.Tag == "ar" && info.Direction != RTL {
			t.Errorf("ar direction = %q, want rtl", info.Direction)
		}
		if info.Tag == "en-US" && info.Direction != LTR {
			t.Errorf("en-US direction = %q, want ltr", info.Direction)
		}
	}
}

func TestRegistry_ListIsSortedByTag(t *testing.T) {
	r := NewRegistry(zap.NewNop())
	r.Register("zh-Hans", NewMessageMap("zh-Hans", nil))
	r.Register("ar", NewMessageMap("ar", nil))
	r.Register("ja-JP", NewMessageMap("ja-JP", nil))

	infos := r.List()
	for i := 1; i < len(infos); i++ {
		if infos[i-1].Tag > infos[i].Tag {
			t.Errorf("List() not sorted: %q before %q", infos[i-1].Tag, infos[i].Tag)
		}
	}
}

func TestRegistry_ConcurrentRegisterResolve(t *testing.T) {
	r := NewRegistry(zap.NewNop())
	const n = 50
	var wg sync.WaitGroup
	wg.Add(n * 2)

	for i := range n {
		go func(i int) {
			defer wg.Done()
			tag := "zh-Hans"
			if i%2 == 0 {
				tag = "en-US"
			}
			r.Register(tag, NewMessageMap(tag, map[string]string{"a": "b"}))
		}(i)
		go func(i int) {
			defer wg.Done()
			_ = r.Resolve("zh-Hans").T("a")
		}(i)
	}
	wg.Wait()
}
