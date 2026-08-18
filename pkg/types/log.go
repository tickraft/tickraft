// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package types

// LogLevel is the cross-domain log level for system logging configuration
// and runtime log emission.
//
// Previously this concept was expressed as bare string literals
// ("debug", "info", "warn", "error") in pkg/config/validate.go,
// pkg/telemetry/model.go, and pkg/api/handler/types.go. Centralizing
// the type and its constants here eliminates the typo risk.
type LogLevel string

const (
	// LogLevelDebug enables debug-level logging.
	LogLevelDebug LogLevel = "debug"
	// LogLevelInfo enables info-level logging.
	LogLevelInfo LogLevel = "info"
	// LogLevelWarn enables warn-level logging.
	LogLevelWarn LogLevel = "warn"
	// LogLevelError enables error-level logging.
	LogLevelError LogLevel = "error"
)

// IsValid reports whether l is one of the recognized LogLevel constants.
func (l LogLevel) IsValid() bool {
	switch l {
	case LogLevelDebug, LogLevelInfo, LogLevelWarn, LogLevelError:
		return true
	}
	return false
}
