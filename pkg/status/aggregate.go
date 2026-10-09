// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package status

import (
	"context"
	"errors"
	"maps"
	"slices"
	"time"

	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/errdefs"
	"github.com/tickraft/tickraft/pkg/telemetry"
	"github.com/tickraft/tickraft/pkg/types"
)

// Sources bundles the data sources the aggregation reads. Logger is
// optional; probe read failures are warned when present.
type Sources struct {
	// Monitors lists the monitor topology. Nil renders monitor-derived
	// components as unknown.
	Monitors MonitorLister
	// Probes reads the latest probe record per point. Nil has the same
	// unknown-rendering effect.
	Probes ProbeReader
	// Logger receives probe read warnings; nil drops them.
	Logger *zap.Logger
}

// RenderComponents aggregates the given components against the data
// sources and returns the rendered states in configuration order. When
// comps is empty, one component per enabled monitor point is derived;
// systemProbes render after the configured components. Richer status
// surfaces (multiple pages, uptime history) build on RenderComponents
// so the health vocabulary and aggregation semantics stay identical to
// the built-in page.
func RenderComponents(
	ctx context.Context,
	src Sources,
	systemProbes []SystemProbe,
	comps []Component,
) ([]ComponentStatus, error) {
	byID, err := loadMonitorMap(ctx, src.Monitors)
	if err != nil {
		return nil, err
	}
	if len(comps) == 0 {
		comps = autoComponents(byID)
	}
	out := make([]ComponentStatus, 0, len(comps)+len(systemProbes))
	for _, c := range comps {
		out = append(out, renderComponent(ctx, src, c, byID))
	}
	for _, p := range systemProbes {
		out = append(out, systemProbeStatus(ctx, p))
	}
	return out, nil
}

// Overall returns the worst non-unknown status across the rendered
// components. Unknown components never drag the overall state down.
func Overall(comps []ComponentStatus) string {
	worst := StatusUnknown
	rank := map[string]int{
		StatusUnknown:       0,
		StatusOperational:   1,
		StatusDegraded:      2,
		StatusPartialOutage: 3,
		StatusMajorOutage:   4,
	}
	for _, c := range comps {
		if _, ok := rank[c.Status]; !ok {
			continue
		}
		if c.Status == StatusUnknown {
			continue
		}
		if worst == StatusUnknown || rank[c.Status] > rank[worst] {
			worst = c.Status
		}
	}
	return worst
}

// loadMonitorMap lists the monitor topology keyed by ID. Disabled points
// are excluded: they neither render nor influence component state.
func loadMonitorMap(ctx context.Context, monitors MonitorLister) (map[int64]telemetry.MonitorPoint, error) {
	byID := make(map[int64]telemetry.MonitorPoint)
	if monitors == nil {
		return byID, nil
	}
	var (
		active  []telemetry.MonitorPoint
		passive []telemetry.MonitorPoint
		errA    error
	)
	if active, errA = monitors.ListActive(ctx); errA != nil {
		return nil, errA
	}
	if passive, errA = monitors.ListPassive(ctx); errA != nil {
		return nil, errA
	}
	for i := range active {
		if active[i].Enabled {
			byID[active[i].ID] = active[i]
		}
	}
	for i := range passive {
		if passive[i].Enabled {
			byID[passive[i].ID] = passive[i]
		}
	}
	return byID, nil
}

// autoComponents derives one component per enabled monitor point when the
// configuration has no component map, so the page is useful without any
// mapping work.
func autoComponents(byID map[int64]telemetry.MonitorPoint) []Component {
	comps := make([]Component, 0, len(byID))
	for _, id := range slices.Sorted(maps.Keys(byID)) {
		p := byID[id]
		comps = append(comps, Component{
			Name:     p.Name,
			PointIDs: []int64{p.ID},
		})
	}
	return comps
}

// renderComponent derives the rendered state of one configured component.
func renderComponent(
	ctx context.Context,
	src Sources,
	c Component,
	byID map[int64]telemetry.MonitorPoint,
) ComponentStatus {
	cs := ComponentStatus{Name: c.Name, Description: c.Description, Status: StatusUnknown}
	states := make([]string, 0, len(c.PointIDs))
	var lastChecked time.Time
	for _, id := range c.PointIDs {
		p, ok := byID[id]
		if !ok {
			states = append(states, StatusUnknown)
			continue
		}
		cs.MonitorCount++
		var probe *telemetry.ProbeRecord
		if src.Probes != nil {
			// A missing record is not an error here: inactive or
			// never-run points simply render as unknown.
			if pr, err := src.Probes.LatestByPoint(ctx, id); err == nil {
				probe = pr
			} else if !errors.Is(err, errdefs.ErrNotFound) && src.Logger != nil {
				src.Logger.Warn("status: read latest probe", zap.Int64("point_id", id), zap.Error(err))
			}
		}
		states = append(states, pointHealth(p, probe))
		if probe != nil && probe.StartedAt.After(lastChecked) {
			lastChecked = probe.StartedAt
		}
	}
	if len(states) > 0 {
		cs.Status = aggregateHealth(states)
	}
	cs.LastChecked = lastChecked
	return cs
}

// systemProbeStatus renders an injected infrastructure probe.
func systemProbeStatus(ctx context.Context, p SystemProbe) ComponentStatus {
	if p.Check == nil {
		return ComponentStatus{Name: p.Name, Status: StatusUnknown}
	}
	st := StatusOperational
	if err := p.Check(ctx); err != nil {
		st = StatusDegraded
	}
	return ComponentStatus{Name: p.Name, Status: st, LastChecked: time.Now()}
}

// pointHealth maps one monitor point plus its latest probe record onto
// the status vocabulary. Active points derive from the probe result;
// passive points have no probe rows and derive from the runtime point
// status alone.
func pointHealth(p telemetry.MonitorPoint, probe *telemetry.ProbeRecord) string {
	if probe == nil {
		if p.Status == telemetry.MonitorStatusError {
			return StatusDegraded
		}
		return StatusUnknown
	}
	switch probe.Status {
	case types.AssetStatusNormal:
		return StatusOperational
	case types.AssetStatusAbnormal:
		return StatusDegraded
	case types.AssetStatusOffline:
		return StatusMajorOutage
	default:
		return StatusUnknown
	}
}

// aggregateHealth folds point states into a component status: all healthy
// is operational, all-outage is a major outage, all-degraded stays
// degraded (the service is impaired but up), and any other mix is a
// partial outage. No data at all is unknown.
func aggregateHealth(states []string) string {
	bad, major, seen := 0, 0, 0
	for _, st := range states {
		switch st {
		case StatusOperational:
			seen++
		case StatusMajorOutage:
			bad++
			major++
			seen++
		case StatusDegraded, StatusPartialOutage:
			bad++
			seen++
		case StatusUnknown:
		}
	}
	switch {
	case seen == 0:
		return StatusUnknown
	case bad == 0:
		return StatusOperational
	case major == seen:
		return StatusMajorOutage
	case bad == seen && major == 0:
		return StatusDegraded
	default:
		return StatusPartialOutage
	}
}
