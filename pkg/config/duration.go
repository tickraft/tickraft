// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package config

import (
	"fmt"
	"time"
)

// Duration wraps time.Duration so that human-readable duration strings such as
// "5s", "1m30s", or "24h" can be used directly in YAML and JSON configuration
// files. The zero value represents a zero duration.
//
// Both YAML (gopkg.in/yaml.v3) and encoding/json honor the TextMarshaler and
// TextUnmarshaler interfaces for scalar values, so a field of type Duration
// accepts a plain string in the config file while exposing a time.Duration via
// the Duration method.
type Duration time.Duration

// String returns the duration formatted using time.Duration's String method.
func (d Duration) String() string {
	return time.Duration(d).String()
}

// Duration returns the underlying time.Duration value.
func (d Duration) Duration() time.Duration {
	return time.Duration(d)
}

// IsZero reports whether the duration is zero.
func (d Duration) IsZero() bool {
	return time.Duration(d) == 0
}

// UnmarshalText implements encoding.TextUnmarshaler. It parses a duration
// string using time.ParseDuration. An empty input leaves the value unchanged
// (zero duration), which allows a default applied before unmarshaling to be
// retained when the YAML field is omitted.
func (d *Duration) UnmarshalText(text []byte) error {
	s := string(text)
	if s == "" {
		return nil
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("parse duration %q: %w", s, err)
	}
	*d = Duration(v)
	return nil
}

// MarshalText implements encoding.TextMarshaler so the value serializes back
// to the same human-readable form used in the source config file.
func (d Duration) MarshalText() ([]byte, error) {
	return []byte(time.Duration(d).String()), nil
}
