// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package telemetry

import (
	"context"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/tickraft/tickraft/pkg/asset"
	"github.com/tickraft/tickraft/pkg/event"
	"github.com/tickraft/tickraft/pkg/pool"
)

// Collector is the collection engine interface.
type Collector interface {
	// Start begins the telemetry processing loop.
	Start(ctx context.Context) error
	// Stop gracefully stops all components.
	Stop(ctx context.Context) error
	// RegisterAsset registers an asset for observation.
	RegisterAsset(ctx context.Context, config Config) error
	// UnregisterAsset removes an asset from observation.
	UnregisterAsset(ctx context.Context, assetID int64) error
	// Submit submits a telemetry to the processing channel.
	Submit(t *Telemetry)
}

// Config holds the collection configuration for an asset.
type Config struct {
	// AssetID is the ID of the asset to observe.
	AssetID int64
	// CollectConfig is the JSON-encoded collection configuration.
	CollectConfig string
	// Timeout is the offline detection threshold in seconds.
	Timeout int
}

// Option configures a telemetry.
type Option interface {
	apply(*Options)
}

// Options holds the configuration for constructing a telemetry.
type Options struct {
	ProcessorRegistry *ProcessorRegistry
	AssetStore        asset.Store
	Bus               event.Bus
	DB                *gorm.DB
	Logger            *zap.Logger

	// AggregationWindow configures the metric tumbling window. A non-positive
	// value disables aggregation.
	AggregationWindow time.Duration
	// MetricStore persists aggregated metric data points.
	MetricStore MetricStore
	// LogStore persists collected log entries.
	LogStore LogStore
	// Persistence is an explicit persistence layer. When set it takes
	// precedence over MetricStore/LogStore.
	Persistence *Persistence
	// Pool injects a worker pool for concurrent telemetry processing. When
	// nil, the manager creates a default IO pool sized to
	// runtime.NumCPU and owns its lifecycle. An injected pool is not
	// shut down by the manager; the caller remains responsible for it.
	Pool pool.Pool
	// ProberService injects the active probing coordinator. When set,
	// the Manager starts and stops it alongside the listener pipeline.
	// When nil, active probing is disabled and the Manager only runs
	// the passive listener pipeline.
	ProberService *ProberService
	// ListenerRegistry holds the passive telemetry listeners (HTTP and
	// protocol listeners). When set, the Manager starts all registered
	// ProtocolListeners on Start and stops them on Stop. HTTPListeners
	// are looked up by the API router layer to mount their handlers.
	// When nil, no protocol listeners are managed by the Manager.
	ListenerRegistry *ListenerRegistry
}

// processorRegistryOption sets the processor registry.
type processorRegistryOption struct {
	registry *ProcessorRegistry
}

func (o processorRegistryOption) apply(opts *Options) { opts.ProcessorRegistry = o.registry }

// WithProcessorRegistry sets the processor registry.
func WithProcessorRegistry(registry *ProcessorRegistry) Option {
	return processorRegistryOption{registry: registry}
}

// assetStoreOption sets the asset persistence store.
type assetStoreOption struct {
	store asset.Store
}

func (o assetStoreOption) apply(opts *Options) { opts.AssetStore = o.store }

// WithAssetStore sets the asset persistence store.
func WithAssetStore(store asset.Store) Option { return assetStoreOption{store: store} }

// eventBusOption sets the event bus for event publishing.
type eventBusOption struct {
	bus event.Bus
}

func (o eventBusOption) apply(opts *Options) { opts.Bus = o.bus }

// WithEventBus sets the event bus for event publishing.
func WithEventBus(bus event.Bus) Option { return eventBusOption{bus: bus} }

// dbOption sets the GORM database instance for persistence.
type dbOption struct {
	db *gorm.DB
}

func (o dbOption) apply(opts *Options) { opts.DB = o.db }

// WithDB sets the GORM database instance for persistence.
func WithDB(dbc *gorm.DB) Option { return dbOption{db: dbc} }

// loggerOption sets the structured logger.
type loggerOption struct {
	logger *zap.Logger
}

func (o loggerOption) apply(opts *Options) { opts.Logger = o.logger }

// WithLogger sets the structured logger.
func WithLogger(logger *zap.Logger) Option { return loggerOption{logger: logger} }

// aggregationWindowOption sets the metric tumbling window duration.
type aggregationWindowOption time.Duration

func (o aggregationWindowOption) apply(opts *Options) { opts.AggregationWindow = time.Duration(o) }

// WithAggregationWindow sets the metric tumbling window duration. A non-positive
// value disables aggregation.
func WithAggregationWindow(d time.Duration) Option { return aggregationWindowOption(d) }

// metricStoreOption sets the store used to persist metric data points.
type metricStoreOption struct {
	store MetricStore
}

func (o metricStoreOption) apply(opts *Options) { opts.MetricStore = o.store }

// WithMetricStore sets the store used to persist metric data points. When both
// MetricStore and LogStore are provided, a Persistence layer is created
// automatically.
func WithMetricStore(store MetricStore) Option { return metricStoreOption{store: store} }

// logStoreOption sets the store used to persist log entries.
type logStoreOption struct {
	store LogStore
}

func (o logStoreOption) apply(opts *Options) { opts.LogStore = o.store }

// WithLogStore sets the store used to persist log entries. When both MetricStore
// and LogStore are provided, a Persistence layer is created automatically.
func WithLogStore(store LogStore) Option { return logStoreOption{store: store} }

// persistenceOption injects a pre-built persistence layer.
type persistenceOption struct {
	p *Persistence
}

func (o persistenceOption) apply(opts *Options) { opts.Persistence = o.p }

// WithPersistence injects a pre-built persistence layer, overriding any
// MetricStore/LogStore configuration.
func WithPersistence(p *Persistence) Option { return persistenceOption{p: p} }

// poolOption injects a worker pool used for concurrent telemetry processing.
type poolOption struct {
	p pool.Pool
}

func (o poolOption) apply(opts *Options) { opts.Pool = o.p }

// WithPool injects a worker pool used for concurrent telemetry processing.
// When this option is not supplied the manager creates a default IO pool
// sized to runtime.NumCPU and owns its lifecycle (shutting it down on
// Stop). An injected pool is never shut down by the manager; the caller
// retains full lifecycle responsibility.
func WithPool(p pool.Pool) Option { return poolOption{p: p} }

// proberServiceOption injects the active probing coordinator.
type proberServiceOption struct {
	svc *ProberService
}

func (o proberServiceOption) apply(opts *Options) { opts.ProberService = o.svc }

// WithProberService injects the active probing coordinator. When set,
// the Manager starts the ProberService alongside the listener pipeline
// and stops it in reverse order on shutdown.
func WithProberService(svc *ProberService) Option { return proberServiceOption{svc: svc} }

// listenerRegistryOption injects the passive listener registry.
type listenerRegistryOption struct {
	reg *ListenerRegistry
}

func (o listenerRegistryOption) apply(opts *Options) { opts.ListenerRegistry = o.reg }

// WithListenerRegistry injects the passive listener registry. When set,
// the Manager starts all registered ProtocolListeners on Start and stops
// them on Stop. HTTPListeners in the registry are looked up by the API
// router layer to mount their handlers on the telemetry endpoint.
func WithListenerRegistry(reg *ListenerRegistry) Option {
	return listenerRegistryOption{reg: reg}
}

// New creates a new Collector with the given options.
//
// Returns an error if the internal time wheel or default telemetry pool
// cannot be initialized (see [Manager] / [timewheel.New] for details).
// The error path is unreachable in practice but is returned rather
// than panicking to honor the "no panic in business logic" rule.
func New(options ...Option) (Collector, error) {
	return newManager(options...)
}
