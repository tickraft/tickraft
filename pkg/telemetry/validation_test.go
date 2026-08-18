// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package telemetry

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/asset"
	"github.com/tickraft/tickraft/pkg/types"
)

// newTestValidator creates a Validator backed by a mock store pre-populated
// with a single asset (id=1, tenant=1, type=device).
func newTestValidator() (*Validator, *mgrMockStore) {
	store := newMgrMockStore()
	_ = store.Create(context.Background(), &asset.Asset{
		ID:        1,
		TenantID:  1,
		AssetType: types.AssetTypeDevice,
		AssetKey:  "device-1",
		Status:    types.AssetStatusUnknown,
	})
	return NewValidator(store, zap.NewNop()), store
}

func TestValidatorValidTelemetry(t *testing.T) {
	v, _ := newTestValidator()
	tel := &Telemetry{
		AssetID:    1,
		TenantID:   1,
		AssetType:  types.AssetTypeDevice,
		Metrics:    map[string]float64{"cpu": 50.0},
		LogContent: "ok",
	}
	if err := v.Validate(context.Background(), tel); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestValidatorAssetIDInvalid(t *testing.T) {
	v, _ := newTestValidator()
	tel := &Telemetry{
		AssetID:   0,
		TenantID:  1,
		AssetType: types.AssetTypeDevice,
	}
	err := v.Validate(context.Background(), tel)
	if err == nil {
		t.Fatal("expected error for zero AssetID")
	}
	if !errors.Is(err, ErrValidationFailed) {
		t.Errorf("expected ErrValidationFailed, got %v", err)
	}
}

func TestValidatorAssetTypeEmpty(t *testing.T) {
	v, _ := newTestValidator()
	tel := &Telemetry{
		AssetID:  1,
		TenantID: 1,
	}
	err := v.Validate(context.Background(), tel)
	if err == nil {
		t.Fatal("expected error for empty AssetType")
	}
	if !errors.Is(err, ErrValidationFailed) {
		t.Errorf("expected ErrValidationFailed, got %v", err)
	}
}

func TestValidatorAssetNotFound(t *testing.T) {
	v, _ := newTestValidator()
	tel := &Telemetry{
		AssetID:   999,
		TenantID:  1,
		AssetType: types.AssetTypeDevice,
	}
	err := v.Validate(context.Background(), tel)
	if err == nil {
		t.Fatal("expected error for missing asset")
	}
	if !errors.Is(err, ErrAssetNotFound) {
		t.Errorf("expected ErrAssetNotFound, got %v", err)
	}
}

func TestValidatorTenantMismatch(t *testing.T) {
	v, _ := newTestValidator()
	tel := &Telemetry{
		AssetID:   1,
		TenantID:  2,
		AssetType: types.AssetTypeDevice,
	}
	err := v.Validate(context.Background(), tel)
	if err == nil {
		t.Fatal("expected error for tenant mismatch")
	}
	if !errors.Is(err, ErrTenantMismatch) {
		t.Errorf("expected ErrTenantMismatch, got %v", err)
	}
}

func TestValidatorMetricsExceeded(t *testing.T) {
	v, _ := newTestValidator()
	metrics := make(map[string]float64, MaxMetricsPerReport+1)
	for i := 0; i <= MaxMetricsPerReport; i++ {
		metrics[fmt.Sprintf("m%d", i)] = float64(i)
	}
	tel := &Telemetry{
		AssetID:   1,
		TenantID:  1,
		AssetType: types.AssetTypeDevice,
		Metrics:   metrics,
	}
	err := v.Validate(context.Background(), tel)
	if err == nil {
		t.Fatal("expected error for metrics exceeding limit")
	}
	if !errors.Is(err, ErrMetricLimitExceeded) {
		t.Errorf("expected ErrMetricLimitExceeded, got %v", err)
	}
}

func TestValidatorLogBodyExceeded(t *testing.T) {
	v, _ := newTestValidator()
	tel := &Telemetry{
		AssetID:    1,
		TenantID:   1,
		AssetType:  types.AssetTypeDevice,
		LogContent: strings.Repeat("x", MaxLogBodyBytes+1),
	}
	err := v.Validate(context.Background(), tel)
	if err == nil {
		t.Fatal("expected error for oversized log body")
	}
	if !errors.Is(err, ErrLogLimitExceeded) {
		t.Errorf("expected ErrLogLimitExceeded, got %v", err)
	}
}

func TestValidatorNilStoreSkipsAssetCheck(t *testing.T) {
	v := NewValidator(nil, zap.NewNop())
	tel := &Telemetry{
		AssetID:   1,
		TenantID:  1,
		AssetType: types.AssetTypeDevice,
	}
	// With no store, asset existence and tenant checks are skipped.
	if err := v.Validate(context.Background(), tel); err != nil {
		t.Fatalf("expected nil error with nil store, got %v", err)
	}
}

// countingStore wraps a asset.Store and counts GetByID calls so tests
// can verify that the validator cache reduces database queries.
type countingStore struct {
	asset.Store
	mu           sync.Mutex
	getByIDCalls int
}

func (s *countingStore) GetByID(ctx context.Context, id int64) (*asset.Asset, error) {
	s.mu.Lock()
	s.getByIDCalls++
	s.mu.Unlock()
	return s.Store.GetByID(ctx, id)
}

func (s *countingStore) calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.getByIDCalls
}

func TestValidatorCacheReducesDBQueries(t *testing.T) {
	base := newMgrMockStore()
	_ = base.Create(context.Background(), &asset.Asset{
		ID:        1,
		TenantID:  1,
		AssetType: types.AssetTypeDevice,
		AssetKey:  "device-1",
		Status:    types.AssetStatusUnknown,
	})
	store := &countingStore{Store: base}
	v := NewValidator(store, zap.NewNop())

	tel := &Telemetry{
		AssetID:   1,
		TenantID:  1,
		AssetType: types.AssetTypeDevice,
		Metrics:   map[string]float64{"cpu": 50.0},
	}

	// First validation: cache miss, expect a DB query.
	if err := v.Validate(context.Background(), tel); err != nil {
		t.Fatalf("first validate failed: %v", err)
	}
	if got := store.calls(); got != 1 {
		t.Fatalf("expected 1 DB query after first validate, got %d", got)
	}

	// Second validation: cache hit, expect no additional DB query.
	if err := v.Validate(context.Background(), tel); err != nil {
		t.Fatalf("second validate failed: %v", err)
	}
	if got := store.calls(); got != 1 {
		t.Fatalf("expected 1 DB query after second validate (cache hit), got %d", got)
	}

	// Invalidate the cache entry; next validation should query the store again.
	v.InvalidateAsset(context.Background(), 1)
	if err := v.Validate(context.Background(), tel); err != nil {
		t.Fatalf("third validate failed: %v", err)
	}
	if got := store.calls(); got != 2 {
		t.Fatalf("expected 2 DB queries after invalidation, got %d", got)
	}
}

func TestValidatorCacheEvictsOnTenantMismatch(t *testing.T) {
	base := newMgrMockStore()
	_ = base.Create(context.Background(), &asset.Asset{
		ID:        1,
		TenantID:  1,
		AssetType: types.AssetTypeDevice,
		AssetKey:  "device-1",
		Status:    types.AssetStatusUnknown,
	})
	store := &countingStore{Store: base}
	v := NewValidator(store, zap.NewNop())

	// Prime the cache with a valid telemetry.
	validTel := &Telemetry{
		AssetID:   1,
		TenantID:  1,
		AssetType: types.AssetTypeDevice,
	}
	if err := v.Validate(context.Background(), validTel); err != nil {
		t.Fatalf("prime validate failed: %v", err)
	}

	// A tenant-mismatch telemetry should still hit the cache (no extra DB query)
	// and return ErrTenantMismatch.
	mismatchTel := &Telemetry{
		AssetID:   1,
		TenantID:  2,
		AssetType: types.AssetTypeDevice,
	}
	err := v.Validate(context.Background(), mismatchTel)
	if err == nil {
		t.Fatal("expected ErrTenantMismatch")
	}
	if !errors.Is(err, ErrTenantMismatch) {
		t.Fatalf("expected ErrTenantMismatch, got %v", err)
	}
	if got := store.calls(); got != 1 {
		t.Fatalf("expected 1 DB query (cached), got %d", got)
	}
}
