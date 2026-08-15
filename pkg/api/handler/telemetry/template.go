// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package telemetry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/tickraft/tickraft/pkg/api"
	"github.com/tickraft/tickraft/pkg/api/httputil"
	"github.com/tickraft/tickraft/pkg/errdefs"
	"github.com/tickraft/tickraft/pkg/telemetry"
)

// maxTemplateNameLength is the maximum allowed length for a template name.
// It matches the varchar(128) constraint on TelemetryTemplate.Name.
const maxTemplateNameLength = 128

// maxTemplateDescriptionLength is the maximum allowed length for a template
// description. It matches the varchar(512) constraint on
// TelemetryTemplate.Description.
const maxTemplateDescriptionLength = 512

// templateRequest is the request body for creating and updating templates.
// Config is accepted as raw JSON so the caller can send an object without
// pre-marshalling; the handler stores it as a string in the model.
type templateRequest struct {
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	Category     string          `json:"category"`
	ExecutorType string          `json:"executor_type"`
	Config       json.RawMessage `json:"config"`
}

// templateResponse is the API response representation of a template. The
// Config field is parsed from the stored JSON string into a generic map so
// the API consumer receives a structured object rather than an opaque
// string.
type templateResponse struct {
	ID           int64          `json:"id"`
	Name         string         `json:"name"`
	Description  string         `json:"description"`
	Category     string         `json:"category"`
	ExecutorType string         `json:"executor_type"`
	Config       map[string]any `json:"config"`
	IsBuiltin    bool           `json:"is_builtin"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

// applyTemplateRequest is the request body for applying a template to
// create a telemetry monitoring point.
type applyTemplateRequest struct {
	// Name overrides the monitoring point name. When empty, defaults to
	// "<template name> (applied)".
	Name string `json:"name"`
	// Overrides merges into the template config to customise the resulting
	// monitoring point. Top-level keys in Overrides replace the
	// corresponding keys in the template config.
	Overrides map[string]any `json:"overrides,omitempty"`
}

// TemplateHandler handles telemetry template API requests. It is injected
// via the WithTemplateHandler RouteOption and registered on the
// /api/v1/telemetry/templates route group. The ApplyTemplate endpoint
// delegates monitoring point creation to the injected Service.
type TemplateHandler struct {
	store *telemetry.TemplateStore
	svc   Service
}

// NewTemplateHandler creates a new TemplateHandler backed by the given
// store. The Service is used by ApplyTemplate to create monitoring points
// and must be non-nil.
func NewTemplateHandler(store *telemetry.TemplateStore, svc Service) *TemplateHandler {
	return &TemplateHandler{store: store, svc: svc}
}

// ListTemplates handles GET /api/v1/telemetry/templates. It accepts an
// optional ?category= query parameter for filtering and returns all
// matching templates (both built-in and custom).
func (h *TemplateHandler) ListTemplates(ctx context.Context, arc *app.RequestContext) {
	category := arc.Query("category")
	templates, err := h.store.List(ctx, category)
	if err != nil {
		api.Fail(arc, err)
		return
	}
	resp := make([]templateResponse, 0, len(templates))
	for i := range templates {
		r, convErr := toTemplateResponse(&templates[i])
		if convErr != nil {
			api.Fail(arc, convErr)
			return
		}
		resp = append(resp, r)
	}
	api.Success(arc, resp)
}

// GetTemplate handles GET /api/v1/telemetry/templates/:id.
func (h *TemplateHandler) GetTemplate(ctx context.Context, arc *app.RequestContext) {
	id, ok := httputil.ParseID(arc)
	if !ok {
		return
	}
	t, err := h.store.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, errdefs.ErrNotFound) {
			api.FailWithCode(arc, http.StatusNotFound, errdefs.CodeNotFound, "template not found")
			return
		}
		api.Fail(arc, err)
		return
	}
	resp, err := toTemplateResponse(t)
	if err != nil {
		api.Fail(arc, err)
		return
	}
	api.Success(arc, resp)
}

// CreateTemplate handles POST /api/v1/telemetry/templates. Built-in
// templates cannot be created through this endpoint; only custom templates
// are created with IsBuiltin=false.
func (h *TemplateHandler) CreateTemplate(ctx context.Context, arc *app.RequestContext) {
	var req templateRequest
	if !api.BindAndValidate(arc, &req) {
		return
	}
	if err := validateTemplateRequest(&req); err != nil {
		api.FailWithCode(arc, http.StatusBadRequest, errdefs.CodeBadRequest, err.Error())
		return
	}

	model := &telemetry.Template{
		Name:         req.Name,
		Description:  req.Description,
		Category:     req.Category,
		ExecutorType: req.ExecutorType,
		Config:       string(req.Config),
		IsBuiltin:    false,
	}
	if err := h.store.Create(ctx, model); err != nil {
		if errors.Is(err, errdefs.ErrConflict) {
			api.FailWithCode(arc, http.StatusConflict, errdefs.CodeConflict, "template name already exists")
			return
		}
		api.Fail(arc, err)
		return
	}
	resp, err := toTemplateResponse(model)
	if err != nil {
		api.Fail(arc, err)
		return
	}
	api.Success(arc, resp)
}

// UpdateTemplate handles PUT /api/v1/telemetry/templates/:id. Built-in
// templates are read-only; attempting to update one returns 403 Forbidden.
func (h *TemplateHandler) UpdateTemplate(ctx context.Context, arc *app.RequestContext) {
	id, ok := httputil.ParseID(arc)
	if !ok {
		return
	}
	var req templateRequest
	if !api.BindAndValidate(arc, &req) {
		return
	}
	existing, err := h.store.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, errdefs.ErrNotFound) {
			api.FailWithCode(arc, http.StatusNotFound, errdefs.CodeNotFound, "template not found")
			return
		}
		api.Fail(arc, err)
		return
	}
	// The read-only guard runs before request validation so a malformed
	// body cannot mask the 403 contract for built-in templates.
	if existing.IsBuiltin {
		api.FailWithCode(arc, http.StatusForbidden, errdefs.CodeForbidden, "built-in templates are read-only")
		return
	}
	if err := validateTemplateRequest(&req); err != nil {
		api.FailWithCode(arc, http.StatusBadRequest, errdefs.CodeBadRequest, err.Error())
		return
	}

	existing.Name = req.Name
	existing.Description = req.Description
	existing.Category = req.Category
	existing.ExecutorType = req.ExecutorType
	existing.Config = string(req.Config)

	if err := h.store.Update(ctx, existing); err != nil {
		if errors.Is(err, errdefs.ErrConflict) {
			api.FailWithCode(arc, http.StatusConflict, errdefs.CodeConflict, "template name already exists")
			return
		}
		if errors.Is(err, errdefs.ErrNotFound) {
			api.FailWithCode(arc, http.StatusNotFound, errdefs.CodeNotFound, "template not found")
			return
		}
		api.Fail(arc, err)
		return
	}
	resp, err := toTemplateResponse(existing)
	if err != nil {
		api.Fail(arc, err)
		return
	}
	api.Success(arc, resp)
}

// DeleteTemplate handles DELETE /api/v1/telemetry/templates/:id. Built-in
// templates cannot be deleted; attempting to delete one returns 403
// Forbidden.
func (h *TemplateHandler) DeleteTemplate(ctx context.Context, arc *app.RequestContext) {
	id, ok := httputil.ParseID(arc)
	if !ok {
		return
	}
	existing, err := h.store.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, errdefs.ErrNotFound) {
			api.FailWithCode(arc, http.StatusNotFound, errdefs.CodeNotFound, "template not found")
			return
		}
		api.Fail(arc, err)
		return
	}
	if existing.IsBuiltin {
		api.FailWithCode(arc, http.StatusForbidden, errdefs.CodeForbidden, "built-in templates cannot be deleted")
		return
	}
	if err := h.store.Delete(ctx, id); err != nil {
		if errors.Is(err, errdefs.ErrNotFound) {
			api.FailWithCode(arc, http.StatusNotFound, errdefs.CodeNotFound, "template not found")
			return
		}
		api.Fail(arc, err)
		return
	}
	api.Success(arc, nil)
}

// ListBuiltinTemplates handles GET /api/v1/telemetry/templates/builtin.
// It returns only the system-seeded built-in templates.
func (h *TemplateHandler) ListBuiltinTemplates(ctx context.Context, arc *app.RequestContext) {
	templates, err := h.store.ListBuiltin(ctx)
	if err != nil {
		api.Fail(arc, err)
		return
	}
	resp := make([]templateResponse, 0, len(templates))
	for i := range templates {
		r, convErr := toTemplateResponse(&templates[i])
		if convErr != nil {
			api.Fail(arc, convErr)
			return
		}
		resp = append(resp, r)
	}
	api.Success(arc, resp)
}

// ApplyTemplate handles POST /api/v1/telemetry/templates/:id/apply. It
// loads the template, merges any overrides from the request body, and
// creates a new telemetry monitoring point via the Service.
func (h *TemplateHandler) ApplyTemplate(ctx context.Context, arc *app.RequestContext) {
	id, ok := httputil.ParseID(arc)
	if !ok {
		return
	}

	t, err := h.store.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, errdefs.ErrNotFound) {
			api.FailWithCode(arc, http.StatusNotFound, errdefs.CodeNotFound, "template not found")
			return
		}
		api.Fail(arc, err)
		return
	}

	var req applyTemplateRequest
	// The body is optional: callers may POST with an empty body to apply the
	// template as-is. Distinguish EOF (empty body, acceptable) from actual JSON
	// parsing errors (which should return 400).
	if err := arc.Bind(&req); err != nil && !errors.Is(err, io.EOF) {
		api.FailWithCode(arc, http.StatusBadRequest, errdefs.CodeBadRequest, "invalid request body")
		return
	}

	// Parse the template config into a map for merging.
	cfg := make(map[string]any)
	if t.Config != "" {
		if err := json.Unmarshal([]byte(t.Config), &cfg); err != nil {
			api.FailWithCode(arc, http.StatusInternalServerError, errdefs.CodeInternal,
				fmt.Sprintf("invalid template config: %v", err))
			return
		}
	}
	// Shallow-merge overrides: top-level keys in Overrides replace the
	// corresponding keys in the template config.
	for k, v := range req.Overrides {
		cfg[k] = v
	}

	// Derive a schedule from the config interval when available.
	schedule := deriveSchedule(cfg)

	name := req.Name
	if name == "" {
		name = t.Name + " (applied)"
	}

	task := &Task{
		Name:        name,
		Description: t.Description,
		AssetType:   t.ExecutorType,
		Schedule:    schedule,
		Enabled:     true,
		Config:      cfg,
	}

	created, err := h.svc.CreateTask(ctx, task)
	if err != nil {
		api.Fail(arc, err)
		return
	}
	api.Success(arc, created)
}

// validateTemplateRequest validates the required fields and length
// constraints of a template create/update request.
func validateTemplateRequest(req *templateRequest) error {
	if req.Name == "" {
		return errors.New("name is required")
	}
	if len(req.Name) > maxTemplateNameLength {
		return fmt.Errorf("name exceeds maximum length of %d characters", maxTemplateNameLength)
	}
	if len(req.Description) > maxTemplateDescriptionLength {
		return fmt.Errorf("description exceeds maximum length of %d characters", maxTemplateDescriptionLength)
	}
	if req.Category == "" {
		return errors.New("category is required")
	}
	if req.ExecutorType == "" {
		return errors.New("executor_type is required")
	}
	if len(req.Config) == 0 {
		return errors.New("config is required")
	}
	// Verify the config is valid JSON.
	var tmp any
	if err := json.Unmarshal(req.Config, &tmp); err != nil {
		return fmt.Errorf("config is not valid JSON: %w", err)
	}
	return nil
}

// toTemplateResponse converts a TelemetryTemplate to a templateResponse,
// parsing the stored JSON config string into a map.
func toTemplateResponse(t *telemetry.Template) (templateResponse, error) {
	var cfg map[string]any
	if t.Config != "" {
		if err := json.Unmarshal([]byte(t.Config), &cfg); err != nil {
			return templateResponse{}, fmt.Errorf("parse template config: %w", err)
		}
	}
	return templateResponse{
		ID:           t.ID,
		Name:         t.Name,
		Description:  t.Description,
		Category:     t.Category,
		ExecutorType: t.ExecutorType,
		Config:       cfg,
		IsBuiltin:    t.IsBuiltin,
		CreatedAt:    t.CreatedAt,
		UpdatedAt:    t.UpdatedAt,
	}, nil
}

// deriveSchedule extracts the "interval" field from the config map and
// formats it as a cron-style interval string (e.g. "60s"). When the
// interval is absent or invalid, it defaults to "60s".
func deriveSchedule(cfg map[string]any) string {
	if v, ok := cfg["interval"]; ok {
		switch n := v.(type) {
		case float64:
			if n > 0 {
				return fmt.Sprintf("%ds", int(n))
			}
		case int:
			if n > 0 {
				return fmt.Sprintf("%ds", n)
			}
		}
	}
	return "60s"
}
