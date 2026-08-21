// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
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
	// RunID is the unique identifier of this execution run, used for
	// idempotency control. It is propagated to the execution record.
	RunID string
	// TriggerType records the execution trigger source ("schedule",
	// "manual", "event"). It is propagated to the execution record.
	TriggerType string
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
