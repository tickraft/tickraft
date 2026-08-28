// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package executor

import "strings"

// Dispatch variable placeholder embedded anywhere in the executor config
// string (URL, body, headers, command arguments — wherever the config JSON
// carries them). The runner expands it at dispatch time, so the remote
// receiver learns the report credential without any manual ID copying:
//
//	{{task_ref}}  the task_ref dispatch credential of this run (the run
//	              handle; renderable in both modes)
//
// For webhook/http executors the X-Tickraft-Task-Ref header carries the
// same value implicitly; the variable exists so every other executor
// (local scripts, custom configs) reaches parity: embed once at creation,
// every fire carries a fresh value. The remote echoes it back verbatim in
// the task_ref field of the telemetry report.
const dispatchVarTaskRef = "{{task_ref}}"

// expandDispatchVars rewrites the dispatch variable placeholders in an
// executor config string. Plain textual substitution on purpose — no
// template engine, no escaping rules — so placeholders can live anywhere
// in the JSON payload. Configs without placeholders pass through unchanged.
func expandDispatchVars(config, taskRef string) string {
	if !strings.Contains(config, "{{") {
		return config
	}
	return strings.ReplaceAll(config, dispatchVarTaskRef, taskRef)
}
