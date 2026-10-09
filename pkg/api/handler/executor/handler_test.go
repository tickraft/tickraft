// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details

package executor

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"

	"github.com/tickraft/tickraft/pkg/executor"
	httpprober "github.com/tickraft/tickraft/pkg/executor/http"
	"github.com/tickraft/tickraft/pkg/executor/icmp"
	"github.com/tickraft/tickraft/pkg/executor/local"
	"github.com/tickraft/tickraft/pkg/executor/tcp"
	"github.com/tickraft/tickraft/pkg/executor/webhook"
)

// stubExecutor is a minimal executor.Executor whose capability is fixed at
// construction; it exercises capability filtering without standing up the
// real executors.
type stubExecutor struct {
	name string
	caps executor.Capability
}

func (e *stubExecutor) Name() string                      { return e.name }
func (e *stubExecutor) Capabilities() executor.Capability { return e.caps }
func (e *stubExecutor) Execute(context.Context, executor.ExecutionRequest) (*executor.Result, error) {
	return nil, errors.New("stub executor: not implemented")
}

// newListHarness builds a Handler over a registry with the five CE
// executors plus a plugin-style stub, mirroring what a runtime registers.
func newListHarness(t *testing.T) *Handler {
	t.Helper()
	reg := executor.NewRegistry()
	for _, e := range []executor.Executor{
		local.New(),
		webhook.New(),
		icmp.New(0),
		tcp.New(0),
		httpprober.New(),
		&stubExecutor{name: "dns", caps: executor.CapProbe},
	} {
		if err := reg.Register(e); err != nil {
			t.Fatalf("register executor %q: %v", e.Name(), err)
		}
	}
	return NewHandler(reg)
}

// decodeTypes runs the handler method and decodes the success envelope.
func decodeTypes(t *testing.T, h *Handler, run func(*Handler, *app.RequestContext)) []Type {
	t.Helper()
	arc := &app.RequestContext{}
	run(h, arc)
	var env struct {
		Data []Type `json:"data"`
	}
	if err := json.Unmarshal(arc.Response.Body(), &env); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return env.Data
}

// TestHandlerListProbersDerivedFromRegistry verifies that the prober list
// is derived from the registry's CapProbe membership, includes plugin
// executors, and never contains non-probe types like udp/webhook/local.
func TestHandlerListProbersDerivedFromRegistry(t *testing.T) {
	h := newListHarness(t)
	types := decodeTypes(t, h, func(h *Handler, arc *app.RequestContext) {
		h.ListProbers(context.Background(), arc)
	})

	got := map[string]bool{}
	for _, ty := range types {
		got[ty.Type] = true
	}
	for _, want := range []string{"icmp", "tcp", "http", "dns"} {
		if !got[want] {
			t.Errorf("probers missing %q: %v", want, types)
		}
	}
	for _, absent := range []string{"local", "webhook", "udp"} {
		if got[absent] {
			t.Errorf("probers must not contain %q: %v", absent, types)
		}
	}
	// Sorted by type for a stable response order.
	for i := 1; i < len(types); i++ {
		if types[i-1].Type > types[i].Type {
			t.Fatalf("probers not sorted: %v", types)
		}
	}
}

// TestHandlerListExecutorsDerivedFromRegistry verifies that the executor
// list follows OpExecute semantics (any write capability), so the
// dual-mode http executor appears alongside local and webhook while
// pure probers do not.
func TestHandlerListExecutorsDerivedFromRegistry(t *testing.T) {
	h := newListHarness(t)
	types := decodeTypes(t, h, func(h *Handler, arc *app.RequestContext) {
		h.List(context.Background(), arc)
	})

	got := map[string]bool{}
	for _, ty := range types {
		got[ty.Type] = true
	}
	for _, want := range []string{"http", "local", "webhook"} {
		if !got[want] {
			t.Errorf("executors missing %q: %v", want, types)
		}
	}
	for _, absent := range []string{"icmp", "tcp", "dns", "udp"} {
		if got[absent] {
			t.Errorf("executors must not contain %q: %v", absent, types)
		}
	}
}

// TestHandlerNilRegistryReturnsEmpty verifies the nil-registry fallback:
// both endpoints answer an empty list instead of panicking.
func TestHandlerNilRegistryReturnsEmpty(t *testing.T) {
	h := NewHandler(nil)
	probers := decodeTypes(t, h, func(h *Handler, arc *app.RequestContext) {
		h.ListProbers(context.Background(), arc)
	})
	if len(probers) != 0 {
		t.Errorf("probers = %v, want empty", probers)
	}
	executors := decodeTypes(t, h, func(h *Handler, arc *app.RequestContext) {
		h.List(context.Background(), arc)
	})
	if len(executors) != 0 {
		t.Errorf("executors = %v, want empty", executors)
	}
}

// TestDescribeFallbackForUnknownType verifies that types without a catalog
// entry fall back to the raw executor name instead of an empty display
// name.
func TestDescribeFallbackForUnknownType(t *testing.T) {
	ty := Describe("dns")
	if ty.Name != "dns" {
		t.Errorf("Describe(dns).Name = %q, want %q", ty.Name, "dns")
	}
	known := Describe("http")
	if known.Name == "http" || known.Description == "" {
		t.Errorf("Describe(http) = %+v, want catalog entry", known)
	}
}
