// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package remediation

import (
	"time"

	"gorm.io/gorm"
)

// TriggerType identifies the category of event that activates a remediation
// rule. It maps 1:1 to the event types the Manager subscribes to.
type TriggerType string

const (
	// TriggerMetric activates on metric threshold breaches
	// (event.TypeTelemetryMetricExceeded).
	TriggerMetric TriggerType = "metric"
	// TriggerLog activates on log keyword matches
	// (event.TypeTelemetryLogMatched).
	TriggerLog TriggerType = "log"
	// TriggerStatusChange activates on asset status transitions
	// (event.TypeAssetStatusChanged).
	TriggerStatusChange TriggerType = "status_change"
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
	// ConditionExpr is the optional expression evaluated against EventContext
	// variables. An empty expression matches all events of the trigger type.
	ConditionExpr string `gorm:"column:condition_expr;type:text" json:"condition_expr,omitempty"`
	// ExecutorType identifies which operator to invoke on match.
	// The default deployment supports "local" only.
	ExecutorType string `gorm:"column:executor_type;type:varchar(64);not null;default:'local'" json:"executor_type"`
	// ExecutorConfig is the JSON-encoded operator configuration. For the
	// local operator it carries {command, args, env}.
	ExecutorConfig string `gorm:"column:executor_config;type:text" json:"executor_config,omitempty"`
	// Cooldown is the minimum interval in seconds between consecutive
	// executions of this rule.
	Cooldown int `gorm:"column:cooldown;not null;default:300" json:"cooldown"`
	// CircuitBreakerThreshold is the consecutive failure count after which
	// the circuit breaker trips and pauses the rule.
	CircuitBreakerThreshold int `gorm:"column:circuit_breaker_threshold;not null;default:5" json:"circuit_breaker_threshold"`
	// Enabled indicates whether the rule participates in evaluation.
	Enabled bool `gorm:"column:enabled;not null;default:true" json:"enabled"`
	// Status is the operational status of the rule: active or paused.
	Status string `gorm:"column:status;type:varchar(16);not null;default:'active'" json:"status"`
	// LastRunAt records the last execution timestamp, used for cooldown
	// enforcement. Nullable.
	LastRunAt *time.Time `gorm:"column:last_run_at;index" json:"last_run_at,omitempty"`
	// Metadata is a JSON blob carrying runtime state, including the
	// consecutive_failures count used by the circuit breaker. See
	// ruleMetadata.
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

// EventContext defines the variables exposed to a Rule's condition
// expression. Fields are populated from the incoming event payload based on
// the trigger type. The expr-lang compiler accesses fields via the expr
// struct tag using snake_case identifiers (e.g. metric_value > threshold).
type EventContext struct {
	// Type is the trigger type identifier.
	Type string `expr:"type"`
	// AssetID is the asset associated with the event.
	AssetID int64 `expr:"asset_id"`
	// AssetKey is the tenant-unique asset key, populated for status_change
	// triggers whose payload carries one. Empty for metric and log
	// triggers, which only carry the numeric asset ID.
	AssetKey string `expr:"asset_key"`
	// TenantID is the owning tenant (0 in the runtime).
	TenantID int64 `expr:"tenant_id"`
	// MetricName is the metric that breached a threshold (metric trigger).
	MetricName string `expr:"metric_name"`
	// MetricValue is the observed metric value (metric trigger).
	MetricValue float64 `expr:"metric_value"`
	// Threshold is the configured threshold (metric trigger).
	Threshold float64 `expr:"threshold"`
	// Level is the log severity (log trigger).
	Level string `expr:"level"`
	// Keyword is the matched log keyword (log trigger).
	Keyword string `expr:"keyword"`
	// Content is the log line that matched (log trigger).
	Content string `expr:"content"`
	// SourceIP is the origin of the log (log trigger).
	SourceIP string `expr:"source_ip"`
	// PrevStatus is the asset status before the transition (status_change).
	PrevStatus string `expr:"prev_status"`
	// CurrStatus is the asset status after the transition (status_change).
	CurrStatus string `expr:"curr_status"`
}
