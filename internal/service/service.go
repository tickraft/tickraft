// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package service

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/config"
)

// Start starts all components (server + worker + prism) in a single process.
// Startup order: runtime (DB, logger, event bus, auth) → worker → prism → API server.
// Shutdown is performed in reverse order on SIGINT/SIGTERM or a component error.
//
// The caller is expected to have already resolved cfg from CLI flags / config
// files; this function must not depend on cobra so it can be unit-tested.
//
// Start wraps ctx in an internal cancellable context so that a fatal
// component error reported on the error channel can trigger cancellation of
// all child components before the graceful shutdown sequence runs.
func Start(ctx context.Context, cfg *config.Config) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	rt, err := initRuntime(ctx, cfg)
	if err != nil {
		return err
	}
	defer rt.close()

	rt.logger.Info("starting tickraft services")

	errCh := make(chan error, 4)

	// 1. Worker engines (executor + telemetry + scheduler).
	// Concurrence (0=auto) sizes the executor goroutine pool; ProbeTimeout
	// is the fallback timeout for probers without an explicit per-probe value.
	workerStop, err := startWorkerEngines(ctx, rt,
		cfg.Worker.Concurrence,
		cfg.Worker.ProbeTimeout.Duration())
	if err != nil {
		return err
	}

	// 2. Prism. Concurrence sizes the notification dispatch goroutine
	// pool. Channels are loaded from the database at startup and
	// hot-reloaded on CRUD operations via the API.
	prismStop, err := startPrismEngine(ctx, rt, cfg.Prism.Concurrence)
	if err != nil {
		stopQuietly(ctx, rt.logger, "worker", workerStop)
		return err
	}

	// 3. Maintenance loop.
	maintenanceStop, err := startMaintenanceLoop(ctx, rt, cfg.Server.MaintenanceInterval.Duration())
	if err != nil {
		stopQuietly(ctx, rt.logger, "prism", prismStop)
		stopQuietly(ctx, rt.logger, "worker", workerStop)
		return err
	}

	// 4. API server (started last so it only binds after engines are ready).
	serverStop, err := startAPIServer(ctx, rt, errCh)
	if err != nil {
		stopQuietly(ctx, rt.logger, "maintenance", maintenanceStop)
		stopQuietly(ctx, rt.logger, "prism", prismStop)
		stopQuietly(ctx, rt.logger, "worker", workerStop)
		return err
	}

	// Wait for shutdown signal or a fatal component error.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-quit:
		rt.logger.Info("shutting down all components...")
	case runErr := <-errCh:
		rt.logger.Error("component error, initiating shutdown", zap.Error(runErr))
		cancel()
	}

	// Graceful shutdown in reverse startup order.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), defaultShutdownTimeout)
	defer shutdownCancel()

	stopAndLog(shutdownCtx, rt.logger, "server", serverStop)
	stopAndLog(shutdownCtx, rt.logger, "maintenance", maintenanceStop)
	stopAndLog(shutdownCtx, rt.logger, "prism", prismStop)
	stopAndLog(shutdownCtx, rt.logger, "worker", workerStop)

	rt.logger.Info("all components stopped")
	return nil
}

// stopAndLog invokes stop with the given shutdown context and logs any
// error at the Error level. It never returns an error.
func stopAndLog(ctx context.Context, logger *zap.Logger, name string, stop stopFunc) {
	if stop == nil {
		return
	}
	if err := stop(ctx); err != nil {
		logger.Error("stop "+name, zap.Error(err))
	}
}

// stopQuietly invokes stop with a short timeout and logs any error at warn
// level. It is used to clean up already-started components when a later
// component fails to start; the cleanup error is secondary to the startup
// failure being propagated by the caller, so it is logged rather than
// returned.
func stopQuietly(ctx context.Context, logger *zap.Logger, name string, stop stopFunc) {
	if stop == nil {
		return
	}
	stopCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := stop(stopCtx); err != nil {
		logger.Warn("stop "+name+" on startup failure", zap.Error(err))
	}
}
