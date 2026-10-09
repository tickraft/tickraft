// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

//go:build windows

package local

import "os/exec"

// applyProcessGroup is a no-op on Windows: CommandContext already terminates
// the direct process on context cancellation. Job-object-based group kill is
// out of scope for the local executor.
func applyProcessGroup(cmd *exec.Cmd) {}

// killProcessGroup is a no-op on Windows.
func killProcessGroup(cmd *exec.Cmd) error { return nil }
