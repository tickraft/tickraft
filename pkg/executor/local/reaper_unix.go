// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

//go:build !windows

package local

import (
	"os/exec"
	"syscall"
)

// applyProcessGroup configures the command to start in its own process group
// (Setpgid) so a timeout can kill the command together with any children it
// spawned (shells, pipelines, daemons) rather than orphaning them. This is
// the foundation of the TimeoutReaper: without a process group, a timed-out
// "sh -c 'foo | bar'" would leave foo and bar running after the parent
// shell is killed.
func applyProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProcessGroup sends SIGKILL to the entire process group led by the
// command. The negative PID addresses the group as a whole. It is a no-op
// when the process has already exited, so it is safe to invoke from
// cmd.Cancel after the process has terminated.
func killProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	// Negative PID addresses the process group. SIGKILL cannot be caught or
	// ignored, so the group is torn down deterministically.
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
