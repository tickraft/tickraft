// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package rule

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/expr-lang/expr"
)

// regexFn registers the regex(pattern, target) boolean function with
// expr-lang. It compiles the pattern on each call; callers that need
// cached compilation should pre-compile outside the engine.
var regexFn = expr.Function(
	"regex",
	func(params ...any) (any, error) {
		if len(params) != 2 {
			return nil, fmt.Errorf("regex: expected 2 arguments, got %d", len(params))
		}
		pattern, ok := params[0].(string)
		if !ok {
			return nil, fmt.Errorf("regex: pattern must be string, got %T", params[0])
		}
		target, ok := params[1].(string)
		if !ok {
			return nil, fmt.Errorf("regex: target must be string, got %T", params[1])
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("regex: invalid pattern %q: %w", pattern, err)
		}
		return re.MatchString(target), nil
	},
	new(func(string, string) bool),
)

// containsAnyFn registers the containsAny(haystack, needles) boolean
// function with expr-lang. It returns true when haystack contains any
// of the needles as a substring.
var containsAnyFn = expr.Function(
	"containsAny",
	func(params ...any) (any, error) {
		if len(params) != 2 {
			return nil, fmt.Errorf("containsAny: expected 2 arguments, got %d", len(params))
		}
		haystack, ok := params[0].(string)
		if !ok {
			return nil, fmt.Errorf("containsAny: haystack must be string, got %T", params[0])
		}
		needles, ok := params[1].([]string)
		if !ok {
			return nil, fmt.Errorf("containsAny: needles must be []string, got %T", params[1])
		}
		for _, n := range needles {
			if strings.Contains(haystack, n) {
				return true, nil
			}
		}
		return false, nil
	},
	new(func(string, []string) bool),
)

// inRangeFn registers the inRange(value, min, max) boolean function
// with expr-lang. The range is closed: value matches when
// min <= value <= max.
var inRangeFn = expr.Function(
	"inRange",
	func(params ...any) (any, error) {
		if len(params) != 3 {
			return nil, fmt.Errorf("inRange: expected 3 arguments, got %d", len(params))
		}
		value, ok := toFloat64(params[0])
		if !ok {
			return nil, fmt.Errorf("inRange: value must be numeric, got %T", params[0])
		}
		lo, ok := toFloat64(params[1])
		if !ok {
			return nil, fmt.Errorf("inRange: min must be numeric, got %T", params[1])
		}
		hi, ok := toFloat64(params[2])
		if !ok {
			return nil, fmt.Errorf("inRange: max must be numeric, got %T", params[2])
		}
		return value >= lo && value <= hi, nil
	},
	new(func(float64, float64, float64) bool),
)

// agoFn registers the ago(duration) time function with expr-lang. It
// returns the time instant that is duration before now, allowing
// expressions such as `metric.Timestamp > ago("5m")`.
var agoFn = expr.Function(
	"ago",
	func(params ...any) (any, error) {
		if len(params) != 1 {
			return nil, fmt.Errorf("ago: expected 1 argument, got %d", len(params))
		}
		durStr, ok := params[0].(string)
		if !ok {
			return nil, fmt.Errorf("ago: duration must be string, got %T", params[0])
		}
		d, err := time.ParseDuration(durStr)
		if err != nil {
			return nil, fmt.Errorf("ago: invalid duration %q: %w", durStr, err)
		}
		return time.Now().Add(-d), nil
	},
	new(func(string) time.Time),
)

// toFloat64 coerces numeric primitive types to float64. It is used by
// inRangeFn to accept int, int64, float64, etc. uniformly.
func toFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case int32:
		return float64(n), true
	default:
		return 0, false
	}
}
