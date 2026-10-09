// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package i18n

import (
	"strings"
)

// Direction is the text layout direction for a locale.
type Direction string

const (
	// LTR is left-to-right text direction (the default for most locales).
	LTR Direction = "ltr"
	// RTL is right-to-left text direction (e.g. Arabic, Hebrew).
	RTL Direction = "rtl"
)

// DefaultLocale is the fallback locale used when no exact match is found in
// the Registry. It is always registered by NewRegistry.
const DefaultLocale = "zh-Hans"

// localeEnUS is the builtin English locale tag.
const localeEnUS = "en-US"

// scriptHans is the script subtag of the default (Simplified Chinese)
// locale, used when constructing fallback Locales.
const scriptHans = "Hans"

// displayZhHans is the native display name of the default (Simplified
// Chinese) locale.
const displayZhHans = "简体中文"

// LocaleMeta describes a supported locale for API exposure. Extended
// editions extend the builtin set with additional LocaleMeta entries via
// Registry.Register at startup.
type LocaleMeta struct {
	// Tag is the canonical BCP 47 tag (e.g. "zh-Hans", "en-US").
	Tag string
	// NativeName is the locale name rendered in its own language (e.g.
	// "简体中文" for zh-Hans).
	NativeName string
	// Direction is the text direction for the locale.
	Direction Direction
}

// BuiltinLocales lists the locales embedded in the kernel.
// callers may extend this list by registering additional
// locale bundles via Registry.Register.
var BuiltinLocales = []LocaleMeta{
	{Tag: DefaultLocale, NativeName: displayZhHans, Direction: LTR},
	{Tag: localeEnUS, NativeName: "English", Direction: LTR},
}

// rtlLanguages is the set of languages rendered right-to-left. The set is
// intentionally small and covers the languages currently shipped with the
// platform; additional RTL languages can be added here as new resource packs
// are introduced.
var rtlLanguages = map[string]bool{
	"ar": true,
	"he": true,
	"fa": true,
	"ur": true,
}

// Locale represents a BCP 47 language tag decomposed into its primary
// language subtag, an optional region subtag and an optional script subtag.
//
// The Tag field stores the canonical BCP 47 form with standard casing:
// language in lowercase, region in uppercase, script in title case (e.g.
// "en-US", "zh-Hans", "zh-Hant-TW"). The decomposition fields are populated by
// Parse and are used to drive the fallback chain and the text direction
// classifier.
type Locale struct {
	// Tag is the canonical BCP 47 tag with standard casing (e.g. "zh-Hans").
	Tag string
	// Language is the primary language subtag, lowercase (e.g. "zh", "en").
	Language string
	// Region is the region subtag without the leading dash, uppercase (e.g.
	// "CN", "TW"). Empty when no region is present.
	Region string
	// Script is the script subtag in title case (e.g. "Hant", "Hans"). Empty
	// when no script is present. Script subtags are currently preserved for
	// future use and do not participate in the fallback chain.
	Script string
}

// Parse decomposes a BCP 47 language tag into a Locale. The input is
// normalized to standard BCP 47 casing: language subtag to lowercase, region
// subtag to uppercase, script subtag to title case. Tags that fail validation
// fall back to the default locale so callers never receive a zero-value
// Locale.
//
// Examples:
//
//	locale := Parse("zh-Hans")    // {Tag:"zh-Hans", Language:"zh", Script:"Hans"}
//	locale := Parse("en-US")      // {Tag:"en-US", Language:"en", Region:"US"}
//	locale := Parse("en")         // {Tag:"en", Language:"en"}
//	locale := Parse("zh-Hant-TW") // {Tag:"zh-Hant-TW", Language:"zh", Script:"Hant", Region:"TW"}
//	locale := Parse("ZH-hans")    // {Tag:"zh-Hans", Language:"zh", Script:"Hans"} (normalized)
//	locale := Parse("invalid")    // {Tag:"zh-Hans", Language:"zh"} (fallback)
func Parse(tag string) Locale {
	trimmed := strings.TrimSpace(tag)
	trimmed = strings.Trim(trimmed, "-")
	if trimmed == "" {
		return Locale{Tag: DefaultLocale, Language: "zh", Script: scriptHans}
	}

	parts := strings.Split(trimmed, "-")
	if len(parts) == 0 {
		return Locale{Tag: DefaultLocale, Language: "zh", Script: scriptHans}
	}

	lang := strings.ToLower(parts[0])
	if !isValidLanguage(lang) {
		return Locale{Tag: DefaultLocale, Language: "zh", Script: scriptHans}
	}

	loc := Locale{Language: lang}
	subTags := []string{lang}
	for _, sub := range parts[1:] {
		if sub == "" {
			continue
		}
		lower := strings.ToLower(sub)
		switch {
		case isRegion(lower):
			loc.Region = strings.ToUpper(sub)
			subTags = append(subTags, loc.Region)
		case isScript(lower):
			loc.Script = strings.ToUpper(sub[:1]) + lower[1:]
			subTags = append(subTags, loc.Script)
		}
	}

	loc.Tag = strings.Join(subTags, "-")
	return loc
}

// Direction classifies the text direction for the locale based on its
// primary language subtag. Languages in the rtlLanguages set return RTL;
// all others return LTR.
func (l Locale) Direction() Direction {
	if rtlLanguages[l.Language] {
		return RTL
	}
	return LTR
}

// Match reports whether two locales refer to the same language, ignoring
// region and script differences. It is used by the Registry fallback chain
// to find a language-only translator when an exact match is missing.
func (l Locale) Match(other Locale) bool {
	return l.Language == other.Language
}

// String returns the canonical tag form.
func (l Locale) String() string {
	return l.Tag
}

// isValidLanguage reports whether s is a valid 2-3 letter ASCII language
// subtag per BCP 47.
func isValidLanguage(s string) bool {
	if len(s) < 2 || len(s) > 3 {
		return false
	}
	for _, r := range s {
		if r < 'a' || r > 'z' {
			return false
		}
	}
	return true
}

// isRegion reports whether s is a 2-letter ASCII region subtag. BCP 47 also
// allows 3-digit UN M.49 region codes; the platform does not currently use
// them so they are intentionally rejected to keep the validation strict.
func isRegion(s string) bool {
	if len(s) != 2 {
		return false
	}
	for _, r := range s {
		if r < 'a' || r > 'z' {
			return false
		}
	}
	return true
}

// isScript reports whether s is a 4-letter ASCII script subtag.
func isScript(s string) bool {
	if len(s) != 4 {
		return false
	}
	for _, r := range s {
		if r < 'a' || r > 'z' {
			return false
		}
	}
	return true
}
