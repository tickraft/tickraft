// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package i18n

// Translator resolves message keys for a specific locale, applying a
// fallback to the default locale ("zh-Hans") when a key is missing from the
// primary bundle.
//
// The Translator is the primary read-side abstraction used by formatters and
// channel renderers. It does not expose mutation methods: hot-reload is
// handled by replacing the Bundle inside the Registry, which produces a new
// Translator via Resolve.
type Translator interface {
	// T returns the localized message for the given key. When the key is
	// missing from the translator's own bundle, T falls back to the default
	// locale ("zh-Hans") bundle, and finally returns the key itself when the
	// key is missing from the default bundle as well.
	//
	// Implementations may accept optional params for future positional
	// substitution support; the current contract ignores params for keys
	// that do not declare placeholders.
	T(key string, params ...any) string
	// Locale returns the locale tag this translator is bound to.
	Locale() string
	// Direction returns the text direction for the translator's locale.
	Direction() Direction
	// Lookup performs a non-fallback key lookup. It returns the value only
	// if the key is present in the translator's own bundle, and falls back
	// to the default bundle only when allowDefault is true. The boolean
	// indicates whether a value was found in the consulted bundle(s).
	Lookup(key string, allowDefault bool) (value string, ok bool)
}

// translator is the default Translator implementation. It wraps a primary
// Bundle and an optional default Bundle used for fallback. Both bundles are
// resolved at construction time so reads are lock-free.
type translator struct {
	primary  Bundle
	fallback Bundle
}

// NewTranslator creates a Translator bound to the given primary bundle. When
// fallback is non-nil, missing keys are resolved against it; otherwise the
// translator's Lookup with allowDefault=false is the only source.
func NewTranslator(primary, fallback Bundle) Translator {
	return &translator{primary: primary, fallback: fallback}
}

// T implements Translator. It consults the primary bundle first, then the
// fallback bundle, and finally returns the key itself.
func (t *translator) T(key string, _ ...any) string {
	if t.primary != nil {
		if v, ok := t.primary.Lookup(key); ok {
			return v
		}
	}
	if t.fallback != nil {
		if v, ok := t.fallback.Lookup(key); ok {
			return v
		}
	}
	return key
}

// Locale implements Translator.
func (t *translator) Locale() string {
	if t.primary != nil {
		return t.primary.Locale()
	}
	return DefaultLocale
}

// Direction implements Translator.
func (t *translator) Direction() Direction {
	return Parse(t.Locale()).Direction()
}

// Lookup implements Translator.
func (t *translator) Lookup(key string, allowDefault bool) (string, bool) {
	if t.primary != nil {
		if v, ok := t.primary.Lookup(key); ok {
			return v, true
		}
	}
	if allowDefault && t.fallback != nil {
		if v, ok := t.fallback.Lookup(key); ok {
			return v, true
		}
	}
	return "", false
}
