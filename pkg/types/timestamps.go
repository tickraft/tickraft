// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package types

import "time"

// Timestamp is a kernel-level value object for instants. It wraps
// time.Time to provide a stable cross-domain type for fields that
// previously used bare time.Time and to allow future extensions (e.g.
// monotonic-aware comparison, RFC3339 normalization) without touching
// every call site.
//
// Migration note: existing packages are not required to switch all
// time.Time fields to Timestamp in a single pass. The type is provided
// for new code and for gradual migration where the added type safety is
// valuable.
type Timestamp time.Time

// Time returns the underlying time.Time.
func (t Timestamp) Time() time.Time { return time.Time(t) }

// String returns the RFC3339 representation of the timestamp.
func (t Timestamp) String() string { return time.Time(t).Format(time.RFC3339) }
