// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package executor

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestOperationString(t *testing.T) {
	tests := []struct {
		name string
		op   Operation
		want string
	}{
		{"probe", OpProbe, "probe"},
		{"execute", OpExecute, "execute"},
		{"unknown zero", 0, "unknown"},
		{"unknown out of range", 99, "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.op.String(); got != tt.want {
				t.Errorf("Operation(%d).String() = %q, want %q", tt.op, got, tt.want)
			}
		})
	}
}

func TestOperationValues(t *testing.T) {
	if OpProbe != 1 {
		t.Errorf("OpProbe = %d, want 1", OpProbe)
	}
	if OpExecute != 2 {
		t.Errorf("OpExecute = %d, want 2", OpExecute)
	}
}

func TestOperationIsRead(t *testing.T) {
	if !OpProbe.IsRead() {
		t.Errorf("OpProbe.IsRead() = false, want true")
	}
	if OpExecute.IsRead() {
		t.Errorf("OpExecute.IsRead() = true, want false")
	}
}

func TestOperationIsWrite(t *testing.T) {
	if OpProbe.IsWrite() {
		t.Errorf("OpProbe.IsWrite() = true, want false")
	}
	if !OpExecute.IsWrite() {
		t.Errorf("OpExecute.IsWrite() = false, want true")
	}
}

func TestOperationRequiredCap(t *testing.T) {
	tests := []struct {
		name string
		op   Operation
		want Capability
	}{
		{"probe", OpProbe, CapProbe},
		{"execute", OpExecute, CapWrite},
		{"unknown zero", 0, 0},
		{"unknown out of range", 99, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.op.requiredCap(); got != tt.want {
				t.Errorf("Operation(%d).requiredCap() = %d, want %d", tt.op, got, tt.want)
			}
		})
	}
}

func TestOperationMarshalJSON(t *testing.T) {
	tests := []struct {
		name    string
		op      Operation
		want    string
		wantErr bool
	}{
		{"probe", OpProbe, `"probe"`, false},
		{"execute", OpExecute, `"execute"`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.op.MarshalJSON()
			if (err != nil) != tt.wantErr {
				t.Errorf("Operation.MarshalJSON() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if string(got) != tt.want {
				t.Errorf("Operation.MarshalJSON() = %q, want %q", string(got), tt.want)
			}
		})
	}
}

func TestOperationUnmarshalJSON(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		want       Operation
		wantErr    error
		wantAnyErr bool
	}{
		{"probe", `"probe"`, OpProbe, nil, false},
		{"execute", `"execute"`, OpExecute, nil, false},
		{"invalid string", `"unknown"`, 0, ErrInvalidOperation, false},
		{"invalid type number", `123`, 0, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var op Operation
			err := op.UnmarshalJSON([]byte(tt.input))
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("Operation.UnmarshalJSON() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if tt.wantAnyErr {
				if err == nil {
					t.Errorf("Operation.UnmarshalJSON() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("Operation.UnmarshalJSON() unexpected error = %v", err)
				return
			}
			if op != tt.want {
				t.Errorf("Operation.UnmarshalJSON() = %d, want %d", op, tt.want)
			}
		})
	}
}

func TestOperationJSONRoundTrip(t *testing.T) {
	tests := []Operation{OpProbe, OpExecute}
	for _, op := range tests {
		t.Run(op.String(), func(t *testing.T) {
			data, err := json.Marshal(op)
			if err != nil {
				t.Fatalf("json.Marshal() error = %v", err)
			}
			var got Operation
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("json.Unmarshal() error = %v", err)
			}
			if got != op {
				t.Errorf("round trip = %d, want %d", got, op)
			}
		})
	}
}

func TestOperationUnmarshalJSONInvalidJSON(t *testing.T) {
	var op Operation
	err := op.UnmarshalJSON([]byte(`not json`))
	if err == nil {
		t.Errorf("Operation.UnmarshalJSON() expected error for invalid JSON, got nil")
	}
}

// TestParseOperation pins the event-consumer contract: a triggered event
// must carry an explicit probe or execute operation; the runner drops
// payloads whose operation is missing or unknown.
func TestParseOperation(t *testing.T) {
	if op, err := ParseOperation("execute"); err != nil || op != OpExecute {
		t.Errorf("ParseOperation(execute) = %v (err %v), want OpExecute", op, err)
	}
	if op, err := ParseOperation("probe"); err != nil || op != OpProbe {
		t.Errorf("ParseOperation(probe) = %v (err %v), want OpProbe", op, err)
	}
	for _, s := range []string{"", "bogus"} {
		if _, err := ParseOperation(s); !errors.Is(err, ErrInvalidOperation) {
			t.Errorf("ParseOperation(%q) error = %v, want ErrInvalidOperation", s, err)
		}
	}
}
