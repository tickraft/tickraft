// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package prism

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/tickraft/tickraft/pkg/event"
	"github.com/tickraft/tickraft/pkg/pool"
	"github.com/tickraft/tickraft/pkg/prism/alert"
	"github.com/tickraft/tickraft/pkg/prism/governance"
	"go.uber.org/zap"
)

// DispatchResult is the structured outcome of a synchronous Dispatch call.
type DispatchResult struct {
	// Accepted indicates whether the alert was accepted for processing,
	// i.e. at least one rule matched or no rules are registered
	// (default-allow).
	Accepted bool
	// EventID is the deduplication / tracking identifier assigned to the
	// alert by the engine. It is stable for a single Dispatch call, stamped
	// on the Event before any channel or callback observes it, and
	// suitable for correlating with delivery records.
	EventID string
	// MatchedRules lists the names of rules that matched the alert. Rules
	// that do not implement NamedMatcher are counted toward the accept
	// decision but omitted from this slice.
	MatchedRules []string
	// DispatchedChannels lists the channel names that received a
	// notification dispatch request. Channel sends happen asynchronously
	// via the worker pool; this slice reflects the channels the engine
	// submitted to the pool, not the channels that have completed
	// delivery.
	DispatchedChannels []string
	// Message is a short human-readable acknowledgement message intended
	// for callers.
	Message string
}

// dispatch is the event-bus subscription callback. It delegates to Dispatch
// and discards the result so the existing fire-and-forget semantics are
// preserved.
func (e *Engine) dispatch(ctx context.Context, evt alert.Event) {
	// Dispatch returns a DispatchResult carrying observability metadata (matched
	// rules, dispatched channels). The event-bus callback has no caller to
	// surface this to, and Dispatch already logs every relevant outcome
	// (suppression, channel send failure, onAlert error) via zap, so the result
	// is intentionally not captured here.
	e.Dispatch(ctx, evt)
}

// Dispatch synchronously evaluates the alert against the registered rules
// and, if it matches (or no rules are registered), submits notification
// jobs to the worker pool for each registered channel. It returns a
// DispatchResult carrying the matched rule names and the dispatched channel
// names. Channel sends happen asynchronously via the worker pool; this
// method returns as soon as the jobs are submitted (or the alert is
// suppressed).
//
// Before rule evaluation, the engine invokes the registered governance.Middleware
// chain in order. The first middleware that returns governance.DecisionSuppress or
// governance.DecisionAggregate short-circuits the chain: the alert is still recorded
// (the OnAlert callback is invoked) but notification channels are skipped.
// In an single-process deployment the chain is empty, so
// Dispatch proceeds directly to rule evaluation.
//
// The engine assigns a unique EventID to the alert at the start of Dispatch
// and stamps it on the Event before any channel or callback observes
// it, so the same identifier flows through rules, the OnAlert callback,
// every channel Send, and the returned DispatchResult.
//
// All registered rules are evaluated (no short-circuit) so that every
// matching rule name is collected for the response. This differs from the
// historical fire-and-forget dispatch path which broke on the first match;
// the dispatch decision (any-match) is unchanged. Each rule.Match call is
// wrapped with panic recovery so a buggy custom Matcher cannot crash the
// engine; a panicking rule is logged and treated as not matching.
func (e *Engine) Dispatch(ctx context.Context, evt alert.Event) DispatchResult {
	// Defensive check: ensure the event has at least one violation.
	// When Violations is empty, initialize a default violation based on
	// the event type so that downstream consumers (channels, governance)
	// do not receive an empty violation list.
	if len(evt.Violations) == 0 {
		evt.Violations = []alert.Violation{{
			Kind: string(evt.Type),
		}}
	}

	eventID := newEventID()
	evt.EventID = eventID

	rules := e.rulesSnapshot()
	channels := e.channelsSnapshot()
	guards := e.guardsSnapshot()

	// Governance guard chain: invoked before rule evaluation. The
	// first non-Pass decision short-circuits the chain. Suppressed and
	// aggregated alerts are still recorded (OnAlert) but skip channel
	// dispatch. default deployments pass an empty chain, so this loop
	// is a no-op and Dispatch falls through to rule evaluation.
	for _, g := range guards {
		decision := process(ctx, g, &evt, e.logger)
		switch decision {
		case governance.DecisionSuppress:
			e.recordAlert(ctx, evt, eventID)
			e.logger.Debug("alert suppressed by governance guard",
				zap.String("event_id", eventID),
				zap.String("guard", guardName(g)),
				zap.String("type", string(evt.Type)),
				zap.Int64("asset_id", evt.AssetID),
			)
			return DispatchResult{
				Accepted: false,
				EventID:  eventID,
				Message:  "alert suppressed by governance guard",
			}
		case governance.DecisionAggregate:
			e.recordAlert(ctx, evt, eventID)
			e.logger.Debug("alert aggregated by governance guard",
				zap.String("event_id", eventID),
				zap.String("guard", guardName(g)),
				zap.String("type", string(evt.Type)),
				zap.Int64("asset_id", evt.AssetID),
			)
			return DispatchResult{
				Accepted: false,
				EventID:  eventID,
				Message:  "alert aggregated by governance guard",
			}
		}
	}

	// All governance guards passed (DecisionPass). Invoke the post-guard
	// hook so the callers can notify the Suppressor about active
	// source alerts before rule evaluation. The hook is nil in default
	// deployments, so this is a no-op there.
	postGuardHook(ctx, e.postGuardHook, &evt, e.logger)

	matchedRules := make([]string, 0)
	matched := len(rules) == 0
	// Collect structured violations from any matched rule that implements
	// ViolationMatcher. When a compound rule (e.g. "cpu > 90 && mem > 85")
	// matches multiple conditions, each condition contributes one Violation.
	// When violations are collected, they replace the single violation
	// populated by the payload converter (metricPayloadToAlert /
	// logPayloadToAlert) so downstream consumers (channels, governance
	// fingerprint, record persistence) see the full set of matched
	// conditions. When no ViolationMatcher rules match or none return
	// violations, the payload-populated Event.Violations are preserved.
	var collectedViolations []alert.Violation
	for _, r := range rules {
		if match(ctx, r, evt, e.logger) {
			matched = true
			if nr, ok := r.(NamedMatcher); ok {
				matchedRules = append(matchedRules, nr.Name())
			}
			if vm, ok := r.(ViolationMatcher); ok {
				collectedViolations = append(collectedViolations,
					matchWithViolations(ctx, vm, evt, e.logger)...)
			}
		}
	}
	if len(collectedViolations) > 0 {
		evt.Violations = collectedViolations
	}
	if !matched {
		e.logger.Debug("alert suppressed by rules",
			zap.String("event_id", eventID),
			zap.String("type", string(evt.Type)),
			zap.Int64("asset_id", evt.AssetID),
		)
		return DispatchResult{
			Accepted: false,
			EventID:  eventID,
			Message:  "alert suppressed by rules",
		}
	}

	// Invoke the OnAlert callback (if registered) so that callers can
	// persist the alert record without the prism package depending on a
	// store. Errors from the callback are logged but do not suppress
	// channel notification.
	e.recordAlert(ctx, evt, eventID)

	dispatchedChannels := make([]string, 0, len(channels))
	if len(channels) == 0 {
		// No channels registered: log the alert so it is still
		// observable in deployments without a configured
		// notification sink.
		primary, _ := evt.PrimaryViolation()
		metricName := ""
		if primary.Metric != nil {
			metricName = primary.Metric.Name
		}
		e.logger.Info("alert received (no channels registered)",
			zap.String("event_id", eventID),
			zap.String("type", string(evt.Type)),
			zap.Int64("asset_id", evt.AssetID),
			zap.Int64("tenant_id", evt.TenantID),
			zap.String("metric_name", metricName),
			zap.String("level", primary.Severity),
		)
		return DispatchResult{
			Accepted:     true,
			EventID:      eventID,
			MatchedRules: matchedRules,
			Message:      "alert accepted; no channels registered",
		}
	}

	for _, ch := range channels {
		c := ch
		dispatchedChannels = append(dispatchedChannels, c.Name())
		e.wg.Add(1)
		job := pool.Lambda(func(jobCtx context.Context) error {
			defer e.wg.Done()
			// Enforce a per-channel send timeout so a slow or unresponsive
			// channel cannot indefinitely occupy a worker pool slot and
			// cause backpressure on the dispatch path. The timeout is
			// applied here (in the pool worker) rather than at the channel
			// implementation so it covers all channel types uniformly.
			sendCtx, cancel := context.WithTimeout(jobCtx, channelSendTimeout)
			defer cancel()
			if err := c.Send(sendCtx, evt); err != nil {
				e.logger.Warn("channel send failed",
					zap.String("event_id", eventID),
					zap.String("channel", c.Name()),
					zap.Error(err),
				)
			}
			return nil
		})
		if err := e.notifyPool.Submit(ctx, job); err != nil {
			e.wg.Done()
			e.logger.Warn("notification pool submit failed, alert dropped",
				zap.String("event_id", eventID),
				zap.String("channel", c.Name()),
				zap.Error(err),
			)
			// Forward to the dead-letter handler if registered so the event
			// can be persisted for retry by a background worker. When no
			// handler is registered the alert is dropped (logged above).
			if e.deadLetterHandler != nil {
				handleDeadLetter(ctx, e.deadLetterHandler, evt, c.Name(), e.logger)
			}
		}
	}

	return DispatchResult{
		Accepted:           true,
		EventID:            eventID,
		MatchedRules:       matchedRules,
		DispatchedChannels: dispatchedChannels,
		Message:            "alert accepted",
	}
}

// onAlert invokes the OnAlert callback with panic recovery so that a
// buggy callback cannot crash the prism engine. The returned error is
// non-nil only when the callback returned an error; panics are recovered,
// logged, and reported as an error so the caller can log them.
func onAlert(ctx context.Context, fn OnAlertFunc, evt alert.Event, logger *zap.Logger) (err error) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("onAlert callback panicked",
				zap.String("type", string(evt.Type)),
				zap.Int64("asset_id", evt.AssetID),
				zap.Any("panic", r),
			)
			err = fmt.Errorf("alert: onAlert callback panicked: %v", r)
		}
	}()
	fn(ctx, evt)
	return nil
}

// recordAlert invokes the OnAlert callback (if registered) so that callers
// can persist the alert record without the prism package depending on a
// store. Errors from the callback are logged but do not suppress channel
// notification. It is called both for governance-suppressed alerts (which are
// still recorded) and for alerts that passed rule evaluation.
func (e *Engine) recordAlert(ctx context.Context, evt alert.Event, eventID string) {
	if e.onAlert == nil {
		return
	}
	if err := onAlert(ctx, e.onAlert, evt, e.logger); err != nil {
		e.logger.Warn("onAlert callback returned error",
			zap.String("event_id", eventID),
			zap.String("type", string(evt.Type)),
			zap.Int64("asset_id", evt.AssetID),
			zap.Error(err),
		)
	}
}

// process invokes a governance.Guard.Process with panic recovery so
// that a buggy guard cannot crash the engine. A panic is recovered,
// logged, and treated as governance.DecisionPass so the alert is not silently swallowed
// by a faulty guard.
func process(ctx context.Context, g governance.Guard, evt *alert.Event, logger *zap.Logger) (decision governance.Decision) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("governance guard panicked",
				zap.String("type", string(evt.Type)),
				zap.Int64("asset_id", evt.AssetID),
				zap.Any("panic", r),
			)
			decision = governance.DecisionPass
		}
	}()
	return g.Process(ctx, evt)
}

// postGuardHook invokes the PostGuardHook callback with panic recovery so
// that a buggy hook cannot crash the engine. A panic is recovered, logged at
// error level, and the dispatch continues — the hook's side effect (e.g.
// suppression notification) is lost but the alert itself is not.
func postGuardHook(ctx context.Context, hook PostGuardHook, evt *alert.Event, logger *zap.Logger) {
	if hook == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			logger.Error("postGuardHook panicked",
				zap.String("event_id", evt.EventID),
				zap.String("type", string(evt.Type)),
				zap.Int64("asset_id", evt.AssetID),
				zap.Any("panic", r),
			)
		}
	}()
	hook(ctx, evt)
}

// handleDeadLetter invokes the DeadLetterHandler with panic recovery so a
// buggy handler cannot crash the engine. The event is already lost from the
// notification path, so a panic here is logged and swallowed.
func handleDeadLetter(ctx context.Context, handler DeadLetterHandler, evt alert.Event, channelName string, logger *zap.Logger) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("deadLetterHandler panicked",
				zap.String("event_id", evt.EventID),
				zap.String("channel", channelName),
				zap.Any("panic", r),
			)
		}
	}()
	handler.HandleDeadLetter(ctx, evt, channelName)
}

// guardName returns a human-readable name for a governance guard
// for logging. It uses a type-naming fallback when the guard does not
// expose a Name method.
func guardName(g governance.Guard) string {
	if namer, ok := g.(interface{ Name() string }); ok {
		return namer.Name()
	}
	return fmt.Sprintf("%T", g)
}

// newEventID generates a unique identifier for a dispatched alert event.
// It uses a monotonically increasing counter combined with the current
// timestamp and a per-process random prefix, producing IDs that are unique
// across process restarts and concurrent goroutines without the syscall
// overhead of crypto/rand.
func newEventID() string {
	ts := time.Now().UnixNano()
	seq := atomic.AddUint64(&eventIDCounter, 1)
	return fmt.Sprintf("%x-%x", ts, seq)
}

// eventIDCounter is a process-wide monotonic counter ensuring uniqueness
// of event IDs across concurrent Dispatch calls within the same process.
// It is seeded with a random offset at init time so IDs from different
// process instances are unlikely to collide even if they start at the
// same nanosecond.
var eventIDCounter uint64

func init() {
	// Seed the counter with a random offset so IDs from different process
	// instances are unlikely to collide even if they start at the same nanosecond.
	// crypto/rand is used here only once at init, not on the hot path.
	var seed [8]byte
	_, _ = rand.Read(seed[:])
	eventIDCounter = binary.BigEndian.Uint64(seed[:])
}

// metricPayloadToAlert converts a typed metric alert event into the
// normalized Event consumed by channels.
func metricPayloadToAlert(ev event.Event[event.MetricExceededPayload]) alert.Event {
	p := ev.Payload
	ts := ev.Timestamp
	if ts.IsZero() {
		ts = time.Now()
	}
	assetID, _ := strconv.ParseInt(p.AssetID, 10, 64)
	tenantID, _ := strconv.ParseInt(p.TenantID, 10, 64)
	severity := p.Severity
	if severity == "" {
		severity = "warning"
	}
	return alert.Event{
		Type:      alert.TypeMetric,
		AssetID:   assetID,
		TenantID:  tenantID,
		Timestamp: ts,
		Violations: []alert.Violation{{
			Kind:     alert.ViolationKindMetric,
			Severity: severity,
			Metric: &alert.MetricContext{
				Name:      p.MetricName,
				Value:     p.MetricValue,
				Threshold: p.Threshold,
				Metrics:   p.Resources,
			},
		}},
	}
}

// logPayloadToAlert converts a typed log alert event into the normalized
// Event consumed by channels.
func logPayloadToAlert(ev event.Event[event.LogMatchedPayload]) alert.Event {
	p := ev.Payload
	ts := ev.Timestamp
	if ts.IsZero() {
		ts = time.Now()
	}
	assetID, _ := strconv.ParseInt(p.AssetID, 10, 64)
	tenantID, _ := strconv.ParseInt(p.TenantID, 10, 64)
	return alert.Event{
		Type:      alert.TypeLog,
		AssetID:   assetID,
		TenantID:  tenantID,
		Timestamp: ts,
		Violations: []alert.Violation{{
			Kind:     alert.ViolationKindLog,
			Severity: mapLogLevel(p.Level),
			Source:   p.SourceIP,
			Log: &alert.LogContext{
				Keyword: p.Keyword,
				Content: p.Content,
			},
		}},
	}
}

// statusPayloadToAlert converts a typed asset status-change event into the
// normalized Event consumed by channels. A transition whose Source is
// "timeout" (heartbeat loss reported by telemetry.MarkOffline) is mapped to
// TypeHeartbeat with ViolationKindHeartbeat; every other transition is mapped
// to TypeStatus with ViolationKindStatus. Transitions to a non-abnormal state
// (healthy/unknown) are skipped by returning ok=false so the engine does not
// emit alert noise for recoveries.
func statusPayloadToAlert(ev event.Event[event.StatusChangePayload]) (alert.Event, bool) {
	p := ev.Payload
	if !isAbnormalStatus(p.CurrStatus) {
		return alert.Event{}, false
	}
	ts := ev.Timestamp
	if ts.IsZero() {
		ts = time.Now()
	}
	assetID, _ := strconv.ParseInt(p.AssetID, 10, 64)
	tenantID, _ := strconv.ParseInt(p.TenantID, 10, 64)

	alertType := alert.TypeStatus
	kind := alert.ViolationKindStatus
	severity := "error"
	message := fmt.Sprintf("asset %s transitioned %s -> %s", p.AssetID, p.PrevStatus, p.CurrStatus)
	if p.Source == "timeout" {
		alertType = alert.TypeHeartbeat
		kind = alert.ViolationKindHeartbeat
		severity = "critical"
		message = fmt.Sprintf("asset %s heartbeat lost, marked offline", p.AssetID)
	}
	if p.Reason != "" {
		message = fmt.Sprintf("%s (%s)", message, p.Reason)
	}

	return alert.Event{
		Type:      alertType,
		AssetID:   assetID,
		TenantID:  tenantID,
		Timestamp: ts,
		Violations: []alert.Violation{{
			Kind:     kind,
			Severity: severity,
			Source:   p.AssetKey,
			Message:  message,
			Status: &alert.StatusContext{
				PrevStatus: p.PrevStatus,
				CurrStatus: p.CurrStatus,
			},
		}},
	}, true
}

// isAbnormalStatus reports whether status represents an alert-worthy
// (non-healthy, non-unknown) asset state. The engine uses it to skip
// recovery transitions so alerts are emitted only for degradations.
func isAbnormalStatus(status string) bool {
	switch status {
	case "offline", "critical", "warning":
		return true
	default:
		return false
	}
}

// mapLogLevel normalizes a raw log level (debug/info/warn/error/fatal) into
// the unified severity scale: critical > error > warning > info > debug.
func mapLogLevel(level string) string {
	switch level {
	case "fatal", "critical":
		return "critical"
	case "error":
		return "error"
	case "warn", "warning":
		return "warning"
	case "info", "notice":
		return "info"
	case "debug":
		return "debug"
	default:
		return level
	}
}
