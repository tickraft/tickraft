// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package rule

import (
	"github.com/tickraft/tickraft/pkg/prism/alert"
	"github.com/tickraft/tickraft/pkg/types"
)

// Scene enumerates the rule matching scenes handled by the engine.
type Scene string

const (
	// SceneTask matches scheduler tasks prior to dispatch.
	SceneTask Scene = "task"
	// SceneProbe matches collector probe results and passive reports.
	SceneProbe Scene = "probe"
	// SceneMetric matches prism metric and log events as a pre-filter.
	SceneMetric Scene = Scene(types.EventKindMetric)
	// SceneRemediation matches self-healing remediation contexts,
	// selecting which remediation workflow to dispatch for a triggering
	// alert event.
	SceneRemediation Scene = "remediation"
)

// AlertType is a local alias that mirrors alert.Type so rule
// expressions and engine code share the same vocabulary without
// introducing a duplicate type definition.
type AlertType = alert.Type

// Rule is the runtime view of a rule consumed by the Engine. It pairs
// the compiled expression text with its identifying metadata so that
// the engine can correlate match results back to the originating rule.
type Rule struct {
	// ID uniquely identifies the rule within the persistence layer.
	ID int64
	// TenantID scopes the rule to a tenant for multi-tenant isolation.
	TenantID int64
	// Name is the human-readable rule name.
	Name string
	// Scene selects which matching scene the rule participates in.
	Scene Scene
	// Expression is the expr-lang source text compiled by the Compiler.
	Expression string
	// Priority orders rules within a scene; higher values fire first.
	Priority int
	// Enabled indicates whether the rule participates in matching.
	Enabled bool
	// Metadata holds optional extension key-value pairs.
	Metadata map[string]string
}

// Spec is the static configuration specification used to load
// rules from configuration files at startup. Unlike Rule, it omits
// runtime fields such as ID and TenantID which are assigned by the
// persistence layer or the calling context.
type Spec struct {
	// Name is the human-readable rule name.
	Name string
	// Scene selects which matching scene the rule participates in.
	Scene Scene
	// Expression is the expr-lang source text compiled by the Compiler.
	Expression string
	// Priority orders rules within a scene; higher values fire first.
	Priority int
	// Metadata holds optional extension key-value pairs.
	Metadata map[string]string
}
