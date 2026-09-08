// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package tracking implements a decorator that wraps an alert.Channel
// to record delivery results (success or failure) to a persistent store,
// without modifying the open-source prism engine.
//
// The decorator is transparent: it preserves the exact return-value
// semantics of the wrapped channel's Send method. A failure to persist
// the delivery record is logged as a warning but never propagated to the
// caller, so observability tracking can never alter the alerting
// pipeline's behaviour.
//
// Beyond the binary outcome, the decorator measures the send duration,
// extracts the HTTP response code when the channel error exposes one,
// renders the alert title through the shared format pipeline, and
// persists the full alert event so failed deliveries can be replayed by
// the retry endpoint.
package tracking

import (
	"context"
	"errors"
	"time"

	"github.com/bytedance/sonic"
	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/prism/alert"

	"github.com/tickraft/tickraft/pkg/prism/channel/format"
)

// DeliveryStatus is the outcome of a channel delivery attempt.
type DeliveryStatus string

const (
	// StatusSuccess indicates the wrapped channel delivered the alert
	// without error.
	StatusSuccess DeliveryStatus = "success"
	// StatusFailed indicates the wrapped channel returned an error while
	// attempting to deliver the alert.
	StatusFailed DeliveryStatus = "failed"
)

// Identity carries the persisted channel-config identity into delivery
// records so a record can be correlated back to its configuration row
// (for filtering, display, and retry).
type Identity struct {
	// ChannelID is the sys_prism_channel row ID. It is zero for
	// channels built from environment variables, which have no row.
	ChannelID int64
	// ChannelName is the display name of the channel configuration.
	ChannelName string
	// ChannelType is the channel type key ("feishu", "slack", ...).
	ChannelType string
}

// DeliveryRecord is the persisted representation of a single channel
// delivery attempt. It is produced by the tracking decorator after
// every Send call and handed to the DeliveryRecordStore.
type DeliveryRecord struct {
	// Identity identifies the channel configuration that attempted the
	// delivery.
	Identity
	// AlertType is the alert category, derived from alert.Type.
	AlertType string
	// AlertTitle is the rendered alert headline (format.Message.Title).
	// It is empty when no formatter is injected into the decorator.
	AlertTitle string
	// EventID is the alert event's tracking identifier (alert.EventID),
	// stable for a single Dispatch call.
	EventID string
	// Status is the delivery outcome (StatusSuccess or StatusFailed).
	Status DeliveryStatus
	// Error is the error message returned by the wrapped channel. It is
	// empty on success.
	Error string
	// ResponseCode is the HTTP status code reported by the channel error
	// when it exposes one (all webhook adapters do). Zero means success,
	// a network error, or a channel without an HTTP transport.
	ResponseCode int
	// DurationMs is the wall-clock duration of the Send call.
	DurationMs int64
	// Event is the full alert event. The store serializes it so the
	// retry endpoint can replay the exact payload.
	Event alert.Event
	// SentAt is the time at which the delivery attempt was recorded.
	SentAt time.Time
}

// DeliveryRecordStore is the consumer-defined interface that persists
// delivery records. Implementations (typically backed by a database) are
// injected into the tracking decorator at construction time.
type DeliveryRecordStore interface {
	// Record persists a single delivery record. The returned error is
	// logged by the decorator but never propagated to the caller of Send.
	Record(ctx context.Context, rec DeliveryRecord) error
}

// statusCodeCarrier is implemented by the channel adapters' internal
// error types so the decorator can extract the HTTP response code.
type statusCodeCarrier interface {
	StatusCode() int
}

// ResponseCodeOf extracts the HTTP status code from a channel error when
// the underlying error type exposes one. It returns zero for nil errors,
// network errors, and channels without an HTTP transport.
func ResponseCodeOf(err error) int {
	if err == nil {
		return 0
	}
	var sc statusCodeCarrier
	if errors.As(err, &sc) {
		return sc.StatusCode()
	}
	return 0
}

// Channel is a decorator that wraps an alert.Channel and records the
// outcome of every Send call to a DeliveryRecordStore. It satisfies the
// alert.Channel interface.
type Channel struct {
	inner  alert.Channel
	id     Identity
	store  DeliveryRecordStore
	render format.RenderOptions
	logger *zap.Logger
}

// Compile-time assertion that Channel implements alert.Channel.
var _ alert.Channel = (*Channel)(nil)

// New returns a tracking decorator that wraps inner and persists every
// delivery result to store. id identifies the channel configuration the
// wrapper records on each delivery; render supplies the formatter used
// to render the alert title (zero-value options disable title rendering).
// If logger is nil, a no-op logger is used so the decorator is safe to
// construct without explicit logging configuration.
func New(
	inner alert.Channel,
	store DeliveryRecordStore,
	logger *zap.Logger,
	id Identity,
	render format.RenderOptions,
) *Channel {
	if logger == nil {
		logger = zap.NewNop()
	}
	if render.Logger == nil {
		render.Logger = logger
	}
	return &Channel{
		inner:  inner,
		id:     id,
		store:  store,
		render: render,
		logger: logger,
	}
}

// Name implements alert.Channel by transparently delegating to the
// wrapped channel.
func (c *Channel) Name() string {
	return c.inner.Name()
}

// Send implements alert.Channel. It delegates the actual delivery to the
// wrapped channel and then records the outcome to the store.
//
// The return value is always the error returned by the wrapped channel
// (nil on success). A failure to persist the delivery record is logged
// as a warning and never returned, so tracking can never change the
// semantics of the underlying channel.
func (c *Channel) Send(ctx context.Context, evt alert.Event) error {
	start := time.Now()
	err := c.inner.Send(ctx, evt)
	duration := time.Since(start)

	rec := DeliveryRecord{
		Identity:     c.id,
		AlertType:    string(evt.Type),
		EventID:      evt.EventID,
		Status:       StatusFailed,
		Error:        "",
		ResponseCode: ResponseCodeOf(err),
		DurationMs:   duration.Milliseconds(),
		Event:        evt,
		SentAt:       start,
	}
	if err == nil {
		rec.Status = StatusSuccess
	} else {
		rec.Error = err.Error()
	}

	// Title rendering is best-effort: without an injected formatter the
	// record keeps an empty title and the list falls back to the type.
	if c.render.Formatter != nil || c.render.Library != nil {
		if msg := format.Render(ctx, evt, c.render); msg.Title != "" {
			rec.AlertTitle = msg.Title
		}
	}

	if recordErr := c.store.Record(ctx, rec); recordErr != nil {
		c.logger.Warn("failed to record delivery",
			zap.Error(recordErr),
			zap.String("channel", rec.ChannelName),
			zap.String("alert_type", rec.AlertType),
			zap.String("status", string(rec.Status)),
		)
	}

	return err
}

// MarshalEvent serializes an alert event for storage in a delivery
// record's request payload column. It is used by the store layer so the
// JSON encoding of the persisted event stays in one place.
func MarshalEvent(evt alert.Event) ([]byte, error) {
	return sonic.Marshal(evt)
}
