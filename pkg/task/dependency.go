// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package task

import (
	"sync"

	"github.com/tickraft/tickraft/pkg/types"
)

// dependencyChecker tracks task execution results and determines whether
// a dependent task is allowed to execute based on its upstream status.
type dependencyChecker struct {
	mu       sync.RWMutex
	statuses map[int64]types.AssetStatus
}

// newDependencyChecker creates an empty dependencyChecker.
func newDependencyChecker() *dependencyChecker {
	return &dependencyChecker{
		statuses: make(map[int64]types.AssetStatus),
	}
}

// CanExecute returns true if the task identified by taskID is allowed to run.
// A task with DependsOn == 0 has no dependency and is always allowed.
// A task with a dependency is allowed only if its upstream completed with
// StatusNormal.
func (d *dependencyChecker) CanExecute(taskID int64) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()

	status, ok := d.statuses[taskID]
	if !ok {
		// No recorded status means the dependency has not completed yet.
		return false
	}
	return status == types.AssetStatusNormal
}

// UpdateStatus records the execution result for a task.
func (d *dependencyChecker) UpdateStatus(taskID int64, status types.AssetStatus) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.statuses[taskID] = status
}

// Reset clears the recorded status for a task, typically before a new
// execution cycle begins.
func (d *dependencyChecker) Reset(taskID int64) {
	d.mu.Lock()
	defer d.mu.Unlock()

	delete(d.statuses, taskID)
}
