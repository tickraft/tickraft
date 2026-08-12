// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package governance

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/tickraft/tickraft/pkg/prism/alert"
	"go.uber.org/zap"
)

// defaultDedupWindow is the suppression window used when no window is
// configured. It is short on purpose: Dedup only suppresses exact duplicate
// alerts that fire repeatedly within a brief burst, not sustained alerts.
const defaultDedupWindow = 60 * time.Second

// Dedup is a basic in-memory alert deduplication Guard. Two alerts
// produce the same fingerprint when their category, asset, metric name and
// level all match; the second alert within the configured window is
// suppressed (DecisionSuppress). The first alert always passes through
// (DecisionPass) and resets the window.
//
// State is kept purely in memory: it does not touch the database and is lost
// on restart. This is the baseline governance capability; the
// callers may inject the full governance chain (silence, aggregator,
// suppressor, storm) which supersedes Dedup.
//
// Dedup is safe for concurrent use.
type Dedup struct {
	window time.Duration
	logger *zap.Logger

	mu        sync.Mutex
	seen      map[string]time.Time
	lastSweep time.Time
}

// NewDedup creates a Dedup middleware with the given suppression window. A
// non-positive window defaults to 60s. logger must be non-nil; pass
// zap.NewNop() when no logging is desired.
func NewDedup(window time.Duration, logger *zap.Logger) *Dedup {
	if window <= 0 {
		window = defaultDedupWindow
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Dedup{
		window: window,
		logger: logger,
		seen:   make(map[string]time.Time),
	}
}

// Process implements Guard. It suppresses an alert when an identical
// alert (same fingerprint) was observed within the suppression window.
func (d *Dedup) Process(_ context.Context, evt *alert.Event) Decision {
	if evt == nil {
		return DecisionPass
	}
	fp := fingerprint(evt)

	now := time.Now()
	d.mu.Lock()
	defer d.mu.Unlock()

	d.maybeSweepLocked(now)

	if last, ok := d.seen[fp]; ok && now.Sub(last) < d.window {
		primary, _ := evt.PrimaryViolation()
		metricName := ""
		if primary.Metric != nil {
			metricName = primary.Metric.Name
		}
		d.logger.Debug("alert suppressed by dedup",
			zap.String("type", string(evt.Type)),
			zap.Int64("asset_id", evt.AssetID),
			zap.String("metric_name", metricName),
			zap.String("level", primary.Severity),
			zap.Duration("age", now.Sub(last)),
		)
		return DecisionSuppress
	}
	d.seen[fp] = now
	return DecisionPass
}

// maybeSweepLocked evicts expired fingerprints so the map does not grow
// without bound. Sweeping runs at most once per window to amortize cost.
func (d *Dedup) maybeSweepLocked(now time.Time) {
	if d.lastSweep.IsZero() || now.Sub(d.lastSweep) >= d.window {
		for fp, last := range d.seen {
			if now.Sub(last) >= d.window {
				delete(d.seen, fp)
			}
		}
		d.lastSweep = now
	}
}

// fingerprint returns a stable string key identifying the alert kind for
// deduplication. Two alerts with the same fingerprint are treated as
// duplicates within the suppression window.
// The fingerprint includes all violations (kind:severity pairs) sorted to
// ensure stable deduplication for multi-violation events.
func fingerprint(evt *alert.Event) string {
	var violationParts []string
	for _, v := range evt.Violations {
		part := v.Kind
		if v.Severity != "" {
			part += ":" + v.Severity
		}
		if v.Metric != nil && v.Metric.Name != "" {
			part += ":" + v.Metric.Name
		}
		violationParts = append(violationParts, part)
	}
	sort.Strings(violationParts)
	return fmt.Sprintf("%s|%d|%s", evt.Type, evt.AssetID, strings.Join(violationParts, ","))
}
