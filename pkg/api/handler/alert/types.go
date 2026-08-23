// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package alert

import (
	"context"

	"github.com/tickraft/tickraft/pkg/prism/alert"
)

// The wire and storage shapes are the same model: this package carries no
// Rule/Record DTOs. alert.Rule and alert.Record hold both gorm and
// json tags, and internal columns (TenantID, DeletedAt) serialize to nothing,
// so handlers bind and return the model types directly. See
// docs/model-layering-design.md for the layering contract.

// Service defines the operations for managing alert rules and records.
type Service interface {
	// ListRules returns a page of alert rules and the total count.
	ListRules(ctx context.Context, page, size int) ([]*alert.Rule, int64, error)
	// GetRule returns a single alert rule by ID.
	GetRule(ctx context.Context, id int64) (*alert.Rule, error)
	// CreateRule creates a new alert rule from the given request.
	CreateRule(ctx context.Context, req *alert.Rule) (*alert.Rule, error)
	// UpdateRule updates an existing alert rule identified by ID.
	UpdateRule(ctx context.Context, id int64, req *alert.Rule) (*alert.Rule, error)
	// DeleteRule deletes an alert rule by ID.
	DeleteRule(ctx context.Context, id int64) error
	// ListRecords returns a page of alert records matching the filter and
	// the total count.
	ListRecords(ctx context.Context, page, size int, filter alert.RecordFilter) ([]*alert.Record, int64, error)
	// GetRecord returns a single alert record by ID.
	GetRecord(ctx context.Context, id int64) (*alert.Record, error)
	// AcknowledgeRecord transitions the alert record identified by ID to the
	// "acknowledged" status and sets acknowledged_at to the current time.
	// It returns the updated record. Returns ErrRecordNotFound when no
	// record with the given ID exists.
	AcknowledgeRecord(ctx context.Context, id int64) (*alert.Record, error)
	// ResolveRecord transitions the alert record identified by ID to the
	// "resolved" status and sets resolved_at to the current time. It
	// returns the updated record. Returns ErrRecordNotFound when no
	// record with the given ID exists.
	ResolveRecord(ctx context.Context, id int64) (*alert.Record, error)
}
