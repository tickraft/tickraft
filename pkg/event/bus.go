// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package event

import (
	"context"
	"fmt"
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
	// Options configure priority, tenant ID and metadata.
	Publish(ctx context.Context, eventType Type, payload any, opts ...PublishOption) error

	// Subscribe registers a subscriber and returns a Subscription used to unsubscribe.
	// Options configure handler timeout and retry strategy.
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
func Subscribe[T any](
	bus Bus,
	eventType Type,
	handler func(ctx context.Context, event Event[T]) error,
	opts ...SubscribeOption,
) (Subscription, error) {
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
type PublishOption interface {
	apply(*publishConfig)
}

type publishConfig struct {
	priority int
	tenantID string
	metadata map[string]string
}

// priorityOption sets the event priority.
type priorityOption int

func (o priorityOption) apply(c *publishConfig) { c.priority = int(o) }

// WithPriority sets the event priority; higher values mean higher priority.
func WithPriority(priority int) PublishOption { return priorityOption(priority) }

// tenantIDOption sets the tenant identifier.
type tenantIDOption string

func (o tenantIDOption) apply(c *publishConfig) { c.tenantID = string(o) }

// WithTenantID sets the tenant identifier, used for sharding and isolation.
func WithTenantID(tenantID string) PublishOption { return tenantIDOption(tenantID) }

// metadataOption sets the event metadata.
type metadataOption struct {
	metadata map[string]string
}

func (o metadataOption) apply(c *publishConfig) { c.metadata = o.metadata }

// WithMetadata sets event metadata, carrying key-value context propagated across modules.
func WithMetadata(metadata map[string]string) PublishOption {
	return metadataOption{metadata: metadata}
}

// SubscribeOption configures subscription behavior.
type SubscribeOption interface {
	apply(*subscribeConfig)
}

type subscribeConfig struct {
	timeout     time.Duration
	maxRetries  int
	baseBackoff time.Duration
}

// timeoutOption sets the Handler execution timeout.
type timeoutOption time.Duration

func (o timeoutOption) apply(c *subscribeConfig) { c.timeout = time.Duration(o) }

// WithTimeout sets the Handler execution timeout; the Handler's context is cancelled on expiry.
func WithTimeout(timeout time.Duration) SubscribeOption { return timeoutOption(timeout) }

// retryOption sets the exponential backoff retry strategy.
type retryOption struct {
	maxRetries  int
	baseBackoff time.Duration
}

func (o retryOption) apply(c *subscribeConfig) {
	c.maxRetries = o.maxRetries
	c.baseBackoff = o.baseBackoff
}

// WithRetry configures an exponential backoff retry strategy.
// maxRetries is the maximum retry count and baseBackoff is the base backoff interval.
// The actual backoff interval is baseBackoff * 2^n, where n is the current retry attempt.
func WithRetry(maxRetries int, baseBackoff time.Duration) SubscribeOption {
	return retryOption{maxRetries: maxRetries, baseBackoff: baseBackoff}
}

// Option configures Bus construction.
type Option interface {
	apply(*channelBus)
}

// loggerOption sets the zap logger.
type loggerOption struct {
	logger *zap.Logger
}

func (o loggerOption) apply(b *channelBus) {
	if o.logger != nil {
		b.logger = o.logger
	}
}

// WithLogger sets the zap logger; the default is no-op.
func WithLogger(logger *zap.Logger) Option { return loggerOption{logger: logger} }

// failedEventStoreOption configures the persistent store for failed events.
type failedEventStoreOption struct {
	store FailedEventStore
}

func (o failedEventStoreOption) apply(b *channelBus) {
	if o.store != nil {
		b.failedStore = o.store
	}
}

// WithFailedEventStore configures the persistent store for failed events.
func WithFailedEventStore(store FailedEventStore) Option {
	return failedEventStoreOption{store: store}
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

// instrumenterOption sets the Instrumenter for observability.
type instrumenterOption struct {
	i Instrumenter
}

func (o instrumenterOption) apply(b *channelBus) {
	if o.i != nil {
		b.instrumenter = o.i
	}
}

// WithInstrumenter sets the Instrumenter for observability.
// The default Instrumenter is a no-op; callers may inject
// a Prometheus-backed implementation.
func WithInstrumenter(i Instrumenter) Option { return instrumenterOption{i: i} }

// NewBus creates a new event bus instance.
func NewBus(options ...Option) Bus {
	bus := &channelBus{
		subscribers:  make(map[Type][]*subscriber),
		queues:       make(map[Type]*typeQueue),
		logger:       zap.NewNop(),
		failedStore:  NoopFailedEventStore{},
		instrumenter: noopInstrumenter{},
	}
	for _, o := range options {
		o.apply(bus)
	}
	return bus
}

// defaultBufferSize is the fixed per-type priority queue capacity. When the
// queue is full, newly published events are dropped (counted via the
// Instrumenter's IncDrop) rather than blocking the publisher.
const defaultBufferSize = 1024

// defaultTimeout is the default Handler execution timeout, applied when a
// subscription does not override it via WithTimeout.
const defaultTimeout = 3 * time.Second

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
