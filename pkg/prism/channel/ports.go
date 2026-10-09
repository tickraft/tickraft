// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package channel

import (
	"context"

	"github.com/tickraft/tickraft/pkg/pagination"
	"github.com/tickraft/tickraft/pkg/prism/alert"
)

// Service defines the operations for managing notification channels and
// their delivery records. Channel configurations returned by List, Get,
// Create, and Update carry masked sensitive fields; the plaintext config
// never crosses this boundary.
//
// The concrete implementation is injected via the WithChannelService
// RouteOption. The default ChannelService persists to sys_prism_channel
// directly; multi-tenant editions inject their own tenant-scoped
// implementation with the same wire contract.
type Service interface {
	// ListChannels returns all channel configurations with sensitive
	// config fields masked.
	ListChannels(ctx context.Context) ([]*Channel, error)
	// GetChannel returns a single channel configuration with sensitive
	// config fields masked.
	GetChannel(ctx context.Context, id int64) (*Channel, error)
	// CreateChannel creates a new channel configuration and returns the
	// masked echo.
	CreateChannel(ctx context.Context, req *CreateRequest) (*Channel, error)
	// UpdateChannel applies a partial update to the channel identified by
	// id. Omitted fields keep their stored values; sensitive fields
	// submitted empty or still masked keep the stored plaintext. Returns
	// the masked echo.
	UpdateChannel(ctx context.Context, id int64, req *UpdateRequest) (*Channel, error)
	// DeleteChannel removes the channel identified by id.
	DeleteChannel(ctx context.Context, id int64) error
	// TestChannel sends a test alert notification, either through a saved
	// channel (req.ID) or an inline type+config pair. A nil error means
	// the test notification was delivered successfully.
	TestChannel(ctx context.Context, req *TestRequest) error
	// TestAllChannels probes every enabled saved channel concurrently
	// with a synthetic alert and returns one result per channel,
	// ordered by channel id. It is the batch connectivity-diagnosis
	// companion of TestChannel.
	TestAllChannels(ctx context.Context) ([]TestResult, error)
	// ListChannelOptions returns the compact id/name/type/enabled
	// projection of every channel, used by filter dropdowns.
	ListChannelOptions(ctx context.Context) ([]Option, error)
	// ListDeliveries returns delivery records matching params together
	// with the total count (offset pagination).
	ListDeliveries(ctx context.Context, params DeliveryListParams) ([]DeliveryRecord, int64, error)
	// ListDeliveriesKeyset returns a page of delivery records using
	// keyset pagination.
	ListDeliveriesKeyset(ctx context.Context, params DeliveryListParams) (pagination.PageResult[DeliveryRecord], error)
	// RetryDelivery replays a failed delivery through the channel's
	// current configuration and returns the updated record. The record's
	// Status reflects the retry outcome, so a returned record with status
	// "failed" means the retry ran but the channel rejected the message
	// again.
	RetryDelivery(ctx context.Context, id int64) (*DeliveryRecord, error)
}

// Runtime is the seam through which the channel service reaches the prism
// runtime: hot-reloading the engine's in-memory channel list after
// mutations, and building a runtime alert.Channel from a persisted row for
// test dispatches and delivery retries. The prism engine satisfies it;
// defining the interface here keeps this package independent of pkg/prism
// (which imports it). A nil Runtime disables hot reload and test dispatch.
type Runtime interface {
	// ReloadChannels rebuilds the engine's in-memory channel list from the
	// persisted enabled channels.
	ReloadChannels(ctx context.Context) error
	// BuildChannel constructs a runtime alert.Channel from a persisted
	// channel definition without the delivery-tracking decorator; the
	// caller owns recording the outcome.
	BuildChannel(ch *Channel) (alert.Channel, error)
	// BuildTrackedChannel constructs a runtime alert.Channel wrapped with
	// the delivery-tracking decorator, so dispatches through it record
	// sys_prism_delivery rows. Used by test dispatches, which (unlike
	// retries) do not self-record their outcome.
	BuildTrackedChannel(ch *Channel) (alert.Channel, error)
}
