// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package executor

import (
	"time"

	"github.com/tickraft/tickraft/pkg/types"
)

// ExecutionRequest is the execution context passed to an executor.
// It is constructed by the Runner from an ExecutionPayload and carries all
// information required to execute a task.
type ExecutionRequest struct {
	// ID is the unique task identifier.
	ID int64
	// TenantID is the tenant identifier used for multi-tenant isolation.
	TenantID int64
	// AssetID is the associated asset identifier.
	AssetID int64
	// ExecutorName identifies the executor to look up in the registry.
	ExecutorName string
	// Config stores executor-specific configuration as a JSON string.
	Config string
	// Operation represents the operation type (OpProbe or OpExecute).
	Operation Operation
	// Timeout is the maximum execution duration.
	Timeout time.Duration
	// MaxRetries is the number of retry attempts after a failed
	// execution; 0 disables retries.
	MaxRetries int
	// RetryInterval is the delay between retry attempts; zero applies the
	// retry policy's default spacing.
	RetryInterval time.Duration
	// RunID is the unique identifier of this execution run, used for
	// idempotency control. It is propagated to the execution record.
	RunID string
	// TriggerType records the execution trigger source ("schedule",
	// "manual", "event"). It is propagated to the execution record.
	TriggerType string
	// TriggeredAt is the moment the runner received the trigger event,
	// captured at dispatch. The gap between TriggeredAt and the record's
	// StartedAt is the worker pool pickup delay. It is propagated to the
	// execution record.
	TriggeredAt time.Time
	// ReportStatus marks a Mode A task (remote status reporting): the
	// executor outcome is only the dispatch result. A running execution
	// row is opened before the executor runs and stays running until the
	// remote reporter closes it via the telemetry report endpoint.
	ReportStatus bool
	// ExecutionID is the sys_schedule_execution row ID opened by the dispatch
	// store for a ReportStatus task. Zero on Mode B tasks (and on Mode A
	// tasks when no dispatch store is injected). Executors use it to stamp
	// the dispatch identity headers.
	ExecutionID int64
	// Metadata holds optional key-value extension data.
	Metadata map[string]string
}

// ExecutionRecord captures the result of a single task execution.
type ExecutionRecord struct {
	// TaskID is the unique task identifier.
	TaskID int64
	// TenantID is the tenant identifier.
	TenantID int64
	// AssetID is the associated asset identifier.
	AssetID int64
	// ExecutorName identifies the executor that was used.
	ExecutorName string
	// Operation represents the operation type (OpProbe or OpExecute).
	Operation Operation
	// Status is the execution result status.
	Status types.AssetStatus
	// StatusCode is the protocol-specific status code.
	StatusCode int
	// Output contains the execution output.
	Output string
	// ErrorMsg describes the error when execution failed.
	ErrorMsg string
	// Duration is the total execution duration.
	Duration time.Duration
	// RetryCount is the number of retries attempted.
	RetryCount int
	// StartedAt is the execution start time.
	StartedAt time.Time
	// FinishedAt is the execution completion time.
	FinishedAt time.Time
	// RunID is the unique identifier of this execution run, used for
	// idempotency control. Propagated from ExecutionRequest.
	RunID string
	// TriggerType records the execution trigger source ("schedule",
	// "manual", "event"). Propagated from ExecutionRequest.
	TriggerType string
	// TriggeredAt is the moment the runner received the trigger event.
	// Propagated from ExecutionRequest; the gap to StartedAt is the worker
	// pool pickup delay.
	TriggeredAt time.Time
	// Node is the hostname of the worker that executed the task, for
	// locating executions in multi-node deployments.
	Node string
	// ExitCode is the process exit code for command executors (local);
	// 0 for protocol executors and successful runs. Propagated from the
	// executor Result.
	ExitCode int
	// TimedOut marks an execution that ended because the runner context
	// deadline expired. It is orthogonal to Status (which stays in the
	// asset vocabulary): consumers translate it into the persisted
	// execution vocabulary's distinct "timeout" state.
	TimedOut bool
}

// TargetConfig describes the target configuration for an execution action.
// This is a helper type used internally by executors when parsing config.
type TargetConfig struct {
	// AssetID is the associated asset ID.
	AssetID int64 `json:"asset_id"`
	// AssetType identifies the target asset type.
	AssetType types.AssetType `json:"asset_type"`
	// Address is the target IP, domain, or URL.
	Address string `json:"address,omitempty"`
	// Port is the target port number.
	Port int `json:"port,omitempty"`
	// Params holds executor/prober-specific parameters.
	Params map[string]string `json:"params,omitempty"`
}

// Result holds the outcome of an execution action.
type Result struct {
	// Status is the execution result status.
	Status types.AssetStatus
	// StatusCode is the protocol-specific status code (e.g. HTTP 200).
	StatusCode int
	// ExitCode is the process exit code for command executors (local).
	// It is 0 for protocol executors, 0 on success, the process exit code
	// on failure, and -1 when no exit code is available (command not
	// found, killed by signal). It backs the "code" variable of the
	// execution judgment env.
	ExitCode int
	// Body contains the response body or execution output.
	Body string
	// ErrorMsg describes the error when execution failed.
	ErrorMsg string
	// Duration is the total execution duration.
	Duration time.Duration
	// Metrics carries optional numeric metrics from the execution.
	Metrics map[string]float64
}
