// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package status

import (
	"context"

	"github.com/tickraft/tickraft/pkg/telemetry"
)

// MonitorLister lists the monitor points consumed by the aggregation
// service. telemetry.MonitorStore satisfies it.
type MonitorLister interface {
	// ListActive returns the active probing points.
	ListActive(ctx context.Context) ([]telemetry.MonitorPoint, error)
	// ListPassive returns the passive receiving points.
	ListPassive(ctx context.Context) ([]telemetry.MonitorPoint, error)
}

// ProbeReader reads the latest probe record of a monitor point.
// telemetry.ProbeRecordStore satisfies it.
type ProbeReader interface {
	// LatestByPoint returns the most recent probe record for the point,
	// or an error wrapping errdefs.ErrNotFound when none exists.
	LatestByPoint(ctx context.Context, pointID int64) (*telemetry.ProbeRecord, error)
}

// SystemProbe is an infrastructure health check rendered as an extra
// component after the monitor-derived ones (for example task engine
// liveness). A nil error means operational; any error means degraded.
type SystemProbe struct {
	// Name is the component display name.
	Name string
	// Check runs the probe. It must be safe for concurrent use.
	Check func(ctx context.Context) error
}

// Service is the status page contract: configuration management plus the
// aggregated public view. The HTTP layer maps ErrDisabled to 404 and
// ErrValidation to 400.
type Service interface {
	// GetConfig returns the current status page configuration.
	GetConfig(ctx context.Context) (*Config, error)
	// UpdateConfig validates and persists the configuration and
	// invalidates the cached public view.
	UpdateConfig(ctx context.Context, cfg *Config) (*Config, error)
	// PublicView renders the aggregated page. It returns ErrDisabled
	// when the page is not enabled. Successful renders are cached for
	// the configured TTL.
	PublicView(ctx context.Context) (*PublicView, error)
}
