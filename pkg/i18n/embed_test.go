// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package i18n

import (
	"testing"

	"go.uber.org/zap"
)

func TestEmbeddedFS_LoadsAllBuiltinLocales(t *testing.T) {
	loader := NewLoader(zap.NewNop())
	r := NewRegistry(zap.NewNop())

	if err := loader.LoadToRegistry(EmbeddedFS(), r); err != nil {
		t.Fatalf("LoadToRegistry(EmbeddedFS): %v", err)
	}

	expected := []string{"en-US", "zh-Hans"}
	for _, tag := range expected {
		if !r.Has(tag) {
			t.Errorf("embedded FS missing locale %q", tag)
		}
	}
}

func TestEmbeddedFS_EnglishTranslations(t *testing.T) {
	loader := NewLoader(zap.NewNop())
	r := NewRegistry(zap.NewNop())
	if err := loader.LoadToRegistry(EmbeddedFS(), r); err != nil {
		t.Fatalf("LoadToRegistry: %v", err)
	}

	tr := r.Resolve("en-US")
	if got := tr.T("_meta.native_name"); got != "English" {
		t.Errorf("en-US _meta.native_name = %q, want English", got)
	}
	if got := tr.T("level.critical"); got != "Critical" {
		t.Errorf("en-US level.critical = %q, want Critical", got)
	}
	if got := tr.T("time.format"); got != "Jan 2, 2006 3:04 PM" {
		t.Errorf("en-US time.format = %q", got)
	}
}

func TestEmbeddedFS_ChineseTranslations(t *testing.T) {
	loader := NewLoader(zap.NewNop())
	r := NewRegistry(zap.NewNop())
	if err := loader.LoadToRegistry(EmbeddedFS(), r); err != nil {
		t.Fatalf("LoadToRegistry: %v", err)
	}

	tr := r.Resolve("zh-Hans")
	if got := tr.T("_meta.native_name"); got != displayZhHans {
		t.Errorf("zh-Hans _meta.native_name = %q, want 简体中文", got)
	}
	if got := tr.T("level.critical"); got != "严重" {
		t.Errorf("zh-Hans level.critical = %q, want 严重", got)
	}
	if got := tr.T("time.format"); got != "2006-01-02 15:04:05" {
		t.Errorf("zh-Hans time.format = %q", got)
	}
}

func TestEmbeddedFS_StyleVariants(t *testing.T) {
	loader := NewLoader(zap.NewNop())
	r := NewRegistry(zap.NewNop())
	if err := loader.LoadToRegistry(EmbeddedFS(), r); err != nil {
		t.Fatalf("LoadToRegistry: %v", err)
	}

	tr := r.Resolve("en-US")
	// The alert.metric.title key has three style variants.
	concise, ok := tr.Lookup("alert.metric.title.concise", false)
	if !ok {
		t.Errorf("alert.metric.title.concise key missing")
	}
	if concise == "" {
		t.Errorf("alert.metric.title.concise should be non-empty")
	}

	detailed, ok := tr.Lookup("alert.metric.title.detailed", false)
	if !ok {
		t.Errorf("alert.metric.title.detailed key missing")
	}
	if detailed == "" {
		t.Errorf("alert.metric.title.detailed should be non-empty")
	}

	technical, ok := tr.Lookup("alert.metric.title.technical", false)
	if !ok {
		t.Errorf("alert.metric.title.technical key missing")
	}
	if technical == "" {
		t.Errorf("alert.metric.title.technical should be non-empty")
	}
}

func TestEmbeddedFS_NativeNamesInList(t *testing.T) {
	loader := NewLoader(zap.NewNop())
	r := NewRegistry(zap.NewNop())
	if err := loader.LoadToRegistry(EmbeddedFS(), r); err != nil {
		t.Fatalf("LoadToRegistry: %v", err)
	}

	infos := r.List()
	nativeNames := make(map[string]string)
	for _, info := range infos {
		nativeNames[info.Tag] = info.NativeName
	}

	if nativeNames["en-US"] != "English" {
		t.Errorf("en-US native name = %q, want English", nativeNames["en-US"])
	}
	if nativeNames["zh-Hans"] != displayZhHans {
		t.Errorf("zh-Hans native name = %q, want 简体中文", nativeNames["zh-Hans"])
	}
}
