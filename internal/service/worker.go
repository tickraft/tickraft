// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package service

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/tickraft/tickraft/pkg/asset"
	"github.com/tickraft/tickraft/pkg/event"
	"github.com/tickraft/tickraft/pkg/executor"
	"github.com/tickraft/tickraft/pkg/executor/http"
	"github.com/tickraft/tickraft/pkg/executor/icmp"
	"github.com/tickraft/tickraft/pkg/executor/local"
	mqttexec "github.com/tickraft/tickraft/pkg/executor/mqtt"
	"github.com/tickraft/tickraft/pkg/executor/tcp"
	"github.com/tickraft/tickraft/pkg/executor/webhook"
	"github.com/tickraft/tickraft/pkg/task"
	"github.com/tickraft/tickraft/pkg/telemetry"
	"github.com/tickraft/tickraft/pkg/telemetry/processor"
)

// registerBuiltinExecutors registers the built-in executors (local script,
// webhook, and ICMP/TCP/MQTT/HTTP probers) into the registry using the
// given probe timeout for prober executors.
func registerBuiltinExecutors(reg *executor.Registry, probeTimeout time.Duration) error {
	executors := []executor.Executor{
		local.New(local.WithLogger(zap.L())),
		webhook.New(webhook.WithLogger(zap.L())),
		icmp.New(probeTimeout),
		tcp.New(probeTimeout),
		mqttexec.New(probeTimeout, mqttexec.WithLogger(zap.L())),
		http.New(http.WithLogger(zap.L())),
	}
	for _, e := range executors {
		if err := reg.Register(e); err != nil {
			return fmt.Errorf("register executor %q: %w", e.Name(), err)
		}
	}
	return nil
}

// registerBuiltinProcessors registers the built-in processors (device and
// task) into the processor registry.
func registerBuiltinProcessors(reg *telemetry.ProcessorRegistry, assetStore asset.Store,
	bus event.Bus, logger *zap.Logger) error {
	processors := []telemetry.Processor{
		processor.NewDevice(assetStore, bus, logger),
		processor.NewTask(assetStore, bus, logger),
	}
	for _, p := range processors {
		if err := reg.Register(p); err != nil {
			return fmt.Errorf("register processor %q: %w", p.Type(), err)
		}
	}
	return nil
}

// migrateCollectorTables runs AutoMigrate for the telemetry's GORM models,
// including the unified monitor_points table.
func migrateCollectorTables(ctx context.Context, dbc *gorm.DB) error {
	if err := dbc.WithContext(ctx).AutoMigrate(
		&telemetry.StatusHistory{},
		&telemetry.CollectMetric{},
		&telemetry.CollectLog{},
		&telemetry.Template{},
	); err != nil {
		return fmt.Errorf("telemetry: auto migrate: %w", err)
	}
	if err := telemetry.Migrate(ctx, dbc); err != nil {
		return fmt.Errorf("telemetry: migrate monitor_points: %w", err)
	}
	return nil
}

// buildWorkerRegistry builds an executor registry containing all built-in
// executors (both actuator and prober roles). The Worker is a unified
// deployment that always registers every executor; the executor Role enum
// is retained only as executor metadata, not as a startup filter.
func buildWorkerRegistry(probeTimeout time.Duration) (*executor.Registry, error) {
	reg := executor.NewRegistry()
	if err := registerBuiltinExecutors(reg, probeTimeout); err != nil {
		return nil, err
	}
	return reg, nil
}

// startWorkerEngines starts the executor runner, scheduler, and telemetry
// manager together as a unified worker. It returns a stop function that
// gracefully stops them in reverse sub-order (telemetry → scheduler →
// executor).
//
// The Worker always starts all three modules (Scheduler + Executor +
// Collector); role-based module filtering is no longer supported. The event
// bus connects the three modules per the existing contract
// (TypeExecutionTriggered, TypeExecutionCompleted, TypeAssetStatusChanged).
//
// In standalone mode the telemetry runs in-process without binding its own
// HTTP listener; webhook report ingestion is handled by the main API server
// routes registered in startAPIServer.
func startWorkerEngines(
	ctx context.Context,
	rt *runtime,
	executorPoolSize int,
	probeTimeout time.Duration,
) (stopFunc, error) {
	var (
		runner    executor.Runner
		sched     task.TaskEngine
		collector telemetry.Collector
	)

	// Collector needs the asset store (already created in initRuntime)
	// and its own table migrations.
	if err := migrateCollectorTables(ctx, rt.dbc); err != nil {
		return nil, err
	}

	bus := rt.eventBus()

	// Executor runner + scheduler (started together because the scheduler
	// publishes ExecutionTriggered events that the runner consumes).
	reg, err := buildWorkerRegistry(probeTimeout)
	if err != nil {
		return nil, err
	}
	// Store the registry on the runtime so startAPIServer can prevalidate
	// executor types at creation time (see newTaskRouteOptions and
	// newTelemetryRouteOptions).
	rt.executorRegistry = reg

	// Migrate scheduler tables and build persistent task/execution stores
	// before constructing the executor runner so that execution results
	// can be persisted via the executor.RecordStore adapter. Earlier the
	// runner was created with the default noopRecordStore, which silently
	// dropped every execution result; wiring the GORM-backed adapter
	// ensures ListExecutions returns real outcomes (status, output,
	// error, duration, finished_at) instead of only trigger placeholders.
	if err = task.Migrate(ctx, rt.dbc); err != nil {
		stopWorkerEngines(ctx, rt.logger, collector, sched, runner)
		return nil, fmt.Errorf("migrate scheduler tables: %w", err)
	}
	taskStore := task.NewStore(rt.dbc)
	execStore := task.NewExecutionStore(rt.dbc)

	// Probe records live in the telemetry domain (sys_probe_record), not
	// the task scheduling log. The routing below is the assembly-layer
	// decision that owns domain ownership: OpExecute records flow to the
	// task adapter (sys_schedule_log), OpProbe records to the telemetry
	// store keyed by monitor point.
	probeStore := telemetry.NewProbeRecordStore(rt.dbc)
	if err = probeStore.Migrate(ctx); err != nil {
		stopWorkerEngines(ctx, rt.logger, collector, sched, runner)
		return nil, fmt.Errorf("migrate probe record table: %w", err)
	}
	recordStore := routingRecordStore{
		tasks:  task.NewExecutionRecordStore(execStore),
		probes: probeStore,
	}

	runner, err = executor.New(
		executor.WithExecutorRegistry(reg),
		executor.WithWorkerPoolSize(executorPoolSize),
		executor.WithEventBus(bus),
		executor.WithLogger(rt.logger),
		executor.WithRecordStore(recordStore),
		// Mode A (remote status reporting) dispatch rows always land in the
		// task domain: they are schedule-task executions referenced by the
		// telemetry report contract, independent of the probe/execute
		// record routing above.
		executor.WithDispatchStore(task.NewDispatchStore(rt.dbc)),
	)
	if err != nil {
		return nil, fmt.Errorf("create executor: %w", err)
	}
	if err = runner.Start(ctx); err != nil {
		return nil, fmt.Errorf("start executor: %w", err)
	}
	runner.SubscribeEvents(ctx)
	rt.logger.Info("executor runner started")

	sched, err = task.NewEngine(
		task.WithEventBus(bus),
		task.WithLogger(rt.logger),
		task.WithStore(taskStore),
		// Sweeper: reaps running Mode A dispatch rows whose remote status
		// report never arrived (miss-report fallback), flipping them to
		// timeout and releasing the concurrency slot.
		task.WithExecutionStore(execStore),
	)
	if err != nil {
		stopWorkerEngines(ctx, rt.logger, collector, sched, runner)
		return nil, fmt.Errorf("create scheduler: %w", err)
	}
	sched.SubscribeEvents(ctx)

	// Restore persisted tasks into memory and schedule them before
	// the engine starts serving traffic.
	if err = sched.Restore(ctx); err != nil {
		stopWorkerEngines(ctx, rt.logger, collector, sched, runner)
		return nil, fmt.Errorf("restore scheduler tasks: %w", err)
	}

	// Store on the runtime so startAPIServer can build the task
	// service backed by the real engine and persistent stores.
	rt.schedulerEngine = sched
	rt.schedulerTaskStore = taskStore
	rt.schedulerExecStore = execStore

	// Mode A report consumer: binds remote task status reports — bridged
	// from the telemetry listener callback onto the bus by startAPIServer —
	// to execution rows.
	reportConsumer := task.NewReportConsumer(bus, rt.dbc, rt.logger)
	if err = reportConsumer.Start(ctx); err != nil {
		stopWorkerEngines(ctx, rt.logger, collector, sched, runner)
		return nil, fmt.Errorf("start task report consumer: %w", err)
	}

	rt.logger.Info("scheduler started")

	// Collector manager. In standalone single-port mode the telemetry does
	// not bind its own HTTP listener; webhook report ingestion is handled
	// by the main API server.
	procReg := telemetry.NewProcessorRegistry()
	if err = registerBuiltinProcessors(procReg, rt.assetStore, bus, rt.logger); err != nil {
		stopWorkerEngines(ctx, rt.logger, collector, sched, runner)
		return nil, fmt.Errorf("register processors: %w", err)
	}

	metricStore := telemetry.NewMetricStore(rt.dbc)
	logStore := telemetry.NewLogStore(rt.dbc)

	// ProberService: coordinates active probing by scheduling MonitorPoints
	// (Mode=ModeActive) through the shared task.TaskEngine. Created before
	// telemetry.New so it can be injected via WithProberService; the
	// engine's Start calls proberSvc.Start which loads and registers all
	// active, enabled points from the DB.
	monitorStore := telemetry.NewMonitorStore(rt.dbc)
	proberSvc := telemetry.NewProberService(
		sched, rt.logger,
		telemetry.WithProberMonitorStore(monitorStore),
	)
	rt.proberSvc = proberSvc

	collector, err = telemetry.New(
		telemetry.WithProcessorRegistry(procReg),
		telemetry.WithAssetStore(rt.assetStore),
		telemetry.WithEventBus(bus),
		telemetry.WithDB(rt.dbc),
		telemetry.WithLogger(rt.logger),
		telemetry.WithMetricStore(metricStore),
		telemetry.WithLogStore(logStore),
		telemetry.WithAggregationWindow(time.Minute),
		telemetry.WithProberService(proberSvc),
		telemetry.WithMonitorStore(monitorStore),
	)
	if err != nil {
		stopWorkerEngines(ctx, rt.logger, collector, sched, runner)
		return nil, fmt.Errorf("create telemetry: %w", err)
	}
	if err = collector.Start(ctx); err != nil {
		stopWorkerEngines(ctx, rt.logger, collector, sched, runner)
		return nil, fmt.Errorf("start telemetry: %w", err)
	}
	// Store the collector on the runtime so startAPIServer can wire the
	// webhook listener's ingest callback to the collector's Submit method.
	// This enables POST /api/v1/telemetry to forward received telemetry
	// into the processing pipeline.
	rt.telemetryCollector = collector
	// Store the metric/log stores so startAPIServer can wire the telemetry
	// handler's history/logs endpoints to real persistent data, and the
	// probe record store for the monitor status/history/logs endpoints.
	rt.metricStore = metricStore
	rt.logStore = logStore
	rt.probeRecordStore = probeStore
	rt.logger.Info("telemetry started")

	return func(ctx context.Context) error {
		reportConsumer.Stop()
		stopWorkerEngines(ctx, rt.logger, collector, sched, runner)
		return nil
	}, nil
}

// stopWorkerEngines stops the telemetry, scheduler, and executor in reverse
// sub-order, logging any errors. Components that were not started are skipped.
func stopWorkerEngines(
	ctx context.Context,
	logger *zap.Logger,
	collector telemetry.Collector,
	sched task.TaskEngine,
	runner executor.Runner,
) {
	if collector != nil {
		if err := collector.Stop(ctx); err != nil {
			logger.Error("stop telemetry", zap.Error(err))
		}
	}
	if sched != nil {
		if err := sched.Stop(ctx); err != nil {
			logger.Error("stop scheduler", zap.Error(err))
		}
	}
	if runner != nil {
		if err := runner.Stop(ctx); err != nil {
			logger.Error("stop executor", zap.Error(err))
		}
	}
}

// routingRecordStore implements executor.RecordStore by dispatching each
// record to the domain that owns it: OpProbe records (active monitor point
// probes) go to the telemetry probe record store, everything else goes to
// the task-domain execution log. Keeping the routing here — the assembly
// layer — leaves the task and telemetry packages unaware of each other's
// persistence.
type routingRecordStore struct {
	tasks  executor.RecordStore
	probes executor.RecordStore
}

// Save persists the record through the domain store matching its operation.
func (s routingRecordStore) Save(ctx context.Context, record executor.ExecutionRecord) error {
	if record.Operation == executor.OpProbe {
		return s.probes.Save(ctx, record)
	}
	return s.tasks.Save(ctx, record)
}

// Compile-time assertion that routingRecordStore satisfies the runner SPI.
var _ executor.RecordStore = routingRecordStore{}
