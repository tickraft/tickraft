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

type funcOption func(*Options)

func (f funcOption) apply(o *Options) { f(o) }

// WithProcessorRegistry sets the processor registry.
func WithProcessorRegistry(registry *ProcessorRegistry) Option {
	return funcOption(func(o *Options) { o.ProcessorRegistry = registry })
}

// WithAssetStore sets the asset persistence store.
func WithAssetStore(store asset.Store) Option {
	return funcOption(func(o *Options) { o.AssetStore = store })
}

// WithEventBus sets the event bus for event publishing.
func WithEventBus(bus event.Bus) Option {
	return funcOption(func(o *Options) { o.Bus = bus })
}

// WithDB sets the GORM database instance for persistence.
func WithDB(dbc *gorm.DB) Option {
	return funcOption(func(o *Options) { o.DB = dbc })
}

// WithLogger sets the structured logger.
func WithLogger(logger *zap.Logger) Option {
	return funcOption(func(o *Options) { o.Logger = logger })
}

// WithAggregationWindow sets the metric tumbling window duration. A non-positive
// value disables aggregation.
func WithAggregationWindow(d time.Duration) Option {
	return funcOption(func(o *Options) { o.AggregationWindow = d })
}

// WithMetricStore sets the store used to persist metric data points. When both
// MetricStore and LogStore are provided, a Persistence layer is created
// automatically.
func WithMetricStore(store MetricStore) Option {
	return funcOption(func(o *Options) { o.MetricStore = store })
}

// WithLogStore sets the store used to persist log entries. When both MetricStore
// and LogStore are provided, a Persistence layer is created automatically.
func WithLogStore(store LogStore) Option {
	return funcOption(func(o *Options) { o.LogStore = store })
}

// WithPersistence injects a pre-built persistence layer, overriding any
// MetricStore/LogStore configuration.
func WithPersistence(p *Persistence) Option {
	return funcOption(func(o *Options) { o.Persistence = p })
}

// WithPool injects a worker pool used for concurrent telemetry processing.
// When this option is not supplied the manager creates a default IO pool
// sized to runtime.NumCPU and owns its lifecycle (shutting it down on
// Stop). An injected pool is never shut down by the manager; the caller
// retains full lifecycle responsibility.
func WithPool(p pool.Pool) Option {
	return funcOption(func(o *Options) { o.Pool = p })
}

// WithProberService injects the active probing coordinator. When set,
// the Manager starts the ProberService alongside the listener pipeline
// and stops it in reverse order on shutdown.
func WithProberService(svc *ProberService) Option {
	return funcOption(func(o *Options) { o.ProberService = svc })
}

// WithListenerRegistry injects the passive listener registry. When set,
// the Manager starts all registered ProtocolListeners on Start and stops
// them on Stop. HTTPListeners in the registry are looked up by the API
// router layer to mount their handlers on the telemetry endpoint.
func WithListenerRegistry(reg *ListenerRegistry) Option {
	return funcOption(func(o *Options) { o.ListenerRegistry = reg })
}

// New creates a new Collector with the given options.
//
// Returns an error if the internal time wheel or default telemetry pool
// cannot be initialized (see [Manager] / [timewheel.New] for details).
// The error path is unreachable in practice but is returned rather
// than panicking to honor the "no panic in business logic" rule.
func New(opts ...Option) (Collector, error) {
	return newManager(opts...)
}
