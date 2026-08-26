// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package task exposes the scheduled-task and execution HTTP endpoints.
// Request bodies bind directly onto the shared pkg/task model; the Service
// interface defined here is what x deployments implement to reuse the routes.
package task

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/hertz/pkg/app"

	"github.com/tickraft/tickraft/pkg/api"
	"github.com/tickraft/tickraft/pkg/api/httputil"
	"github.com/tickraft/tickraft/pkg/errdefs"
	"github.com/tickraft/tickraft/pkg/task"
	taskservice "github.com/tickraft/tickraft/pkg/task/service"
)

// maxStatsDays bounds the days parameter of GET /tasks/stats; larger
// windows would scan unbounded history for a chart series.
const maxStatsDays = 90

// Handler exposes task and execution CRUD endpoints.
// It is injected via the WithTaskService RouteOption and registered on
// the /api/v1/tasks route group.
type Handler struct {
	svc taskservice.Service
}

// NewHandler creates a new task Handler backed by the given service.
func NewHandler(svc taskservice.Service) *Handler {
	return &Handler{svc: svc}
}

// copyTaskRequest is the optional request body for the copy endpoint.
type copyTaskRequest struct {
	Name string `json:"name"`
}

// ListTasks handles GET /api/v1/tasks.
func (h *Handler) ListTasks(ctx context.Context, arc *app.RequestContext) {
	page, size, ok := httputil.ParsePaging(arc)
	if !ok {
		return
	}
	filter := taskservice.Filter{
		Group: arc.Query("group"),
	}
	if tagsParam := arc.Query("tags"); tagsParam != "" {
		for tag := range strings.SplitSeq(tagsParam, ",") {
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
	tsk, err := h.svc.GetTask(ctx, id)
	if err != nil {
		api.Fail(arc, err)
		return
	}
	api.Success(arc, tsk)
}

// CreateTask handles POST /api/v1/tasks.
func (h *Handler) CreateTask(ctx context.Context, arc *app.RequestContext) {
	var req task.Task
	if !api.BindAndValidate(arc, &req) {
		return
	}
	if req.Name == "" {
		api.FailWithCode(arc, http.StatusBadRequest, errdefs.CodeBadRequest, "name is required")
		return
	}
	if len(req.Name) > httputil.MaxNameLength {
		api.FailWithCode(arc, http.StatusBadRequest, errdefs.CodeBadRequest,
			"name exceeds maximum length of 255 characters")
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
	var req task.Task
	if !api.BindAndValidate(arc, &req) {
		return
	}
	if len(req.Name) > httputil.MaxNameLength {
		api.FailWithCode(arc, http.StatusBadRequest, errdefs.CodeBadRequest,
			"name exceeds maximum length of 255 characters")
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

// GetExecutionStats handles GET /api/v1/tasks/stats. Supported query
// parameters: from/to (RFC3339, default last 24h), the optional task_id
// filter that scopes the aggregation to a single task's executions, and
// the optional days filter (1-90) that switches the range to the last N
// UTC days and adds a zero-filled per-day series.
func (h *Handler) GetExecutionStats(ctx context.Context, arc *app.RequestContext) {
	to := time.Now()
	from := to.Add(-24 * time.Hour)
	if v := arc.Query("from"); v != "" {
		if parsed, err := time.Parse(time.RFC3339, v); err == nil {
			from = parsed
		} else {
			api.FailWithCode(arc, http.StatusBadRequest, errdefs.CodeBadRequest,
				"invalid 'from' timestamp, expected RFC3339 format")
			return
		}
	}
	if v := arc.Query("to"); v != "" {
		if parsed, err := time.Parse(time.RFC3339, v); err == nil {
			to = parsed
		} else {
			api.FailWithCode(arc, http.StatusBadRequest, errdefs.CodeBadRequest,
				"invalid 'to' timestamp, expected RFC3339 format")
			return
		}
	}
	var taskID int64
	if v := arc.Query("task_id"); v != "" {
		parsed, err := strconv.ParseInt(v, 10, 64)
		if err != nil || parsed <= 0 {
			api.FailWithCode(arc, http.StatusBadRequest, errdefs.CodeBadRequest,
				"invalid 'task_id', expected a positive integer")
			return
		}
		taskID = parsed
	}
	days := 0
	if v := arc.Query("days"); v != "" {
		parsed, err := strconv.Atoi(v)
		if err != nil || parsed < 1 || parsed > maxStatsDays {
			api.FailWithCode(arc, http.StatusBadRequest, errdefs.CodeBadRequest,
				fmt.Sprintf("invalid 'days', expected an integer between 1 and %d", maxStatsDays))
			return
		}
		days = parsed
	}
	stats, err := h.svc.GetExecutionStats(ctx, from, to, taskID, days)
	if err != nil {
		api.Fail(arc, err)
		return
	}
	api.Success(arc, stats)
}

// ListExecutions handles GET /api/v1/tasks/:id/executions. Supported query
// parameters: page, size, status (success/failed/timeout/running/unknown),
// executor_type and task_name (substring match). A task id of 0
// lists executions across all tasks.
func (h *Handler) ListExecutions(ctx context.Context, arc *app.RequestContext) {
	taskID, ok := httputil.ParseID(arc)
	if !ok {
		return
	}
	page, size, ok := httputil.ParsePaging(arc)
	if !ok {
		return
	}
	filter := taskservice.ExecutionFilter{
		Status:       arc.Query("status"),
		ExecutorType: arc.Query("executor_type"),
		TaskName:     arc.Query("task_name"),
		TriggerType:  arc.Query("trigger_type"),
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
