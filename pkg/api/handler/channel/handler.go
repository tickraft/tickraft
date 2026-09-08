// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package channel exposes the notification channel CRUD, test, options,
// and delivery record endpoints of the prism engine.
package channel

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/cloudwego/hertz/pkg/app"

	"github.com/tickraft/tickraft/pkg/api/httputil"
	"github.com/tickraft/tickraft/pkg/errdefs"
	"github.com/tickraft/tickraft/pkg/pagination"
	"github.com/tickraft/tickraft/pkg/prism/channel"
)

// Handler exposes notification channel endpoints. It is injected via the
// WithChannelService RouteOption and registered on the /api/v1/prism/channels
// route group.
type Handler struct {
	svc channel.Service
}

// NewHandler creates a new channel Handler backed by the given service.
func NewHandler(svc channel.Service) *Handler {
	return &Handler{svc: svc}
}

// failChannelError maps a channel service error onto the HTTP response:
// domain validation errors become 400 with the channel business code
// attached, a missing tenant scope becomes 400, a missing channel becomes
// 404, everything else falls through to the generic errdefs mapping.
func failChannelError(arc *app.RequestContext, err error) {
	var ve *channel.ValidationError
	if errors.As(err, &ve) {
		httputil.FailWithCode(arc, http.StatusBadRequest, ve.Code, ve.Msg)
		return
	}
	if errors.Is(err, channel.ErrTenantRequired) {
		httputil.FailWithCode(arc, http.StatusBadRequest, errdefs.CodeBadRequest,
			"tenant context required")
		return
	}
	if errors.Is(err, errdefs.ErrChannelNotFound) {
		httputil.FailWithCode(arc, http.StatusNotFound, channel.CodeNotFound, "channel not found")
		return
	}
	httputil.Fail(arc, err)
}

// ListChannels handles GET /api/v1/prism/channels. It returns every
// channel configuration with sensitive config fields masked.
func (h *Handler) ListChannels(ctx context.Context, arc *app.RequestContext) {
	ctx = httputil.ScopeTenantContext(ctx, arc)
	items, err := h.svc.ListChannels(ctx)
	if err != nil {
		httputil.Fail(arc, err)
		return
	}
	httputil.Success(arc, map[string]any{"items": items})
}

// GetChannel handles GET /api/v1/prism/channels/:id. It returns the
// channel configuration with sensitive config fields masked.
func (h *Handler) GetChannel(ctx context.Context, arc *app.RequestContext) {
	ctx = httputil.ScopeTenantContext(ctx, arc)
	id, ok := httputil.ParseID(arc)
	if !ok {
		return
	}
	ch, err := h.svc.GetChannel(ctx, id)
	if err != nil {
		failChannelError(arc, err)
		return
	}
	httputil.Success(arc, ch)
}

// CreateChannel handles POST /api/v1/prism/channels.
func (h *Handler) CreateChannel(ctx context.Context, arc *app.RequestContext) {
	ctx = httputil.ScopeTenantContext(ctx, arc)
	var req channel.CreateRequest
	if !httputil.BindAndValidate(arc, &req) {
		return
	}
	if len(req.Name) > httputil.MaxNameLength {
		httputil.FailWithCode(arc, http.StatusBadRequest, errdefs.CodeBadRequest,
			"name exceeds maximum length of 255 characters")
		return
	}
	created, err := h.svc.CreateChannel(ctx, &req)
	if err != nil {
		failChannelError(arc, err)
		return
	}
	httputil.Success(arc, created)
}

// UpdateChannel handles PUT /api/v1/prism/channels/:id. Omitted fields
// keep their stored values; sensitive fields submitted empty or still
// masked keep the stored plaintext.
func (h *Handler) UpdateChannel(ctx context.Context, arc *app.RequestContext) {
	ctx = httputil.ScopeTenantContext(ctx, arc)
	id, ok := httputil.ParseID(arc)
	if !ok {
		return
	}
	var req channel.UpdateRequest
	if !httputil.BindAndValidate(arc, &req) {
		return
	}
	if len(req.Name) > httputil.MaxNameLength {
		httputil.FailWithCode(arc, http.StatusBadRequest, errdefs.CodeBadRequest,
			"name exceeds maximum length of 255 characters")
		return
	}
	updated, err := h.svc.UpdateChannel(ctx, id, &req)
	if err != nil {
		failChannelError(arc, err)
		return
	}
	httputil.Success(arc, updated)
}

// DeleteChannel handles DELETE /api/v1/prism/channels/:id.
func (h *Handler) DeleteChannel(ctx context.Context, arc *app.RequestContext) {
	ctx = httputil.ScopeTenantContext(ctx, arc)
	id, ok := httputil.ParseID(arc)
	if !ok {
		return
	}
	if err := h.svc.DeleteChannel(ctx, id); err != nil {
		failChannelError(arc, err)
		return
	}
	httputil.Success(arc, nil)
}

// TestChannel handles POST /api/v1/prism/channels/test. The body either
// references a saved channel by id or supplies an inline type+config
// pair. The response reports whether the synthetic alert was delivered.
func (h *Handler) TestChannel(ctx context.Context, arc *app.RequestContext) {
	ctx = httputil.ScopeTenantContext(ctx, arc)
	var req channel.TestRequest
	if !httputil.BindAndValidate(arc, &req) {
		return
	}
	if err := h.svc.TestChannel(ctx, &req); err != nil {
		var ve *channel.ValidationError
		if errors.As(err, &ve) {
			httputil.FailWithCode(arc, http.StatusBadRequest, ve.Code, ve.Msg)
			return
		}
		if errors.Is(err, errdefs.ErrChannelNotFound) {
			httputil.FailWithCode(arc, http.StatusNotFound, channel.CodeNotFound, "channel not found")
			return
		}
		httputil.FailWithCode(arc, http.StatusInternalServerError, channel.CodeTestFailed,
			"test notification failed")
		return
	}
	httputil.Success(arc, map[string]string{"status": "success"})
}

// ListChannelOptions handles GET /api/v1/prism/channels/options. It
// returns the compact id/name/type/enabled projection of every channel,
// used by the deliveries page filter dropdown.
func (h *Handler) ListChannelOptions(ctx context.Context, arc *app.RequestContext) {
	ctx = httputil.ScopeTenantContext(ctx, arc)
	options, err := h.svc.ListChannelOptions(ctx)
	if err != nil {
		httputil.Fail(arc, err)
		return
	}
	httputil.Success(arc, map[string]any{"items": options})
}

// ListAllDeliveries handles GET /api/v1/prism/channels/deliveries. It
// returns delivery records across all channels with optional channel,
// status, alert-title, and time-range filters.
func (h *Handler) ListAllDeliveries(ctx context.Context, arc *app.RequestContext) {
	ctx = httputil.ScopeTenantContext(ctx, arc)
	req, ok := httputil.ParsePageRequest(arc)
	if !ok {
		return
	}
	params, failMsg := parseDeliveryParams(arc)
	if failMsg != "" {
		httputil.FailWithCode(arc, http.StatusBadRequest, errdefs.CodeBadRequest, failMsg)
		return
	}
	h.fetchDeliveryPage(ctx, arc, params, req)
}

// ListChannelDeliveries handles GET /api/v1/prism/channels/:id/deliveries.
// It returns the delivery records of the given channel with optional
// status and time-range filters.
func (h *Handler) ListChannelDeliveries(ctx context.Context, arc *app.RequestContext) {
	ctx = httputil.ScopeTenantContext(ctx, arc)
	id, ok := httputil.ParseID(arc)
	if !ok {
		return
	}
	// Resolve the channel so an unknown id fails with 404; delivery
	// records are filtered by the configuration row ID carried on each
	// record by the tracking decorator.
	if _, err := h.svc.GetChannel(ctx, id); err != nil {
		failChannelError(arc, err)
		return
	}

	req, ok := httputil.ParsePageRequest(arc)
	if !ok {
		return
	}
	params, failMsg := parseDeliveryParams(arc)
	if failMsg != "" {
		httputil.FailWithCode(arc, http.StatusBadRequest, errdefs.CodeBadRequest, failMsg)
		return
	}
	params.ChannelID = id

	h.fetchDeliveryPage(ctx, arc, params, req)
}

// RetryDelivery handles POST /api/v1/prism/channels/deliveries/:id/retry.
// It replays a failed delivery: the persisted alert event is re-sent
// synchronously through the channel's current configuration and the
// outcome is appended to the record's attempt history. The response
// carries the updated record; its status reflects the retry outcome, so a
// 200 with status "failed" means the retry ran but the channel rejected
// the message again.
func (h *Handler) RetryDelivery(ctx context.Context, arc *app.RequestContext) {
	ctx = httputil.ScopeTenantContext(ctx, arc)
	id, ok := httputil.ParseID(arc)
	if !ok {
		return
	}
	rec, err := h.svc.RetryDelivery(ctx, id)
	if err != nil {
		failChannelError(arc, err)
		return
	}
	httputil.Success(arc, rec)
}

// fetchDeliveryPage runs the shared pagination flow of the two delivery
// list endpoints — keyset mode when the cursor query parameter is present
// (avoiding the O(N) cost of OFFSET on deep pages), offset mode otherwise
// — and writes the response.
func (h *Handler) fetchDeliveryPage(
	ctx context.Context,
	arc *app.RequestContext,
	params channel.DeliveryListParams,
	req pagination.PageRequest,
) {
	params.Page, params.Size = req.Page, req.Size
	if req.IsKeyset() {
		params.Cursor = req.Cursor
		result, err := h.svc.ListDeliveriesKeyset(ctx, params)
		if err != nil {
			httputil.Fail(arc, err)
			return
		}
		httputil.SuccessPageCursor(arc, result.Items, result.Total, result.NextCursor, params.Size)
		return
	}

	records, total, err := h.svc.ListDeliveries(ctx, params)
	if err != nil {
		httputil.Fail(arc, err)
		return
	}
	httputil.SuccessPage(arc, records, total, params.Page, params.Size)
}

// parseDeliveryParams extracts the channel, alert-title, status, and
// time-range query parameters for the delivery list endpoints.
// Pagination (page/size/cursor) is parsed separately via
// httputil.ParsePageRequest. It returns a non-empty failMsg when a query
// parameter is invalid; the caller should respond with a 400 using it.
func parseDeliveryParams(arc *app.RequestContext) (params channel.DeliveryListParams, failMsg string) {
	if s := arc.Query("channel_id"); s != "" {
		v, err := strconv.ParseInt(s, 10, 64)
		if err != nil || v <= 0 {
			return params, "invalid channel_id"
		}
		params.ChannelID = v
	}

	if s := arc.Query("alert_title"); s != "" {
		params.AlertTitle = s
	}

	if status := arc.Query("status"); status != "" {
		params.Status = status
	}

	if t, err := parseTimeQuery(arc, "start_time"); err != nil {
		return params, "invalid start_time, expected RFC3339 format"
	} else if t != nil {
		params.StartTime = t
	}

	if t, err := parseTimeQuery(arc, "end_time"); err != nil {
		return params, "invalid end_time, expected RFC3339 format"
	} else if t != nil {
		params.EndTime = t
	}

	return params, ""
}

// parseTimeQuery parses the RFC3339 value of a time query parameter. It
// returns nil when the parameter is absent.
func parseTimeQuery(arc *app.RequestContext, name string) (*time.Time, error) {
	s := arc.Query(name)
	if s == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil, err
	}
	return &t, nil
}
