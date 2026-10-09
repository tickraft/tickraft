// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package types

import "strconv"

// ID value objects.
//
// These named types replace the bare int64 / string fields previously
// scattered across pkg/event, pkg/task, pkg/asset, pkg/telemetry,
// pkg/executor, pkg/auth, and pkg/api/handler. They carry no behavior
// beyond JSON marshaling (which is transparent because the underlying
// type is int64) and exist primarily to:
//
//   - Document intent at the type level (a field of type TenantID cannot
//     be accidentally assigned a TaskID).
//   - Provide a single canonical String()/ParseXxx() pair for cross-format
//     conversion (e.g. event payload JSON uses string IDs while domain
//     models use int64; the helpers bridge the two without sprinkling
//     strconv calls across the codebase).
//
// Migration note: existing packages are not required to switch all fields
// to these named types in a single pass. New code SHOULD use them; existing
// code MAY continue to use int64 and convert at the boundary via
// ParseXxx/String() helpers. The pkg/event payload types in particular
// retain string fields for JSON wire-format compatibility (event payloads
// encode IDs as strings) and convert via these helpers.

// TenantID identifies a tenant. The runtime is single-tenant: the value
// is always 0 in the default deployment. The field is retained on
// cross-domain value objects so that the callers can inject a
// real tenant identifier without altering the kernel type signatures.
type TenantID int64

// UserID identifies a system user.
type UserID int64

// AssetID identifies a scheduled or observed asset.
type AssetID int64

// TaskID identifies a scheduled task.
type TaskID int64

// ExecutionID identifies a single execution run of a task.
type ExecutionID int64

// RuleID identifies an alert rule.
type RuleID int64

// String returns the canonical string representation of the ID.
func (t TenantID) String() string { return strconv.FormatInt(int64(t), 10) }

// String returns the canonical string representation of the ID.
func (u UserID) String() string { return strconv.FormatInt(int64(u), 10) }

// String returns the canonical string representation of the ID.
func (a AssetID) String() string { return strconv.FormatInt(int64(a), 10) }

// String returns the canonical string representation of the ID.
func (t TaskID) String() string { return strconv.FormatInt(int64(t), 10) }

// String returns the canonical string representation of the ID.
func (e ExecutionID) String() string { return strconv.FormatInt(int64(e), 10) }

// String returns the canonical string representation of the ID.
func (r RuleID) String() string { return strconv.FormatInt(int64(r), 10) }

// ParseTenantID parses a string into a TenantID. It returns an error if
// the string is empty or not a valid int64.
func ParseTenantID(s string) (TenantID, error) {
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, err
	}
	return TenantID(v), nil
}

// ParseUserID parses a string into a UserID.
func ParseUserID(s string) (UserID, error) {
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, err
	}
	return UserID(v), nil
}

// ParseAssetID parses a string into an AssetID.
func ParseAssetID(s string) (AssetID, error) {
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, err
	}
	return AssetID(v), nil
}

// ParseTaskID parses a string into a TaskID.
func ParseTaskID(s string) (TaskID, error) {
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, err
	}
	return TaskID(v), nil
}

// ParseExecutionID parses a string into an ExecutionID.
func ParseExecutionID(s string) (ExecutionID, error) {
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, err
	}
	return ExecutionID(v), nil
}

// ParseRuleID parses a string into a RuleID.
func ParseRuleID(s string) (RuleID, error) {
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, err
	}
	return RuleID(v), nil
}
