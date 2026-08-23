// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package telemetry provides the database-backed TelemetryService
// implementation that bridges the handler layer with the persistent
// MonitorStore. It implements the telemetryhandler.Service interface
// defined in pkg/api/handler/telemetry. The wire shape and the storage
// shape are the same telemetry.MonitorPoint model, so this service only
// validates, enforces quotas, and orchestrates the store — there is no
// DTO conversion.
package telemetry

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/api/handler"
	telemetryhandler "github.com/tickraft/tickraft/pkg/api/handler/telemetry"
	"github.com/tickraft/tickraft/pkg/errdefs"
	"github.com/tickraft/tickraft/pkg/pagination"
	"github.com/tickraft/tickraft/pkg/quota"
	"github.com/tickraft/tickraft/pkg/telemetry"
)

// Compile-time assertion that Service implements telemetryhandler.Service.
var _ telemetryhandler.Service = (*Service)(nil)

// PointUpsertHandler is called after a monitoring point is created or
// updated. It allows the caller to register/re-register the point with
// the prober scheduling engine or the passive offline-detection wheel.
type PointUpsertHandler func(ctx context.Context, point telemetry.MonitorPoint) error

// PointDeleteHandler is called after a monitoring point is deleted, and
// before the row is rewritten on update (to unregister the old shape).
// It receives the full pre-delete point so the handler can branch on the
// point's mode and asset.
type PointDeleteHandler func(ctx context.Context, point telemetry.MonitorPoint) error

// Service implements telemetryhandler.Service backed by the persistent
// MonitorStore. All CRUD operations are persisted to the monitor_points table
// via GORM, surviving process restarts.
type Service struct {
	store            *telemetry.MonitorStore
	logger           *zap.Logger
	onPointUpsert    PointUpsertHandler
	onPointDelete    PointDeleteHandler
	validateExecutor func(executorType string) error
	probeNow         func(ctx context.Context, pointID int64) error
}

// Option configures a Service at construction time.
type Option interface {
	apply(*Service)
}

// pointHandlersOption injects the point upsert/delete callbacks.
type pointHandlersOption struct {
	upsert PointUpsertHandler
	del    PointDeleteHandler
}

func (o pointHandlersOption) apply(s *Service) {
	s.onPointUpsert = o.upsert
	s.onPointDelete = o.del
}

// WithPointHandlers injects callbacks that are invoked after a monitoring
// point is created/updated or deleted. These are used to wire the
// ProberService so active points are scheduled in real time.
func WithPointHandlers(upsert PointUpsertHandler, del PointDeleteHandler) Option {
	return pointHandlersOption{upsert, del}
}

// executorValidatorOption injects the active-point executor capability check.
type executorValidatorOption struct {
	v func(executorType string) error
}

func (o executorValidatorOption) apply(s *Service) { s.validateExecutor = o.v }

// WithExecutorValidator injects a callback invoked before an active
// monitoring point is created/updated. It receives the point's executor
// type and returns an error when the type cannot probe (lacks CapProbe),
// turning "point registered but every probe fails capability lookup" into
// an immediate 400.
func WithExecutorValidator(v func(executorType string) error) Option {
	return executorValidatorOption{v}
}

// probeTriggerOption injects the on-demand probe dispatcher.
type probeTriggerOption struct {
	fn func(ctx context.Context, pointID int64) error
}

func (o probeTriggerOption) apply(s *Service) { s.probeNow = o.fn }

// WithProbeTrigger injects the callback invoked by ProbeNow to dispatch a
// real probe through the prober scheduling engine. The natural wiring is
// ProberService.ProbeNow.
func WithProbeTrigger(fn func(ctx context.Context, pointID int64) error) Option {
	return probeTriggerOption{fn}
}

// NewService creates a database-backed telemetry Service from the given
// MonitorStore. If logger is nil, a no-op logger is used.
func NewService(store *telemetry.MonitorStore, logger *zap.Logger, options ...Option) *Service {
	if logger == nil {
		logger = zap.NewNop()
	}
	s := &Service{store: store, logger: logger}
	for _, o := range options {
		o.apply(s)
	}
	return s
}

// ListTasks returns a page of telemetry tasks ordered by ascending ID, plus
// the total count. When filter.Mode is non-empty, only tasks whose Mode matches
// are returned.
func (s *Service) ListTasks(
	ctx context.Context,
	page, size int,
	filter telemetryhandler.Filter,
) ([]telemetry.MonitorPoint, int64, error) {
	page, size = pagination.Clamp(page, size)

	mode := telemetry.Mode(filter.Mode)
	points, total, err := s.store.ListPaged(ctx, mode, page, size)
	if err != nil {
		return nil, 0, mapError(err)
	}

	return points, total, nil
}

// GetTask returns a single telemetry task by ID.
func (s *Service) GetTask(ctx context.Context, id int64) (*telemetry.MonitorPoint, error) {
	p, err := s.store.GetByID(ctx, id)
	if err != nil {
		return nil, mapError(err)
	}
	return p, nil
}

// CreateTask creates a new telemetry task from the given request, applies quota
// checks, and persists it.
func (s *Service) CreateTask(ctx context.Context, req *telemetry.MonitorPoint) (*telemetry.MonitorPoint, error) {
	if req == nil {
		return nil, telemetryhandler.ErrInvalidRequest
	}
	if err := s.checkProberQuotaForCreate(ctx, string(req.Mode)); err != nil {
		return nil, err
	}
	if err := checkHTTPIntervalQuota(string(req.Mode), req.Type, req.Schedule); err != nil {
		return nil, handler.NewServiceError(http.StatusBadRequest, errdefs.CodeBadRequest, err.Error())
	}
	if err := s.checkActiveExecutor(string(req.Mode), req.Type); err != nil {
		return nil, err
	}

	// Server-assigned fields are dropped from the request so a client
	// cannot pick its own row ID, timestamps, or runtime state (status,
	// interval, timeout are not wire-bindable via json:"-"); Create
	// back-fills ID, CreatedAt, and UpdatedAt after the insert.
	req.ID = 0
	req.CreatedAt = time.Time{}
	req.UpdatedAt = time.Time{}
	req.Status = telemetry.MonitorStatusInactive
	if err := s.store.Create(ctx, req); err != nil {
		return nil, mapError(err)
	}
	s.logger.Info("telemetry task created", zap.Int64("id", req.ID), zap.String("name", req.Name))

	// Schedule the point with the prober engine if it is active+enabled.
	// Errors are logged but do not fail the create: the point is already
	// persisted and will be picked up on the next ProberService.Start.
	if s.onPointUpsert != nil {
		if err := s.onPointUpsert(ctx, *req); err != nil {
			s.logger.Warn("telemetry task created but prober registration failed",
				zap.Int64("id", req.ID),
				zap.Error(err),
			)
		}
	}
	return req, nil
}

// UpdateTask merges the request fields onto the existing task. The ID and
// CreatedAt are preserved; UpdatedAt is refreshed by GORM auto-update.
func (s *Service) UpdateTask(
	ctx context.Context,
	id int64,
	req *telemetry.MonitorPoint,
) (*telemetry.MonitorPoint, error) {
	if req == nil {
		return nil, telemetryhandler.ErrInvalidRequest
	}

	existing, err := s.store.GetByID(ctx, id)
	if err != nil {
		return nil, mapError(err)
	}

	if err := s.checkProberQuotaForUpdate(ctx, string(existing.Mode), string(req.Mode)); err != nil {
		return nil, err
	}
	if err := checkHTTPIntervalQuota(string(req.Mode), req.Type, req.Schedule); err != nil {
		return nil, handler.NewServiceError(http.StatusBadRequest, errdefs.CodeBadRequest, err.Error())
	}
	if err := s.checkActiveExecutor(string(req.Mode), req.Type); err != nil {
		return nil, err
	}

	// Unregister the old point shape from the scheduling engines before
	// updating. This covers mode changes (active→passive) and disabled
	// points. If the point stays active, RegisterPoint in the post-update
	// hook will re-register it with the new schedule.
	if s.onPointDelete != nil {
		if err := s.onPointDelete(ctx, *existing); err != nil {
			s.logger.Warn("telemetry task update: unregister old point failed",
				zap.Int64("id", id),
				zap.Error(err),
			)
		}
	}

	// Copy the editable fields onto the existing row; runtime state
	// (status, interval, timeout — not wire-bindable anyway) and
	// server-assigned lifecycle fields stay as loaded.
	existing.Name = req.Name
	existing.Description = req.Description
	existing.AssetType = req.AssetType
	existing.AssetID = req.AssetID
	existing.Mode = req.Mode
	existing.Type = req.Type
	existing.Schedule = req.Schedule
	existing.Enabled = req.Enabled
	existing.Config = req.Config
	if err := s.store.Update(ctx, existing); err != nil {
		return nil, mapError(err)
	}
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
	return existing, nil
}

// DeleteTask removes a telemetry task by ID.
func (s *Service) DeleteTask(ctx context.Context, id int64) error {
	// Fetch the point before deleting so the unregister hook receives the
	// full shape (mode, asset) it needs to reconcile the right engine.
	existing, err := s.store.GetByID(ctx, id)
	if err != nil {
		return mapError(err)
	}
	if err := s.store.Delete(ctx, id); err != nil {
		return mapError(err)
	}
	s.logger.Info("telemetry task deleted", zap.Int64("id", id))

	// Unregister the point from the scheduling engines.
	if s.onPointDelete != nil {
		if err := s.onPointDelete(ctx, *existing); err != nil {
			s.logger.Warn("telemetry task deleted but unregistration failed",
				zap.Int64("id", id),
				zap.Error(err),
			)
		}
	}
	return nil
}

// ProbeNow dispatches an on-demand probe for an active, enabled monitoring
// point and returns the point for status rendering. The probe runs
// asynchronously: its result lands in the probe record store, and clients
// poll the status/history endpoints for the refreshed outcome. Passive
// points and disabled points are rejected with 400.
func (s *Service) ProbeNow(ctx context.Context, id int64) (*telemetry.MonitorPoint, error) {
	point, err := s.store.GetByID(ctx, id)
	if err != nil {
		return nil, mapError(err)
	}
	if !point.IsActive() {
		return nil, handler.NewServiceError(http.StatusBadRequest, errdefs.CodeBadRequest,
			fmt.Sprintf("point %d is passive; on-demand probe requires an active point", id))
	}
	if !point.Enabled {
		return nil, handler.NewServiceError(http.StatusBadRequest, errdefs.CodeBadRequest,
			fmt.Sprintf("point %d is disabled; enable it before probing", id))
	}
	if s.probeNow == nil {
		return nil, handler.NewServiceError(http.StatusServiceUnavailable, errdefs.CodeInternal,
			"on-demand probing is unavailable: prober service is not running")
	}
	if err := s.probeNow(ctx, id); err != nil {
		s.logger.Warn("on-demand probe dispatch failed",
			zap.Int64("id", id),
			zap.Error(err),
		)
		return nil, handler.NewServiceError(http.StatusInternalServerError, errdefs.CodeInternal,
			fmt.Sprintf("failed to dispatch probe for point %d", id))
	}
	s.logger.Info("on-demand probe dispatched", zap.Int64("id", id))
	return point, nil
}

// Summary returns aggregate monitor point counts by mode and enabled state
// over the full dataset. It backs the monitor list summary chips.
func (s *Service) Summary(ctx context.Context) (telemetry.PointSummary, error) {
	return s.store.Summary(ctx)
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

// checkActiveExecutor validates that an active point's executor type can
// probe. Passive points do not run through the executor pipeline (their Type
// names a listener), so they are exempt.
func (s *Service) checkActiveExecutor(mode, executorType string) error {
	if !strings.EqualFold(mode, string(telemetry.ModeActive)) || s.validateExecutor == nil {
		return nil
	}
	if err := s.validateExecutor(executorType); err != nil {
		return handler.NewServiceError(
			http.StatusBadRequest, errdefs.CodeBadRequest,
			fmt.Sprintf("executor type %q cannot probe: %v", executorType, err),
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
		//nolint:nilerr // intentional: non-interval schedules (e.g. cron expressions) do not parse as
		// durations and are not subject to the interval quota
		return nil
	}
	minInterval := time.Duration(ceiling) * time.Second
	if interval < minInterval {
		return fmt.Errorf("HTTP probe interval %s is below minimum %s", interval, minInterval)
	}
	return nil
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
