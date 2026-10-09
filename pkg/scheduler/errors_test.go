// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package scheduler

import "testing"

func TestSentinelErrors(t *testing.T) {
	errs := []error{
		ErrSchedulerStopped,
		ErrInvalidCronExpr,
	}
	for _, err := range errs {
		if err == nil {
			t.Errorf("sentinel error must not be nil")
		}
	}
}
