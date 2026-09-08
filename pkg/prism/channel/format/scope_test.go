// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package format

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/tickraft/tickraft/pkg/i18n"
	"github.com/tickraft/tickraft/pkg/prism/alert"
	"github.com/tickraft/tickraft/pkg/prism/alert/template"
)

// maskEverything is a test Masker that replaces every field value with a
// fixed marker.
type maskEverything struct{}

func (maskEverything) Mask(msg Message) Message {
	if msg.Title != "" {
		msg.Title = "***"
	}
	if msg.Description != "" {
		msg.Description = "***"
	}
	for k, v := range msg.Fields {
		if v != "" {
			msg.Fields[k] = "***"
		}
	}
	return msg
}

func scopeSampleEvent() alert.Event {
	return alert.Event{
		Type:      alert.TypeLog,
		AssetID:   3,
		TenantID:  1,
		Timestamp: fixedTime,
		Violations: []alert.Violation{{
			Kind:     alert.ViolationKindLog,
			Severity: "error",
			Message:  "boom",
			Log:      &alert.LogContext{Content: "boom at 10.0.0.5"},
		}},
	}
}

// fixedTime keeps message timestamps deterministic.
var fixedTime = timeFromRFC3339("2026-09-04T08:00:00Z")

func timeFromRFC3339(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestRender_ScopeExtranetMasksAndKeepsLink(t *testing.T) {
	opts := RenderOptions{
		Formatter: testFormatter(t),
		Scope: ScopeOptions{
			NetworkScope: ScopeExtranet,
			Masker:       maskEverything{},
		},
	}
	msg := Render(context.Background(), scopeSampleEvent(), opts)
	if msg.Title != "***" {
		t.Errorf("title should be masked, got %q", msg.Title)
	}
	for k, v := range msg.Fields {
		if v == "boom at 10.0.0.5" {
			t.Errorf("field %q still carries unmasked content", k)
		}
	}
}

func TestRender_ScopeExtranetLinkAdapterWins(t *testing.T) {
	opts := RenderOptions{
		Formatter:       testFormatter(t),
		FrontendBaseURL: "http://console.intra",
		Scope: ScopeOptions{
			NetworkScope:    ScopeExtranet,
			ExtranetBaseURL: "https://dmz.example.com",
			LinkAdapter: func(link string) (string, bool) {
				return "https://dmz.example.com/resources/3?_e=123&_s=abc", true
			},
		},
	}
	msg := Render(context.Background(), scopeSampleEvent(), opts)
	if msg.AssetLink != "https://dmz.example.com/resources/3?_e=123&_s=abc" {
		t.Errorf("asset link should come from the adapter, got %q", msg.AssetLink)
	}
	if msg.LinkNote != "" {
		t.Errorf("an adapted link should not carry the intranet note, got %q", msg.LinkNote)
	}
}

func TestRender_ScopeExtranetRebaseWithoutAdapter(t *testing.T) {
	opts := RenderOptions{
		Formatter:       testFormatter(t),
		FrontendBaseURL: "http://console.intra",
		Scope: ScopeOptions{
			NetworkScope:    ScopeExtranet,
			ExtranetBaseURL: "https://dmz.example.com",
		},
	}
	msg := Render(context.Background(), scopeSampleEvent(), opts)
	if msg.AssetLink != "https://dmz.example.com/resources/3" {
		t.Errorf("asset link should be rebased onto the public base, got %q", msg.AssetLink)
	}
}

func TestRender_ScopeExtranetWithoutPublicBaseKeepsHint(t *testing.T) {
	opts := RenderOptions{
		Formatter:       testFormatter(t),
		FrontendBaseURL: "http://console.intra",
		Scope:           ScopeOptions{NetworkScope: ScopeExtranet},
	}
	msg := Render(context.Background(), scopeSampleEvent(), opts)
	if msg.AssetLink != "http://console.intra/resources/3" {
		t.Errorf("asset link should stay intranet, got %q", msg.AssetLink)
	}
	if msg.LinkNote == "" {
		t.Error("a non-adapted extranet link should carry the intranet note")
	}
	if !strings.Contains(RenderText(msg), msg.AssetLink) {
		t.Error("RenderText should render the asset link")
	}
}

func TestRender_NoScopeKeepsFullContent(t *testing.T) {
	opts := RenderOptions{
		Formatter:       testFormatter(t),
		FrontendBaseURL: "http://console.intra",
		Scope:           ScopeOptions{NetworkScope: ScopeIntranet, Masker: maskEverything{}},
	}
	msg := Render(context.Background(), scopeSampleEvent(), opts)
	if msg.Title == "***" {
		t.Error("intranet scope must not mask content")
	}
	if msg.AssetLink != "http://console.intra/resources/3" {
		t.Errorf("intranet link should stay untouched, got %q", msg.AssetLink)
	}
}

func TestRender_ScopeSelectsTemplateVariant(t *testing.T) {
	lib := template.NewLibrary(nil)
	lib.Register(scopedTmpl("cpu_high", template.ScopeBoth))
	lib.Register(scopedTmpl("cpu_high_extranet", template.ScopeExtranet))
	evt := alert.Event{
		Type:       alert.TypeLog,
		TemplateID: "cpu_high",
		Locale:     "en-US",
	}
	msg := Render(context.Background(), evt, RenderOptions{
		Library: lib,
		Scope:   ScopeOptions{NetworkScope: ScopeExtranet},
	})
	if !strings.Contains(msg.Title, "cpu_high_extranet") {
		t.Errorf("extranet scope should render the variant template, got title %q", msg.Title)
	}
}

func TestExtranetLinkAdapterDeclineFallsBack(t *testing.T) {
	scope := ScopeOptions{
		NetworkScope:    ScopeExtranet,
		ExtranetBaseURL: "https://dmz.example.com",
		LinkAdapter:     func(string) (string, bool) { return "", false },
	}
	link, hint := ExtranetLink("http://console.intra/resources/3", scope)
	if hint {
		t.Error("a declined adapter should fall back to the unsigned rebase, not the hint")
	}
	if link != "https://dmz.example.com/resources/3" {
		t.Errorf("unexpected rebased link %q", link)
	}
}

func TestRebaseLink(t *testing.T) {
	cases := []struct {
		link, base, want string
		ok               bool
	}{
		{"https://a.in/x/y?z=1", "https://b.com", "https://b.com/x/y?z=1", true},
		{"/x/y?z=1", "https://b.com", "https://b.com/x/y?z=1", true},
		{"https://a.in/x#frag", "https://b.com", "https://b.com/x#frag", true},
		{"not a url", "https://b.com", "", false},
	}
	for _, tc := range cases {
		got, ok := rebaseLink(tc.link, tc.base)
		if ok != tc.ok || got != tc.want {
			t.Errorf("rebaseLink(%q, %q) = (%q, %v), want (%q, %v)",
				tc.link, tc.base, got, ok, tc.want, tc.ok)
		}
	}
}

func TestMaskFuncAdapter(t *testing.T) {
	var called bool
	m := MaskFunc(func(msg Message) Message {
		called = true
		return msg
	})
	_ = m.Mask(Message{})
	if !called {
		t.Error("MaskFunc should delegate to the wrapped function")
	}
}

func TestScopeOptionsEnabled(t *testing.T) {
	if (ScopeOptions{}).Enabled() {
		t.Error("zero ScopeOptions must not be enabled")
	}
	if !(ScopeOptions{NetworkScope: ScopeIntranet}).Enabled() {
		t.Error("intranet scope should be enabled")
	}
}

// testFormatter builds a Formatter backed by the embedded i18n bundle.
func testFormatter(t *testing.T) i18n.Formatter {
	t.Helper()
	registry := i18n.NewRegistry(nil)
	loader := i18n.NewLoader(nil)
	if err := loader.LoadToRegistry(i18n.EmbeddedFS(), registry); err != nil {
		t.Fatalf("load i18n resources: %v", err)
	}
	return i18n.NewDefaultFormatter(registry, nil)
}

// scopedTmpl builds a minimal renderable template for variant tests.
func scopedTmpl(id, scope string) template.Template {
	return template.Template{
		ID:           id,
		Name:         id,
		AlertType:    template.AlertTypeLog,
		NetworkScope: scope,
		Translations: map[string]map[string]string{
			"en-US": {
				"title.detailed":       "T:" + id,
				"description.detailed": "D:" + id,
			},
		},
		Styles: []string{template.StyleDetailed},
	}
}
