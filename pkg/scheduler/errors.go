// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package scheduler

import "errors"

var (
	// ErrSchedulerStopped is returned when operating on a stopped engine.
	ErrSchedulerStopped = errors.New("scheduler: already stopped")
	// ErrInvalidCronExpr is returned when the cron expression is invalid.
	ErrInvalidCronExpr = errors.New("scheduler: invalid cron expression")
)
