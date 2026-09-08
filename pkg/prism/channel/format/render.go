// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package format

import (
	"context"

	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/i18n"
	"github.com/tickraft/tickraft/pkg/prism/alert"
	"github.com/tickraft/tickraft/pkg/prism/alert/template"
)

// RenderOptions configures the Render helper. All fields are optional;
// however, a nil Formatter will cause Render to return a zero-value
// Message (with an error log) when template rendering is not applicable.
// Callers should always inject a Formatter at startup.
type RenderOptions struct {
	// Formatter renders alert events into localized messages. When non-nil
	// and template rendering is not applicable, Render delegates to
	// BuildWithOpts with this Formatter.
	Formatter i18n.Formatter
	// Library is the alert template library used for template-based
	// rendering. When non-nil and alert.TemplateID is non-empty, Render
	// uses template.NewRenderer to render the alert via the Library.
	Library template.Library
	// Registry resolves level labels, field labels, and time formats for
	// template-based rendering. Passed to template.NewRenderer. When nil
	// the template renderer falls back to English defaults.
	Registry i18n.Registry
	// Logger is the structured logger. When nil, a no-op logger is used.
	Logger *zap.Logger
	// FrontendBaseURL is the base URL for constructing resource deep links.
	// Used by both rendering paths.
	FrontendBaseURL string
	// Scope carries the network-scope rendering policy (M5 private
	// deployment): a concrete intranet/extranet scope selects the scoped
	// template variant, and extranet additionally masks the message and
	// adapts the asset link onto the public domain. The zero value keeps
	// full rendering.
	Scope ScopeOptions
}

// Render produces a canonical Message from an alert event using the best
// available rendering path:
//
//  1. Template-based: when Library is non-nil and alert.TemplateID is
//     non-empty, Render uses template.NewRenderer to render the alert via
//     the Library. The Registry (when non-nil) supplies localized level
//     labels, field labels, and time formats.
//  2. Formatter-based: when a Formatter is configured, Render delegates to
//     BuildWithOpts for locale-aware rendering backed by the merged i18n
//     bundle (open-source + extended resource files).
//
// When neither path produces a Message (i.e. template rendering fails or
// is not applicable AND Formatter is nil), Render returns a zero-value
// Message and logs an error. This indicates a misconfiguration: the
// service layer should always inject a Formatter at startup.
//
// This helper centralizes the two-tier dispatch so individual channel
// adapters can call a single function instead of duplicating the logic.
func Render(ctx context.Context, evt alert.Event, opts RenderOptions) Message {
	logger := opts.Logger
	if logger == nil {
		logger = zap.NewNop()
	}

	// 1. Template-based rendering takes precedence when a template ID is set.
	if opts.Library != nil && evt.TemplateID != "" {
		r := template.NewRenderer(opts.Library, opts.Registry, logger)
		formatted, err := r.Render(ctx, evt, template.RenderOptions{
			TemplateID:      evt.TemplateID,
			Locale:          evt.Locale,
			FrontendBaseURL: opts.FrontendBaseURL,
			NetworkScope:    opts.Scope.NetworkScope,
		})
		if err == nil {
			return applyScope(formattedToMessage(formatted, evt), opts)
		}
		logger.Warn("format: template render failed, falling back to formatter",
			zap.String("template_id", evt.TemplateID),
			zap.Error(err),
		)
	}

	// 2. Formatter-based rendering.
	if opts.Formatter != nil {
		return applyScope(BuildWithOpts(ctx, evt, i18n.FormatOptions{
			Locale:          evt.Locale,
			FrontendBaseURL: opts.FrontendBaseURL,
		}, opts.Formatter, logger), opts)
	}

	// No rendering path available: Formatter is nil and template rendering
	// did not apply or failed. Return a zero-value Message and log an error
	// so the misconfiguration is visible.
	logger.Error("format: no formatter configured and template rendering not applicable; returning empty message",
		zap.String("alert_type", string(evt.Type)),
		zap.String("template_id", evt.TemplateID),
	)
	return Message{}
}

// formattedToMessage converts an i18n.FormattedMessage (produced by the
// template renderer) into a canonical Message. The alert's
// timestamp and asset link are carried over; the locale is derived from
// the alert or defaults to the i18n.DefaultLocale ("zh-Hans").
func formattedToMessage(formatted i18n.FormattedMessage, evt alert.Event) Message {
	locale := evt.Locale
	if locale == "" {
		locale = i18n.DefaultLocale
	}
	msg := Message{
		Title:       formatted.Title,
		Level:       formatted.Level,
		Description: formatted.Description,
		Timestamp:   evt.Timestamp,
		AssetLink:   formatted.AssetLink,
		Fields:      formatted.Fields,
		Direction:   string(formatted.Direction),
		Locale:      locale,
	}
	if msg.Title == "" {
		switch evt.Type {
		case alert.TypeMetric:
			msg.Title = alert.MetricName(evt)
		case alert.TypeLog:
			msg.Title = alert.Keyword(evt)
		default:
			msg.Title = string(evt.Type)
		}
	}
	if msg.Fields == nil {
		msg.Fields = make(map[string]string)
	}
	return msg
}
