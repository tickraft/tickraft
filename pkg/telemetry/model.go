// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package telemetry

import (
	"time"

	"github.com/tickraft/tickraft/pkg/types"
)

// CollectionConfig is the GORM model for the sys_collect_config table.
// It stores the observation configuration for each registered asset.
type CollectionConfig struct {
	ID int64 `gorm:"primaryKey;autoIncrement" json:"id"`
	// TenantID is the tenant identifier for multi-tenancy isolation.
	// The runtime is single-tenant: this field is always 0.
	// The runtime injects the actual tenant ID via the store layer.
	TenantID      int64     `gorm:"index;not null" json:"tenant_id"`
	AssetID       int64     `gorm:"uniqueIndex;not null" json:"asset_id"`
	AssetType     string    `gorm:"size:32;not null" json:"asset_type"`
	CollectType   string    `gorm:"size:32;not null" json:"collect_type"`
	CollectConfig string    `gorm:"type:text" json:"collect_config"`
	Timeout       int       `gorm:"not null" json:"timeout"`
	ProbeInterval int       `gorm:"not null;default:0" json:"probe_interval"`
	Enable        bool      `gorm:"not null;default:true" json:"enable"`
	CreatedAt     time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt     time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

// TableName returns the database table name for CollectionConfig.
func (CollectionConfig) TableName() string {
	return "sys_collect_config"
}

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
	TenantID int64 `gorm:"index;not null" json:"tenant_id"`
	// Name is the human-readable display name of the monitoring point.
	Name string `gorm:"size:255;not null" json:"name"`
	// Description is an optional human-readable description of the monitoring
	// point.
	Description string `gorm:"size:1024" json:"description,omitempty"`
	// AssetType is the category of the monitored asset (e.g. host, service,
	// website, device).
	AssetType string `gorm:"size:32" json:"asset_type,omitempty"`
	// AssetID optionally links this monitoring point to an asset row. When
	// non-zero, metric history and log entries for the linked asset are
	// surfaced via the history/logs API endpoints.
	AssetID int64 `gorm:"index" json:"asset_id,omitempty"`
	// Mode distinguishes active probing (ModeActive) from passive receiving
	// (ModePassive). See the Mode type in point.go.
	Mode Mode `gorm:"size:16;not null;index" json:"mode"`
	// Type identifies the prober or listener type. For ModeActive this is
	// the executor type (icmp, tcp, http, udp). For ModePassive this is
	// the listener type (webhook).
	Type string `gorm:"size:32;not null" json:"type"`
	// Status is the derived runtime status of the monitoring point
	// (active, inactive, error). See MonitorStatus constants in point.go.
	Status string `gorm:"size:32;not null;default:inactive" json:"status"`
	// Schedule is the probe schedule expression: a Go duration string
	// (e.g. "60s") for interval-based probing or a cron expression. Empty
	// means use Interval.
	Schedule string `gorm:"size:64" json:"schedule,omitempty"`
	// Interval is the probe interval in seconds for active points.
	// Ignored for passive points (interval=0).
	Interval int `gorm:"not null;default:60" json:"interval"`
	// Timeout is the probe timeout in seconds for active points, or the
	// offline detection threshold for passive points.
	Timeout int `gorm:"not null;default:10" json:"timeout"`
	// Enabled controls whether the monitoring point is active. When false,
	// the prober service skips scheduling and the listener rejects data.
	Enabled bool `gorm:"not null;default:true" json:"enabled"`
	// Config is the JSON-encoded type-specific configuration. For an ICMP
	// prober this might contain {"target":"192.0.2.1"}; for a webhook
	// listener it might contain {"path":"/api/v1/telemetry","auth":"asset-key"}.
	Config string `gorm:"type:text" json:"config,omitempty"`
	// CreatedAt is the point creation timestamp.
	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
	// UpdatedAt is the last update timestamp.
	UpdatedAt time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

// TableName returns the database table name for MonitorPoint.
func (MonitorPoint) TableName() string { return "monitor_points" }

// IsActive reports whether the monitoring point is in active probing mode.
func (p MonitorPoint) IsActive() bool { return p.Mode == ModeActive }

// IsPassive reports whether the monitoring point is in passive receiving mode.
func (p MonitorPoint) IsPassive() bool { return p.Mode == ModePassive }
