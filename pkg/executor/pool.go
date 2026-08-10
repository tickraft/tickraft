// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package executor

import (
	"sync"

	"github.com/tickraft/tickraft/pkg/types"
)

// resultPool reuses *Result objects to reduce GC pressure in high-throughput
// execution scenarios. Executors obtain results via AcquireResult and the
// Runner returns them via ReleaseResult after consuming the result in finish.
var resultPool = sync.Pool{
	New: func() any {
		return &Result{Metrics: make(map[string]float64, 4)}
	},
}

// AcquireResult returns a *Result from the pool, reset to a clean state.
// Callers must populate the returned result's fields before returning it
// from Executor.Execute.
func AcquireResult() *Result {
	r := resultPool.Get().(*Result) //nolint:errcheck // pool always yields *Result
	r.reset()
	return r
}

// ReleaseResult returns a *Result to the pool for reuse. The caller must not
// reference the result after calling this function. A nil result is silently
// ignored.
func ReleaseResult(r *Result) {
	if r == nil {
		return
	}
	resultPool.Put(r)
}

// reset clears all fields of the result so it can be safely reused without
// leaking data from a previous execution. The Metrics map is cleared in-place
// to retain its allocated capacity for future use.
func (r *Result) reset() {
	r.Status = types.AssetStatus("")
	r.StatusCode = 0
	r.Body = ""
	r.ErrorMsg = ""
	r.Duration = 0
	for k := range r.Metrics {
		delete(r.Metrics, k)
	}
}
