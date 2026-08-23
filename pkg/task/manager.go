// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package task

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/cron"
	"github.com/tickraft/tickraft/pkg/event"
	"github.com/tickraft/tickraft/pkg/quota"
	"github.com/tickraft/tickraft/pkg/scheduler"
)

// Manager is the task lifecycle management interface. It owns task
// registration, scheduling, pause/resume, dependency tracking, and
// event-driven triggers. A Manager holds a scheduler.Engine instance
// and registers timed callbacks via Engine.Add/Remove.
type Manager interface {
	// Register registers a new task for scheduling.
	Register(ctx context.Context, task Task) error
	// Schedule manually triggers a task for execution by publishing an
	// ExecutionTriggered event on the event bus.
	Schedule(ctx context.Context, taskID int64) error
	// Update updates an existing task's schedule.
	Update(ctx context.Context, task Task) error
	// Unschedule removes a task from scheduling.
	Unschedule(ctx context.Context, taskID int64) error
	// Pause removes the task from the scheduling wheel but keeps it in
	// the store. The task can be resumed via Resume.
	Pause(taskID int64) error
	// Resume re-adds a paused task to the scheduling wheel.
	Resume(taskID int64) error
	// Stop gracefully stops the manager and the underlying engine.
	Stop(ctx context.Context) error
	// SubscribeEvents subscribes to status change events (for event-driven
	// tasks) and task completion events (for dependency tracking) on the
	// event bus.
	SubscribeEvents(ctx context.Context)
	// Restore loads persisted tasks from the configured Store into
	// memory and schedules them. If no store is configured, Restore is a
	// no-op.
	Restore(ctx context.Context) error
}

// Compile-time assertion that Service implements Manager.
var _ Manager = (*Service)(nil)

// Option configures a Service.
type Option interface {
	apply(*Options)
}

// Options holds the resolved Service configuration.
type Options struct {
	// Engine is the pure timing engine. Required.
	Engine scheduler.Engine
	// Bus is the event bus for publishing task trigger events and
	// subscribing to task completion events.
	// If nil, a new bus is created internally.
	Bus event.Bus
	// Logger is the structured logger.
	Logger *zap.Logger
	// Store persists task configurations across restarts. If nil,
	// persistence is disabled and the manager operates in-memory only.
	Store Store
	// ShardManager controls which tasks this manager instance owns in a
	// sharded deployment. If nil, all tasks are owned (no sharding).
	ShardManager *scheduler.ShardManager
}

// engineOption sets the timing engine that the Service uses for scheduling
// callbacks.
type engineOption struct {
	e scheduler.Engine
}

func (o engineOption) apply(opts *Options) { opts.Engine = o.e }

// WithEngine sets the timing engine that the Service uses for
// scheduling callbacks. This option is required.
func WithEngine(e scheduler.Engine) Option { return engineOption{e: e} }

// eventBusOption sets the event bus for publishing task trigger events and
// subscribing to task completion events.
type eventBusOption struct {
	b event.Bus
}

func (o eventBusOption) apply(opts *Options) { opts.Bus = o.b }

// WithEventBus sets the event bus for publishing task trigger events and
// subscribing to task completion events. If not set, a new bus is created
// internally and closed on Stop.
func WithEventBus(b event.Bus) Option { return eventBusOption{b: b} }

// loggerOption sets the structured logger.
type loggerOption struct {
	l *zap.Logger
}

func (o loggerOption) apply(opts *Options) { opts.Logger = o.l }

// WithLogger sets the structured logger. Defaults to a no-op logger.
func WithLogger(l *zap.Logger) Option { return loggerOption{l: l} }

// storeOption configures the Store used to persist task configurations
// across restarts.
type storeOption struct {
	s Store
}

func (o storeOption) apply(opts *Options) { opts.Store = o.s }

// WithStore configures the Store used to persist task configurations
// across restarts. Passing nil disables persistence.
func WithStore(s Store) Option { return storeOption{s: s} }

// shardManagerOption sets the shard manager for distributed task ownership
// filtering.
type shardManagerOption struct {
	sm *scheduler.ShardManager
}

func (o shardManagerOption) apply(opts *Options) { opts.ShardManager = o.sm }

// WithShardManager sets the shard manager for distributed task ownership
// filtering. If not set, all tasks are owned (no sharding).
func WithShardManager(sm *scheduler.ShardManager) Option {
	return shardManagerOption{sm: sm}
}

// NewService creates a new Service with the given options.
//
// If no Engine is provided via WithEngine, a default scheduler.Engine is
// created internally and started automatically. If an Engine is provided,
// the caller is responsible for starting it (calling Engine.Start) before
// the Service schedules any tasks.
//
// All options are optional except that at most one Engine may be provided.
// Returns an error if the internal engine cannot be initialized.
func NewService(options ...Option) (*Service, error) {
	opts := &Options{
		Logger: zap.NewNop(),
	}
	for _, o := range options {
		o.apply(opts)
	}

	// Create a default engine if none was provided.
	ownEngine := false
	if opts.Engine == nil {
		eng, err := scheduler.NewEngine(scheduler.WithEngineLogger(opts.Logger))
		if err != nil {
			return nil, fmt.Errorf("task: create engine: %w", err)
		}
		if err := eng.Start(context.Background()); err != nil {
			return nil, fmt.Errorf("task: start engine: %w", err)
		}
		opts.Engine = eng
		ownEngine = true
	}

	bus := opts.Bus
	ownBus := false
	if bus == nil {
		bus = event.NewBus()
		ownBus = true
	}

	m := &Service{
		engine:           opts.Engine,
		ownEngine:        ownEngine,
		store:            opts.Store,
		deps:             newDependencyChecker(),
		shardManager:     opts.ShardManager,
		bus:              bus,
		ownBus:           ownBus,
		logger:           opts.Logger,
		tasks:            make(map[int64]Task),
		scheds:           make(map[int64]cron.Schedule),
		scheduleTypes:    make(map[int64]ScheduleType),
		eventDrivenTasks: make(map[int64]struct{}),
		running:          make(map[int64]struct{}),
	}
	return m, nil
}

// Service implements Manager by delegating timing to scheduler.Engine
// and owning all task business semantics: lifecycle, dependency tracking,
// per-task concurrency control, and event-driven triggers.
type Service struct {
	engine       scheduler.Engine
	ownEngine    bool
	store        Store
	deps         *dependencyChecker
	shardManager *scheduler.ShardManager
	bus          event.Bus
	ownBus       bool
	logger       *zap.Logger

	// taskMu protects the tasks map.
	taskMu sync.RWMutex
	tasks  map[int64]Task

	// mu protects the scheds, scheduleTypes, and eventDrivenTasks maps.
	mu               sync.RWMutex
	scheds           map[int64]cron.Schedule
	scheduleTypes    map[int64]ScheduleType
	eventDrivenTasks map[int64]struct{}

	// runningMu protects the running map which tracks taskIDs with an
	// in-flight execution for per-task concurrency control.
	runningMu sync.Mutex
	running   map[int64]struct{}
}

// Register registers a new task for scheduling.
// It parses the task's schedule string, stores the task, and registers a
// timed callback with the engine. Disabled tasks are stored but not put on
// the wheel; they start running when resumed (Resume or an update with
// Enabled=true).
func (m *Service) Register(ctx context.Context, task Task) error {
	scheduleType, interval, err := ClassifySchedule(task.Schedule)
	if err != nil {
		return fmt.Errorf("register task %d: %w", task.ID, err)
	}
	if err := checkMinInterval(scheduleType, interval); err != nil {
		return fmt.Errorf("register task %d: %w", task.ID, err)
	}
	sched, err := parseSchedule(task.Schedule)
	if err != nil {
		return fmt.Errorf("register task %d: %w", task.ID, err)
	}

	m.setTask(task)

	m.mu.Lock()
	if task.Enabled {
		m.scheds[task.ID] = sched
		m.scheduleTypes[task.ID] = scheduleType
		if scheduleType == ScheduleTypeEvent {
			m.eventDrivenTasks[task.ID] = struct{}{}
		} else {
			delete(m.eventDrivenTasks, task.ID)
		}
	}
	m.mu.Unlock()

	// Register the timed callback with the engine. Event-driven tasks
	// use a neverSchedule so the engine never fires them; they are
	// triggered by external events via SubscribeEvents.
	if task.Enabled && scheduleType != ScheduleTypeEvent {
		if err := m.engine.Add(task.ID, sched, m.onFire); err != nil {
			return fmt.Errorf("register task %d: %w", task.ID, err)
		}
	}

	m.logger.Info("task registered",
		zap.Int64("task_id", task.ID),
		zap.String("schedule_type", string(scheduleType)),
		zap.Bool("enabled", task.Enabled),
	)
	return nil
}

// Schedule manually triggers a task for immediate execution by publishing
// a TaskTriggered event on the event bus.
func (m *Service) Schedule(_ context.Context, taskID int64) error {
	task, err := m.getTask(taskID)
	if err != nil {
		return fmt.Errorf("schedule task %d: %w", taskID, err)
	}
	m.trigger(task, TriggerTypeManual)
	return nil
}

// Update updates an existing task's schedule by unscheduling and re-registering.
func (m *Service) Update(ctx context.Context, task Task) error {
	m.unscheduleInternal(task.ID)
	return m.Register(ctx, task)
}

// Unschedule removes a task from scheduling.
func (m *Service) Unschedule(_ context.Context, taskID int64) error {
	m.unscheduleInternal(taskID)
	m.deleteTask(taskID)

	m.mu.Lock()
	delete(m.scheds, taskID)
	delete(m.scheduleTypes, taskID)
	delete(m.eventDrivenTasks, taskID)
	m.mu.Unlock()

	m.logger.Info("task unscheduled",
		zap.Int64("task_id", taskID),
	)
	return nil
}

// Pause removes the task from the scheduling wheel but keeps it in the
// in-memory task store and the persistent store. The task's Enabled flag is
// set to false and persisted. A paused task can be resumed via Resume.
func (m *Service) Pause(taskID int64) error {
	task, err := m.getTask(taskID)
	if err != nil {
		return fmt.Errorf("pause task %d: %w", taskID, err)
	}

	// Check if the task is currently on the wheel by checking if it has
	// a schedule entry. Event-driven tasks are never on the wheel.
	m.mu.RLock()
	_, hasSched := m.scheds[taskID]
	schedType := m.scheduleTypes[taskID]
	m.mu.RUnlock()

	onWheel := hasSched && schedType != ScheduleTypeEvent
	if !onWheel {
		return ErrTaskAlreadyPaused
	}

	m.unscheduleInternal(taskID)

	// Drop the in-memory schedule registration. unscheduleInternal only
	// removes the task from the engine wheel; without this delete the
	// scheds entry survives and Resume's onWheel guard rejects every
	// resume with ErrTaskNotPaused.
	m.mu.Lock()
	delete(m.scheds, taskID)
	delete(m.scheduleTypes, taskID)
	m.mu.Unlock()

	task.Enabled = false
	m.setTask(task)

	m.logger.Info("task paused",
		zap.Int64("task_id", taskID),
	)
	return nil
}

// Resume re-adds a paused task to the scheduling wheel and sets Enabled=true.
// The next fire time is recomputed from the task's schedule.
func (m *Service) Resume(taskID int64) error {
	task, err := m.getTask(taskID)
	if err != nil {
		return fmt.Errorf("resume task %d: %w", taskID, err)
	}

	m.mu.RLock()
	sched := m.scheds[taskID]
	schedType := m.scheduleTypes[taskID]
	m.mu.RUnlock()

	onWheel := sched != nil && schedType != ScheduleTypeEvent
	if onWheel {
		return ErrTaskNotPaused
	}

	scheduleType, _, cerr := ClassifySchedule(task.Schedule)
	if cerr != nil {
		return fmt.Errorf("resume task %d: %w", taskID, cerr)
	}
	if scheduleType != ScheduleTypeEvent {
		if sched == nil {
			// Re-parse from the task's schedule string so Resume is resilient.
			parsed, perr := parseSchedule(task.Schedule)
			if perr != nil {
				return fmt.Errorf("resume task %d: %w", taskID, perr)
			}
			sched = parsed
			m.mu.Lock()
			m.scheds[taskID] = sched
			m.scheduleTypes[taskID] = scheduleType
			m.mu.Unlock()
		}
		if err := m.engine.Add(taskID, sched, m.onFire); err != nil {
			return fmt.Errorf("resume task %d: %w", taskID, err)
		}
	} else {
		m.mu.Lock()
		m.eventDrivenTasks[taskID] = struct{}{}
		m.mu.Unlock()
	}

	task.Enabled = true
	m.setTask(task)

	m.logger.Info("task resumed",
		zap.Int64("task_id", taskID),
	)
	return nil
}

// Stop gracefully stops the manager and, if the engine was created
// internally, the underlying engine. It also closes the event bus if it
// was created internally.
func (m *Service) Stop(ctx context.Context) error {
	if m.ownEngine {
		if err := m.engine.Stop(ctx); err != nil {
			return fmt.Errorf("stop engine: %w", err)
		}
	}
	if m.ownBus && m.bus != nil {
		if err := m.bus.Close(); err != nil {
			m.logger.Warn("failed to close event bus", zap.Error(err))
		}
	}
	m.logger.Info("task manager stopped")
	return nil
}

// onFire is the callback invoked when a task's time wheel entry expires.
// It retrieves the task, checks shard ownership and dependencies, publishes
// an ExecutionTriggered event. The engine handles rescheduling automatically.
//
// onFire is invoked directly by the engine on its own goroutine. A panic
// here would crash the engine, so the callback is wrapped with a deferred
// recovery that logs the panic and stack trace via zap and returns.
//
// SubscribeEvents, handleStatusChange, trigger, and newRunID live in
// events.go. Restore, getTask, setTask, deleteTask, and listTasks live in
// persistence.go. ClassifySchedule and parseSchedule live in schedule.go.
func (m *Service) onFire(taskID int64) {
	defer func() {
		if r := recover(); r != nil {
			m.logger.Error("panic in task onFire callback",
				zap.Int64("task_id", taskID),
				zap.Any("panic", r),
				zap.Stack("stack"),
			)
		}
	}()

	task, err := m.getTask(taskID)
	if err != nil {
		m.logger.Warn("task not found on fire", zap.Int64("task_id", taskID))
		return
	}

	if !m.shardManager.Owns(taskID) {
		m.logger.Debug("task not owned by this shard, skipping",
			zap.Int64("task_id", taskID),
		)
		return
	}

	if task.DependsOn != 0 {
		if !m.deps.CanExecute(task.DependsOn) {
			m.logger.Warn("dependency not met, skipping task",
				zap.Int64("task_id", taskID),
				zap.Int64("depends_on", task.DependsOn),
				zap.String("skip_reason", ErrDependencyNotMet.Error()),
			)
			return
		}
	}

	// Concurrency == 1 means no concurrent execution: claim the running
	// slot atomically (check-and-set under a single lock) to avoid the
	// TOCTOU race where two concurrent fires both observe "not running"
	// and both proceed to trigger.
	if task.Concurrency == 1 {
		if !m.tryClaimRunning(taskID) {
			m.logger.Warn("previous execution still running, skipping task",
				zap.Int64("task_id", taskID),
				zap.String("skip_reason", ErrTaskRunning.Error()),
			)
			return
		}
	}

	m.trigger(task, TriggerTypeSchedule)
}

// tryClaimRunning atomically checks whether the task is already running and,
// if not, marks it as running. It returns true when the caller has
// successfully claimed the slot and must call releaseRunning (implicitly via
// the ExecutionCompleted subscription, or explicitly on publish failure) when
// the execution finishes. Returns false when the task is already running.
//
// This helper exists to make the check-and-set atomic under a single lock
// acquisition, preventing the race where two concurrent onFire callbacks
// both observe "not running" between separate lock/unlock pairs.
func (m *Service) tryClaimRunning(taskID int64) bool {
	m.runningMu.Lock()
	defer m.runningMu.Unlock()
	if _, running := m.running[taskID]; running {
		return false
	}
	m.running[taskID] = struct{}{}
	return true
}

// releaseRunning clears the running marker for a task. It is called by the
// ExecutionCompleted subscriber when an execution finishes and by trigger
// when publishing the ExecutionTriggered event fails, so the next fire is
// not permanently blocked for Concurrency == 1 tasks.
func (m *Service) releaseRunning(taskID int64) {
	m.runningMu.Lock()
	delete(m.running, taskID)
	m.runningMu.Unlock()
}

// checkMinInterval validates that interval-based schedules respect the
// quota-imposed minimum interval. It queries pkg/quota.Ceiling at check
// time so dynamic plan changes take effect immediately. Non-interval
// schedule types always pass.
func checkMinInterval(scheduleType ScheduleType, interval time.Duration) error {
	if scheduleType != ScheduleTypeInterval {
		return nil
	}
	minSecs := quota.Ceiling(quota.TypeScheduledTaskInterval)
	if minSecs <= 0 {
		return nil
	}
	minInterval := time.Duration(minSecs) * time.Second
	if interval > 0 && interval < minInterval {
		return fmt.Errorf("%w: got %s, minimum %s", ErrIntervalTooSmall, interval, minInterval)
	}
	return nil
}

// unscheduleInternal removes a task from the engine without deleting it
// from the task store. An engine.Remove failure is logged via zap rather than
// propagated: the caller paths (Update, Pause, Unschedule, Restore) all
// continue with their own bookkeeping regardless, and surfacing the error
// would force every caller to handle a partial-failure state that is
// already self-correcting on the next Add.
func (m *Service) unscheduleInternal(taskID int64) {
	if err := m.engine.Remove(taskID); err != nil {
		m.logger.Warn("failed to remove task from engine",
			zap.Int64("task_id", taskID),
			zap.Error(err),
		)
	}
	m.deps.Reset(taskID)
}
