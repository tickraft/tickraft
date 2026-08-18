// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// gorules defines custom ruleguard rules enforced via gocritic.
//
// This file lives under a dot-prefixed directory so the Go toolchain
// ignores it; it is only parsed by the ruleguard engine at lint time.
package gorules

import "github.com/quasilyte/go-ruleguard/dsl"

// builtinMinMax flags if-statements that reimplement the Go 1.21 builtin
// min/max functions. Same-name bindings ($a, $b) require identical text on
// both sides, so only true clamp/aggregation patterns match — default-value
// fallbacks like `if n <= 0 { n = 100 }` are not reported.
func builtinMinMax(m dsl.Matcher) {
	m.Match(`if $a > $b { $a = $b }`).
		Report(`replace with the builtin: $a = min($a, $b)`)
	m.Match(`if $a >= $b { $a = $b }`).
		Report(`replace with the builtin: $a = min($a, $b)`)
	m.Match(`if $a < $b { $a = $b }`).
		Report(`replace with the builtin: $a = max($a, $b)`)
	m.Match(`if $a <= $b { $a = $b }`).
		Report(`replace with the builtin: $a = max($a, $b)`)
	m.Match(`if $a > $b { $b = $a }`).
		Report(`replace with the builtin: $b = max($b, $a)`)
	m.Match(`if $a >= $b { $b = $a }`).
		Report(`replace with the builtin: $b = max($b, $a)`)
	m.Match(`if $a < $b { $b = $a }`).
		Report(`replace with the builtin: $b = min($b, $a)`)
	m.Match(`if $a <= $b { $b = $a }`).
		Report(`replace with the builtin: $b = min($b, $a)`)
}
