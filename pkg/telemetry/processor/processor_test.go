// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package processor

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/tickraft/tickraft/pkg/asset"
	"github.com/tickraft/tickraft/pkg/event"
	"github.com/tickraft/tickraft/pkg/pagination"
	"github.com/tickraft/tickraft/pkg/telemetry"
	"github.com/tickraft/tickraft/pkg/types"
	"go.uber.org/zap"
)

// procMockStore implements asset.Store for testing.
type procMockStore struct {
	mu     sync.RWMutex
	assets map[int64]*asset.Asset
}

func newProcMockStore() *procMockStore {
	return &procMockStore{assets: make(map[int64]*asset.Asset)}
}

func (s *procMockStore) CountByStatus(_ context.Context) (map[string]int64, error) {
	return map[string]int64{}, nil
}

func (s *procMockStore) Create(_ context.Context, r *asset.Asset) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.assets[r.ID] = r
	return nil
}

func (s *procMockStore) Update(_ context.Context, r *asset.Asset) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.assets[r.ID] = r
	return nil
}

func (s *procMockStore) GetByID(_ context.Context, id int64) (*asset.Asset, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.assets[id]
	if !ok {
		return nil, fmt.Errorf("asset not found: %d", id)
	}
	return r, nil
}

func (s *procMockStore) GetByKey(_ context.Context, tenantID int64, key string) (*asset.Asset, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, r := range s.assets {
		if r.TenantID == tenantID && r.AssetKey == key {
			return r, nil
		}
	}
	return nil, fmt.Errorf("asset not found: tenant=%d key=%s", tenantID, key)
}

func (s *procMockStore) UpdateStatus(_ context.Context, id int64, status types.AssetStatus, activeAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.assets[id]; ok {
		r.Status = status
		r.LastActiveAt = activeAt
	}
	return nil
}

func (s *procMockStore) Migrate(_ context.Context) error { return nil }

func (s *procMockStore) List(_ context.Context, page, size int, _ asset.ListFilter) ([]*asset.Asset, int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	total := int64(len(s.assets))
	all := make([]*asset.Asset, 0, total)
	for _, r := range s.assets {
		all = append(all, r)
	}
	if page < 1 {
		page = 1
	}
	if size <= 0 {
		size = 20
	}
	offset := (page - 1) * size
	if offset >= int(total) {
		return nil, total, nil
	}
	end := offset + size
	if end > int(total) {
		end = int(total)
	}
	return all[offset:end], total, nil
}

func (s *procMockStore) Delete(_ context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.assets, id)
	return nil
}

func (s *procMockStore) CountByType(_ context.Context, tenantID int64, assetType types.AssetType) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var count int64
	for _, r := range s.assets {
		if r.TenantID == tenantID && r.AssetType == assetType {
			count++
		}
	}
	return count, nil
}

func (s *procMockStore) ExistsByKey(_ context.Context, key string) (bool, error) {
	if key == "" {
		return false, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, r := range s.assets {
		if r.AssetKey == key {
			return true, nil
		}
	}
	return false, nil
}

func (s *procMockStore) ListKeyset(_ context.Context, _ pagination.PageRequest) (pagination.PageResult[*asset.Asset], error) {
	return pagination.PageResult[*asset.Asset]{}, nil
}

func TestDeviceProcessorStateTransitions(t *testing.T) {
	proc := NewDevice(newProcMockStore(), event.NewBus(), zap.NewNop())

	tests := []struct {
		name       string
		tel        *telemetry.Telemetry
		wantStatus types.AssetStatus
	}{
		{
			name: "telemetry with normal status",
			tel: &telemetry.Telemetry{
				AssetID:   1,
				AssetType: types.AssetTypeDevice,
				Status:    types.AssetStatusNormal,
			},
			wantStatus: types.AssetStatusNormal,
		},
		{
			name: "telemetry with abnormal status",
			tel: &telemetry.Telemetry{
				AssetID:   1,
				AssetType: types.AssetTypeDevice,
				Status:    types.AssetStatusAbnormal,
			},
			wantStatus: types.AssetStatusAbnormal,
		},
		{
			name: "telemetry with offline status",
			tel: &telemetry.Telemetry{
				AssetID:   1,
				AssetType: types.AssetTypeDevice,
				Status:    types.AssetStatusOffline,
			},
			wantStatus: types.AssetStatusOffline,
		},
		{
			name: "telemetry with no status defaults to normal",
			tel: &telemetry.Telemetry{
				AssetID:   1,
				AssetType: types.AssetTypeDevice,
			},
			wantStatus: types.AssetStatusNormal,
		},
		{
			name: "telemetry with abnormal metrics",
			tel: &telemetry.Telemetry{
				AssetID:   1,
				AssetType: types.AssetTypeDevice,
				Metrics: map[string]float64{
					"packet_loss": 80,
				},
			},
			wantStatus: types.AssetStatusAbnormal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := proc.Process(context.Background(), tt.tel)
			if err != nil {
				t.Fatalf("Process failed: %v", err)
			}
			if result.CurrStatus != tt.wantStatus {
				t.Errorf("CurrStatus: got %q, want %q", result.CurrStatus, tt.wantStatus)
			}
		})
	}
}

func TestDeviceProcessorMetricAlerts(t *testing.T) {
	proc := NewDevice(newProcMockStore(), event.NewBus(), zap.NewNop())

	tel := &telemetry.Telemetry{
		AssetID:   1,
		AssetType: types.AssetTypeDevice,
		Status:    types.AssetStatusNormal,
		Metrics: map[string]float64{
			"rtt_ms":      600,
			"packet_loss": 20,
		},
	}

	result, err := proc.Process(context.Background(), tel)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Should have metric alerts for high RTT and packet loss.
	if len(result.Alerts) < 2 {
		t.Errorf("expected at least 2 alerts, got %d", len(result.Alerts))
	}
}

func TestDeviceProcessorOnTimeout(t *testing.T) {
	proc := NewDevice(newProcMockStore(), event.NewBus(), zap.NewNop())

	if err := proc.OnTimeout(context.Background(), 1); err != nil {
		t.Fatalf("OnTimeout failed: %v", err)
	}
}

func TestTaskProcessorProcess(t *testing.T) {
	proc := NewTask(newProcMockStore(), event.NewBus(), zap.NewNop())

	tel := &telemetry.Telemetry{
		AssetID:   1,
		AssetType: types.AssetTypeTask,
		Status:    types.AssetStatusNormal,
	}

	result, err := proc.Process(context.Background(), tel)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	if result.CurrStatus != types.AssetStatusNormal {
		t.Errorf("CurrStatus: got %q, want %q", result.CurrStatus, types.AssetStatusNormal)
	}
}

func TestTaskProcessorAbnormalTelemetry(t *testing.T) {
	proc := NewTask(newProcMockStore(), event.NewBus(), zap.NewNop())

	tel := &telemetry.Telemetry{
		AssetID:   1,
		AssetType: types.AssetTypeTask,
		Status:    types.AssetStatusAbnormal,
	}

	result, err := proc.Process(context.Background(), tel)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	if result.CurrStatus != types.AssetStatusAbnormal {
		t.Errorf("CurrStatus: got %q, want %q", result.CurrStatus, types.AssetStatusAbnormal)
	}

	if len(result.Alerts) == 0 {
		t.Error("expected alerts for abnormal task telemetry")
	}
}

func TestTaskProcessorOnTimeout(t *testing.T) {
	proc := NewTask(newProcMockStore(), event.NewBus(), zap.NewNop())

	if err := proc.OnTimeout(context.Background(), 1); err != nil {
		t.Fatalf("OnTimeout failed: %v", err)
	}
}
