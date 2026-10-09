// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package event

import (
	"container/heap"
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

// ErrBusClosed indicates that the event bus is closed and rejects publishing or subscribing.
var ErrBusClosed = errors.New("event: bus is closed")

// channelBus is the in-process event bus implementation based on channels and a priority heap.
// It uses a map[Type]*typeQueue to manage the priority queue of each event type.
// Each event type has an independent consumer goroutine that pops events from the heap by priority and dispatches them.
type channelBus struct {
	logger       *zap.Logger
	failedStore  FailedEventStore
	instrumenter Instrumenter

	mu          sync.RWMutex
	queues      map[Type]*typeQueue
	subscribers map[Type][]*subscriber
	wg          sync.WaitGroup
	closed      atomic.Bool
	seq         atomic.Uint64
	nextSubID   atomic.Uint64
}

// typeQueue manages the priority queue and consumption signal for a single event type.
type typeQueue struct {
	pq     priorityQueue
	mu     sync.Mutex
	signal chan struct{}
	done   chan struct{}
}

// subscriber represents an active event subscriber.
type subscriber struct {
	id       string
	handler  Handler
	config   subscribeConfig
	canceled atomic.Bool
}

// queueItem is an element in the priority queue, carrying the event envelope and a sequence number.
type queueItem struct {
	envelope *Envelope
	seq      uint64
	// ctx carries the publisher's context values into the consumer loop.
	// Cancellation and deadlines are stripped at enqueue time via
	// context.WithoutCancel: the consumer delivers asynchronously, long
	// after the publisher may have returned (HTTP handlers publish with
	// their request context), and a completed request must not abort
	// subscriber work mid-delivery.
	ctx context.Context
}

// priorityQueue implements heap.Interface, ordering by Priority descending;
// items with the same Priority are ordered by seq ascending (FIFO).
type priorityQueue []*queueItem

// Len returns the queue length.
func (pq priorityQueue) Len() int { return len(pq) }

// Less compares priority: higher Priority wins; on equal Priority, smaller seq wins.
func (pq priorityQueue) Less(i, j int) bool {
	if pq[i].envelope.Priority != pq[j].envelope.Priority {
		return pq[i].envelope.Priority > pq[j].envelope.Priority
	}
	return pq[i].seq < pq[j].seq
}

// Swap swaps element positions.
func (pq priorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
}

// Push adds an element to the queue.
func (pq *priorityQueue) Push(x any) {
	*pq = append(*pq, x.(*queueItem)) //nolint:errcheck // heap.Push is only called with *queueItem
}

// Pop removes and returns the last element from the queue.
func (pq *priorityQueue) Pop() any {
	old := *pq
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	*pq = old[0 : n-1]
	return item
}

// envelopePool manages reuse of Envelope objects to reduce GC pressure.
var envelopePool = sync.Pool{
	New: func() any {
		return &Envelope{}
	},
}

// acquireEnvelope fetches an Envelope object from the sync.Pool.
func acquireEnvelope() *Envelope {
	return envelopePool.Get().(*Envelope) //nolint:errcheck // pool always yields *Envelope
}

// releaseEnvelope returns the Envelope object to the sync.Pool.
// All fields are cleared before release to avoid stale data.
func releaseEnvelope(env *Envelope) {
	if env == nil {
		return
	}
	env.Type = ""
	env.Payload = nil
	env.Timestamp = time.Time{}
	env.Priority = 0
	env.EventID = ""
	env.TenantID = ""
	env.Metadata = nil
	envelopePool.Put(env)
}

// eventSeq is the process-wide counter behind generateEventID.
var eventSeq atomic.Uint64

// generateEventID generates a unique event identifier from a millisecond
// timestamp and a process-wide counter. IDs key logs and the failed-event
// store; they carry no secrecy, so a crypto/rand read per publish would be
// pure overhead on the ingestion path.
func generateEventID() string {
	return fmt.Sprintf("evt-%d-%d", time.Now().UnixMilli(), eventSeq.Add(1))
}

// Publish publishes an event to the bus. Delivery is asynchronous: the
// event is pushed onto the per-type priority queue and dispatched by that
// type's consumer goroutine.
func (b *channelBus) Publish(ctx context.Context, eventType Type, payload any, options ...PublishOption) error {
	if b.closed.Load() {
		return ErrBusClosed
	}

	cfg := &publishConfig{}
	for _, o := range options {
		o.apply(cfg)
	}

	env := acquireEnvelope()
	env.Type = eventType
	env.Payload = payload
	env.Timestamp = time.Now()
	env.Priority = cfg.priority
	env.EventID = generateEventID()
	env.TenantID = cfg.tenantID
	env.Metadata = cfg.metadata

	b.instrumenter.IncPublish(eventType, cfg.tenantID)

	// Push onto the priority queue.
	b.mu.RLock()
	tq, exists := b.queues[eventType]
	b.mu.RUnlock()

	if !exists {
		// No subscribers: silently drop.
		releaseEnvelope(env)
		return nil
	}

	tq.mu.Lock()
	if b.closed.Load() {
		tq.mu.Unlock()
		b.instrumenter.IncDrop(eventType, "publish_closed")
		releaseEnvelope(env)
		return ErrBusClosed
	}
	if tq.pq.Len() >= defaultBufferSize {
		tq.mu.Unlock()
		b.instrumenter.IncDrop(eventType, "channel_full")
		b.logger.Warn("event queue full, dropping event",
			zap.String("type", string(eventType)),
			zap.String("event_id", env.EventID),
		)
		releaseEnvelope(env)
		return nil
	}
	heap.Push(&tq.pq, &queueItem{
		envelope: env,
		seq:      b.seq.Add(1),
		ctx:      context.WithoutCancel(ctx),
	})
	tq.mu.Unlock()

	// Notify the consumer goroutine.
	select {
	case tq.signal <- struct{}{}:
	default:
	}

	return nil
}

// Subscribe registers a subscriber and returns a Subscription.
// The first time a subscriber is registered for an event type, the consumer goroutine for that type is lazily started.
func (b *channelBus) Subscribe(eventType Type, handler Handler, options ...SubscribeOption) (Subscription, error) {
	if b.closed.Load() {
		return nil, ErrBusClosed
	}

	cfg := &subscribeConfig{}
	for _, o := range options {
		o.apply(cfg)
	}

	sub := &subscriber{
		id:      fmt.Sprintf("sub-%d", b.nextSubID.Add(1)),
		handler: handler,
		config:  *cfg,
	}

	b.mu.Lock()
	if b.closed.Load() {
		b.mu.Unlock()
		return nil, ErrBusClosed
	}
	// Copy-on-write: subscriber slices are published immutable under the
	// write lock, so dispatch can iterate them without a per-event copy.
	subs := b.subscribers[eventType]
	next := make([]*subscriber, len(subs)+1)
	copy(next, subs)
	next[len(subs)] = sub
	b.subscribers[eventType] = next

	// Lazily start the consumer goroutine.
	if _, exists := b.queues[eventType]; !exists {
		tq := &typeQueue{
			signal: make(chan struct{}, 1),
			done:   make(chan struct{}),
		}
		b.queues[eventType] = tq
		b.wg.Add(1)
		go b.consumeLoop(tq, eventType)
	}
	b.mu.Unlock()

	b.instrumenter.IncSubscriberCount(eventType)

	return &subscription{
		id: sub.id,
		cancel: func() {
			// Mark canceled first so a concurrent dispatch skips this
			// subscriber immediately, even before the slice is pruned.
			// CompareAndSwap makes Cancel idempotent: a second call is a
			// no-op, which also keeps the subscriber counter consistent
			// (the previous Store-based implementation decremented the
			// counter on every call, drifting negative on repeat cancels).
			if !sub.canceled.CompareAndSwap(false, true) {
				return
			}
			// Remove the subscriber from the list under the write lock
			// so short-lived subscriptions do not accumulate indefinitely.
			// Without this prune the list would grow without bound and
			// hold references to handler closures, preventing GC.
			// Copy-on-write: build a fresh slice so concurrent dispatch
			// loops iterating the previous snapshot stay safe.
			//
			// The per-type consumer goroutine is intentionally NOT shut
			// down when the last subscriber cancels: subscriptions are
			// static for the process lifetime (wired at startup), and a
			// parked consumer on an empty queue costs nothing.
			b.mu.Lock()
			subs := b.subscribers[eventType]
			next := make([]*subscriber, 0, len(subs))
			for _, s := range subs {
				if s != sub {
					next = append(next, s)
				}
			}
			b.subscribers[eventType] = next
			if len(b.subscribers[eventType]) == 0 {
				delete(b.subscribers, eventType)
			}
			b.mu.Unlock()
			b.instrumenter.DecSubscriberCount(eventType)
		},
	}, nil
}

// subscription is the concrete implementation of the Subscription interface.
type subscription struct {
	id     string
	cancel func()
}

// ID returns the unique identifier of the subscription.
func (s *subscription) ID() string {
	return s.id
}

// Cancel cancels the subscription; no further events will be delivered after cancellation.
func (s *subscription) Cancel() {
	if s.cancel != nil {
		s.cancel()
	}
}

// consumeLoop is the main loop of the consumer goroutine for each event type.
// It waits for a signal, then pops events from the priority queue and dispatches them.
// On receiving the done signal it drains the queue and exits.
func (b *channelBus) consumeLoop(tq *typeQueue, eventType Type) {
	defer b.wg.Done()
	for {
		select {
		case <-tq.done:
			b.drainQueue(tq, eventType)
			return
		case <-tq.signal:
			b.drainQueue(tq, eventType)
		}
	}
}

// drainQueue pops all pending events from the priority queue and dispatches them.
func (b *channelBus) drainQueue(tq *typeQueue, eventType Type) {
	for {
		tq.mu.Lock()
		if tq.pq.Len() == 0 {
			tq.mu.Unlock()
			return
		}
		item := heap.Pop(&tq.pq).(*queueItem) //nolint:errcheck // queue only stores *queueItem
		tq.mu.Unlock()

		// item.ctx is always set at enqueue time (context.WithoutCancel);
		// cancellation was stripped precisely so the consumer can deliver it
		// here, long after the publisher returned.
		b.dispatch(item.ctx, eventType, *item.envelope)
		releaseEnvelope(item.envelope)
	}
}

// dispatch delivers the event envelope to all active subscribers of the given event type.
func (b *channelBus) dispatch(ctx context.Context, eventType Type, env Envelope) {
	b.mu.RLock()
	// Subscriber lists are copy-on-write (see Subscribe/Cancel): the slice
	// stored in the map is never mutated after publication, so iterating it
	// after releasing the read lock is safe with no per-event copy.
	subs := b.subscribers[eventType]
	b.mu.RUnlock()

	for _, sub := range subs {
		if sub.canceled.Load() {
			continue
		}
		b.callHandler(ctx, sub, eventType, env)
	}
}

// callHandler executes the Handler invocation chain for a single subscriber:
// timeout control -> exponential backoff retry -> panic recovery -> Handler execution.
func (b *channelBus) callHandler(ctx context.Context, sub *subscriber, eventType Type, env Envelope) {
	// Determine the timeout: the subscription's override, or the package default.
	timeout := sub.config.timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}

	// Retry config.
	maxRetries := sub.config.maxRetries
	baseBackoff := sub.config.baseBackoff

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			b.instrumenter.IncRetry(eventType, sub.id)
			if baseBackoff > 0 {
				// The dispatch context is never cancelled (enqueue strips
				// publisher cancellation via context.WithoutCancel), so the
				// backoff is a plain sleep; hung handlers are bounded by the
				// per-attempt WithTimeout above.
				time.Sleep(baseBackoff * time.Duration(1<<uint(attempt-1)))
			}
		}

		callCtx, cancel := context.WithTimeout(ctx, timeout)
		start := time.Now()
		err := b.call(callCtx, sub, eventType, env)
		cancel()
		elapsed := time.Since(start)
		b.instrumenter.ObserveHandlerDuration(eventType, sub.id, elapsed)

		if err == nil {
			return
		}
		lastErr = err
	}

	// All retries failed.
	b.handleFailedEvent(eventType, env, lastErr)
}

// call invokes the Handler safely, recovering from panics and recording metrics and logs.
func (b *channelBus) call(ctx context.Context, sub *subscriber, eventType Type, env Envelope) (err error) {
	defer func() {
		if r := recover(); r != nil {
			b.instrumenter.IncHandlerPanic(eventType, sub.id)
			b.logger.Error("handler panic recovered",
				zap.String("event_type", string(eventType)),
				zap.String("event_id", env.EventID),
				zap.String("tenant_id", env.TenantID),
				zap.String("handler_id", sub.id),
				zap.Any("panic", r),
				zap.Stack("stack"),
			)
			err = fmt.Errorf("event: handler panic: %v", r)
		}
	}()
	return sub.handler(ctx, env)
}

// handleFailedEvent processes events whose retries have all failed: it logs and persists them to the FailedEventStore.
func (b *channelBus) handleFailedEvent(eventType Type, env Envelope, err error) {
	b.logger.Error("handler failed after retries",
		zap.String("event_type", string(eventType)),
		zap.String("event_id", env.EventID),
		zap.String("tenant_id", env.TenantID),
		zap.Error(err),
	)
	if b.failedStore != nil {
		if saveErr := b.failedStore.Save(context.Background(), env, err); saveErr != nil {
			b.logger.Error("failed to save failed event",
				zap.String("event_type", string(eventType)),
				zap.String("event_id", env.EventID),
				zap.Error(saveErr),
			)
		}
	}
}

// Close gracefully shuts down the bus.
// It marks the bus as closed -> closes all done channels -> waits for all consumer goroutines
// to drain their queues and exit.
func (b *channelBus) Close() error {
	if !b.closed.CompareAndSwap(false, true) {
		return nil
	}

	b.mu.Lock()
	for _, tq := range b.queues {
		close(tq.done)
	}
	b.mu.Unlock()

	b.wg.Wait()
	return nil
}
