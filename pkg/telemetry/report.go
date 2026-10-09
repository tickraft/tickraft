// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package telemetry

import (
	"context"
	"time"

	"github.com/tickraft/tickraft/pkg/event"
)

// TaskReport is a remote task status report received through the unified
// telemetry endpoint (POST /api/v1/telemetry) with kind task_status. Mode A
// tasks (report_status=true) only record the dispatch lifecycle locally; the
// real execution outcome arrives here, keyed by task_ref — the dispatch
// credential handed to the remote via the X-Tickraft-Task-Ref header or the
// {{task_ref}} variable (a reporter that only knows the task number sends it
// as a decimal string instead).
//
// The HTTP listener hands parsed reports to the callback configured via the
// http.WithTaskReport option. Assemblies that configure the callback bridge
// it onto the event bus; assemblies that do not (the distributed collector
// deployment) keep the reports in the raw ingest pipeline instead.
//
// Status vocabulary: running / completed / failed / timeout (per execution).
type TaskReport struct {
	// Kind selects the report category: KindTaskStatus.
	Kind Kind
	// TaskRef identifies the reported execution: the dispatch credential
	// (the run handle) or the task number as a decimal string.
	TaskRef string
	// Status is the reported status value (running/completed/failed/timeout).
	Status string
	// StartedAt is the reported execution start time. Zero when the reporter
	// omits it.
	StartedAt time.Time
	// FinishedAt is the reported execution finish time. Zero while running.
	FinishedAt time.Time
	// Output is the reported execution output.
	Output string
	// Error is the reported execution error message.
	Error string
	// Reason describes the status transition.
	Reason string
	// TenantID is the tenant resolved from the reporter's credential (or the
	// request's asset identity) at ingestion time.
	TenantID int64
}

// TaskReportCallback consumes parsed task status reports. Implementations
// must be safe for concurrent use.
type TaskReportCallback func(ctx context.Context, report *TaskReport)

// PublishTaskReport publishes a parsed report as a task.status_reported
// event on the given bus. It is the assembly-side bridge between the HTTP
// listener's task-report callback and the task domain's report consumer:
// route wiring stays in the assembly while the two modules communicate
// only through the event bus.
func PublishTaskReport(ctx context.Context, bus event.Bus, report *TaskReport) error {
	payload := event.TaskReportPayload{
		Kind:     string(report.Kind),
		TaskRef:  report.TaskRef,
		Status:   report.Status,
		Output:   report.Output,
		Error:    report.Error,
		Reason:   report.Reason,
		TenantID: report.TenantID,
	}
	if !report.StartedAt.IsZero() {
		payload.StartedAt = report.StartedAt.UnixNano()
	}
	if !report.FinishedAt.IsZero() {
		payload.FinishedAt = report.FinishedAt.UnixNano()
	}
	return event.Publish(ctx, bus, event.TypeTaskStatusReported, payload)
}
