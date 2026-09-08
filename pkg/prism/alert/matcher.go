// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package alert

import (
	"context"
	"strconv"
	"time"

	"github.com/bytedance/sonic"

	"github.com/tickraft/tickraft/pkg/asset"
	"github.com/tickraft/tickraft/pkg/cache"
	"github.com/tickraft/tickraft/pkg/types"
)

// Matcher hot-path asset cache: every Match call resolves the asset to
// enrich the evaluation env, so lookups are LRU-cached like the telemetry
// validator's. There is no explicit invalidation hook in the prism engine;
// the TTL bounds how long a renamed or retagged asset keeps evaluating
// under its old attributes.
const (
	matcherCacheSize = 1024
	matcherCacheTTL  = 5 * time.Minute
)

// MatchedRule identifies a rule that matched an evaluated event. The
// dispatcher uses it to attribute persisted records (and the payload
// violations they are built from) to the rule that gated the alert.
type MatchedRule struct {
	// ID is the matched rule's sys_prism_alert_rule id.
	ID int64
	// Name is the matched rule's display name.
	Name string
}

// MatchResult is the outcome of evaluating an alert event against the
// configured rules. Forward is the dispatch decision; Violations is the
// structured detail of the same evaluation.
type MatchResult struct {
	// Forward reports whether the event should be dispatched to
	// channels.
	Forward bool
	// Matched identifies every rule that matched the event. It is empty
	// when no rule matched and under the default-allow contract (no
	// rules configured), where no rule identity exists to report.
	Matched []MatchedRule
	// Violations carries one Violation per matched metric-fact
	// comparison across all matching rules, so a compound rule such
	// as `metrics["cpu"] > 90 && metrics["mem"] > 85` contributes two.
	// It is nil when no rule matched or when every matched rule is
	// predicate-only (no metric-fact comparisons); callers then keep
	// the event's payload violations.
	Violations []Violation
}

// Matcher evaluates whether an alert event should be dispatched to
// channels. Match performs a single evaluation that yields both the
// forward decision and the structured violations — there is no separate
// violations pass, so implementations must not evaluate the rule set
// twice.
type Matcher interface {
	// Match evaluates the alert event and returns the dispatch decision
	// together with the violations of every matched rule.
	Match(ctx context.Context, evt Event) MatchResult
}

// Channel sends an alert notification to an external system.
type Channel interface {
	// Send delivers the alert payload. It must be safe for concurrent use.
	Send(ctx context.Context, evt Event) error
	// Name identifies the channel in logs and metrics.
	Name() string
}

// Compile-time assertion that AlertMatcher satisfies Matcher. Failures
// surface at build time rather than at registration time.
var _ Matcher = (*AlertMatcher)(nil)

// AlertMatcher adapts the rule Engine to the Matcher interface, acting
// as a pre-filter for alert dispatch. Injected into the prism engine
// via AddRule, it evaluates the event once against the loaded rules and
// reports Forward=true when at least one rule matches. An empty rule
// set forwards every alert (default-allow) so the rule engine never
// silently drops alerts simply because no rules are configured.
//
//nolint:revive // AlertMatcher is the design-doc contract name, kept for cross-repo symmetry with tickraft-x
type AlertMatcher struct {
	engine *Engine
	store  asset.Getter
	cache  *cache.LRUCache
}

// cachedMatcherAsset is the cache snapshot of an asset lookup. The LRU
// cache serializes entries as JSON, so only the fields the evaluation
// env consumes are kept — caching the full asset.Asset would round-trip
// every column for nothing.
type cachedMatcherAsset struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Metadata string `json:"metadata,omitempty"`
}

// NewAlertMatcher creates an AlertMatcher backed by the supplied engine
// and optional asset.Getter (satisfied by any asset.Store). The getter
// enriches the evaluation environment with the asset associated with
// the alert; a nil getter leaves the asset domain limited to the
// event's asset id.
func NewAlertMatcher(engine *Engine, store asset.Getter) *AlertMatcher {
	return &AlertMatcher{
		engine: engine,
		store:  store,
		cache:  cache.NewLRU(matcherCacheSize, matcherCacheTTL),
	}
}

// buildEnv projects the alert into an AlertEnv, enriching the asset
// domain from the asset store when the lookup succeeds. A failed
// lookup is non-fatal: the env keeps the event's asset id and empty
// name/type/tags.
func (m *AlertMatcher) buildEnv(ctx context.Context, evt Event) AlertEnv {
	return buildAlertEnv(evt, m.loadAsset(ctx, evt.AssetID))
}

// loadAsset resolves the asset for rule evaluation through the LRU
// cache. Cache hits rebuild the three enrichment fields; misses load
// from the store and populate the cache. A failed or absent lookup
// yields nil and is not cached, mirroring the uncached semantics.
func (m *AlertMatcher) loadAsset(ctx context.Context, assetID int64) *asset.Asset {
	if m.store == nil {
		return nil
	}
	key := strconv.FormatInt(assetID, 10)
	if raw, ok := m.cache.Get(ctx, key); ok {
		var snap cachedMatcherAsset
		if sonic.Unmarshal(raw, &snap) == nil {
			return &asset.Asset{
				Name:      snap.Name,
				AssetType: types.AssetType(snap.Type),
				Metadata:  snap.Metadata,
			}
		}
	}
	found, err := m.store.GetByID(ctx, assetID)
	if err != nil || found == nil {
		return nil
	}
	if raw, err := sonic.Marshal(cachedMatcherAsset{
		Name:     found.Name,
		Type:     string(found.AssetType),
		Metadata: found.Metadata,
	}); err == nil {
		m.cache.Set(ctx, key, raw)
	}
	return found
}

// Match implements Matcher. It projects the alert into an AlertEnv,
// evaluates the rules once, and packages all outcomes: Forward is true
// when at least one rule matched (or when no rules are loaded, the
// default-allow contract), Matched identifies the matching rules, and
// Violations carries the structured violations they produced.
func (m *AlertMatcher) Match(ctx context.Context, evt Event) MatchResult {
	// Default-allow semantics: when no rules are loaded the matcher
	// forwards every alert without evaluation.
	if !m.engine.HasRules() {
		return MatchResult{Forward: true}
	}
	env := m.buildEnv(ctx, evt)
	matched, violations := m.engine.Evaluate(ctx, evt.TenantID, env)
	return MatchResult{Forward: len(matched) > 0, Matched: matched, Violations: violations}
}
