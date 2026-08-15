// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package telemetry

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"time"

	"github.com/tickraft/tickraft/pkg/asset"
	"github.com/tickraft/tickraft/pkg/event"
	"github.com/tickraft/tickraft/pkg/pool"
	"github.com/tickraft/tickraft/pkg/timewheel"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// Compile-time assertion that Manager implements Collector.
var _ Collector = (*Manager)(nil)

// Manager is the core observation engine implementing the architecture:
// Ingest -> Processor -> stateManager -> emitter.
//
// The telemetry is fully decoupled from the scheduler: it does not subscribe
// to task execution events. It only publishes status-change and alert events
// through the event bus. Passive collection channels feed telemetry via
// Submit; active probing is coordinated by the optional ProberService, which
// shares the same processing pipeline.
type Manager struct {
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

	// started indicates whether the manager has been started.
	started bool

	// cancel is the root context cancel function.
	cancel context.CancelFunc

	// wg tracks running goroutines for graceful shutdown.
	wg sync.WaitGroup

	// reportPool executes telemetry processing jobs concurrently. It is
	// either injected via WithPool or created internally as a default
	// IO pool.
	reportPool pool.Pool
	// poolOwned indicates whether the manager created reportPool and is
	// responsible for shutting it down on Stop. An injected pool
	// (poolOwned == false) is left untouched.
	poolOwned bool
	// proberSvc coordinates active probing. When non-nil it is started
	// alongside the listener pipeline and stopped in reverse order.
	proberSvc *ProberService
	// listenerRegistry holds passive telemetry listeners. When non-nil,
	// all registered ProtocolListeners are started on Manager.Start and
	// stopped on Manager.Stop. HTTPListeners are looked up by the API
	// router layer to mount their handlers.
	listenerRegistry *ListenerRegistry
	// protocolListeners is the snapshot of ProtocolListeners started by
	// Start, retained so Stop can stop them in reverse order.
	protocolListeners []ProtocolListener
}

// newManager creates a new Manager with the given options.
//
// Returns an error if the internal time wheel or default telemetry pool
// cannot be initialized. These paths are unreachable in practice
// because the wheel's worker count and the IO pool's size are both
// sanitized to positive values, but the error is returned rather than
// panicking to honor the "no panic in business logic" rule.
func newManager(opts ...Option) (*Manager, error) {
	o := &Options{}
	for _, opt := range opts {
		opt.apply(o)
	}

	logger := o.Logger
	if logger == nil {
		logger = zap.NewNop()
	}

	bus := o.Bus
	if bus == nil {
		bus = event.NewBus()
	}

	wheel, err := timewheel.NewWheel(100)
	if err != nil {
		return nil, fmt.Errorf("telemetry: create time wheel: %w", err)
	}

	m := &Manager{
		processorRegistry: o.ProcessorRegistry,
		store:             o.AssetStore,
		bus:               bus,
		dbc:               o.DB,
		wheel:             wheel,
		logger:            logger,
		telemetryCh:       make(chan *Telemetry, 1024),
		validator:         NewValidator(o.AssetStore, logger),
		proberSvc:         o.ProberService,
		listenerRegistry:  o.ListenerRegistry,
	}

	m.state = newStateManager(m.store, m.dbc, m.wheel, m.logger, m.handleTimeout)
	m.emitter = newEmitter(m.bus, m.logger)

	// Configure aggregation when a positive window is provided.
	if o.AggregationWindow > 0 {
		m.aggregator = NewAggregator(o.AggregationWindow, logger)
	}

	// Configure persistence: explicit injection takes precedence, otherwise
	// auto-create when both stores are provided.
	if o.Persistence != nil {
		m.persistence = o.Persistence
	} else if o.MetricStore != nil && o.LogStore != nil {
		m.persistence = NewPersistence(o.MetricStore, o.LogStore, logger)
	}

	// Configure the report processing pool. An injected pool takes
	// precedence and its lifecycle is owned by the caller. When no pool
	// is injected a default IO pool is created and the manager owns its
	// lifecycle, shutting it down on Stop.
	if o.Pool != nil {
		m.reportPool = o.Pool
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
func (m *Manager) Start(ctx context.Context) error {
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
	// goroutine lifecycle: bound to ctx (cancelled by Manager.Stop via
	// m.cancel); tracked by m.wg so Stop can wait for full shutdown.
	go func() {
		defer m.wg.Done()
		m.wheel.Start(ctx)
	}()

	// Start the telemetry processing loop.
	m.wg.Add(1)
	// goroutine lifecycle: bound to ctx (cancelled by Manager.Stop);
	// processLoop selects on ctx.Done and exits; tracked by m.wg.
	go func() {
		defer m.wg.Done()
		defer m.recoverPanic("process loop")
		m.processLoop(ctx)
	}()

	// Start the aggregator and a consumer goroutine that persists flushed
	// aggregated metrics.
	if m.aggregator != nil {
		m.aggregator.Start(ctx)
		m.wg.Add(1)
		// goroutine lifecycle: bound to ctx (cancelled by Manager.Stop);
		// consumeAggregated selects on ctx.Done and exits; tracked by m.wg.
		go func() {
			defer m.wg.Done()
			defer m.recoverPanic("aggregated consumer")
			m.consumeAggregated(ctx)
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
	// telemetry into the same pipeline as webhook data. HTTPListeners are
	// not started here — they are stateless handler providers mounted by
	// the API router layer.
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

	m.logger.Info("telemetry manager started")
	return nil
}

// Stop gracefully stops all components.
func (m *Manager) Stop(ctx context.Context) error {
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

	// shutdownPool releases the default pool when the manager owns it.
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
		m.logger.Info("telemetry manager stopped")
		return nil
	case <-ctx.Done():
		shutdownPool()
		return fmt.Errorf("telemetry manager stop timeout: %w", ctx.Err())
	}
}

// RegisterAsset registers an asset for observation.
func (m *Manager) RegisterAsset(ctx context.Context, config Config) error {
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

	// Persist the collect configuration. The AssetType is resolved from the
	// asset store when available so the not-null column is populated; the
	// CollectType and ProbeInterval columns are left to their defaults
	// (minimal config does not carry them).
	if m.dbc != nil {
		model := &CollectionConfig{
			AssetID:       config.AssetID,
			CollectConfig: config.CollectConfig,
			Timeout:       config.Timeout,
			Enable:        true,
		}
		if m.store != nil {
			if a, err := m.store.GetByID(ctx, config.AssetID); err == nil && a != nil {
				model.TenantID = a.TenantID
				model.AssetType = string(a.AssetType)
			}
		}
		if err := m.dbc.WithContext(ctx).Save(model).Error; err != nil {
			return fmt.Errorf("persist collect config: %w", err)
		}
	}

	m.logger.Info("asset registered for observation",
		zap.Int64("asset_id", config.AssetID),
		zap.Int("timeout", config.Timeout),
	)
	return nil
}

// UnregisterAsset removes an asset from observation.
func (m *Manager) UnregisterAsset(ctx context.Context, assetID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Remove from state manager.
	m.state.UnregisterAsset(assetID)

	// Invalidate any cached validator entry so subsequent telemetry for this
	// asset re-fetch from the store instead of serving stale data.
	if m.validator != nil {
		m.validator.InvalidateAsset(ctx, assetID)
	}

	// Remove from database.
	if m.dbc != nil {
		if err := m.dbc.WithContext(ctx).Where("asset_id = ?", assetID).Delete(&CollectionConfig{}).Error; err != nil {
			m.logger.Error("failed to delete collect config", zap.Int64("asset_id", assetID), zap.Error(err))
		}
	}

	m.logger.Info("asset unregistered from observation",
		zap.Int64("asset_id", assetID),
	)
	return nil
}

// Submit submits a telemetry to the processing channel.
// This is used by external collectors (e.g. the HTTP webhook listener) to
// feed data into the manager.
func (m *Manager) Submit(t *Telemetry) {
	select {
	case m.telemetryCh <- t:
	default:
		m.logger.Warn("telemetry channel full, dropping telemetry",
			zap.Int64("asset_id", t.AssetID),
		)
	}
}

// recoverPanic is the shared panic-isolation helper for telemetry-managed
// goroutines. It logs the panic value and stack via zap so a single
// goroutine failure does not crash the whole process and the manager can
// keep serving other telemetry streams.
func (m *Manager) recoverPanic(scope string) {
	if r := recover(); r != nil {
		m.logger.Error("telemetry goroutine panicked",
			zap.String("scope", scope),
			zap.Any("panic", r),
			zap.Stack("stack"),
		)
	}
}
