// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package telemetry provides the database-backed TelemetryService
// implementation that bridges the handler layer with the persistent
// MonitorStore. It implements the telemetryhandler.Service interface
// defined in pkg/api/handler/telemetry.
package telemetry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/api/handler"
	telemetryhandler "github.com/tickraft/tickraft/pkg/api/handler/telemetry"
	"github.com/tickraft/tickraft/pkg/api/httputil"
	"github.com/tickraft/tickraft/pkg/errdefs"
	"github.com/tickraft/tickraft/pkg/quota"
	"github.com/tickraft/tickraft/pkg/telemetry"
)

// Compile-time assertion that Service implements telemetryhandler.Service.
var _ telemetryhandler.Service = (*Service)(nil)

// PointUpsertHandler is called after a monitoring point is created or
// updated. It allows the caller to register/re-register the point with
// the prober scheduling engine.
type PointUpsertHandler func(ctx context.Context, point telemetry.MonitorPoint) error

// PointDeleteHandler is called after a monitoring point is deleted. It
// allows the caller to unregister the point from the prober scheduling engine.
type PointDeleteHandler func(ctx context.Context, pointID int64) error

// Service implements telemetryhandler.Service backed by the persistent
// MonitorStore. All CRUD operations are persisted to the monitor_points table
// via GORM, surviving process restarts.
type Service struct {
	store         *telemetry.MonitorStore
	logger        *zap.Logger
	onPointUpsert PointUpsertHandler
	onPointDelete PointDeleteHandler
}

// Option configures a Service at construction time.
type Option func(*Service)

// WithPointHandlers injects callbacks that are invoked after a monitoring
// point is created/updated or deleted. These are used to wire the
// ProberService so active points are scheduled in real time.
func WithPointHandlers(upsert PointUpsertHandler, del PointDeleteHandler) Option {
	return func(s *Service) {
		s.onPointUpsert = upsert
		s.onPointDelete = del
	}
}

// NewService creates a database-backed telemetry Service from the given
// MonitorStore. If logger is nil, a no-op logger is used.
func NewService(store *telemetry.MonitorStore, logger *zap.Logger, opts ...Option) *Service {
	if logger == nil {
		logger = zap.NewNop()
	}
	s := &Service{store: store, logger: logger}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// ListTasks returns a page of telemetry tasks ordered by ascending ID, plus
// the total count. When filter.Mode is non-empty, only tasks whose Mode matches
// are returned.
func (s *Service) ListTasks(ctx context.Context, page, size int, filter telemetryhandler.Filter) ([]telemetryhandler.Task, int64, error) {
	page, size = httputil.ClampPaging(page, size)

	mode := telemetry.Mode(filter.Mode)
	points, total, err := s.store.ListPaged(ctx, mode, page, size)
	if err != nil {
		return nil, 0, mapError(err)
	}

	result := make([]telemetryhandler.Task, 0, len(points))
	for i := range points {
		result = append(result, *pointToTask(&points[i]))
	}
	return result, total, nil
}

// GetTask returns a single telemetry task by ID.
func (s *Service) GetTask(ctx context.Context, id int64) (*telemetryhandler.Task, error) {
	p, err := s.store.GetByID(ctx, id)
	if err != nil {
		return nil, mapError(err)
	}
	return pointToTask(p), nil
}

// CreateTask creates a new telemetry task from the given request, applies quota
// checks, and persists it.
func (s *Service) CreateTask(ctx context.Context, req *telemetryhandler.Task) (*telemetryhandler.Task, error) {
	if req == nil {
		return nil, telemetryhandler.ErrInvalidRequest
	}
	if err := s.checkProberQuotaForCreate(ctx, req.Mode); err != nil {
		return nil, err
	}
	if err := checkHTTPIntervalQuota(req.Mode, req.Type, req.Schedule); err != nil {
		return nil, handler.NewServiceError(http.StatusBadRequest, errdefs.CodeBadRequest, err.Error())
	}

	p := taskToPoint(req)
	p.Status = telemetry.MonitorStatusInactive
	if err := s.store.Create(ctx, p); err != nil {
		return nil, mapError(err)
	}
	created := pointToTask(p)
	s.logger.Info("telemetry task created", zap.Int64("id", p.ID), zap.String("name", p.Name))

	// Schedule the point with the prober engine if it is active+enabled.
	// Errors are logged but do not fail the create: the point is already
	// persisted and will be picked up on the next ProberService.Start.
	if s.onPointUpsert != nil {
		if err := s.onPointUpsert(ctx, *p); err != nil {
			s.logger.Warn("telemetry task created but prober registration failed",
				zap.Int64("id", p.ID),
				zap.Error(err),
			)
		}
	}
	return created, nil
}

// UpdateTask merges the request fields onto the existing task. The ID and
// CreatedAt are preserved; UpdatedAt is refreshed by GORM auto-update.
func (s *Service) UpdateTask(ctx context.Context, id int64, req *telemetryhandler.Task) (*telemetryhandler.Task, error) {
	if req == nil {
		return nil, telemetryhandler.ErrInvalidRequest
	}

	existing, err := s.store.GetByID(ctx, id)
	if err != nil {
		return nil, mapError(err)
	}

	if err := s.checkProberQuotaForUpdate(ctx, string(existing.Mode), req.Mode); err != nil {
		return nil, err
	}
	if err := checkHTTPIntervalQuota(req.Mode, req.Type, req.Schedule); err != nil {
		return nil, handler.NewServiceError(http.StatusBadRequest, errdefs.CodeBadRequest, err.Error())
	}

	// Unregister the old point from the prober engine before updating.
	// This covers mode changes (active→passive) and disabled points.
	// If the point stays active, RegisterPoint in the post-update hook
	// will re-register it with the new schedule.
	if s.onPointDelete != nil {
		if err := s.onPointDelete(ctx, id); err != nil {
			s.logger.Warn("telemetry task update: unregister old point failed",
				zap.Int64("id", id),
				zap.Error(err),
			)
		}
	}

	mergeTaskOntoPoint(existing, req)
	if err := s.store.Update(ctx, existing); err != nil {
		return nil, mapError(err)
	}
	updated := pointToTask(existing)
	s.logger.Info("telemetry task updated", zap.Int64("id", id))

	// Re-register the updated point if it is active+enabled.
	if s.onPointUpsert != nil {
		if err := s.onPointUpsert(ctx, *existing); err != nil {
			s.logger.Warn("telemetry task updated but prober registration failed",
				zap.Int64("id", id),
				zap.Error(err),
			)
		}
	}
	return updated, nil
}

// DeleteTask removes a telemetry task by ID.
func (s *Service) DeleteTask(ctx context.Context, id int64) error {
	if err := s.store.Delete(ctx, id); err != nil {
		return mapError(err)
	}
	s.logger.Info("telemetry task deleted", zap.Int64("id", id))

	// Unregister the point from the prober engine.
	if s.onPointDelete != nil {
		if err := s.onPointDelete(ctx, id); err != nil {
			s.logger.Warn("telemetry task deleted but prober unregistration failed",
				zap.Int64("id", id),
				zap.Error(err),
			)
		}
	}
	return nil
}

// --- Quota helpers ---

// checkProberQuotaForCreate returns an error when creating a task whose Mode
// is "active" would exceed the TypeProber ceiling.
func (s *Service) checkProberQuotaForCreate(ctx context.Context, mode string) error {
	if !strings.EqualFold(mode, string(telemetry.ModeActive)) {
		return nil
	}
	ceiling := quota.Ceiling(quota.TypeProber)
	if ceiling <= 0 {
		return nil
	}
	active, err := s.store.ListActive(ctx)
	if err != nil {
		return mapError(err)
	}
	if len(active) >= ceiling {
		return handler.NewServiceError(
			http.StatusConflict, errdefs.CodeConflict,
			fmt.Sprintf("prober quota exceeded: maximum %d active probers", ceiling),
		)
	}
	return nil
}

// checkProberQuotaForUpdate returns an error when the mode transitions to
// "active" and the new active count would exceed the TypeProber ceiling.
func (s *Service) checkProberQuotaForUpdate(ctx context.Context, oldMode, newMode string) error {
	if !strings.EqualFold(newMode, string(telemetry.ModeActive)) {
		return nil
	}
	if strings.EqualFold(oldMode, string(telemetry.ModeActive)) {
		return nil
	}
	ceiling := quota.Ceiling(quota.TypeProber)
	if ceiling <= 0 {
		return nil
	}
	active, err := s.store.ListActive(ctx)
	if err != nil {
		return mapError(err)
	}
	if len(active) >= ceiling {
		return handler.NewServiceError(
			http.StatusConflict, errdefs.CodeConflict,
			fmt.Sprintf("prober quota exceeded: maximum %d active probers", ceiling),
		)
	}
	return nil
}

// checkHTTPIntervalQuota validates the schedule of an active HTTP prober
// against the minimum HTTP probe interval quota (TypeHTTPInterval, in seconds).
func checkHTTPIntervalQuota(mode, typ, schedule string) error {
	if !strings.EqualFold(mode, string(telemetry.ModeActive)) || !strings.EqualFold(typ, "http") {
		return nil
	}
	ceiling := quota.Ceiling(quota.TypeHTTPInterval)
	if ceiling <= 0 {
		return nil
	}
	interval, err := time.ParseDuration(schedule)
	if err != nil {
		return nil
	}
	minInterval := time.Duration(ceiling) * time.Second
	if interval < minInterval {
		return fmt.Errorf("HTTP probe interval %s is below minimum %s", interval, minInterval)
	}
	return nil
}

// --- Mapping helpers ---

// pointToTask converts a MonitorPoint to a handler Task DTO.
func pointToTask(p *telemetry.MonitorPoint) *telemetryhandler.Task {
	t := &telemetryhandler.Task{
		ID:          p.ID,
		Name:        p.Name,
		Description: p.Description,
		AssetType:   p.AssetType,
		AssetID:     p.AssetID,
		Mode:        string(p.Mode),
		Type:        p.Type,
		Schedule:    p.Schedule,
		Enabled:     p.Enabled,
		CreatedAt:   p.CreatedAt,
		UpdatedAt:   p.UpdatedAt,
	}
	if p.Config != "" {
		var cfg map[string]any
		if err := json.Unmarshal([]byte(p.Config), &cfg); err == nil {
			t.Config = cfg
		}
	}
	return t
}

// taskToPoint converts a handler Task DTO to a MonitorPoint. ID is preserved
// from the DTO; timestamps are left zero so GORM auto-create/auto-update
// populate them on insert.
func taskToPoint(t *telemetryhandler.Task) *telemetry.MonitorPoint {
	p := &telemetry.MonitorPoint{
		ID:          t.ID,
		Name:        t.Name,
		Description: t.Description,
		AssetType:   t.AssetType,
		AssetID:     t.AssetID,
		Mode:        telemetry.Mode(t.Mode),
		Type:        t.Type,
		Schedule:    t.Schedule,
		Enabled:     t.Enabled,
	}
	p.Config = configToString(t.Config)
	return p
}

// mergeTaskOntoPoint copies editable fields from the Task DTO onto an existing
// MonitorPoint, preserving ID, CreatedAt, Status, Interval, and Timeout.
func mergeTaskOntoPoint(p *telemetry.MonitorPoint, t *telemetryhandler.Task) {
	p.Name = t.Name
	p.Description = t.Description
	p.AssetType = t.AssetType
	p.AssetID = t.AssetID
	p.Mode = telemetry.Mode(t.Mode)
	p.Type = t.Type
	p.Schedule = t.Schedule
	p.Enabled = t.Enabled
	p.Config = configToString(t.Config)
}

// configToString marshals a config map to a JSON string. Returns "" for nil or
// empty maps.
func configToString(cfg map[string]any) string {
	if len(cfg) == 0 {
		return ""
	}
	b, err := json.Marshal(cfg)
	if err != nil {
		return ""
	}
	return string(b)
}

// mapError translates MonitorStore errors into handler-level service errors.
func mapError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, errdefs.ErrNotFound) {
		return telemetryhandler.ErrTelemetryTaskNotFound
	}
	if errors.Is(err, errdefs.ErrInvalidArgument) {
		return telemetryhandler.ErrInvalidRequest
	}
	return handler.NewServiceError(http.StatusInternalServerError, errdefs.CodeInternal, err.Error())
}
