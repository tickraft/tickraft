// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package telemetry

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/tickraft/tickraft/pkg/executor"
	"github.com/tickraft/tickraft/pkg/task"
	"go.uber.org/zap"
)

// proberTaskIDOffset separates prober task IDs from regular scheduled task
// IDs in the shared task.Manager. Regular tasks use auto-increment IDs from
// sys_schedule_task (starting at 1). Prober tasks use this offset plus the
// monitor_point ID to avoid collision in the scheduling engine and task store.
const proberTaskIDOffset = int64(1) << 40

func proberTaskID(pointID int64) int64 {
	return proberTaskIDOffset + pointID
}

// ProberService manages active probing by holding a task.Manager
// instance and consuming executor.Prober executors. When a monitoring
// point (Mode=ModeActive) is registered, ProberService schedules it via the
// task.Manager. On fire, the task.Manager publishes an ExecutionTriggered
// event; the executor runner picks it up, runs the prober executor, and
// publishes the result.
//
// The service operates exclusively on MonitorPoint records where
// Mode=ModeActive. Passive points (Mode=ModePassive) are handled by the
// listener pipeline and are never touched by this service.
type ProberService struct {
	sched   task.Manager
	execReg *executor.Registry
	manager *Manager
	// store persists and queries monitoring points backed by the
	// monitor_points table.
	store  *MonitorStore
	logger *zap.Logger
}

// ProberOption configures a ProberService at construction time.
type ProberOption func(*ProberService)

// WithProberMonitorStore injects a MonitorStore for querying and persisting
// active monitoring points.
func WithProberMonitorStore(store *MonitorStore) ProberOption {
	return func(s *ProberService) { s.store = store }
}

// NewProberService creates a ProberService with the given task engine,
// executor registry, and optional configuration. The variadic options allow
// callers to inject a MonitorStore for point persistence without changing
// the positional signature.
func NewProberService(sched task.Manager, execReg *executor.Registry, manager *Manager, logger *zap.Logger, opts ...ProberOption) *ProberService {
	s := &ProberService{
		sched:   sched,
		execReg: execReg,
		manager: manager,
		logger:  logger,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// ListActivePoints returns all monitoring points in active probing mode
// (Mode=ModeActive) from the injected MonitorStore. When no store is
// configured, it returns an empty slice and a nil error.
func (s *ProberService) ListActivePoints(ctx context.Context) ([]MonitorPoint, error) {
	if s.store == nil {
		return nil, nil
	}
	return s.store.ListActive(ctx)
}

// RegisterPoint registers an active monitoring point for periodic probing
// via the task.Manager. The point must have Mode=ModeActive and Enabled=true;
// a passive point is rejected with an error and a disabled point is skipped
// silently.
func (s *ProberService) RegisterPoint(ctx context.Context, point MonitorPoint) error {
	if !point.IsActive() {
		return fmt.Errorf("telemetry: cannot register passive point as active prober")
	}
	if !point.Enabled {
		return nil
	}
	if s.sched == nil {
		return fmt.Errorf("telemetry: prober scheduler is nil")
	}
	t := pointToProbeTask(point)
	if err := s.sched.Register(ctx, t); err != nil {
		return fmt.Errorf("register prober point %d: %w", point.ID, err)
	}
	s.logger.Info("prober point registered",
		zap.Int64("point_id", point.ID),
		zap.String("type", point.Type),
	)
	return nil
}

// UnregisterPoint removes an active monitoring point from the scheduling
// engine and the task store.
func (s *ProberService) UnregisterPoint(ctx context.Context, pointID int64) error {
	if s.sched == nil {
		return nil
	}
	taskID := proberTaskID(pointID)
	if err := s.sched.Unschedule(ctx, taskID); err != nil {
		return fmt.Errorf("unregister prober point %d: %w", pointID, err)
	}
	s.logger.Info("prober point unregistered", zap.Int64("point_id", pointID))
	return nil
}

// Start loads all active, enabled monitoring points from the store and
// registers each with the scheduling engine. Points with invalid schedules
// are skipped with a warning log.
//
// The scheduler engine lifecycle (SubscribeEvents + Restore) is owned by
// the application bootstrap; ProberService coordinates active point
// registration and does not start the shared scheduler itself to avoid
// double-subscribing event handlers.
func (s *ProberService) Start(ctx context.Context) error {
	if s.store == nil {
		s.logger.Warn("prober service starting without monitor store; active points will not be scheduled")
		return nil
	}
	points, err := s.store.ListActive(ctx)
	if err != nil {
		return fmt.Errorf("prober start: load active points: %w", err)
	}
	registered := 0
	for _, p := range points {
		if !p.Enabled {
			continue
		}
		if err := s.RegisterPoint(ctx, p); err != nil {
			s.logger.Warn("prober start: register point",
				zap.Int64("point_id", p.ID),
				zap.String("type", p.Type),
				zap.Error(err),
			)
			continue
		}
		registered++
	}
	s.logger.Info("prober service started",
		zap.Int("active_points", len(points)),
		zap.Int("registered", registered),
	)
	return nil
}

// Stop gracefully stops the prober service.
//
// The shared scheduler is stopped by the bootstrap (stopWorkerEngines).
// ProberService must not stop it to avoid premature teardown of the
// shared task scheduling subsystem.
func (s *ProberService) Stop(_ context.Context) error {
	return nil
}

// pointToProbeTask converts a MonitorPoint to a task.Task for registration
// with the scheduling engine.
func pointToProbeTask(point MonitorPoint) task.Task {
	scheduleType, cronExpr, interval := parsePointSchedule(point)
	timeout := time.Duration(point.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	metadata := map[string]string{
		"schedule_type":    string(scheduleType),
		"monitor_point_id": strconv.FormatInt(point.ID, 10),
	}
	switch scheduleType {
	case task.ScheduleTypeInterval:
		metadata["interval"] = interval.String()
	case task.ScheduleTypeCron:
		metadata["cron_expr"] = cronExpr
	}
	return task.Task{
		ID:           proberTaskID(point.ID),
		TenantID:     point.TenantID,
		AssetID:      point.AssetID,
		ExecutorName: point.Type,
		Config:       point.Config,
		Operation:    executor.OpProbe,
		Timeout:      timeout,
		Metadata:     metadata,
		Group:        "prober",
	}
}

// parsePointSchedule derives the schedule type, cron expression, and interval
// from a MonitorPoint's Schedule and Interval fields. A non-empty Schedule is
// tried as a Go duration first, then as a cron expression. An empty Schedule
// falls back to the Interval field (seconds).
func parsePointSchedule(point MonitorPoint) (task.ScheduleType, string, time.Duration) {
	if point.Schedule != "" {
		if d, err := time.ParseDuration(point.Schedule); err == nil && d > 0 {
			return task.ScheduleTypeInterval, "", d
		}
		return task.ScheduleTypeCron, point.Schedule, 0
	}
	interval := time.Duration(point.Interval) * time.Second
	if interval <= 0 {
		interval = 60 * time.Second
	}
	return task.ScheduleTypeInterval, "", interval
}
