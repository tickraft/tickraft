// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package alert

import (
	"context"
	"time"
)

// Rule represents an alert rule definition backed by an expr-lang
// expression evaluated by the rule engine.
type Rule struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Scene       string    `json:"scene"`              // task, probe, metric, remediation
	Expression  string    `json:"expression"`         // expr-lang source text
	Priority    int       `json:"priority,omitempty"` // higher fires first
	Enabled     bool      `json:"enabled"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Record represents a historical alert event.
type Record struct {
	ID             int64      `json:"id"`
	RuleID         int64      `json:"rule_id"`
	RuleName       string     `json:"rule_name"`
	Severity       string     `json:"severity,omitempty"` // info, warning, critical
	Value          float64    `json:"value"`
	Status         string     `json:"status"` // firing, acknowledged, resolved
	Message        string     `json:"message"`
	FiredAt        time.Time  `json:"fired_at"`
	AcknowledgedAt *time.Time `json:"acknowledged_at,omitempty"`
	ResolvedAt     *time.Time `json:"resolved_at,omitempty"`
}

// RecordFilter holds optional server-side filtering criteria for listing
// alert records. A zero-value filter matches all records.
type RecordFilter struct {
	// Severity filters by an exact severity match (info/warning/critical).
	Severity string
	// Status filters by an exact status match (firing/acknowledged/resolved).
	Status string
	// From restricts the result to records fired at or after this RFC3339
	// time.
	From time.Time
	// To restricts the result to records fired at or before this RFC3339
	// time.
	To time.Time
}

// Service defines the operations for managing alert rules and records.
type Service interface {
	// ListRules returns a page of alert rules and the total count.
	ListRules(ctx context.Context, page, size int) ([]Rule, int64, error)
	// GetRule returns a single alert rule by ID.
	GetRule(ctx context.Context, id int64) (*Rule, error)
	// CreateRule creates a new alert rule from the given request.
	CreateRule(ctx context.Context, req *Rule) (*Rule, error)
	// UpdateRule updates an existing alert rule identified by ID.
	UpdateRule(ctx context.Context, id int64, req *Rule) (*Rule, error)
	// DeleteRule deletes an alert rule by ID.
	DeleteRule(ctx context.Context, id int64) error
	// ListRecords returns a page of alert records matching the filter and
	// the total count.
	ListRecords(ctx context.Context, page, size int, filter RecordFilter) ([]Record, int64, error)
	// GetRecord returns a single alert record by ID.
	GetRecord(ctx context.Context, id int64) (*Record, error)
	// AcknowledgeRecord transitions the alert record identified by ID to the
	// "acknowledged" status and sets acknowledged_at to the current time.
	// It returns the updated record. Returns ErrRecordNotFound when no
	// record with the given ID exists.
	AcknowledgeRecord(ctx context.Context, id int64) (*Record, error)
	// ResolveRecord transitions the alert record identified by ID to the
	// "resolved" status and sets resolved_at to the current time. It
	// returns the updated record. Returns ErrRecordNotFound when no
	// record with the given ID exists.
	ResolveRecord(ctx context.Context, id int64) (*Record, error)
}
