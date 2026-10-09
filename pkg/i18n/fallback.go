// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package i18n

import "go.uber.org/zap"

// ResolveKey returns the message for the given key from the translator,
// applying the fallback chain (primary bundle -> default-locale bundle -> key
// itself). It is the canonical key-resolution path used by formatters and
// channel renderers.
//
// When the key is missing from both the primary and the default bundles, the
// key itself is returned and a warning is logged via the provided logger so
// operators can identify missing translations. A nil logger suppresses the
// warning.
func ResolveKey(t Translator, key string, logger *zap.Logger) string {
	if t == nil {
		if logger != nil {
			logger.Warn("i18n translator is nil, returning key as-is",
				zap.String("key", key),
			)
		}
		return key
	}

	// Consult the primary bundle first.
	if v, ok := t.Lookup(key, false); ok {
		return v
	}

	// Fall back to the default-locale bundle. The translator's Lookup with
	// allowDefault=true performs this internally.
	if v, ok := t.Lookup(key, true); ok {
		if logger != nil {
			logger.Debug("i18n key resolved via default fallback",
				zap.String("key", key),
				zap.String("locale", t.Locale()),
			)
		}
		return v
	}

	// Last resort: return the key itself and log a warning so missing
	// translations are visible during development.
	if logger != nil {
		logger.Warn("i18n key missing in all bundles, returning key as-is",
			zap.String("key", key),
			zap.String("locale", t.Locale()),
		)
	}
	return key
}

// MissingVariablePlaceholder returns the placeholder text used when a template
// variable is not provided. The placeholder format is `[MISSING:variable_name]`
// so it is easy to grep for in logs and rendered output.
func MissingVariablePlaceholder(name string) string {
	return "[MISSING:" + name + "]"
}
