// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package quota

import (
	"context"
	"sync"
)

// Layer classifies a quota type into one of the three architectural layers.
type Layer int

// Layer values.
const (
	// LayerUnknown is the zero value for types without a layer mapping.
	LayerUnknown Layer = iota
	// LayerAsset covers asset-level quotas: device count, host count,
	// team member count, custom field count, etc.
	LayerAsset
	// LayerConfig covers configuration-level quotas: scheduled task count,
	// prober count, remediation rule count, minimum intervals.
	LayerConfig
	// LayerRuntime covers runtime-level quotas: daily events, ingestion
	// TPS, concurrent tasks, API rate limits.
	LayerRuntime
)

// layerOf maps each Type to its Layer.
var layerOf = map[Type]Layer{
	TypeAsset:                 LayerAsset,
	TypeDevice:                LayerAsset,
	TypeHost:                  LayerAsset,
	TypeTeamMember:            LayerAsset,
	TypeCustomField:           LayerAsset,
	TypeScheduledTask:         LayerConfig,
	TypeProber:                LayerConfig,
	TypeRemediation:           LayerConfig,
	TypeProbeInterval:         LayerConfig,
	TypeScheduledTaskInterval: LayerConfig,
	TypeDailyEvents:           LayerRuntime,
	TypeIngestionMetricTPS:    LayerRuntime,
	TypeIngestionEventTPS:     LayerRuntime,
	TypeConcurrentTasks:       LayerRuntime,
	TypeAPIMinute:             LayerRuntime,
	TypeAPIDaily:              LayerRuntime,
	TypeAPIConcurrent:         LayerRuntime,
}

// LayerOf returns the architectural layer for the given Type.
// Returns LayerUnknown for unrecognized types.
func LayerOf(t Type) Layer {
	if l, ok := layerOf[t]; ok {
		return l
	}
	return LayerUnknown
}

// scalableTypes marks quota types that support addon-based scaling.
var scalableTypes = map[Type]bool{
	TypeAsset:         true,
	TypeDevice:        true,
	TypeHost:          true,
	TypeTeamMember:    true,
	TypeScheduledTask: true,
	TypeProber:        true,
	TypeRemediation:   true,
	TypeDailyEvents:   true,
}

// IsScalable reports whether the given Type supports addon-based scaling.
func IsScalable(t Type) bool {
	return scalableTypes[t]
}

// Spec is the complete quota definition for a single Type. It bundles the
// ceiling value, layer classification, and scalability flag so callers can
// avoid multiple round-trip queries.
type Spec struct {
	// Ceiling is the current quota upper bound. A value of 0 means the
	// type is unlimited (for count-based quotas) or has no minimum (for
	// interval-based quotas).
	Ceiling int
	// Layer is the architectural layer this type belongs to.
	Layer Layer
	// Scalable reports whether the type supports addon-based scaling
	// (additional capacity on top of the base ceiling).
	Scalable bool
}

// Provider returns the current quota ceiling for a given resource [Type].
//
// A Provider may return fixed values (as the default implementation does)
// or dynamic values read from an external source at call time. Callers must
// never cache the result — each [Ceiling] call should query the active
// Provider so changes take effect immediately.
type Provider interface {
	// Ceiling returns the current quota ceiling for the given type.
	// A return value of 0 means "no quota" — the enforcement code
	// should treat 0 as "unlimited" for count-based quotas and as
	// "no minimum" for interval-based quotas.
	Ceiling(t Type) int
}

// ProviderWithSpec extends [Provider] with a method to retrieve the full
// [Spec] for a type in a single call. Providers should implement this
// interface to allow callers to batch metadata queries.
type ProviderWithSpec interface {
	Provider
	// Spec returns the complete quota specification for the given type.
	Spec(t Type) Spec
}

// UsageProvider extends [Provider] with usage tracking capabilities.
// It allows querying current usage, calculating usage rates, and checking
// if quota limits are being approached for proactive alerting.
type UsageProvider interface {
	Provider
	// CurrentUsage returns the current usage count for the given type.
	// Returns error if usage tracking is not available.
	CurrentUsage(ctx context.Context, t Type) (used int, err error)

	// UsageRate calculates the usage rate as a float between 0.0 and 1.0.
	// A rate of 1.0 means the quota is at its ceiling.
	// Returns error if usage tracking is not available.
	UsageRate(ctx context.Context, t Type) (rate float64, err error)

	// IsNearLimit checks if the quota is approaching its limit.
	// The threshold parameter defines what "near" means (e.g., 0.9 for 90%).
	// If threshold <= 0, a default of 0.9 is used.
	// Returns (near, used, ceiling, error).
	IsNearLimit(ctx context.Context, t Type, threshold float64) (near bool, used int, ceiling int, err error)
}

// UsageSnapshot captures a point-in-time view of quota usage.
type UsageSnapshot struct {
	Type     Type   `json:"type"`
	Used     int    `json:"used"`
	Ceiling  int    `json:"ceiling"`
	Rate     float64 `json:"rate"`
	NearLimit bool   `json:"near_limit"`
}

// zeroProvider is a no-op Provider that returns 0 for every type,
// indicating unlimited / no restriction. It is the fallback when no
// provider has been registered.
type zeroProvider struct{}

func (zeroProvider) Ceiling(Type) int { return 0 }

var (
	providerMu sync.RWMutex
	active     Provider = zeroProvider{}
)

// SetProvider replaces the active quota Provider. The runtime calls this
// during initialization to inject the default Provider.
//
// Passing nil reverts to a zero-value Provider that returns 0 (unlimited)
// for every type.
func SetProvider(p Provider) {
	providerMu.Lock()
	defer providerMu.Unlock()
	if p == nil {
		active = zeroProvider{}
	} else {
		active = p
	}
}

// Ceiling returns the current quota ceiling for the given type from the
// active Provider. It is safe for concurrent use.
//
// Enforcement code must call this at check time (not cache the result)
// so provider changes take effect immediately.
func Ceiling(t Type) int {
	providerMu.RLock()
	p := active
	providerMu.RUnlock()
	return p.Ceiling(t)
}

// GetSpec returns the complete quota specification for the given type from
// the active Provider. If the active Provider does not implement
// ProviderWithSpec, a Spec is synthesized from the Ceiling value and the
// static type metadata.
func GetSpec(t Type) Spec {
	providerMu.RLock()
	p := active
	providerMu.RUnlock()
	if ps, ok := p.(ProviderWithSpec); ok {
		return ps.Spec(t)
	}
	return Spec{
		Ceiling:  p.Ceiling(t),
		Layer:    LayerOf(t),
		Scalable: IsScalable(t),
	}
}

// ActiveProvider returns the currently registered Provider. It is intended
// for testing and introspection.
func ActiveProvider() Provider {
	providerMu.RLock()
	defer providerMu.RUnlock()
	return active
}
