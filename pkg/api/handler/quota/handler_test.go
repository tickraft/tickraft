// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details

package quota

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"go.uber.org/zap"

	pkgquota "github.com/tickraft/tickraft/pkg/quota"
)

// fixedProvider is a test Provider with a caller-supplied ceiling table.
type fixedProvider struct{ ceilings map[pkgquota.Type]int }

func (p fixedProvider) Ceiling(t pkgquota.Type) int { return p.ceilings[t] }

// runUsage executes the Usage handler and decodes the success envelope.
func runUsage(t *testing.T, h *Handler) []UsageItem {
	t.Helper()
	arc := &app.RequestContext{}
	h.Usage(context.Background(), arc)
	var env struct {
		Data []UsageItem `json:"data"`
	}
	if err := json.Unmarshal(arc.Response.Body(), &env); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return env.Data
}

func countOf(n int64) func(context.Context) (int64, error) {
	return func(context.Context) (int64, error) { return n, nil }
}

// TestUsageAggregatesSourcesWithRatio verifies each source maps to one row
// with the ceiling from the active Provider and the ratio derived from
// used/ceiling.
func TestUsageAggregatesSourcesWithRatio(t *testing.T) {
	prev := pkgquota.ActiveProvider()
	pkgquota.SetProvider(fixedProvider{ceilings: map[pkgquota.Type]int{
		pkgquota.TypeDevice: 20,
		pkgquota.TypeHost:   0, // unlimited: ratio must stay 0
	}})
	t.Cleanup(func() { pkgquota.SetProvider(prev) })

	h := NewHandler(zap.NewNop(),
		UsageSource{Type: pkgquota.TypeDevice, Count: countOf(10)},
		UsageSource{Type: pkgquota.TypeHost, Count: countOf(7)},
	)
	items := runUsage(t, h)

	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %v", items)
	}
	if items[0] != (UsageItem{Type: "device", Used: 10, Ceiling: 20, Ratio: 0.5}) {
		t.Errorf("device item mismatch: %+v", items[0])
	}
	if items[1] != (UsageItem{Type: "host", Used: 7, Ceiling: 0, Ratio: 0}) {
		t.Errorf("unlimited item must carry ratio 0: %+v", items[1])
	}
}

// TestUsageSkipsNilCountAndOmitsFailures verifies the warn-and-continue
// contract: nil Count sources are dropped at construction and a failing
// counter is omitted from the response without failing the request.
func TestUsageSkipsNilCountAndOmitsFailures(t *testing.T) {
	prev := pkgquota.ActiveProvider()
	pkgquota.SetProvider(fixedProvider{ceilings: map[pkgquota.Type]int{
		pkgquota.TypeContact: 2,
	}})
	t.Cleanup(func() { pkgquota.SetProvider(prev) })

	h := NewHandler(zap.NewNop(),
		UsageSource{Type: pkgquota.TypeScheduledTask, Count: nil},
		UsageSource{
			Type: pkgquota.TypeRemediation,
			Count: func(context.Context) (int64, error) {
				return 0, errors.New("store down")
			},
		},
		UsageSource{Type: pkgquota.TypeContact, Count: countOf(2)},
	)
	items := runUsage(t, h)

	if len(items) != 1 {
		t.Fatalf("expected only the healthy source, got %v", items)
	}
	if items[0].Type != "contact" || items[0].Used != 2 || items[0].Ratio != 1 {
		t.Errorf("contact item mismatch: %+v", items[0])
	}
}
