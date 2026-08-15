// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package middleware

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/config"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/route"
	"github.com/tickraft/tickraft/pkg/api/httputil"
	"github.com/tickraft/tickraft/pkg/errdefs"
)

// newAssetKeyTestEngine builds a route engine with the given asset-key
// middleware and a downstream handler that records whether it was called.
func newAssetKeyTestEngine(mw app.HandlerFunc) (*route.Engine, *bool) {
	var handlerCalled bool
	engine := route.NewEngine(config.NewOptions([]config.Option{}))
	engine.Use(mw)
	engine.POST("/", func(ctx context.Context, arc *app.RequestContext) {
		handlerCalled = true
		httputil.Success(arc, nil)
	})
	return engine, &handlerCalled
}

// TestAssetKeyalid verifies that when the X-Tickraft-Asset-Key header
// is present and the getter returns (true, nil), the request proceeds to
// the downstream handler with HTTP 200.
func TestAssetKeyalid(t *testing.T) {
	getter := func(_ context.Context, key string) (bool, error) {
		if key != "valid-key" {
			t.Errorf("getter received key %q, want %q", key, "valid-key")
		}
		return true, nil
	}
	engine, called := newAssetKeyTestEngine(NewAssetKeyMiddleware(getter))

	w := ut.PerformRequest(engine, "POST", "/", nil,
		ut.Header{Key: "X-Tickraft-Asset-Key", Value: "valid-key"})

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if !*called {
		t.Error("downstream handler was not called for a valid asset key")
	}
}

// TestAssetKeyissing verifies that when the X-Tickraft-Asset-Key
// header is absent, the middleware returns 401 with business code
// CodeAssetKeyMissing and does NOT invoke the downstream handler.
func TestAssetKeyissing(t *testing.T) {
	getter := func(_ context.Context, key string) (bool, error) {
		t.Error("getter should not be called when header is missing")
		return false, nil
	}
	engine, called := newAssetKeyTestEngine(NewAssetKeyMiddleware(getter))

	w := ut.PerformRequest(engine, "POST", "/", nil)

	if w.Code != 401 {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	if *called {
		t.Error("downstream handler should not be called when header is missing")
	}
	body := w.Body.String()
	if !strings.Contains(body, "40102") {
		t.Errorf("body = %q, want to contain business code %d (CodeAssetKeyMissing)", body, errdefs.CodeAssetKeyMissing)
	}
	if !strings.Contains(body, "missing asset key") {
		t.Errorf("body = %q, want to contain 'missing asset key'", body)
	}
}

// TestAssetKeynvalid verifies that when the getter returns (false, nil)
// for a present header, the middleware returns 403 with business code
// CodeAssetKeyInvalid and does NOT invoke the downstream handler.
func TestAssetKeynvalid(t *testing.T) {
	getter := func(_ context.Context, key string) (bool, error) {
		return false, nil
	}
	engine, called := newAssetKeyTestEngine(NewAssetKeyMiddleware(getter))

	w := ut.PerformRequest(engine, "POST", "/", nil,
		ut.Header{Key: "X-Tickraft-Asset-Key", Value: "bad-key"})

	if w.Code != 403 {
		t.Fatalf("status = %d, want 403", w.Code)
	}
	if *called {
		t.Error("downstream handler should not be called for an invalid asset key")
	}
	body := w.Body.String()
	if !strings.Contains(body, "40301") {
		t.Errorf("body = %q, want to contain business code %d (CodeAssetKeyInvalid)", body, errdefs.CodeAssetKeyInvalid)
	}
	if !strings.Contains(body, "invalid asset key") {
		t.Errorf("body = %q, want to contain 'invalid asset key'", body)
	}
}

// TestAssetKeyetterError verifies that when the getter returns
// (false, err) for a present header, the middleware returns 500 with
// business code CodeInternal and does NOT invoke the downstream handler.
func TestAssetKeyetterError(t *testing.T) {
	getter := func(_ context.Context, key string) (bool, error) {
		return false, errors.New("database down")
	}
	engine, called := newAssetKeyTestEngine(NewAssetKeyMiddleware(getter))

	w := ut.PerformRequest(engine, "POST", "/", nil,
		ut.Header{Key: "X-Tickraft-Asset-Key", Value: "any-key"})

	if w.Code != 500 {
		t.Fatalf("status = %d, want 500", w.Code)
	}
	if *called {
		t.Error("downstream handler should not be called when getter errors")
	}
	body := w.Body.String()
	if !strings.Contains(body, "50000") {
		t.Errorf("body = %q, want to contain business code %d (CodeInternal)", body, errdefs.CodeInternal)
	}
	// Internal-error messages must not leak the underlying cause.
	if strings.Contains(body, "database down") {
		t.Errorf("body = %q, must not leak internal error detail", body)
	}
}

// TestAssetKeymptyHeaderValue verifies that an empty header value is
// treated the same as a missing header: 401 with CodeAssetKeyMissing.
func TestAssetKeymptyHeaderValue(t *testing.T) {
	getter := func(_ context.Context, key string) (bool, error) {
		t.Error("getter should not be called for an empty header value")
		return false, nil
	}
	engine, called := newAssetKeyTestEngine(NewAssetKeyMiddleware(getter))

	// Setting an empty value via ut.Header may be elided by the test
	// helper; we explicitly set it on the request header to be safe.
	w := ut.PerformRequest(engine, "POST", "/", nil,
		ut.Header{Key: "X-Tickraft-Asset-Key", Value: ""})

	if w.Code != 401 {
		t.Fatalf("status = %d, want 401 (empty header treated as missing)", w.Code)
	}
	if *called {
		t.Error("downstream handler should not be called for an empty header value")
	}
}
