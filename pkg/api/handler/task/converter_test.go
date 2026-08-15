// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package task

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/tickraft/tickraft/pkg/task"
)

// configJSONEqual reports whether two map[string]any values are equivalent
// after JSON marshaling. It is used to eliminate numeric type differences
// (JSON unmarshal produces float64 while code construction may use int64).
func configJSONEqual(t *testing.T, a, b map[string]any) bool {
	t.Helper()
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	if string(ja) != string(jb) {
		t.Errorf("config not equal:\n  got  %s\n  want %s", ja, jb)
		return false
	}
	return true
}

// --- nil input tests ---

func TestDomainTaskToHandler_Nil(t *testing.T) {
	if got := DomainTaskToHandler(nil); got != nil {
		t.Errorf("DomainTaskToHandler(nil) = %v, want nil", got)
	}
}

func TestHandlerToDomainTask_Nil(t *testing.T) {
	if got := HandlerToDomainTask(nil); got != nil {
		t.Errorf("HandlerToDomainTask(nil) = %v, want nil", got)
	}
}

// --- HandlerToDomainTask forward conversion tests ---

func TestHandlerToDomainTask_FullFields(t *testing.T) {
	created := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	updated := time.Date(2025, 1, 2, 3, 5, 0, 0, time.UTC)
	src := &Task{
		ID:          42,
		Name:        "probe-http",
		Description: "probe example.com",
		Executor:    "http",
		Schedule:    "*/5 * * * *",
		Enabled:     true,
		Config: map[string]any{
			"url":              "https://example.com",
			"method":           "GET",
			configKeyTenantID:  float64(7),
			configKeyAssetID:   float64(9),
			configKeyTimeout:   "45s",
			configKeyPriority:  float64(3),
			configKeyDependsOn: float64(11),
		},
		CreatedAt: created,
		UpdatedAt: updated,
	}

	got := HandlerToDomainTask(src)
	if got == nil {
		t.Fatal("HandlerToDomainTask returned nil for non-nil input")
	}

	if got.ID != 42 {
		t.Errorf("ID = %d, want 42", got.ID)
	}
	if got.ExecutorName != "http" {
		t.Errorf("ExecutorName = %q, want %q", got.ExecutorName, "http")
	}
	if got.TenantID != 7 {
		t.Errorf("TenantID = %d, want 7", got.TenantID)
	}
	if got.AssetID != 9 {
		t.Errorf("AssetID = %d, want 9", got.AssetID)
	}
	if got.Timeout != 45*time.Second {
		t.Errorf("Timeout = %v, want 45s", got.Timeout)
	}
	if got.Priority != 3 {
		t.Errorf("Priority = %d, want 3", got.Priority)
	}
	if got.DependsOn != 11 {
		t.Errorf("DependsOn = %d, want 11", got.DependsOn)
	}

	// Config should contain only pure executor configuration
	// (task-domain-only fields stripped).
	var execCfg map[string]any
	if err := json.Unmarshal([]byte(got.Config), &execCfg); err != nil {
		t.Fatalf("Config is not valid JSON: %v", err)
	}
	wantExecCfg := map[string]any{"url": "https://example.com", "method": "GET"}
	configJSONEqual(t, execCfg, wantExecCfg)

	// Metadata should contain handler-only fields.
	if got.Metadata[metaKeyName] != "probe-http" {
		t.Errorf("Metadata[%q] = %q, want %q", metaKeyName, got.Metadata[metaKeyName], "probe-http")
	}
	if got.Metadata[metaKeyDescription] != "probe example.com" {
		t.Errorf("Metadata[%q] = %q, want %q", metaKeyDescription, got.Metadata[metaKeyDescription], "probe example.com")
	}
	if got.Metadata[metaKeySchedule] != "*/5 * * * *" {
		t.Errorf("Metadata[%q] = %q, want %q", metaKeySchedule, got.Metadata[metaKeySchedule], "*/5 * * * *")
	}
	if got.Metadata[metaKeyEnabled] != "true" {
		t.Errorf("Metadata[%q] = %q, want %q", metaKeyEnabled, got.Metadata[metaKeyEnabled], "true")
	}
	if got.Metadata[metaKeyCreatedAt] != created.Format(time.RFC3339Nano) {
		t.Errorf("Metadata[%q] = %q, want %q", metaKeyCreatedAt, got.Metadata[metaKeyCreatedAt], created.Format(time.RFC3339Nano))
	}
	if got.Metadata[metaKeyUpdatedAt] != updated.Format(time.RFC3339Nano) {
		t.Errorf("Metadata[%q] = %q, want %q", metaKeyUpdatedAt, got.Metadata[metaKeyUpdatedAt], updated.Format(time.RFC3339Nano))
	}
}

func TestHandlerToDomainTask_EmptyConfig(t *testing.T) {
	src := &Task{
		ID:       1,
		Name:     "bare",
		Executor: "tcp",
	}
	got := HandlerToDomainTask(src)
	if got == nil {
		t.Fatal("HandlerToDomainTask returned nil")
	}
	if got.Config != "" {
		t.Errorf("Config = %q, want empty", got.Config)
	}
	if got.TenantID != 0 || got.AssetID != 0 || got.Timeout != 0 ||
		got.Priority != 0 || got.DependsOn != 0 {
		t.Errorf("task domain fields not zero: %+v", got)
	}
	// Metadata should still be initialized and populated with
	// handler-only fields.
	if got.Metadata == nil {
		t.Fatal("Metadata is nil, want initialized map")
	}
	if got.Metadata[metaKeyName] != "bare" {
		t.Errorf("Metadata[%q] = %q, want %q", metaKeyName, got.Metadata[metaKeyName], "bare")
	}
	if got.Metadata[metaKeyEnabled] != "false" {
		t.Errorf("Metadata[%q] = %q, want %q", metaKeyEnabled, got.Metadata[metaKeyEnabled], "false")
	}
}

func TestHandlerToDomainTask_ZeroTimestamps(t *testing.T) {
	src := &Task{ID: 5, Name: "no-ts", Executor: "script"}
	got := HandlerToDomainTask(src)
	if _, ok := got.Metadata[metaKeyCreatedAt]; ok {
		t.Errorf("Metadata should not contain %q for zero CreatedAt", metaKeyCreatedAt)
	}
	if _, ok := got.Metadata[metaKeyUpdatedAt]; ok {
		t.Errorf("Metadata should not contain %q for zero UpdatedAt", metaKeyUpdatedAt)
	}
}

// --- DomainTaskToHandler reverse conversion tests ---

func TestDomainTaskToHandler_FullFields(t *testing.T) {
	created := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	updated := time.Date(2025, 1, 2, 3, 5, 0, 0, time.UTC)
	src := &task.Task{
		ID:           100,
		TenantID:     8,
		AssetID:      16,
		ExecutorName: "tcp",
		Config:       `{"host":"127.0.0.1","port":6379}`,
		Timeout:      30 * time.Second,
		Priority:     5,
		DependsOn:    3,
		Metadata: map[string]string{
			metaKeyName:        "tcp-probe",
			metaKeyDescription: "probe tcp-service",
			metaKeySchedule:    "*/1 * * * *",
			metaKeyEnabled:     "true",
			metaKeyCreatedAt:   created.Format(time.RFC3339Nano),
			metaKeyUpdatedAt:   updated.Format(time.RFC3339Nano),
		},
	}

	got := DomainTaskToHandler(src)
	if got == nil {
		t.Fatal("DomainTaskToHandler returned nil for non-nil input")
	}

	if got.ID != 100 {
		t.Errorf("ID = %d, want 100", got.ID)
	}
	if got.Executor != "tcp" {
		t.Errorf("Executor = %q, want %q", got.Executor, "tcp")
	}
	if got.Name != "tcp-probe" {
		t.Errorf("Name = %q, want %q", got.Name, "tcp-probe")
	}
	if got.Description != "probe tcp-service" {
		t.Errorf("Description = %q, want %q", got.Description, "probe tcp-service")
	}
	if got.Schedule != "*/1 * * * *" {
		t.Errorf("Schedule = %q, want %q", got.Schedule, "*/1 * * * *")
	}
	if !got.Enabled {
		t.Errorf("Enabled = false, want true")
	}
	if !got.CreatedAt.Equal(created) {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, created)
	}
	if !got.UpdatedAt.Equal(updated) {
		t.Errorf("UpdatedAt = %v, want %v", got.UpdatedAt, updated)
	}

	// Config should contain both executor configuration and
	// task-domain-only fields.
	wantCfg := map[string]any{
		"host":             "127.0.0.1",
		"port":             float64(6379),
		configKeyTenantID:  float64(8),
		configKeyAssetID:   float64(16),
		configKeyTimeout:   float64(30), // 30s expressed as seconds
		configKeyPriority:  float64(5),
		configKeyDependsOn: float64(3),
	}
	configJSONEqual(t, got.Config, wantCfg)
}

func TestDomainTaskToHandler_MinimalFields(t *testing.T) {
	src := &task.Task{
		ID:           1,
		ExecutorName: "http",
	}
	got := DomainTaskToHandler(src)
	if got == nil {
		t.Fatal("DomainTaskToHandler returned nil")
	}
	if got.ID != 1 {
		t.Errorf("ID = %d, want 1", got.ID)
	}
	if got.Executor != "http" {
		t.Errorf("Executor = %q, want %q", got.Executor, "http")
	}
	if got.Name != "" {
		t.Errorf("Name = %q, want empty", got.Name)
	}
	if got.Enabled {
		t.Errorf("Enabled = true, want false (default)")
	}
	if got.Config != nil {
		t.Errorf("Config = %v, want nil (no task domain fields, no Config)", got.Config)
	}
}

func TestDomainTaskToHandler_InvalidConfig(t *testing.T) {
	src := &task.Task{
		ID:           2,
		ExecutorName: "script",
		Config:       "{not-json",
	}
	got := DomainTaskToHandler(src)
	if got == nil {
		t.Fatal("DomainTaskToHandler returned nil")
	}
	// On unmarshal failure Config should be nil and not carry
	// corrupted data.
	if got.Config != nil {
		t.Errorf("Config = %v, want nil for invalid JSON", got.Config)
	}
}

func TestDomainTaskToHandler_EmptyMetadata(t *testing.T) {
	src := &task.Task{
		ID:           3,
		ExecutorName: "http",
		Metadata:     map[string]string{},
	}
	got := DomainTaskToHandler(src)
	if got.Name != "" {
		t.Errorf("Name = %q, want empty", got.Name)
	}
	if got.Enabled {
		t.Errorf("Enabled = true, want false")
	}
}

// --- round-trip consistency tests ---

func TestRoundTrip_HandlerToDomainToHandler(t *testing.T) {
	created := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)
	updated := time.Date(2025, 6, 2, 12, 0, 0, 0, time.UTC)
	original := &Task{
		ID:          77,
		Name:        "round-trip",
		Description: "verify round trip",
		Executor:    "http",
		Schedule:    "0 */2 * * *",
		Enabled:     true,
		Config: map[string]any{
			"url":              "https://api.example.com/health",
			"method":           "POST",
			"headers":          map[string]any{"X-Tickraft-Request-Id": "abc"},
			configKeyTenantID:  float64(12),
			configKeyAssetID:   float64(34),
			configKeyTimeout:   float64(60), // 60s expressed as seconds, matching the post-round-trip format
			configKeyPriority:  float64(2),
			configKeyDependsOn: float64(56),
		},
		CreatedAt: created,
		UpdatedAt: updated,
	}

	// handler -> domain -> handler
	domainTask := HandlerToDomainTask(original)
	roundTripped := DomainTaskToHandler(domainTask)
	if roundTripped == nil {
		t.Fatal("round-trip returned nil")
	}

	// Core scalar fields should remain consistent.
	if roundTripped.ID != original.ID {
		t.Errorf("ID = %d, want %d", roundTripped.ID, original.ID)
	}
	if roundTripped.Name != original.Name {
		t.Errorf("Name = %q, want %q", roundTripped.Name, original.Name)
	}
	if roundTripped.Description != original.Description {
		t.Errorf("Description = %q, want %q", roundTripped.Description, original.Description)
	}
	if roundTripped.Executor != original.Executor {
		t.Errorf("Executor = %q, want %q", roundTripped.Executor, original.Executor)
	}
	if roundTripped.Schedule != original.Schedule {
		t.Errorf("Schedule = %q, want %q", roundTripped.Schedule, original.Schedule)
	}
	if roundTripped.Enabled != original.Enabled {
		t.Errorf("Enabled = %v, want %v", roundTripped.Enabled, original.Enabled)
	}
	if !roundTripped.CreatedAt.Equal(original.CreatedAt) {
		t.Errorf("CreatedAt = %v, want %v", roundTripped.CreatedAt, original.CreatedAt)
	}
	if !roundTripped.UpdatedAt.Equal(original.UpdatedAt) {
		t.Errorf("UpdatedAt = %v, want %v", roundTripped.UpdatedAt, original.UpdatedAt)
	}

	// Compare Config via JSON marshaling to eliminate numeric type
	// differences.
	configJSONEqual(t, roundTripped.Config, original.Config)
}

func TestRoundTrip_DomainToHandlerToDomain(t *testing.T) {
	created := time.Date(2025, 3, 15, 8, 30, 0, 0, time.UTC)
	updated := time.Date(2025, 3, 16, 8, 30, 0, 0, time.UTC)
	original := &task.Task{
		ID:           200,
		TenantID:     50,
		AssetID:      60,
		ExecutorName: "script",
		Config:       `{"command":"ls -la","cwd":"/tmp"}`,
		Timeout:      90 * time.Second,
		Priority:     7,
		DependsOn:    30,
		Metadata: map[string]string{
			metaKeyName:        "script-task",
			metaKeyDescription: "run script",
			metaKeySchedule:    "@every 1h",
			metaKeyEnabled:     "false",
			metaKeyCreatedAt:   created.Format(time.RFC3339Nano),
			metaKeyUpdatedAt:   updated.Format(time.RFC3339Nano),
		},
	}

	// domain -> handler -> domain
	h := DomainTaskToHandler(original)
	roundTripped := HandlerToDomainTask(h)
	if roundTripped == nil {
		t.Fatal("round-trip returned nil")
	}

	if roundTripped.ID != original.ID {
		t.Errorf("ID = %d, want %d", roundTripped.ID, original.ID)
	}
	if roundTripped.TenantID != original.TenantID {
		t.Errorf("TenantID = %d, want %d", roundTripped.TenantID, original.TenantID)
	}
	if roundTripped.AssetID != original.AssetID {
		t.Errorf("AssetID = %d, want %d", roundTripped.AssetID, original.AssetID)
	}
	if roundTripped.ExecutorName != original.ExecutorName {
		t.Errorf("ExecutorName = %q, want %q", roundTripped.ExecutorName, original.ExecutorName)
	}
	if roundTripped.Timeout != original.Timeout {
		t.Errorf("Timeout = %v, want %v", roundTripped.Timeout, original.Timeout)
	}
	if roundTripped.Priority != original.Priority {
		t.Errorf("Priority = %d, want %d", roundTripped.Priority, original.Priority)
	}
	if roundTripped.DependsOn != original.DependsOn {
		t.Errorf("DependsOn = %d, want %d", roundTripped.DependsOn, original.DependsOn)
	}

	// Compare Config via JSON.
	var origCfg, rtCfg map[string]any
	if err := json.Unmarshal([]byte(original.Config), &origCfg); err != nil {
		t.Fatalf("original Config invalid: %v", err)
	}
	if err := json.Unmarshal([]byte(roundTripped.Config), &rtCfg); err != nil {
		t.Fatalf("round-tripped Config invalid: %v", err)
	}
	configJSONEqual(t, rtCfg, origCfg)

	// Metadata should match.
	if !reflect.DeepEqual(roundTripped.Metadata, original.Metadata) {
		t.Errorf("Metadata = %v, want %v", roundTripped.Metadata, original.Metadata)
	}
}

// --- helper function tests ---

func TestParseInt64FromAny(t *testing.T) {
	cases := []struct {
		in   any
		want int64
	}{
		{nil, 0},
		{float64(42), 42},
		{int(42), 42},
		{int64(42), 42},
		{"42", 42},
		{"not-a-number", 0},
		{true, 0}, // unsupported types return 0
	}
	for _, c := range cases {
		got := parseInt64FromAny(c.in)
		if got != c.want {
			t.Errorf("parseInt64FromAny(%v) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestParseIntFromAny(t *testing.T) {
	cases := []struct {
		in   any
		want int
	}{
		{nil, 0},
		{float64(7), 7},
		{int(7), 7},
		{int64(7), 7},
		{"7", 7},
		{"bad", 0},
	}
	for _, c := range cases {
		got := parseIntFromAny(c.in)
		if got != c.want {
			t.Errorf("parseIntFromAny(%v) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestParseDurationFromAny(t *testing.T) {
	cases := []struct {
		in   any
		want time.Duration
	}{
		{nil, 0},
		{"", 0},
		{"30s", 30 * time.Second},
		{"2m", 2 * time.Minute},
		{"60", 60 * time.Second}, // pure numeric string interpreted as seconds
		{float64(60), 60 * time.Second},
		{int(60), 60 * time.Second},
		{int64(60), 60 * time.Second},
		{"invalid", 0},
	}
	for _, c := range cases {
		got := parseDurationFromAny(c.in)
		if got != c.want {
			t.Errorf("parseDurationFromAny(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestIsDomainConfigKey(t *testing.T) {
	for _, k := range []string{
		configKeyTenantID,
		configKeyAssetID,
		configKeyTimeout,
		configKeyPriority,
		configKeyDependsOn,
	} {
		if !isDomainConfigKey(k) {
			t.Errorf("isDomainConfigKey(%q) = false, want true", k)
		}
	}
	for _, k := range []string{"url", "method", "host", "port", ""} {
		if isDomainConfigKey(k) {
			t.Errorf("isDomainConfigKey(%q) = true, want false", k)
		}
	}
}
