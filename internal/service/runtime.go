// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package service orchestrates the long-lived components of a tickraft
// process: the API server, worker engines (executor + scheduler + telemetry),
// prism, and the maintenance loop.
//
// The runtime runs all components in a single process via Start.
// The callers implements its own service package and does
// not import this package.
//
// This package must not depend on cobra: command-line flag parsing stays in
// the cli package, which resolves a *config.Config and hands it to Start.
package service

import (
	"context"
	"fmt"
	"os"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gorm.io/gorm"

	"github.com/tickraft/tickraft/internal/quota"
	"github.com/tickraft/tickraft/pkg/asset"
	"github.com/tickraft/tickraft/pkg/auth"
	"github.com/tickraft/tickraft/pkg/auth/jwt"
	"github.com/tickraft/tickraft/pkg/cache"
	"github.com/tickraft/tickraft/pkg/config"
	"github.com/tickraft/tickraft/pkg/db"
	"github.com/tickraft/tickraft/pkg/errdefs"
	"github.com/tickraft/tickraft/pkg/event"
	"github.com/tickraft/tickraft/pkg/i18n"
	"github.com/tickraft/tickraft/pkg/prism"
	"github.com/tickraft/tickraft/pkg/task"
	"github.com/tickraft/tickraft/pkg/telemetry"
	"github.com/tickraft/tickraft/pkg/user"
)

// defaultShutdownTimeout is the maximum time allowed for graceful shutdown
// of all components after a SIGINT/SIGTERM is received.
const defaultShutdownTimeout = 10 * time.Second

// stopFunc gracefully stops a started component. It is returned by the
// component starter helpers and invoked in reverse order during shutdown.
type stopFunc func(ctx context.Context) error

// account is the minimal in-memory representation of the implicit admin
// account. It is never persisted: the runtime runs as a single admin user
// who is treated as the owner of an implicit personal account (Type=1).
// callers replace this with database-backed Account rows loaded
// via the extended model package.
//
// This struct intentionally omits TenantID and other extended fields
// (billing, subscription, quotas) to keep the standalone runtime free of
// extended concerns. The full Account definition lives exclusively in the
// callers's internal/model package.
type account struct {
	ID        int64
	OwnerID   int64
	Type      int
	Name      string
	Status    int
	CreatedAt time.Time
	UpdatedAt time.Time
}

// runtime holds shared, long-lived resources initialized once per process.
// It is created by initRuntime and released by close.
//
// The fields are unexported; the Start path treats the runtime as an
// opaque internal value.
type runtime struct {
	cfg    *config.Config
	logger *zap.Logger
	dbc    *gorm.DB
	cache  *cache.LRUCache
	bus    event.Bus
	authz  *auth.Service
	jwt    *jwt.JWT

	// assetStore is the asset persistence store, created once and
	// shared between the telemetry manager, asset management API, and
	// the asset-key middleware getter.
	assetStore asset.Store

	// Alert-related resources: the prismEngine is created by
	// startPrismEngine and stores are accessed via accessor methods
	// (RuleStore, RecordStore, ChannelStore, RemediationStore, RuleEngine).
	prismEngine *prism.Engine

	// Scheduler-related resources created by startWorkerEngines and consumed
	// by startAPIServer when wiring the task service into the API routes.
	// The engine drives task scheduling while the task and execution stores
	// persist configuration and history across process restarts.
	schedulerEngine    task.Manager
	schedulerTaskStore task.Store
	schedulerExecStore task.ExecutionStore

	// i18nRegistry holds the locale bundles used for alert message rendering
	// and the /api/v1/i18n/locales endpoint. The runtime loads builtin
	// locales (zh-Hans, en-US) from the embedded asset filesystem; the extended
	// edition merges additional locales (zh-Hant, en-GB, ar, ja, de, fr, es,
	// ru, ko) at startup via Registry.Register.
	i18nRegistry i18n.Registry

	// telemetryCollector is the telemetry engine started by startWorkerEngines.
	// It is stored on the runtime so startAPIServer can wire the webhook
	// listener's ingest callback to the collector's Submit method, enabling
	// the POST /api/v1/telemetry endpoint to forward received telemetry into
	// the processing pipeline. It is nil when the worker engines were not
	// started (e.g. isolated tests).
	telemetryCollector telemetry.Collector

	// metricStore and logStore persist collected metrics and logs. They are
	// created by startWorkerEngines and consumed by startAPIServer to wire
	// the telemetry handler's history/logs endpoints.
	metricStore telemetry.MetricStore
	logStore    telemetry.LogStore

	// proberSvc coordinates active probing. It is created by
	// startWorkerEngines and consumed by startAPIServer to wire
	// register/unregister hooks into the telemetry CRUD service so that
	// active monitoring points are scheduled in real time.
	proberSvc *telemetry.ProberService

	// implicitAccount is the in-memory account that backs the built-in admin
	// user. It is never persisted to the database: the runtime has a single
	// admin user who is treated as the owner of an implicit personal account
	// (Type=1). callers replace this with real Account rows from
	// the database.
	implicitAccount *account
}

// ImplicitAccount returns the in-memory implicit account used by the
// runtime. It is always non-nil after a successful initRuntime. The
// returned pointer is shared and must not be mutated by the caller.
func (r *runtime) ImplicitAccount() *account {
	return r.implicitAccount
}

// eventBus returns the shared event bus, creating it lazily on first use.
// The bus is configured with a GORM-backed FailedEventStore so events that
// exhaust all retries are persisted to the database for audit and replay.
func (r *runtime) eventBus() event.Bus {
	if r.bus == nil {
		failedStore := event.NewFailedEventStore(r.dbc)
		if err := failedStore.Migrate(context.Background()); err != nil {
			r.logger.Error("migrate failed event store", zap.Error(err))
		}
		r.bus = event.NewBus(
			event.WithFailedEventStore(failedStore),
			event.WithLogger(r.logger),
		)
	}
	return r.bus
}

// close releases all shared resources held by the runtime in reverse order:
// auth service, event bus, cache, then the database connection.
func (r *runtime) close() {
	if r.authz != nil {
		r.authz.Close()
	}
	if r.bus != nil {
		if closeErr := r.bus.Close(); closeErr != nil {
			r.logger.Error("close event bus", zap.Error(closeErr))
		}
	}
	if r.cache != nil {
		if closeErr := r.cache.Close(context.Background()); closeErr != nil {
			r.logger.Error("close cache", zap.Error(closeErr))
		}
	}
	if r.dbc != nil {
		if sqlDB, err := r.dbc.DB(); err == nil {
			if closeErr := sqlDB.Close(); closeErr != nil {
				r.logger.Error("close database", zap.Error(closeErr))
			}
		} else {
			r.logger.Error("get underlying sql.DB for close", zap.Error(err))
		}
	}
}

// initRuntime creates the logger, opens the database, runs core migrations,
// and returns a runtime bundling the shared resources. The event bus and
// auth services are initialized lazily by the components that need them.
func initRuntime(ctx context.Context, cfg *config.Config) (*runtime, error) {
	// Logger mode ("debug" or "release") determines zap logger configuration;
	// the --log-mode CLI flag can override the config value at startup.
	logger, err := newLogger(cfg.Logger.Mode)
	if err != nil {
		return nil, fmt.Errorf("init logger: %w", err)
	}
	// ignored because: zap.Sync flushes buffered log entries; on
	// stderr/stdout it is a no-op on most platforms and a failure here is
	// not actionable since logging is already initialized. This is the
	// zap-recommended shutdown pattern.
	defer func() { _ = logger.Sync() }()
	zap.ReplaceGlobals(logger)

	// Register the CE default quota Provider before any component that
	// may query quota ceilings is initialized.
	quota.Register()

	// Resolve the database configuration via ResolveDBConfig so both the DSN
	// path (database.dsn) and the direct-fields path (database.driver,
	// database.address, database.credential, database.params) are supported.
	// When DSN is non-empty it takes precedence and is parsed via db.Parse;
	// otherwise the embedded db.Config fields are used as-is.
	dbCfg, err := cfg.Database.ResolveDBConfig()
	if err != nil {
		return nil, fmt.Errorf("resolve database config: %w", err)
	}
	dbc, err := db.Open(ctx, dbCfg)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	cacheInst := cache.NewLRU(1024, 5*time.Minute)

	if err = db.AutoMigrate(ctx, dbc); err != nil {
		closeRuntimeDB(dbc, cacheInst)
		return nil, fmt.Errorf("auto migrate: %w", err)
	}

	// Ensure the built-in admin user exists.
	adminPassword, err := db.EnsureAdminUser(ctx, dbc, cfg.Auth.AdminUsername, cfg.Auth.AdminPassword)
	if err != nil {
		closeRuntimeDB(dbc, cacheInst)
		return nil, fmt.Errorf("ensure admin user: %w", err)
	}
	if adminPassword != "" {
		// Print the initial password to stderr so the operator can
		// see it in the terminal or systemd journal without it being
		// persisted in structured log files.
		fmt.Fprintf(os.Stderr,
			"[security] Generated random admin password for user %q: %s\n",
			cfg.Auth.AdminUsername, adminPassword)
		fmt.Fprintln(os.Stderr, "[security] Please change it immediately after first login.")
		logger.Warn("generated random admin password; change it after first login",
			zap.String("username", cfg.Auth.AdminUsername),
		)
	}

	// Construct the implicit in-memory account that backs the admin user.
	// The admin user (ID=1) is treated as the owner of a personal account
	// (Type=1). This object is never persisted: callers may replace it
	// with database-backed Account rows loaded via the callers
	// AccountStore.
	implicitAccount := &account{
		ID:      1,
		OwnerID: 1,
		Type:    1,
		Name:    cfg.Auth.AdminUsername,
		Status:  1,
	}

	// Create the asset store early so it can be shared between the
	// telemetry manager, asset management API, and asset-key
	// middleware getter. The asset table migration is idempotent.
	assetStore := asset.NewStore(dbc)
	if err = assetStore.Migrate(ctx); err != nil {
		closeRuntimeDB(dbc, cacheInst)
		return nil, fmt.Errorf("migrate asset store: %w", err)
	}

	// Initialize the i18n Registry with builtin locale bundles. The
	// callers merges additional locales on top of these at
	// startup. A load failure is non-fatal: the Registry falls back
	// to an empty default-locale bundle so alerts still render.
	i18nRegistry := i18n.NewRegistry(logger)
	i18nLoader := i18n.NewLoader(logger)
	if err = i18nLoader.LoadToRegistry(i18n.EmbeddedFS(), i18nRegistry); err != nil {
		logger.Warn("init runtime: failed to load i18n resources, falling back to default locale",
			zap.Error(err))
	}

	return &runtime{
		cfg:             cfg,
		logger:          logger,
		dbc:             dbc,
		cache:           cacheInst,
		assetStore:      assetStore,
		i18nRegistry:    i18nRegistry,
		implicitAccount: implicitAccount,
	}, nil
}

// closeRuntimeDB closes the gorm.DB and cache instances. It is used to clean
// up partially initialized resources when initRuntime fails after opening
// the database. Errors are logged via the global zap logger (set up before
// this helper is ever called) rather than propagated, since the caller is
// already on an error path.
func closeRuntimeDB(dbc *gorm.DB, cacheInst *cache.LRUCache) {
	if cacheInst != nil {
		if closeErr := cacheInst.Close(context.Background()); closeErr != nil {
			zap.L().Warn("close cache on runtime init failure", zap.Error(closeErr))
		}
	}
	if dbc != nil {
		if sqlDB, err := dbc.DB(); err == nil {
			if closeErr := sqlDB.Close(); closeErr != nil {
				zap.L().Warn("close database on runtime init failure", zap.Error(closeErr))
			}
		} else {
			zap.L().Warn("get underlying sql.DB on runtime init failure", zap.Error(err))
		}
	}
}

// initAuth initializes the JWT manager and auth service. The auth services
// are stored on the runtime.
func initAuth(_ context.Context, rt *runtime) error {
	jwtSecret := rt.cfg.Auth.JWTSecret
	if jwtSecret == "" {
		jwtSecret = os.Getenv("TICKRAFT_JWT_SECRET")
	}
	if jwtSecret == "" {
		return fmt.Errorf("auth.jwt_secret is required (set auth.jwt_secret in config or TICKRAFT_JWT_SECRET env var): %w", errdefs.ErrInvalidArgument)
	}

	blacklistStore := auth.NewBlacklistStore(rt.dbc, rt.cache)
	blacklistChecker := func(jti string) (bool, error) {
		return blacklistStore.Exists(context.Background(), jti)
	}
	jwtMgr, err := jwt.New(jwt.Config{
		Secret:        jwtSecret,
		AccessExpire:  0, // use default 2h
		RefreshExpire: 0, // use default 7d
		Issuer:        "tickraft",
	}, blacklistChecker)
	if err != nil {
		return fmt.Errorf("init jwt manager: %w", err)
	}

	userStore := user.NewStore(rt.dbc, rt.cache)
	apiKeyStore := user.NewAPIKeyStore(rt.dbc, rt.cache)
	authz := auth.NewService(jwtMgr, userStore, apiKeyStore, blacklistStore)

	rt.jwt = jwtMgr
	rt.authz = authz
	return nil
}

// newLogger creates a zap.Logger configured for the given mode.
// Both modes only add stacktraces at ErrorLevel and above — Warn-level
// logs (i18n fallbacks, event publish failures, etc.) are expected
// operational conditions that do not benefit from stack traces.
func newLogger(mode string) (*zap.Logger, error) {
	level := zapcore.InfoLevel
	encoderConfig := zap.NewProductionEncoderConfig()
	if mode != "release" {
		level = zapcore.DebugLevel
		encoderConfig = zap.NewDevelopmentEncoderConfig()
		encoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	}

	core := zapcore.NewCore(
		zapcore.NewConsoleEncoder(encoderConfig),
		zapcore.AddSync(os.Stderr),
		level,
	)

	return zap.New(core, zap.AddStacktrace(zapcore.ErrorLevel)), nil
}
