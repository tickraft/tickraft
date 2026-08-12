// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package quota defines the resource quota SPI (Service Provider Interface).
// It is the single entry point for all quota enforcement code.
//
// # Architecture
//
// The runtime registers a Provider at startup via [SetProvider]. All
// enforcement code calls [Ceiling] at check time — never caches the
// result — so provider changes take effect without restart.
//
// The default implementation lives in internal/quota and returns
// compiled-in fixed ceilings.
//
// # Three-Layer Quota Model
//
// Quota types are organized into three layers:
//
//   - LayerAsset — asset-level quotas (devices, hosts, team members, etc.)
//   - LayerConfig — configuration-level quotas (scheduled tasks, probers,
//     remediation rules, minimum intervals)
//   - LayerRuntime — runtime-level quotas (daily events, ingestion TPS,
//     concurrent tasks, API rate limits)
//
// # Hard constraints
//
//   - This package MUST NOT contain any concrete Provider implementation
//     or default ceiling values.
//   - The Provider interface is the only SPI surface.
package quota

// Type identifies a resource category subject to quota enforcement.
//
// Type values align with persisted quota records so callers can match
// records against the policy without translation.
type Type string

// Asset-layer quota types.
const (
	// TypeAsset is the total monitored asset quota type (unified across
	// hosts, devices, services, IoT, and cluster nodes).
	TypeAsset Type = "asset"
	// TypeDevice is the monitored device / monitoring-asset quota type.
	TypeDevice Type = "device"
	// TypeHost is the host asset quota type.
	TypeHost Type = "host"
	// TypeTeamMember is the team member quota type.
	TypeTeamMember Type = "team_member"
	// TypeCustomField is the custom field quota type.
	TypeCustomField Type = "custom_field"
)

// Config-layer quota types.
const (
	// TypeScheduledTask is the scheduled-task quota type (max task count).
	TypeScheduledTask Type = "scheduled_task"
	// TypeProber is the active prober quota type (ICMP/TCP/HTTP probes).
	TypeProber Type = "prober"
	// TypeRemediation is the remediation task quota type.
	TypeRemediation Type = "remediation"
	// TypeProbeInterval is the minimum allowed probe interval in
	// seconds.
	TypeProbeInterval Type = "probe_interval"
	// TypeScheduledTaskInterval is the minimum allowed scheduling interval
	// for interval-based scheduled tasks, expressed in seconds.
	TypeScheduledTaskInterval Type = "scheduled_task_interval"
)

// Runtime-layer quota types.
const (
	// TypeDailyEvents is the daily event ingestion quota type.
	TypeDailyEvents Type = "daily_events"
	// TypeIngestionMetricTPS is the per-second metric ingestion rate
	// limit. This ceiling value is consumed by a separate rate-limiter
	// component; the quota system itself does not perform TPS enforcement.
	TypeIngestionMetricTPS Type = "ingestion_metric_tps"
	// TypeIngestionEventTPS is the per-second event ingestion rate limit.
	// This ceiling value is consumed by a separate rate-limiter component.
	TypeIngestionEventTPS Type = "ingestion_event_tps"
	// TypeConcurrentTasks is the concurrent task execution quota type.
	TypeConcurrentTasks Type = "concurrent_tasks"
	// TypeAPIMinute is the per-minute API call quota type.
	TypeAPIMinute Type = "api_minute"
	// TypeAPIDaily is the per-day API call quota type.
	TypeAPIDaily Type = "api_daily"
	// TypeAPIConcurrent is the concurrent API request quota type.
	TypeAPIConcurrent Type = "api_concurrent"
)

// Backward-compatible type aliases. These allow existing callers to
// reference renamed types without code changes during the transition.
const (
	// TypeTask is retained for callers that treat generic "task" assets
	// as uncapped. Maps to TypeScheduledTask for ceiling lookups.
	TypeTask = TypeScheduledTask
	// TypeHTTPInterval is retained as an alias for TypeProbeInterval.
	TypeHTTPInterval = TypeProbeInterval
)
