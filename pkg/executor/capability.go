// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package executor

// Capability uses a bitmask to describe the capabilities of an executor.
// An executor may possess multiple capabilities simultaneously, combined via bitwise OR.
type Capability uint64

const (
	// CapProbe indicates active probing capability (ICMP/TCP/HTTP probing, etc.), a read operation.
	CapProbe Capability = 1 << iota // 1
	// CapExec indicates command execution capability (SSH/Local command execution, etc.), a write operation.
	CapExec // 2
	// CapMutate indicates data mutation capability (MySQL/Redis write operations, etc.), a write operation.
	CapMutate // 4
	// CapNotify indicates notification sending capability (Webhook/email notifications, etc.), a write operation.
	CapNotify // 8
	// CapWrite aggregates all write capabilities (CapExec | CapMutate | CapNotify).
	CapWrite = CapExec | CapMutate | CapNotify // 14
)

// HasCap reports whether capabilities contains the specified capability c.
// Returns true when the bitwise AND of capabilities and c equals c.
func HasCap(capabilities, c Capability) bool {
	return capabilities&c == c
}

// HasWrite reports whether capabilities contains any write capability
// (i.e., at least one of CapExec, CapMutate, or CapNotify).
func HasWrite(capabilities Capability) bool {
	return capabilities&CapWrite != 0
}

// IsDualMode reports whether capabilities includes both probing capability (CapProbe)
// and any write capability (CapWrite). A dual-mode executor can both probe and mutate data.
func IsDualMode(capabilities Capability) bool {
	return capabilities&CapProbe != 0 && capabilities&CapWrite != 0
}
