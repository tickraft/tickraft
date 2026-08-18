// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package event

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"go.uber.org/zap"
)

// Envelope is the non-generic event envelope carrying metadata and payload.
// All events published via Bus.Publish are wrapped in an Envelope for delivery.
type Envelope struct {
	// Type identifies the event type.
	Type Type
	// Payload carries the event data; the concrete type is agreed upon by publisher and subscriber.
	Payload any
	// Timestamp records when the event was produced.
	Timestamp time.Time
	// Priority controls event dispatch priority; higher values mean higher priority.
	Priority int
	// EventID is the unique identifier of the event, used for idempotent deduplication and tracing.
	EventID string
	// TenantID identifies the tenant, used for sharding and isolation.
	TenantID string
	// Metadata carries key-value context propagated across modules.
	Metadata map[string]string
}

// Handler processes a non-generic event envelope.
// Returning an error triggers retry or failed-event persistence according to the subscription config.
type Handler func(ctx context.Context, event Envelope) error

// Bus is the event bus abstraction, serving as an SPI extension anchor.
// This repository provides the channel-based channelBus implementation;
// callers may inject a StreamBridge adapter for distributed extensions.
type Bus interface {
	// Publish publishes an event to the bus.
	// Options configure priority, sync mode, event ID, tenant ID and metadata.
	Publish(ctx context.Context, eventType Type, payload any, opts ...PublishOption) error

	// Subscribe registers a subscriber and returns a Subscription used to unsubscribe.
	// Options configure filter function, timeout, retry strategy and sync mode.
	Subscribe(eventType Type, handler Handler, opts ...SubscribeOption) (Subscription, error)

	// Close gracefully shuts down the bus, waiting for all in-flight Handlers to finish.
	Close() error
}

// Subscription represents an active subscription that can be cancelled via Cancel.
type Subscription interface {
	// ID returns the unique identifier of the subscription.
	ID() string
	// Cancel cancels the subscription; no further events will be delivered after cancellation.
	Cancel()
}

// Event is a generic event with type-safe payload.
type Event[T any] struct {
	// Type identifies the event type.
	Type Type
	// Payload carries the type-safe event data.
	Payload T
	// Timestamp records when the event was produced.
	Timestamp time.Time
	// Priority controls event dispatch priority; higher values mean higher priority.
	Priority int
	// EventID is the unique identifier of the event, used for idempotent deduplication and tracing.
	EventID string
	// TenantID identifies the tenant, used for sharding and isolation.
	TenantID string
	// Metadata carries key-value context propagated across modules.
	Metadata map[string]string
}

// Publish is the generic wrapper of Bus.Publish, providing compile-time type safety.
// Internally it converts the generic Event[T] into a non-generic Envelope and calls Bus.Publish.
func Publish[T any](ctx context.Context, bus Bus, eventType Type, payload T, opts ...PublishOption) error {
	return bus.Publish(ctx, eventType, payload, opts...)
}

// Subscribe is the generic wrapper of Bus.Subscribe, providing compile-time type safety.
// The handler parameter is a type-safe function receiving an Event[T] payload.
// Internally it converts the generic handler into a non-generic Handler and calls Bus.Subscribe.
func Subscribe[T any](bus Bus, eventType Type, handler func(ctx context.Context, event Event[T]) error,
	opts ...SubscribeOption) (Subscription, error) {
	wrapper := func(ctx context.Context, env Envelope) error {
		typed, ok := env.Payload.(T)
		if !ok {
			return fmt.Errorf("event: payload type assertion failed for type %s: expected %T, got %T",
				env.Type, typed, env.Payload)
		}
		event := Event[T]{
			Type:      env.Type,
			Payload:   typed,
			Timestamp: env.Timestamp,
			Priority:  env.Priority,
			EventID:   env.EventID,
			TenantID:  env.TenantID,
			Metadata:  env.Metadata,
		}
		return handler(ctx, event)
	}
	return bus.Subscribe(eventType, wrapper, opts...)
}

// PublishOption configures event publishing behavior.
type PublishOption func(*publishConfig)

type publishConfig struct {
	priority int
	sync     bool
	eventID  string
	tenantID string
	metadata map[string]string
}

// WithPriority sets the event priority; higher values mean higher priority.
func WithPriority(priority int) PublishOption {
	return func(c *publishConfig) {
		c.priority = priority
	}
}

// WithSync enables synchronous dispatch mode: the publisher blocks until all Handlers finish.
func WithSync() PublishOption {
	return func(c *publishConfig) {
		c.sync = true
	}
}

// WithEventID sets the event unique identifier; when not set, the bus generates one automatically.
func WithEventID(eventID string) PublishOption {
	return func(c *publishConfig) {
		c.eventID = eventID
	}
}

// WithTenantID sets the tenant identifier, used for sharding and isolation.
func WithTenantID(tenantID string) PublishOption {
	return func(c *publishConfig) {
		c.tenantID = tenantID
	}
}

// WithMetadata sets event metadata, carrying key-value context propagated across modules.
func WithMetadata(metadata map[string]string) PublishOption {
	return func(c *publishConfig) {
		c.metadata = metadata
	}
}

// SubscribeOption configures subscription behavior.
type SubscribeOption func(*subscribeConfig)

type subscribeConfig struct {
	filter      FilterFunc
	timeout     time.Duration
	maxRetries  int
	baseBackoff time.Duration
	jitter      float64
	syncMode    bool
}

// WithFilter sets the event filter function; events are delivered only when it returns true.
func WithFilter(filter FilterFunc) SubscribeOption {
	return func(c *subscribeConfig) {
		c.filter = filter
	}
}

// WithTimeout sets the Handler execution timeout; the Handler's context is cancelled on expiry.
func WithTimeout(timeout time.Duration) SubscribeOption {
	return func(c *subscribeConfig) {
		c.timeout = timeout
	}
}

// WithRetry configures an exponential backoff retry strategy.
// maxRetries is the maximum retry count and baseBackoff is the base backoff interval.
// The actual backoff interval is baseBackoff * 2^n, where n is the current retry attempt.
func WithRetry(maxRetries int, baseBackoff time.Duration) SubscribeOption {
	return func(c *subscribeConfig) {
		c.maxRetries = maxRetries
		c.baseBackoff = baseBackoff
	}
}

// WithJitter configures the jitter factor applied to the exponential backoff.
// factor must be in [0.0, 1.0]; values outside this range are clamped.
// factor=0.0 (default) means no jitter, preserving deterministic backoff.
// factor=1.0 means full jitter: backoff is randomized in [0, exponential_backoff].
// factor=0.3 means partial jitter: backoff is randomized in [0.7*exponential, exponential].
// The jittered backoff formula is: exponential * (1.0 - factor + factor * rand.Float64()).
// WithJitter is only effective when combined with WithRetry.
func WithJitter(factor float64) SubscribeOption {
	return func(c *subscribeConfig) {
		switch {
		case factor < 0.0:
			c.jitter = 0.0
		case factor > 1.0:
			c.jitter = 1.0
		default:
			c.jitter = factor
		}
	}
}

// WithSyncMode marks the subscriber as synchronous: the Handler is invoked directly in the publisher goroutine.
func WithSyncMode() SubscribeOption {
	return func(c *subscribeConfig) {
		c.syncMode = true
	}
}

// Option configures Bus construction.
type Option func(*channelBus)

// WithBufferSize sets the priority queue buffer size; the default is 1024.
func WithBufferSize(size int) Option {
	return func(b *channelBus) {
		if size > 0 {
			b.bufferSize = size
		}
	}
}

// WithLogger sets the zap logger; the default is no-op.
func WithLogger(logger *zap.Logger) Option {
	return func(b *channelBus) {
		if logger != nil {
			b.logger = logger
		}
	}
}

// WithDefaultTimeout sets the default Handler execution timeout; the default is 3 seconds.
func WithDefaultTimeout(timeout time.Duration) Option {
	return func(b *channelBus) {
		if timeout > 0 {
			b.defaultTimeout = timeout
		}
	}
}

// WithFailedEventStore configures the persistent store for failed events.
func WithFailedEventStore(store FailedEventStore) Option {
	return func(b *channelBus) {
		if store != nil {
			b.failedStore = store
		}
	}
}

// WithDebug enables event lifecycle tracing logs.
func WithDebug(enabled bool) Option {
	return func(b *channelBus) {
		b.debug = enabled
	}
}

// Instrumenter is the SPI extension point for event bus observability.
// This package provides a no-op default; callers
// inject a Prometheus-backed implementation via WithInstrumenter.
type Instrumenter interface {
	// IncPublish increments the publish counter for an event type.
	IncPublish(eventType Type, tenantID string)
	// IncDrop increments the drop counter for an event type with a reason.
	IncDrop(eventType Type, reason string)
	// ObserveHandlerDuration records the duration of a handler invocation.
	ObserveHandlerDuration(eventType Type, handlerID string, duration time.Duration)
	// IncHandlerPanic increments the panic counter for a handler.
	IncHandlerPanic(eventType Type, handlerID string)
	// IncRetry increments the retry counter for a handler.
	IncRetry(eventType Type, handlerID string)
	// IncSubscriberCount increments the subscriber count for an event type.
	IncSubscriberCount(eventType Type)
	// DecSubscriberCount decrements the subscriber count for an event type.
	DecSubscriberCount(eventType Type)
}

// noopInstrumenter is the default no-op Instrumenter used when none is injected.
type noopInstrumenter struct{}

func (noopInstrumenter) IncPublish(Type, string)                            {}
func (noopInstrumenter) IncDrop(Type, string)                               {}
func (noopInstrumenter) ObserveHandlerDuration(Type, string, time.Duration) {}
func (noopInstrumenter) IncHandlerPanic(Type, string)                       {}
func (noopInstrumenter) IncRetry(Type, string)                              {}
func (noopInstrumenter) IncSubscriberCount(Type)                            {}
func (noopInstrumenter) DecSubscriberCount(Type)                            {}

// WithInstrumenter sets the Instrumenter for observability.
// The default Instrumenter is a no-op; callers may inject
// a Prometheus-backed implementation.
func WithInstrumenter(i Instrumenter) Option {
	return func(b *channelBus) {
		if i != nil {
			b.instrumenter = i
		}
	}
}

// NewBus creates a new event bus instance.
func NewBus(opts ...Option) Bus {
	b := &channelBus{
		subscribers:    make(map[Type][]*subscriber),
		queues:         make(map[Type]*typeQueue),
		bufferSize:     defaultBufferSize,
		logger:         zap.NewNop(),
		defaultTimeout: defaultTimeout,
		failedStore:    NoopFailedEventStore{},
		instrumenter:   noopInstrumenter{},
	}
	for _, opt := range opts {
		opt(b)
	}
	return b
}

// FilterFunc is the event filter function; events are delivered to subscribers only when it returns true.
type FilterFunc func(Envelope) bool

// Matcher defines the event matching interface, used for fuzzy event type matching.
type Matcher interface {
	// Match reports whether the given envelope matches.
	Match(env Envelope) bool
}

// ExactMatcher matches event types exactly.
type ExactMatcher struct {
	// Type is the event type to match.
	Type Type
}

// Match reports whether the envelope's event type equals the target type.
func (m ExactMatcher) Match(env Envelope) bool {
	return env.Type == m.Type
}

// RegexMatcher matches event types based on a regular expression.
type RegexMatcher struct {
	// pattern is the compiled regular expression.
	pattern *regexp.Regexp
}

// NewRegexMatcher creates a regex matcher; pattern is the event type regular expression.
func NewRegexMatcher(pattern string) (*RegexMatcher, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("event: compile regex pattern %q: %w", pattern, err)
	}
	return &RegexMatcher{pattern: re}, nil
}

// Match reports whether the envelope's event type matches the regular expression.
func (m *RegexMatcher) Match(env Envelope) bool {
	return m.pattern.MatchString(string(env.Type))
}

// FailedEventStore defines the persistent store interface for failed events.
// When all Handler retries fail, the event envelope and error are saved to this store.
type FailedEventStore interface {
	// Save persists the failed event envelope and the corresponding error.
	Save(ctx context.Context, env Envelope, err error) error
}

// NoopFailedEventStore is a no-op FailedEventStore that performs no persistence.
type NoopFailedEventStore struct{}

// Save does nothing and returns nil.
func (NoopFailedEventStore) Save(context.Context, Envelope, error) error {
	return nil
}

// defaultBufferSize is the default priority queue buffer size.
const defaultBufferSize = 1024

// defaultTimeout is the default Handler execution timeout.
const defaultTimeout = 3 * time.Second
