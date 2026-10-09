// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package httputil holds the HTTP primitives shared by the http and webhook
// executors: response judgment, body truncation limits, and the defensive
// client timeout. Keeping them here prevents the two executors from drifting
// apart again. The package is internal to pkg/executor on purpose — these are
// implementation details of the executor family, not kernel SPI.
package httputil

import (
	"io"
	"net/http"
	"time"

	"github.com/tickraft/tickraft/pkg/types"
)

// ProbeBodyLimit caps the response body retained by probe executions: probes
// only need a snippet for status reporting and judgment expressions.
const ProbeBodyLimit = 4 * 1024 // 4 KiB

// ActionBodyLimit caps the response body retained by action executions:
// callbacks may carry larger payloads worth inspecting in execution logs.
const ActionBodyLimit = 64 * 1024 // 64 KiB

// HardTimeout bounds a single client request independently of the caller's
// context. Timeout control belongs to the caller's context (the runner's
// lifecycle or the remediation operator wraps every Execute call); this
// ceiling only guards a direct Execute call made with an unbounded context.
// httpx.Config.Timeout cannot express "no client timeout" (zero falls back
// to its 30s default), so an explicit generous ceiling is used instead.
const HardTimeout = 10 * time.Minute

// ResponseStatus maps an HTTP status code to an asset status. An explicit
// expect (>0) requires an exact match — a 2xx response can be judged
// abnormal and a non-2xx response normal. Otherwise any 2xx is normal.
func ResponseStatus(expect, code int) types.AssetStatus {
	if expect > 0 {
		if code == expect {
			return types.AssetStatusNormal
		}
		return types.AssetStatusAbnormal
	}
	if code >= 200 && code < 300 {
		return types.AssetStatusNormal
	}
	return types.AssetStatusAbnormal
}

// ReadBody reads at most limit bytes from r. Read errors are swallowed: a
// truncated body still reports status, and the caller cannot act on a
// mid-body transport failure beyond what the status code already says.
func ReadBody(r io.Reader, limit int) []byte {
	body, _ := io.ReadAll(io.LimitReader(r, int64(limit)))
	return body
}

// HeaderTaskRef is the dispatch credential stamped on Mode A (ReportStatus)
// outbound requests: the task_ref the remote echoes back verbatim in the
// telemetry report to pin the exact execution row. Reporters that cannot
// capture it may report by task number instead through the same endpoint.
const HeaderTaskRef = "X-Tickraft-Task-Ref"

// SetDispatchHeaders stamps the dispatch credential header on h when the
// request belongs to a Mode A task with an opened execution row. It is a
// no-op otherwise (Mode B tasks carry no dispatch identity).
func SetDispatchHeaders(h http.Header, taskRef string) {
	if taskRef == "" {
		return
	}
	h.Set(HeaderTaskRef, taskRef)
}
