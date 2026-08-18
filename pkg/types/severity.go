// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package types

// Severity is the cross-domain severity level for alerts, log entries,
// and other observable events.
//
// Previously this concept was expressed as bare string literals
// ("info", "warning", "critical", "error") scattered across 8+ packages
// (pkg/model, pkg/api/handler, pkg/event/payload, pkg/telemetry,
// pkg/prism/alert/template, pkg/i18n, etc.). Centralizing the type and its
// constants here eliminates the typo risk and gives the compiler a way
// to detect mismatched severity values at the call site.
//
// Migration note: existing packages are not required to switch all
// string fields to Severity in a single pass. The type is provided for
// new code and for gradual migration. Until migration is complete, callers
// MAY compare a Severity value to its underlying string via string(sev)
// or Severity("warning") at the boundary.
type Severity string

const (
	// SeverityInfo indicates an informational message that does not
	// require action.
	SeverityInfo Severity = "info"
	// SeverityWarning indicates a potentially harmful situation that
	// may warrant attention.
	SeverityWarning Severity = "warning"
	// SeverityCritical indicates a critical condition that requires
	// immediate attention.
	SeverityCritical Severity = "critical"
	// SeverityError indicates an error condition.
	SeverityError Severity = "error"
	// SeverityDebug indicates a verbose diagnostic message. It ranks below
	// SeverityInfo and is used by log ingestion and level normalization.
	// It is not an alerting level: Severity.IsValid deliberately excludes
	// it so alert validation keeps rejecting debug as a record severity.
	SeverityDebug Severity = "debug"
)

// IsValid reports whether s is one of the recognized Severity constants.
// SeverityDebug is deliberately excluded: debug is a log level, not an
// alerting level, so alert validation keeps rejecting it.
func (s Severity) IsValid() bool {
	switch s {
	case SeverityInfo, SeverityWarning, SeverityCritical, SeverityError:
		return true
	default:
		return false
	}
}
