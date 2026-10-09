// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package telemetry

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/tickraft/tickraft/pkg/asset"
	"github.com/tickraft/tickraft/pkg/event"
	"github.com/tickraft/tickraft/pkg/pool"
	"github.com/tickraft/tickraft/pkg/timewheel"
)

// Compile-time assertion that Engine implements Collector.
var _ Collector = (*Engine)(nil)

// Engine is the core observation engine implementing the architecture:
// Ingest -> Processor -> stateManager -> emitter.
//
// The telemetry is fully decoupled from the scheduler: it does not subscribe
// to task execution events. It only publishes status-change and alert events
// through the event bus. Passive collection channels feed telemetry via
// Submit; active probing is coordinated by the optional ProberService, which
// shares the same processing pipeline.
type Engine struct {
	mu                sync.RWMutex
	processorRegistry *ProcessorRegistry
	store             asset.Store
	bus               event.Bus
	dbc               *gorm.DB
	wheel             timewheel.Wheel
	state             *stateManager
	emitter           *emitter
	logger            *zap.Logger

	// validator validates incoming telemetry before processing. Always non-nil.
	validator *Validator
	// aggregator optionally aggregates metrics over tumbling windows. nil when
	// aggregation is disabled.
	aggregator *Aggregator
	// persistence optionally writes metrics and logs to durable storage. nil
	// when no stores are configured.
	persistence *Persistence

	// telemetryCh is the central channel for all incoming telemetry.
	telemetryCh chan *Telemetry

	// started indicates whether the engine has been started.
	started bool

	// cancel is the root context cancel function.
	cancel context.CancelFunc

	// wg tracks running goroutines for graceful shutdown.
	wg sync.WaitGroup

	// reportPool executes telemetry processing jobs concurrently. It is
	// either injected via WithPool or created internally as a default
	// IO pool.
	reportPool pool.Pool
	// poolOwned indicates whether the engine created reportPool and is
	// responsible for shutting it down on Stop. An injected pool
	// (poolOwned == false) is left untouched.
	poolOwned bool
	// proberSvc coordinates active probing. When non-nil it is started
	// alongside the listener pipeline and stopped in reverse order.
	proberSvc *ProberService
	// listenerRegistry holds passive protocol listeners. When non-nil,
	// all registered ProtocolListeners are started on Engine.Start and
	// stopped on Engine.Stop.
	listenerRegistry *ListenerRegistry
	// monitorStore holds the monitoring-point store. When non-nil, Start
	// bootstraps passive offline detection by registering every asset
	// with an enabled passive point on the timeout wheel.
	monitorStore *MonitorStore
	// protocolListeners is the snapshot of ProtocolListeners started by
	// Start, retained so Stop can stop them in reverse order.
	protocolListeners []ProtocolListener
}

// newEngine creates a new Engine with the given options.
//
// Returns an error when no asset store is configured — the engine cannot
// track asset status without one, so a missing store is a wiring defect
// that must fail at startup instead of surfacing as lost status updates.
// It also returns an error if the internal time wheel or default telemetry
// pool cannot be initialized. These paths are unreachable in practice
// because the wheel's worker count and the IO pool's size are both
// sanitized to positive values, but the error is returned rather than
// panicking to honor the "no panic in business logic" rule.
func newEngine(options ...Option) (*Engine, error) {
	opts := &Options{}
	for _, o := range options {
		o.apply(opts)
	}

	if opts.AssetStore == nil {
		return nil, fmt.Errorf("telemetry: asset store is required, configure it with WithAssetStore")
	}

	logger := opts.Logger
	if logger == nil {
		logger = zap.NewNop()
	}

	bus := opts.Bus
	if bus == nil {
		bus = event.NewBus()
	}

	wheel, err := timewheel.NewWheel(100)
	if err != nil {
		return nil, fmt.Errorf("telemetry: create time wheel: %w", err)
	}

	m := &Engine{
		processorRegistry: opts.ProcessorRegistry,
		store:             opts.AssetStore,
		bus:               bus,
		dbc:               opts.DB,
		wheel:             wheel,
		logger:            logger,
		telemetryCh:       make(chan *Telemetry, 1024),
		validator:         NewValidator(opts.AssetStore, logger),
		proberSvc:         opts.ProberService,
		listenerRegistry:  opts.ListenerRegistry,
		monitorStore:      opts.MonitorStore,
	}

	m.state = newStateManager(m.store, m.dbc, m.wheel, m.logger, m.handleTimeout)
	m.emitter = newEmitter(m.bus, m.logger)

	// Configure aggregation when a positive window is provided.
	if opts.AggregationWindow > 0 {
		m.aggregator = NewAggregator(opts.AggregationWindow, logger)
	}

	// Configure persistence: explicit injection takes precedence, otherwise
	// auto-create when both stores are provided.
	if opts.Persistence != nil {
		m.persistence = opts.Persistence
	} else if opts.MetricStore != nil && opts.LogStore != nil {
		m.persistence = NewPersistence(opts.MetricStore, opts.LogStore, logger)
	}

	// Configure the report processing pool. An injected pool takes
	// precedence and its lifecycle is owned by the caller. When no pool
	// is injected a default IO pool is created and the engine owns its
	// lifecycle, shutting it down on Stop.
	if opts.Pool != nil {
		m.reportPool = opts.Pool
		m.poolOwned = false
	} else {
		reportPool, err := pool.NewIOPool(runtime.NumCPU())
		if err != nil {
			return nil, fmt.Errorf("telemetry: create default telemetry pool: %w", err)
		}
		m.reportPool = reportPool
		m.poolOwned = true
	}

	return m, nil
}

// Start begins all listeners, the time wheel, and the telemetry processing loop.
func (m *Engine) Start(ctx context.Context) error {
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return nil
	}

	ctx, cancel := context.WithCancel(ctx)
	m.cancel = cancel
	m.started = true
	m.mu.Unlock()

	// Start the time wheel.
	m.wg.Add(1)
	// goroutine lifecycle: bound to ctx (cancelled by Engine.Stop via
	// m.cancel); tracked by m.wg so Stop can wait for full shutdown.
	go func() {
		defer m.wg.Done()
		m.wheel.Start(ctx)
	}()

	// Start the telemetry processing loop.
	m.wg.Add(1)
	// goroutine lifecycle: bound to ctx (cancelled by Engine.Stop via
	// m.cancel); processLoop selects on ctx.Done and exits; superviseLoop
	// restarts it after a panic so ingestion cannot silently stop; tracked
	// by m.wg.
	go func() {
		defer m.wg.Done()
		superviseLoop(ctx, m.logger, "process loop", loopRestartBackoff, m.processLoop)
	}()

	// Start the aggregator and a consumer goroutine that persists flushed
	// aggregated metrics.
	if m.aggregator != nil {
		m.aggregator.Start(ctx)
		m.wg.Add(1)
		// goroutine lifecycle: bound to ctx (cancelled by Engine.Stop);
		// consumeAggregated selects on ctx.Done and exits; superviseLoop
		// restarts it after a panic; tracked by m.wg.
		go func() {
			defer m.wg.Done()
			superviseLoop(ctx, m.logger, "aggregated consumer", loopRestartBackoff, m.consumeAggregated)
		}()
	}

	// Start the active prober service when injected. It coordinates
	// scheduled active probing on top of the shared scheduler engine.
	if m.proberSvc != nil {
		if err := m.proberSvc.Start(ctx); err != nil {
			m.logger.Error("prober service start failed", zap.Error(err))
		}
	}

	// Start all registered ProtocolListeners (Syslog, SNMP, MQTT, etc.).
	// Each listener receives an ingest callback that feeds received
	// telemetry into the same pipeline as webhook data.
	if m.listenerRegistry != nil {
		listeners := m.listenerRegistry.ListProtocol()
		m.protocolListeners = make([]ProtocolListener, 0, len(listeners))
		ingest := func(_ context.Context, t *Telemetry) { m.Submit(t) }
		for _, l := range listeners {
			if err := l.Start(ctx, ingest); err != nil {
				m.logger.Error("protocol listener start failed",
					zap.String("type", l.Type()),
					zap.Error(err),
				)
				continue
			}
			m.protocolListeners = append(m.protocolListeners, l)
			m.logger.Info("protocol listener started",
				zap.String("type", l.Type()),
			)
		}
	}

	// Bootstrap passive offline detection: register every asset with an
	// enabled passive point on the timeout wheel so a silent asset is
	// declared offline even if no report ever arrives after a restart.
	m.registerPassiveTimeouts(ctx)

	m.logger.Info("telemetry engine started")
	return nil
}

// registerPassiveTimeouts seeds the timeout wheel for every asset that has
// at least one enabled passive monitoring point, using the most lenient
// heartbeat threshold among those points. It runs once on Start; point CRUD
// keeps the registrations current via SyncAssetObservation. Without a
// MonitorStore the engine keeps its lazy behavior: registration happens on
// the asset's first report (UpdateActive auto-registers).
func (m *Engine) registerPassiveTimeouts(ctx context.Context) {
	if m.monitorStore == nil {
		return
	}
	points, err := m.monitorStore.ListPassive(ctx)
	if err != nil {
		m.logger.Error("offline detection bootstrap: list passive points failed", zap.Error(err))
		return
	}
	timeouts := make(map[int64]time.Duration)
	for i := range points {
		p := &points[i]
		if !p.Enabled || p.AssetID <= 0 {
			continue
		}
		if t := HeartbeatTimeout(*p); t > timeouts[p.AssetID] {
			timeouts[p.AssetID] = t
		}
	}
	for assetID, timeout := range timeouts {
		if err := m.RegisterAsset(ctx, Config{AssetID: assetID, Timeout: int(timeout / time.Second)}); err != nil {
			m.logger.Warn("offline detection bootstrap: register asset failed",
				zap.Int64("asset_id", assetID),
				zap.Error(err),
			)
		}
	}
	if len(timeouts) > 0 {
		m.logger.Info("offline detection bootstrapped", zap.Int("assets", len(timeouts)))
	}
}

// Stop gracefully stops all components in reverse start order.
//
// A caller context that is already cancelled or expires mid-shutdown makes
// the per-component Stop calls return their timeout errors (logged, not
// propagated); the underlying goroutines still terminate because every
// component's internal context is cancelled here, so shutdown completes
// on its own even after Stop has returned.
func (m *Engine) Stop(ctx context.Context) error {
	m.mu.Lock()
	if !m.started {
		m.mu.Unlock()
		return nil
	}
	m.started = false
	m.mu.Unlock()

	// Stop the active prober service in reverse start order.
	if m.proberSvc != nil {
		if stopErr := m.proberSvc.Stop(ctx); stopErr != nil {
			m.logger.Error("failed to stop prober service", zap.Error(stopErr))
		}
	}

	// Stop all started ProtocolListeners in reverse start order so the
	// ingest pipeline drains before the root context is cancelled.
	for i := len(m.protocolListeners) - 1; i >= 0; i-- {
		l := m.protocolListeners[i]
		if stopErr := l.Stop(ctx); stopErr != nil {
			m.logger.Error("failed to stop protocol listener",
				zap.String("type", l.Type()),
				zap.Error(stopErr),
			)
		}
	}
	m.protocolListeners = nil

	// Stop the aggregator before cancelling the root context. This flushes
	// remaining buffered metrics while the consumer goroutine is still alive
	// to drain the flush channel and persist them.
	if m.aggregator != nil {
		if stopErr := m.aggregator.Stop(ctx); stopErr != nil {
			m.logger.Error("failed to stop aggregator", zap.Error(stopErr))
		}
	}

	// Cancel the root context to signal all remaining goroutines.
	if m.cancel != nil {
		m.cancel()
	}

	// Stop the time wheel.
	if stopErr := m.wheel.Stop(ctx); stopErr != nil {
		m.logger.Error("failed to stop time wheel", zap.Error(stopErr))
	}

	// Wait for all goroutines to finish.
	done := make(chan struct{})
	// goroutine lifecycle: bounded — waits for m.wg to drain after the
	// processLoop, aggregator consumer, and time wheel goroutines observe
	// ctx cancellation; exits after close(done).
	go func() {
		m.wg.Wait()
		close(done)
	}()

	// shutdownPool releases the default pool when the engine owns it.
	// It is called in both the graceful and timeout branches so workers
	// are never leaked. An injected pool (poolOwned == false) is left
	// untouched; its lifecycle is the caller's responsibility.
	shutdownPool := func() {
		if !m.poolOwned || m.reportPool == nil {
			return
		}
		if poolErr := m.reportPool.Shutdown(ctx); poolErr != nil {
			m.logger.Error("failed to shutdown default telemetry pool", zap.Error(poolErr))
		}
	}

	select {
	case <-done:
		shutdownPool()
		m.logger.Info("telemetry engine stopped")
		return nil
	case <-ctx.Done():
		shutdownPool()
		return fmt.Errorf("telemetry engine stop timeout: %w", ctx.Err())
	}
}

// RegisterAsset registers an asset for observation.
func (m *Engine) RegisterAsset(_ context.Context, config Config) error {
	if config.AssetID <= 0 {
		return fmt.Errorf("%w: asset_id must be positive", ErrInvalidConfig)
	}
	if config.Timeout <= 0 {
		return fmt.Errorf("%w: timeout must be positive", ErrInvalidConfig)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Register timeout detection in the state manager.
	timeout := time.Duration(config.Timeout) * time.Second
	m.state.RegisterAsset(config.AssetID, timeout)

	m.logger.Info("asset registered for observation",
		zap.Int64("asset_id", config.AssetID),
		zap.Int("timeout", config.Timeout),
	)
	return nil
}

// UnregisterAsset removes an asset from observation.
func (m *Engine) UnregisterAsset(ctx context.Context, assetID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Remove from state manager.
	m.state.UnregisterAsset(assetID)

	// Invalidate any cached validator entry so subsequent telemetry for this
	// asset re-fetch from the store instead of serving stale data.
	if m.validator != nil {
		m.validator.InvalidateAsset(ctx, assetID)
	}

	m.logger.Info("asset unregistered from observation",
		zap.Int64("asset_id", assetID),
	)
	return nil
}

// Submit submits a telemetry to the processing channel.
// This is used by external collectors (e.g. the HTTP webhook listener) to
// feed data into the engine.
func (m *Engine) Submit(t *Telemetry) {
	select {
	case m.telemetryCh <- t:
	default:
		m.logger.Warn("telemetry channel full, dropping telemetry",
			zap.Int64("asset_id", t.AssetID),
		)
	}
}

// recoverPanic is the panic-isolation helper for inline fallback paths. It
// logs the panic value and stack via zap so a single report failure does
// not crash the calling goroutine. Long-lived loops use superviseLoop,
// which recovers and restarts.
func (m *Engine) recoverPanic(scope string) {
	if r := recover(); r != nil {
		m.logger.Error("telemetry goroutine panicked",
			zap.String("scope", scope),
			zap.Any("panic", r),
			zap.Stack("stack"),
		)
	}
}

// loopRestartBackoff is the delay before superviseLoop relaunches a
// recovered loop, bounding restart churn while keeping outage windows short.
const loopRestartBackoff = time.Second

// superviseLoop runs fn until ctx is cancelled, restarting it with a backoff
// after a panic or an unexpected early return. Without the restart a single
// recovered panic would permanently stop the loop while the engine keeps
// accepting (and eventually dropping) telemetry.
func superviseLoop(
	ctx context.Context, logger *zap.Logger, scope string, backoff time.Duration, fn func(context.Context),
) {
	for {
		func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Error("telemetry goroutine panicked",
						zap.String("scope", scope),
						zap.Any("panic", r),
						zap.Stack("stack"),
					)
				}
			}()
			fn(ctx)
		}()
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		logger.Warn("telemetry goroutine restarting after recovery",
			zap.String("scope", scope),
		)
	}
}
