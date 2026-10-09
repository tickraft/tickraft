// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package channel

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/tickraft/tickraft/pkg/prism/alert"
)

// ---------------------------------------------------------------------------
// Test fakes
// ---------------------------------------------------------------------------

// fakeChannel is a minimal alert.Channel implementation used to verify
// that the registry builds registered types.
type fakeChannel struct {
	name string
}

// Send implements alert.Channel. It is a no-op for tests.
func (f *fakeChannel) Send(_ context.Context, _ alert.Event) error {
	return nil
}

// Name implements alert.Channel.
func (f *fakeChannel) Name() string { return f.name }

// errGuardDenied is the sentinel a fake TypeGuard returns to simulate a
// licensing rejection.
var errGuardDenied = errors.New("guard: type not licensed")

// ---------------------------------------------------------------------------
// Registry: RegisterType / LookupType
// ---------------------------------------------------------------------------

// withPristineRegistry resets the type registry for the duration of a test
// and restores an empty registry afterwards.
func withPristineRegistry(t *testing.T) {
	t.Helper()
	ResetRegistryForTest()
	t.Cleanup(ResetRegistryForTest)
}

func TestRegistry_RegisterAndLookup(t *testing.T) {
	withPristineRegistry(t)

	called := false
	RegisterType("custom", TypeInfo{
		Build: func(configJSON string, opts BuildOptions) (alert.Channel, error) {
			called = true
			return &fakeChannel{name: "custom"}, nil
		},
	})

	info, ok := LookupType("custom")
	if !ok {
		t.Fatal("expected custom type to be registered")
	}
	ch, err := info.Build(`{}`, BuildOptions{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := ch.Send(context.Background(), alert.Event{}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if !called {
		t.Error("expected registered Build to be invoked")
	}
}

func TestRegistry_LookupCaseInsensitive(t *testing.T) {
	withPristineRegistry(t)

	RegisterType("Feishu", TypeInfo{Build: func(string, BuildOptions) (alert.Channel, error) {
		return &fakeChannel{name: "feishu"}, nil
	}})

	for _, name := range []string{"feishu", "FEISHU", "Feishu", " feishu "} {
		if _, ok := LookupType(name); !ok {
			t.Errorf("LookupType(%q): got false, want true", name)
		}
	}
}

func TestRegistry_LookupUnregistered(t *testing.T) {
	withPristineRegistry(t)

	if _, ok := LookupType("nope"); ok {
		t.Error("LookupType on unregistered type: got true, want false")
	}
}

func TestRegistry_RegisterOverwrites(t *testing.T) {
	withPristineRegistry(t)

	first, second := 0, 0
	RegisterType("dup", TypeInfo{Build: func(string, BuildOptions) (alert.Channel, error) {
		first++
		return &fakeChannel{}, nil
	}})
	RegisterType("dup", TypeInfo{Build: func(string, BuildOptions) (alert.Channel, error) {
		second++
		return &fakeChannel{}, nil
	}})

	info, ok := LookupType("dup")
	if !ok {
		t.Fatal("expected dup type registered")
	}
	if _, err := info.Build(`{}`, BuildOptions{}); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if first != 0 || second != 1 {
		t.Errorf("last registration should win: first=%d second=%d", first, second)
	}
}

// ---------------------------------------------------------------------------
// Registry: ValidType
// ---------------------------------------------------------------------------

func TestRegistry_ValidTypeBuiltins(t *testing.T) {
	withPristineRegistry(t)

	for _, name := range []string{
		"webhook", "email", "dingtalk", "discord", "feishu", "slack",
		"teams", "telegram", "wecom",
	} {
		if !ValidType(name) {
			t.Errorf("ValidType(%q): got false, want true (built-in)", name)
		}
	}
}

func TestRegistry_ValidTypeRegisteredAndUnknown(t *testing.T) {
	withPristineRegistry(t)

	if ValidType("carrier-pigeon") {
		t.Error("ValidType on unknown type: got true, want false")
	}

	RegisterType("carrier-pigeon", TypeInfo{})
	if !ValidType("Carrier-Pigeon") {
		t.Error("ValidType on registered type: got false, want true")
	}
}

// ---------------------------------------------------------------------------
// Registry: AssertTypeAllowed
// ---------------------------------------------------------------------------

func TestRegistry_AssertTypeAllowedInvalidType(t *testing.T) {
	withPristineRegistry(t)

	err := AssertTypeAllowed(context.Background(), "nope")
	if err == nil {
		t.Fatal("expected error for invalid type")
	}
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected *ValidationError, got %T", err)
	}
	if ve.Code != CodeTypeInvalid {
		t.Errorf("code: got %d, want %d", ve.Code, CodeTypeInvalid)
	}
}

func TestRegistry_AssertTypeAllowedBuiltinNoGuard(t *testing.T) {
	withPristineRegistry(t)

	// Built-in types without a registered override are allowed.
	if err := AssertTypeAllowed(context.Background(), "webhook"); err != nil {
		t.Errorf("AssertTypeAllowed(webhook): unexpected error %v", err)
	}
}

func TestRegistry_AssertTypeAllowedGuardAllows(t *testing.T) {
	withPristineRegistry(t)

	RegisterType("sms", TypeInfo{
		TypeGuard: func(context.Context) error { return nil },
	})
	if err := AssertTypeAllowed(context.Background(), "SMS"); err != nil {
		t.Errorf("AssertTypeAllowed(SMS): unexpected error %v", err)
	}
}

func TestRegistry_AssertTypeAllowedGuardRejects(t *testing.T) {
	withPristineRegistry(t)

	RegisterType("sms", TypeInfo{
		TypeGuard: func(context.Context) error { return errGuardDenied },
	})

	err := AssertTypeAllowed(context.Background(), "sms")
	if !errors.Is(err, errGuardDenied) {
		t.Fatalf("AssertTypeAllowed(sms): got %v, want errGuardDenied", err)
	}
	// The guard error must surface unwrapped so the HTTP layer can map
	// 403-class errors precisely.
	validationError := &ValidationError{}
	if errors.As(err, &validationError) {
		t.Errorf("guard error should pass through unchanged, got %T", err)
	}
}

// ---------------------------------------------------------------------------
// Registry: ValidateTypeConfig
// ---------------------------------------------------------------------------

func TestRegistry_ValidateTypeConfig(t *testing.T) {
	withPristineRegistry(t)

	RegisterType("custom", TypeInfo{
		Validate: func(configJSON string) error {
			if configJSON == "" {
				return fmt.Errorf("config is required")
			}
			return nil
		},
	})

	if err := ValidateTypeConfig("custom", `{}`); err != nil {
		t.Errorf("ValidateTypeConfig on valid config: unexpected error %v", err)
	}
	if err := ValidateTypeConfig("custom", ""); err == nil {
		t.Error("ValidateTypeConfig on invalid config: expected error")
	}

	// Types without a validator (and unknown types) validate trivially.
	if err := ValidateTypeConfig("webhook", ""); err != nil {
		t.Errorf("ValidateTypeConfig on type without validator: unexpected error %v", err)
	}
	if err := ValidateTypeConfig("unknown", ""); err != nil {
		t.Errorf("ValidateTypeConfig on unknown type: unexpected error %v", err)
	}
}

// ---------------------------------------------------------------------------
// Registry: SensitiveKeys
// ---------------------------------------------------------------------------

func TestRegistry_SensitiveKeysBuiltin(t *testing.T) {
	withPristineRegistry(t)

	keys := SensitiveKeys()
	for _, want := range []string{
		"webhookurl", "robotwebhookurl", "secret", "bottoken",
		"apikey", "apisecret", "signingkey",
	} {
		if _, ok := keys[want]; !ok {
			t.Errorf("SensitiveKeys: built-in key %q missing: %v", want, keys)
		}
	}
}

func TestRegistry_SensitiveKeysNormalized(t *testing.T) {
	withPristineRegistry(t)

	// camelCase and snake_case variants of the same logical key collapse
	// to one normalized entry.
	RegisterType("custom", TypeInfo{SensitiveKeys: []string{"App_Secret"}})

	keys := SensitiveKeys()
	if _, ok := keys["appsecret"]; !ok {
		t.Errorf("SensitiveKeys: normalized key %q missing: %v", "appsecret", keys)
	}
	for _, notNormalized := range []string{"App_Secret", "app_secret"} {
		if _, ok := keys[notNormalized]; ok {
			t.Errorf("SensitiveKeys: raw key %q should be stored normalized", notNormalized)
		}
	}
}

// ---------------------------------------------------------------------------
// Registry: concurrency
// ---------------------------------------------------------------------------

// TestRegistry_ConcurrentRegisterAndLookup verifies that RegisterType and
// LookupType are safe to call concurrently (the registry is written at
// startup and read on every request).
func TestRegistry_ConcurrentRegisterAndLookup(t *testing.T) {
	withPristineRegistry(t)

	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			RegisterType(fmt.Sprintf("type-%d", i), TypeInfo{})
		}(i)
		go func(i int) {
			defer wg.Done()
			_, _ = LookupType(fmt.Sprintf("type-%d", i))
			_ = ValidType(fmt.Sprintf("type-%d", i))
			_ = SensitiveKeys()
		}(i)
	}
	wg.Wait()

	for i := range 8 {
		if _, ok := LookupType(fmt.Sprintf("type-%d", i)); !ok {
			t.Errorf("type-%d missing after concurrent registration", i)
		}
	}
}
