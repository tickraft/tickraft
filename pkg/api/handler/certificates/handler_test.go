// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package certificates

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/cloudwego/hertz/pkg/common/config"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/route"

	"github.com/tickraft/tickraft/pkg/api"
)

// stubReloader is a test stub for the Reloader interface. It returns the
// configured fingerprint and error on each ReloadTLSConfig call, and records
// the call count so tests can assert the handler actually invoked the reloader.
type stubReloader struct {
	fingerprint string
	err         error
	calls       int
}

// ReloadTLSConfig satisfies the Reloader interface. It increments
// calls so tests can assert the handler invoked the reloader exactly once per
// request.
func (s *stubReloader) ReloadTLSConfig() (string, error) {
	s.calls++
	return s.fingerprint, s.err
}

// newCertificateTestEngine wires a Handler backed by the given
// reloader onto a fresh route.Engine at the reload path, mirroring the
// production registration in routes.go.
func newCertificateTestEngine(h *Handler) *route.Engine {
	engine := route.NewEngine(config.NewOptions([]config.Option{}))
	engine.POST("/api/v1/system/certificates/reload", h.Reload)
	return engine
}

// decodeAPIResponse unmarshals the recorded response body into an
// api.Response so tests can assert on the envelope code/message/data fields.
func decodeAPIResponse(t *testing.T, w *ut.ResponseRecorder) api.Response {
	t.Helper()
	var resp api.Response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v (body=%q)", err, w.Body.String())
	}
	return resp
}

// decodeReloadData re-marshals the api.Response Data field and decodes it into
// a reloadResponse so tests can assert on the returned fingerprint.
func decodeReloadData(t *testing.T, resp api.Response) string {
	t.Helper()
	raw, err := json.Marshal(resp.Data)
	if err != nil {
		t.Fatalf("marshal data: %v", err)
	}
	var got struct {
		Fingerprint string `json:"fingerprint"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal reload data: %v", err)
	}
	return got.Fingerprint
}

// TestNewHandlerNilReloader verifies that passing a nil reloader
// returns an error at construction time. This protects against a wiring
// mistake that would otherwise surface as a nil-pointer dereference on the
// first request.
func TestNewHandlerNilReloader(t *testing.T) {
	_, err := NewHandler(nil)
	if err == nil {
		t.Fatal("expected error when reloader is nil, got nil")
	}
}

// TestCertificateReloadSuccess verifies that a successful reload returns
// 200 with code=0 and the fingerprint returned by the reloader. It also
// asserts the reloader was invoked exactly once per request.
func TestCertificateReloadSuccess(t *testing.T) {
	const wantFingerprint = "abcdef0123456789"
	reloader := &stubReloader{fingerprint: wantFingerprint}
	h, err := NewHandler(reloader)
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	engine := newCertificateTestEngine(h)

	w := ut.PerformRequest(engine, "POST", "/api/v1/system/certificates/reload", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body=%q)", w.Code, http.StatusOK, w.Body.String())
	}
	resp := decodeAPIResponse(t, w)
	if resp.Code != 0 {
		t.Errorf("code = %d, want 0", resp.Code)
	}
	if got := decodeReloadData(t, resp); got != wantFingerprint {
		t.Errorf("fingerprint = %q, want %q", got, wantFingerprint)
	}
	if reloader.calls != 1 {
		t.Errorf("reloader calls = %d, want 1", reloader.calls)
	}
}

// TestCertificateReloadFailure verifies that a reload failure is surfaced
// via api.Fail (non-zero code, non-empty message) and that the reloader was
// still invoked exactly once. The handler must not panic and must not
// silently swallow the error.
func TestCertificateReloadFailure(t *testing.T) {
	reloadErr := errors.New("reload: open cert file: permission denied")
	reloader := &stubReloader{err: reloadErr}
	h, err := NewHandler(reloader)
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	engine := newCertificateTestEngine(h)

	w := ut.PerformRequest(engine, "POST", "/api/v1/system/certificates/reload", nil)
	// The handler delegates to api.Fail which maps the error to an HTTP
	// status. A generic error maps to 500 (internal error) per the
	// mapError fallback. The exact status is not load-bearing here; what
	// matters is that the response is an error envelope.
	if w.Code == http.StatusOK {
		t.Fatalf("status = %d, want non-200 (error) (body=%q)", w.Code, w.Body.String())
	}
	resp := decodeAPIResponse(t, w)
	if resp.Code == 0 {
		t.Errorf("code = 0, want non-zero (error)")
	}
	if resp.Message == "" {
		t.Errorf("message = %q, want non-empty error message", resp.Message)
	}
	if resp.Data != nil {
		t.Errorf("data = %v, want nil on error", resp.Data)
	}
	if reloader.calls != 1 {
		t.Errorf("reloader calls = %d, want 1", reloader.calls)
	}
}

// TestCertificateReloadSignature verifies Reload has the Hertz handler
// signature so it can be registered directly as an app.HandlerFunc.
func TestCertificateReloadSignature(t *testing.T) {
	var _ = (*Handler)(nil).Reload
}

// TestStubReloaderSatisfiesInterface verifies the test stub satisfies the
// Reloader interface at compile time.
func TestStubReloaderSatisfiesInterface(t *testing.T) {
	var _ Reloader = (*stubReloader)(nil)
}
