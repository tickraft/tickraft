// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package i18n

import (
	"strings"
	"sync"

	"go.uber.org/zap"
)

// LocaleInfo describes a locale exposed by the Registry for API consumption
// (e.g. GET /api/v1/i18n/locales). It carries both the canonical tag and a
// human-readable native name suitable for display in language switchers.
type LocaleInfo struct {
	// Tag is the canonical BCP 47 tag with standard casing (e.g. "zh-Hans").
	Tag string `json:"tag"`
	// Language is the primary language subtag, lowercase (e.g. "zh").
	Language string `json:"language"`
	// Region is the region subtag without the leading dash, uppercase (e.g.
	// "CN"). Empty when the locale has no region.
	Region string `json:"region,omitempty"`
	// Direction is the text direction for the locale ("ltr" or "rtl").
	Direction Direction `json:"direction"`
	// NativeName is the locale name rendered in its own language (e.g.
	// "简体中文" for zh-Hans). It is loaded from the bundle key
	// "_meta.native_name" when present, falling back to the tag otherwise.
	NativeName string `json:"native_name"`
}

// Registry is a concurrent-safe registry of locale bundles. It implements
// the fallback chain used across the platform:
//
//  1. Exact match on the requested tag (e.g. "zh-Hans", "en-US").
//  2. Language-only match (e.g. "en" when "en-US" is missing).
//  3. Default locale "zh-Hans".
//
// Implementations must be safe for concurrent use.
type Registry interface {
	// Register adds or replaces the bundle for the given locale tag. The
	// tag is normalized to standard BCP 47 casing. Re-registering an
	// existing locale atomically replaces its bundle.
	Register(tag string, bundle Bundle)
	// Unregister removes the bundle for the given tag. Removing the
	// default locale ("zh-Hans") is a no-op; the default bundle is always
	// available so the fallback chain can terminate.
	Unregister(tag string)
	// Resolve returns a Translator for the requested tag, applying the
	// fallback chain. The returned Translator always has a non-nil primary
	// bundle: when neither the exact tag nor the language-only tag is
	// registered, the default locale bundle is used as the primary.
	Resolve(tag string) Translator
	// List returns metadata for all registered locales, sorted by tag.
	// The default locale is always included.
	List() []LocaleInfo
	// Has reports whether a bundle is registered for the tag, checking
	// both exact match and language-only match. It is used by the language
	// preference handler to validate user input (e.g. "en-US" is accepted
	// when "en" is registered).
	Has(tag string) bool
}

// registry is the default Registry implementation.
type registry struct {
	mu      sync.RWMutex
	bundles map[string]Bundle
	logger  *zap.Logger
}

// NewRegistry creates a Registry pre-populated with an empty default-locale
// bundle. The logger is used to record fallback-path decisions; pass a
// no-op logger when observability is not required.
func NewRegistry(logger *zap.Logger) Registry {
	if logger == nil {
		logger = zap.NewNop()
	}
	r := &registry{
		bundles: make(map[string]Bundle),
		logger:  logger,
	}
	r.bundles[DefaultLocale] = emptyBundle{tag: DefaultLocale}
	return r
}

// Register implements Registry.
func (r *registry) Register(tag string, bundle Bundle) {
	if bundle == nil {
		return
	}
	normalized := Parse(tag).Tag
	if normalized == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.bundles[normalized] = bundle
}

// Unregister implements Registry.
func (r *registry) Unregister(tag string) {
	normalized := Parse(tag).Tag
	if normalized == DefaultLocale {
		// Never remove the default locale; the fallback chain must
		// always terminate.
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.bundles, normalized)
}

// Resolve implements Registry. The fallback chain is:
//
//  1. Exact tag match (e.g. "zh-Hans").
//  2. Language-only match (e.g. "zh" when "zh-Hans" is missing).
//  3. Default locale "zh-Hans".
//
// Steps 2 and 3 emit an info-level log entry so the operator can observe
// when a request falls back from the requested locale. Step 1 (exact match)
// is the common path and is silent.
func (r *registry) Resolve(tag string) Translator {
	loc := Parse(tag)
	requested := loc.Tag

	r.mu.RLock()
	primary, primaryOK := r.bundles[requested]
	r.mu.RUnlock()

	if primaryOK {
		// Exact match: no fallback needed, but the translator still
		// carries the default bundle for missing-key lookups.
		return NewTranslator(primary, r.defaultBundle())
	}

	// Step 2: language-only fallback.
	languageOnly := loc.Language
	r.mu.RLock()
	langBundle, langOK := r.bundles[languageOnly]
	r.mu.RUnlock()

	if langOK && languageOnly != requested {
		r.logger.Info("i18n locale resolved via language fallback",
			zap.String("requested", requested),
			zap.String("resolved", languageOnly),
		)
		return NewTranslator(langBundle, r.defaultBundle())
	}

	// Step 3: default locale fallback.
	r.logger.Info("i18n locale resolved via default fallback",
		zap.String("requested", requested),
		zap.String("resolved", DefaultLocale),
	)
	return NewTranslator(r.defaultBundle(), nil)
}

// List implements Registry.
func (r *registry) List() []LocaleInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	defaultBundle := r.bundles[DefaultLocale]
	out := make([]LocaleInfo, 0, len(r.bundles))
	for tag, b := range r.bundles {
		loc := Parse(tag)
		info := LocaleInfo{
			Tag:        tag,
			Language:   loc.Language,
			Region:     loc.Region,
			Direction:  loc.Direction(),
			NativeName: nativeName(b, defaultBundle, tag),
		}
		out = append(out, info)
	}
	// Sort by tag for deterministic API output.
	sortLocaleInfos(out)
	return out
}

// Has implements Registry. It checks both exact tag match and language-only
// match, so "en-US" is accepted when "en" is registered as a language-only
// bundle. This allows the frontend to use region-specific tags (en-US) while
// the backend registers language-only bundles (en) for broader fallback
// coverage.
//
// Invalid input that Parse would fall back to the default locale is rejected:
// Parse returns DefaultLocale ("zh-Hans") for tags that fail validation, so Has
// verifies the input's first subtag matches the parsed language before
// accepting the match.
func (r *registry) Has(tag string) bool {
	trimmed := strings.TrimSpace(tag)
	if trimmed == "" {
		return false
	}
	loc := Parse(tag)
	// Reject Parse fallback: if the input's language subtag doesn't match
	// the parsed language, the input was invalid and Parse fell back to
	// the default locale.
	inputFirst := strings.ToLower(strings.SplitN(trimmed, "-", 2)[0])
	if loc.Language != inputFirst {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if _, ok := r.bundles[loc.Tag]; ok {
		return true
	}
	if loc.Tag != loc.Language {
		if _, ok := r.bundles[loc.Language]; ok {
			return true
		}
	}
	return false
}

// defaultBundle returns the default-locale bundle. It acquires the read
// lock internally and is safe to call from any context. The default bundle
// is never deleted (Unregister refuses to remove it), so the returned
// Bundle is always non-nil even if it is the emptyBundle sentinel
// installed by NewRegistry.
func (r *registry) defaultBundle() Bundle {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.bundles[DefaultLocale]
}

// nativeName resolves the human-readable locale name from the bundle's
// "_meta.native_name" key, falling back to the default bundle and finally
// to the tag itself.
func nativeName(b, defaultBundle Bundle, tag string) string {
	const key = "_meta.native_name"
	if b != nil {
		if v, ok := b.Lookup(key); ok {
			return v
		}
	}
	if defaultBundle != nil {
		if v, ok := defaultBundle.Lookup(key); ok {
			return v
		}
	}
	return tag
}

// sortLocaleInfos sorts the slice in place by Tag. A small insertion sort is
// used because the locale count is expected to remain in the low tens.
func sortLocaleInfos(infos []LocaleInfo) {
	for i := 1; i < len(infos); i++ {
		j := i
		for j > 0 && infos[j-1].Tag > infos[j].Tag {
			infos[j-1], infos[j] = infos[j], infos[j-1]
			j--
		}
	}
}
