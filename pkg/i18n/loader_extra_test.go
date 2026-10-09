// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package i18n

import (
	"context"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestLoader_Load_NoRegistry(t *testing.T) {
	dir, cleanup := writingTempDir(t, map[string]string{
		"en-US.toml": `[a]
b = "c"`,
	})
	defer cleanup()

	loader := NewLoader(zap.NewNop())
	// Load just walks and logs; with no target it's a no-op on the registry.
	err := loader.Load(osDirFS{root: dir})
	if err != nil {
		t.Errorf("Load returned unexpected error: %v", err)
	}
}

func TestLoader_LoadInto_MergesIntoTarget(t *testing.T) {
	dir, cleanup := writingTempDir(t, map[string]string{
		"en-US.toml": `[a]
b = "merged"`,
	})
	defer cleanup()

	loader := NewLoader(zap.NewNop())
	target := NewMessageMap("en-US", map[string]string{"existing": "kept"})
	err := loader.LoadInto(osDirFS{root: dir}, target)
	if err != nil {
		t.Errorf("LoadInto returned unexpected error: %v", err)
	}
	// LoadInto with a *MessageMap target should merge into it.
	if v, ok := target.Lookup("existing"); !ok || v != "kept" {
		t.Errorf("LoadInto should preserve existing keys: existing = %q, ok=%v", v, ok)
	}
}

func TestLoader_LoadInto_NilTarget(t *testing.T) {
	dir, cleanup := writingTempDir(t, map[string]string{
		"en-US.toml": `[a]
b = "c"`,
	})
	defer cleanup()

	loader := NewLoader(zap.NewNop())
	// LoadInto with nil target should not panic.
	if err := loader.LoadInto(osDirFS{root: dir}, nil); err != nil {
		t.Errorf("LoadInto(nil) returned unexpected error: %v", err)
	}
}

func TestLoader_NewLoader_NilLogger(t *testing.T) {
	l := NewLoader(nil)
	if l == nil {
		t.Errorf("NewLoader(nil) should not return nil")
	}
}

func TestLoader_LoadToRegistry_EmptyDir(t *testing.T) {
	dir, cleanup := writingTempDir(t, nil)
	defer cleanup()

	r := NewRegistry(zap.NewNop())
	loader := NewLoader(zap.NewNop())
	if err := loader.LoadToRegistry(osDirFS{root: dir}, r); err != nil {
		t.Errorf("LoadToRegistry on empty dir should not error: %v", err)
	}
	// Only the default locale should be registered.
	infos := r.List()
	if len(infos) != 1 {
		t.Errorf("expected 1 locale (default), got %d", len(infos))
	}
}

func TestLoader_LoadToRegistry_SkipsNonResourceFiles(t *testing.T) {
	dir, cleanup := writingTempDir(t, map[string]string{
		"en-US.toml": `[a]
b = "c"`,
		"readme.md": `not a resource`,
		"notes.txt": `not a resource`,
	})
	defer cleanup()

	r := NewRegistry(zap.NewNop())
	loader := NewLoader(zap.NewNop())
	if err := loader.LoadToRegistry(osDirFS{root: dir}, r); err != nil {
		t.Fatalf("LoadToRegistry: %v", err)
	}
	if !r.Has("en-US") {
		t.Errorf("en-US should be registered")
	}
}

func TestNewRegistry_NilLogger(t *testing.T) {
	r := NewRegistry(nil)
	if r == nil {
		t.Errorf("NewRegistry(nil) should not return nil")
	}
	if !r.Has(DefaultLocale) {
		t.Errorf("default locale should always be registered")
	}
}

func TestRegistry_Register_LowercasesTag(t *testing.T) {
	r := NewRegistry(zap.NewNop())
	r.Register("ZH-CN", NewMessageMap("zh-Hans", map[string]string{"a": "b"}))
	if !r.Has("zh-cn") {
		t.Errorf("Register should lowercase the tag")
	}
	if !r.Has("ZH-CN") {
		t.Errorf("Has should match case-insensitively after lowercasing")
	}
}

func TestRegistry_Resolve_NilSafe(t *testing.T) {
	r := NewRegistry(zap.NewNop())
	// Resolve an unknown locale; should not panic.
	tr := r.Resolve("xx-yy")
	if tr == nil {
		t.Errorf("Resolve should never return nil")
	}
	if got := tr.Locale(); got != DefaultLocale {
		t.Errorf("Resolve(unknown) locale = %q, want %q", got, DefaultLocale)
	}
}

func TestTranslator_Direction(t *testing.T) {
	r := NewRegistry(zap.NewNop())
	r.Register("ar", NewMessageMap("ar", map[string]string{"a": "b"}))
	tr := r.Resolve("ar")
	if got := tr.Direction(); got != RTL {
		t.Errorf("ar Direction = %q, want rtl", got)
	}
}

func TestTranslator_LookupAllowDefault(t *testing.T) {
	r := NewRegistry(zap.NewNop())
	r.Register("zh-Hans", NewMessageMap("zh-Hans", map[string]string{"a": "中文"}))
	r.Register("fr", NewMessageMap("fr", map[string]string{}))
	tr := r.Resolve("fr")

	// Lookup with allowDefault=false should not find the key in fr.
	if _, ok := tr.Lookup("a", false); ok {
		t.Errorf("Lookup(a, allowDefault=false) should return ok=false for fr")
	}
	// Lookup with allowDefault=true should find the key in zh-Hans (default).
	if v, ok := tr.Lookup("a", true); !ok || v != "中文" {
		t.Errorf("Lookup(a, allowDefault=true) = %q,%v, want 中文,true", v, ok)
	}
}

func TestRegistry_List_IncludesNativeNameFallback(t *testing.T) {
	r := NewRegistry(zap.NewNop())
	// Register a locale without _meta.native_name; should fall back to tag.
	r.Register("ja", NewMessageMap("ja", map[string]string{}))
	infos := r.List()
	for _, info := range infos {
		if info.Tag == "ja" && info.NativeName != "ja" {
			t.Errorf("ja native name should fall back to tag, got %q", info.NativeName)
		}
	}
}

func TestOsDirFS_Open(t *testing.T) {
	dir, cleanup := writingTempDir(t, map[string]string{
		"en-US.toml": `[a]
b = "c"`,
	})
	defer cleanup()

	fsys := osDirFS{root: dir}
	f, err := fsys.Open("en-US.toml")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	_ = f.Close()
}

func TestOpenOSFile_NonExistent(t *testing.T) {
	_, err := openOSFile("/nonexistent/path/file.txt")
	if err == nil {
		t.Errorf("openOSFile on nonexistent path should error")
	}
}

func TestMissingVariablePlaceholder_Format(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"host", "[MISSING:host]"},
		{"metric_name", "[MISSING:metric_name]"},
		{"", "[MISSING:]"},
	}
	for _, c := range cases {
		if got := MissingVariablePlaceholder(c.in); got != c.want {
			t.Errorf("MissingVariablePlaceholder(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestParse_RegionOnlyNoLanguage(t *testing.T) {
	// "--CN" trims to "cn" which is a valid 2-letter subtag syntactically,
	// so it is accepted as a language-only locale rather than falling back.
	loc := Parse("--CN")
	if loc.Language != "cn" {
		t.Errorf("Parse(--CN) language = %q, want cn", loc.Language)
	}
}

func TestParse_MultipleDashes(t *testing.T) {
	loc := Parse("zh--CN")
	// After trimming, the tag is "zh--cn". The language is valid ("zh"),
	// so it should be accepted. Empty subtags between dashes are skipped.
	if loc.Language != "zh" {
		t.Errorf("Parse(zh--CN) language = %q, want zh", loc.Language)
	}
}

func TestRegistry_ReRegisterDefault(t *testing.T) {
	r := NewRegistry(zap.NewNop())
	r.Register("en-US", NewMessageMap("en-US", map[string]string{"a": "1"}))
	r.Register("en-US", NewMessageMap("en-US", map[string]string{"a": "2"}))
	tr := r.Resolve("en-US")
	if got := tr.T("a"); got != "2" {
		t.Errorf("re-register default: T(a) = %q, want 2", got)
	}
}

func TestLoader_Watch_WatcherExitsOnContextCancel(t *testing.T) {
	dir, cleanup := writingTempDir(t, map[string]string{
		"en-US.toml": `[a]
b = "c"`,
	})
	defer cleanup()

	r := NewRegistry(zap.NewNop())
	loader := NewLoader(zap.NewNop())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = loader.Watch(ctx, dir, r)
		close(done)
	}()

	cancel()
	select {
	case <-done:
		// success
	case <-time.After(2 * time.Second):
		t.Errorf("Watch did not exit after context cancel")
	}
}

func TestFlattenAny_NestedMaps(t *testing.T) {
	in := map[string]any{
		"alert": map[string]any{
			"metric": map[string]any{
				"title": "CPU Alert",
				"value": 42,
			},
		},
		"flat": "value",
	}
	out := make(map[string]string)
	flattenAny("", in, out)

	if out["alert.metric.title"] != "CPU Alert" {
		t.Errorf("flattenAny nested title = %q", out["alert.metric.title"])
	}
	if out["alert.metric.value"] != "42" {
		t.Errorf("flattenAny nested value = %q, want 42", out["alert.metric.value"])
	}
	if out["flat"] != "value" {
		t.Errorf("flattenAny flat = %q", out["flat"])
	}
}

func TestParseJSON_InvalidJSON(t *testing.T) {
	_, err := parseJSON([]byte(`{invalid`))
	if err == nil {
		t.Errorf("parseJSON on invalid JSON should error")
	}
}

func TestParseResourceBytes_UnsupportedExt(t *testing.T) {
	_, err := parseResourceBytes([]byte(`x`), ".yaml")
	if err == nil {
		t.Errorf("parseResourceBytes with unsupported ext should error")
	}
}
