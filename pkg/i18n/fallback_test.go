// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package i18n

import (
	"testing"

	"go.uber.org/zap"
)

func TestResolveKey_PrimaryBundle(t *testing.T) {
	r := NewRegistry(zap.NewNop())
	r.Register("zh-Hans", NewMessageMap("zh-Hans", map[string]string{"a": "中文"}))
	tr := r.Resolve("zh-Hans")
	if got := ResolveKey(tr, "a", zap.NewNop()); got != "中文" {
		t.Errorf("ResolveKey(primary) = %q, want 中文", got)
	}
}

func TestResolveKey_DefaultFallback(t *testing.T) {
	r := NewRegistry(zap.NewNop())
	r.Register("zh-Hans", NewMessageMap("zh-Hans", map[string]string{"a": "中文"}))
	tr := r.Resolve("fr-FR")
	if got := ResolveKey(tr, "a", zap.NewNop()); got != "中文" {
		t.Errorf("ResolveKey(fallback) = %q, want 中文", got)
	}
}

func TestResolveKey_MissingReturnsKey(t *testing.T) {
	r := NewRegistry(zap.NewNop())
	tr := r.Resolve("en-US")
	if got := ResolveKey(tr, "missing.key", zap.NewNop()); got != "missing.key" {
		t.Errorf("ResolveKey(missing) = %q, want missing.key", got)
	}
}

func TestResolveKey_NilTranslator(t *testing.T) {
	if got := ResolveKey(nil, "a", zap.NewNop()); got != "a" {
		t.Errorf("ResolveKey(nil translator) = %q, want a", got)
	}
}

func TestMissingVariablePlaceholder(t *testing.T) {
	if got := MissingVariablePlaceholder("host"); got != "[MISSING:host]" {
		t.Errorf("MissingVariablePlaceholder(host) = %q, want [MISSING:host]", got)
	}
}
