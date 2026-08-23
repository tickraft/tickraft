// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package channel

import (
	"context"

	"github.com/tickraft/tickraft/pkg/prism/channel"
)

// The wire and storage shapes are the same model: this package carries no
// Channel DTO. prismchannel.Channel holds both gorm and json tags, and the
// soft-delete column serializes to nothing, so handlers bind and return the
// model type directly. See docs/model-layering-design.md for the layering
// contract.

// Service defines the operations for managing notification channels.
// The concrete implementation is injected via the WithChannelService
// RouteOption; when omitted, the handler package falls back to an in-memory
// implementation.
type Service interface {
	// ListChannels returns a page of notification channels and the total count.
	ListChannels(ctx context.Context, page, size int) ([]*channel.Channel, int64, error)
	// GetChannel returns a single notification channel by ID.
	GetChannel(ctx context.Context, id int64) (*channel.Channel, error)
	// CreateChannel creates a new notification channel from the given request.
	CreateChannel(ctx context.Context, req *channel.Channel) (*channel.Channel, error)
	// UpdateChannel updates an existing notification channel identified by ID.
	UpdateChannel(ctx context.Context, id int64, req *channel.Channel) (*channel.Channel, error)
	// DeleteChannel deletes a notification channel by ID.
	DeleteChannel(ctx context.Context, id int64) error
	// TestChannel sends a test notification through the channel identified by
	// ID and returns an error describing any delivery failure. A nil error
	// means the test notification was delivered successfully.
	TestChannel(ctx context.Context, id int64) error
}
