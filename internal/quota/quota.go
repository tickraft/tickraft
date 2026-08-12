// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package quota

import (
	"github.com/tickraft/tickraft/pkg/quota"
)

// CE default ceiling constants. These values are the stable public contract
// of the runtime and align with the unified quota policy.
const (
	// CeilingDevice is the maximum number of monitored device assets.
	CeilingDevice = 20
	// CeilingHost is the maximum number of host assets. Set to 0 (host
	// monitoring is not enabled in this edition).
	CeilingHost = 0
	// CeilingProber is the maximum number of active probers.
	CeilingProber = 20
	// CeilingScheduledTask is the maximum number of scheduled tasks.
	CeilingScheduledTask = 20
	// CeilingRemediation is the maximum number of remediation rules.
	CeilingRemediation = 5
	// CeilingProbeIntervalSeconds is the minimum allowed probe
	// interval in seconds.
	CeilingProbeIntervalSeconds = 60
	// CeilingScheduledTaskIntervalSeconds is the minimum allowed
	// scheduling interval for interval-based scheduled tasks in seconds.
	CeilingScheduledTaskIntervalSeconds = 60
	// CeilingDailyEvents is the maximum number of events per day.
	CeilingDailyEvents = 100000
)

// ceilings is the fixed CE ceiling table.
var ceilings = map[quota.Type]int{
	quota.TypeDevice:                CeilingDevice,
	quota.TypeHost:                  CeilingHost,
	quota.TypeProber:                CeilingProber,
	quota.TypeScheduledTask:         CeilingScheduledTask,
	quota.TypeRemediation:           CeilingRemediation,
	quota.TypeProbeInterval:         CeilingProbeIntervalSeconds,
	quota.TypeScheduledTaskInterval: CeilingScheduledTaskIntervalSeconds,
	quota.TypeDailyEvents:           CeilingDailyEvents,
}

// DefaultProvider is the Community Edition default quota Provider. It
// returns the compiled-in fixed ceilings and ignores all runtime
// configuration. It implements pkgquota.ProviderWithSpec.
type DefaultProvider struct{}

// Ceiling returns the fixed CE default for the given type, or 0 when the
// type is not recognized (indicating unlimited / not configured).
func (DefaultProvider) Ceiling(t quota.Type) int { return ceilings[t] }

// Spec returns the complete quota specification for the given type.
func (DefaultProvider) Spec(t quota.Type) quota.Spec {
	return quota.Spec{
		Ceiling:  ceilings[t],
		Layer:    quota.LayerOf(t),
		Scalable: quota.IsScalable(t),
	}
}

// Register injects the DefaultProvider into pkg/quota as the active
// Provider. It must be called once during application initialization,
// before any quota enforcement code runs.
func Register() {
	quota.SetProvider(DefaultProvider{})
}
