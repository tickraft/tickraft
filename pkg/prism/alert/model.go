// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package alert

import (
	"time"

	"gorm.io/gorm"

	// Registers the tolerantjson serializer that Rule.Metadata depends on.
	// The blank import guarantees registration before any schema parse.
	_ "github.com/tickraft/tickraft/pkg/db"
)

// Rule is the alert rule model: the single representation shared by the
// persistence layer (sys_prism_alert_rule), the evaluation engine, and the
// CRUD API. It carries both gorm and json tags; internal columns
// (TenantID, DeletedAt) are excluded from serialization so the API can
// never read or bind them.
type Rule struct {
	// ID is the auto-incremented primary key.
	ID int64 `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	// TenantID scopes the rule to a tenant for multi-tenant isolation.
	// 0 is the global scope matched against every event; tenant
	// filtering is an engine internal, never an expression variable.
	TenantID int64 `gorm:"column:tenant_id;not null;index" json:"-"`
	// Name is the human-readable rule name.
	Name string `gorm:"column:name;type:varchar(255);not null" json:"name"`
	// Description is an optional free-form rule description.
	Description string `gorm:"column:description;type:text" json:"description,omitempty"`
	// Expression is the expr-lang source text compiled by the Compiler.
	Expression string `gorm:"column:expression;type:text;not null" json:"expression"`
	// Enabled indicates whether the rule participates in matching. The
	// column has no gorm default: a default tag would make GORM omit the
	// zero value on insert, silently persisting enabled=true for rules
	// created with Enabled=false.
	Enabled bool `gorm:"column:enabled;not null;index" json:"enabled"`
	// Priority orders rules; higher values fire first.
	Priority int `gorm:"column:priority;not null;default:0" json:"priority,omitempty"`
	// GroupID is the resource group the rule belongs to. nil means the
	// rule is tenant-wide (visible to all members); a non-nil value
	// restricts visibility to members assigned to that group (B1-04).
	GroupID *int64 `gorm:"column:group_id;index" json:"group_id,omitempty"`
	// Metadata holds extension key-value pairs, persisted as JSON in a
	// text column via the tolerantjson serializer. Reads never fail: an
	// empty, "null", or malformed column decodes to nil.
	Metadata map[string]string `gorm:"column:metadata;type:text;serializer:tolerantjson" json:"metadata,omitempty"`
	// CreatedAt is the rule creation timestamp. Static rules loaded from
	// configuration files carry zero-value timestamps.
	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	// UpdatedAt is the rule last-update timestamp.
	UpdatedAt time.Time `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
	// DeletedAt records the soft-delete timestamp.
	DeletedAt gorm.DeletedAt `gorm:"column:deleted_at;index" json:"-"`
}

// TableName returns the database table name for Rule.
func (Rule) TableName() string { return "sys_prism_alert_rule" }

// Alert record lifecycle statuses stored in Record.Status and the
// sys_prism_alert_record.status column. The GORM default tag on Record.Status
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

// Record is the GORM model for the sys_prism_alert_record table.
//
// It persists alert records generated when the alert engine evaluates a rule
// match. Records are append-only except for lifecycle transitions:
//   - firing -> acknowledged: AcknowledgedAt populated by Acknowledge.
//   - firing/acknowledged -> resolved: ResolvedAt populated by Resolve.
//
// RuleID is a plain foreign key referencing sys_prism_alert_rule.id
// (managed by this package's rule Store, see store.go). The
// association is not declared as a GORM belongs-to relation because
// records and rules are persisted and reloaded independently; callers
// that need rule context resolve it via the rule Store.
type Record struct {
	ID       int64   `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	RuleID   int64   `gorm:"column:rule_id;not null;index" json:"rule_id"`
	RuleName string  `gorm:"column:rule_name;type:varchar(255);not null" json:"rule_name"`
	Severity string  `gorm:"column:severity;type:varchar(32);not null;default:'warning'" json:"severity"`
	Value    float64 `gorm:"column:value;not null;default:0" json:"value"`
	Message  string  `gorm:"column:message;type:varchar(1024)" json:"message,omitempty"`
	// EventID is the engine-assigned event identifier shared by every
	// record created from one dispatch. It is the correlation key inbound
	// card callbacks use to locate records without exposing record IDs in
	// IM payloads. Empty on records persisted before the column existed.
	EventID string `gorm:"column:event_id;type:varchar(64);index" json:"event_id,omitempty"`
	// firing, acknowledged, resolved
	Status         string     `gorm:"column:status;type:varchar(16);not null;default:'firing'" json:"status"`
	TriggeredAt    time.Time  `gorm:"column:triggered_at;not null;index" json:"triggered_at"`
	AcknowledgedAt *time.Time `gorm:"column:acknowledged_at" json:"acknowledged_at,omitempty"`
	ResolvedAt     *time.Time `gorm:"column:resolved_at" json:"resolved_at,omitempty"`
	CreatedAt      time.Time  `gorm:"column:created_at;autoCreateTime" json:"created_at"`
}

// TableName returns the database table name.
func (Record) TableName() string { return "sys_prism_alert_record" }
