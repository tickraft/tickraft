// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package alert exposes the alert rule and record CRUD endpoints of the
// prism alert engine.
package alert

import (
	"context"
	"encoding/csv"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/cloudwego/hertz/pkg/app"

	"github.com/tickraft/tickraft/pkg/api/httputil"
	"github.com/tickraft/tickraft/pkg/errdefs"
	"github.com/tickraft/tickraft/pkg/prism/alert"
)

// Handler exposes alert rule and record CRUD endpoints.
// It is injected via the WithAlertService RouteOption and registered on
// the /api/v1/prism/alert route group.
type Handler struct {
	svc alert.Service
}

// NewHandler creates a new alert Handler backed by the given service.
func NewHandler(svc alert.Service) *Handler {
	return &Handler{svc: svc}
}

// ListAlertRules handles GET /api/v1/prism/alert/rules.
func (h *Handler) ListAlertRules(ctx context.Context, arc *app.RequestContext) {
	page, size, ok := httputil.ParsePaging(arc)
	if !ok {
		return
	}
	items, total, err := h.svc.ListRules(ctx, page, size)
	if err != nil {
		httputil.Fail(arc, err)
		return
	}
	httputil.SuccessPage(arc, items, total, page, size)
}

// GetAlertRule handles GET /api/v1/prism/alert/rules/:id.
func (h *Handler) GetAlertRule(ctx context.Context, arc *app.RequestContext) {
	id, ok := httputil.ParseID(arc)
	if !ok {
		return
	}
	rule, err := h.svc.GetRule(ctx, id)
	if err != nil {
		httputil.Fail(arc, err)
		return
	}
	httputil.Success(arc, rule)
}

// CreateAlertRule handles POST /api/v1/prism/alert/rules.
func (h *Handler) CreateAlertRule(ctx context.Context, arc *app.RequestContext) {
	var req alert.Rule
	if !httputil.BindAndValidate(arc, &req) {
		return
	}
	if req.Name == "" {
		httputil.FailWithCode(arc, http.StatusBadRequest, errdefs.CodeBadRequest, "name is required")
		return
	}
	if len(req.Name) > httputil.MaxNameLength {
		httputil.FailWithCode(arc, http.StatusBadRequest, errdefs.CodeBadRequest,
			"name exceeds maximum length of 255 characters")
		return
	}
	if len(req.Description) > httputil.MaxDescriptionLength {
		httputil.FailWithCode(arc, http.StatusBadRequest, errdefs.CodeBadRequest,
			"description exceeds maximum length of 1024 characters")
		return
	}
	created, err := h.svc.CreateRule(ctx, &req)
	if err != nil {
		httputil.Fail(arc, err)
		return
	}
	httputil.Success(arc, created)
}

// UpdateAlertRule handles PUT /api/v1/prism/alert/rules/:id.
func (h *Handler) UpdateAlertRule(ctx context.Context, arc *app.RequestContext) {
	id, ok := httputil.ParseID(arc)
	if !ok {
		return
	}
	var req alert.Rule
	if !httputil.BindAndValidate(arc, &req) {
		return
	}
	if len(req.Name) > httputil.MaxNameLength {
		httputil.FailWithCode(arc, http.StatusBadRequest, errdefs.CodeBadRequest,
			"name exceeds maximum length of 255 characters")
		return
	}
	if len(req.Description) > httputil.MaxDescriptionLength {
		httputil.FailWithCode(arc, http.StatusBadRequest, errdefs.CodeBadRequest,
			"description exceeds maximum length of 1024 characters")
		return
	}
	req.ID = id
	updated, err := h.svc.UpdateRule(ctx, id, &req)
	if err != nil {
		httputil.Fail(arc, err)
		return
	}
	httputil.Success(arc, updated)
}

// DeleteAlertRule handles DELETE /api/v1/prism/alert/rules/:id.
func (h *Handler) DeleteAlertRule(ctx context.Context, arc *app.RequestContext) {
	id, ok := httputil.ParseID(arc)
	if !ok {
		return
	}
	if err := h.svc.DeleteRule(ctx, id); err != nil {
		httputil.Fail(arc, err)
		return
	}
	httputil.Success(arc, nil)
}

// ListAlertRecords handles GET /api/v1/prism/alert/records. Supported query
// parameters: page, size, severity, status, from and to (RFC3339).
func (h *Handler) ListAlertRecords(ctx context.Context, arc *app.RequestContext) {
	page, size, ok := httputil.ParsePaging(arc)
	if !ok {
		return
	}
	filter := alert.RecordFilter{
		Severity: arc.Query("severity"),
		Status:   arc.Query("status"),
	}
	if v := arc.Query("from"); v != "" {
		parsed, err := time.Parse(time.RFC3339, v)
		if err != nil {
			httputil.FailWithCode(arc, http.StatusBadRequest, errdefs.CodeBadRequest,
				"invalid 'from' timestamp, expected RFC3339 format")
			return
		}
		filter.From = parsed
	}
	if v := arc.Query("to"); v != "" {
		parsed, err := time.Parse(time.RFC3339, v)
		if err != nil {
			httputil.FailWithCode(arc, http.StatusBadRequest, errdefs.CodeBadRequest,
				"invalid 'to' timestamp, expected RFC3339 format")
			return
		}
		filter.To = parsed
	}
	items, total, err := h.svc.ListRecords(ctx, page, size, filter)
	if err != nil {
		httputil.Fail(arc, err)
		return
	}
	httputil.SuccessPage(arc, items, total, page, size)
}

// GetAlertRecord handles GET /api/v1/prism/alert/records/:id.
func (h *Handler) GetAlertRecord(ctx context.Context, arc *app.RequestContext) {
	id, ok := httputil.ParseID(arc)
	if !ok {
		return
	}
	record, err := h.svc.GetRecord(ctx, id)
	if err != nil {
		httputil.Fail(arc, err)
		return
	}
	httputil.Success(arc, record)
}

// AcknowledgeAlertRecord handles PUT /api/v1/prism/alert/records/:id/acknowledge.
func (h *Handler) AcknowledgeAlertRecord(ctx context.Context, arc *app.RequestContext) {
	id, ok := httputil.ParseID(arc)
	if !ok {
		return
	}
	record, err := h.svc.AcknowledgeRecord(ctx, id)
	if err != nil {
		httputil.Fail(arc, err)
		return
	}
	httputil.Success(arc, record)
}

// ResolveAlertRecord handles PUT /api/v1/prism/alert/records/:id/resolve.
func (h *Handler) ResolveAlertRecord(ctx context.Context, arc *app.RequestContext) {
	id, ok := httputil.ParseID(arc)
	if !ok {
		return
	}
	record, err := h.svc.ResolveRecord(ctx, id)
	if err != nil {
		httputil.Fail(arc, err)
		return
	}
	httputil.Success(arc, record)
}

// CSV export bounds: pages of 500 rows pulled from the store, capped at
// maxExportRows so a runaway export cannot stream unbounded data.
const (
	exportPageSize = 500
	maxExportRows  = 100000
)

// ExportAlertRecords handles GET /api/v1/prism/alert/records/export. It
// streams the records matching the same filters as the list endpoint
// (severity/status/from/to) as CSV, ordered by descending ID like the
// list. The response is chunked: rows stream page by page instead of
// aggregating in memory. Store failures after the headers are sent abort
// the stream (the client sees a truncated download).
func (h *Handler) ExportAlertRecords(ctx context.Context, arc *app.RequestContext) {
	filter := alert.RecordFilter{
		Severity: arc.Query("severity"),
		Status:   arc.Query("status"),
	}
	if v := arc.Query("from"); v != "" {
		parsed, err := time.Parse(time.RFC3339, v)
		if err != nil {
			httputil.FailWithCode(arc, http.StatusBadRequest, errdefs.CodeBadRequest,
				"invalid 'from' timestamp, expected RFC3339 format")
			return
		}
		filter.From = parsed
	}
	if v := arc.Query("to"); v != "" {
		parsed, err := time.Parse(time.RFC3339, v)
		if err != nil {
			httputil.FailWithCode(arc, http.StatusBadRequest, errdefs.CodeBadRequest,
				"invalid 'to' timestamp, expected RFC3339 format")
			return
		}
		filter.To = parsed
	}

	arc.SetStatusCode(http.StatusOK)
	arc.Response.Header.Set("Content-Type", "text/csv; charset=utf-8")
	arc.Response.Header.Set("Content-Disposition", `attachment; filename="alert_records.csv"`)
	// A pipe body stream (chunked, size -1) keeps memory bounded: rows flow
	// to the client page by page. The producer goroutine runs on a
	// background context because the stream outlives this handler; client
	// disconnects surface as write errors on the pipe.
	pr, pw := io.Pipe()
	arc.Response.SetBodyStream(pr, -1)
	go func() {
		err := h.streamRecordsCSV(pw, filter)
		_ = pw.CloseWithError(err) // nil error closes the pipe for a clean EOF
	}()
}

// streamRecordsCSV pages through the store and writes the CSV body to w.
// It returns nil on completion (or when the row cap truncates the export)
// and the store error otherwise.
func (h *Handler) streamRecordsCSV(w io.Writer, filter alert.RecordFilter) error {
	cw := csv.NewWriter(w)
	if err := cw.Write([]string{
		"id", "rule_id", "rule_name", "severity", "value", "message",
		"event_id", "status", "triggered_at", "acknowledged_at", "resolved_at", "created_at",
	}); err != nil {
		return err
	}
	written := 0
	for page := 1; written < maxExportRows; page++ {
		items, _, err := h.svc.ListRecords(context.Background(), page, exportPageSize, filter)
		if err != nil {
			return err
		}
		for _, rec := range items {
			if err := cw.Write(recordCSVRow(rec)); err != nil {
				return err
			}
			written++
			if written >= maxExportRows {
				break
			}
		}
		cw.Flush()
		if err := cw.Error(); err != nil {
			return err
		}
		if len(items) < exportPageSize {
			return nil
		}
	}
	return nil
}

// recordCSVRow renders one record as a CSV row; timestamps are RFC3339,
// absent acknowledge/resolve times render as empty fields.
func recordCSVRow(rec *alert.Record) []string {
	acknowledged, resolved := "", ""
	if rec.AcknowledgedAt != nil {
		acknowledged = rec.AcknowledgedAt.Format(time.RFC3339)
	}
	if rec.ResolvedAt != nil {
		resolved = rec.ResolvedAt.Format(time.RFC3339)
	}
	return []string{
		strconv.FormatInt(rec.ID, 10),
		strconv.FormatInt(rec.RuleID, 10),
		rec.RuleName,
		rec.Severity,
		strconv.FormatFloat(rec.Value, 'f', -1, 64),
		rec.Message,
		rec.EventID,
		rec.Status,
		rec.TriggeredAt.Format(time.RFC3339),
		acknowledged,
		resolved,
		rec.CreatedAt.Format(time.RFC3339),
	}
}
