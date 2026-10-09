// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package i18n

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.uber.org/zap"
)

// writingTempDir creates a temp directory, writes the given files (map of
// relative path -> content), and returns the directory path. The cleanup
// function removes the directory.
func writingTempDir(t *testing.T, files map[string]string) (dir string, cleanup func()) {
	t.Helper()
	dir, err := os.MkdirTemp("", "i18n-loader-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	for path, content := range files {
		full := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			_ = os.RemoveAll(dir)
			t.Fatalf("MkdirAll(%s): %v", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			_ = os.RemoveAll(dir)
			t.Fatalf("WriteFile(%s): %v", full, err)
		}
	}
	return dir, func() { _ = os.RemoveAll(dir) }
}

func TestLoader_LoadToRegistry_TOML(t *testing.T) {
	dir, cleanup := writingTempDir(t, map[string]string{
		"en-US.toml": `[alert.metric]
title = "CPU Alert"

[alert.log]
title = "Log Alert"
`,
		"zh-Hans.toml": `[alert.metric]
title = "CPU 告警"
`,
	})
	defer cleanup()

	r := NewRegistry(zap.NewNop())
	loader := NewLoader(zap.NewNop())

	// Use os.DirFS to wrap the temp directory.
	fsys := osDirFS{root: dir}
	if err := loader.LoadToRegistry(fsys, r); err != nil {
		t.Fatalf("LoadToRegistry: %v", err)
	}

	if !r.Has("en-US") {
		t.Errorf("en-US locale should be registered")
	}
	if !r.Has("zh-Hans") {
		t.Errorf("zh-Hans locale should be registered")
	}

	tr := r.Resolve("en-US")
	if got := tr.T("alert.metric.title"); got != "CPU Alert" {
		t.Errorf("en-US T(alert.metric.title) = %q, want CPU Alert", got)
	}
	if got := tr.T("alert.log.title"); got != "Log Alert" {
		t.Errorf("en-US T(alert.log.title) = %q, want Log Alert", got)
	}

	trzh := r.Resolve("zh-Hans")
	if got := trzh.T("alert.metric.title"); got != "CPU 告警" {
		t.Errorf("zh-Hans T(alert.metric.title) = %q, want CPU 告警", got)
	}
	// zh-Hans doesn't have alert.log.title; with zh-Hans as the default locale,
	// the fallback chain terminates at zh-Hans itself, so the key is returned.
	if got := trzh.T("alert.log.title"); got != "alert.log.title" {
		t.Errorf("zh-Hans missing key T(alert.log.title) = %q, want alert.log.title", got)
	}
}

func TestLoader_LoadToRegistry_JSON(t *testing.T) {
	dir, cleanup := writingTempDir(t, map[string]string{
		"en-US.json": `{
			"alert": {
				"metric": {"title": "CPU Alert"}
			}
		}`,
	})
	defer cleanup()

	r := NewRegistry(zap.NewNop())
	loader := NewLoader(zap.NewNop())
	if err := loader.LoadToRegistry(osDirFS{root: dir}, r); err != nil {
		t.Fatalf("LoadToRegistry: %v", err)
	}

	tr := r.Resolve("en-US")
	if got := tr.T("alert.metric.title"); got != "CPU Alert" {
		t.Errorf("json T(alert.metric.title) = %q, want CPU Alert", got)
	}
}

func TestLoader_LoadToRegistry_SkipsInvalidStem(t *testing.T) {
	dir, cleanup := writingTempDir(t, map[string]string{
		"en-US.toml": `[a]
b = "c"`,
		"invalid.toml": `[a]
b = "c"`,
	})
	defer cleanup()

	r := NewRegistry(zap.NewNop())
	loader := NewLoader(zap.NewNop())
	if err := loader.LoadToRegistry(osDirFS{root: dir}, r); err != nil {
		t.Fatalf("LoadToRegistry: %v", err)
	}
	// "invalid" falls back to the default locale in Parse, so Has() can't
	// detect the skip. Verify the "invalid" tag is not in the locale list.
	for _, info := range r.List() {
		if info.Tag == "invalid" {
			t.Errorf("invalid stem should be skipped, but found in locale list")
		}
	}
}

func TestLoader_LoadToRegistry_NilRegistry(t *testing.T) {
	loader := NewLoader(zap.NewNop())
	if err := loader.LoadToRegistry(osDirFS{root: "."}, nil); err == nil {
		t.Errorf("LoadToRegistry(nil) should return error")
	}
}

func TestLoader_Watch_HotReload(t *testing.T) {
	dir, cleanup := writingTempDir(t, map[string]string{
		"en-US.toml": `[alert.metric]
title = "Old Title"`,
	})
	defer cleanup()

	r := NewRegistry(zap.NewNop())
	loader := NewLoader(zap.NewNop())

	// Initial load.
	if err := loader.LoadToRegistry(osDirFS{root: dir}, r); err != nil {
		t.Fatalf("LoadToRegistry: %v", err)
	}
	tr := r.Resolve("en-US")
	if got := tr.T("alert.metric.title"); got != "Old Title" {
		t.Fatalf("initial T() = %q, want Old Title", got)
	}

	// Start watcher.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	watchDone := make(chan struct{})
	go func() {
		_ = loader.Watch(ctx, dir, r)
		close(watchDone)
	}()

	// Give watcher time to register.
	time.Sleep(100 * time.Millisecond)

	// Modify file.
	newContent := `[alert.metric]
title = "New Title"`
	if err := os.WriteFile(filepath.Join(dir, "en-US.toml"), []byte(newContent), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Wait for hot-reload (with timeout).
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		tr := r.Resolve("en-US")
		if tr.T("alert.metric.title") == "New Title" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	tr2 := r.Resolve("en-US")
	if got := tr2.T("alert.metric.title"); got != "New Title" {
		t.Errorf("after hot-reload T() = %q, want New Title", got)
	}

	cancel()
	select {
	case <-watchDone:
	case <-time.After(1 * time.Second):
		t.Errorf("Watch did not exit after context cancel")
	}
}

func TestLoader_Watch_ParseFailureKeepsOldBundle(t *testing.T) {
	dir, cleanup := writingTempDir(t, map[string]string{
		"en-US.toml": `[alert.metric]
title = "Original"`,
	})
	defer cleanup()

	r := NewRegistry(zap.NewNop())
	loader := NewLoader(zap.NewNop())
	if err := loader.LoadToRegistry(osDirFS{root: dir}, r); err != nil {
		t.Fatalf("LoadToRegistry: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = loader.Watch(ctx, dir, r) }()
	time.Sleep(100 * time.Millisecond)

	// Write invalid TOML.
	if err := os.WriteFile(filepath.Join(dir, "en-US.toml"), []byte("not = valid = toml = ="), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	time.Sleep(500 * time.Millisecond)

	tr := r.Resolve("en-US")
	if got := tr.T("alert.metric.title"); got != "Original" {
		t.Errorf("after parse failure T() = %q, want Original (kept old bundle)", got)
	}
}

func TestLoader_Watch_EmptyDir(t *testing.T) {
	loader := NewLoader(zap.NewNop())
	r := NewRegistry(zap.NewNop())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := loader.Watch(ctx, "", r)
	if err == nil {
		t.Errorf("Watch with empty dir should return error")
	}
}

func TestLoader_Watch_NilRegistry(t *testing.T) {
	loader := NewLoader(zap.NewNop())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := loader.Watch(ctx, "/tmp", nil); err == nil {
		t.Errorf("Watch with nil registry should return error")
	}
}
