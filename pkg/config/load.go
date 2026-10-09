// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package config

import (
	"fmt"
	"os"
	"regexp"

	"gopkg.in/yaml.v3"
)

// envVarRegex matches environment variable references in the form ${VAR} or
// ${VAR:-default}. The variable name must start with an uppercase letter or
// underscore and contain only uppercase letters, digits, and underscores. The
// optional default value (after ":-") may be any characters except "}".
//
// Capture groups:
//  1. The variable name.
//  2. The default value, present only when the ":-default" suffix exists
//     (may be empty, e.g. ${VAR:-}).
var envVarRegex = regexp.MustCompile(`\$\{([A-Z_][A-Z0-9_]*)(?::-([^}]*))?\}`)

// Load reads the YAML configuration file at path, performs environment
// variable interpolation, applies defaults, and unmarshals it into a Config.
//
// Environment variable interpolation supports two forms:
//   - ${VAR}: expands to the value of VAR. Returns an error if VAR is unset.
//   - ${VAR:-default}: expands to the value of VAR, or "default" if VAR is
//     unset. Note that a variable explicitly set to an empty string is
//     considered set, so the empty value is used rather than the default
//     (mirroring os.LookupEnv semantics).
//
// Interpolation is performed on the raw file bytes before YAML unmarshaling,
// so env var references may appear anywhere in the file including inside
// quoted strings.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path) //nolint:gosec // config path provided by operator
	if err != nil {
		return nil, fmt.Errorf("read config file %q: %w", path, err)
	}
	return LoadFromBytes(data)
}

// LoadFromBytes parses the given YAML bytes into a Config, performing
// environment variable interpolation first, then applying defaults and
// unmarshaling. It is primarily intended for tests and in-memory loading.
func LoadFromBytes(data []byte) (*Config, error) {
	interpolated, err := Interpolate(string(data))
	if err != nil {
		return nil, err
	}

	cfg := &Config{}
	cfg.SetDefaults()
	if err := yaml.Unmarshal([]byte(interpolated), cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}
	return cfg, nil
}

// Interpolate replaces ${VAR} and ${VAR:-default} references in s with the
// corresponding environment variable values. A reference to an unset variable
// without a default produces an error. It is exported so callers
// can reuse the same interpolation semantics on arbitrary config blobs.
func Interpolate(s string) (string, error) {
	var firstErr error
	result := envVarRegex.ReplaceAllStringFunc(s, func(match string) string {
		if firstErr != nil {
			return match
		}
		indices := envVarRegex.FindStringSubmatchIndex(match)
		if indices == nil {
			return match
		}
		// indices: [fullStart, fullEnd, nameStart, nameEnd, defaultStart, defaultEnd]
		name := match[indices[2]:indices[3]]
		if val, ok := os.LookupEnv(name); ok {
			return val
		}
		// indices[4] >= 0 means the optional ":-default" group matched.
		if indices[4] >= 0 {
			return match[indices[4]:indices[5]]
		}
		firstErr = fmt.Errorf("config: environment variable %s is not set", name)
		return match
	})
	if firstErr != nil {
		return "", firstErr
	}
	return result, nil
}
