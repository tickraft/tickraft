// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package prism

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/tickraft/tickraft/pkg/errdefs"
	"github.com/tickraft/tickraft/pkg/event"
	"github.com/tickraft/tickraft/pkg/pool"
	"github.com/tickraft/tickraft/pkg/prism/alert"
	"github.com/tickraft/tickraft/pkg/prism/channel"
	"github.com/tickraft/tickraft/pkg/prism/governance"
	"github.com/tickraft/tickraft/pkg/prism/remediation"
	"github.com/tickraft/tickraft/pkg/prism/rule"
	"go.uber.org/zap"
)

// defaultNotificationPoolSize is the worker count used when no pool size is
// configured.
const defaultNotificationPoolSize = 8

// channelSendTimeout is the maximum time allowed for a single channel Send
// operation. It prevents a slow or unresponsive notification channel from
// indefinitely blocking a worker pool slot and causing backpressure on the
// dispatch path. Individual channels may enforce shorter timeouts internally;
// this is the upper bound.
const channelSendTimeout = 30 * time.Second

// Engine is the alert evaluation and notification dispatch engine. It
// subscribes to telemetry alert events on the event bus, evaluates
// registered rules against each event, and dispatches matching alerts to
// registered channels through a bounded worker pool.
//
// A governance.Guard chain is invoked before rule evaluation. In a
// single-process default deployment the chain is empty, so Dispatch
// proceeds directly to rule evaluation. The callers may inject the
// full governance chain (silence → aggregator → suppressor → storm) at
// startup.
type Engine struct {
	bus    event.Bus
	logger *zap.Logger

	rulesMu    sync.RWMutex
	rules      []Matcher
	channelsMu sync.RWMutex
	channels   []Channel
	guardsMu   sync.RWMutex
	guards     []governance.Guard

	onAlert OnAlertFunc

	// postGuardHook is invoked after the governance guard chain passes and
	// before rule evaluation. nil in default deployment (no-op).
	postGuardHook PostGuardHook

	// deadLetterHandler is invoked when a notification dispatch fails to be
	// submitted to the worker pool. nil in default deployment (log and drop).
	deadLetterHandler DeadLetterHandler

	notifyPool pool.Pool
	poolOwned  bool

	startMu sync.Mutex
	started bool
	cancel  context.CancelFunc
	wg      sync.WaitGroup

	// Orchestration fields: populated by NewFromConfig, nil when the Engine
	// is created via New() directly (standalone dispatch engine mode).
	ruleEngine       *rule.Engine
	ruleStore        *rule.Store
	recordStore      alert.RecordStore
	channelStore     *channel.Store
	remediationStore *remediation.Store
	remediationMgr   *remediation.Manager
	ruleEngineStopFn func(context.Context) error
}

// Option configures an Engine.
type Option interface {
	apply(*options)
}

type options struct {
	bus                  event.Bus
	logger               *zap.Logger
	notificationPoolSize int
	notifyPool           pool.Pool
	onAlert              OnAlertFunc
	postGuardHook        PostGuardHook
	deadLetterHandler    DeadLetterHandler
	guards               []governance.Guard
}

type funcOption func(*options)

func (f funcOption) apply(o *options) { f(o) }

// WithEventBus sets the event bus used to subscribe to alert events.
func WithEventBus(bus event.Bus) Option {
	return funcOption(func(o *options) { o.bus = bus })
}

// WithLogger sets the structured logger.
func WithLogger(logger *zap.Logger) Option {
	return funcOption(func(o *options) { o.logger = logger })
}

// WithNotificationPoolSize sets the goroutine pool size for sending
// notifications. A non-positive value defaults to 8. Ignored when
// WithPool is used to inject an externally-owned pool.
func WithNotificationPoolSize(n int) Option {
	return funcOption(func(o *options) { o.notificationPoolSize = n })
}

// WithPool injects an externally-owned worker pool for notification
// dispatch. When set, the engine does not create or shut down its own
// pool; the caller is responsible for the pool lifecycle.
func WithPool(p pool.Pool) Option {
	return funcOption(func(o *options) { o.notifyPool = p })
}

// WithOnAlert registers a callback invoked when an alert event matches the
// registered rules (or when no rules are registered). The callback is
// called synchronously within dispatch before channel notification, so it
// must return quickly. It is typically used to persist alert records to a
// store without introducing a dependency from the prism package to the
// store package.
func WithOnAlert(fn OnAlertFunc) Option {
	return funcOption(func(o *options) { o.onAlert = fn })
}

// WithPostGuardHook registers a hook invoked after the governance guard chain
// passes (every guard returned DecisionPass) and before rule evaluation. The
// callers may use this to notify the Suppressor about active source
// alerts. Passing nil clears any previously registered hook. In an
// single-process deployment no hook is registered, so Dispatch
// proceeds directly to rule evaluation.
func WithPostGuardHook(h PostGuardHook) Option {
	return funcOption(func(o *options) { o.postGuardHook = h })
}

// WithDeadLetterHandler registers a handler invoked when a notification
// dispatch fails to be submitted to the worker pool (e.g. pool at capacity).
// The handler may persist the event to a dead-letter queue for later retry.
// When nil (default), rejected notifications are logged and dropped.
func WithDeadLetterHandler(h DeadLetterHandler) Option {
	return funcOption(func(o *options) { o.deadLetterHandler = h })
}

// WithGuards sets the governance guard chain invoked before rule
// evaluation in Dispatch. The chain is called in order; the first non-Pass
// decision short-circuits the chain. In an single-process
// deployment the chain is empty, so Dispatch proceeds directly to rule
// evaluation.
func WithGuards(guards ...governance.Guard) Option {
	return funcOption(func(o *options) {
		for _, g := range guards {
			if g != nil {
				o.guards = append(o.guards, g)
			}
		}
	})
}

// New creates a new alert Engine with the given options.
//
// When no notification pool is injected via WithPool, the engine creates
// and owns a bounded IO pool sized by WithNotificationPoolSize (default 8).
// The owned pool is shut down on Stop.
//
// Returns an error if the internally-created notification pool cannot be
// initialized. This path is unreachable in practice because the worker
// count is sanitized to a positive value, but the error is returned
// rather than panicking to honor the "no panic in business logic" rule.
func New(opts ...Option) (*Engine, error) {
	o := &options{
		logger:               zap.NewNop(),
		notificationPoolSize: defaultNotificationPoolSize,
	}
	for _, opt := range opts {
		opt.apply(o)
	}

	e := &Engine{
		bus:               o.bus,
		logger:            o.logger,
		onAlert:           o.onAlert,
		postGuardHook:     o.postGuardHook,
		deadLetterHandler: o.deadLetterHandler,
		guards:            o.guards,
	}

	if o.notifyPool != nil {
		e.notifyPool = o.notifyPool
		e.poolOwned = false
	} else {
		size := o.notificationPoolSize
		if size <= 0 {
			size = defaultNotificationPoolSize
		}
		p, err := pool.New(
			pool.WithWorkers(size),
			pool.WithRejectionPolicy(pool.RejectionCallerRuns),
		)
		if err != nil {
			// Unreachable in practice: size is sanitized to >= 1
			// above. Returned as an error (not panic) to honor
			// the "no panic in business logic" rule.
			return nil, fmt.Errorf("prism: create notification pool: %w", err)
		}
		e.notifyPool = p
		e.poolOwned = true
	}

	return e, nil
}

// AddChannel registers a notification channel. Channels are notified
// concurrently for each dispatched alert. It must be called before Start.
func (e *Engine) AddChannel(ch Channel) {
	if ch == nil {
		return
	}
	e.channelsMu.Lock()
	e.channels = append(e.channels, ch)
	e.channelsMu.Unlock()
}

// SetChannels atomically replaces all notification channels. It is safe
// to call after Start for hot-reload of channels from the database.
func (e *Engine) SetChannels(chs []Channel) {
	filtered := make([]Channel, 0, len(chs))
	for _, ch := range chs {
		if ch != nil {
			filtered = append(filtered, ch)
		}
	}
	e.channelsMu.Lock()
	e.channels = filtered
	e.channelsMu.Unlock()
}

// ReloadChannels reloads notification channels from the database channel
// store. It queries all enabled channel records, builds runtime Channel
// instances, and atomically replaces the engine's channel list. This is
// called by the API layer after channel CRUD operations.
func (e *Engine) ReloadChannels(ctx context.Context) error {
	if e.channelStore == nil {
		return nil
	}
	records, err := e.channelStore.ListEnabled(ctx)
	if err != nil {
		return fmt.Errorf("list enabled channels: %w", err)
	}
	channels, err := BuildChannelsFromRecords(records)
	if err != nil {
		e.logger.Warn("reload channels: some channels failed to build", zap.Error(err))
	}
	e.SetChannels(channels)
	e.logger.Info("channels reloaded", zap.Int("count", len(channels)))
	return nil
}

// Channels returns the registered notification channels. The returned
// slice is a copy and safe to read concurrently with AddChannel.
func (e *Engine) Channels() []Channel {
	e.channelsMu.RLock()
	defer e.channelsMu.RUnlock()
	out := make([]Channel, len(e.channels))
	copy(out, e.channels)
	return out
}

// AddGuard registers a governance guard invoked before rule
// evaluation in Dispatch. Guards are called in registration order. It
// must be called before Start.
func (e *Engine) AddGuard(g governance.Guard) {
	if g == nil {
		return
	}
	e.guardsMu.Lock()
	e.guards = append(e.guards, g)
	e.guardsMu.Unlock()
}

// Start subscribes to alert events on the event bus. It returns an error
// if the engine is already started or no event bus is configured.
func (e *Engine) Start(ctx context.Context) error {
	e.startMu.Lock()
	defer e.startMu.Unlock()
	if e.started {
		return nil
	}
	if e.bus == nil {
		return errdefs.ErrBusNotConfigured
	}

	runCtx, cancel := context.WithCancel(ctx)
	e.cancel = cancel
	e.started = true

	// Start the remediation engine first so it is draining events before
	// the alert pipeline subscribes. On failure the partial start is
	// rolled back so Start can be retried.
	if e.remediationMgr != nil {
		if err := e.remediationMgr.Start(runCtx); err != nil {
			e.started = false
			cancel()
			return fmt.Errorf("prism: start remediation engine: %w", err)
		}
	}

	// Subscribe to telemetry alert events. Each handler normalizes the
	// typed payload into an Event and dispatches it through the
	// rule engine and notification channels.
	if _, err := event.Subscribe[event.MetricExceededPayload](e.bus, event.TypeTelemetryMetricExceeded, func(_ context.Context, ev event.Event[event.MetricExceededPayload]) error {
		e.dispatch(runCtx, metricPayloadToAlert(ev))
		return nil
	}); err != nil {
		return fmt.Errorf("prism: subscribe to metric exceeded events: %w", err)
	}
	if _, err := event.Subscribe[event.LogMatchedPayload](e.bus, event.TypeTelemetryLogMatched, func(_ context.Context, ev event.Event[event.LogMatchedPayload]) error {
		e.dispatch(runCtx, logPayloadToAlert(ev))
		return nil
	}); err != nil {
		return fmt.Errorf("prism: subscribe to log matched events: %w", err)
	}
	// Subscribe to asset status-change events. A transition to an abnormal
	// state (offline/critical/warning) is normalized into a heartbeat or
	// status alert Event; transitions to a healthy/unknown state (recoveries)
	// are skipped by statusPayloadToAlert so the engine does not emit alert
	// noise for recoveries. A heartbeat-loss transition (Source == "timeout",
	// published by telemetry.MarkOffline) is mapped to TypeHeartbeat; every
	// other abnormal transition is mapped to TypeStatus.
	if _, err := event.Subscribe[event.StatusChangePayload](e.bus, event.TypeAssetStatusChanged, func(_ context.Context, ev event.Event[event.StatusChangePayload]) error {
		evt, ok := statusPayloadToAlert(ev)
		if !ok {
			return nil
		}
		e.dispatch(runCtx, evt)
		return nil
	}); err != nil {
		return fmt.Errorf("prism: subscribe to status change events: %w", err)
	}

	e.logger.Info("prism engine started",
		zap.Int("rules", len(e.rulesSnapshot())),
		zap.Int("channels", len(e.channelsSnapshot())),
	)
	return nil
}

// Stop cancels the run context, waits for in-flight notifications to
// finish or the context to expire, and shuts down the owned notification
// pool. It also stops the rule engine if one was registered via
// NewFromConfig. It is idempotent.
func (e *Engine) Stop(ctx context.Context) error {
	e.startMu.Lock()
	if !e.started {
		e.startMu.Unlock()
		return nil
	}
	e.started = false
	if e.cancel != nil {
		e.cancel()
	}
	e.startMu.Unlock()

	// Stop the remediation engine first so it stops accepting new
	// dispatches, then the rule engine so its reload loop is cancelled
	// before the dispatch engine drains.
	if e.remediationMgr != nil {
		if err := e.remediationMgr.Stop(ctx); err != nil {
			e.logger.Warn("remediation engine stop returned error", zap.Error(err))
		}
	}
	if e.ruleEngineStopFn != nil {
		if err := e.ruleEngineStopFn(ctx); err != nil {
			e.logger.Warn("rule engine stop returned error", zap.Error(err))
		}
	}

	done := make(chan struct{})
	// goroutine lifecycle: bounded — drains in-flight notification goroutines
	// spawned by Dispatch; exits after e.wg.Wait() returns. In-flight channel
	// Send jobs observe the cancelled runCtx (cancelled above) and exit
	// promptly, so the wait is bounded in practice.
	go func() {
		e.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		return ctx.Err()
	}

	if e.poolOwned && e.notifyPool != nil {
		if err := e.notifyPool.Shutdown(ctx); err != nil {
			e.logger.Warn("notification pool shutdown returned error",
				zap.Error(err),
			)
		}
	}

	e.logger.Info("prism engine stopped")
	return nil
}

// rulesSnapshot returns the current rules without holding the read lock
// across the call site.
func (e *Engine) rulesSnapshot() []Matcher {
	e.rulesMu.RLock()
	defer e.rulesMu.RUnlock()
	return append([]Matcher(nil), e.rules...)
}

// channelsSnapshot returns the current channels without holding the read
// lock across the call site.
func (e *Engine) channelsSnapshot() []Channel {
	e.channelsMu.RLock()
	defer e.channelsMu.RUnlock()
	return append([]Channel(nil), e.channels...)
}

// guardsSnapshot returns the current governance guards without
// holding the read lock across the call site.
func (e *Engine) guardsSnapshot() []governance.Guard {
	e.guardsMu.RLock()
	defer e.guardsMu.RUnlock()
	return append([]governance.Guard(nil), e.guards...)
}

// --- Accessors for orchestration fields (populated by NewFromConfig) ---

// RuleStore returns the rule persistence store. Returns nil when the Engine
// was created via New() directly without orchestration.
func (e *Engine) RuleStore() *rule.Store { return e.ruleStore }

// RecordStore returns the alert record persistence store. Returns nil when
// the Engine was created via New() directly without orchestration.
func (e *Engine) RecordStore() alert.RecordStore { return e.recordStore }

// ChannelStore returns the notification channel persistence store. Returns
// nil when the Engine was created via New() directly without orchestration.
func (e *Engine) ChannelStore() *channel.Store { return e.channelStore }

// RemediationStore returns the remediation rule persistence store. Returns
// nil when the Engine was created via New() directly without orchestration.
func (e *Engine) RemediationStore() *remediation.Store { return e.remediationStore }

// RuleEngine returns the rule matching engine. Returns nil when the Engine
// was created via New() directly without orchestration or when no rule
// engine was registered.
func (e *Engine) RuleEngine() *rule.Engine { return e.ruleEngine }
