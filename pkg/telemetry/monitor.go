// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package telemetry

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"github.com/tickraft/tickraft/pkg/db/errmap"
	"github.com/tickraft/tickraft/pkg/errdefs"
	"github.com/tickraft/tickraft/pkg/pagination"
)

// MonitorStore provides CRUD operations for unified monitoring points backed
// by a GORM database. All methods map low-level driver errors to the shared
// sentinel errors (errdefs.ErrNotFound, errdefs.ErrConflict) via errmap.MapError
// so callers can use errors.Is for consistent handling.
//
// The store is the persistence layer for the MonitorPoint model. The
// ProberService uses it to list active points (Mode=ModeActive); the listener
// pipeline and API handlers use it to list passive points (Mode=ModePassive)
// or all points.
type MonitorStore struct {
	dbc *gorm.DB
}

// NewMonitorStore creates a new MonitorStore backed by the given GORM database.
func NewMonitorStore(dbc *gorm.DB) *MonitorStore {
	return &MonitorStore{dbc: dbc}
}

// List returns monitoring points filtered by an optional mode. When mode is
// empty, all points are returned ordered by ascending ID.
func (s *MonitorStore) List(ctx context.Context, mode Mode) ([]MonitorPoint, error) {
	query := s.dbc.WithContext(ctx).Model(&MonitorPoint{})
	if mode != "" {
		query = query.Where("mode = ?", mode)
	}
	var points []MonitorPoint
	if err := query.Order("id ASC").Find(&points).Error; err != nil {
		return nil, fmt.Errorf("telemetry: list monitor points: %w", errmap.MapError(err))
	}
	return points, nil
}

// ListPaged returns a page of monitoring points filtered by an optional mode,
// together with the total count. When mode is empty, all points are included.
// page is 1-based; size is normalized by pagination.Clamp.
func (s *MonitorStore) ListPaged(ctx context.Context, mode Mode, page, size int) ([]MonitorPoint, int64, error) {
	query := s.dbc.WithContext(ctx).Model(&MonitorPoint{})
	if mode != "" {
		query = query.Where("mode = ?", mode)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("telemetry: count monitor points: %w", errmap.MapError(err))
	}
	page, size = pagination.Clamp(page, size)
	offset := (page - 1) * size
	var points []MonitorPoint
	if err := query.Order("id ASC").Offset(offset).Limit(size).Find(&points).Error; err != nil {
		return nil, 0, fmt.Errorf("telemetry: list monitor points paged: %w", errmap.MapError(err))
	}
	return points, total, nil
}

// GetByID retrieves a single monitoring point by its ID. It returns
// errdefs.ErrNotFound when no point exists with the given ID.
func (s *MonitorStore) GetByID(ctx context.Context, id int64) (*MonitorPoint, error) {
	var p MonitorPoint
	if err := s.dbc.WithContext(ctx).First(&p, id).Error; err != nil {
		return nil, fmt.Errorf("telemetry: get monitor point %d: %w", id, errmap.MapError(err))
	}
	return &p, nil
}

// Create inserts a new monitoring point. The caller is responsible for
// populating all required fields (Name, Mode, Type). A nil point yields
// errdefs.ErrInvalidArgument.
func (s *MonitorStore) Create(ctx context.Context, p *MonitorPoint) error {
	if p == nil {
		return fmt.Errorf("telemetry: create monitor point: %w", errdefs.ErrInvalidArgument)
	}
	if err := s.dbc.WithContext(ctx).Create(p).Error; err != nil {
		return fmt.Errorf("telemetry: create monitor point: %w", errmap.MapError(err))
	}
	return nil
}

// pointUpdateColumns lists the user-editable columns touched by Update.
// Runtime-managed state (tenant_id, status, interval, timeout) and
// lifecycle fields (created_at) are deliberately absent so a stale or
// partial API payload can never clear them.
var pointUpdateColumns = []string{
	"name", "description", "asset_type", "asset_id", "mode", "type",
	"schedule", "enabled", "config", "updated_at",
}

// Update applies a column-level update limited to the user-editable
// columns. Unlike a full Save, the runtime-managed fields (status,
// interval, timeout) and lifecycle fields are never touched. The ID
// field identifies the row to update. It returns errdefs.ErrNotFound
// when the ID does not exist.
func (s *MonitorStore) Update(ctx context.Context, p *MonitorPoint) error {
	if p == nil {
		return fmt.Errorf("telemetry: update monitor point: %w", errdefs.ErrInvalidArgument)
	}
	result := s.dbc.WithContext(ctx).
		Model(&MonitorPoint{}).
		Where("id = ?", p.ID).
		Select(pointUpdateColumns).
		Updates(p)
	if result.Error != nil {
		return fmt.Errorf("telemetry: update monitor point %d: %w", p.ID, errmap.MapError(result.Error))
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("telemetry: update monitor point %d: %w", p.ID, errdefs.ErrNotFound)
	}
	return nil
}

// Delete permanently removes a monitoring point by ID. It returns
// errdefs.ErrNotFound when the ID does not exist.
func (s *MonitorStore) Delete(ctx context.Context, id int64) error {
	result := s.dbc.WithContext(ctx).Delete(&MonitorPoint{}, id)
	if result.Error != nil {
		return fmt.Errorf("telemetry: delete monitor point %d: %w", id, errmap.MapError(result.Error))
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("telemetry: delete monitor point %d: %w", id, errdefs.ErrNotFound)
	}
	return nil
}

// ListActive returns all monitoring points in active probing mode
// (Mode=ModeActive), ordered by ascending ID. It is a convenience wrapper
// around List for the ProberService.
func (s *MonitorStore) ListActive(ctx context.Context) ([]MonitorPoint, error) {
	return s.List(ctx, ModeActive)
}

// ListPassive returns all monitoring points in passive receiving mode
// (Mode=ModePassive), ordered by ascending ID. It is a convenience wrapper
// around List for the listener pipeline.
func (s *MonitorStore) ListPassive(ctx context.Context) ([]MonitorPoint, error) {
	return s.List(ctx, ModePassive)
}

// PointSummary aggregates monitor point counts by mode and enabled state.
// The mode counts and the enabled counts are independent dimensions over
// the same dataset (active+passive = enabled+disabled = total).
type PointSummary struct {
	// Active is the number of active probing points (Mode=ModeActive).
	Active int64 `json:"active"`
	// Passive is the number of passive receiving points (Mode=ModePassive).
	Passive int64 `json:"passive"`
	// Enabled is the number of enabled points across both modes.
	Enabled int64 `json:"enabled"`
	// Disabled is the number of disabled points across both modes.
	Disabled int64 `json:"disabled"`
}

// Summary returns aggregate monitor point counts grouped by mode and
// enabled state, computed in a single grouped query. It backs the monitor
// list summary chips so the counts reflect the full dataset instead of the
// current page.
func (s *MonitorStore) Summary(ctx context.Context) (PointSummary, error) {
	var rows []struct {
		Mode    Mode
		Enabled bool
		Count   int64 `gorm:"column:count"`
	}
	if err := s.dbc.WithContext(ctx).Model(&MonitorPoint{}).
		Select("mode, enabled, COUNT(*) AS count").
		Group("mode, enabled").
		Find(&rows).Error; err != nil {
		return PointSummary{}, fmt.Errorf("telemetry: summarize monitor points: %w", errmap.MapError(err))
	}
	var summary PointSummary
	for _, row := range rows {
		if row.Mode == ModeActive {
			summary.Active += row.Count
		}
		if row.Mode == ModePassive {
			summary.Passive += row.Count
		}
		if row.Enabled {
			summary.Enabled += row.Count
		} else {
			summary.Disabled += row.Count
		}
	}
	return summary, nil
}
