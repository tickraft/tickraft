// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package asset

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/api"
	"github.com/tickraft/tickraft/pkg/api/httputil"
	"github.com/tickraft/tickraft/pkg/asset"
	"github.com/tickraft/tickraft/pkg/errdefs"
	"github.com/tickraft/tickraft/pkg/quota"
	"github.com/tickraft/tickraft/pkg/types"
)

// maxDeviceQuota is retained as a fallback for logging only. The actual
// enforcement uses quota.Ceiling(quota.TypeDevice) which supports dynamic
// provider-based overrides (e.g. subscription plans).
const maxDeviceQuota = 20

// Handler exposes asset CRUD endpoints for the telemetry module.
// It is injected via the WithAssetHandler RouteOption and registered on the
// /api/v1/assets route group.
//
// All mutating operations emit structured audit logs (operation, outcome,
// resource id, asset key/type) so the asset lifecycle is traceable end-to-end
// for operational forensics and compliance review.
type Handler struct {
	store  asset.Store
	logger *zap.Logger
}

// NewHandler creates a new Handler backed by the given store.
// A nil logger falls back to a no-op logger so the handler is safe to use in
// tests without explicit logging configuration; production wiring passes the
// runtime zap logger so audit events flow through the same pipeline as the
// rest of the application.
func NewHandler(store asset.Store, logger *zap.Logger) *Handler {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Handler{store: store, logger: logger}
}

// CreateAsset handles POST /api/v1/assets.
func (h *Handler) CreateAsset(ctx context.Context, arc *app.RequestContext) {
	var a asset.Asset
	if !api.BindAndValidate(arc, &a) {
		return
	}
	if a.Name == "" {
		api.FailWithCode(arc, http.StatusBadRequest, errdefs.CodeBadRequest, "name is required")
		return
	}
	if a.AssetKey == "" {
		api.FailWithCode(arc, http.StatusBadRequest, errdefs.CodeBadRequest, "asset_key is required")
		return
	}
	if a.AssetType == "" {
		api.FailWithCode(arc, http.StatusBadRequest, errdefs.CodeBadRequest, "asset_type is required")
		return
	}
	if a.Status == "" {
		a.Status = types.AssetStatusUnknown
	}

	// Enforce device and host quotas before persisting. The check
	// counts existing assets of the same type and rejects with 409
	// Conflict when the ceiling (from quota.Ceiling, which delegates to
	// the active Provider) is reached. A ceiling of 0 means the resource
	// type is not allowed in the current plan.
	//
	// The runtime is single-tenant: the tenant ID passed to CountByType
	// is fixed to 0.
	var ceiling int
	switch a.AssetType {
	case types.AssetTypeDevice:
		ceiling = quota.Ceiling(quota.TypeDevice)
	case types.AssetTypeHost:
		ceiling = quota.Ceiling(quota.TypeHost)
	}
	if ceiling > 0 || a.AssetType == types.AssetTypeHost {
		const tenantID = 0
		count, err := h.store.CountByType(ctx, tenantID, a.AssetType)
		if err != nil {
			h.logger.Error("asset create quota check failed",
				zap.String("operation", "asset.create"),
				zap.String("asset_key", a.AssetKey),
				zap.String("asset_type", string(a.AssetType)),
				zap.Error(err),
			)
			api.Fail(arc, err)
			return
		}
		if count >= int64(ceiling) {
			h.logger.Warn("asset create rejected: quota exceeded",
				zap.String("operation", "asset.create"),
				zap.String("outcome", "quota_exceeded"),
				zap.String("asset_key", a.AssetKey),
				zap.String("asset_type", string(a.AssetType)),
				zap.Int64("current_count", count),
				zap.Int("quota", ceiling),
			)
			api.FailWithCode(arc, http.StatusConflict, errdefs.CodeConflict, "quota_exceeded")
			return
		}
	}

	if err := h.store.Create(ctx, &a); err != nil {
		if errors.Is(err, errdefs.ErrConflict) {
			h.logger.Warn("asset create rejected: duplicate asset key",
				zap.String("operation", "asset.create"),
				zap.String("outcome", "conflict"),
				zap.String("asset_key", a.AssetKey),
				zap.String("asset_type", string(a.AssetType)),
			)
			api.FailWithCode(arc, http.StatusConflict, errdefs.CodeConflict, "asset key already exists")
			return
		}
		h.logger.Error("asset create failed",
			zap.String("operation", "asset.create"),
			zap.String("outcome", "error"),
			zap.String("asset_key", a.AssetKey),
			zap.String("asset_type", string(a.AssetType)),
			zap.Error(err),
		)
		api.Fail(arc, err)
		return
	}
	h.logger.Info("asset created",
		zap.String("operation", "asset.create"),
		zap.String("outcome", "success"),
		zap.Int64("id", a.ID),
		zap.String("asset_key", a.AssetKey),
		zap.String("asset_type", string(a.AssetType)),
		zap.String("name", a.Name),
	)
	api.Success(arc, a)
}

// ListAssets handles GET /api/v1/assets. Supported query parameters: page,
// page_size, keyword (substring match on name/asset_key), asset_type and
// status (exact match).
func (h *Handler) ListAssets(ctx context.Context, arc *app.RequestContext) {
	page, size := httputil.ParsePaging(arc)
	filter := asset.ListFilter{
		Keyword:   arc.Query("keyword"),
		AssetType: arc.Query("asset_type"),
		Status:    arc.Query("status"),
	}
	items, total, err := h.store.List(ctx, page, size, filter)
	if err != nil {
		api.Fail(arc, err)
		return
	}
	api.SuccessPage(arc, items, total, page, size)
}

// GetAsset handles GET /api/v1/assets/:id.
func (h *Handler) GetAsset(ctx context.Context, arc *app.RequestContext) {
	id, ok := httputil.ParseID(arc)
	if !ok {
		return
	}
	a, err := h.store.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, errdefs.ErrNotFound) {
			h.logger.Info("asset get: not found",
				zap.String("operation", "asset.get"),
				zap.String("outcome", "not_found"),
				zap.Int64("id", id),
			)
			api.FailWithCode(arc, http.StatusNotFound, errdefs.CodeNotFound, "asset not found")
			return
		}
		h.logger.Error("asset get failed",
			zap.String("operation", "asset.get"),
			zap.String("outcome", "error"),
			zap.Int64("id", id),
			zap.Error(err),
		)
		api.Fail(arc, err)
		return
	}
	h.logger.Debug("asset retrieved",
		zap.String("operation", "asset.get"),
		zap.String("outcome", "success"),
		zap.Int64("id", id),
		zap.String("asset_key", a.AssetKey),
	)
	api.Success(arc, a)
}

// UpdateAsset handles PUT /api/v1/assets/:id. It loads the
// existing asset, applies the fields from the request body, and saves.
func (h *Handler) UpdateAsset(ctx context.Context, arc *app.RequestContext) {
	id, ok := httputil.ParseID(arc)
	if !ok {
		return
	}

	// Verify the asset exists before updating.
	existing, err := h.store.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, errdefs.ErrNotFound) {
			h.logger.Info("asset update: not found",
				zap.String("operation", "asset.update"),
				zap.String("outcome", "not_found"),
				zap.Int64("id", id),
			)
			api.FailWithCode(arc, http.StatusNotFound, errdefs.CodeNotFound, "asset not found")
			return
		}
		h.logger.Error("asset update: load existing failed",
			zap.String("operation", "asset.update"),
			zap.String("outcome", "error"),
			zap.Int64("id", id),
			zap.Error(err),
		)
		api.Fail(arc, err)
		return
	}

	// Capture the previous state for audit diffing.
	prevName := existing.Name
	prevStatus := existing.Status

	// Bind the request body onto the existing asset so that unspecified
	// fields retain their current values.
	if !api.BindAndValidate(arc, existing) {
		return
	}
	existing.ID = id

	if err := h.store.Update(ctx, existing); err != nil {
		h.logger.Error("asset update failed",
			zap.String("operation", "asset.update"),
			zap.String("outcome", "error"),
			zap.Int64("id", id),
			zap.Error(err),
		)
		api.Fail(arc, err)
		return
	}
	h.logger.Info("asset updated",
		zap.String("operation", "asset.update"),
		zap.String("outcome", "success"),
		zap.Int64("id", id),
		zap.String("asset_key", existing.AssetKey),
		zap.String("name", existing.Name),
		zap.String("prev_name", prevName),
		zap.String("status", string(existing.Status)),
		zap.String("prev_status", string(prevStatus)),
	)
	api.Success(arc, existing)
}

// DeleteAsset handles DELETE /api/v1/assets/:id.
func (h *Handler) DeleteAsset(ctx context.Context, arc *app.RequestContext) {
	id, ok := httputil.ParseID(arc)
	if !ok {
		return
	}
	if err := h.store.Delete(ctx, id); err != nil {
		if errors.Is(err, errdefs.ErrNotFound) {
			h.logger.Info("asset delete: not found",
				zap.String("operation", "asset.delete"),
				zap.String("outcome", "not_found"),
				zap.Int64("id", id),
			)
			api.FailWithCode(arc, http.StatusNotFound, errdefs.CodeNotFound, "asset not found")
			return
		}
		h.logger.Error("asset delete failed",
			zap.String("operation", "asset.delete"),
			zap.String("outcome", "error"),
			zap.Int64("id", id),
			zap.Error(err),
		)
		api.Fail(arc, err)
		return
	}
	h.logger.Info("asset deleted",
		zap.String("operation", "asset.delete"),
		zap.String("outcome", "success"),
		zap.Int64("id", id),
	)
	api.Success(arc, nil)
}

// assetStatusRequest is the request body for updating an asset's status.
type assetStatusRequest struct {
	Status types.AssetStatus `json:"status"`
}

// validAssetStatuses is the set of accepted asset status values. It mirrors
// the constants defined in pkg/types/asset.go.
var validAssetStatuses = map[types.AssetStatus]bool{
	types.AssetStatusNormal:   true,
	types.AssetStatusAbnormal: true,
	types.AssetStatusOffline:  true,
	types.AssetStatusUnknown:  true,
}

// UpdateAssetStatus handles PUT /api/v1/assets/:id/status. It validates the
// requested status, verifies the asset exists, and delegates to
// store.UpdateStatus with the current timestamp as the last-active time.
func (h *Handler) UpdateAssetStatus(ctx context.Context, arc *app.RequestContext) {
	id, ok := httputil.ParseID(arc)
	if !ok {
		return
	}
	var req assetStatusRequest
	if !api.BindAndValidate(arc, &req) {
		return
	}
	if !validAssetStatuses[req.Status] {
		h.logger.Warn("asset status update rejected: invalid status",
			zap.String("operation", "asset.status_update"),
			zap.String("outcome", "invalid_status"),
			zap.Int64("id", id),
			zap.String("requested_status", string(req.Status)),
		)
		api.FailWithCode(arc, http.StatusBadRequest, errdefs.CodeBadRequest,
			"invalid status: must be one of normal, abnormal, offline, unknown")
		return
	}

	// Verify the asset exists before updating the status.
	existing, err := h.store.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, errdefs.ErrNotFound) {
			h.logger.Info("asset status update: not found",
				zap.String("operation", "asset.status_update"),
				zap.String("outcome", "not_found"),
				zap.Int64("id", id),
			)
			api.FailWithCode(arc, http.StatusNotFound, errdefs.CodeNotFound, "asset not found")
			return
		}
		h.logger.Error("asset status update: load failed",
			zap.String("operation", "asset.status_update"),
			zap.String("outcome", "error"),
			zap.Int64("id", id),
			zap.Error(err),
		)
		api.Fail(arc, err)
		return
	}

	prevStatus := existing.Status

	if err := h.store.UpdateStatus(ctx, id, req.Status, time.Now()); err != nil {
		if errors.Is(err, errdefs.ErrNotFound) {
			h.logger.Info("asset status update: not found",
				zap.String("operation", "asset.status_update"),
				zap.String("outcome", "not_found"),
				zap.Int64("id", id),
			)
			api.FailWithCode(arc, http.StatusNotFound, errdefs.CodeNotFound, "asset not found")
			return
		}
		h.logger.Error("asset status update failed",
			zap.String("operation", "asset.status_update"),
			zap.String("outcome", "error"),
			zap.Int64("id", id),
			zap.Error(err),
		)
		api.Fail(arc, err)
		return
	}
	h.logger.Info("asset status updated",
		zap.String("operation", "asset.status_update"),
		zap.String("outcome", "success"),
		zap.Int64("id", id),
		zap.String("asset_key", existing.AssetKey),
		zap.String("status", string(req.Status)),
		zap.String("prev_status", string(prevStatus)),
	)
	api.Success(arc, nil)
}

// probeResult is the response returned by ProbeAsset. It echoes the asset ID
// and the status determined by the probe.
type probeResult struct {
	AssetID int64             `json:"asset_id"`
	Status  types.AssetStatus `json:"status"`
}

// ProbeAsset handles POST /api/v1/assets/:id/probe. It loads the asset and
// returns its current status as a probe result. The default
// implementation performs a lightweight status read; the callers
// overrides this to dispatch a real probe through the executor/telemetry
// pipeline.
func (h *Handler) ProbeAsset(ctx context.Context, arc *app.RequestContext) {
	id, ok := httputil.ParseID(arc)
	if !ok {
		return
	}
	a, err := h.store.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, errdefs.ErrNotFound) {
			h.logger.Info("asset probe: not found",
				zap.String("operation", "asset.probe"),
				zap.String("outcome", "not_found"),
				zap.Int64("id", id),
			)
			api.FailWithCode(arc, http.StatusNotFound, errdefs.CodeNotFound, "asset not found")
			return
		}
		h.logger.Error("asset probe failed",
			zap.String("operation", "asset.probe"),
			zap.String("outcome", "error"),
			zap.Int64("id", id),
			zap.Error(err),
		)
		api.Fail(arc, err)
		return
	}
	h.logger.Info("asset probed",
		zap.String("operation", "asset.probe"),
		zap.String("outcome", "success"),
		zap.Int64("id", id),
		zap.String("asset_key", a.AssetKey),
		zap.String("status", string(a.Status)),
	)
	api.Success(arc, probeResult{
		AssetID: a.ID,
		Status:  a.Status,
	})
}
