// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package http

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"sync"

	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/telemetry"
)

// SecretOwner identifies the passive monitoring point a per-point secret
// belongs to. AssetID is the point's linked asset (0 when the point has no
// asset binding); when non-zero, a report authenticated with the secret is
// bound to that asset.
type SecretOwner struct {
	PointID int64
	AssetID int64
}

// SecretRegistry holds the per-point webhook secrets of passive monitoring
// points in memory. It is the authoritative verifier for
// X-Tickraft-Signature headers signed with a point-specific secret; the
// listener-level global secret (WithSecret) remains a fallback credential.
//
// The registry is maintained by the wiring layer: loaded once at startup from
// the MonitorStore and kept in sync by the point CRUD hooks via SetPoint
// and RemovePoint. Secrets of disabled points are not registered — a disabled
// point must not authenticate ingestion.
//
// Implementations must be safe for concurrent use.
type SecretRegistry struct {
	mu       sync.RWMutex
	bySecret map[string]SecretOwner
	byPoint  map[int64]string
}

// NewSecretRegistry creates an empty registry.
func NewSecretRegistry() *SecretRegistry {
	return &SecretRegistry{
		bySecret: make(map[string]SecretOwner),
		byPoint:  make(map[int64]string),
	}
}

// SetPoint registers the point's per-point secret, replacing any secret
// previously registered for the same point. An empty secret removes the
// point's entry. Points whose config selects asset-key authentication
// (auth_type "asset-key") never register a secret — their secret, if present,
// is not an HMAC credential.
func (r *SecretRegistry) SetPoint(point telemetry.MonitorPoint) {
	secret := pointSecret(point)
	r.mu.Lock()
	defer r.mu.Unlock()
	if prev, ok := r.byPoint[point.ID]; ok {
		delete(r.bySecret, prev)
		delete(r.byPoint, point.ID)
	}
	if secret == "" {
		return
	}
	r.bySecret[secret] = SecretOwner{PointID: point.ID, AssetID: point.AssetID}
	r.byPoint[point.ID] = secret
}

// RemovePoint drops any secret registered for the point.
func (r *SecretRegistry) RemovePoint(pointID int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if prev, ok := r.byPoint[pointID]; ok {
		delete(r.bySecret, prev)
		delete(r.byPoint, pointID)
	}
}

// Len reports the number of registered secrets.
func (r *SecretRegistry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.bySecret)
}

// Match verifies the hex-encoded HMAC-SHA256 signature against every
// registered per-point secret and returns the owner of the matching one.
// Comparison is constant-time per candidate.
func (r *SecretRegistry) Match(body []byte, signature string) (SecretOwner, bool) {
	if signature == "" {
		return SecretOwner{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for secret, owner := range r.bySecret {
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(body)
		expected := hex.EncodeToString(mac.Sum(nil))
		if hmac.Equal([]byte(signature), []byte(expected)) {
			return owner, true
		}
	}
	return SecretOwner{}, false
}

// pointSecret extracts the HMAC secret from a passive point's config. Only
// enabled passive points with hmac (or unspecified) auth_type expose a
// secret; asset-key points authenticate via the asset store instead. The
// config key is snake_case on the wire (the UI request layer decamelizes
// config maps), matching the executor config key convention.
func pointSecret(point telemetry.MonitorPoint) string {
	if !point.IsPassive() || !point.Enabled {
		return ""
	}
	authType, _ := point.Config["auth_type"].(string)
	if authType == "asset-key" {
		return ""
	}
	secret, _ := point.Config["secret"].(string)
	return secret
}

// LoadSecrets populates the registry from the persistent MonitorStore: the
// per-point secrets of all enabled passive webhook points are registered so
// that signatures verify immediately after a restart. Store errors are
// returned to the caller; a partial load is not attempted.
func LoadSecrets(
	ctx context.Context,
	registry *SecretRegistry,
	store *telemetry.MonitorStore,
	logger *zap.Logger,
) error {
	points, err := store.ListPassive(ctx)
	if err != nil {
		return err
	}
	loaded := 0
	// Indexed iteration: MonitorPoint is large enough that the value copy of
	// a range clause is wasteful; pointSecret/SetPoint take the value anyway.
	for i := range points {
		secret := pointSecret(points[i])
		if secret == "" {
			continue
		}
		registry.SetPoint(points[i])
		loaded++
	}
	if logger != nil && loaded > 0 {
		logger.Info("http listener: loaded per-point webhook secrets",
			zap.Int("count", loaded))
	}
	return nil
}
