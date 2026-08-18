// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package alert

import "time"

// Alert record lifecycle statuses stored in Record.Status and the
// sys_prism_record.status column. The GORM default tag on Record.Status
// keeps the literal "firing" because struct tags cannot reference
// constants.
const (
	// StatusFiring marks a record that is active and not yet acknowledged.
	StatusFiring = "firing"
	// StatusAcknowledged marks a record that has been acknowledged by an
	// operator but is not yet resolved.
	StatusAcknowledged = "acknowledged"
	// StatusResolved marks a record whose underlying condition has been
	// resolved.
	StatusResolved = "resolved"
)

// Record is the GORM model for the sys_prism_record table.
//
// It persists alert records generated when the alert engine evaluates a rule
// match. Records are append-only except for lifecycle transitions:
//   - firing -> acknowledged: AcknowledgedAt populated by Acknowledge.
//   - firing/acknowledged -> resolved: ResolvedAt populated by Resolve.
//
// RuleID is a plain foreign key referencing sys_prism_rule.id (managed by
// the rule package). The association is not declared as a GORM belongs-to
// relation because the Rule persistence model lives in pkg/prism/rule;
// callers that need rule context resolve it via the rule.Store.
type Record struct {
	ID       int64   `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	RuleID   int64   `gorm:"column:rule_id;not null;index" json:"rule_id"`
	RuleName string  `gorm:"column:rule_name;type:varchar(255);not null" json:"rule_name"`
	Severity string  `gorm:"column:severity;type:varchar(32);not null;default:'warning'" json:"severity"`
	Value    float64 `gorm:"column:value;not null;default:0" json:"value"`
	Message  string  `gorm:"column:message;type:varchar(1024)" json:"message,omitempty"`
	// firing, acknowledged, resolved
	Status         string     `gorm:"column:status;type:varchar(16);not null;default:'firing'" json:"status"`
	TriggeredAt    time.Time  `gorm:"column:triggered_at;not null;index" json:"triggered_at"`
	AcknowledgedAt *time.Time `gorm:"column:acknowledged_at" json:"acknowledged_at,omitempty"`
	ResolvedAt     *time.Time `gorm:"column:resolved_at" json:"resolved_at,omitempty"`
	CreatedAt      time.Time  `gorm:"column:created_at;autoCreateTime" json:"created_at"`
}

// TableName returns the database table name.
func (Record) TableName() string { return "sys_prism_record" }
