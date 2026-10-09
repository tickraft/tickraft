// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package executor

import "context"

// Executor is the SPI interface that all executors must implement.
// Implementations declare their capabilities via Capabilities() and the
// caller validates them through LookupWithOp.
// Implementations must be safe for concurrent use.
type Executor interface {
	// Name returns the unique identifier of the executor (e.g. "icmp", "ssh", "http").
	Name() string

	// Capabilities returns the capability bitmask supported by the executor.
	Capabilities() Capability

	// Execute performs the probe or execution operation as specified by
	// req.Operation.
	Execute(ctx context.Context, req ExecutionRequest) (*Result, error)
}
