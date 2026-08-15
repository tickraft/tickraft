// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package task

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/tickraft/tickraft/pkg/db/errmap"
	"github.com/tickraft/tickraft/pkg/executor"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// store is the GORM-backed implementation of Store. It persists task
// configurations to the sys_schedule_task table using ScheduleTask,
// converting between the domain Task type and the persistence model on each
// operation.
type store struct {
	dbc *gorm.DB
}

// NewStore creates a new Store backed by the given *gorm.DB.
func NewStore(dbc *gorm.DB) *store {
	return &store{dbc: dbc}
}

// defaultExecutionListLimit caps execution history queries when the caller
// does not specify a limit.
const defaultExecutionListLimit = 200

// Migrate creates or updates the sys_schedule_task table schema.
func (s *store) Migrate(ctx context.Context) error {
	if err := s.dbc.WithContext(ctx).AutoMigrate(&ScheduleTask{}); err != nil {
		return fmt.Errorf("task: migrate task table: %w", err)
	}
	return nil
}

// Save creates or updates a task in the database. It performs an upsert:
// if a task with the same ID exists, all configurable columns are updated;
// otherwise a new row is inserted.
func (s *store) Save(ctx context.Context, t *Task) error {
	if t == nil {
		return fmt.Errorf("task: save: nil task")
	}
	m, err := taskToModel(t)
	if err != nil {
		return fmt.Errorf("task: save: %w", err)
	}
	// Use OnConflict upsert so that both first-time inserts (Register) and
	// subsequent updates (Update) are handled by a single query. Hard-delete
	// in Delete ensures no soft-deleted rows collide with the conflict target.
	if err := s.dbc.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns(taskUpsertColumns),
	}).Create(m).Error; err != nil {
		return fmt.Errorf("task: save: %w", errmap.MapError(err))
	}
	return nil
}

// Get retrieves a task by its ID. Returns errdefs.ErrNotFound if no task
// with the given ID exists.
func (s *store) Get(ctx context.Context, id int64) (*Task, error) {
	var m ScheduleTask
	if err := s.dbc.WithContext(ctx).First(&m, id).Error; err != nil {
		return nil, fmt.Errorf("task: get: %w", errmap.MapError(err))
	}
	t, err := m.ToTask()
	if err != nil {
		return nil, fmt.Errorf("task: get: %w", err)
	}
	return &t, nil
}

// List returns tasks matching the given options. A zero-value ListOptions
// returns all tasks. The Group filter is applied as a SQL WHERE clause for
// efficiency; the Tags filter is applied in Go after fetching because tags
// are stored as a comma-separated string and a SQL LIKE-based approach risks
// false-positive substring matches. Given the runtime's modest
// task volume, in-memory tag filtering is acceptable.
func (s *store) List(ctx context.Context, opts ListOptions) ([]*Task, error) {
	var models []ScheduleTask
	query := s.dbc.WithContext(ctx)
	if opts.Group != "" {
		query = query.Where("`group` = ?", opts.Group)
	}
	if err := query.Find(&models).Error; err != nil {
		return nil, fmt.Errorf("task: list: %w", errmap.MapError(err))
	}
	tasks := make([]*Task, 0, len(models))
	for i := range models {
		t, err := models[i].ToTask()
		if err != nil {
			return nil, fmt.Errorf("task: list: parse task %d: %w", models[i].ID, err)
		}
		if !matchAnyTag(t.Tags, opts.Tags) {
			continue
		}
		tasks = append(tasks, &t)
	}
	return tasks, nil
}

// matchAnyTag reports whether the task's tags contain at least one of the
// requested tags. If requested is empty or nil, all tasks match.
func matchAnyTag(taskTags, requested []string) bool {
	if len(requested) == 0 {
		return true
	}
	tagSet := make(map[string]struct{}, len(taskTags))
	for _, t := range taskTags {
		tagSet[t] = struct{}{}
	}
	for _, r := range requested {
		if _, ok := tagSet[r]; ok {
			return true
		}
	}
	return false
}

// Delete permanently removes a task by its ID. Unscoped is used so that
// the row is hard-deleted (not soft-deleted), allowing the same task ID to
// be re-registered later without colliding with a soft-deleted record.
func (s *store) Delete(ctx context.Context, id int64) error {
	if err := s.dbc.WithContext(ctx).Unscoped().Delete(&ScheduleTask{}, id).Error; err != nil {
		return fmt.Errorf("task: delete: %w", errmap.MapError(err))
	}
	return nil
}

// Compile-time assertion that store implements Store.
var _ Store = (*store)(nil)

// taskUpsertColumns lists the columns updated on conflict during Save.
// created_at and deleted_at are excluded so that the original creation
// timestamp and soft-delete state are preserved across updates.
var taskUpsertColumns = []string{
	"tenant_id",
	"asset_id",
	"name",
	"executor_type",
	"executor_config",
	"schedule_type",
	"cron_expr",
	"interval",
	"timeout",
	"priority",
	"depends_on",
	"max_retries",
	"retry_interval",
	"enabled",
	"metadata",
	"group",
	"tags",
	"run_id",
	"retry_policy",
	"concurrency",
	"updated_at",
}

// taskToModel converts a scheduler.Task domain object to a ScheduleTask for
// persistence. Schedule-related columns (schedule_type, cron_expr, interval)
// are populated from the task's Metadata map for query convenience; the
// Metadata JSON column remains the source of truth consumed by ToTask.
func taskToModel(t *Task) (*ScheduleTask, error) {
	var metadataJSON string
	if t.Metadata != nil {
		b, err := json.Marshal(t.Metadata)
		if err != nil {
			return nil, fmt.Errorf("marshal metadata: %w", err)
		}
		metadataJSON = string(b)
	}

	m := &ScheduleTask{
		ID:             t.ID,
		TenantID:       t.TenantID,
		AssetID:        t.AssetID,
		ExecutorType:   t.ExecutorName,
		ExecutorConfig: t.Config,
		Timeout:        int64(t.Timeout.Seconds()),
		Priority:       t.Priority,
		DependsOn:      t.DependsOn,
		Enabled:        true,
		Metadata:       metadataJSON,
		Group:          t.Group,
		Tags:           strings.Join(t.Tags, ","),
		RunID:          t.RunID,
		RetryPolicy:    t.RetryPolicy,
		Concurrency:    t.Concurrency,
	}

	// Populate dedicated schedule columns from Metadata for queryability.
	if t.Metadata != nil {
		// Name is persisted in Metadata by the handler adapter; copy it to the
		// dedicated column so it can be queried directly.
		if v, ok := t.Metadata["name"]; ok {
			m.Name = v
		}
		if v, ok := t.Metadata["schedule_type"]; ok {
			m.ScheduleType = v
		}
		if v, ok := t.Metadata["cron_expr"]; ok {
			m.CronExpr = v
		}
		if v, ok := t.Metadata["interval"]; ok {
			if d, err := time.ParseDuration(v); err == nil {
				m.Interval = int64(d.Seconds())
			}
		}
		// Honor the enabled flag persisted in Metadata by the handler
		// adapter and the engine's Pause/Resume. Default to true when the
		// key is absent so existing tasks remain enabled.
		if v, ok := t.Metadata["enabled"]; ok {
			if b, err := strconv.ParseBool(v); err == nil {
				m.Enabled = b
			}
		}
	}

	return m, nil
}

// executionStore is the GORM-backed implementation of ExecutionStore. It
// persists task execution history to the sys_schedule_log table using
// ScheduleLog, converting between the domain Execution type and the
// persistence model on each operation.
type executionStore struct {
	dbc *gorm.DB
}

// NewExecutionStore creates a new ExecutionStore backed by the given *gorm.DB.
func NewExecutionStore(dbc *gorm.DB) ExecutionStore {
	return &executionStore{dbc: dbc}
}

// Migrate creates or updates the sys_schedule_log table schema.
func (s *executionStore) Migrate(ctx context.Context) error {
	if err := s.dbc.WithContext(ctx).AutoMigrate(&ScheduleLog{}); err != nil {
		return fmt.Errorf("task: migrate execution table: %w", err)
	}
	return nil
}

// Save records a single execution in the database. The execution is always
// inserted as a new row; if exec.ID is zero the database assigns an
// auto-increment value, otherwise the provided ID is used.
func (s *executionStore) Save(ctx context.Context, exec *Execution) error {
	if exec == nil {
		return fmt.Errorf("task: save execution: nil execution")
	}
	m := ExecutionToModel(exec)
	if err := s.dbc.WithContext(ctx).Create(m).Error; err != nil {
		return fmt.Errorf("task: save execution: %w", errmap.MapError(err))
	}
	// Reflect the assigned ID back to the caller.
	exec.ID = m.ID
	return nil
}

// List returns execution history for the given task ID, ordered by most
// recent first (descending ID). If limit is positive, at most limit records
// are returned; otherwise at most defaultExecutionListLimit records are
// returned. Execution history grows without bound, so callers can never
// fetch the full table by passing a zero limit.
func (s *executionStore) List(ctx context.Context, taskID int64, limit int) ([]*Execution, error) {
	if limit <= 0 {
		limit = defaultExecutionListLimit
	}
	var models []ScheduleLog
	query := s.dbc.WithContext(ctx).
		Where("task_id = ?", taskID).
		Order("id DESC").
		Limit(limit)
	if err := query.Find(&models).Error; err != nil {
		return nil, fmt.Errorf("task: list executions: %w", errmap.MapError(err))
	}
	result := make([]*Execution, 0, len(models))
	for i := range models {
		exec := models[i].ToExecution()
		result = append(result, &exec)
	}
	return result, nil
}

// Query returns a page of executions matching the filter, ordered by most
// recent first (descending ID), along with the total count of matching rows.
// page starts at 1; size is normalized via ClampPaging semantics (defaults
// and the max-page-size cap are applied by the caller).
func (s *executionStore) Query(ctx context.Context, q ExecutionQuery, page, size int) ([]*Execution, int64, error) {
	if page < 1 {
		page = 1
	}
	if size <= 0 {
		size = defaultExecutionListLimit
	}

	query := s.dbc.WithContext(ctx).Model(&ScheduleLog{})
	if q.TaskID > 0 {
		query = query.Where("task_id = ?", q.TaskID)
	}
	if q.TaskIDs != nil {
		if len(q.TaskIDs) == 0 {
			return []*Execution{}, 0, nil
		}
		query = query.Where("task_id IN ?", q.TaskIDs)
	}
	if q.Status != "" {
		query = query.Where("status = ?", q.Status)
	}
	if q.ExecutorType != "" {
		query = query.Where("executor_type = ?", q.ExecutorType)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("task: query executions: %w", errmap.MapError(err))
	}

	var models []ScheduleLog
	if err := query.
		Order("id DESC").
		Offset((page - 1) * size).
		Limit(size).
		Find(&models).Error; err != nil {
		return nil, 0, fmt.Errorf("task: query executions: %w", errmap.MapError(err))
	}
	result := make([]*Execution, 0, len(models))
	for i := range models {
		exec := models[i].ToExecution()
		result = append(result, &exec)
	}
	return result, total, nil
}

// Get retrieves a single execution record by its ID. It returns
// ErrExecutionNotFound when no record with the given ID exists.
func (s *executionStore) Get(ctx context.Context, id int64) (*Execution, error) {
	var m ScheduleLog
	if err := s.dbc.WithContext(ctx).First(&m, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrExecutionNotFound
		}
		return nil, fmt.Errorf("task: get execution: %w", errmap.MapError(err))
	}
	exec := m.ToExecution()
	return &exec, nil
}

// DeleteExecutionsOlderThan removes all execution records whose created_at
// timestamp is strictly before the given time. This supports retention-based
// cleanup of stale execution history. The deletion is unconditional (not
// scoped to a specific task or tenant) because the caller is expected to be a
// system-level maintenance routine.
func (s *executionStore) DeleteExecutionsOlderThan(ctx context.Context, before time.Time) error {
	if err := s.dbc.WithContext(ctx).Where("created_at < ?", before).Delete(&ScheduleLog{}).Error; err != nil {
		return fmt.Errorf("task: delete old executions: %w", errmap.MapError(err))
	}
	return nil
}

// Stats returns aggregated execution statistics for the given time range.
// The query scans the sys_schedule_log table filtering by created_at between
// [from, to] (inclusive) and computes:
//   - TotalExecutions: total row count in the range
//   - SuccessCount: rows whose status is "normal" (types.AssetStatusNormal)
//   - FailureCount: rows whose status is "abnormal" (types.AssetStatusAbnormal)
//   - AverageDurationMs: average of the duration column (milliseconds)
//
// SuccessRate is computed as SuccessCount/TotalExecutions*100, with a
// zero-total range yielding 0. COALESCE is used so that an empty range
// produces zero-valued aggregates rather than NULLs.
func (s *executionStore) Stats(ctx context.Context, from, to time.Time) (ExecutionStatsResult, error) {
	// statsRow is a local struct used to scan the aggregated query result.
	// GORM maps the snake_case column aliases to the exported fields by name.
	var row struct {
		Total       int64
		Success     int64
		Failure     int64
		AvgDuration float64
	}
	err := s.dbc.WithContext(ctx).
		Model(&ScheduleLog{}).
		Select(`
			COUNT(*) AS total,
			COALESCE(SUM(CASE WHEN status = 'normal' THEN 1 ELSE 0 END), 0) AS success,
			COALESCE(SUM(CASE WHEN status = 'abnormal' THEN 1 ELSE 0 END), 0) AS failure,
			COALESCE(AVG(duration), 0) AS avg_duration
		`).
		Where("created_at BETWEEN ? AND ?", from, to).
		Scan(&row).Error
	if err != nil {
		return ExecutionStatsResult{}, fmt.Errorf("task: compute execution stats: %w", errmap.MapError(err))
	}

	result := ExecutionStatsResult{
		TotalExecutions:   row.Total,
		SuccessCount:      row.Success,
		FailureCount:      row.Failure,
		AverageDurationMs: row.AvgDuration,
	}
	if row.Total > 0 {
		result.SuccessRate = float64(row.Success) / float64(row.Total) * 100
	}
	return result, nil
}

// Compile-time assertion that executionStore implements ExecutionStore.
var _ ExecutionStore = (*executionStore)(nil)

// Migrate creates or updates the sys_schedule_task and sys_schedule_log
// table schemas. It is intended to be called once during application startup.
func Migrate(ctx context.Context, dbc *gorm.DB) error {
	if err := dbc.WithContext(ctx).AutoMigrate(
		&ScheduleTask{},
		&ScheduleLog{},
	); err != nil {
		return fmt.Errorf("task: migrate tables: %w", err)
	}
	return nil
}

// ExecutionRecordStore adapts a task.ExecutionStore to the
// executor.RecordStore interface.
//
// The executor Runner persists execution results through
// executor.RecordStore.Save(record ExecutionRecord), which has no context
// parameter. The scheduler's persistent ExecutionStore exposes
// Save(ctx, *Execution) instead. This adapter bridges the two so that real
// execution results (Status, Output, Error, Duration, StatusCode,
// FinishedAt) flow into the same sys_schedule_log table that
// ListExecutions reads from.
//
// A background context is used for the underlying Save call because the
// executor.RecordStore.Save signature does not accept one. The adapter is
// safe for concurrent use because the wrapped ExecutionStore is.
type ExecutionRecordStore struct {
	store ExecutionStore
}

// NewExecutionRecordStore wraps the given task.ExecutionStore so it can
// be passed to executor.WithRecordStore. When store is nil the returned
// adapter is a no-op (Save returns nil), which lets callers pass the result
// to executor.WithRecordStore unconditionally without risking a nil-interface
// panic in the Runner.
func NewExecutionRecordStore(store ExecutionStore) *ExecutionRecordStore {
	return &ExecutionRecordStore{store: store}
}

// Save implements executor.RecordStore. It converts the executor record into
// the domain Execution type and persists it via the wrapped
// task.ExecutionStore. Errors are returned to the caller, which is the
// executor Runner; the Runner logs them but does not fail the task.
func (s *ExecutionRecordStore) Save(record executor.ExecutionRecord) error {
	if s == nil || s.store == nil {
		return nil
	}
	exec := &Execution{
		TaskID:       record.TaskID,
		TenantID:     record.TenantID,
		AssetID:      record.AssetID,
		ExecutorName: record.ExecutorName,
		Status:       string(record.Status),
		StatusCode:   record.StatusCode,
		Output:       record.Output,
		Error:        record.ErrorMsg,
		Duration:     int64(record.Duration / 1_000_000),
		RetryCount:   record.RetryCount,
		StartedAt:    record.StartedAt,
		FinishedAt:   record.FinishedAt,
		RunID:        record.RunID,
		TriggerType:  record.TriggerType,
	}
	if err := s.store.Save(context.Background(), exec); err != nil {
		return fmt.Errorf("task: persist execution record: %w", err)
	}
	return nil
}

// Compile-time assertion that ExecutionRecordStore implements executor.RecordStore.
var _ executor.RecordStore = (*ExecutionRecordStore)(nil)
