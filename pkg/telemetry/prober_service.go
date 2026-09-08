// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package telemetry

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/bytedance/sonic"
	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/executor"
	"github.com/tickraft/tickraft/pkg/task"
)

// ProbeTaskIDOffset separates prober task IDs from regular scheduled task
// IDs in the shared task.TaskEngine. Regular tasks use auto-increment IDs
// from sys_schedule_task (positive, starting at 1). Prober tasks use the
// negated offset plus the monitor_point ID. The sign matters: SQLite
// allocates auto rowids above the largest existing rowid, so a probe task
// persisted with a positive synthetic ID (the legacy scheme) pushed the
// counter into the probe range and every regular task created afterwards
// collided with it. Negative IDs can never lift the counter nor collide
// with a future regular ID.
// ProbeRecordStore inverts the mapping (−task ID − offset = point ID) to
// key probe records by their originating monitor point.
const ProbeTaskIDOffset int64 = 1 << 40

func proberTaskID(pointID int64) int64 {
	return -(ProbeTaskIDOffset + pointID)
}

// legacyProberTaskID returns the probe task ID under the pre-negative
// scheme. Upgraded deployments still carry such rows in sys_schedule_task;
// UnregisterPoint and Start unschedule them so a point is never probed
// twice (legacy row plus the current negative-ID row).
func legacyProberTaskID(pointID int64) int64 {
	return ProbeTaskIDOffset + pointID
}

// ProberService manages active probing by holding a task.TaskEngine
// instance and consuming executor.Prober executors. When a monitoring
// point (Mode=ModeActive) is registered, ProberService schedules it via the
// task.TaskEngine. On fire, the task.TaskEngine publishes an ExecutionTriggered
// event; the executor runner picks it up, runs the prober executor, and
// publishes the result.
//
// The service operates exclusively on MonitorPoint records where
// Mode=ModeActive. Passive points (Mode=ModePassive) are handled by the
// listener pipeline and are never touched by this service.
type ProberService struct {
	sched task.TaskEngine
	// store persists and queries monitoring points backed by the
	// sys_monitor_point table.
	store  *MonitorStore
	logger *zap.Logger
}

// ProberOption configures a ProberService at construction time.
type ProberOption interface {
	apply(*ProberService)
}

// proberMonitorStoreOption injects a MonitorStore for querying and
// persisting active monitoring points.
type proberMonitorStoreOption struct {
	store *MonitorStore
}

func (o proberMonitorStoreOption) apply(s *ProberService) { s.store = o.store }

// WithProberMonitorStore injects a MonitorStore for querying and persisting
// active monitoring points.
func WithProberMonitorStore(store *MonitorStore) ProberOption {
	return proberMonitorStoreOption{store: store}
}

// NewProberService creates a ProberService with the given task manager and
// optional configuration. The variadic options allow callers to inject a
// MonitorStore for point persistence without changing the positional
// signature.
func NewProberService(
	sched task.TaskEngine,
	logger *zap.Logger,
	options ...ProberOption,
) *ProberService {
	s := &ProberService{
		sched:  sched,
		logger: logger,
	}
	for _, o := range options {
		o.apply(s)
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
// via the task.TaskEngine. The point must have Mode=ModeActive and Enabled=true;
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
// engine and the task store. The legacy-scheme row (positive synthetic ID
// persisted by earlier versions) is unscheduled too so upgrades never
// leave a point scheduled twice; Unschedule on an absent ID is a no-op.
func (s *ProberService) UnregisterPoint(ctx context.Context, pointID int64) error {
	if s.sched == nil {
		return nil
	}
	taskID := proberTaskID(pointID)
	if err := s.sched.Unschedule(ctx, taskID); err != nil {
		return fmt.Errorf("unregister prober point %d: %w", pointID, err)
	}
	if err := s.sched.Unschedule(ctx, legacyProberTaskID(pointID)); err != nil {
		return fmt.Errorf("unregister legacy prober point %d: %w", pointID, err)
	}
	s.logger.Info("prober point unregistered", zap.Int64("point_id", pointID))
	return nil
}

// ProbeNow dispatches an on-demand probe for an active monitoring point by
// manually triggering its prober task (TriggerTypeManual). When the task is
// not yet registered with the scheduling engine (e.g. registration raced a
// restart), the point is re-registered from the store before the retry.
// The probe itself runs asynchronously; callers poll the probe record store
// for the outcome.
func (s *ProberService) ProbeNow(ctx context.Context, pointID int64) error {
	if s.sched == nil {
		return fmt.Errorf("telemetry: probe point %d: scheduler is nil", pointID)
	}
	taskID := proberTaskID(pointID)
	err := s.sched.Schedule(ctx, taskID)
	if err == nil {
		s.logger.Info("on-demand probe dispatched", zap.Int64("point_id", pointID))
		return nil
	}
	// The task may be missing from the in-memory store (startup race or a
	// registration failure). Re-register from the persisted point, then
	// retry the manual trigger once.
	if s.store == nil {
		return fmt.Errorf("telemetry: probe point %d: %w", pointID, err)
	}
	point, pErr := s.store.GetByID(ctx, pointID)
	if pErr != nil {
		return fmt.Errorf("telemetry: probe point %d: %w", pointID, err)
	}
	if rErr := s.RegisterPoint(ctx, *point); rErr != nil {
		return fmt.Errorf("telemetry: probe point %d: %w", pointID, err)
	}
	if err := s.sched.Schedule(ctx, taskID); err != nil {
		return fmt.Errorf("telemetry: probe point %d: %w", pointID, err)
	}
	s.logger.Info("on-demand probe dispatched after re-register", zap.Int64("point_id", pointID))
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
	// Sweep legacy-scheme probe rows before registering: Restore already
	// put them on the wheel, and each point registered below with its new
	// negative ID would otherwise leave the legacy row probing in parallel.
	for i := range points {
		if err := s.sched.Unschedule(ctx, legacyProberTaskID(points[i].ID)); err != nil {
			s.logger.Warn("prober start: unschedule legacy probe task",
				zap.Int64("point_id", points[i].ID),
				zap.Error(err),
			)
		}
	}
	registered := 0
	for i := range points {
		p := &points[i]
		if !p.Enabled {
			continue
		}
		if err := s.RegisterPoint(ctx, *p); err != nil {
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
	timeoutSeconds := int64(point.Timeout)
	if timeoutSeconds <= 0 {
		timeoutSeconds = 10
	}
	metadata := map[string]string{
		"monitor_point_id": strconv.FormatInt(point.ID, 10),
	}
	// Execution judgment transmission: the
	// optional "expression" key of the point's config JSON rides the task
	// metadata through the trigger event into the runner, which applies it
	// to the probe result.
	cfgJSON := point.ConfigJSON()
	if exprStr := executor.ConfigExpression(cfgJSON); exprStr != "" {
		metadata["expression"] = exprStr
	}
	var config map[string]any
	if cfgJSON != "" {
		// A malformed config blob registers the task with an empty
		// config; the executor surfaces the misconfiguration on fire.
		if err := sonic.Unmarshal([]byte(cfgJSON), &config); err != nil {
			config = nil
		}
	}
	return task.Task{
		ID:             proberTaskID(point.ID),
		TenantID:       point.TenantID,
		AssetID:        point.AssetID,
		Name:           fmt.Sprintf("prober-%d", point.ID),
		Enabled:        true,
		ExecutorType:   point.Type,
		Schedule:       probeSchedule(point),
		Config:         config,
		Operation:      executor.OpProbe,
		TimeoutSeconds: timeoutSeconds,
		Metadata:       metadata,
		Group:          "prober",
		// Concurrency 1 serializes probes per point: the engine's
		// no-overlap gate keeps a slow probe from stacking on itself when
		// the interval is shorter than the probe duration, which in turn
		// keeps probe records strictly ordered per point.
		Concurrency: 1,
	}
}

// probeSchedule derives the task schedule string from a MonitorPoint's
// Schedule and Interval fields. A non-empty Schedule is used verbatim (a Go
// duration string or a cron expression; the engine classifies it). An empty
// Schedule falls back to the Interval field rendered as a duration,
// defaulting to 60s.
func probeSchedule(point MonitorPoint) string {
	if point.Schedule != "" {
		return point.Schedule
	}
	interval := point.Interval
	if interval <= 0 {
		interval = 60
	}
	return (time.Duration(interval) * time.Second).String()
}
