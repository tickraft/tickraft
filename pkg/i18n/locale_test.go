// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package i18n

import "testing"

func TestParse_LocaleDecomposition(t *testing.T) {
	cases := []struct {
		in   string
		want Locale
	}{
		{"en", Locale{Tag: "en", Language: "en"}},
		{"EN", Locale{Tag: "en", Language: "en"}},
		{"  zh-Hans  ", Locale{Tag: "zh-Hans", Language: "zh", Script: "Hans"}},
		{"zh-Hant-TW", Locale{Tag: "zh-Hant-TW", Language: "zh", Script: "Hant", Region: "TW"}},
		{"ja-JP", Locale{Tag: "ja-JP", Language: "ja", Region: "JP"}},
		{"ar", Locale{Tag: "ar", Language: "ar"}},
		{"", Locale{Tag: DefaultLocale, Language: "zh", Script: "Hans"}},
		{"invalid", Locale{Tag: DefaultLocale, Language: "zh", Script: "Hans"}},
		{"x", Locale{Tag: DefaultLocale, Language: "zh", Script: "Hans"}},
		{"zh-", Locale{Tag: "zh", Language: "zh"}},
	}
	for _, c := range cases {
		got := Parse(c.in)
		if got != c.want {
			t.Errorf("Parse(%q) = %+v, want %+v", c.in, got, c.want)
		}
	}
}

func TestLocale_Direction(t *testing.T) {
	cases := []struct {
		tag  string
		want Direction
	}{
		{"en", LTR},
		{"zh-Hans", LTR},
		{"ja-JP", LTR},
		{"ar", RTL},
		{"ar-SA", RTL},
		{"he", RTL},
		{"fa-IR", RTL},
		{"ur-PK", RTL},
	}
	for _, c := range cases {
		if got := Parse(c.tag).Direction(); got != c.want {
			t.Errorf("Direction(%q) = %q, want %q", c.tag, got, c.want)
		}
	}
}

func TestLocale_Match(t *testing.T) {
	a := Parse("zh-Hans")
	b := Parse("zh-TW")
	c := Parse("en")
	if !a.Match(b) {
		t.Errorf("zh-Hans should Match zh-TW (same language)")
	}
	if a.Match(c) {
		t.Errorf("zh-Hans should not Match en")
	}
}

func TestLocale_String(t *testing.T) {
	if got := Parse("zh-Hans").String(); got != "zh-Hans" {
		t.Errorf("String() = %q, want %q", got, "zh-Hans")
	}
}
