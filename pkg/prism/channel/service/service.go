// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package service provides the notification channel management contract
// (Service) plus its store-backed implementation, which keeps the prism
// engine's in-memory channel list hot-reloaded on every mutation.
package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/tickraft/tickraft/pkg/errdefs"
	"github.com/tickraft/tickraft/pkg/pagination"
	"github.com/tickraft/tickraft/pkg/prism"
	"github.com/tickraft/tickraft/pkg/prism/alert"
	"github.com/tickraft/tickraft/pkg/prism/channel"
)

// ChannelService implements Service using the prism channel
// store. Mutating operations (Create/Update/Delete) trigger a hot-reload
// of the engine's in-memory channel list via ReloadChannels. The wire
// shape and the storage shape are the same prismchannel.Channel model, so
// this service only orchestrates the store and the engine reload — there
// is no DTO conversion.
var _ Service = (*ChannelService)(nil)

// ChannelService implements Service on top of the prism channel store.
type ChannelService struct {
	channels *channel.Store
	engine   *prism.Engine
}

// NewChannelService creates a ChannelService backed by the given store
// and engine. The engine is used to hot-reload channels after mutations;
// a nil engine disables hot-reload (useful for tests).
func NewChannelService(store *channel.Store, engine *prism.Engine) *ChannelService {
	return &ChannelService{channels: store, engine: engine}
}

// ListChannels returns a page of notification channels and the total count.
func (s *ChannelService) ListChannels(ctx context.Context, page, size int) ([]*channel.Channel, int64, error) {
	page, size = pagination.Clamp(page, size)
	channels, total, err := s.channels.List(ctx, page, size)
	if err != nil {
		return nil, 0, mapChannelStoreError(err)
	}

	return channels, total, nil
}

// GetChannel returns a single notification channel by ID.
func (s *ChannelService) GetChannel(ctx context.Context, id int64) (*channel.Channel, error) {
	ch, err := s.channels.GetByID(ctx, id)
	if err != nil {
		return nil, mapChannelStoreError(err)
	}
	return ch, nil
}

// CreateChannel creates a new notification channel from the given request.
// After a successful insert the engine's channel list is hot-reloaded.
func (s *ChannelService) CreateChannel(ctx context.Context, req *channel.Channel) (*channel.Channel, error) {
	if req == nil {
		return nil, errdefs.ErrInvalidRequest
	}
	if req.Name == "" || req.Type == "" || req.Config == "" {
		return nil, errdefs.ErrInvalidRequest
	}
	// Server-assigned fields are dropped from the request so a client
	// cannot pick its own row ID, timestamps, or usage bookkeeping;
	// Create back-fills ID, CreatedAt, and UpdatedAt after the insert.
	req.ID = 0
	req.CreatedAt = time.Time{}
	req.UpdatedAt = time.Time{}
	req.LastUsedAt = nil
	if err := s.channels.Create(ctx, req); err != nil {
		return nil, mapChannelStoreError(err)
	}
	s.reloadChannels(ctx)
	return req, nil
}

// UpdateChannel updates an existing notification channel identified by ID.
// After a successful update the engine's channel list is hot-reloaded.
func (s *ChannelService) UpdateChannel(
	ctx context.Context,
	id int64,
	req *channel.Channel,
) (*channel.Channel, error) {
	if req == nil {
		return nil, errdefs.ErrInvalidRequest
	}
	existing, err := s.channels.GetByID(ctx, id)
	if err != nil {
		return nil, mapChannelStoreError(err)
	}
	// Server-assigned lifecycle fields and engine-owned usage bookkeeping
	// stay server-owned: the store's column-whitelisted Update never writes
	// them, and restoring them here keeps the echoed response truthful.
	// Tenant and soft-delete columns are not wire-bindable (json:"-").
	req.ID = existing.ID
	req.CreatedAt = existing.CreatedAt
	req.LastUsedAt = existing.LastUsedAt
	if err = s.channels.Update(ctx, req); err != nil {
		return nil, mapChannelStoreError(err)
	}
	s.reloadChannels(ctx)
	return req, nil
}

// DeleteChannel deletes a notification channel by ID. After a successful
// delete the engine's channel list is hot-reloaded.
func (s *ChannelService) DeleteChannel(ctx context.Context, id int64) error {
	if err := s.channels.DeleteByID(ctx, id); err != nil {
		return mapChannelStoreError(err)
	}
	s.reloadChannels(ctx)
	return nil
}

// TestChannel sends a synthetic alert event through the channel identified by
// ID and returns an error describing any delivery failure.
func (s *ChannelService) TestChannel(ctx context.Context, id int64) error {
	m, err := s.channels.GetByID(ctx, id)
	if err != nil {
		return mapChannelStoreError(err)
	}
	ch, err := prism.BuildChannel(m)
	if err != nil {
		return errdefs.NewServiceError(http.StatusBadRequest, errdefs.CodeBadRequest,
			fmt.Sprintf("build channel: %v", err))
	}
	evt := alert.Event{
		Type:      alert.TypeMetric,
		Timestamp: time.Now(),
		Violations: []alert.Violation{
			{
				Kind:    alert.ViolationKindMetric,
				Message: "Test notification from tickraft",
			},
		},
	}
	return ch.Send(ctx, evt)
}

// reloadChannels triggers a hot-reload of the engine's channel list.
// Errors are logged but not returned to the caller, since a reload failure
// does not invalidate the CRUD operation that triggered it.
func (s *ChannelService) reloadChannels(ctx context.Context) {
	if s.engine == nil {
		return
	}
	if err := s.engine.ReloadChannels(ctx); err != nil {
		// Best-effort: log and continue. The CRUD operation succeeded;
		// the engine will pick up the change on next restart.
		_ = err // engine.ReloadChannels already logged the error
	}
}

// mapChannelStoreError translates a channel store error into a handler-level
// ServiceError suitable for the API response layer.
func mapChannelStoreError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, channel.ErrChannelNotFound) {
		return errdefs.ErrChannelNotFound
	}
	if errors.Is(err, errdefs.ErrNotFound) {
		return errdefs.ErrChannelNotFound
	}
	return errdefs.NewServiceError(http.StatusInternalServerError, errdefs.CodeInternal, err.Error())
}
