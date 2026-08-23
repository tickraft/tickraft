// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package asset

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/tickraft/tickraft/pkg/types"
)

func TestTypeConstants(t *testing.T) {
	tests := []struct {
		name  string
		value types.AssetType
		want  string
	}{
		{"TypeTask", types.AssetTypeTask, "task"},
		{"TypeDevice", types.AssetTypeDevice, "device"},
		{"TypeHost", types.AssetTypeHost, "host"},
		{"TypePort", types.AssetTypePort, "port"},
		{"TypeWebsite", types.AssetTypeWebsite, "website"},
		{"TypeService", types.AssetTypeService, "service"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.value) != tt.want {
				t.Errorf("%s = %q, want %q", tt.name, tt.value, tt.want)
			}
		})
	}
}

func TestStatusConstants(t *testing.T) {
	tests := []struct {
		name  string
		value types.AssetStatus
		want  string
	}{
		{"StatusNormal", types.AssetStatusNormal, "normal"},
		{"StatusAbnormal", types.AssetStatusAbnormal, "abnormal"},
		{"StatusOffline", types.AssetStatusOffline, "offline"},
		{"StatusUnknown", types.AssetStatusUnknown, "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.value) != tt.want {
				t.Errorf("%s = %q, want %q", tt.name, tt.value, tt.want)
			}
		})
	}
}

func TestAssetJSONRoundTrip(t *testing.T) {
	now := time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC)
	a := &Asset{
		ID:           1,
		TenantID:     100,
		AssetType:    types.AssetTypeTask,
		AssetKey:     "task-001",
		Name:         "Test Task",
		Status:       types.AssetStatusNormal,
		Metadata:     `{"key":"value"}`,
		LastActiveAt: now,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	data, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("json.Marshal() error: %v", err)
	}

	var got Asset
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal() error: %v", err)
	}

	if got.ID != a.ID {
		t.Errorf("ID = %d, want %d", got.ID, a.ID)
	}
	if got.TenantID != 0 {
		t.Errorf("TenantID = %d, want 0: tenant_id is json:\"-\" and must not round-trip the wire", got.TenantID)
	}
	if got.AssetType != a.AssetType {
		t.Errorf("AssetType = %q, want %q", got.AssetType, a.AssetType)
	}
	if got.AssetKey != a.AssetKey {
		t.Errorf("AssetKey = %q, want %q", got.AssetKey, a.AssetKey)
	}
	if got.Name != a.Name {
		t.Errorf("Name = %q, want %q", got.Name, a.Name)
	}
	if got.Status != a.Status {
		t.Errorf("Status = %q, want %q", got.Status, a.Status)
	}
	if got.Metadata != a.Metadata {
		t.Errorf("Metadata = %q, want %q", got.Metadata, a.Metadata)
	}
	if !got.LastActiveAt.Equal(a.LastActiveAt) {
		t.Errorf("LastActiveAt = %v, want %v", got.LastActiveAt, a.LastActiveAt)
	}
	if !got.CreatedAt.Equal(a.CreatedAt) {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, a.CreatedAt)
	}
	if !got.UpdatedAt.Equal(a.UpdatedAt) {
		t.Errorf("UpdatedAt = %v, want %v", got.UpdatedAt, a.UpdatedAt)
	}
}

func TestAssetMetadataOmitEmpty(t *testing.T) {
	a := &Asset{
		ID:        2,
		TenantID:  200,
		AssetType: types.AssetTypeDevice,
		AssetKey:  "device-001",
		Name:      "Test Device",
		Status:    types.AssetStatusOffline,
		Metadata:  "",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	data, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("json.Marshal() error: %v", err)
	}

	// When Metadata is empty, the "metadata" key should be omitted due to omitempty.
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("json.Unmarshal() error: %v", err)
	}
	if _, ok := raw["metadata"]; ok {
		t.Error("metadata should be omitted when empty, but it was present in JSON output")
	}
}

func TestAssetFieldPresence(t *testing.T) {
	a := Asset{}

	// Verify all expected fields exist by setting and reading them.
	a.ID = 42
	a.TenantID = 10
	a.AssetType = types.AssetTypeHost
	a.AssetKey = "host-001"
	a.Name = "Host A"
	a.Status = types.AssetStatusAbnormal
	a.Metadata = `{"region":"us-east"}`
	a.LastActiveAt = time.Now()
	a.CreatedAt = time.Now()
	a.UpdatedAt = time.Now()

	if a.ID != 42 {
		t.Errorf("ID = %d, want 42", a.ID)
	}
	if a.TenantID != 10 {
		t.Errorf("TenantID = %d, want 10", a.TenantID)
	}
	if a.AssetType != types.AssetTypeHost {
		t.Errorf("AssetType = %q, want %q", a.AssetType, types.AssetTypeHost)
	}
	if a.AssetKey != "host-001" {
		t.Errorf("AssetKey = %q, want %q", a.AssetKey, "host-001")
	}
	if a.Name != "Host A" {
		t.Errorf("Name = %q, want %q", a.Name, "Host A")
	}
	if a.Status != types.AssetStatusAbnormal {
		t.Errorf("Status = %q, want %q", a.Status, types.AssetStatusAbnormal)
	}
	if a.Metadata != `{"region":"us-east"}` {
		t.Errorf("Metadata = %q, want %q", a.Metadata, `{"region":"us-east"}`)
	}
}
