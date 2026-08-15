// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package scheduler provides the scheduler-backed TaskService implementation
// that bridges the handler layer with the scheduler engine and persistent
// stores. It implements the task.Service interface defined in
// pkg/api/handler/task.
package scheduler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tickraft/tickraft/internal/api/service/taskconv"
	"github.com/tickraft/tickraft/pkg/api/handler"
	"github.com/tickraft/tickraft/pkg/api/handler/task"
	"github.com/tickraft/tickraft/pkg/errdefs"
	"github.com/tickraft/tickraft/pkg/quota"
	"github.com/tickraft/tickraft/pkg/scheduler"
	schedtask "github.com/tickraft/tickraft/pkg/task"
	"go.uber.org/zap"
)

// Compile-time assertion that TaskService implements task.Service.
var _ task.Service = (*TaskService)(nil)

// TaskService implements task.Service by delegating task lifecycle
// operations to the scheduler engine (schedtask.Manager) and reading
// persisted state from the task and execution stores.
//
// ID assignment uses an atomic counter seeded from the maximum existing ID
// in the store on the first CreateTask call, ensuring no collisions after a
// restart.
type TaskService struct {
	engine     schedtask.Manager
	tasks      schedtask.Store
	execs      schedtask.ExecutionStore
	logger     *zap.Logger
	nextID     int64
	idInitOnce sync.Once
	idInitErr  error
}

// NewTaskService creates a scheduler-backed TaskService from the given engine
// and persistent stores. If logger is nil, a no-op logger is used.
func NewTaskService(engine schedtask.Manager, tasks schedtask.Store, execs schedtask.ExecutionStore, logger *zap.Logger) *TaskService {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &TaskService{
		engine: engine,
		tasks:  tasks,
		execs:  execs,
		logger: logger,
	}
}

// ListTasks returns a page of tasks matching the given filter and the total
// count. A zero-value Filter returns all tasks.
func (s *TaskService) ListTasks(ctx context.Context, page, size int, filter task.Filter) ([]task.Task, int64, error) {
	opts := schedtask.ListOptions{Group: filter.Group, Tags: filter.Tags}
	all, err := s.tasks.List(ctx, opts)
	if err != nil {
		return nil, 0, mapError(err)
	}

	sort.Slice(all, func(i, j int) bool { return all[i].ID < all[j].ID })

	total := len(all)
	page, size = clampPaging(page, size)
	start, end := pageWindow(page, size, total)

	result := make([]task.Task, 0, end-start)
	for _, t := range all[start:end] {
		result = append(result, *taskconv.DomainTaskToHandler(t))
	}
	return result, int64(total), nil
}

// GetTask returns a single task by ID.
func (s *TaskService) GetTask(ctx context.Context, id int64) (*task.Task, error) {
	t, err := s.tasks.Get(ctx, id)
	if err != nil {
		return nil, mapError(err)
	}
	return taskconv.DomainTaskToHandler(t), nil
}

// CreateTask creates a new task from the given request.
func (s *TaskService) CreateTask(ctx context.Context, req *task.Task) (*task.Task, error) {
	if req == nil {
		return nil, handler.ErrInvalidRequest
	}
	if req.Executor == "" {
		return nil, handler.NewServiceError(http.StatusBadRequest, errdefs.CodeBadRequest, "executor is required")
	}
	if err := validateScheduleInterval(req.Schedule); err != nil {
		return nil, err
	}

	// Enforce scheduled-task count quota before assigning an ID.
	maxTasks := quota.Ceiling(quota.TypeScheduledTask)
	if maxTasks > 0 {
		existing, err := s.tasks.List(ctx, schedtask.ListOptions{})
		if err != nil {
			return nil, mapError(err)
		}
		if len(existing) >= maxTasks {
			return nil, handler.NewServiceError(
				http.StatusConflict, errdefs.CodeConflict,
				fmt.Sprintf("scheduled task quota exceeded: maximum %d tasks", maxTasks),
			)
		}
	}

	id, err := s.assignID(ctx)
	if err != nil {
		return nil, mapError(err)
	}

	now := time.Now()
	h := *req
	h.ID = id
	h.CreatedAt = now
	h.UpdatedAt = now

	st := taskconv.HandlerToDomainTask(&h)
	if st.Metadata == nil {
		st.Metadata = make(map[string]string)
	}
	scheduleToMetadata(st.Metadata, h.Schedule)

	if err = s.engine.Register(ctx, *st); err != nil {
		return nil, mapError(err)
	}

	s.logger.Info("task created", zap.Int64("id", id), zap.String("executor", h.Executor))
	return &h, nil
}

// UpdateTask updates an existing task identified by ID.
func (s *TaskService) UpdateTask(ctx context.Context, id int64, req *task.Task) (*task.Task, error) {
	if req == nil {
		return nil, handler.ErrInvalidRequest
	}
	if req.Executor == "" {
		return nil, handler.NewServiceError(http.StatusBadRequest, errdefs.CodeBadRequest, "executor is required")
	}
	if err := validateScheduleInterval(req.Schedule); err != nil {
		return nil, err
	}

	existing, err := s.tasks.Get(ctx, id)
	if err != nil {
		return nil, mapError(err)
	}

	h := *req
	h.ID = id
	h.CreatedAt = taskconv.DomainTaskToHandler(existing).CreatedAt
	h.UpdatedAt = time.Now()

	st := taskconv.HandlerToDomainTask(&h)
	if st.Metadata == nil {
		st.Metadata = make(map[string]string)
	}
	scheduleToMetadata(st.Metadata, h.Schedule)

	if err = s.engine.Update(ctx, *st); err != nil {
		return nil, mapError(err)
	}

	s.logger.Info("task updated", zap.Int64("id", id))
	return &h, nil
}

// DeleteTask deletes a task by ID.
func (s *TaskService) DeleteTask(ctx context.Context, id int64) error {
	if err := s.engine.Unschedule(ctx, id); err != nil {
		return mapError(err)
	}
	s.logger.Info("task deleted", zap.Int64("id", id))
	return nil
}

// TriggerTask triggers an immediate execution of a task and records an
// execution entry in the execution store.
func (s *TaskService) TriggerTask(ctx context.Context, id int64) error {
	if _, err := s.tasks.Get(ctx, id); err != nil {
		return mapError(err)
	}

	if err := s.engine.Schedule(ctx, id); err != nil {
		return mapError(err)
	}

	exec := &schedtask.Execution{
		TaskID:      id,
		Status:      "triggered",
		StartedAt:   time.Now(),
		TriggerType: string(schedtask.TriggerTypeManual),
	}
	if err := s.execs.Save(ctx, exec); err != nil {
		s.logger.Warn("failed to save execution entry",
			zap.Int64("task_id", id),
			zap.Error(err),
		)
	}

	s.logger.Info("task triggered", zap.Int64("id", id))
	return nil
}

// PauseTask pauses a task by removing it from the scheduling wheel.
func (s *TaskService) PauseTask(_ context.Context, id int64) error {
	if err := s.engine.Pause(id); err != nil {
		return mapError(err)
	}
	s.logger.Info("task paused", zap.Int64("id", id))
	return nil
}

// ResumeTask resumes a paused task by re-adding it to the scheduling wheel.
func (s *TaskService) ResumeTask(_ context.Context, id int64) error {
	if err := s.engine.Resume(id); err != nil {
		return mapError(err)
	}
	s.logger.Info("task resumed", zap.Int64("id", id))
	return nil
}

// ListExecutions returns a page of executions matching the filter and the
// total count. A taskID <= 0 lists executions across all tasks. Results are
// enriched with the owning task's name, resolved from the task store.
func (s *TaskService) ListExecutions(ctx context.Context, taskID int64, page, size int, filter task.ExecutionFilter) ([]task.Execution, int64, error) {
	var taskIDs []int64
	nameOf := func(id int64) string { return "" }
	if filter.TaskName != "" || taskID <= 0 {
		all, err := s.tasks.List(ctx, schedtask.ListOptions{})
		if err != nil {
			return nil, 0, mapError(err)
		}
		names := make(map[int64]string, len(all))
		needle := strings.ToLower(filter.TaskName)
		taskIDs = make([]int64, 0, len(all))
		for _, t := range all {
			names[t.ID] = t.Metadata["name"]
			if needle != "" && !strings.Contains(strings.ToLower(names[t.ID]), needle) {
				continue
			}
			taskIDs = append(taskIDs, t.ID)
		}
		if needle != "" && len(taskIDs) == 0 {
			return []task.Execution{}, 0, nil
		}
		nameOf = func(id int64) string { return names[id] }
	}

	page, size = clampPaging(page, size)
	q := schedtask.ExecutionQuery{
		TaskID:       taskID,
		TaskIDs:      taskIDs,
		Status:       filter.Status,
		ExecutorType: filter.ExecutorType,
	}
	execs, total, err := s.execs.Query(ctx, q, page, size)
	if err != nil {
		return nil, 0, mapError(err)
	}
	result := make([]task.Execution, 0, len(execs))
	for _, e := range execs {
		h := executionToHandler(e)
		h.TaskName = nameOf(e.TaskID)
		result = append(result, h)
	}
	return result, total, nil
}

// GetExecution returns a single execution record by ID. A positive taskID
// additionally requires the record to belong to that task.
func (s *TaskService) GetExecution(ctx context.Context, taskID, id int64) (*task.Execution, error) {
	e, err := s.execs.Get(ctx, id)
	if err != nil {
		if errors.Is(err, schedtask.ErrExecutionNotFound) {
			return nil, handler.ErrExecutionNotFound
		}
		return nil, mapError(err)
	}
	if taskID > 0 && e.TaskID != taskID {
		return nil, handler.ErrExecutionNotFound
	}
	h := executionToHandler(e)
	if t, err := s.tasks.Get(ctx, e.TaskID); err == nil {
		h.TaskName = t.Metadata["name"]
	}
	return &h, nil
}

// CopyTask creates a new task by cloning the configuration of an existing
// task. The new task is assigned a fresh ID and the given name; an empty name
// defaults to "<source name> (copy)".
func (s *TaskService) CopyTask(ctx context.Context, id int64, newName string) (*task.Task, error) {
	existing, err := s.tasks.Get(ctx, id)
	if err != nil {
		return nil, mapError(err)
	}

	source := taskconv.DomainTaskToHandler(existing)

	name := newName
	if name == "" {
		name = source.Name + " (copy)"
	}

	clone := &task.Task{
		Name:        name,
		Description: source.Description,
		Executor:    source.Executor,
		Schedule:    source.Schedule,
		Enabled:     source.Enabled,
		Config:      source.Config,
		Group:       source.Group,
		Tags:        source.Tags,
		RetryPolicy: source.RetryPolicy,
		Concurrency: source.Concurrency,
	}

	created, err := s.CreateTask(ctx, clone)
	if err != nil {
		return nil, err
	}

	s.logger.Info("task copied", zap.Int64("source_id", id), zap.Int64("new_id", created.ID))
	return created, nil
}

// GetExecutionStats returns aggregated execution statistics for the given
// time range (inclusive on both ends).
func (s *TaskService) GetExecutionStats(ctx context.Context, from, to time.Time) (task.ExecutionStats, error) {
	raw, err := s.execs.Stats(ctx, from, to)
	if err != nil {
		return task.ExecutionStats{}, mapError(err)
	}
	return task.ExecutionStats{
		TotalExecutions:   raw.TotalExecutions,
		SuccessCount:      raw.SuccessCount,
		FailureCount:      raw.FailureCount,
		SuccessRate:       raw.SuccessRate,
		AverageDurationMs: raw.AverageDurationMs,
	}, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// assignID initializes the ID counter from the store on first use and returns
// the next monotonically increasing ID.
func (s *TaskService) assignID(ctx context.Context) (int64, error) {
	s.idInitOnce.Do(func() {
		existing, err := s.tasks.List(ctx, schedtask.ListOptions{})
		if err != nil {
			s.idInitErr = fmt.Errorf("seed task id from store: %w", err)
			return
		}
		var maxID int64
		for _, t := range existing {
			if t.ID > maxID {
				maxID = t.ID
			}
		}
		atomic.StoreInt64(&s.nextID, maxID)
	})
	if s.idInitErr != nil {
		return 0, s.idInitErr
	}
	return atomic.AddInt64(&s.nextID, 1), nil
}

// scheduleToMetadata populates the engine-expected schedule keys
// (schedule_type, cron_expr, interval) on the given metadata map based on
// the user-facing schedule string. A nil metadata map is a no-op.
func scheduleToMetadata(metadata map[string]string, schedule string) {
	if metadata == nil {
		return
	}
	switch {
	case schedule == "":
		metadata["schedule_type"] = string(schedtask.ScheduleTypeEvent)
	case isIntervalSchedule(schedule):
		metadata["schedule_type"] = string(schedtask.ScheduleTypeInterval)
		metadata["interval"] = schedule
	default:
		metadata["schedule_type"] = string(schedtask.ScheduleTypeCron)
		metadata["cron_expr"] = schedule
	}
}

// isIntervalSchedule reports whether the schedule string is a valid Go
// duration (e.g. "30s", "5m", "1h30m").
func isIntervalSchedule(schedule string) bool {
	_, err := time.ParseDuration(schedule)
	return err == nil
}

// validateScheduleInterval checks that an interval-based schedule string
// respects the quota-imposed minimum scheduling interval. Non-interval
// schedules (cron, event) are always accepted. Returns a handler-level
// ServiceError (HTTP 400) when the interval is too small.
func validateScheduleInterval(schedule string) error {
	if schedule == "" || !isIntervalSchedule(schedule) {
		return nil
	}
	interval, err := time.ParseDuration(schedule)
	if err != nil {
		return nil // malformed durations are handled later by parseSchedule
	}
	minSecs := quota.Ceiling(quota.TypeScheduledTaskInterval)
	if minSecs <= 0 {
		return nil
	}
	minInterval := time.Duration(minSecs) * time.Second
	if interval < minInterval {
		return handler.NewServiceError(
			http.StatusBadRequest,
			errdefs.CodeBadRequest,
			fmt.Sprintf("schedule interval %s is smaller than the minimum allowed %s", interval, minInterval),
		)
	}
	return nil
}

// executionToHandler converts a scheduler domain Execution into a handler
// Execution DTO, normalizing the persisted status ("normal"/"abnormal"/
// "triggered") into the API lifecycle status (success/failed/running). A nil
// input returns the zero value.
func executionToHandler(e *schedtask.Execution) task.Execution {
	if e == nil {
		return task.Execution{}
	}
	h := task.Execution{
		ID:           e.ID,
		TaskID:       e.TaskID,
		ExecutorType: e.ExecutorName,
		Status:       lifecycleStatus(e.Status),
		Output:       e.Output,
		Error:        e.Error,
		StatusCode:   e.StatusCode,
		Duration:     e.Duration,
		RetryCount:   e.RetryCount,
		StartedAt:    e.StartedAt,
	}
	if !e.FinishedAt.IsZero() {
		fa := e.FinishedAt
		h.FinishedAt = &fa
	}
	return h
}

// lifecycleStatus maps a persisted execution status to the API contract
// lifecycle status (pending/running/success/failed).
func lifecycleStatus(stored string) string {
	switch stored {
	case "normal":
		return "success"
	case "abnormal", "unknown":
		return "failed"
	case "triggered":
		return "running"
	default:
		return stored
	}
}

// mapError translates scheduler and store errors into handler-level service
// errors carrying the appropriate HTTP status and business code.
func mapError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, errdefs.ErrNotFound) || errors.Is(err, schedtask.ErrTaskNotFound) {
		return handler.ErrTaskNotFound
	}
	if errors.Is(err, schedtask.ErrIntervalTooSmall) {
		return handler.NewServiceError(http.StatusBadRequest, errdefs.CodeBadRequest, err.Error())
	}
	if errors.Is(err, scheduler.ErrSchedulerStopped) {
		return handler.NewServiceError(http.StatusServiceUnavailable, errdefs.CodeInternal, "scheduler unavailable")
	}
	if errors.Is(err, schedtask.ErrTaskAlreadyPaused) || errors.Is(err, schedtask.ErrTaskNotPaused) {
		return handler.NewServiceError(http.StatusConflict, errdefs.CodeConflict, err.Error())
	}
	return handler.NewServiceError(http.StatusInternalServerError, errdefs.CodeInternal, err.Error())
}

// clampPaging normalizes page and size parameters to sane defaults.
func clampPaging(page, size int) (int, int) {
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}
	if size > 100 {
		size = 100
	}
	return page, size
}

// pageWindow returns the [start, end) slice indices for the given page and
// size within a collection of total length.
func pageWindow(page, size, total int) (int, int) {
	start := min(max((page-1)*size, 0), total)
	end := min(start+size, total)
	return start, end
}
