// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package channel

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/tickraft/tickraft/pkg/db"
	"github.com/tickraft/tickraft/pkg/errdefs"
)

// testEncryptionKey is a 32-byte AES-256 key for the encryption tests.
var testEncryptionKey = []byte("0123456789abcdef0123456789abcdef")

// newStoreTestDB opens an in-memory SQLite database with the channel and
// delivery record tables migrated.
func newStoreTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	ctx := context.Background()
	gdb, err := db.Open(ctx, db.Config{Driver: "sqlite3", Addr: ":memory:"})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, e := gdb.DB(); e == nil {
			_ = sqlDB.Close()
		}
	})
	if err := Migrate(ctx, gdb); err != nil {
		t.Fatalf("migrate channel tables: %v", err)
	}
	return gdb
}

// boolPtr returns a pointer to b, used to set the *bool Enabled field.
func boolPtr(b bool) *bool { return &b }

// newChannel builds a Channel literal for test setup.
func newChannel(name, typ, configJSON string, enabled bool) Channel {
	return Channel{
		Name:    name,
		Type:    typ,
		Config:  configJSON,
		Enabled: &enabled,
	}
}

// ---------------------------------------------------------------------------
// Store: Create / Get / List / Update / Delete
// ---------------------------------------------------------------------------

func TestStore_CreateAndGet(t *testing.T) {
	gdb := newStoreTestDB(t)
	s := NewStore(gdb, testEncryptionKey)
	ctx := context.Background()

	ch := newChannel(
		"feishu-default",
		"feishu",
		`{"webhook_url":"https://hook.example/x","signing_key":"topsecret"}`,
		true,
	)
	if err := s.Create(ctx, &ch); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if ch.ID == 0 {
		t.Fatal("expected non-zero ID after Create")
	}
	// The caller's struct should keep the plaintext Config.
	if !strings.Contains(ch.Config, "topsecret") {
		t.Errorf("expected caller Config to keep plaintext, got %q", ch.Config)
	}

	// The persisted row must NOT contain the plaintext secret.
	var stored Channel
	if err := gdb.First(&stored, ch.ID).Error; err != nil {
		t.Fatalf("query stored: %v", err)
	}
	if strings.Contains(stored.Config, "topsecret") {
		t.Errorf("expected stored Config to be encrypted, got %q", stored.Config)
	}
	if !strings.Contains(stored.Config, encPrefix) {
		t.Errorf("expected stored Config to contain %q prefix", encPrefix)
	}

	// Get should return the decrypted config.
	got, err := s.Get(ctx, ch.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !strings.Contains(got.Config, "topsecret") {
		t.Errorf("expected decrypted Config to contain plaintext secret, got %q", got.Config)
	}
	if !strings.Contains(got.Config, "https://hook.example/x") {
		t.Errorf("expected decrypted Config to contain webhook url, got %q", got.Config)
	}
}

func TestStore_List(t *testing.T) {
	gdb := newStoreTestDB(t)
	s := NewStore(gdb, testEncryptionKey)
	ctx := context.Background()

	channels := []Channel{
		newChannel("a", "feishu", `{"secret":"s1"}`, true),
		newChannel("b", "slack", `{"bot_token":"t2"}`, false),
		newChannel("c", "webhook", `{"secret":"s3"}`, true),
	}
	for i := range channels {
		if err := s.Create(ctx, &channels[i]); err != nil {
			t.Fatalf("Create %d: %v", i, err)
		}
	}

	got, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 channels, got %d", len(got))
	}
	// Every returned config should be decrypted.
	for _, c := range got {
		if strings.Contains(c.Config, encPrefix) {
			t.Errorf("expected decrypted config, got %q", c.Config)
		}
	}
}

func TestStore_ListEnabled(t *testing.T) {
	gdb := newStoreTestDB(t)
	s := NewStore(gdb, testEncryptionKey)
	ctx := context.Background()

	onCh := newChannel("on", "feishu", `{"secret":"s1"}`, true)
	if err := s.Create(ctx, &onCh); err != nil {
		t.Fatalf("Create on: %v", err)
	}
	offCh := newChannel("off", "slack", `{"bot_token":"t2"}`, false)
	if err := s.Create(ctx, &offCh); err != nil {
		t.Fatalf("Create off: %v", err)
	}

	got, err := s.ListEnabled(ctx)
	if err != nil {
		t.Fatalf("ListEnabled: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 enabled channel, got %d", len(got))
	}
	if got[0].Name != "on" {
		t.Errorf("expected enabled channel 'on', got %q", got[0].Name)
	}
}

func TestStore_Update(t *testing.T) {
	gdb := newStoreTestDB(t)
	s := NewStore(gdb, testEncryptionKey)
	ctx := context.Background()

	ch := newChannel("feishu", "feishu", `{"secret":"orig"}`, true)
	if err := s.Create(ctx, &ch); err != nil {
		t.Fatalf("Create: %v", err)
	}

	ch.Name = "feishu-renamed"
	ch.Config = `{"secret":"updated"}`
	ch.Enabled = boolPtr(false)
	if err := s.Update(ctx, &ch); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := s.Get(ctx, ch.ID)
	if err != nil {
		t.Fatalf("Get after Update: %v", err)
	}
	if got.Name != "feishu-renamed" {
		t.Errorf("name: got %q, want %q", got.Name, "feishu-renamed")
	}
	if got.Enabled == nil || *got.Enabled {
		t.Errorf("enabled: got %v, want false", got.Enabled)
	}
	if !strings.Contains(got.Config, "updated") {
		t.Errorf("expected updated secret, got %q", got.Config)
	}

	// The persisted row should be encrypted and must not retain the old
	// plaintext secret.
	var stored Channel
	if err := gdb.First(&stored, ch.ID).Error; err != nil {
		t.Fatalf("query stored: %v", err)
	}
	if strings.Contains(stored.Config, "updated") {
		t.Errorf("expected stored Config encrypted, got %q", stored.Config)
	}
	if strings.Contains(stored.Config, "orig") {
		t.Errorf("expected old secret gone, got %q", stored.Config)
	}
}

// TestStore_UpdatePreservesEngineState pins the Update column whitelist: a
// PUT-shaped model (client-bound fields, zero last_used_at) must never reset
// the engine-owned last_used_at bookkeeping.
func TestStore_UpdatePreservesEngineState(t *testing.T) {
	gdb := newStoreTestDB(t)
	s := NewStore(gdb, nil)
	ctx := context.Background()

	created := &Channel{Name: "hook", Type: "webhook", Config: `{"url":"https://example.test"}`, Enabled: boolPtr(true)}
	if err := s.Create(ctx, created); err != nil {
		t.Fatalf("create: %v", err)
	}

	used := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	if err := s.TouchLastUsedAt(ctx, created.ID, used); err != nil {
		t.Fatalf("touch last_used_at: %v", err)
	}

	// PUT-shaped update: renamed and disabled, last_used_at left zero.
	updated := &Channel{
		ID:      created.ID,
		Name:    "hook2",
		Type:    "webhook",
		Config:  `{"url":"https://example.test/v2"}`,
		Enabled: boolPtr(false),
	}
	if err := s.Update(ctx, updated); err != nil {
		t.Fatalf("update: %v", err)
	}

	got, err := s.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "hook2" || (got.Enabled != nil && *got.Enabled) {
		t.Errorf("editable fields not applied: name=%q enabled=%v", got.Name, got.Enabled)
	}
	if got.LastUsedAt == nil || !got.LastUsedAt.Equal(used) {
		t.Errorf("last_used_at = %v, want preserved %v (engine-owned, never reset by PUT)", got.LastUsedAt, used)
	}
	if got.CreatedAt.IsZero() {
		t.Error("created_at was cleared by update")
	}
}

func TestStore_Delete(t *testing.T) {
	gdb := newStoreTestDB(t)
	s := NewStore(gdb, testEncryptionKey)
	ctx := context.Background()

	ch := newChannel("feishu", "feishu", `{"secret":"s"}`, true)
	if err := s.Create(ctx, &ch); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := s.Delete(ctx, ch.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if _, err := s.Get(ctx, ch.ID); !errors.Is(err, ErrChannelNotFound) {
		t.Errorf("expected ErrChannelNotFound after delete, got %v", err)
	}
}

func TestStore_NotFound(t *testing.T) {
	gdb := newStoreTestDB(t)
	s := NewStore(gdb, testEncryptionKey)
	ctx := context.Background()

	if _, err := s.Get(ctx, 999); !errors.Is(err, ErrChannelNotFound) {
		t.Errorf("Get on missing row = %v, want ErrChannelNotFound", err)
	}
	if err := s.Delete(ctx, 999); !errors.Is(err, ErrChannelNotFound) {
		t.Errorf("Delete on missing row = %v, want ErrChannelNotFound", err)
	}
	if err := s.Update(ctx, &Channel{ID: 999, Name: "x", Type: "webhook", Config: "{}"}); err == nil ||
		!errors.Is(err, ErrChannelNotFound) {
		t.Errorf("Update on missing row = %v, want ErrChannelNotFound", err)
	}
	if err := s.TouchLastUsedAt(ctx, 999, time.Now()); !errors.Is(err, ErrChannelNotFound) {
		t.Errorf("TouchLastUsedAt on missing row = %v, want ErrChannelNotFound", err)
	}
}

// ---------------------------------------------------------------------------
// Store: encryption helpers
// ---------------------------------------------------------------------------

func TestStore_EncryptionRoundTrip(t *testing.T) {
	original := `{"webhook_url":"https://hook.example/x","secret":"topsecret","bot_token":"123456:ABC",` +
		`"signing_key":"signing-key-value","name":"mychannel","timeout":30}`

	encrypted, err := encryptConfig(original, testEncryptionKey)
	if err != nil {
		t.Fatalf("encryptConfig: %v", err)
	}
	if encrypted == original {
		t.Fatal("expected encrypted JSON to differ from original")
	}
	// Non-sensitive fields should remain readable in the encrypted JSON.
	if !strings.Contains(encrypted, "mychannel") {
		t.Errorf("expected non-sensitive field preserved, got %q", encrypted)
	}
	// Sensitive plaintext must be gone.
	if strings.Contains(encrypted, "topsecret") {
		t.Errorf("expected secret encrypted, got %q", encrypted)
	}
	if strings.Contains(encrypted, "signing-key-value") {
		t.Errorf("expected signing_key encrypted, got %q", encrypted)
	}

	decrypted, err := decryptConfig(encrypted, testEncryptionKey)
	if err != nil {
		t.Fatalf("decryptConfig: %v", err)
	}
	if !jsonEqual(t, original, decrypted) {
		t.Errorf("round-trip mismatch:\noriginal:  %s\ndecrypted: %s", original, decrypted)
	}
}

func TestStore_EncryptionRoundTrip_CamelCaseKeys(t *testing.T) {
	// The frontend writes camelCase keys (webhookUrl, botToken); they must
	// be recognised as sensitive just like the snake_case variants.
	original := `{"webhookUrl":"https://hook.example/x","botToken":"123456:ABC","name":"mychannel"}`

	encrypted, err := encryptConfig(original, testEncryptionKey)
	if err != nil {
		t.Fatalf("encryptConfig: %v", err)
	}
	if strings.Contains(encrypted, "https://hook.example/x") {
		t.Errorf("expected webhookUrl encrypted, got %q", encrypted)
	}
	if strings.Contains(encrypted, "123456:ABC") {
		t.Errorf("expected botToken encrypted, got %q", encrypted)
	}
	if !strings.Contains(encrypted, "mychannel") {
		t.Errorf("expected non-sensitive field preserved, got %q", encrypted)
	}

	decrypted, err := decryptConfig(encrypted, testEncryptionKey)
	if err != nil {
		t.Fatalf("decryptConfig: %v", err)
	}
	if !jsonEqual(t, original, decrypted) {
		t.Errorf("round-trip mismatch:\noriginal:  %s\ndecrypted: %s", original, decrypted)
	}

	masked, err := maskConfigJSON(original)
	if err != nil {
		t.Fatalf("maskConfigJSON: %v", err)
	}
	if strings.Contains(masked, "https://hook.example/x") {
		t.Errorf("expected webhookUrl masked, got %q", masked)
	}
}

func TestStore_EncryptConfig_AlreadyEncrypted(t *testing.T) {
	original := `{"secret":"plaintext"}`

	first, err := encryptConfig(original, testEncryptionKey)
	if err != nil {
		t.Fatalf("first encrypt: %v", err)
	}
	// Encrypting again must be idempotent (no double encryption).
	second, err := encryptConfig(first, testEncryptionKey)
	if err != nil {
		t.Fatalf("second encrypt: %v", err)
	}
	if first != second {
		t.Errorf("expected idempotent encryption, got\nfirst:  %s\nsecond: %s", first, second)
	}
}

func TestStore_NoEncryptionKey(t *testing.T) {
	gdb := newStoreTestDB(t)
	s := NewStore(gdb, nil) // encryption disabled
	ctx := context.Background()

	ch := newChannel("feishu", "feishu", `{"secret":"plaintext"}`, true)
	if err := s.Create(ctx, &ch); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// The stored row should contain the plaintext secret verbatim.
	var stored Channel
	if err := gdb.First(&stored, ch.ID).Error; err != nil {
		t.Fatalf("query stored: %v", err)
	}
	if !strings.Contains(stored.Config, "plaintext") {
		t.Errorf("expected plaintext stored without key, got %q", stored.Config)
	}

	got, err := s.Get(ctx, ch.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !strings.Contains(got.Config, "plaintext") {
		t.Errorf("expected plaintext returned without key, got %q", got.Config)
	}
}

// ---------------------------------------------------------------------------
// Store: MaskConfig
// ---------------------------------------------------------------------------

func TestStore_MaskConfig(t *testing.T) {
	gdb := newStoreTestDB(t)
	s := NewStore(gdb, testEncryptionKey)
	ctx := context.Background()

	ch := newChannel(
		"feishu",
		"feishu",
		`{"webhook_url":"https://hook.example/abcdef","secret":"topsecret1234","name":"visible"}`,
		true,
	)
	if err := s.Create(ctx, &ch); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := s.Get(ctx, ch.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	masked, err := s.MaskConfig(got)
	if err != nil {
		t.Fatalf("MaskConfig: %v", err)
	}

	var fields map[string]string
	if err := json.Unmarshal([]byte(masked.Config), &fields); err != nil {
		t.Fatalf("unmarshal masked: %v", err)
	}

	if fields["webhook_url"] != "****cdef" {
		t.Errorf("webhook_url mask: got %q, want %q", fields["webhook_url"], "****cdef")
	}
	if fields["secret"] != "****1234" {
		t.Errorf("secret mask: got %q, want %q", fields["secret"], "****1234")
	}
	if fields["name"] != "visible" {
		t.Errorf("name should be unmasked: got %q, want %q", fields["name"], "visible")
	}
	// The original config must not be mutated by masking.
	if strings.Contains(got.Config, "****") {
		t.Errorf("original config should not be masked, got %q", got.Config)
	}
}

func TestStore_MaskConfig_ShortValue(t *testing.T) {
	s := NewStore(nil, testEncryptionKey)
	ch := &Channel{Config: `{"secret":"ab"}`}
	masked, err := s.MaskConfig(ch)
	if err != nil {
		t.Fatalf("MaskConfig: %v", err)
	}
	var fields map[string]string
	if err := json.Unmarshal([]byte(masked.Config), &fields); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if fields["secret"] != "****" {
		t.Errorf("short secret mask: got %q, want %q", fields["secret"], "****")
	}
}

func TestStore_MaskConfig_Nil(t *testing.T) {
	s := NewStore(nil, testEncryptionKey)
	if _, err := s.MaskConfig(nil); err == nil {
		t.Error("expected error for nil channel")
	}
}

func TestStore_MaskConfig_NoKey(t *testing.T) {
	s := NewStore(nil, nil) // no encryption key
	ch := &Channel{Config: `{"secret":"topsecret1234"}`}
	masked, err := s.MaskConfig(ch)
	if err != nil {
		t.Fatalf("MaskConfig: %v", err)
	}
	var fields map[string]string
	if err := json.Unmarshal([]byte(masked.Config), &fields); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if fields["secret"] != "****1234" {
		t.Errorf("mask without key: got %q, want %q", fields["secret"], "****1234")
	}
}

func TestStore_EncryptInvalidJSON(t *testing.T) {
	if _, err := encryptConfig("{not json", testEncryptionKey); err == nil {
		t.Error("expected error for invalid JSON")
	}
	if _, err := decryptConfig("{not json", testEncryptionKey); err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestStore_DecryptTamperedCiphertext(t *testing.T) {
	encrypted, err := encryptConfig(`{"secret":"value"}`, testEncryptionKey)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	// Tamper: strip the prefix and corrupt the base64 payload.
	tampered := encPrefix + "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal([]byte(encrypted), &fields); err != nil {
		t.Fatalf("unmarshal encrypted: %v", err)
	}
	fields["secret"] = encodeStringField(tampered)
	corrupted, err := marshalConfigObject(fields)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	if _, err := decryptConfig(corrupted, testEncryptionKey); err == nil {
		t.Error("expected error for tampered ciphertext")
	}
}

// TestStore_SensitiveKeysUnion verifies that keys contributed by a
// registered TypeInfo extend the built-in sensitive set (and are cleaned up
// afterwards so other tests see the pristine registry).
func TestStore_SensitiveKeysUnion(t *testing.T) {
	ResetRegistryForTest()
	t.Cleanup(ResetRegistryForTest)

	RegisterType("custom", TypeInfo{SensitiveKeys: []string{"AccessKey", "access_secret"}})

	keys := SensitiveKeys()
	for _, want := range []string{"webhookurl", "accesskey", "accesssecret"} {
		if _, ok := keys[want]; !ok {
			t.Errorf("SensitiveKeys: %q missing from union: %v", want, keys)
		}
	}
}

// ---------------------------------------------------------------------------
// Service: masked-secret merge on update
// ---------------------------------------------------------------------------

func TestMergeMaskedSecrets(t *testing.T) {
	stored := `{"webhook_url":"https://hook.example/x","secret":"topsecret1234","name":"visible"}`

	// The client echoes the masked read back with the name changed.
	incoming := `{"webhook_url":"****cdef","secret":"****1234","name":"renamed"}`

	merged, err := mergeMaskedSecrets(stored, incoming)
	if err != nil {
		t.Fatalf("mergeMaskedSecrets: %v", err)
	}

	var fields map[string]string
	if err := json.Unmarshal([]byte(merged), &fields); err != nil {
		t.Fatalf("unmarshal merged: %v", err)
	}
	if fields["webhook_url"] != "https://hook.example/x" {
		t.Errorf("masked webhook_url should keep stored plaintext, got %q", fields["webhook_url"])
	}
	if fields["secret"] != "topsecret1234" {
		t.Errorf("masked secret should keep stored plaintext, got %q", fields["secret"])
	}
	if fields["name"] != "renamed" {
		t.Errorf("non-sensitive field should pass through, got %q", fields["name"])
	}
}

func TestMergeMaskedSecrets_EmptySecretKeepsStored(t *testing.T) {
	stored := `{"secret":"topsecret1234"}`
	incoming := `{"secret":""}`

	merged, err := mergeMaskedSecrets(stored, incoming)
	if err != nil {
		t.Fatalf("mergeMaskedSecrets: %v", err)
	}
	var fields map[string]string
	if err := json.Unmarshal([]byte(merged), &fields); err != nil {
		t.Fatalf("unmarshal merged: %v", err)
	}
	if fields["secret"] != "topsecret1234" {
		t.Errorf("empty secret should keep stored plaintext, got %q", fields["secret"])
	}
}

func TestMergeMaskedSecrets_NewSecretOverrides(t *testing.T) {
	stored := `{"secret":"topsecret1234"}`
	incoming := `{"secret":"brandnewsecret"}`

	merged, err := mergeMaskedSecrets(stored, incoming)
	if err != nil {
		t.Fatalf("mergeMaskedSecrets: %v", err)
	}
	var fields map[string]string
	if err := json.Unmarshal([]byte(merged), &fields); err != nil {
		t.Fatalf("unmarshal merged: %v", err)
	}
	if fields["secret"] != "brandnewsecret" {
		t.Errorf("fresh secret should override, got %q", fields["secret"])
	}
}

// TestChannelService_UpdateKeepsMaskedSecret exercises the full service
// round trip: a client PUT that echoes masked secrets must not clobber the
// stored plaintext, and the response must be masked again.
func TestChannelService_UpdateKeepsMaskedSecret(t *testing.T) {
	gdb := newStoreTestDB(t)
	store := NewStore(gdb, testEncryptionKey)
	svc := NewChannelService(store, NewDeliveryStore(gdb), nil)
	ctx := context.Background()

	created, err := svc.CreateChannel(ctx, &CreateRequest{
		Name:    "ops",
		Type:    "feishu",
		Config:  json.RawMessage(`{"webhook_url":"https://hook.example/x","secret":"topsecret1234"}`),
		Enabled: boolPtr(true),
	})
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}

	// The update echoes the masked config read from the API.
	updated, err := svc.UpdateChannel(ctx, created.ID, &UpdateRequest{
		Name:   "ops-renamed",
		Config: json.RawMessage(created.Config),
	})
	if err != nil {
		t.Fatalf("UpdateChannel: %v", err)
	}
	if updated.Name != "ops-renamed" {
		t.Errorf("name: got %q, want ops-renamed", updated.Name)
	}

	// The stored plaintext must have survived the masked round trip.
	got, err := store.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !strings.Contains(got.Config, "https://hook.example/x") {
		t.Errorf("stored plaintext secret was clobbered: %q", got.Config)
	}

	// The API echo must be masked.
	if !strings.Contains(updated.Config, "****") {
		t.Errorf("expected masked echo, got %q", updated.Config)
	}
}

// TestChannelService_NotFoundMapping verifies the store-not-found →
// errdefs mapping used by the HTTP layer.
func TestChannelService_NotFoundMapping(t *testing.T) {
	gdb := newStoreTestDB(t)
	svc := NewChannelService(NewStore(gdb, nil), NewDeliveryStore(gdb), nil)
	ctx := context.Background()

	if _, err := svc.GetChannel(ctx, 999); !errors.Is(err, errdefs.ErrChannelNotFound) {
		t.Errorf("GetChannel on missing row = %v, want errdefs.ErrChannelNotFound", err)
	}
	if err := svc.DeleteChannel(ctx, 999); !errors.Is(err, errdefs.ErrChannelNotFound) {
		t.Errorf("DeleteChannel on missing row = %v, want errdefs.ErrChannelNotFound", err)
	}
}

// jsonEqual reports whether two JSON strings decode to equal values,
// ignoring formatting and key-ordering differences.
func jsonEqual(t *testing.T, a, b string) bool {
	t.Helper()
	var va, vb any
	if err := json.Unmarshal([]byte(a), &va); err != nil {
		t.Fatalf("unmarshal a: %v", err)
	}
	if err := json.Unmarshal([]byte(b), &vb); err != nil {
		t.Fatalf("unmarshal b: %v", err)
	}
	return reflect.DeepEqual(va, vb)
}
