// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package task

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/tickraft/tickraft/pkg/db"
	"github.com/tickraft/tickraft/pkg/executor"
	"github.com/tickraft/tickraft/pkg/pagination"
)

// store is the GORM-backed implementation of Store. Task is itself the GORM
// model for sys_schedule_task, so persistence is a direct row mapping with
// no conversion layer.
type store struct {
	dbc *gorm.DB
}

// NewStore creates a new Store backed by the given *gorm.DB.
func NewStore(dbc *gorm.DB) *store { //nolint:revive // returning the unexported concrete type is intentional; consumers use the exported interface
	return &store{dbc: dbc}
}

// Migrate creates or updates the sys_schedule_task table schema.
func (s *store) Migrate(ctx context.Context) error {
	if err := s.dbc.WithContext(ctx).AutoMigrate(&Task{}); err != nil {
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
	// Use OnConflict upsert so that both first-time inserts (Register) and
	// subsequent updates (Update) are handled by a single query. Hard-delete
	// in Delete ensures no soft-deleted rows collide with the conflict target.
	//
	// The explicit Select is required because GORM omits zero-valued fields
	// that carry a column default (enabled default:true, concurrency
	// default:1) from the INSERT column list; on conflict the excluded row
	// would then carry the column default instead of the field value, so a
	// disabled task (Enabled=false) would silently persist as enabled.
	if err := s.dbc.WithContext(ctx).
		Select(taskWriteColumns).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "id"}},
			DoUpdates: clause.AssignmentColumns(taskWriteColumns),
		}).
		Create(t).Error; err != nil {
		return fmt.Errorf("task: save: %w", db.MapError(err))
	}
	return nil
}

// Get retrieves a task by its ID. Returns errdefs.ErrNotFound if no task
// with the given ID exists.
func (s *store) Get(ctx context.Context, id int64) (*Task, error) {
	var t Task
	if err := s.dbc.WithContext(ctx).First(&t, id).Error; err != nil {
		return nil, fmt.Errorf("task: get: %w", db.MapError(err))
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
	var tasks []*Task
	query := s.dbc.WithContext(ctx)
	if opts.Group != "" {
		query = query.Where("`group` = ?", opts.Group)
	}
	if opts.IDs != nil {
		query = query.Where("id IN ?", opts.IDs)
	}
	if opts.NameLike != "" {
		needle := likeEscape(strings.ToLower(opts.NameLike))
		query = query.Where("LOWER(name) LIKE ? ESCAPE '\\'", "%"+needle+"%")
	}
	if err := query.Find(&tasks).Error; err != nil {
		return nil, fmt.Errorf("task: list: %w", db.MapError(err))
	}
	filtered := make([]*Task, 0, len(tasks))
	for _, t := range tasks {
		if !matchAnyTag(t.Tags, opts.Tags) {
			continue
		}
		filtered = append(filtered, t)
	}
	return filtered, nil
}

// likeEscape escapes LIKE wildcards so the pattern matches the caller's
// substring literally; combined with ESCAPE '\' it reproduces Go-side
// strings.Contains semantics in SQL.
func likeEscape(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// Count returns the number of persisted (non-deleted) tasks.
func (s *store) Count(ctx context.Context) (int64, error) {
	var count int64
	if err := s.dbc.WithContext(ctx).Model(&Task{}).Count(&count).Error; err != nil {
		return 0, fmt.Errorf("task: count: %w", db.MapError(err))
	}
	return count, nil
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
	if err := s.dbc.WithContext(ctx).Unscoped().Delete(&Task{}, id).Error; err != nil {
		return fmt.Errorf("task: delete: %w", db.MapError(err))
	}
	return nil
}

// Compile-time assertion that store implements Store.
var _ Store = (*store)(nil)

// taskWriteColumns lists every column Save writes, both in the INSERT
// column list and in the ON CONFLICT update set. created_at and deleted_at
// are excluded so that the original creation timestamp and soft-delete
// state are preserved across updates.
var taskWriteColumns = []string{
	"id",
	"tenant_id",
	"asset_id",
	"name",
	"description",
	"executor_type",
	"schedule",
	"executor_config",
	"timeout",
	"priority",
	"depends_on",
	"max_retries",
	"retry_interval",
	"enabled",
	"report_status",
	"metadata",
	"group",
	"tags",
	"run_id",
	"retry_policy",
	"concurrency",
	"updated_at",
}

// executionStore is the GORM-backed implementation of ExecutionStore. It
// persists task execution history to the sys_schedule_execution table.
type executionStore struct {
	dbc *gorm.DB
}

// NewExecutionStore creates a new ExecutionStore backed by the given *gorm.DB.
func NewExecutionStore(dbc *gorm.DB) ExecutionStore {
	return &executionStore{dbc: dbc}
}

// Migrate creates or updates the sys_schedule_execution table schema.
func (s *executionStore) Migrate(ctx context.Context) error {
	if err := s.dbc.WithContext(ctx).AutoMigrate(&Execution{}); err != nil {
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
	if err := s.dbc.WithContext(ctx).Create(exec).Error; err != nil {
		return fmt.Errorf("task: save execution: %w", db.MapError(err))
	}
	return nil
}

// Query returns a page of executions matching the filter, ordered by most
// recent first (descending ID), along with the total count of matching rows.
// page starts at 1; size is normalized by pagination.Clamp.
func (s *executionStore) Query(ctx context.Context, q ExecutionQuery, page, size int) ([]*Execution, int64, error) {
	page, size = pagination.Clamp(page, size)

	query := s.dbc.WithContext(ctx).Model(&Execution{})
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
	if q.TriggerType != "" {
		query = query.Where("trigger_type = ?", q.TriggerType)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("task: query executions: %w", db.MapError(err))
	}

	var execs []*Execution
	if err := query.
		Order("id DESC").
		Offset((page - 1) * size).
		Limit(size).
		Find(&execs).Error; err != nil {
		return nil, 0, fmt.Errorf("task: query executions: %w", db.MapError(err))
	}
	return execs, total, nil
}

// Get retrieves a single execution record by its ID. It returns
// ErrExecutionNotFound when no record with the given ID exists.
func (s *executionStore) Get(ctx context.Context, id int64) (*Execution, error) {
	var exec Execution
	if err := s.dbc.WithContext(ctx).First(&exec, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrExecutionNotFound
		}
		return nil, fmt.Errorf("task: get execution: %w", db.MapError(err))
	}
	return &exec, nil
}

// MarkTimeout transitions a still-running execution to the timeout state.
// The status guard makes the transition atomic: a row that already reached
// a terminal state (remote report landed, or another instance swept it) is
// left untouched and reported as not transitioned.
func (s *executionStore) MarkTimeout(
	ctx context.Context, id int64, finishedAt time.Time, durationMs int64,
) (bool, error) {
	res := s.dbc.WithContext(ctx).
		Model(&Execution{}).
		Where("id = ? AND status = ?", id, StatusRunning).
		Updates(map[string]any{
			"status":      StatusTimeout,
			"finished_at": finishedAt,
			"duration":    durationMs,
		})
	if res.Error != nil {
		return false, fmt.Errorf("task: mark execution timeout: %w", db.MapError(res.Error))
	}
	return res.RowsAffected > 0, nil
}

// DeleteExecutionsOlderThan removes all execution records whose created_at
// timestamp is strictly before the given time. This supports retention-based
// cleanup of stale execution history. The deletion is unconditional (not
// scoped to a specific task or tenant) because the caller is expected to be a
// system-level maintenance routine.
func (s *executionStore) DeleteExecutionsOlderThan(ctx context.Context, before time.Time) error {
	if err := s.dbc.WithContext(ctx).Where("created_at < ?", before).Delete(&Execution{}).Error; err != nil {
		return fmt.Errorf("task: delete old executions: %w", db.MapError(err))
	}
	return nil
}

// Stats returns aggregated execution statistics for the given time range.
// A positive taskID scopes the aggregation to that task's executions; zero
// aggregates across all tasks. The query scans the sys_schedule_execution table
// filtering by created_at between [from, to] (inclusive) and computes:
//   - TotalExecutions: total row count in the range
//   - SuccessCount: rows whose status is StatusSuccess
//   - FailureCount: rows whose status is StatusFailed
//   - AverageDurationMs: average of the duration column (milliseconds)
//
// SuccessRate is computed as SuccessCount/TotalExecutions*100, with a
// zero-total range yielding 0. COALESCE is used so that an empty range
// produces zero-valued aggregates rather than NULLs.
func (s *executionStore) Stats(ctx context.Context, from, to time.Time, taskID int64) (ExecutionStatsResult, error) {
	// statsRow is a local struct used to scan the aggregated query result.
	// GORM maps the snake_case column aliases to the exported fields by name.
	var row struct {
		Total       int64
		Success     int64
		Failure     int64
		AvgDuration float64
	}
	query := s.dbc.WithContext(ctx).
		Model(&Execution{}).
		Select(`
			COUNT(*) AS total,
			COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0) AS success,
			COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0) AS failure,
			COALESCE(AVG(duration), 0) AS avg_duration
		`, StatusSuccess, StatusFailed).
		Where("created_at BETWEEN ? AND ?", from, to)
	if taskID > 0 {
		query = query.Where("task_id = ?", taskID)
	}
	err := query.Scan(&row).Error
	if err != nil {
		return ExecutionStatsResult{}, fmt.Errorf("task: compute execution stats: %w", db.MapError(err))
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

// StatsByDay returns per-day aggregates for the given time range, grouped
// by the server-local calendar date of created_at and ordered by date. Days without
// executions are omitted; callers zero-fill gaps when a contiguous series
// is required.
//
// SQLite normalizes timestamp strings with a timezone offset to UTC before
// DATE() evaluates, so the 'localtime' modifier is required to group by
// the server-local day and stay consistent with the local-midnight window
// the service layer computes.
func (s *executionStore) StatsByDay(ctx context.Context, from, to time.Time, taskID int64) ([]DailyStat, error) {
	var rows []struct {
		Date    string
		Total   int64
		Success int64
		Failed  int64
	}
	query := s.dbc.WithContext(ctx).
		Model(&Execution{}).
		Select(`
			DATE(created_at, 'localtime') AS date,
			COUNT(*) AS total,
			COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0) AS success,
			COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0) AS failed
		`, StatusSuccess, StatusFailed).
		Where("created_at BETWEEN ? AND ?", from, to).
		Order("date")
	if taskID > 0 {
		query = query.Where("task_id = ?", taskID)
	}
	if err := query.Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("task: compute daily execution stats: %w", db.MapError(err))
	}
	stats := make([]DailyStat, 0, len(rows))
	for _, row := range rows {
		stats = append(stats, DailyStat{
			Date:    row.Date,
			Total:   row.Total,
			Success: row.Success,
			Failed:  row.Failed,
		})
	}
	return stats, nil
}

// Compile-time assertion that executionStore implements ExecutionStore.
var _ ExecutionStore = (*executionStore)(nil)

// Migrate creates or updates the sys_schedule_task and sys_schedule_execution
// table schemas. It is intended to be called once during application startup.
func Migrate(ctx context.Context, dbc *gorm.DB) error {
	if err := dbc.WithContext(ctx).AutoMigrate(
		&Task{},
		&Execution{},
	); err != nil {
		return fmt.Errorf("task: migrate tables: %w", err)
	}
	return nil
}

// ExecutionRecordStore adapts a task.ExecutionStore to the
// executor.RecordStore interface.
//
// The executor Runner persists execution results through
// executor.RecordStore.Save(ctx, record); the scheduler's persistent
// ExecutionStore exposes Save(ctx, *Execution) instead. This adapter bridges
// the two so that real execution results (Status, Output, Error, Duration,
// StatusCode, FinishedAt) flow into the same sys_schedule_execution table that
// ListExecutions reads from. The asset-status vocabulary carried on
// executor records is translated to the persisted execution vocabulary
// here — this is the single bridge between the two.
//
// The adapter is safe for concurrent use because the wrapped
// ExecutionStore is.
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
// the task Execution type and persists it via the wrapped ExecutionStore.
// Errors are returned to the caller, which is the executor Runner; the Runner
// logs them but does not fail the task.
func (s *ExecutionRecordStore) Save(ctx context.Context, record executor.ExecutionRecord) error {
	if s == nil || s.store == nil {
		return nil
	}
	// Timeout takes precedence over the bridged asset status: a deadline
	// expiry is not a plain failure and is persisted as the distinct
	// "timeout" state so UIs can surface it separately.
	status := ExecutionStatusFromAsset(record.Status)
	if record.TimedOut {
		status = StatusTimeout
	}
	exec := &Execution{
		TaskID:       record.TaskID,
		TenantID:     record.TenantID,
		AssetID:      record.AssetID,
		ExecutorType: record.ExecutorName,
		Status:       status,
		StatusCode:   record.StatusCode,
		Output:       record.Output,
		Error:        record.ErrorMsg,
		Duration:     int64(record.Duration / 1_000_000),
		RetryCount:   record.RetryCount,
		StartedAt:    record.StartedAt,
		RunID:        record.RunID,
		TriggerType:  record.TriggerType,
		Node:         record.Node,
		ExitCode:     record.ExitCode,
	}
	if !record.TriggeredAt.IsZero() {
		triggeredAt := record.TriggeredAt
		exec.TriggeredAt = &triggeredAt
	}
	if !record.FinishedAt.IsZero() {
		finishedAt := record.FinishedAt
		exec.FinishedAt = &finishedAt
	}
	if err := s.store.Save(ctx, exec); err != nil {
		return fmt.Errorf("task: persist execution record: %w", err)
	}
	return nil
}

// Compile-time assertion that ExecutionRecordStore implements executor.RecordStore.
var _ executor.RecordStore = (*ExecutionRecordStore)(nil)
