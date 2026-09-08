// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package channel

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/bytedance/sonic"
	"gorm.io/gorm"

	"github.com/tickraft/tickraft/pkg/db"
)

// ErrChannelNotFound is returned when a channel cannot be located by its
// ID.
var ErrChannelNotFound = errors.New("channel: not found")

// encPrefix marks encrypted field values inside the Config JSON so that
// decryptConfig can recognise them without knowledge of the field name.
const encPrefix = "enc:"

// builtinSensitiveKeys lists the normalized (lowercase, underscore-stripped)
// Config JSON keys whose values are considered secret and must be encrypted
// at rest and masked in API responses. Keys are matched case- and
// underscore-insensitively. Types registered via RegisterType may extend
// this set through TypeInfo.SensitiveKeys (see SensitiveKeys).
var builtinSensitiveKeys = map[string]struct{}{
	"webhookurl":      {},
	"robotwebhookurl": {},
	"secret":          {},
	"bottoken":        {},
	"apikey":          {},
	"apisecret":       {},
	"signingkey":      {},
}

// isMaskedValue reports whether val is a masked secret placeholder produced
// by MaskConfig ("****" plus the last four characters of the plaintext).
func isMaskedValue(val string) bool {
	return strings.HasPrefix(val, "****")
}

// Store persists and queries notification channel configurations,
// encrypting sensitive Config fields at rest. Decrypted plaintext is never
// cached: secrets are only materialised in memory when explicitly requested
// by Get/List/MaskConfig.
//
// When a TenantResolver is installed (NewTenantStore) every query and
// write is scoped to the tenant resolved from the call context; the
// default NewStore construction runs unscoped.
type Store struct {
	dbc           *gorm.DB
	encryptionKey []byte
	tenant        TenantResolver
}

// NewStore creates a Store backed by GORM. encryptionKey must be 32 bytes
// for AES-256-GCM; a nil or empty key disables encryption (intended for
// development and test environments only).
func NewStore(dbc *gorm.DB, encryptionKey []byte) *Store {
	return &Store{
		dbc:           dbc,
		encryptionKey: encryptionKey,
	}
}

// NewTenantStore creates a Store scoped to the tenant resolved from each
// call's context: List/ListEnabled filter by tenant, Get/Update/Delete
// additionally match the tenant (a cross-tenant row behaves as not found),
// and Create stamps the owning tenant. resolve must be non-nil; see
// TenantResolver for the contract.
func NewTenantStore(dbc *gorm.DB, encryptionKey []byte, resolve TenantResolver) *Store {
	return &Store{
		dbc:           dbc,
		encryptionKey: encryptionKey,
		tenant:        resolve,
	}
}

// queryScope returns the base query for the call, tenant-filtered when a
// resolver is installed. It fails with ErrTenantRequired when scoping is
// active but the context carries no tenant.
func (s *Store) queryScope(ctx context.Context) (*gorm.DB, error) {
	q := s.dbc.WithContext(ctx)
	if s.tenant == nil {
		return q, nil
	}
	tenantID, err := requiredTenant(ctx, s.tenant)
	if err != nil {
		return nil, err
	}
	return q.Where("tenant_id = ?", tenantID), nil
}

// List returns all channel configurations. Sensitive Config fields are
// decrypted before returning.
func (s *Store) List(ctx context.Context) ([]Channel, error) {
	q, err := s.queryScope(ctx)
	if err != nil {
		return nil, err
	}
	var channels []Channel
	if err := q.Find(&channels).Error; err != nil {
		return nil, db.MapError(err)
	}
	if err := decryptChannels(channels, s.encryptionKey); err != nil {
		return nil, err
	}
	return channels, nil
}

// ListEnabled returns the enabled channel configurations ordered by ID
// ascending, with sensitive Config fields decrypted. It is used by the
// prism engine to load active channels into memory at startup and during
// hot-reload.
func (s *Store) ListEnabled(ctx context.Context) ([]*Channel, error) {
	q, err := s.queryScope(ctx)
	if err != nil {
		return nil, err
	}
	var channels []*Channel
	if err := q.
		Where("enabled = ?", true).
		Order("id ASC").
		Find(&channels).Error; err != nil {
		return nil, db.MapError(err)
	}
	if err := decryptChannelPtrs(channels, s.encryptionKey); err != nil {
		return nil, err
	}
	return channels, nil
}

// Get returns the channel configuration with the given id. Sensitive
// Config fields are decrypted before returning.
func (s *Store) Get(ctx context.Context, id int64) (*Channel, error) {
	q, err := s.queryScope(ctx)
	if err != nil {
		return nil, err
	}
	var ch Channel
	if err := q.First(&ch, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrChannelNotFound
		}
		return nil, db.MapError(err)
	}
	decrypted, err := decryptConfig(ch.Config, s.encryptionKey)
	if err != nil {
		return nil, fmt.Errorf("decrypt config %d: %w", ch.ID, err)
	}
	ch.Config = decrypted
	return &ch, nil
}

// Create inserts a new channel configuration. Sensitive Config fields are
// encrypted before the row is written. The passed channel keeps its
// plaintext Config and is populated with the new ID. When Enabled is nil
// the column default (true) is applied by the database; an explicit false
// or true is persisted as given.
func (s *Store) Create(ctx context.Context, ch *Channel) error {
	if s.tenant != nil {
		tenantID, err := requiredTenant(ctx, s.tenant)
		if err != nil {
			return err
		}
		ch.TenantID = tenantID
	}
	plaintext := ch.Config
	encrypted, err := encryptConfig(plaintext, s.encryptionKey)
	if err != nil {
		return fmt.Errorf("encrypt config: %w", err)
	}

	ch.Config = encrypted
	if err := s.dbc.WithContext(ctx).Create(ch).Error; err != nil {
		ch.Config = plaintext
		return db.MapError(err)
	}
	ch.Config = plaintext
	return nil
}

// Update updates the mutable fields of the channel configuration
// identified by ch.ID. Sensitive Config fields are re-encrypted before the
// row is written. A nil ch.Enabled is treated as false so the column is
// always written with a concrete value.
func (s *Store) Update(ctx context.Context, ch *Channel) error {
	plaintext := ch.Config
	encrypted, err := encryptConfig(plaintext, s.encryptionKey)
	if err != nil {
		return fmt.Errorf("encrypt config: %w", err)
	}

	enabledVal := false
	if ch.Enabled != nil {
		enabledVal = *ch.Enabled
	}

	q := s.dbc.WithContext(ctx).
		Model(&Channel{}).
		Where("id = ?", ch.ID)
	if s.tenant != nil {
		tenantID, err := requiredTenant(ctx, s.tenant)
		if err != nil {
			ch.Config = plaintext
			return err
		}
		q = q.Where("tenant_id = ?", tenantID)
		ch.TenantID = tenantID
	}
	result := q.Updates(map[string]any{
		"name":    ch.Name,
		"type":    ch.Type,
		"config":  encrypted,
		"enabled": enabledVal,
	})
	if result.Error != nil {
		ch.Config = plaintext
		return db.MapError(result.Error)
	}
	if result.RowsAffected == 0 {
		ch.Config = plaintext
		return ErrChannelNotFound
	}

	ch.Config = plaintext
	return nil
}

// TouchTest stamps the channel row with the outcome and time of a test
// dispatch so the channel list can surface the last test state. The
// result string follows the tracking delivery statuses ("success" /
// "failed").
func (s *Store) TouchTest(ctx context.Context, id int64, outcome string) error {
	q := s.dbc.WithContext(ctx).Model(&Channel{}).Where("id = ?", id)
	if s.tenant != nil {
		tenantID, err := requiredTenant(ctx, s.tenant)
		if err != nil {
			return err
		}
		q = q.Where("tenant_id = ?", tenantID)
	}
	res := q.Updates(map[string]any{
		"last_test_at":     time.Now(),
		"last_test_result": outcome,
	})
	if res.Error != nil {
		return db.MapError(res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrChannelNotFound
	}
	return nil
}

// Delete removes the channel configuration with the given id.
func (s *Store) Delete(ctx context.Context, id int64) error {
	q, err := s.queryScope(ctx)
	if err != nil {
		return err
	}
	result := q.Where("id = ?", id).Delete(&Channel{})
	if result.Error != nil {
		return db.MapError(result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrChannelNotFound
	}
	return nil
}

// TouchLastUsedAt sets last_used_at to the given time for the channel
// identified by id. It is intended to be called by the prism engine after
// a successful notification delivery. It is lenient about tenancy: when a
// resolver is installed and the context carries a tenant the update is
// scoped, otherwise it falls back to the primary-key update (dispatch
// contexts may be tenant-less).
func (s *Store) TouchLastUsedAt(ctx context.Context, id int64, at time.Time) error {
	q := s.dbc.WithContext(ctx).
		Model(&Channel{}).
		Where("id = ?", id)
	if tenantID := optionalTenant(ctx, s.tenant); tenantID > 0 {
		q = q.Where("tenant_id = ?", tenantID)
	}
	result := q.Update("last_used_at", at)
	if result.Error != nil {
		return db.MapError(result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrChannelNotFound
	}
	return nil
}

// MaskConfig returns a copy of ch with sensitive Config fields replaced by
// a masked representation. Encrypted values are first decrypted and then
// masked as "****" followed by the last four characters of the plaintext.
// The original ch is not modified.
func (s *Store) MaskConfig(ch *Channel) (*Channel, error) {
	if ch == nil {
		return nil, fmt.Errorf("mask config: nil channel")
	}

	decrypted, err := decryptConfig(ch.Config, s.encryptionKey)
	if err != nil {
		return nil, fmt.Errorf("decrypt config for masking: %w", err)
	}

	masked, err := maskConfigJSON(decrypted)
	if err != nil {
		return nil, fmt.Errorf("mask config: %w", err)
	}

	cp := *ch
	cp.Config = masked
	return &cp, nil
}

// decryptChannels decrypts the Config field of every entry in place.
func decryptChannels(channels []Channel, key []byte) error {
	for i := range channels {
		decrypted, err := decryptConfig(channels[i].Config, key)
		if err != nil {
			return fmt.Errorf("decrypt config %d: %w", channels[i].ID, err)
		}
		channels[i].Config = decrypted
	}
	return nil
}

// decryptChannelPtrs decrypts the Config field of every entry in place.
func decryptChannelPtrs(channels []*Channel, key []byte) error {
	for _, ch := range channels {
		decrypted, err := decryptConfig(ch.Config, key)
		if err != nil {
			return fmt.Errorf("decrypt config %d: %w", ch.ID, err)
		}
		ch.Config = decrypted
	}
	return nil
}

// encryptConfig parses configJSON, encrypts the values of sensitive keys
// with AES-256-GCM, and re-serialises the result. Encrypted values are
// prefixed with encPrefix. When key is empty the input is returned
// unchanged so development and test environments can run without
// encryption.
func encryptConfig(configJSON string, key []byte) (string, error) {
	if len(key) == 0 || configJSON == "" {
		return configJSON, nil
	}

	fields, err := parseConfigObject(configJSON)
	if err != nil {
		return "", err
	}

	sensitive := SensitiveKeys()
	for name, raw := range fields {
		if _, ok := sensitive[normalizeKey(name)]; !ok {
			continue
		}
		val, ok := decodeStringField(raw)
		if !ok {
			continue // non-string value, leave untouched
		}
		if strings.HasPrefix(val, encPrefix) {
			continue // already encrypted, keep idempotent
		}
		encrypted, err := encryptValue(val, key)
		if err != nil {
			return "", fmt.Errorf("encrypt field %q: %w", name, err)
		}
		fields[name] = encodeStringField(encrypted)
	}

	return marshalConfigObject(fields)
}

// decryptConfig parses configJSON, decrypts any encPrefix-prefixed values,
// and re-serialises the result. When key is empty the input is returned
// unchanged.
func decryptConfig(configJSON string, key []byte) (string, error) {
	if len(key) == 0 || configJSON == "" {
		return configJSON, nil
	}

	fields, err := parseConfigObject(configJSON)
	if err != nil {
		return "", err
	}

	changed := false
	for name, raw := range fields {
		val, ok := decodeStringField(raw)
		if !ok {
			continue
		}
		if !strings.HasPrefix(val, encPrefix) {
			continue
		}
		plaintext, err := decryptValue(val, key)
		if err != nil {
			return "", fmt.Errorf("decrypt field %q: %w", name, err)
		}
		fields[name] = encodeStringField(plaintext)
		changed = true
	}

	if !changed {
		// Preserve the exact bytes when nothing was decrypted.
		return configJSON, nil
	}
	return marshalConfigObject(fields)
}

// maskConfigJSON parses configJSON and replaces the values of sensitive
// keys with a masked representation.
func maskConfigJSON(configJSON string) (string, error) {
	if configJSON == "" {
		return "", nil
	}

	fields, err := parseConfigObject(configJSON)
	if err != nil {
		return "", err
	}

	sensitive := SensitiveKeys()
	for name, raw := range fields {
		if _, ok := sensitive[normalizeKey(name)]; !ok {
			continue
		}
		val, ok := decodeStringField(raw)
		if !ok {
			continue
		}
		fields[name] = encodeStringField(maskValue(val))
	}

	return marshalConfigObject(fields)
}

// parseConfigObject unmarshals configJSON into a map of raw JSON values.
func parseConfigObject(configJSON string) (map[string]json.RawMessage, error) {
	fields := make(map[string]json.RawMessage)
	if err := sonic.Unmarshal([]byte(configJSON), &fields); err != nil {
		return nil, fmt.Errorf("parse config json: %w", err)
	}
	return fields, nil
}

// marshalConfigObject serialises the field map back to compact JSON.
func marshalConfigObject(fields map[string]json.RawMessage) (string, error) {
	out, err := sonic.Marshal(fields)
	if err != nil {
		return "", fmt.Errorf("marshal config json: %w", err)
	}
	return string(out), nil
}

// decodeStringField returns the string value encoded in raw and true when
// raw is a JSON string.
func decodeStringField(raw json.RawMessage) (string, bool) {
	var val string
	if err := sonic.Unmarshal(raw, &val); err != nil {
		return "", false
	}
	return val, true
}

// encodeStringField returns the JSON encoding of val.
func encodeStringField(val string) json.RawMessage {
	b, err := sonic.Marshal(val)
	if err != nil {
		return json.RawMessage(`""`)
	}
	return b
}

// normalizeKey lowercases a config key and strips underscores so that
// camelCase and snake_case writers are covered.
func normalizeKey(name string) string {
	return strings.ToLower(strings.ReplaceAll(name, "_", ""))
}

// maskValue masks a secret value as "****" plus its last four characters.
// Values with four or fewer characters are fully masked to avoid leaking
// the entire secret.
func maskValue(val string) string {
	if len(val) <= 4 {
		return "****"
	}
	return "****" + val[len(val)-4:]
}

// encryptValue encrypts plaintext with AES-256-GCM. The nonce is prepended
// to the ciphertext, base64-encoded, and prefixed with encPrefix so callers
// can distinguish encrypted from plaintext values.
func encryptValue(plaintext string, key []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return encPrefix + base64.StdEncoding.EncodeToString(sealed), nil
}

// decryptValue reverses encryptValue. Values without the encPrefix are
// returned unchanged.
func decryptValue(encrypted string, key []byte) (string, error) {
	if !strings.HasPrefix(encrypted, encPrefix) {
		return encrypted, nil
	}

	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(encrypted, encPrefix))
	if err != nil {
		return "", fmt.Errorf("decode base64: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create gcm: %w", err)
	}

	if len(raw) < gcm.NonceSize() {
		return "", fmt.Errorf("ciphertext too short")
	}
	nonce, ciphertext := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("open gcm: %w", err)
	}
	return string(plaintext), nil
}
