// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package remediation

import (
	"context"
	"time"
)

// Rule represents a self-healing (remediation) rule definition
// managed through the CRUD API at /api/v1/prism/remediation/rules. A rule
// binds a trigger event type (metric, log, status_change) to an executor
// action (e.g., local script) with optional condition filtering, cooldown,
// and circuit-breaker safety mechanisms. The open-source edition supports
// the "local" executor type; additional types are injected via the Operator
// SPI.
type Rule struct {
	ID                      int64      `json:"id"`
	Name                    string     `json:"name"`
	Description             string     `json:"description,omitempty"`
	AssetID                 int64      `json:"asset_id"`
	TriggerEventType        string     `json:"trigger_event_type"`
	ConditionExpr           string     `json:"condition_expr,omitempty"`
	ExecutorType            string     `json:"executor_type"`
	ExecutorConfig          string     `json:"executor_config,omitempty"`
	Cooldown                int        `json:"cooldown"`
	CircuitBreakerThreshold int        `json:"circuit_breaker_threshold"`
	Enabled                 bool       `json:"enabled"`
	Status                  string     `json:"status,omitempty"`
	LastRunAt               *time.Time `json:"last_run_at,omitempty"`
	CreatedAt               time.Time  `json:"created_at"`
	UpdatedAt               time.Time  `json:"updated_at"`
}

// Service defines the operations for managing remediation
// rules. The concrete implementation is injected via the
// WithRemediationRuleService RouteOption; when omitted, the handler package
// falls back to an in-memory implementation.
type Service interface {
	// ListRules returns a page of remediation rules and the total count.
	ListRules(ctx context.Context, page, size int) ([]Rule, int64, error)
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
	ListRecords(ctx context.Context, page, size int, status string) ([]Record, int64, error)
}

// Record represents a single remediation dispatch lifecycle record. One
// record is persisted per run and updated as the dispatch progresses
// through the triggered -> started -> completed/failed states. Skipped
// dispatches (cooldown, circuit breaker, missing operator) persist a record
// with status "skipped".
type Record struct {
	// ID is the record identifier.
	ID int64 `json:"id"`
	// RuleID references the remediation rule that produced this record.
	RuleID int64 `json:"rule_id"`
	// RuleName is the rule name snapshot at trigger time.
	RuleName string `json:"rule_name"`
	// AssetID is the numeric asset identifier of the triggering event.
	AssetID int64 `json:"asset_id"`
	// AssetKey is the asset key of the triggering event, when available.
	AssetKey string `json:"asset_key,omitempty"`
	// RunID is the unique identifier of this remediation run.
	RunID string `json:"run_id"`
	// Trigger is the trigger type that activated the rule: metric, log,
	// or status_change.
	Trigger string `json:"trigger"`
	// Status is the dispatch lifecycle state: triggered, started,
	// completed, skipped, or failed.
	Status string `json:"status"`
	// Error captures the failure or skip message when the dispatch did
	// not complete successfully.
	Error string `json:"error,omitempty"`
	// StartedAt is the time the operator started the dispatch.
	StartedAt *time.Time `json:"started_at,omitempty"`
	// FinishedAt is the time the dispatch finished.
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	// CreatedAt records when the record was created.
	CreatedAt time.Time `json:"created_at"`
}
