// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package executor

import (
	"github.com/bytedance/sonic"
)

// Operation describes the type of operation an executor supports.
// The operation type determines the capability (Capability) required for
// execution.
type Operation int

const (
	// OpProbe represents a probe operation. It is a read operation that
	// requires the CapProbe capability.
	OpProbe Operation = iota + 1 // 1
	// OpExecute represents an execute operation. It is a write operation
	// that requires the CapWrite capability.
	OpExecute // 2
)

// String representations of the Operation values, used for JSON
// serialization and parsing.
const (
	opProbeName   = "probe"
	opExecuteName = "execute"
)

// String returns the human-readable string representation of Operation.
// OpProbe returns "probe", OpExecute returns "execute".
func (o Operation) String() string {
	switch o {
	case OpProbe:
		return opProbeName
	case OpExecute:
		return opExecuteName
	default:
		return "unknown"
	}
}

// IsRead reports whether the operation is a read operation.
// Only OpProbe returns true.
func (o Operation) IsRead() bool {
	return o == OpProbe
}

// IsWrite reports whether the operation is a write operation.
// Only OpExecute returns true.
func (o Operation) IsWrite() bool {
	return o == OpExecute
}

// requiredCap returns the capability required by this operation.
// OpProbe maps to CapProbe, OpExecute maps to CapWrite.
// Unknown operations return 0.
//
// Note: for OpExecute, callers should use isSupportedBy instead of HasCap
// for validation, because CapWrite is a composite mask and HasCap's ALL
// semantics do not apply to "any write capability" checks.
func (o Operation) requiredCap() Capability {
	switch o {
	case OpProbe:
		return CapProbe
	case OpExecute:
		return CapWrite
	default:
		return 0
	}
}

// isSupportedBy reports whether the given capabilities satisfy this
// operation's requirements.
// OpProbe requires exactly CapProbe; OpExecute requires any write capability
// (at least one of CapExec/CapMutate/CapNotify, i.e. HasWrite semantics).
func (o Operation) isSupportedBy(caps Capability) bool {
	switch o {
	case OpProbe:
		return HasCap(caps, CapProbe)
	case OpExecute:
		return HasWrite(caps)
	default:
		return false
	}
}

// MarshalJSON serializes Operation as a JSON string ("probe" or "execute").
func (o Operation) MarshalJSON() ([]byte, error) {
	return sonic.Marshal(o.String())
}

// UnmarshalJSON deserializes Operation from a JSON string (accepts "probe"
// or "execute").
func (o *Operation) UnmarshalJSON(data []byte) error {
	var s string
	if err := sonic.Unmarshal(data, &s); err != nil {
		return err
	}
	switch s {
	case opProbeName:
		*o = OpProbe
	case opExecuteName:
		*o = OpExecute
	default:
		return ErrInvalidOperation
	}
	return nil
}

// ParseOperation parses a string into an Operation.
// Accepts "probe" and "execute"; the empty string and unrecognized
// values return ErrInvalidOperation.
func ParseOperation(s string) (Operation, error) {
	switch s {
	case opExecuteName:
		return OpExecute, nil
	case opProbeName:
		return OpProbe, nil
	default:
		return 0, ErrInvalidOperation
	}
}
