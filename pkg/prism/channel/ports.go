// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package channel

import (
	"context"

	"github.com/tickraft/tickraft/pkg/prism/alert"
)

// Service defines the operations for managing notification channels.
// The wire and storage shapes are the same model: there is no Channel
// DTO. Channel holds both gorm and json tags, and the soft-delete column
// serializes to nothing, so handlers bind and return the model type
// directly. See docs/model-layering-design.md for the layering contract.
//
// The concrete implementation is injected via the WithChannelService
// RouteOption; when omitted, the handler package falls back to an in-memory
// implementation.
type Service interface {
	// ListChannels returns a page of notification channels and the total count.
	ListChannels(ctx context.Context, page, size int) ([]*Channel, int64, error)
	// GetChannel returns a single notification channel by ID.
	GetChannel(ctx context.Context, id int64) (*Channel, error)
	// CreateChannel creates a new notification channel from the given request.
	CreateChannel(ctx context.Context, req *Channel) (*Channel, error)
	// UpdateChannel updates an existing notification channel identified by ID.
	UpdateChannel(ctx context.Context, id int64, req *Channel) (*Channel, error)
	// DeleteChannel deletes a notification channel by ID.
	DeleteChannel(ctx context.Context, id int64) error
	// TestChannel sends a test notification through the channel identified by
	// ID and returns an error describing any delivery failure. A nil error
	// means the test notification was delivered successfully.
	TestChannel(ctx context.Context, id int64) error
}

// Runtime is the seam through which the channel service reaches the prism
// runtime: hot-reloading the engine's in-memory channel list after
// mutations, and building a runtime alert.Channel from a persisted row for
// test dispatches. The prism engine satisfies it; defining the interface
// here keeps this package independent of pkg/prism (which imports it).
// A nil Runtime disables hot reload and test dispatch.
type Runtime interface {
	// ReloadChannels rebuilds the engine's in-memory channel list from the
	// persisted enabled channels.
	ReloadChannels(ctx context.Context) error
	// BuildChannel constructs a runtime alert.Channel from a persisted
	// channel definition.
	BuildChannel(ch *Channel) (alert.Channel, error)
}
