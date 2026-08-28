// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package telemetry

import (
	"time"

	"github.com/bytedance/sonic"

	// Registers the tolerantjson serializer that MonitorPoint.Config
	// depends on. The blank import guarantees registration before any
	// schema parse.
	_ "github.com/tickraft/tickraft/pkg/db"
	"github.com/tickraft/tickraft/pkg/types"
)

// StatusHistory is the GORM model for the sys_collect_status_history table.
// It records every status transition for audit and analysis.
type StatusHistory struct {
	ID int64 `gorm:"primaryKey;autoIncrement" json:"id"`
	// TenantID is the tenant identifier for multi-tenancy isolation.
	// The runtime is single-tenant: this field is always 0.
	// The runtime injects the actual tenant ID via the store layer.
	TenantID   int64             `gorm:"index;not null" json:"tenant_id"`
	AssetID    int64             `gorm:"index;not null" json:"asset_id"`
	AssetType  string            `gorm:"size:32;not null" json:"asset_type"`
	PrevStatus types.AssetStatus `gorm:"size:32;not null" json:"prev_status"`
	CurrStatus types.AssetStatus `gorm:"size:32;not null" json:"curr_status"`
	Reason     string            `gorm:"size:255" json:"reason"`
	CreatedAt  time.Time         `gorm:"autoCreateTime;index" json:"created_at"`
}

// TableName returns the database table name for StatusHistory.
func (StatusHistory) TableName() string {
	return "sys_collect_status_history"
}

// CollectMetric is the GORM model for the sys_collect_metric table.
// It stores metric data points collected by listeners and aggregated by the
// aggregation layer.
type CollectMetric struct {
	// ID is the unique identifier of the metric record.
	ID int64 `gorm:"primaryKey;autoIncrement" json:"id"`
	// TenantID is the tenant to which the metric belongs.
	// The runtime is single-tenant: this field is always 0.
	// The runtime injects the actual tenant ID via the store layer.
	TenantID int64 `gorm:"index;not null" json:"tenant_id"`
	// AssetID is the asset that the metric was collected from.
	AssetID int64 `gorm:"index;not null" json:"asset_id"`
	// MetricName is the name of the metric (e.g. "cpu_usage", "memory_percent").
	MetricName string `gorm:"size:64;not null" json:"metric_name"`
	// MetricValue is the numeric value of the metric data point.
	MetricValue float64 `gorm:"not null" json:"metric_value"`
	// Timestamp is the time at which the metric was collected.
	Timestamp time.Time `gorm:"index;not null" json:"timestamp"`
}

// TableName returns the database table name for CollectMetric.
func (CollectMetric) TableName() string { return "sys_collect_metric" }

// CollectLog is the GORM model for the sys_collect_log table.
// It stores log entries received from syslog and other log sources.
type CollectLog struct {
	// ID is the unique identifier of the log record.
	ID int64 `gorm:"primaryKey;autoIncrement" json:"id"`
	// TenantID is the tenant to which the log belongs.
	// The runtime is single-tenant: this field is always 0.
	// The runtime injects the actual tenant ID via the store layer.
	TenantID int64 `gorm:"index;not null" json:"tenant_id"`
	// AssetID is the asset that the log entry is associated with.
	AssetID int64 `gorm:"index;not null" json:"asset_id"`
	// Level is the severity level of the log entry (e.g. "ERROR", "WARN", "INFO").
	Level string `gorm:"size:16;not null" json:"level"`
	// Content is the raw text content of the log entry.
	Content string `gorm:"type:text;not null" json:"content"`
	// SourceIP is the IP address from which the log was received.
	SourceIP string `gorm:"size:64" json:"source_ip,omitempty"`
	// Timestamp is the time at which the log entry was generated.
	Timestamp time.Time `gorm:"index;not null" json:"timestamp"`
}

// TableName returns the database table name for CollectLog.
func (CollectLog) TableName() string { return "sys_collect_log" }

// MonitorPoint is the GORM model for the monitor_points table. It unifies
// active probing (prober) and passive receiving (listener) configurations
// into a single persisted entity distinguished by the Mode field.
//
//   - Mode=ModeActive: the point is periodically probed by the ProberService
//     using the executor identified by Type (e.g. icmp, tcp, http).
//   - Mode=ModePassive: the point passively receives data via the listener
//     identified by Type (e.g. webhook).
//
// The Config field holds the JSON-encoded type-specific configuration
// (target address, port, HTTP path, auth settings, etc.). This replaces the
// former split between prober-specific and listener-specific fields with a
// single flexible payload.
type MonitorPoint struct {
	// ID is the unique identifier of the monitoring point.
	ID int64 `gorm:"primaryKey;autoIncrement" json:"id"`
	// TenantID is the tenant identifier for multi-tenancy isolation.
	// The runtime is single-tenant: this field is always 0.
	// The runtime injects the actual tenant ID via the store layer.
	TenantID int64 `gorm:"index;not null" json:"-"`
	// Name is the human-readable display name of the monitoring point.
	Name string `gorm:"size:255;not null" json:"name"`
	// Description is an optional human-readable description of the
	// monitoring point.
	Description string `gorm:"size:1024" json:"description,omitempty"`
	// AssetType is the category of the monitored asset (e.g. host, service,
	// website, device).
	AssetType string `gorm:"size:32" json:"asset_type,omitempty"`
	// AssetID optionally links this monitoring point to an asset row. When
	// non-zero, metric history and log entries for the linked asset are
	// surfaced via the history/logs API endpoints.
	AssetID int64 `gorm:"index" json:"asset_id,omitempty"`
	// Mode distinguishes active probing (ModeActive) from passive receiving
	// (ModePassive).
	Mode Mode `gorm:"size:16;not null;index" json:"mode"`
	// Type identifies the prober or listener type. For ModeActive this is
	// the executor type (icmp, tcp, http, udp). For ModePassive this is
	// the listener type (webhook).
	Type string `gorm:"size:32;not null" json:"type"`
	// Status is the derived runtime status of the monitoring point
	// (active, inactive, error), maintained by the runtime: for active
	// points ProbeRecordStore refreshes it on every probe result. See the
	// MonitorStatus constants below. Read-only on the wire; not
	// API-editable.
	Status string `gorm:"size:32;not null;default:inactive" json:"status,omitempty"`
	// Schedule is the probe schedule expression: a Go duration string
	// (e.g. "60s") for interval-based probing or a cron expression. Empty
	// means use Interval.
	Schedule string `gorm:"size:64" json:"schedule,omitempty"`
	// Interval is the probe interval in seconds for active points.
	// Runtime-managed fallback when Schedule is empty; not API-editable.
	Interval int `gorm:"not null;default:60" json:"-"`
	// Timeout is the probe timeout in seconds for active points, or the
	// offline detection threshold for passive points. Runtime-managed;
	// not API-editable.
	Timeout int `gorm:"not null;default:10" json:"-"`
	// Enabled controls whether the monitoring point is active. When false,
	// the prober service skips scheduling and the listener rejects data.
	// No column default: GORM substitutes the default for zero-valued
	// fields on insert, which would store disabled (false) points as
	// enabled.
	Enabled bool `gorm:"not null" json:"enabled"`
	// Config is the type-specific configuration, persisted as JSON in a
	// text column via the tolerantjson serializer. For an ICMP prober it
	// might contain {"target":"192.0.2.1"}; for a webhook listener
	// {"path":"/api/v1/telemetry","auth":"asset-key"}. Reads never fail:
	// an empty, "null", or malformed column decodes to nil.
	Config map[string]any `gorm:"column:config;type:text;serializer:tolerantjson" json:"config,omitempty"`
	// CreatedAt is the point creation timestamp.
	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
	// UpdatedAt is the last update timestamp.
	UpdatedAt time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

// TableName returns the database table name for MonitorPoint.
func (MonitorPoint) TableName() string { return "monitor_points" }

// ConfigJSON returns the point's config marshalled to the JSON string
// form consumed by the executor SPI (task.Task.Config, executor config
// payloads). A nil or empty map yields the empty string.
func (p *MonitorPoint) ConfigJSON() string {
	if len(p.Config) == 0 {
		return ""
	}
	raw, err := sonic.Marshal(p.Config)
	if err != nil {
		return ""
	}
	return string(raw)
}

// IsActive reports whether the monitoring point is in active probing mode.
func (p MonitorPoint) IsActive() bool { return p.Mode == ModeActive }

// IsPassive reports whether the monitoring point is in passive receiving mode.
func (p MonitorPoint) IsPassive() bool { return p.Mode == ModePassive }

// Mode defines the monitoring point mode. A monitoring point is either
// actively probed (ModeActive) or passively receiving data (ModePassive).
// The unified MonitorPoint model uses this field to distinguish the two
// operational modes within a single table.
type Mode string

const (
	// ModeActive means the point is actively probed by the ProberService.
	// The Type field identifies the prober executor (icmp, tcp, http, udp).
	ModeActive Mode = "active"
	// ModePassive means the point passively receives data via a listener.
	// The Type field identifies the listener type (webhook).
	ModePassive Mode = "passive"
)

// Monitoring point status constants. These are stored in the MonitorPoint
// Status column and reported by the monitor status API endpoint.
const (
	// MonitorStatusActive indicates the monitoring point is running and
	// producing healthy results.
	MonitorStatusActive = "active"
	// MonitorStatusInactive indicates the monitoring point is disabled or
	// has not yet been started.
	MonitorStatusInactive = "inactive"
	// MonitorStatusError indicates the monitoring point encountered an
	// error during its last probe or reception cycle.
	MonitorStatusError = "error"
	// MonitorStatusPending indicates the monitoring point is registered
	// but awaiting its first probe or data reception.
	MonitorStatusPending = "pending"
)

// ValidMode reports whether the given Mode is a recognized value.
func ValidMode(m Mode) bool {
	return m == ModeActive || m == ModePassive
}

// Kind enumerates the data categories accepted by the unified telemetry
// report endpoint. It determines the internal processing pipeline and
// the payload size limit applied by the server.
type Kind string

const (
	// KindHeartbeat carries asset heartbeat / liveness status.
	KindHeartbeat Kind = Kind(types.EventKindHeartbeat)
	// KindMetrics carries asset metric samples.
	KindMetrics Kind = "metrics"
	// KindLogs carries asset log entries.
	KindLogs Kind = "logs"
	// KindTaskStatus carries a task's execution status report
	// (running/completed/failed/timeout) submitted by a remote reporter.
	KindTaskStatus Kind = "task_status"
)

// Template is the GORM model for the sys_telemetry_template table.
// It stores reusable telemetry monitoring point templates. Built-in templates
// are seeded at startup and marked IsBuiltin=true; custom templates are
// created by users through the API.
type Template struct {
	// ID is the unique identifier of the template.
	ID int64 `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	// Name is the template name, unique across all templates.
	Name string `gorm:"column:name;type:varchar(128);not null;uniqueIndex" json:"name"`
	// Description is the human-readable description of the template.
	Description string `gorm:"column:description;type:varchar(512)" json:"description"`
	// Category classifies the template (e.g. "network", "web", "database").
	Category string `gorm:"column:category;type:varchar(64);index" json:"category"`
	// ExecutorType is the probe/executor type: icmp, tcp, http, dns, etc.
	ExecutorType string `gorm:"column:executor_type;type:varchar(32);not null" json:"executor_type"`
	// Config is the JSON-encoded monitoring point configuration.
	Config string `gorm:"column:config;type:text;not null" json:"config"`
	// IsBuiltin marks system-seeded templates. Built-in templates are
	// read-only and cannot be deleted.
	IsBuiltin bool `gorm:"column:is_builtin;default:false" json:"is_builtin"`
	// CreatedAt is the template creation timestamp.
	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	// UpdatedAt is the last update timestamp.
	UpdatedAt time.Time `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
}

// TableName returns the database table name for Template.
func (Template) TableName() string { return "sys_telemetry_template" }

// ProbeRecord is the GORM model for the sys_probe_record table. It is the
// telemetry-domain home for active probe results: one row per probe
// execution, keyed by the monitor point that scheduled it.
//
// It holds a structured record, not a log entry: a probe is an operation the
// runtime executes, and the row preserves its full result envelope (status,
// status code, duration, output, error) so status filters, latency trends and
// success-rate statistics stay SQL-queryable. Passive collection data lives
// separately in sys_collect_log / sys_collect_metric; the active/passive
// distinction is structural — which data source a reader queries — and no
// row-level marker is needed.
type ProbeRecord struct {
	// ID is the unique identifier of the probe record.
	ID int64 `gorm:"primaryKey;autoIncrement" json:"id"`
	// TenantID is the tenant identifier for multi-tenancy isolation.
	// The runtime is single-tenant: this field is always 0.
	TenantID int64 `gorm:"index;not null" json:"-"`
	// PointID is the monitor point (monitor_points.id) that scheduled the
	// probe. It is recovered from the synthetic prober task ID.
	PointID int64 `gorm:"column:point_id;not null;index" json:"point_id"`
	// AssetID is the target asset, denormalized for asset-level queries.
	AssetID int64 `gorm:"index;not null" json:"asset_id"`
	// ExecutorType is the probe executor type (e.g. icmp, tcp, http, udp).
	ExecutorType string `gorm:"size:32;not null" json:"executor_type"`
	// Status is the probe result in the asset status vocabulary
	// (normal, abnormal, offline, unknown).
	Status types.AssetStatus `gorm:"size:32;not null;index" json:"status"`
	// StatusCode is the numeric status code returned by the executor.
	StatusCode int `gorm:"status_code" json:"status_code,omitempty"`
	// Output is the raw probe output.
	Output string `gorm:"type:text" json:"output,omitempty"`
	// Error is the error message when the probe failed.
	Error string `gorm:"column:error_msg;type:text" json:"error,omitempty"`
	// Duration is the probe duration in milliseconds.
	Duration int64 `gorm:"duration" json:"duration,omitempty"`
	// RetryCount is the number of retries attempted.
	RetryCount int `gorm:"not null;default:0" json:"retry_count,omitempty"`
	// RunID links to the task run for idempotency tracking.
	RunID string `gorm:"size:64;index" json:"run_id,omitempty"`
	// TriggerType records how the probe was triggered.
	TriggerType string `gorm:"size:16" json:"-"`
	// StartedAt is when the probe began.
	StartedAt time.Time `gorm:"column:started_at;not null;index" json:"started_at"`
	// CreatedAt is the row creation timestamp.
	CreatedAt time.Time `gorm:"autoCreateTime" json:"-"`
}

// TableName returns the database table name for ProbeRecord.
func (ProbeRecord) TableName() string { return "sys_probe_record" }
