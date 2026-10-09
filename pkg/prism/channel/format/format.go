// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package format converts an alert.Event into a canonical Message
// shared by all notification channels. Each channel adapter
// translates the canonical Message into its platform-specific payload
// (Slack Block Kit, DingTalk markdown, Feishu interactive card, etc.),
// keeping alert rendering consistent across channels.
//
// The package provides the locale-aware BuildWithOpts path that
// delegates to the open-source pkg/channel/format Formatter, producing
// localized titles, descriptions, level labels, and field labels.
// Extended resource keys (Feishu card footers, Discord embed colors,
// Telegram Markdown markers) are loaded from the locales/ directory and
// merged into the same Registry so that channel adapters can look up
// platform-specific labels through the same i18n.ResolveKey API.
package format

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/i18n"
	"github.com/tickraft/tickraft/pkg/prism/alert"
)

// Message is the canonical, channel-agnostic representation of an alert
// produced from an alert.Event. Channels adapt this struct into
// their native payload format.
type Message struct {
	// Title is the short headline of the alert.
	Title string
	// Level is the severity label, e.g. "critical", "warning", "info".
	Level string
	// Description is a human-readable summary of what triggered the alert.
	Description string
	// Timestamp is when the alert was generated.
	Timestamp time.Time
	// AssetLink is an absolute URL to the asset detail page in the
	// frontend. It is empty when no FrontendBaseURL is configured.
	AssetLink string
	// LinkNote is a localized annotation appended next to AssetLink by the
	// text and markdown renderers, e.g. the intranet-address hint set when
	// an extranet-scoped message has no public domain to rebase onto.
	// Empty means no annotation.
	LinkNote string
	// Fields holds structured key/value facts about the alert for
	// rendering in channel-specific detail sections.
	Fields map[string]string
	// Direction is the text direction ("ltr" or "rtl") for the recipient's
	// locale. Channels use this to set card layout direction or flip
	// markdown alignment. Populated by BuildWithOpts.
	Direction string
	// Locale is the BCP 47 locale tag used to render this message. Populated
	// by BuildWithOpts.
	Locale string
}

// BuildWithOpts converts an alert.Event into a locale-aware Message
// using the open-source i18n Formatter. The opts.Locale field controls the
// output language; opts.Style controls the verbosity. When opts.Locale is
// empty, the alert's Locale field is used; when both are empty, the default
// locale "zh-Hans" (i18n.DefaultLocale) is used.
//
// The formatter parameter allows callers to inject a pre-configured
// Formatter backed by a merged Registry (open-source + extended resource
// files). When nil, a default Formatter backed by the built-in i18n bundle
// is constructed.
func BuildWithOpts(
	ctx context.Context,
	evt alert.Event,
	opts i18n.FormatOptions,
	formatter i18n.Formatter,
	logger *zap.Logger,
) Message {
	if formatter == nil {
		// Construct a default Formatter backed by the built-in i18n
		// resource bundle. An empty Registry alone would fall back to
		// key names, so the embedded resources are loaded explicitly.
		if logger == nil {
			logger = zap.NewNop()
		}
		registry := i18n.NewRegistry(logger)
		loader := i18n.NewLoader(logger)
		if err := loader.LoadToRegistry(i18n.EmbeddedFS(), registry); err != nil {
			logger.Warn("format: load builtin i18n resources failed",
				zap.Error(err),
			)
		}
		formatter = i18n.NewDefaultFormatter(registry, logger)
	}

	// Alert.Locale takes precedence when opts.Locale is not set.
	if opts.Locale == "" && evt.Locale != "" {
		opts.Locale = evt.Locale
	}

	formatted := formatter.Format(ctx, evt, opts)

	msg := Message{
		Title:       formatted.Title,
		Level:       formatted.Level,
		Description: formatted.Description,
		Timestamp:   evt.Timestamp,
		AssetLink:   formatted.AssetLink,
		Fields:      formatted.Fields,
		Direction:   string(formatted.Direction),
		Locale:      opts.Locale,
	}
	if msg.Locale == "" {
		msg.Locale = i18n.DefaultLocale
	}

	// When the Formatter produced an empty title (e.g. missing keys),
	// fall back to a derived title so channels always have a headline.
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

	// Ensure Fields is non-nil for channel adapters that iterate it.
	if msg.Fields == nil {
		msg.Fields = make(map[string]string)
	}

	return msg
}

// messageStyle describes how renderMessage lays out a canonical Message.
// The plain-text renderer uses an empty prefix and bold marker; markdown
// renderers use a line prefix (e.g. "- " or "> ") and a "**" bold marker.
type messageStyle struct {
	headerFormat string
	prefix       string
	boldMarker   string
	linkFormat   string
}

// renderMessage renders a canonical Message as a text or markdown summary.
// The asset_id field (when present) is promoted to the Asset line;
// remaining non-empty fields are appended as "key: value" lines (text) or
// prefixed bold key/value lines (markdown). The AssetLink is appended last
// when present, followed by the LinkNote annotation in parentheses when
// set.
func renderMessage(msg Message, style messageStyle) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, style.headerFormat, msg.Title)
	if resourceID, ok := msg.Fields["asset_id"]; ok && resourceID != "" {
		fmt.Fprintf(&sb, "%s%sAsset%s: %s\n", style.prefix, style.boldMarker, style.boldMarker, resourceID)
	}
	if msg.Level != "" {
		fmt.Fprintf(&sb, "%s%sLevel%s: %s\n", style.prefix, style.boldMarker, style.boldMarker, msg.Level)
	}
	if msg.Description != "" {
		fmt.Fprintf(&sb, "%s%sDetail%s: %s\n", style.prefix, style.boldMarker, style.boldMarker, msg.Description)
	}
	fmt.Fprintf(&sb, "%s%sTime%s: %s\n", style.prefix, style.boldMarker, style.boldMarker,
		msg.Timestamp.Format(time.RFC3339))
	for k, v := range msg.Fields {
		if k == "asset_id" || v == "" {
			continue
		}
		fmt.Fprintf(&sb, "%s%s%s%s: %s\n", style.prefix, style.boldMarker, k, style.boldMarker, v)
	}
	if msg.AssetLink != "" {
		fmt.Fprintf(&sb, style.linkFormat, msg.AssetLink)
		if msg.LinkNote != "" {
			fmt.Fprintf(&sb, "%s(%s)\n", style.prefix, msg.LinkNote)
		}
	}
	return sb.String()
}

// RenderText renders a canonical Message as a plain-text notification
// summary. The asset_id field (when present) is promoted to the Asset
// line; remaining non-empty fields are appended as "key: value" lines.
// The AssetLink is appended last when present.
func RenderText(msg Message) string {
	return renderMessage(msg, messageStyle{
		headerFormat: "[Tickraft Alert] %s\n",
		linkFormat:   "Link: %s\n",
	})
}

// MarkdownStyle controls the Markdown rendering of a Message by channel
// adapters (DingTalk, WeCom, ...).
type MarkdownStyle struct {
	// HeaderFormat is the fmt format applied to the title line, for
	// example "### Tickraft Alert: %s\n\n" (DingTalk) or
	// "## Tickraft Alert: %s\n" (WeCom).
	HeaderFormat string
	// LinePrefix is prepended to the asset, level, detail, time, and
	// field lines, for example "- " (DingTalk bullets) or "> " (WeCom
	// blockquotes).
	LinePrefix string
	// LinkFormat is the fmt format applied to the AssetLink line, for
	// example "- [View Asset](%s)\n" (DingTalk).
	LinkFormat string
}

// RenderMarkdown renders a canonical Message as a markdown notification
// summary using the given style. The asset_id field (when present) is
// promoted to the Asset line; remaining non-empty fields are appended as
// prefixed key/value pairs. The AssetLink is rendered with LinkFormat when
// present.
func RenderMarkdown(msg Message, style MarkdownStyle) string {
	return renderMessage(msg, messageStyle{
		headerFormat: style.HeaderFormat,
		prefix:       style.LinePrefix,
		boldMarker:   "**",
		linkFormat:   style.LinkFormat,
	})
}

// intranetLinkHintKey is the i18n key resolving to the localized label
// appended next to resource links in plain-notification (L0) mode.
const intranetLinkHintKey = "notify.link_intranet_hint"

// IntranetLinkHint returns the localized annotation marking a resource
// link as an intranet address. Channels rendering in plain-notification
// (L0) mode append it so recipients understand why the link only resolves
// inside the deployment network. Falls back to the English label when no
// registry is injected or the key is missing from every bundle.
func IntranetLinkHint(registry i18n.Registry, locale string) string {
	if registry != nil {
		if v := i18n.ResolveKey(registry.Resolve(locale), intranetLinkHintKey, nil); v != intranetLinkHintKey {
			return v
		}
	}
	return "intranet address"
}
