// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package remediation

import (
	"time"

	"gorm.io/gorm"

	"github.com/tickraft/tickraft/pkg/types"
)

// TriggerType identifies the category of event that activates a remediation
// rule. It maps 1:1 to the event types the Manager subscribes to.
type TriggerType string

const (
	// TriggerMetric activates on metric threshold breaches
	// (event.TypeTelemetryMetricExceeded).
	TriggerMetric TriggerType = TriggerType(types.EventKindMetric)
	// TriggerLog activates on log keyword matches
	// (event.TypeTelemetryLogMatched).
	TriggerLog TriggerType = TriggerType(types.EventKindLog)
	// TriggerStatusChange activates on asset status transitions
	// (event.TypeAssetStatusChanged).
	TriggerStatusChange TriggerType = TriggerType(types.EventKindStatusChange)
)

// RuleStatus is the operational status of a remediation rule.
type RuleStatus string

const (
	// StatusActive means the rule participates in evaluation.
	StatusActive RuleStatus = "active"
	// StatusPaused means the rule is skipped (e.g. tripped circuit breaker).
	StatusPaused RuleStatus = "paused"
)

// Rule is the GORM model for the sys_prism_remediation_rule table.
//
// It persists remediation rule definitions that the Manager evaluates
// against incoming events. When an event matches a rule's trigger type and
// condition expression, the rule's configured operator is invoked to perform
// automated remediation.
//
// The default deployment supports only the LocalOperator (ExecutorType
// "local"); the ExecutorConfig JSON carries {command, args, env}. Extended
// editions embed this model to add verification probes and operator-specific
// columns while sharing the same table.
type Rule struct {
	// ID is the auto-incremented primary key.
	ID int64 `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	// TenantID scopes the rule to a tenant for multi-tenant isolation.
	// The runtime is single-tenant: this field is always 0.
	// The extended runtime injects the actual tenant ID.
	TenantID int64 `gorm:"column:tenant_id;not null;index;default:0" json:"tenant_id"`
	// Name is the human-readable rule name.
	Name string `gorm:"column:name;type:varchar(255);not null" json:"name"`
	// Description is an optional free-form rule description.
	Description string `gorm:"column:description;type:varchar(1024)" json:"description,omitempty"`
	// AssetID scopes the rule to a specific asset. A value of 0 means
	// global match across all assets.
	AssetID int64 `gorm:"column:asset_id;not null;index;default:0" json:"asset_id"`
	// TriggerEventType is the event type that activates this rule.
	// Valid values: metric, log, status_change.
	TriggerEventType string `gorm:"column:trigger_event_type;type:varchar(32);not null" json:"trigger_event_type"`
	// Expression is the optional trigger condition evaluated against
	// RemediationEnv variables. An empty expression matches all events
	// of the trigger type.
	Expression string `gorm:"column:expression;type:text" json:"expression,omitempty"`
	// ExecutorType identifies which operator to invoke on match.
	// The default deployment supports "local" only.
	ExecutorType string `gorm:"column:executor_type;type:varchar(64);not null" json:"executor_type"`
	// ExecutorConfig is the JSON-encoded operator configuration. For the
	// local operator it carries {command, args, env}; an optional
	// "expression" key defines the execution judgment and an optional
	// "timeout" key bounds the action duration.
	ExecutorConfig string `gorm:"column:executor_config;type:text" json:"executor_config,omitempty"`
	// Cooldown is the minimum interval in seconds between consecutive
	// executions of this rule. The column has no gorm default so an
	// explicit zero (no cooldown) survives Create.
	Cooldown int `gorm:"column:cooldown;not null" json:"cooldown"`
	// CircuitBreakerThreshold is the consecutive failure count after which
	// the circuit breaker trips and pauses the rule. Zero disables the
	// breaker; the column has no gorm default so the explicit value
	// survives Create.
	CircuitBreakerThreshold int `gorm:"column:circuit_breaker_threshold;not null" json:"circuit_breaker_threshold"` //nolint:revive // struct tag cannot be line-wrapped
	// Enabled indicates whether the rule participates in evaluation. The
	// column has no gorm default: a default tag would make GORM omit the
	// zero value on insert, silently persisting enabled=true for rules
	// created with Enabled=false.
	Enabled bool `gorm:"column:enabled;not null;index" json:"enabled"`
	// Status is the operational status of the rule: active or paused.
	Status string `gorm:"column:status;type:varchar(16);not null;default:'active'" json:"status"`
	// LastRunAt records the last execution timestamp, used for cooldown
	// enforcement. Nullable.
	LastRunAt *time.Time `gorm:"column:last_run_at;index" json:"last_run_at,omitempty"`
	// ConsecutiveFailures is the circuit breaker's running count of
	// consecutive execution failures. Updated atomically by the store
	// (RecordExecutionOutcome); never touched by the CRUD Update path.
	ConsecutiveFailures int `gorm:"column:consecutive_failures;not null;default:0" json:"consecutive_failures"`
	// Metadata is a reserved JSON blob for extension key-value pairs.
	// The circuit breaker no longer stores its counter here (it moved to
	// the consecutive_failures column).
	Metadata string `gorm:"column:metadata;type:text" json:"metadata,omitempty"`
	// CreatedAt is the rule creation timestamp.
	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	// UpdatedAt is the rule last-update timestamp.
	UpdatedAt time.Time `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
	// DeletedAt records the soft-delete timestamp. Soft-deleted rows are
	// excluded from all queries by GORM's default scope.
	DeletedAt gorm.DeletedAt `gorm:"column:deleted_at;index" json:"-"`
}

// TableName returns the database table name for Rule.
func (Rule) TableName() string { return "sys_prism_remediation_rule" }
