// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package remediation

import (
	"context"
	"time"
)

// RuleStore defines the persistence operations for remediation rules.
// Implementations must be safe for concurrent use and enforce tenant
// isolation on every query.
//
// The interface lives in the remediation domain because rule persistence is
// a remediation concern. The GORM-backed implementation lives in this
// package (store.go, see NewStore). The default Engine consumes it
// directly; the callers wraps it to add extended columns.
type RuleStore interface {
	// GetRules returns enabled remediation rules for the given tenant,
	// asset, and trigger type. An assetID of 0 matches global rules that
	// apply across all assets; implementations should also return global
	// rules (asset_id = 0) alongside asset-scoped rules.
	GetRules(ctx context.Context, tenantID int64, assetID int64, triggerType string) ([]*Rule, error)
	// UpdateRuleStatus updates the rule's operational status. It is the
	// resume path for rules paused by the circuit breaker.
	UpdateRuleStatus(ctx context.Context, ruleID int64, status string) error
	// UpdateLastRun records the last execution timestamp for the rule,
	// used by the cooldown check on subsequent triggers.
	UpdateLastRun(ctx context.Context, ruleID int64, lastRunAt time.Time) error
	// RecordExecutionOutcome atomically updates the circuit breaker state
	// after an execution: success resets the consecutive-failure counter,
	// failure increments it and pauses the rule when the threshold is
	// reached.
	RecordExecutionOutcome(ctx context.Context, ruleID int64, success bool) error
}

// ExecutionRequest is the remediation execution context passed to an
// Operator. It is constructed by the Engine from a matched Rule and the
// triggering EventContext.
type ExecutionRequest struct {
	// RuleID is the matched rule identifier.
	RuleID int64
	// RuleName is the matched rule name, for logging.
	RuleName string
	// TenantID is the tenant identifier (0 in the runtime).
	TenantID int64
	// AssetID is the associated asset identifier.
	AssetID int64
	// RunID is the unique identifier of this remediation run, used for
	// idempotency control and tracing.
	RunID string
	// Config is the JSON-encoded operator configuration copied from the
	// Rule.ExecutorConfig.
	Config string
	// Timeout is the maximum execution duration.
	Timeout time.Duration
}

// ExecutionResult holds the outcome of a remediation execution.
type ExecutionResult struct {
	// Success indicates whether the operator reported a normal outcome.
	Success bool
	// Output contains the execution output (stdout for local scripts).
	Output string
	// ErrorMsg describes the error when execution failed.
	ErrorMsg string
	// Duration is the total execution duration.
	Duration time.Duration
}

// Operator is the SPI that remediation action executors implement. The
// default deployment registers only the LocalOperator; callers
// register additional operators (ssh, mysql, redis, ...) against the same
// interface and inject them via WithOperators.
//
// Implementations must be safe for concurrent use.
type Operator interface {
	// Name returns the operator identifier matching Rule.ExecutorType
	// (e.g. "local").
	Name() string
	// Execute performs the remediation action. A non-nil error indicates an
	// infrastructure failure (operator unavailable); a nil error with
	// Success=false indicates the action ran but failed (non-zero exit,
	// timeout). The circuit breaker counts the latter.
	Execute(ctx context.Context, req ExecutionRequest) (*ExecutionResult, error)
}

// RecordStore defines the persistence operations for remediation dispatch
// records. The Engine upserts one row per run as the dispatch progresses;
// the records API reads rows through ListRecords.
type RecordStore interface {
	// UpsertRecord inserts the record when no row with the same RunID
	// exists, or updates the existing row's lifecycle fields (status,
	// error, started_at, finished_at) otherwise.
	UpsertRecord(ctx context.Context, record *Record) error
	// ListRecords returns a page of dispatch records ordered by descending
	// ID, plus the total count. A non-empty status filters by exact
	// lifecycle status match. page is 1-based; size is normalized by
	// pagination.Clamp.
	ListRecords(ctx context.Context, page, size int, status string) ([]*Record, int64, error)
}

// Service defines the operations for managing remediation
// rules. The concrete implementation is injected via the
// WithRemediationRuleService RouteOption; when omitted, the handler package
// falls back to an in-memory implementation.
//
// The wire and storage shapes are the same model: there are no Rule/Record
// DTOs. Rule and Record hold both gorm and json tags, and internal columns
// (TenantID, Metadata, DeletedAt, UpdatedAt) serialize to nothing, so
// handlers bind and return the model types directly. See
// docs/model-layering-design.md for the layering contract.
type Service interface {
	// ListRules returns a page of remediation rules and the total count.
	ListRules(ctx context.Context, page, size int) ([]*Rule, int64, error)
	// GetRule returns a single remediation rule by ID.
	GetRule(ctx context.Context, id int64) (*Rule, error)
	// CreateRule creates a new remediation rule from the given request.
	CreateRule(ctx context.Context, req *Rule) (*Rule, error)
	// UpdateRule updates an existing remediation rule identified by ID.
	UpdateRule(ctx context.Context, id int64, req *Rule) (*Rule, error)
	// DeleteRule deletes a remediation rule by ID.
	DeleteRule(ctx context.Context, id int64) error
	// ListRecords returns a page of remediation dispatch records and the
	// total count, optionally filtered by lifecycle status.
	ListRecords(ctx context.Context, page, size int, status string) ([]*Record, int64, error)
}
