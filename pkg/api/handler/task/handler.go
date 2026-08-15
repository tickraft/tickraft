// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package task

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/hertz/pkg/app"

	"github.com/tickraft/tickraft/pkg/api"
	"github.com/tickraft/tickraft/pkg/api/httputil"
	"github.com/tickraft/tickraft/pkg/errdefs"
)

// Handler exposes task and execution CRUD endpoints.
// It is injected via the WithTaskService RouteOption and registered on
// the /api/v1/tasks route group.
type Handler struct {
	svc Service
}

// NewHandler creates a new task Handler backed by the given service.
func NewHandler(svc Service) *Handler {
	return &Handler{svc: svc}
}

// copyTaskRequest is the optional request body for the copy endpoint.
type copyTaskRequest struct {
	Name string `json:"name"`
}

// ListTasks handles GET /api/v1/tasks.
func (h *Handler) ListTasks(ctx context.Context, arc *app.RequestContext) {
	page, size := httputil.ParsePaging(arc)
	filter := Filter{
		Group: arc.Query("group"),
	}
	if tagsParam := arc.Query("tags"); tagsParam != "" {
		for _, tag := range strings.Split(tagsParam, ",") {
			tag = strings.TrimSpace(tag)
			if tag != "" {
				filter.Tags = append(filter.Tags, tag)
			}
		}
	}
	items, total, err := h.svc.ListTasks(ctx, page, size, filter)
	if err != nil {
		api.Fail(arc, err)
		return
	}
	api.SuccessPage(arc, items, total, page, size)
}

// GetTask handles GET /api/v1/tasks/:id.
func (h *Handler) GetTask(ctx context.Context, arc *app.RequestContext) {
	id, ok := httputil.ParseID(arc)
	if !ok {
		return
	}
	task, err := h.svc.GetTask(ctx, id)
	if err != nil {
		api.Fail(arc, err)
		return
	}
	api.Success(arc, task)
}

// CreateTask handles POST /api/v1/tasks.
func (h *Handler) CreateTask(ctx context.Context, arc *app.RequestContext) {
	var req Task
	if !api.BindAndValidate(arc, &req) {
		return
	}
	if req.Name == "" {
		api.FailWithCode(arc, http.StatusBadRequest, errdefs.CodeBadRequest, "name is required")
		return
	}
	if len(req.Name) > httputil.MaxNameLength {
		api.FailWithCode(arc, http.StatusBadRequest, errdefs.CodeBadRequest, "name exceeds maximum length of 255 characters")
		return
	}
	created, err := h.svc.CreateTask(ctx, &req)
	if err != nil {
		api.Fail(arc, err)
		return
	}
	api.Success(arc, created)
}

// UpdateTask handles PUT /api/v1/tasks/:id.
func (h *Handler) UpdateTask(ctx context.Context, arc *app.RequestContext) {
	id, ok := httputil.ParseID(arc)
	if !ok {
		return
	}
	var req Task
	if !api.BindAndValidate(arc, &req) {
		return
	}
	if len(req.Name) > httputil.MaxNameLength {
		api.FailWithCode(arc, http.StatusBadRequest, errdefs.CodeBadRequest, "name exceeds maximum length of 255 characters")
		return
	}
	req.ID = id
	updated, err := h.svc.UpdateTask(ctx, id, &req)
	if err != nil {
		api.Fail(arc, err)
		return
	}
	api.Success(arc, updated)
}

// DeleteTask handles DELETE /api/v1/tasks/:id.
func (h *Handler) DeleteTask(ctx context.Context, arc *app.RequestContext) {
	id, ok := httputil.ParseID(arc)
	if !ok {
		return
	}
	if err := h.svc.DeleteTask(ctx, id); err != nil {
		api.Fail(arc, err)
		return
	}
	api.Success(arc, nil)
}

// TriggerTask handles POST /api/v1/tasks/:id/trigger.
func (h *Handler) TriggerTask(ctx context.Context, arc *app.RequestContext) {
	id, ok := httputil.ParseID(arc)
	if !ok {
		return
	}
	if err := h.svc.TriggerTask(ctx, id); err != nil {
		api.Fail(arc, err)
		return
	}
	api.Success(arc, nil)
}

// PauseTask handles POST /api/v1/tasks/:id/pause.
func (h *Handler) PauseTask(ctx context.Context, arc *app.RequestContext) {
	id, ok := httputil.ParseID(arc)
	if !ok {
		return
	}
	if err := h.svc.PauseTask(ctx, id); err != nil {
		api.Fail(arc, err)
		return
	}
	api.Success(arc, nil)
}

// ResumeTask handles POST /api/v1/tasks/:id/resume.
func (h *Handler) ResumeTask(ctx context.Context, arc *app.RequestContext) {
	id, ok := httputil.ParseID(arc)
	if !ok {
		return
	}
	if err := h.svc.ResumeTask(ctx, id); err != nil {
		api.Fail(arc, err)
		return
	}
	api.Success(arc, nil)
}

// CopyTask handles POST /api/v1/tasks/:id/copy.
func (h *Handler) CopyTask(ctx context.Context, arc *app.RequestContext) {
	id, ok := httputil.ParseID(arc)
	if !ok {
		return
	}
	var req copyTaskRequest
	if err := arc.Bind(&req); err != nil && !errors.Is(err, io.EOF) {
		api.FailWithCode(arc, http.StatusBadRequest, errdefs.CodeBadRequest, "invalid request body")
		return
	}
	copied, err := h.svc.CopyTask(ctx, id, req.Name)
	if err != nil {
		api.Fail(arc, err)
		return
	}
	api.Success(arc, copied)
}

// GetExecutionStats handles GET /api/v1/tasks/stats.
func (h *Handler) GetExecutionStats(ctx context.Context, arc *app.RequestContext) {
	to := time.Now()
	from := to.Add(-24 * time.Hour)
	if v := arc.Query("from"); v != "" {
		if parsed, err := time.Parse(time.RFC3339, v); err == nil {
			from = parsed
		} else {
			api.FailWithCode(arc, http.StatusBadRequest, errdefs.CodeBadRequest, "invalid 'from' timestamp, expected RFC3339 format")
			return
		}
	}
	if v := arc.Query("to"); v != "" {
		if parsed, err := time.Parse(time.RFC3339, v); err == nil {
			to = parsed
		} else {
			api.FailWithCode(arc, http.StatusBadRequest, errdefs.CodeBadRequest, "invalid 'to' timestamp, expected RFC3339 format")
			return
		}
	}
	stats, err := h.svc.GetExecutionStats(ctx, from, to)
	if err != nil {
		api.Fail(arc, err)
		return
	}
	api.Success(arc, stats)
}

// ListExecutions handles GET /api/v1/tasks/:id/executions. Supported query
// parameters: page, page_size, status (pending/running/success/failed),
// executor (executor type) and task_name (substring match). A task id of 0
// lists executions across all tasks.
func (h *Handler) ListExecutions(ctx context.Context, arc *app.RequestContext) {
	taskID, ok := httputil.ParseID(arc)
	if !ok {
		return
	}
	page, size := httputil.ParsePaging(arc)
	filter := ExecutionFilter{
		Status:       storedStatus(arc.Query("status")),
		ExecutorType: arc.Query("executor"),
		TaskName:     arc.Query("task_name"),
	}
	items, total, err := h.svc.ListExecutions(ctx, taskID, page, size, filter)
	if err != nil {
		api.Fail(arc, err)
		return
	}
	api.SuccessPage(arc, items, total, page, size)
}

// GetExecution handles GET /api/v1/tasks/:id/executions/:execId.
func (h *Handler) GetExecution(ctx context.Context, arc *app.RequestContext) {
	taskID, ok := httputil.ParseID(arc)
	if !ok {
		return
	}
	execIDStr := arc.Param("execId")
	execID, err := strconv.ParseInt(execIDStr, 10, 64)
	if err != nil {
		api.FailWithCode(arc, http.StatusBadRequest, errdefs.CodeBadRequest, "invalid execution id parameter")
		return
	}
	execution, err := h.svc.GetExecution(ctx, taskID, execID)
	if err != nil {
		api.Fail(arc, err)
		return
	}
	api.Success(arc, execution)
}

// storedStatus maps an execution lifecycle status from the API contract
// (pending/running/success/failed) to the persisted status value. Unknown
// values pass through so callers filtering by raw stored values keep working.
func storedStatus(lifecycle string) string {
	switch lifecycle {
	case "success":
		return "normal"
	case "failed":
		return "abnormal"
	case "running":
		return "triggered"
	default:
		return lifecycle
	}
}
