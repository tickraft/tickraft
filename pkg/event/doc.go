// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package event provides in-process event bus capabilities, implementing type-safe
// publish/subscribe based on Go generics.
//
// The event bus is the core decoupling mechanism of the Tickraft kernel.
// It carries in-process asynchronous event delivery and is the key infrastructure
// enabling loosely-coupled collaboration between the scheduler, executors,
// collectors, and the alerting center.
//
// # Core Features
//
//   - Type-safe: Publish[T] / Subscribe[T] based on Go generics, ensuring payload
//     type matching at compile time.
//   - Priority ordering: a heap-based priority queue replaces a FIFO channel;
//     higher numeric values mean higher priority.
//   - Panic recovery: each Handler invocation is wrapped in defer recover() to
//     isolate failures.
//   - Timeout control: Handler execution timeout is configurable; the default is
//     3 seconds.
//   - Exponential backoff retry: when a Handler returns an error it is retried
//     using an exponential backoff strategy.
//   - Failure persistence: the FailedEventStore interface abstracts persistence
//     of failed events.
//   - Memory pooling: sync.Pool manages Envelope objects to reduce GC pressure.
//   - Instrumenter SPI: an open extensibility point for observability. The
//     default is a no-op; callers may inject a
//     Prometheus-backed implementation via WithInstrumenter.
//
// # Quick Start
//
// Create an event bus and publish/subscribe to events:
//
//	bus := event.NewBus()
//	defer bus.Close()
//
//	// Subscribe to an execution triggered event.
//	sub, err := event.Subscribe[event.ExecutionPayload](
//	    bus,
//	    event.TypeExecutionTriggered,
//	    func(ctx context.Context, e event.Event[event.ExecutionPayload]) error {
//	        fmt.Printf("execution triggered: %+v\n", e.Payload)
//	        return nil
//	    },
//	)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer sub.Cancel()
//
//	// Publish an execution triggered event.
//	err = event.Publish(context.Background(), bus, event.TypeExecutionTriggered,
//	    event.ExecutionPayload{
//	        TaskID:       "task-001",
//	        ExecutionID:  "exec-001",
//	        ExecutorType: "http",
//	        TenantID:     "tenant-001",
//	        Action:       "triggered",
//	    },
//	)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
// # Event Priority
//
// Use WithPriority to set the event priority; higher numeric values mean higher
// priority:
//
//	err = bus.Publish(ctx, event.TypeExecutionTriggered, payload,
//	    event.WithPriority(10),  // high priority
//	)
//
//	// Multiple events of the same type are dispatched from highest to lowest
//	// priority; events with the same priority are dispatched in publish order
//	// (FIFO).
//
// # Delivery Model
//
// Delivery is always asynchronous: Publish enqueues onto the per-type priority
// queue and returns without waiting for handlers. Publishers that need to
// observe completion subscribe to a downstream (completion) event instead.
// Context cancellation is stripped at enqueue time via context.WithoutCancel,
// so a publisher returning early never aborts subscriber work mid-delivery.
//
// # Timeout and Retry
//
// Use WithTimeout and WithRetry to configure Handler timeout and retry strategy:
//
//	sub, err = event.Subscribe[event.ExecutionPayload](
//	    bus,
//	    event.TypeExecutionTriggered,
//	    handler,
//	    event.WithTimeout(2*time.Second),
//	    event.WithRetry(3, 100*time.Millisecond),  // at most 3 retries, backoff 100ms/200ms/400ms
//	)
//
// # Failed Event Store
//
// Configure a persistent store for failed events via WithFailedEventStore:
//
//	bus := event.NewBus(
//	    event.WithFailedEventStore(myStore),
//	)
//
// // When all Handler retries fail, the event envelope and the error are saved
// // to the FailedEventStore.
//
// # Instrumenter SPI
//
// The Instrumenter interface is the SPI extension point for event bus
// observability. This package ships a no-op default, so the
// kernel has zero Prometheus dependency. callers
// inject a Prometheus-backed implementation via WithInstrumenter at startup:
//
//	bus := event.NewBus(
//	    event.WithInstrumenter(promInstrumenter),
//	)
//
// The Instrumenter exposes the following hooks covering the full event
// lifecycle:
//
//   - IncPublish(eventType, tenantID): increment the publish counter.
//   - IncDrop(eventType, reason): increment the drop counter (reasons such as
//     channel_full, publish_closed).
//   - ObserveHandlerDuration(eventType, handlerID, duration): record a Handler
//     invocation duration.
//   - IncHandlerPanic(eventType, handlerID): increment the Handler panic
//     counter.
//   - IncRetry(eventType, handlerID): increment the Handler retry counter.
//   - IncSubscriberCount(eventType) / DecSubscriberCount(eventType): adjust the
//     active subscriber gauge for an event type.
//
// # SPI Extension
//
// The Bus interface acts as an SPI extension anchor. callers
// may implement a StreamBridge adapter for distributed extensions:
//
//	type StreamBridge struct {
//	    local event.Bus
//	    // ...
//	}
//
//	func (b *StreamBridge) Publish(ctx context.Context, t event.Type, payload any,
//	    opts ...event.PublishOption) error {
//	    // Local delivery + cross-process delivery via Redis Stream.
//	    return b.local.Publish(ctx, t, payload, opts...)
//	}
//
// Code in this package can use the extended adapter via Publish[T] / Subscribe[T]
// without any modification.
package event
