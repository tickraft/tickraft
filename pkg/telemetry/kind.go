// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package telemetry

import "github.com/tickraft/tickraft/pkg/types"

// Kind enumerates the data categories accepted by the unified telemetry
// report endpoint. It determines the internal processing pipeline and
// the payload size limit applied by the server.
type Kind string

const (
	// KindHeartbeat carries asset heartbeat / liveness status.
	KindHeartbeat Kind = Kind(types.EventKindHeartbeat)
	// KindMetrics carries asset metric samples.
	KindMetrics Kind = "metrics"
	// KindLogs carries asset log entries.
	KindLogs Kind = "logs"
)
