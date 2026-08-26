// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package service

import (
	"context"

	"github.com/tickraft/tickraft/pkg/prism/remediation"
)

// The wire and storage shapes are the same model: this package carries no
// Rule/Record DTOs. prismremediation.Rule and prismremediation.Record hold
// both gorm and json tags, and internal columns (TenantID, Metadata,
// DeletedAt, UpdatedAt) serialize to nothing, so handlers bind and return
// the model types directly. See docs/model-layering-design.md for the
// layering contract.

// Service defines the operations for managing remediation
// rules. The concrete implementation is injected via the
// WithRemediationRuleService RouteOption; when omitted, the handler package
// falls back to an in-memory implementation.
type Service interface {
	// ListRules returns a page of remediation rules and the total count.
	ListRules(ctx context.Context, page, size int) ([]*remediation.Rule, int64, error)
	// GetRule returns a single remediation rule by ID.
	GetRule(ctx context.Context, id int64) (*remediation.Rule, error)
	// CreateRule creates a new remediation rule from the given request.
	CreateRule(ctx context.Context, req *remediation.Rule) (*remediation.Rule, error)
	// UpdateRule updates an existing remediation rule identified by ID.
	UpdateRule(ctx context.Context, id int64, req *remediation.Rule) (*remediation.Rule, error)
	// DeleteRule deletes a remediation rule by ID.
	DeleteRule(ctx context.Context, id int64) error
	// ListRecords returns a page of remediation dispatch records and the
	// total count, optionally filtered by lifecycle status.
	ListRecords(ctx context.Context, page, size int, status string) ([]*remediation.Record, int64, error)
}
