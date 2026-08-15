// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package middleware

import (
	"context"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/config"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/route"
	"github.com/tickraft/tickraft/pkg/api/httputil"
	"github.com/tickraft/tickraft/pkg/auth/region"
)

const (
	testCookieSecret = "test-cookie-signing-secret-key-32b"
	testCookieRegion = "cn"
	testCookieTS     = int64(1700000000)
)

// newRegionTestEngine creates a test engine with the given region middleware
// and a handler that records the resolved region from the context.
func newRegionTestEngine(mw app.HandlerFunc) (*route.Engine, *string) {
	var recorded string
	engine := route.NewEngine(config.NewOptions([]config.Option{}))
	engine.Use(mw)
	engine.GET("/", func(ctx context.Context, arc *app.RequestContext) {
		recorded = httputil.GetRegion(ctx)
		httputil.Success(arc, nil)
	})
	return engine, &recorded
}

// TestRegionHeaderPriority verifies that the X-Tickraft-Region header takes
// the highest priority and is used as the resolved region.
func TestRegionHeaderPriority(t *testing.T) {
	mw := NewRegionMiddleware([]byte(testCookieSecret), nil, nil)
	engine, recorded := newRegionTestEngine(mw)

	// Provide both a header and a valid cookie; header should win.
	signed := region.SignCookie("global", testCookieTS, []byte(testCookieSecret))
	w := ut.PerformRequest(engine, "GET", "/", nil,
		ut.Header{Key: "X-Tickraft-Region", Value: "cn"},
		ut.Header{Key: "Cookie", Value: "tk_region=" + signed},
	)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if *recorded != "cn" {
		t.Errorf("region = %q, want %q", *recorded, "cn")
	}
	if got := w.Header().Get(routeRegionHeader); got != "cn" {
		t.Errorf("response header %q = %q, want %q", routeRegionHeader, got, "cn")
	}
}

// TestRegionCookiePriority verifies that when no header is present, a valid
// HMAC-signed tk_region cookie is used as the resolved region.
func TestRegionCookiePriority(t *testing.T) {
	signed := region.SignCookie(testCookieRegion, testCookieTS, []byte(testCookieSecret))
	mw := NewRegionMiddleware([]byte(testCookieSecret), nil, func(ip string) string {
		t.Error("geoipLookup should not be called when cookie is valid")
		return "global"
	})
	engine, recorded := newRegionTestEngine(mw)

	w := ut.PerformRequest(engine, "GET", "/", nil,
		ut.Header{Key: "Cookie", Value: "tk_region=" + signed},
	)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if *recorded != testCookieRegion {
		t.Errorf("region = %q, want %q", *recorded, testCookieRegion)
	}
	if got := w.Header().Get(routeRegionHeader); got != testCookieRegion {
		t.Errorf("response header %q = %q, want %q", routeRegionHeader, got, testCookieRegion)
	}
}

// TestRegionCookieWithKeyRotator verifies that the key rotator is used for
// cookie verification when provided.
func TestRegionCookieWithKeyRotator(t *testing.T) {
	kr := region.NewKeyRotator(
		[]byte(testCookieSecret),
		nil,
		region.DefaultRotationInterval,
		region.DefaultTransitionPeriod,
	)
	signed := kr.Sign(testCookieRegion, testCookieTS)
	mw := NewRegionMiddleware(nil, kr, nil)
	engine, recorded := newRegionTestEngine(mw)

	w := ut.PerformRequest(engine, "GET", "/", nil,
		ut.Header{Key: "Cookie", Value: "tk_region=" + signed},
	)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if *recorded != testCookieRegion {
		t.Errorf("region = %q, want %q", *recorded, testCookieRegion)
	}
}

// TestRegionGeoIPFallback verifies that when no header and no cookie are
// present, the GeoIP lookup function is used to determine the region.
func TestRegionGeoIPFallback(t *testing.T) {
	geoip := func(ip string) string {
		return "eu-west"
	}
	mw := NewRegionMiddleware(nil, nil, geoip)
	engine, recorded := newRegionTestEngine(mw)

	w := ut.PerformRequest(engine, "GET", "/", nil)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if *recorded != "eu-west" {
		t.Errorf("region = %q, want %q", *recorded, "eu-west")
	}
	if got := w.Header().Get(routeRegionHeader); got != "eu-west" {
		t.Errorf("response header %q = %q, want %q", routeRegionHeader, got, "eu-west")
	}
}

// TestRegionCookieHMACFailure verifies that when the cookie's HMAC signature is
// invalid, the middleware does not block the request and falls back to GeoIP.
func TestRegionCookieHMACFailure(t *testing.T) {
	geoip := func(ip string) string {
		return "global"
	}
	mw := NewRegionMiddleware([]byte(testCookieSecret), nil, geoip)
	engine, recorded := newRegionTestEngine(mw)

	// Cookie with an invalid HMAC signature.
	w := ut.PerformRequest(engine, "GET", "/", nil,
		ut.Header{Key: "Cookie", Value: "tk_region=cn:1700000000:invalid_signature"},
	)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200 (should not block on bad cookie)", w.Code)
	}
	if *recorded != "global" {
		t.Errorf("region = %q, want %q (GeoIP fallback)", *recorded, "global")
	}
}

// TestRegionNoInfo verifies that when no header, no cookie, and no GeoIP lookup
// are available, the default region "global" is used.
func TestRegionNoInfo(t *testing.T) {
	mw := NewRegionMiddleware(nil, nil, nil)
	engine, recorded := newRegionTestEngine(mw)

	w := ut.PerformRequest(engine, "GET", "/", nil)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if *recorded != defaultRegion {
		t.Errorf("region = %q, want %q", *recorded, defaultRegion)
	}
	if got := w.Header().Get(routeRegionHeader); got != defaultRegion {
		t.Errorf("response header %q = %q, want %q", routeRegionHeader, got, defaultRegion)
	}
}
