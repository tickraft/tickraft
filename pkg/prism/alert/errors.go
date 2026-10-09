// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package alert

import "errors"

var (
	// ErrRuleNotFound is returned when a rule cannot be located by its ID.
	ErrRuleNotFound = errors.New("rule: not found")
	// ErrRuleCompileFailed is returned when a rule expression fails to
	// compile. The wrapped cause carries the expr-lang diagnostic.
	ErrRuleCompileFailed = errors.New("rule: compile failed")
	// ErrRuleDuplicate is returned when a rule with the same tenant and name already exists.
	ErrRuleDuplicate = errors.New("rule: duplicate")
)
