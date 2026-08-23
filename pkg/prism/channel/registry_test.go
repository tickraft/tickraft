// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package channel

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/tickraft/tickraft/pkg/prism/alert"
)

// ---------------------------------------------------------------------------
// Test fakes
// ---------------------------------------------------------------------------

// fakeChannel is a minimal alert.Channel implementation used to verify
// that the registry dispatches to registered factories.
type fakeChannel struct {
	name string
}

// Send implements alert.Channel. It is a no-op for tests.
func (f *fakeChannel) Send(_ context.Context, _ alert.Event) error {
	return nil
}

// Name implements alert.Channel.
func (f *fakeChannel) Name() string { return f.name }

// capturingFactory returns a Factory that channels each invocation
// (so tests can assert the factory was actually called) and returns a
// fakeChannel whose name is derived from the config Type.
func capturingFactory(counter *int32) Factory {
	return func(cfg Config) (alert.Channel, error) {
		atomic.AddInt32(counter, 1)
		return &fakeChannel{name: cfg.Type}, nil
	}
}

// clearRegistry clears all registered factories. It is intended
// for test isolation only and must never be called from production code.
func clearRegistry() {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry = make(map[string]Factory)
}

// ---------------------------------------------------------------------------
// Register / LookupFactory: basic operations
// ---------------------------------------------------------------------------

// TestRegisterAndLookup verifies that a factory registered under a type
// name is retrievable via LookupFactory, and that an unregistered type
// returns nil.
func TestRegisterAndLookup(t *testing.T) {
	t.Cleanup(clearRegistry)

	var calls int32
	Register("sms", capturingFactory(&calls))

	if f := LookupFactory("sms"); f == nil {
		t.Fatal("expected factory for sms, got nil")
	}
	if f := LookupFactory("nonexistent"); f != nil {
		t.Error("expected nil for unregistered type, got non-nil")
	}
}

// TestRegisterCaseInsensitive verifies that Register normalizes the type
// name to lowercase so that lookups are case-insensitive.
func TestRegisterCaseInsensitive(t *testing.T) {
	t.Cleanup(clearRegistry)

	var calls int32
	Register("Slack", capturingFactory(&calls))

	// Lookup with different casings; LookupFactory is case-sensitive,
	// so the caller must normalize. Register stores lowercase.
	if f := LookupFactory("slack"); f == nil {
		t.Error("expected factory for lowercase 'slack', got nil")
	}
	if f := LookupFactory("Slack"); f != nil {
		t.Error("expected nil for 'Slack' (not normalized), got non-nil")
	}
}

// TestRegisterOverwrite verifies that registering a new factory for an
// existing type name overwrites the previous one.
func TestRegisterOverwrite(t *testing.T) {
	t.Cleanup(clearRegistry)

	var first, second int32
	Register("overwritable", capturingFactory(&first))
	Register("overwritable", capturingFactory(&second))

	f := LookupFactory("overwritable")
	if f == nil {
		t.Fatal("expected factory, got nil")
	}
	_, _ = f(Config{Type: "overwritable"})

	if first != 0 {
		t.Errorf("first factory call count: got %d, want 0", first)
	}
	if second != 1 {
		t.Errorf("second factory call count: got %d, want 1", second)
	}
}

// ---------------------------------------------------------------------------
// Register: concurrent safety
// ---------------------------------------------------------------------------

// TestRegisterConcurrentSafe verifies that concurrent Register calls do
// not trigger the race detector. Run with -race to be meaningful.
func TestRegisterConcurrentSafe(t *testing.T) {
	t.Cleanup(clearRegistry)

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := range goroutines {
		go func(n int) {
			defer wg.Done()
			typeName := fmt.Sprintf("concurrent-%d", n)
			Register(typeName, capturingFactory(new(int32)))
		}(i)
	}
	wg.Wait()

	// All goroutines should have registered their factory; verify by
	// looking up a sample.
	if f := LookupFactory("concurrent-0"); f == nil {
		t.Error("expected factory for concurrent-0, got nil")
	}
	if f := LookupFactory("concurrent-49"); f == nil {
		t.Error("expected factory for concurrent-49, got nil")
	}
}

// TestResetRegistryForTest verifies that ResetRegistryForTest clears all
// registered factories.
func TestResetRegistryForTest(t *testing.T) {
	t.Cleanup(clearRegistry)

	var calls int32
	Register("temp", capturingFactory(&calls))
	if f := LookupFactory("temp"); f == nil {
		t.Fatal("expected factory before reset, got nil")
	}

	ResetRegistryForTest()

	if f := LookupFactory("temp"); f != nil {
		t.Error("expected nil after reset, got non-nil")
	}
}
