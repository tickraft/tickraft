// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package event

import (
	"container/heap"
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math/rand/v2"
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
	logger         *zap.Logger
	bufferSize     int
	defaultTimeout time.Duration
	failedStore    FailedEventStore
	instrumenter   Instrumenter
	debug          bool

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
	// ctx preserves the publisher's context so the consumer loop
	// delivers the event with the publisher's deadline/cancellation
	// rather than a detached context.Background().
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
	env.Type = ""
	env.Payload = nil
	env.Timestamp = time.Time{}
	env.Priority = 0
	env.EventID = ""
	env.TenantID = ""
	env.Metadata = nil
	envelopePool.Put(env)
}

// generateEventID generates a unique event identifier.
func generateEventID() string {
	b := make([]byte, 16)
	if _, err := cryptorand.Read(b); err != nil {
		return fmt.Sprintf("evt-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// Publish publishes an event to the bus.
// The default mode is asynchronous: events are pushed onto the priority queue and dispatched by the consumer goroutine.
// The WithSync option switches to synchronous mode, where the publisher blocks until all Handlers finish.
func (b *channelBus) Publish(ctx context.Context, eventType Type, payload any, opts ...PublishOption) error {
	if b.closed.Load() {
		return ErrBusClosed
	}

	cfg := &publishConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	env := acquireEnvelope()
	env.Type = eventType
	env.Payload = payload
	env.Timestamp = time.Now()
	env.Priority = cfg.priority
	if cfg.eventID != "" {
		env.EventID = cfg.eventID
	} else {
		env.EventID = generateEventID()
	}
	env.TenantID = cfg.tenantID
	env.Metadata = cfg.metadata

	b.instrumenter.IncPublish(eventType, cfg.tenantID)

	if b.debug {
		b.logger.Debug("event published",
			zap.String("event_type", string(eventType)),
			zap.String("event_id", env.EventID),
			zap.String("tenant_id", env.TenantID),
			zap.Int("priority", env.Priority),
			zap.Bool("sync", cfg.sync),
		)
	}

	if cfg.sync {
		b.dispatch(ctx, eventType, *env)
		releaseEnvelope(env)
		return nil
	}

	// Asynchronous mode: push onto the priority queue.
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
	if tq.pq.Len() >= b.bufferSize {
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
		ctx:      ctx,
	})
	tq.mu.Unlock()

	if b.debug {
		b.logger.Debug("event enqueued",
			zap.String("event_type", string(eventType)),
			zap.String("event_id", env.EventID),
		)
	}

	// Notify the consumer goroutine.
	select {
	case tq.signal <- struct{}{}:
	default:
	}

	return nil
}

// Subscribe registers a subscriber and returns a Subscription.
// The first time a subscriber is registered for an event type, the consumer goroutine for that type is lazily started.
func (b *channelBus) Subscribe(eventType Type, handler Handler, opts ...SubscribeOption) (Subscription, error) {
	if b.closed.Load() {
		return nil, ErrBusClosed
	}

	cfg := &subscribeConfig{}
	for _, opt := range opts {
		opt(cfg)
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
	b.subscribers[eventType] = append(b.subscribers[eventType], sub)

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
			// Remove the subscriber from the slice under the write lock
			// so short-lived subscriptions do not accumulate indefinitely.
			// Without this prune the slice would grow without bound and
			// hold references to handler closures, preventing GC. Swap with
			// the last element and truncate to achieve O(1) removal.
			b.mu.Lock()
			subs := b.subscribers[eventType]
			for i, s := range subs {
				if s == sub {
					last := len(subs) - 1
					subs[i] = subs[last]
					subs[last] = nil
					b.subscribers[eventType] = subs[:last]
					break
				}
			}
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

		env := item.envelope
		if item.ctx != nil {
			b.dispatch(item.ctx, eventType, *env)
		} else {
			b.dispatch(context.Background(), eventType, *env)
		}
		releaseEnvelope(env)
	}
}

// dispatch delivers the event envelope to all active subscribers of the given event type.
func (b *channelBus) dispatch(ctx context.Context, eventType Type, env Envelope) {
	b.mu.RLock()
	// Snapshot the subscriber slice under the read lock so a concurrent
	// Cancel (or Subscribe) mutating the underlying array cannot race
	// with iteration. The copy is shallow: only the slice header and
	// pointer array are copied, not the subscriber structs themselves.
	src := b.subscribers[eventType]
	subs := make([]*subscriber, len(src))
	copy(subs, src)
	b.mu.RUnlock()

	for _, sub := range subs {
		if sub.canceled.Load() {
			continue
		}
		b.callHandler(ctx, sub, eventType, env)
	}
}

// callHandler executes the Handler invocation chain for a single subscriber:
// filter check -> timeout control -> exponential backoff retry -> panic recovery -> Handler execution.
func (b *channelBus) callHandler(ctx context.Context, sub *subscriber, eventType Type, env Envelope) {
	// Filter check.
	if sub.config.filter != nil && !sub.config.filter(env) {
		return
	}

	// Determine the timeout.
	timeout := sub.config.timeout
	if timeout == 0 {
		timeout = b.defaultTimeout
	}

	// Retry config.
	maxRetries := sub.config.maxRetries
	baseBackoff := sub.config.baseBackoff

	if b.debug {
		b.logger.Debug("handler start",
			zap.String("event_type", string(eventType)),
			zap.String("event_id", env.EventID),
			zap.String("handler_id", sub.id),
		)
	}

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			b.instrumenter.IncRetry(eventType, sub.id)
			if baseBackoff > 0 {
				exponential := baseBackoff * time.Duration(1<<uint(attempt-1))
				backoff := exponential
				if sub.config.jitter > 0 {
					scale := 1.0 - sub.config.jitter + sub.config.jitter*rand.Float64()
					backoff = time.Duration(float64(exponential) * scale)
				}
				select {
				case <-time.After(backoff):
				case <-ctx.Done():
					lastErr = ctx.Err()
					b.handleFailedEvent(eventType, env, lastErr)
					return
				}
			}
		}

		callCtx, cancel := context.WithTimeout(ctx, timeout)
		start := time.Now()
		err := b.call(callCtx, sub, eventType, env)
		cancel()
		elapsed := time.Since(start)
		b.instrumenter.ObserveHandlerDuration(eventType, sub.id, elapsed)

		if b.debug {
			level := "completed"
			if err != nil {
				level = "failed"
			}
			b.logger.Debug("handler "+level,
				zap.String("event_type", string(eventType)),
				zap.String("event_id", env.EventID),
				zap.String("handler_id", sub.id),
				zap.Float64("duration_ms", float64(elapsed.Microseconds())/1000.0),
				zap.Int("attempt", attempt),
			)
		}

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
// It marks the bus as closed -> closes all done channels -> waits for all consumer goroutines to drain their queues and exit.
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
