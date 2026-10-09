// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package task

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/asset"
	"github.com/tickraft/tickraft/pkg/errdefs"
	"github.com/tickraft/tickraft/pkg/executor"
	"github.com/tickraft/tickraft/pkg/pagination"
	"github.com/tickraft/tickraft/pkg/quota"
	"github.com/tickraft/tickraft/pkg/scheduler"
)

// Compile-time assertion that TaskService implements Service.
var _ Service = (*TaskService)(nil)

// defaultTaskTimeoutSeconds is applied on create when the request does not
// specify a timeout. The column default cannot cover this case because the
// service always inserts an explicit value.
const defaultTaskTimeoutSeconds = 30

// TaskService implements Service by delegating task lifecycle
// operations to the scheduler engine (TaskEngine) and reading
// persisted state from the task and execution stores.
//
// ID assignment uses an atomic counter seeded from the maximum existing ID
// in the store on the first CreateTask call, ensuring no collisions after a
// restart.
//
// The <Domain>Service name mirrors the convention of the other domain
// service implementations (see pkg/system).
//
//nolint:revive // intentional stutter: mirrors the <Domain>Service convention
type TaskService struct {
	engine     TaskEngine
	tasks      Store
	execs      ExecutionStore
	registry   *executor.Registry
	assets     asset.Getter
	logger     *zap.Logger
	nextID     atomic.Int64
	idInitOnce sync.Once
	idInitErr  error
}

// ServiceOption configures a TaskService at construction time. The distinct
// name avoids colliding with the Engine's Option.
type ServiceOption interface {
	apply(*TaskService)
}

// assetGetterOption injects the asset read surface used by the create-time
// asset-binding validation.
type assetGetterOption struct {
	g asset.Getter
}

func (o assetGetterOption) apply(s *TaskService) { s.assets = o.g }

// WithAssetGetter injects the asset read surface used to validate the
// create-time asset binding (spec asset.md §17, ALIGN-V3-011): a non-zero
// asset_id must resolve to an existing asset row. A nil getter (the
// default) skips the check, which keeps isolated tests unwired — the same
// convention as the nil executor registry.
func WithAssetGetter(g asset.Getter) ServiceOption { return assetGetterOption{g: g} }

// NewTaskService creates a scheduler-backed TaskService from the given engine
// and persistent stores. A non-nil registry enables executor_type capability
// prevalidation on create/update (the type must support OpExecute); a nil
// registry skips the check, which keeps isolated tests unwired. If logger is
// nil, a no-op logger is used. Options may additionally inject the asset read
// surface for create-time asset-binding validation (see WithAssetGetter).
//
//nolint:revive // the five positional parameters are the historical signature kept for API compatibility; the trailing variadic options are optional injections, not positional arguments
func NewTaskService(
	engine TaskEngine,
	tasks Store,
	execs ExecutionStore,
	registry *executor.Registry,
	logger *zap.Logger,
	options ...ServiceOption,
) *TaskService {
	if logger == nil {
		logger = zap.NewNop()
	}
	s := &TaskService{
		engine:   engine,
		tasks:    tasks,
		execs:    execs,
		registry: registry,
		logger:   logger,
	}
	for _, o := range options {
		o.apply(s)
	}
	return s
}

// validateExecutorType rejects executor types that cannot run as tasks.
// API-created tasks always execute as OpExecute (the Operation field is
// runtime-only and reserved for the prober path), so the type must carry a
// write capability. Catching this at create time turns "task registered but
// every run fails capability lookup" into an immediate 400.
func (s *TaskService) validateExecutorType(executorType string) error {
	if s.registry == nil {
		return nil
	}
	if _, err := s.registry.LookupWithOp(executorType, executor.OpExecute); err != nil {
		return errdefs.NewServiceError(
			http.StatusBadRequest, errdefs.CodeBadRequest,
			fmt.Sprintf("executor_type %q cannot run as a task: %v", executorType, err),
		)
	}
	return nil
}

// ListTasks returns a page of tasks matching the given filter and the total
// count. A zero-value Filter returns all tasks.
func (s *TaskService) ListTasks(ctx context.Context, page, size int,
	filter Filter) ([]*Task, int64, error) {
	opts := ListOptions{Group: filter.Group, Tags: filter.Tags, AssetID: filter.AssetID, ExcludeSynthetic: true}
	all, err := s.tasks.List(ctx, opts)
	if err != nil {
		return nil, 0, mapError(err)
	}

	sort.Slice(all, func(i, j int) bool { return all[i].ID < all[j].ID })

	total := len(all)
	page, size = pagination.Clamp(page, size)
	start, end := pagination.Window(page, size, total)

	result := make([]*Task, end-start)
	copy(result, all[start:end])
	return result, int64(total), nil
}

// GetTask returns a single task by ID.
func (s *TaskService) GetTask(ctx context.Context, id int64) (*Task, error) {
	t, err := s.tasks.Get(ctx, id)
	if err != nil {
		return nil, mapError(err)
	}
	return t, nil
}

// validateCreateRequest runs the request-shape validations that precede
// quota enforcement and ID assignment on the create path.
func (s *TaskService) validateCreateRequest(ctx context.Context, req *Task) error {
	if req == nil {
		return errdefs.ErrInvalidRequest
	}
	if req.ExecutorType == "" {
		return errdefs.NewServiceError(http.StatusBadRequest, errdefs.CodeBadRequest, "executor_type is required")
	}
	if err := s.validateExecutorType(req.ExecutorType); err != nil {
		return err
	}
	if err := validateSchedule(req.Schedule); err != nil {
		return err
	}
	if err := ValidateCatchupConfig(req.CatchupPolicy, req.SleepWindows); err != nil {
		return err
	}
	if err := s.validateDependency(ctx, req.DependsOn); err != nil {
		return err
	}
	return s.validateAsset(ctx, req.AssetID)
}

// CreateTask creates a new task from the given request.
func (s *TaskService) CreateTask(ctx context.Context, req *Task) (*Task, error) {
	if err := s.validateCreateRequest(ctx, req); err != nil {
		return nil, err
	}

	// Enforce scheduled-task count quota before assigning an ID.
	maxTasks := quota.Ceiling(quota.TypeScheduledTask)
	if maxTasks > 0 {
		count, err := s.tasks.Count(ctx)
		if err != nil {
			return nil, mapError(err)
		}
		if count >= int64(maxTasks) {
			return nil, errdefs.NewServiceError(
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
	t := *req
	t.ID = id
	t.CreatedAt = now
	t.UpdatedAt = now
	if t.TimeoutSeconds <= 0 {
		t.TimeoutSeconds = defaultTaskTimeoutSeconds
	}
	if t.CatchupPolicy == "" {
		t.CatchupPolicy = CatchupPolicySkip
	}
	// A brand-new task has never dispatched; the watermark starts NULL
	// so the first start never replays history.
	t.LastScheduledAt = nil

	if s.engine != nil {
		if err = s.engine.Register(ctx, t); err != nil {
			return nil, mapError(err)
		}
	} else if err = s.tasks.Save(ctx, &t); err != nil {
		// Engine-less deployments (distributed Server role) persist via the
		// store directly; Worker nodes observe the row through the syncer.
		return nil, mapError(err)
	}

	s.logger.Info("task created", zap.Int64("id", id), zap.String("executor_type", t.ExecutorType))
	return &t, nil
}

// validateDependency rejects a depends_on reference that is negative,
// points at a missing task, or closes a cycle in the dependency chain.
// Dependencies are single-predecessor chains, so the walk terminates at
// the chain head (DependsOn == 0); without it a cyclic reference would
// leave the task silently never executable (CanExecute stays false
// forever). Freshly assigned task IDs cannot self-reference at create
// time, so the cycle walk is defense against rows that bypassed
// validation (imported data, direct DB writes).
func (s *TaskService) validateDependency(ctx context.Context, dependsOn int64) error {
	if dependsOn == 0 {
		return nil
	}
	if dependsOn < 0 {
		return errdefs.NewServiceError(http.StatusBadRequest, errdefs.CodeBadRequest,
			"depends_on must be a positive task id")
	}
	visited := make(map[int64]struct{})
	cur := dependsOn
	for cur != 0 {
		if _, seen := visited[cur]; seen {
			return errdefs.NewServiceError(http.StatusBadRequest, errdefs.CodeBadRequest,
				"depends_on chain forms a cycle")
		}
		visited[cur] = struct{}{}
		upstream, err := s.tasks.Get(ctx, cur)
		if err != nil {
			if errors.Is(err, errdefs.ErrNotFound) {
				return errdefs.NewServiceError(http.StatusBadRequest, errdefs.CodeBadRequest,
					"depends_on references an unknown task")
			}
			return mapError(err)
		}
		cur = upstream.DependsOn
	}
	return nil
}

// validateAsset rejects an asset binding that references a missing asset row
// (spec asset.md §17, ALIGN-V3-011). asset_id 0 means "unbound", which stays
// valid: the create API treats the asset link as optional (Task.AssetID is
// not wire-bindable; the open-source edition has no user-facing asset
// binding), so only a non-zero reference is checked for existence. A nil
// asset getter (unwired deployment or isolated test) skips the check, the
// same convention as the nil executor registry.
func (s *TaskService) validateAsset(ctx context.Context, assetID int64) error {
	if assetID == 0 || s.assets == nil {
		return nil
	}
	if _, err := s.assets.GetByID(ctx, assetID); err != nil {
		if errors.Is(err, errdefs.ErrNotFound) {
			return errdefs.NewServiceError(http.StatusBadRequest, errdefs.CodeBadRequest,
				"asset_id references an unknown asset")
		}
		return mapError(err)
	}
	return nil
}

// UpdateTask updates an existing task identified by ID. Fields that the API
// cannot express (tenant/asset binding, priority, dependencies, metadata
// extension keys) are preserved from the existing row rather than zeroed by
// the PUT. The catch-up policy and sleep windows follow an omit-means-
// preserve rule: an absent catchup_policy ("" body value) or absent
// sleep_windows (nil) keeps the stored setting, so a form that does not
// render the fields (the open-source edition's task form) cannot silently
// reset them; explicit values ("skip" / []) do reset.
func (s *TaskService) UpdateTask(ctx context.Context, id int64, req *Task) (*Task, error) {
	if req == nil {
		return nil, errdefs.ErrInvalidRequest
	}
	if req.ExecutorType == "" {
		return nil, errdefs.NewServiceError(http.StatusBadRequest, errdefs.CodeBadRequest, "executor_type is required")
	}
	if err := s.validateExecutorType(req.ExecutorType); err != nil {
		return nil, err
	}
	if err := validateSchedule(req.Schedule); err != nil {
		return nil, err
	}
	if err := ValidateCatchupConfig(req.CatchupPolicy, req.SleepWindows); err != nil {
		return nil, err
	}

	existing, err := s.tasks.Get(ctx, id)
	if err != nil {
		return nil, mapError(err)
	}

	t := *req
	t.ID = id
	t.TenantID = existing.TenantID
	t.AssetID = existing.AssetID
	t.Priority = existing.Priority
	t.DependsOn = existing.DependsOn
	t.Metadata = existing.Metadata
	if t.TimeoutSeconds <= 0 {
		t.TimeoutSeconds = existing.TimeoutSeconds
	}
	if t.CatchupPolicy == "" {
		t.CatchupPolicy = existing.CatchupPolicy
	}
	if t.CatchupPolicy == "" {
		t.CatchupPolicy = CatchupPolicySkip
	}
	if t.SleepWindows == nil {
		t.SleepWindows = existing.SleepWindows
	}
	// The watermark is engine-internal and never on the wire; preserve it
	// so an update cannot make the task look never-dispatched (which
	// would disable catch-up for it).
	t.LastScheduledAt = existing.LastScheduledAt
	t.CreatedAt = existing.CreatedAt
	t.UpdatedAt = time.Now()

	if s.engine != nil {
		if err = s.engine.Update(ctx, t); err != nil {
			return nil, mapError(err)
		}
	} else if err = s.tasks.Save(ctx, &t); err != nil {
		return nil, mapError(err)
	}

	s.logger.Info("task updated", zap.Int64("id", id))
	return &t, nil
}

// DeleteTask deletes a task by ID.
func (s *TaskService) DeleteTask(ctx context.Context, id int64) error {
	if s.engine != nil {
		if err := s.engine.Unschedule(ctx, id); err != nil {
			return mapError(err)
		}
	} else if err := s.tasks.Delete(ctx, id); err != nil {
		return mapError(err)
	}
	s.logger.Info("task deleted", zap.Int64("id", id))
	return nil
}

// TriggerTask triggers an immediate execution of a task. The real
// execution record is written by the executor runner on completion with
// trigger_type="manual"; no placeholder row is inserted here — a
// running-state placeholder would never be updated because the runner
// always inserts its own record, leaving a permanent ghost row.
func (s *TaskService) TriggerTask(ctx context.Context, id int64) error {
	_, err := s.tasks.Get(ctx, id)
	if err != nil {
		return mapError(err)
	}

	if s.engine != nil {
		if err := s.engine.Schedule(ctx, id); err != nil {
			return mapError(err)
		}
	} else {
		// Engine-less deployments accept the request and log it; dispatch
		// is resolved by the distributed layer observing the mutation.
		s.logger.Info("task trigger requested", zap.Int64("id", id))
		return nil
	}

	s.logger.Info("task triggered", zap.Int64("id", id))
	return nil
}

// PauseTask pauses a task by removing it from the scheduling wheel.
func (s *TaskService) PauseTask(ctx context.Context, id int64) error {
	if s.engine != nil {
		if err := s.engine.Pause(id); err != nil {
			return mapError(err)
		}
	} else {
		return s.setEnabled(ctx, id, false)
	}
	s.logger.Info("task paused", zap.Int64("id", id))
	return nil
}

// ResumeTask resumes a paused task by re-adding it to the scheduling wheel.
func (s *TaskService) ResumeTask(ctx context.Context, id int64) error {
	if s.engine != nil {
		if err := s.engine.Resume(id); err != nil {
			return mapError(err)
		}
	} else {
		return s.setEnabled(ctx, id, true)
	}
	s.logger.Info("task resumed", zap.Int64("id", id))
	return nil
}

// setEnabled flips the enabled column for engine-less deployments; Worker
// nodes observe the change via the Syncer and stop or resume dispatching.
func (s *TaskService) setEnabled(ctx context.Context, id int64, enabled bool) error {
	t, err := s.tasks.Get(ctx, id)
	if err != nil {
		return mapError(err)
	}
	t.Enabled = enabled
	if err = s.tasks.Save(ctx, t); err != nil {
		return mapError(err)
	}
	if enabled {
		s.logger.Info("task resumed", zap.Int64("id", id))
	} else {
		s.logger.Info("task paused", zap.Int64("id", id))
	}
	return nil
}

// ListExecutions returns a page of executions matching the filter and the
// total count. A taskID <= 0 lists executions across all tasks. Results are
// enriched with the owning task's name, resolved from the task store for the
// page's distinct task IDs only.
func (s *TaskService) ListExecutions(
	ctx context.Context,
	taskID int64,
	page, size int,
	filter ExecutionFilter,
) ([]*Execution, int64, error) {
	var taskIDs []int64
	if filter.TaskName != "" {
		// Resolve the name filter to matching task IDs in SQL rather than
		// loading every task row into memory.
		matches, err := s.tasks.List(ctx, ListOptions{NameLike: filter.TaskName, ExcludeSynthetic: true})
		if err != nil {
			return nil, 0, mapError(err)
		}
		if len(matches) == 0 {
			return []*Execution{}, 0, nil
		}
		taskIDs = make([]int64, len(matches))
		for i, t := range matches {
			taskIDs[i] = t.ID
		}
	}

	page, size = pagination.Clamp(page, size)
	q := ExecutionQuery{
		TaskID:       taskID,
		TaskIDs:      taskIDs,
		Status:       filter.Status,
		ExecutorType: filter.ExecutorType,
		TriggerType:  filter.TriggerType,
	}
	execs, total, err := s.execs.Query(ctx, q, page, size)
	if err != nil {
		return nil, 0, mapError(err)
	}
	enrichTaskNames(ctx, s.tasks, execs)
	return execs, total, nil
}

// enrichTaskNames fills each execution's TaskName with the owning task's
// name, resolved by a single store query for the page's distinct task IDs.
// Enrichment is best-effort: on lookup error (or for tasks deleted since
// their execution was recorded) the name stays empty.
func enrichTaskNames(ctx context.Context, tasks Store, execs []*Execution) {
	ids := make(map[int64]struct{}, len(execs))
	for _, e := range execs {
		ids[e.TaskID] = struct{}{}
	}
	if len(ids) == 0 {
		return
	}
	idList := make([]int64, 0, len(ids))
	for id := range ids {
		idList = append(idList, id)
	}
	owners, err := tasks.List(ctx, ListOptions{IDs: idList})
	if err != nil {
		return
	}
	names := make(map[int64]string, len(owners))
	for _, t := range owners {
		names[t.ID] = t.Name
	}
	for _, e := range execs {
		e.TaskName = names[e.TaskID]
	}
}

// GetExecution returns a single execution record by ID. A positive taskID
// additionally requires the record to belong to that task.
func (s *TaskService) GetExecution(ctx context.Context, taskID, id int64) (*Execution, error) {
	e, err := s.execs.Get(ctx, id)
	if err != nil {
		if errors.Is(err, ErrExecutionNotFound) {
			return nil, errdefs.ErrExecutionNotFound
		}
		return nil, mapError(err)
	}
	if taskID > 0 && e.TaskID != taskID {
		return nil, errdefs.ErrExecutionNotFound
	}
	if t, err := s.tasks.Get(ctx, e.TaskID); err == nil {
		e.TaskName = t.Name
	}
	return e, nil
}

// CopyTask creates a new task by cloning the configuration of an existing
// task. The new task is assigned a fresh ID and the given name; an empty name
// defaults to "<source name> (copy)".
func (s *TaskService) CopyTask(ctx context.Context, id int64, newName string) (*Task, error) {
	source, err := s.tasks.Get(ctx, id)
	if err != nil {
		return nil, mapError(err)
	}

	name := newName
	if name == "" {
		name = source.Name + " (copy)"
	}

	clone := &Task{
		Name:                 name,
		Description:          source.Description,
		ExecutorType:         source.ExecutorType,
		Schedule:             source.Schedule,
		Enabled:              source.Enabled,
		Config:               source.Config,
		TimeoutSeconds:       source.TimeoutSeconds,
		MaxRetries:           source.MaxRetries,
		RetryIntervalSeconds: source.RetryIntervalSeconds,
		TenantID:             source.TenantID,
		AssetID:              source.AssetID,
		Priority:             source.Priority,
		DependsOn:            source.DependsOn,
		Metadata:             source.Metadata,
		Group:                source.Group,
		Tags:                 source.Tags,
		RetryPolicy:          source.RetryPolicy,
		Concurrency:          source.Concurrency,
		CatchupPolicy:        source.CatchupPolicy,
		SleepWindows:         source.SleepWindows,
		// LastScheduledAt intentionally not cloned: the copy is a new
		// task that has never dispatched.
	}

	created, err := s.CreateTask(ctx, clone)
	if err != nil {
		return nil, err
	}

	s.logger.Info("task copied", zap.Int64("source_id", id), zap.Int64("new_id", created.ID))
	return created, nil
}

// GetExecutionStats returns aggregated execution statistics for the given
// time range (inclusive on both ends). A positive taskID scopes the
// aggregation to that task's executions; zero aggregates across all tasks.
// A positive days replaces the range with the last N days (today included,
// day boundaries at server-local midnight) and additionally returns a
// zero-filled per-day series so trend charts render contiguous dates.
func (s *TaskService) GetExecutionStats(
	ctx context.Context, from, to time.Time, taskID int64, days int,
) (ExecutionStats, error) {
	if days > 0 {
		now := time.Now()
		from = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).
			AddDate(0, 0, -(days - 1))
		to = now
	}
	raw, err := s.execs.Stats(ctx, from, to, taskID)
	if err != nil {
		return ExecutionStats{}, mapError(err)
	}
	stats := ExecutionStats{
		TotalExecutions:   raw.TotalExecutions,
		SuccessCount:      raw.SuccessCount,
		FailureCount:      raw.FailureCount,
		SuccessRate:       raw.SuccessRate,
		AverageDurationMs: raw.AverageDurationMs,
	}
	if days > 0 {
		sparse, err := s.execs.StatsByDay(ctx, from, to, taskID)
		if err != nil {
			return ExecutionStats{}, mapError(err)
		}
		stats.Daily = fillDailySeries(sparse, from, to)
	}
	return stats, nil
}

// fillDailySeries zero-fills the sparse per-day aggregates into a
// contiguous date series covering [from, to]; from must be a local
// midnight so day stepping stays on date boundaries.
func fillDailySeries(sparse []DailyStat, from, to time.Time) []DailyStat {
	byDate := make(map[string]DailyStat, len(sparse))
	for _, day := range sparse {
		byDate[day.Date] = day
	}
	series := make([]DailyStat, 0, len(sparse))
	for day := from; !day.After(to); day = day.AddDate(0, 0, 1) {
		key := day.Format("2006-01-02")
		if entry, ok := byDate[key]; ok {
			series = append(series, entry)
		} else {
			series = append(series, DailyStat{Date: key})
		}
	}
	return series
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// assignID initializes the ID counter from the store on first use and returns
// the next monotonically increasing ID.
func (s *TaskService) assignID(ctx context.Context) (int64, error) {
	s.idInitOnce.Do(func() {
		existing, err := s.tasks.List(ctx, ListOptions{})
		if err != nil {
			s.idInitErr = fmt.Errorf("seed task id from store: %w", err)
			return
		}
		var maxID int64
		for _, t := range existing {
			maxID = max(maxID, t.ID)
		}
		s.nextID.Store(maxID)
	})
	if s.idInitErr != nil {
		return 0, s.idInitErr
	}
	return s.nextID.Add(1), nil
}

// validateSchedule validates the schedule string (cron expression or Go
// duration; "" is event-driven) and checks interval schedules against the
// quota-imposed minimum. Non-interval schedules are accepted once the
// expression parses. Returns a handler-level ServiceError (HTTP 400) when
// the schedule is invalid.
func validateSchedule(schedule string) error {
	scheduleType, interval, err := ClassifySchedule(schedule)
	if err != nil {
		return errdefs.NewServiceError(http.StatusBadRequest, errdefs.CodeBadRequest, err.Error())
	}
	if scheduleType != ScheduleTypeInterval {
		return nil
	}
	minSecs := quota.Ceiling(quota.TypeScheduledTaskInterval)
	if minSecs <= 0 {
		return nil
	}
	minInterval := time.Duration(minSecs) * time.Second
	if interval < minInterval {
		return errdefs.NewServiceError(
			http.StatusBadRequest,
			errdefs.CodeBadRequest,
			fmt.Sprintf("schedule interval %s is smaller than the minimum allowed %s", interval, minInterval),
		)
	}
	return nil
}

// mapError translates scheduler and store errors into handler-level service
// errors carrying the appropriate HTTP status and business code.
func mapError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, errdefs.ErrNotFound) || errors.Is(err, ErrTaskNotFound) {
		return errdefs.ErrTaskNotFound
	}
	if errors.Is(err, ErrIntervalTooSmall) {
		return errdefs.NewServiceError(http.StatusBadRequest, errdefs.CodeBadRequest, err.Error())
	}
	if errors.Is(err, scheduler.ErrSchedulerStopped) {
		return errdefs.NewServiceError(http.StatusServiceUnavailable, errdefs.CodeInternal, "scheduler unavailable")
	}
	if errors.Is(err, ErrTaskAlreadyPaused) || errors.Is(err, ErrTaskNotPaused) {
		return errdefs.NewServiceError(http.StatusConflict, errdefs.CodeConflict, err.Error())
	}
	return errdefs.NewServiceError(http.StatusInternalServerError, errdefs.CodeInternal, err.Error())
}
