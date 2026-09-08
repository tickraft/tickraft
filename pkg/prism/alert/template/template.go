// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package template

import (
	"context"
	"errors"

	"github.com/tickraft/tickraft/pkg/i18n"
	"github.com/tickraft/tickraft/pkg/prism/alert"
)

// ErrTemplateNotFound is returned by Library.Get and Renderer.Render when the
// requested template ID is not registered.
var ErrTemplateNotFound = errors.New("template: not found")

// ErrTranslationMissing is returned by Renderer.Render when the template has
// no translation for the requested locale and the fallback locale. This
// indicates a misconfigured template rather than a missing key (missing keys
// fall back to the key name).
var ErrTranslationMissing = errors.New("template: translation missing for locale")

// AlertType constants constrain which [alert.Event] values a template
// can render. They mirror the [alert.Type] values but are string
// aliases so templates can be validated without importing the prism alert
// package.
const (
	AlertTypeMetric  = string(alert.TypeMetric)
	AlertTypeLog     = string(alert.TypeLog)
	AlertTypeGeneric = "generic"
)

// Style constants mirror [i18n.Style] so templates can be validated against
// the supported style variants without importing the i18n package.
const (
	StyleConcise   = string(i18n.StyleConcise)
	StyleDetailed  = string(i18n.StyleDetailed)
	StyleTechnical = string(i18n.StyleTechnical)
)

// Network scope constants constrain which network environment a Template
// serves in private deployments: an intranet template renders the full form
// (complete fields, detail links) while an extranet template renders the
// lightweight form for public channels. ScopeBoth — and the empty string —
// serve every environment.
const (
	// ScopeIntranet marks a template for intranet channels.
	ScopeIntranet = "intranet"
	// ScopeExtranet marks a template for extranet channels.
	ScopeExtranet = "extranet"
	// ScopeBoth marks a template valid for both network scopes.
	ScopeBoth = "both"
)

// validAlertTypes is the set of AlertType values a Template may declare.
var validAlertTypes = map[string]bool{
	AlertTypeMetric:  true,
	AlertTypeLog:     true,
	AlertTypeGeneric: true,
}

// validStyles is the set of Style values a Template may declare.
var validStyles = map[string]bool{
	StyleConcise:   true,
	StyleDetailed:  true,
	StyleTechnical: true,
}

// validNetworkScopes is the set of NetworkScope values a Template may
// declare. The empty string is accepted and means the same as ScopeBoth.
var validNetworkScopes = map[string]bool{
	"":            true,
	ScopeIntranet: true,
	ScopeExtranet: true,
	ScopeBoth:     true,
}

// Template is a self-contained alert rendering definition. A Template is
// immutable after construction; the Library and Renderer treat it as
// read-only.
type Template struct {
	// ID is the unique identifier for the template (e.g. "cpu_high"). IDs
	// are lower_snake_case and must be unique within a Library.
	ID string
	// Name is the human-readable display name (e.g. "CPU Usage High").
	Name string
	// Description explains the template's purpose for the management UI.
	Description string
	// AlertType constrains which [alert.Event] values the template
	// can render. Must be one of AlertTypeMetric, AlertTypeLog, or
	// AlertTypeGeneric.
	AlertType string
	// Variables declares the substitution keys the template body expects.
	// Variable names must match ^[a-z][a-z0-9_]*$.
	Variables []string
	// Translations maps locale tag (e.g. "en-US", "zh-Hans") to a flat key/value
	// map of template strings. The key is "title.<style>" or
	// "description.<style>" where <style> is one of concise/detailed/technical.
	// For example: Translations["en-US"]["title.concise"] = "Alert: {{.metric_name}}".
	Translations map[string]map[string]string
	// Styles lists the style variants the template supports. A template
	// does not need to support all three styles, but each declared style
	// must have both a title and description key in every translation.
	Styles []string
	// ChannelHints lists the channels (email/sms/im) the template is
	// optimized for. This is advisory only; channels may use any template.
	ChannelHints []string
	// NetworkScope constrains which network environment the template
	// serves: ScopeIntranet, ScopeExtranet, or ScopeBoth. The empty string
	// behaves as ScopeBoth. When a Renderer renders for a specific scope,
	// a template whose scope does not include it is skipped in favor of the
	// scoped sibling variant (see ScopedTemplateID).
	NetworkScope string
}

// RenderOptions controls how a Renderer renders an [alert.Event].
type RenderOptions struct {
	// TemplateID is the ID of the template to render. Required.
	TemplateID string
	// Locale is the recipient's preferred locale. When empty, the default
	// locale "zh-Hans" is used.
	Locale string
	// Style is the formatting style. When empty, StyleDetailed is used.
	Style string
	// FrontendBaseURL is the base URL for constructing resource links.
	// When empty, FormattedMessage.AssetLink is an empty string.
	FrontendBaseURL string
	// NetworkScope is the network environment the rendered message targets:
	// ScopeIntranet, ScopeExtranet, or empty (no scope policy — every
	// template is eligible and no variant switching happens).
	NetworkScope string
}

// Renderer renders an [alert.Event] into an
// [i18n.FormattedMessage] using a template from a Library. Implementations
// must be safe for concurrent use.
type Renderer interface {
	// Render produces a FormattedMessage for the given alert using the
	// template identified by opts.TemplateID. Returns ErrTemplateNotFound
	// when the template is not registered, or ErrTranslationMissing when
	// the template has no translation for the requested locale or the
	// fallback locale.
	Render(ctx context.Context, alert alert.Event, opts RenderOptions) (i18n.FormattedMessage, error)
}

// IsValidAlertType reports whether t is a recognized AlertType.
func IsValidAlertType(t string) bool {
	return validAlertTypes[t]
}

// IsValidStyle reports whether s is a recognized Style.
func IsValidStyle(s string) bool {
	return validStyles[s]
}

// IsValidNetworkScope reports whether s is a recognized NetworkScope. The
// empty string is valid and equivalent to ScopeBoth.
func IsValidNetworkScope(s string) bool {
	return validNetworkScopes[s]
}
