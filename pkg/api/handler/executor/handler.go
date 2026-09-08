// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details

// Package executor exposes the executor type enumeration endpoints derived
// from the runtime executor registry. Membership is always the registry's
// truth: an executor registered by a plugin or the professional edition
// appears automatically, and a built-in type absent from the registry (for
// example a prober compiled out of a deployment) disappears.
package executor

import (
	"context"
	"sort"

	"github.com/cloudwego/hertz/pkg/app"

	"github.com/tickraft/tickraft/pkg/api/httputil"
	"github.com/tickraft/tickraft/pkg/executor"
)

// Type describes an executor or prober type available in the current
// runtime. Type is the executor name that populates Task.ExecutorType or
// the Type field of an active MonitorPoint.
type Type struct {
	// Type is the executor identifier (http, local, webhook, icmp, tcp).
	Type string `json:"type"`
	// Name is the human-readable display name.
	Name string `json:"name"`
	// Description is a short summary of the capability.
	Description string `json:"description,omitempty"`
}

// displayCatalog carries human-readable labels for the built-in executors.
// Types registered without a catalog entry (plugins, professional edition)
// fall back to the raw executor name.
var displayCatalog = map[string]Type{
	"icmp":       {Type: "icmp", Name: "ICMP Ping", Description: "Probe host reachability via ICMP echo requests"},
	"tcp":        {Type: "tcp", Name: "TCP Port", Description: "Probe TCP port connectivity and response time"},
	"mqtt_probe": {Type: "mqtt_probe", Name: "MQTT", Description: "Connect to an MQTT server and await a message"},
	"http":       {Type: "http", Name: "HTTP", Description: "Probe HTTP endpoint availability and status code"},
	"local":      {Type: "local", Name: "Local Script", Description: "Execute local scripts or shell commands"},
	"webhook":    {Type: "webhook", Name: "Webhook", Description: "Deliver event payloads to external HTTP endpoints"},
}

// Describe returns the display metadata for an executor type, falling back
// to the raw type name when no catalog entry exists.
func Describe(name string) Type {
	if t, ok := displayCatalog[name]; ok {
		return t
	}
	return Type{Type: name, Name: name}
}

// Handler enumerates the executor types registered in the runtime executor
// registry, filtered by capability. A nil registry yields empty lists.
type Handler struct {
	registry *executor.Registry
}

// NewHandler creates a Handler backed by the given executor registry. The
// registry may be nil; the endpoints then return empty lists.
func NewHandler(registry *executor.Registry) *Handler {
	return &Handler{registry: registry}
}

// listBy returns the display metadata of every registered executor whose
// capabilities satisfy keep, sorted by type for a stable response order.
func (h *Handler) listBy(keep func(executor.Capability) bool) []Type {
	if h.registry == nil {
		return []Type{}
	}
	types := make([]Type, 0, 8)
	for _, e := range h.registry.Executors() {
		if keep(e.Capabilities()) {
			types = append(types, Describe(e.Name()))
		}
	}
	sort.Slice(types, func(i, j int) bool { return types[i].Type < types[j].Type })
	return types
}

// List handles GET /api/v1/executors. It returns the executor types that
// support OpExecute semantics (any write capability: local scripts,
// webhooks, ...) — the same predicate the task CRUD prevalidation applies
// to Task.ExecutorType.
func (h *Handler) List(_ context.Context, arc *app.RequestContext) {
	httputil.Success(arc, h.listBy(executor.HasWrite))
}

// ListProbers handles GET /api/v1/telemetry/probers. It returns the
// executor types with probing capability (CapProbe) — the types valid for
// the Type field of an active monitoring point.
func (h *Handler) ListProbers(_ context.Context, arc *app.RequestContext) {
	httputil.Success(arc, h.listBy(func(c executor.Capability) bool {
		return executor.HasCap(c, executor.CapProbe)
	}))
}
