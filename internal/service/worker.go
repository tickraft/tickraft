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
	httpprober "github.com/tickraft/tickraft/pkg/executor/http"
	"github.com/tickraft/tickraft/pkg/executor/icmp"
	"github.com/tickraft/tickraft/pkg/executor/local"
	"github.com/tickraft/tickraft/pkg/executor/tcp"
	"github.com/tickraft/tickraft/pkg/executor/webhook"
	"github.com/tickraft/tickraft/pkg/task"
	"github.com/tickraft/tickraft/pkg/telemetry"
	"github.com/tickraft/tickraft/pkg/telemetry/processor"
)

// registerBuiltinExecutors registers the built-in executors (local script,
// webhook, and ICMP/TCP/HTTP probers) into the registry using the given
// probe timeout for prober executors.
func registerBuiltinExecutors(reg *executor.Registry, probeTimeout time.Duration) error {
	executors := []executor.Executor{
		local.New(local.WithLogger(zap.L())),
		webhook.New(webhook.WithLogger(zap.L())),
		icmp.New(probeTimeout),
		tcp.New(probeTimeout),
		httpprober.New(10 * time.Second),
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
func registerBuiltinProcessors(reg *telemetry.ProcessorRegistry, assetStore asset.Store, bus event.Bus, logger *zap.Logger) error {
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
// then migrates the unified monitor_points table (including optional legacy
// data porting from sys_collect_config).
func migrateCollectorTables(ctx context.Context, dbc *gorm.DB, logger *zap.Logger) error {
	if err := dbc.WithContext(ctx).AutoMigrate(
		&telemetry.CollectionConfig{},
		&telemetry.StatusHistory{},
		&telemetry.CollectMetric{},
		&telemetry.CollectLog{},
		&telemetry.Template{},
	); err != nil {
		return fmt.Errorf("telemetry: auto migrate: %w", err)
	}
	// Migrate the unified monitor_points table and port legacy
	// CollectionConfig rows into it. This runs after the CollectionConfig
	// table is created so the legacy data migration can read from it.
	if err := telemetry.Migrate(ctx, dbc, logger); err != nil {
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
func startWorkerEngines(ctx context.Context, rt *runtime,
	executorPoolSize int, probeTimeout time.Duration,
) (stopFunc, error) {
	var (
		runner    executor.Runner
		sched     task.Manager
		collector telemetry.Collector
	)

	// Collector needs the asset store (already created in initRuntime)
	// and its own table migrations.
	if err := migrateCollectorTables(ctx, rt.dbc, rt.logger); err != nil {
		return nil, err
	}

	bus := rt.eventBus()

	// Executor runner + scheduler (started together because the scheduler
	// publishes ExecutionTriggered events that the runner consumes).
	reg, err := buildWorkerRegistry(probeTimeout)
	if err != nil {
		return nil, err
	}

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
	recordStore := task.NewExecutionRecordStore(execStore)

	runner, err = executor.New(
		executor.WithExecutorRegistry(reg),
		executor.WithWorkerPoolSize(executorPoolSize),
		executor.WithEventBus(bus),
		executor.WithLogger(rt.logger),
		executor.WithRecordStore(recordStore),
	)
	if err != nil {
		return nil, fmt.Errorf("create executor: %w", err)
	}
	if err = runner.Start(ctx); err != nil {
		return nil, fmt.Errorf("start executor: %w", err)
	}
	runner.SubscribeEvents(ctx)
	rt.logger.Info("executor runner started")

	sched, err = task.NewService(
		task.WithEventBus(bus),
		task.WithLogger(rt.logger),
		task.WithStore(taskStore),
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
	// (Mode=ModeActive) through the shared task.Manager. Created before
	// telemetry.New so it can be injected via WithProberService; the
	// Manager.Start calls proberSvc.Start which loads and registers all
	// active, enabled points from the DB.
	monitorStore := telemetry.NewMonitorStore(rt.dbc)
	proberSvc := telemetry.NewProberService(
		sched, reg, nil, rt.logger,
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
	// handler's history/logs endpoints to real persistent data.
	rt.metricStore = metricStore
	rt.logStore = logStore
	rt.logger.Info("telemetry started")

	return func(ctx context.Context) error {
		stopWorkerEngines(ctx, rt.logger, collector, sched, runner)
		return nil
	}, nil
}

// stopWorkerEngines stops the telemetry, scheduler, and executor in reverse
// sub-order, logging any errors. Components that were not started are skipped.
func stopWorkerEngines(ctx context.Context, logger *zap.Logger, collector telemetry.Collector, sched task.Manager, runner executor.Runner) {
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
