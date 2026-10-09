// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package pagination

// Clamp normalizes offset-mode paging parameters: page is clamped to at
// least 1 and size is normalized by [ClampSize].
func Clamp(page, size int) (clampedPage, clampedSize int) {
	return max(page, 1), ClampSize(size)
}

// Window returns the [start, end) index window for the given page/size
// over an in-memory collection of total items. start may be >= total to
// signal an empty result; callers must guard against that before slicing.
func Window(page, size, total int) (start, end int) {
	start = (page - 1) * size
	start = max(start, 0)
	start = min(start, total)
	end = start + size
	end = min(end, total)
	return start, end
}
