// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package http

import (
	"context"
	"encoding/json"
	nethttp "net/http"
	"testing"

	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/telemetry"
)

// passivePoint builds an enabled passive webhook point for registry tests.
// The auth key is the wire-convention snake_case form.
func passivePoint(id, assetID int64, secret, authType string) telemetry.MonitorPoint {
	cfg := map[string]any{"auth_type": authType}
	if secret != "" {
		cfg["secret"] = secret
	}
	return telemetry.MonitorPoint{
		ID:      id,
		Mode:    telemetry.ModePassive,
		Type:    "webhook",
		Enabled: true,
		AssetID: assetID,
		Config:  cfg,
	}
}

func TestSecretRegistry_Match(t *testing.T) {
	reg := NewSecretRegistry()
	reg.SetPoint(passivePoint(1, 10, "point-one-secret", "hmac"))
	reg.SetPoint(passivePoint(2, 20, "point-two-secret", ""))

	body := []byte(`{"kind":"heartbeat"}`)

	owner, ok := reg.Match(body, computeHMAC(body, "point-two-secret"))
	if !ok {
		t.Fatal("expected signature signed with the second point's secret to match")
	}
	if owner.PointID != 2 || owner.AssetID != 20 {
		t.Fatalf("expected owner point 2 / asset 20, got point %d / asset %d", owner.PointID, owner.AssetID)
	}

	if _, ok := reg.Match(body, computeHMAC(body, "unknown-secret")); ok {
		t.Fatal("expected signature signed with an unregistered secret to fail")
	}
	if _, ok := reg.Match(body, ""); ok {
		t.Fatal("expected empty signature to fail")
	}
}

func TestSecretRegistry_MatchPoint(t *testing.T) {
	reg := NewSecretRegistry()
	reg.SetPoint(passivePoint(1, 10, "point-one-secret", "hmac"))
	reg.SetPoint(passivePoint(2, 20, "point-two-secret", ""))

	body := []byte(`{"kind":"heartbeat"}`)

	owner, ok := reg.MatchPoint(2, body, computeHMAC(body, "point-two-secret"))
	if !ok {
		t.Fatal("expected the hinted point's secret to match")
	}
	if owner.PointID != 2 || owner.AssetID != 20 {
		t.Fatalf("expected owner point 2 / asset 20, got point %d / asset %d", owner.PointID, owner.AssetID)
	}

	// A signature signed with a different point's secret must not match the
	// hinted point.
	if _, ok := reg.MatchPoint(1, body, computeHMAC(body, "point-two-secret")); ok {
		t.Fatal("expected another point's credential to fail the hinted match")
	}
	// Unknown points and non-positive IDs never match.
	if _, ok := reg.MatchPoint(999, body, computeHMAC(body, "point-one-secret")); ok {
		t.Fatal("expected an unknown point ID to fail")
	}
	if _, ok := reg.MatchPoint(0, body, computeHMAC(body, "point-one-secret")); ok {
		t.Fatal("expected a zero point ID to fail")
	}
	if _, ok := reg.MatchPoint(1, body, ""); ok {
		t.Fatal("expected an empty signature to fail")
	}
}

func TestSecretRegistry_SetPointReplacesSecret(t *testing.T) {
	reg := NewSecretRegistry()
	reg.SetPoint(passivePoint(1, 10, "old-secret", "hmac"))
	reg.SetPoint(passivePoint(1, 10, "new-secret", "hmac"))

	body := []byte(`{"kind":"heartbeat"}`)
	if _, ok := reg.Match(body, computeHMAC(body, "old-secret")); ok {
		t.Fatal("expected the replaced secret to stop verifying")
	}
	if _, ok := reg.Match(body, computeHMAC(body, "new-secret")); !ok {
		t.Fatal("expected the new secret to verify")
	}
	if reg.Len() != 1 {
		t.Fatalf("expected one registered secret after replacement, got %d", reg.Len())
	}
}

func TestSecretRegistry_RemoveAndEmptySecret(t *testing.T) {
	reg := NewSecretRegistry()
	reg.SetPoint(passivePoint(1, 10, "secret", "hmac"))
	reg.RemovePoint(1)
	if reg.Len() != 0 {
		t.Fatalf("expected empty registry after removal, got %d", reg.Len())
	}

	// Clearing the secret (empty string) also removes the entry.
	reg.SetPoint(passivePoint(2, 20, "secret", "hmac"))
	reg.SetPoint(passivePoint(2, 20, "", "hmac"))
	if reg.Len() != 0 {
		t.Fatalf("expected empty registry after clearing the secret, got %d", reg.Len())
	}
}

func TestSecretRegistry_NonCredentialsNotRegistered(t *testing.T) {
	reg := NewSecretRegistry()
	// asset-key points authenticate via the asset store; their secret is
	// not an HMAC credential.
	reg.SetPoint(passivePoint(1, 10, "asset-key-secret", "asset-key"))
	// Disabled points must not authenticate ingestion.
	disabled := passivePoint(2, 20, "disabled-secret", "hmac")
	disabled.Enabled = false
	reg.SetPoint(disabled)
	// Active points have no webhook secret at all.
	reg.SetPoint(telemetry.MonitorPoint{ID: 3, Mode: telemetry.ModeActive, Enabled: true})

	if reg.Len() != 0 {
		t.Fatalf("expected no registered secrets, got %d", reg.Len())
	}
}

// newPointSecretListener builds a listener with the given registry and no
// global secret, mirroring the wiring used by the API server.
func newPointSecretListener(reg *SecretRegistry, ingest func(context.Context, *telemetry.Telemetry) error) *Listener {
	return New(
		WithStore(newMockStore()),
		WithSecretRegistry(reg),
		WithIngest(ingest),
		WithLogger(zap.NewNop()),
	)
}

func TestListener_PointSecret_BindsOwnerAsset(t *testing.T) {
	reg := NewSecretRegistry()
	reg.SetPoint(passivePoint(1, 1, "point-secret", "hmac"))
	cb, peek := captureIngest()
	h := newPointSecretListener(reg, cb)

	// No asset identity in the request: the owner point's asset is adopted.
	body, _ := json.Marshal(telemetryRequest{Kind: "heartbeat", reportRequest: reportRequest{LogContent: "signed"}})
	sig := computeHMAC(body, "point-secret")
	resp := mustPost(t, h.ReportHandler(), body, [2]string{"X-Tickraft-Signature", sig})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != nethttp.StatusAccepted {
		t.Fatalf("status = %d, want %d", resp.StatusCode, nethttp.StatusAccepted)
	}
	got := peek()
	if got == nil {
		t.Fatal("ingest not called")
	}
	if got.AssetID != 1 || got.TenantID != 100 {
		t.Fatalf("expected report bound to owner asset 1 / tenant 100, got asset %d / tenant %d",
			got.AssetID, got.TenantID)
	}
}

func TestListener_PointSecret_MatchingAssetIDAccepted(t *testing.T) {
	reg := NewSecretRegistry()
	reg.SetPoint(passivePoint(1, 1, "point-secret", "hmac"))
	cb, _ := captureIngest()
	h := newPointSecretListener(reg, cb)

	body, _ := json.Marshal(telemetryRequest{Kind: "heartbeat", reportRequest: reportRequest{AssetID: 1}})
	sig := computeHMAC(body, "point-secret")
	resp := mustPost(t, h.ReportHandler(), body, [2]string{"X-Tickraft-Signature", sig})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != nethttp.StatusAccepted {
		t.Fatalf("status = %d, want %d", resp.StatusCode, nethttp.StatusAccepted)
	}
}

func TestListener_PointSecret_AssetMismatchForbidden(t *testing.T) {
	reg := NewSecretRegistry()
	reg.SetPoint(passivePoint(1, 1, "point-secret", "hmac"))
	h := newPointSecretListener(reg, func(_ context.Context, _ *telemetry.Telemetry) error { return nil })

	body, _ := json.Marshal(telemetryRequest{Kind: "heartbeat", reportRequest: reportRequest{AssetID: 999}})
	sig := computeHMAC(body, "point-secret")
	resp := mustPost(t, h.ReportHandler(), body, [2]string{"X-Tickraft-Signature", sig})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != nethttp.StatusForbidden {
		t.Fatalf("status = %d, want %d", resp.StatusCode, nethttp.StatusForbidden)
	}
}

func TestListener_PointSecret_WrongSignatureRejected(t *testing.T) {
	reg := NewSecretRegistry()
	reg.SetPoint(passivePoint(1, 1, "point-secret", "hmac"))
	h := newPointSecretListener(reg, func(_ context.Context, _ *telemetry.Telemetry) error { return nil })

	body, _ := json.Marshal(telemetryRequest{Kind: "heartbeat", reportRequest: reportRequest{AssetID: 1}})
	resp := mustPost(t, h.ReportHandler(), body, [2]string{"X-Tickraft-Signature", "deadbeef"})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != nethttp.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", resp.StatusCode, nethttp.StatusUnauthorized)
	}
}

func TestListener_PointSecret_NoSignatureStillAssetKey(t *testing.T) {
	// With no global secret configured, unsigned reports keep using
	// asset-key authentication; the registry alone does not force signing.
	reg := NewSecretRegistry()
	reg.SetPoint(passivePoint(1, 1, "point-secret", "hmac"))
	cb, _ := captureIngest()
	h := newPointSecretListener(reg, cb)

	body, _ := json.Marshal(telemetryRequest{Kind: "heartbeat", reportRequest: reportRequest{AssetID: 1}})
	resp := mustPost(t, h.ReportHandler(), body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != nethttp.StatusAccepted {
		t.Fatalf("status = %d, want %d", resp.StatusCode, nethttp.StatusAccepted)
	}
}

func TestListener_PointSecret_UnboundPointTrusted(t *testing.T) {
	// A per-point secret on a point without an asset binding authenticates
	// the reporter the same way the global secret does.
	reg := NewSecretRegistry()
	reg.SetPoint(passivePoint(7, 0, "unbound-secret", "hmac"))
	cb, peek := captureIngest()
	h := newPointSecretListener(reg, cb)

	body, _ := json.Marshal(telemetryRequest{Kind: "heartbeat", reportRequest: reportRequest{AssetID: 1}})
	sig := computeHMAC(body, "unbound-secret")
	resp := mustPost(t, h.ReportHandler(), body, [2]string{"X-Tickraft-Signature", sig})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != nethttp.StatusAccepted {
		t.Fatalf("status = %d, want %d", resp.StatusCode, nethttp.StatusAccepted)
	}
	if got := peek(); got == nil || got.AssetID != 1 {
		t.Fatalf("expected report for the requested asset 1, got %+v", got)
	}
}

func TestListener_PointSecret_GlobalFallback(t *testing.T) {
	// A signature matching neither per-point secret still verifies against
	// the global secret.
	reg := NewSecretRegistry()
	reg.SetPoint(passivePoint(1, 1, "point-secret", "hmac"))
	cb, _ := captureIngest()
	h := New(
		WithStore(newMockStore()),
		WithSecretRegistry(reg),
		WithSecret("global-secret"),
		WithIngest(cb),
		WithLogger(zap.NewNop()),
	)

	body, _ := json.Marshal(telemetryRequest{Kind: "heartbeat", reportRequest: reportRequest{AssetID: 1}})
	sig := computeHMAC(body, "global-secret")
	resp := mustPost(t, h.ReportHandler(), body, [2]string{"X-Tickraft-Signature", sig})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != nethttp.StatusAccepted {
		t.Fatalf("status = %d, want %d", resp.StatusCode, nethttp.StatusAccepted)
	}
}

func TestListener_PointSecret_PointIDHint(t *testing.T) {
	// The point_id query hint narrows verification to exactly that point's
	// secret: one HMAC instead of a scan over the registry.
	reg := NewSecretRegistry()
	reg.SetPoint(passivePoint(1, 1, "point-one-secret", "hmac"))
	reg.SetPoint(passivePoint(2, 2, "point-two-secret", "hmac"))
	cb, peek := captureIngest()
	h := newPointSecretListener(reg, cb)

	body, _ := json.Marshal(telemetryRequest{Kind: "heartbeat", reportRequest: reportRequest{LogContent: "hinted"}})
	sig := computeHMAC(body, "point-two-secret")
	resp := mustPostTo(t, h.ReportHandler(), "?point_id=2", body, [2]string{"X-Tickraft-Signature", sig})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != nethttp.StatusAccepted {
		t.Fatalf("status = %d, want %d", resp.StatusCode, nethttp.StatusAccepted)
	}
	if got := peek(); got == nil || got.AssetID != 2 {
		t.Fatalf("expected report bound to hinted point's asset 2, got %+v", got)
	}
}

func TestListener_PointSecret_PointIDHintNarrowsNoScan(t *testing.T) {
	// A request claiming point 2 but signed with point 1's secret must be
	// rejected even though an unhinted scan would have matched point 1: the
	// hint disables the fallback scan, and no global secret is configured.
	reg := NewSecretRegistry()
	reg.SetPoint(passivePoint(1, 1, "point-one-secret", "hmac"))
	reg.SetPoint(passivePoint(2, 2, "point-two-secret", "hmac"))
	h := newPointSecretListener(reg, func(_ context.Context, _ *telemetry.Telemetry) error { return nil })

	body, _ := json.Marshal(telemetryRequest{Kind: "heartbeat", reportRequest: reportRequest{AssetID: 1}})
	sig := computeHMAC(body, "point-one-secret")
	resp := mustPostTo(t, h.ReportHandler(), "?point_id=2", body, [2]string{"X-Tickraft-Signature", sig})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != nethttp.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", resp.StatusCode, nethttp.StatusUnauthorized)
	}
}

func TestListener_PointSecret_PointIDHintFallsBackToGlobal(t *testing.T) {
	// A hint for a point without a registered secret (asset-key point) is
	// not an error: the global secret remains the fallback credential.
	reg := NewSecretRegistry()
	reg.SetPoint(passivePoint(1, 1, "asset-key-secret", "asset-key"))
	cb, _ := captureIngest()
	h := New(
		WithStore(newMockStore()),
		WithSecretRegistry(reg),
		WithSecret("global-secret"),
		WithIngest(cb),
		WithLogger(zap.NewNop()),
	)

	body, _ := json.Marshal(telemetryRequest{Kind: "heartbeat", reportRequest: reportRequest{AssetID: 1}})
	sig := computeHMAC(body, "global-secret")
	resp := mustPostTo(t, h.ReportHandler(), "?point_id=1", body, [2]string{"X-Tickraft-Signature", sig})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != nethttp.StatusAccepted {
		t.Fatalf("status = %d, want %d", resp.StatusCode, nethttp.StatusAccepted)
	}
}
