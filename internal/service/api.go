// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package service

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/tickraft/tickraft/internal/api/router"
	"github.com/tickraft/tickraft/internal/api/service/prism"
	"github.com/tickraft/tickraft/internal/api/service/scheduler"
	"github.com/tickraft/tickraft/internal/api/service/system"
	telemetrysvc "github.com/tickraft/tickraft/internal/api/service/telemetry"
	"github.com/tickraft/tickraft/internal/web"
	"github.com/tickraft/tickraft/pkg/api"
	"github.com/tickraft/tickraft/pkg/api/handler/asset"
	"github.com/tickraft/tickraft/pkg/api/handler/certificates"
	"github.com/tickraft/tickraft/pkg/api/handler/healthz"
	"github.com/tickraft/tickraft/pkg/api/handler/i18n"
	"github.com/tickraft/tickraft/pkg/api/handler/readyz"
	telemetryhandler "github.com/tickraft/tickraft/pkg/api/handler/telemetry"
	wsHandler "github.com/tickraft/tickraft/pkg/api/handler/ws"
	"github.com/tickraft/tickraft/pkg/auth"
	"github.com/tickraft/tickraft/pkg/task"
	"github.com/tickraft/tickraft/pkg/telemetry"
	telemetryhttp "github.com/tickraft/tickraft/pkg/telemetry/http"
)

// startAPIServer initializes auth, builds the HTTP API server, registers
// routes and SPA assets, and starts the server in a goroutine. It returns a
// stop function that gracefully shuts down the server. Fatal server errors
// are reported on errCh.
//
// The runtime always runs in single-port mode: the server
// listens on a single address (server.addr) and serves both the API and the
// SPA frontend from that one port. Multi-port and virtual-host modes are
// optional features and are not configured here.
//
// This function wires the alert service (backed by the prism engine and
// persistent rule/record stores), the task service (backed by the scheduler
// engine and persistent task/execution stores), the asset management
// handler (backed by the GORM asset store), the healthz handler (probing
// DB and cache), and the asset-key getter (validating
// X-Tickraft-Asset-Key headers against the asset store) into the API
// routes via RegisterOption values.
func startAPIServer(ctx context.Context, rt *runtime, errCh chan<- error) (stopFunc, error) {
	if err := initAuth(ctx, rt); err != nil {
		return nil, err
	}

	// Map config.Server and config.Logger fields to the API server config.
	// The Logger.Mode is passed as the API server's Mode to keep logger
	// and server output format consistent.
	sc := rt.cfg.Server
	cfg := api.ServerConfig{
		Addr:            sc.Addr,
		Mode:            rt.cfg.Logger.Mode,
		EnableCORS:      sc.EnableCORS,
		EnableLog:       sc.EnableAccessLog,
		MaxHeaderBytes:  sc.MaxHeaderBytes,
		ReadTimeout:     sc.ReadTimeout.Duration(),
		WriteTimeout:    sc.WriteTimeout.Duration(),
		TLSEnabled:      sc.TLSEnabled,
		TLSCertFile:     sc.TLSCertFile,
		TLSKeyFile:      sc.TLSKeyFile,
		TLSMinVersion:   sc.TLSMinVersion,
		TLSCipherSuites: append([]string(nil), sc.TLSCipherSuites...),
		TLSClientCAFile: sc.TLSClientCAFile,
		TLSClientAuth:   sc.TLSClientAuth,
		ACME: api.ACMEConfig{
			Enabled:       sc.ACME.Enabled,
			DirectoryURL:  sc.ACME.DirectoryURL,
			Email:         sc.ACME.Email,
			ChallengeType: sc.ACME.ChallengeType,
			Domains:       append([]string(nil), sc.ACME.Domains...),
		},
	}

	cfg.SetDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validate server tls config: %w", err)
	}

	// Bridge Hertz's hlog to the runtime zap logger so all framework-level
	// logs (middleware recovery, access log, TLS reload, ACME, shutdown)
	// flow through the same structured logging pipeline. This must be called
	// before api.NewServer because NewServer calls hlog.SetLevel to apply
	// the mode-based level filter, and that call must target the bridged
	// logger rather than the default stderr-based hlog writer.
	api.SetLogger(rt.logger)

	srv := api.NewServer(cfg)

	// Build the asset-key getter from the asset store so that
	// telemetry report endpoints can validate X-Tickraft-Asset-Key
	// headers against existing resources.
	assetKeyGetter := func(ctx context.Context, key string) (bool, error) {
		return rt.assetStore.ExistsByKey(ctx, key)
	}

	// Build the RegisterOption list from the runtime's shared resources.
	// In standalone mode every engine (worker, prism) has already been
	// started, so all stores must be non-nil. A nil store here indicates a
	// startup sequencing bug — fail hard instead of silently registering a
	// partial route surface.
	var routeOpts []router.RegisterOption

	// Alert service: backed by the rule engine and persistent stores
	// accessed via the prism engine's accessor methods.
	if rt.prismEngine == nil {
		return nil, fmt.Errorf("start api server: prism engine is nil; prism engine may not have started")
	}
	eng := rt.prismEngine
	if eng.RuleStore() == nil || eng.RecordStore() == nil {
		return nil, fmt.Errorf("start api server: prism rule/record stores are nil; prism engine may not have started")
	}
	alertSvc := prism.NewAlertService(eng.RuleStore(), eng.RecordStore(), eng.RuleEngine())
	routeOpts = append(routeOpts, router.WithAlertService(alertSvc))

	// Channel service: backed by the persistent channel store accessed
	// via the prism engine.
	if eng.ChannelStore() == nil {
		return nil, fmt.Errorf("start api server: prism channel store is nil; prism engine may not have started")
	}
	channelSvc := prism.NewChannelService(eng.ChannelStore(), eng)
	routeOpts = append(routeOpts, router.WithChannelService(channelSvc))

	// Remediation rule service: backed by the persistent remediation rule
	// store accessed via the prism engine.
	if eng.RemediationStore() == nil {
		return nil, fmt.Errorf("start api server: prism remediation store is nil; prism engine may not have started")
	}
	remediationRuleSvc := prism.NewRemediationService(eng.RemediationStore())
	routeOpts = append(routeOpts, router.WithRemediationRuleService(remediationRuleSvc))

	// Task service: backed by the scheduler engine and persistent task /
	// execution stores. The scheduler engine must have been started by
	// this point in standalone mode; nil stores indicate a startup order
	// bug.
	if rt.schedulerEngine == nil || rt.schedulerTaskStore == nil || rt.schedulerExecStore == nil {
		return nil, fmt.Errorf("start api server: scheduler engine/stores are nil; worker engines may not have started")
	}
	taskSvc := scheduler.NewTaskService(
		rt.schedulerEngine,
		rt.schedulerTaskStore,
		rt.schedulerExecStore,
		rt.logger,
	)
	routeOpts = append(routeOpts, router.WithTaskService(taskSvc))

	// Asset handler: backed by the GORM asset store created in
	// initRuntime. Must be non-nil.
	if rt.assetStore == nil {
		return nil, fmt.Errorf("start api server: asset store is nil; runtime may not have initialized")
	}
	assetH := asset.NewHandler(rt.assetStore, rt.logger)
	routeOpts = append(routeOpts, router.WithAssetHandler(assetH))

	// Telemetry service: backed by the persistent MonitorStore (monitor_points
	// table) created by the worker engines. All CRUD operations survive
	// process restarts. Prober hooks are wired so active monitoring points
	// are scheduled/unscheduled in real time through the ProberService.
	monitorStore := telemetry.NewMonitorStore(rt.dbc)
	var telemetryOpts []telemetrysvc.Option
	if rt.proberSvc != nil {
		telemetryOpts = append(telemetryOpts, telemetrysvc.WithPointHandlers(
			func(ctx context.Context, point telemetry.MonitorPoint) error {
				return rt.proberSvc.RegisterPoint(ctx, point)
			},
			func(ctx context.Context, pointID int64) error {
				return rt.proberSvc.UnregisterPoint(ctx, pointID)
			},
		))
	}
	telemetrySvc := telemetrysvc.NewService(monitorStore, rt.logger, telemetryOpts...)
	routeOpts = append(routeOpts, router.WithTelemetryService(telemetrySvc))

	// Telemetry data stores: wire the metric and log stores (created by the
	// worker engines) so the telemetry handler's history/logs endpoints
	// query real persistent data instead of returning empty stubs.
	routeOpts = append(routeOpts, router.WithTelemetryDataStores(rt.metricStore, rt.logStore))

	// Telemetry report handler: wires the webhook listener to the telemetry
	// collector so POST /api/v1/telemetry forwards received payloads into
	// the processing pipeline. The webhook listener exposes a net/http.Handler
	// via ReportHandler(); the Hertz/net/http adapter wraps it so it can be
	// mounted on the Hertz route registered by WithTelemetryReportHandler.
	//
	// In standalone mode the telemetry collector is always started by the
	// worker engines; nil indicates a startup order bug.
	if rt.telemetryCollector == nil {
		return nil, fmt.Errorf("start api server: telemetry collector is nil; worker engines may not have started")
	}
	ingest := func(_ context.Context, t *telemetry.Telemetry) {
		rt.telemetryCollector.Submit(t)
	}
	webhookListener := telemetryhttp.New(
		telemetryhttp.WithStore(rt.assetStore),
		telemetryhttp.WithIngest(ingest),
		telemetryhttp.WithLogger(rt.logger),
	)
	reportAdapter := telemetryhandler.NewTelemetryReportHandlerAdapter(webhookListener.ReportHandler(), rt.logger)
	routeOpts = append(routeOpts, router.WithTelemetryReportHandler(reportAdapter))

	// Healthz handler: probes the database (SELECT 1) and the cache (Has
	// sentinel key). The cache may be nil when caching is disabled; the
	// handler skips nil dependencies.
	healthzH := healthz.NewHandler(rt.dbc, rt.cache)
	routeOpts = append(routeOpts, router.WithHealthzHandler(healthzH))

	// Readyz handler: probes the database and cache in parallel with a
	// per-check timeout. Returns 503 when any dependency is down so a load
	// balancer can route traffic away from a not-yet-ready instance. The
	// cache may be nil when caching is disabled; the handler skips nil
	// dependencies.
	readyzH := readyz.NewHandler(rt.dbc, rt.cache)
	routeOpts = append(routeOpts, router.WithReadyzHandler(readyzH))

	// Certificate reload handler: registered only when TLS is enabled so the
	// POST /api/v1/system/certificates/reload endpoint is available exactly
	// when there is a live certificate to reload. The handler delegates to
	// srv.ReloadTLSConfig, which atomically swaps the active *tls.Config and
	// returns the SHA-256 fingerprint of the new leaf certificate.
	if sc.TLSEnabled {
		if _, err := srv.ReloadTLSConfig(); err != nil {
			return nil, fmt.Errorf("initial TLS config load: %w", err)
		}
		certHandler, err := certificates.NewHandler(srv)
		if err != nil {
			return nil, fmt.Errorf("create certificate handler: %w", err)
		}
		routeOpts = append(routeOpts, router.WithCertificateHandler(certHandler))
	}

	// Telemetry template handler: backed by the GORM template store,
	// seeded on every startup with the CE built-in template set
	// (icmp/tcp/http(s); LoadBuiltinTemplates is idempotent and removes
	// any pro-only rows). Template CRUD and the apply endpoint are CE
	// capabilities (docs/architecture/api-routing.md); the pro edition
	// extends the same surface by injecting its own handler with the full
	// (core + pro) built-in set.
	templateStore := telemetry.NewTemplateStore(rt.dbc)
	if err := telemetry.LoadBuiltinTemplates(rt.dbc); err != nil {
		return nil, fmt.Errorf("load builtin telemetry templates: %w", err)
	}
	templateH := telemetryhandler.NewTemplateHandler(templateStore, telemetrySvc)
	routeOpts = append(routeOpts, router.WithTemplateHandler(templateH))

	// System service: backed by the database for config persistence,
	// build-time metadata for version info, and the runtime's task /
	// asset / execution stores for global stats. Always available when
	// the runtime is initialized.
	systemSvc := system.New(rt.dbc, rt.logger, rt.schedulerTaskStore, rt.schedulerExecStore, rt.assetStore)
	if err := systemSvc.Migrate(ctx); err != nil {
		return nil, fmt.Errorf("migrate system service: %w", err)
	}
	routeOpts = append(routeOpts, router.WithSystemService(systemSvc))

	// i18n handler: exposes the locale list via GET /api/v1/i18n/locales.
	// The endpoint is public (no JWT) so the frontend can discover
	// available locales before authentication. The callers
	// extends the locale list transparently by registering additional
	// locale bundles in the Registry at startup.
	if rt.i18nRegistry != nil {
		i18nHandler := i18n.NewHandler(rt.i18nRegistry)
		routeOpts = append(routeOpts, router.WithI18nHandler(i18nHandler))
	}

	// WebSocket realtime push: subscribe to the shared event bus and
	// register the /ws endpoint. Stopped together with the server.
	wsH := wsHandler.NewHandler(rt.jwt, rt.eventBus(), rt.logger)
	if err := wsH.Start(context.Background()); err != nil {
		return nil, fmt.Errorf("start ws handler: %w", err)
	}
	routeOpts = append(routeOpts, router.WithWSHandler(wsH))

	if err := router.RegisterRoutes(srv, rt.jwt, rt.authz, assetKeyGetter, routeOpts...); err != nil {
		return nil, fmt.Errorf("register routes: %w", err)
	}

	// ACME: when ACME auto-issuance is enabled register the HTTP-01 challenge
	// handler on the Hertz server so ACME validations reach it on the same
	// port as the API, then start the ACME renewal loop in a background
	// goroutine. The loop is tied to the server context so it exits when the
	// server shuts down. Failures to issue are logged by the loop and do not
	// abort startup, so a transient ACME server failure cannot take an
	// otherwise-healthy server down.
	if sc.TLSEnabled && sc.ACME.Enabled {
		http01 := api.NewHTTP01Provider()
		api.SetACMEProvider(http01)
		srv.Group("").GET(api.HTTP01ChallengePath+":token", http01.Handler)

		// File-backed ACME cert store so issued certificates and the ACME
		// account key survive process restarts. The store writes the most
		// recent certificate to server.crt.pem / server.key.pem; these paths
		// are set on the server config so ReloadTLSConfig picks them up.
		acmeDataDir := filepath.Join("data", "acme")
		acmeCertStore, err := api.NewFileACMECertStore(acmeDataDir)
		if err != nil {
			return nil, fmt.Errorf("create acme cert store: %w", err)
		}
		cfg.TLSCertFile = acmeCertStore.ServerCertFile()
		cfg.TLSKeyFile = acmeCertStore.ServerKeyFile()
		// Rebuild the TLS config so the cert paths point at the ACME store.
		if _, err := srv.ReloadTLSConfig(); err != nil {
			// Pre-issuance: the cert files do not exist yet. This is
			// expected on first start; the ACME loop will create them.
			rt.logger.Debug("acme: initial tls reload deferred (no cert yet)", zap.Error(err))
		}

		acmeMgr := &api.ACMEManager{
			DirectoryURL:  cfg.ACME.DirectoryURL,
			Email:         cfg.ACME.Email,
			ChallengeType: cfg.ACME.ChallengeType,
			Domains:       cfg.ACME.Domains,
			Reloader:      srv,
			CertStore:     acmeCertStore,
		}
		acmeCtx, acmeCancel := context.WithCancel(ctx)
		// goroutine lifecycle: bound to acmeCtx (derived from ctx); Run
		// exits when acmeCtx is cancelled (on server shutdown via ctx) or
		// on internal ACME loop termination. The goroutine calls acmeCancel
		// itself on exit so the acmeCtx resource is released.
		go func() {
			if err := acmeMgr.Run(acmeCtx); err != nil {
				rt.logger.Error("acme manager exited", zap.Error(err))
				select {
				case errCh <- fmt.Errorf("acme manager: %w", err):
				default:
				}
			}
			acmeCancel()
		}()
		rt.logger.Info("acme manager started",
			zap.String("directory", cfg.ACME.DirectoryURL),
			zap.Strings("domains", acmeMgr.Domains),
		)
	}

	// Register SPA static assets (non-fatal if frontend is not embedded).
	if distFS, distErr := web.DistFS(); distErr != nil {
		rt.logger.Warn("frontend assets not embedded, skipping SPA", zap.Error(distErr))
	} else if spaErr := api.RegisterSPA(srv, distFS); spaErr != nil {
		rt.logger.Warn("register SPA", zap.Error(spaErr))
	}

	// goroutine lifecycle: bounded — runs srv.Start and exits after it
	// returns (server error or Shutdown via the returned stop function).
	// Fatal errors are reported on errCh (buffered), which the standalone
	// supervisor drains on shutdown.
	go func() {
		if runErr := srv.Start(); runErr != nil {
			select {
			case errCh <- runErr:
			default:
			}
		}
	}()

	rt.logger.Info("api server started", zap.String("addr", sc.Addr))

	return func(ctx context.Context) error {
		wsH.Stop()
		return srv.Shutdown(ctx)
	}, nil
}

// startMaintenanceLoop starts the periodic maintenance loop that cleans up
// expired token blacklist entries and deletes stale execution logs beyond the
// configured retention window. The execution store and retention window are
// sourced from the runtime (populated by startWorkerEngines) and the logging
// config respectively; when no execution store is available (e.g. the worker
// engines were not started) the retention cleanup is skipped while blacklist
// cleanup still runs. It returns a stop function that cancels the loop and
// waits for it to exit.
func startMaintenanceLoop(ctx context.Context, rt *runtime,
	maintenanceInterval time.Duration,
) (stopFunc, error) {
	if maintenanceInterval <= 0 {
		maintenanceInterval = 5 * time.Minute
	}

	blacklistStore := auth.NewBlacklistStore(rt.dbc, rt.cache)
	// RetentionDays controls log file retention; Validate normalizes a
	// non-positive value to the default of 30 before this line runs.
	retentionDays := rt.cfg.Logger.RetentionDays

	maintCtx, maintCancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Add(1)
	// goroutine lifecycle: bound to maintCtx (cancelled by the returned
	// stop function); runMaintenanceLoop selects on ctx.Done and exits;
	// tracked by wg so the stop function can wait for full exit.
	go runMaintenanceLoop(maintCtx, &wg, rt.logger, blacklistStore, rt.schedulerExecStore, retentionDays, maintenanceInterval)

	rt.logger.Info("maintenance loop started",
		zap.Duration("interval", maintenanceInterval),
		zap.Int("log_retention_days", retentionDays),
	)

	return func(ctx context.Context) error {
		maintCancel()
		done := make(chan struct{})
		// goroutine lifecycle: bounded — waits for wg (runMaintenanceLoop)
		// to drain after maintCancel propagates; exits after close(done).
		go func() {
			wg.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-ctx.Done():
			return fmt.Errorf("maintenance loop did not finish: %w", ctx.Err())
		}
		rt.logger.Info("maintenance loop stopped")
		return nil
	}, nil
}

// runMaintenanceLoop periodically executes maintenance sweeps: cleaning up
// expired token blacklist entries and deleting stale execution logs that
// exceed the configured retention window. The loop exits when ctx is
// cancelled.
func runMaintenanceLoop(ctx context.Context, wg *sync.WaitGroup, logger *zap.Logger, blacklistStore auth.BlacklistStore, executionStore task.ExecutionStore, retentionDays int, interval time.Duration) {
	defer wg.Done()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runMaintenanceSweep(ctx, logger, blacklistStore, executionStore, retentionDays)
		}
	}
}

// runMaintenanceSweep performs a single maintenance sweep. It cleans up
// expired token blacklist entries and, when an execution store and a positive
// retention window are configured, deletes execution log records older than
// the retention period. The two cleanups are independent: a failure in one
// does not skip the other.
func runMaintenanceSweep(ctx context.Context, logger *zap.Logger, blacklistStore auth.BlacklistStore, executionStore task.ExecutionStore, retentionDays int) {
	if err := blacklistStore.CleanExpired(ctx); err != nil {
		logger.Error("maintenance: clean expired blacklist tokens", zap.Error(err))
	} else {
		logger.Info("maintenance: cleaned expired blacklist tokens")
	}

	if executionStore != nil && retentionDays > 0 {
		before := time.Now().AddDate(0, 0, -retentionDays)
		if err := executionStore.DeleteExecutionsOlderThan(ctx, before); err != nil {
			logger.Error("maintenance: delete old execution logs", zap.Error(err))
		} else {
			logger.Info("maintenance: deleted old execution logs",
				zap.Int("retention_days", retentionDays),
				zap.Time("before", before),
			)
		}
	}
}
