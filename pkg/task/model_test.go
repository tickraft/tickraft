// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package task

import (
	"testing"
	"time"

	"github.com/tickraft/tickraft/pkg/types"
)

func TestScheduleTypeConstants(t *testing.T) {
	tests := []struct {
		typ ScheduleType
		str string
	}{
		{ScheduleTypeCron, "cron"},
		{ScheduleTypeInterval, "interval"},
		{ScheduleTypeEvent, "event"},
	}
	for _, tt := range tests {
		if string(tt.typ) != tt.str {
			t.Errorf("ScheduleType %v = %q, want %q", tt.typ, string(tt.typ), tt.str)
		}
	}
}

func TestTaskFields(t *testing.T) {
	tk := Task{
		ID:             1,
		TenantID:       100,
		AssetID:        200,
		Name:           "probe-example",
		ExecutorType:   "webhook",
		Schedule:       "*/5 * * * *",
		TimeoutSeconds: 30,
		Priority:       5,
		DependsOn:      0,
		Metadata:       map[string]string{"key": "value"},
	}
	if tk.ID != 1 {
		t.Errorf("ID = %d, want 1", tk.ID)
	}
	if tk.TenantID != 100 {
		t.Errorf("TenantID = %d, want 100", tk.TenantID)
	}
	if tk.AssetID != 200 {
		t.Errorf("AssetID = %d, want 200", tk.AssetID)
	}
	if tk.ExecutorType != "webhook" {
		t.Errorf("ExecutorType = %q, want %q", tk.ExecutorType, "webhook")
	}
	if tk.Timeout() != 30*time.Second {
		t.Errorf("Timeout() = %v, want 30s", tk.Timeout())
	}
	if tk.Priority != 5 {
		t.Errorf("Priority = %d, want 5", tk.Priority)
	}
	if tk.DependsOn != 0 {
		t.Errorf("DependsOn = %d, want 0", tk.DependsOn)
	}
	if tk.Metadata["key"] != "value" {
		t.Errorf("Metadata[key] = %q, want %q", tk.Metadata["key"], "value")
	}
}

func TestTaskRetryAccessors(t *testing.T) {
	tk := Task{MaxRetries: 3, RetryIntervalSeconds: 5}
	if tk.RetryInterval() != 5*time.Second {
		t.Errorf("RetryInterval() = %v, want 5s", tk.RetryInterval())
	}
	// Non-positive values yield zero so consumers apply their own defaults.
	zero := Task{}
	if zero.Timeout() != 0 || zero.RetryInterval() != 0 {
		t.Errorf("zero task accessors = %v/%v, want 0/0", zero.Timeout(), zero.RetryInterval())
	}
	negative := Task{TimeoutSeconds: -1, RetryIntervalSeconds: -1}
	if negative.Timeout() != 0 || negative.RetryInterval() != 0 {
		t.Errorf("negative task accessors = %v/%v, want 0/0", negative.Timeout(), negative.RetryInterval())
	}
}

func TestExecutionStatusFromAsset(t *testing.T) {
	tests := []struct {
		asset types.AssetStatus
		want  string
	}{
		{types.AssetStatusNormal, StatusSuccess},
		{types.AssetStatusAbnormal, StatusFailed},
		{types.AssetStatusUnknown, StatusUnknown},
		{types.AssetStatusOffline, StatusUnknown},
		{types.AssetStatus("garbage"), StatusUnknown},
	}
	for _, tt := range tests {
		if got := ExecutionStatusFromAsset(tt.asset); got != tt.want {
			t.Errorf("ExecutionStatusFromAsset(%q) = %q, want %q", tt.asset, got, tt.want)
		}
	}
}

func TestClassifySchedule(t *testing.T) {
	tests := []struct {
		schedule     string
		wantType     ScheduleType
		wantInterval time.Duration
		wantErr      bool
	}{
		{"", ScheduleTypeEvent, 0, false},
		{"30s", ScheduleTypeInterval, 30 * time.Second, false},
		{"1h30m", ScheduleTypeInterval, 90 * time.Minute, false},
		{"0s", ScheduleTypeInterval, 0, true},
		{"-5m", ScheduleTypeInterval, -5 * time.Minute, true},
		{"*/5 * * * *", ScheduleTypeCron, 0, false},
		{"0 0 1 1 *", ScheduleTypeCron, 0, false},
		{"not a cron", ScheduleTypeCron, 0, true},
	}
	for _, tt := range tests {
		gotType, gotInterval, err := ClassifySchedule(tt.schedule)
		if tt.wantErr {
			if err == nil {
				t.Errorf("ClassifySchedule(%q) expected error, got type=%v", tt.schedule, gotType)
			}
			continue
		}
		if err != nil {
			t.Errorf("ClassifySchedule(%q) unexpected error: %v", tt.schedule, err)
			continue
		}
		if gotType != tt.wantType || gotInterval != tt.wantInterval {
			t.Errorf("ClassifySchedule(%q) = (%v, %v), want (%v, %v)",
				tt.schedule, gotType, gotInterval, tt.wantType, tt.wantInterval)
		}
	}
}
