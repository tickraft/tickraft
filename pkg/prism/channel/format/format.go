// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package format

import (
	"context"

	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/i18n"
	"github.com/tickraft/tickraft/pkg/prism/alert"
)

// NewFormatter creates the default Formatter backed by the given
// Registry. A nil logger is replaced with a no-op logger and a nil Registry
// is replaced with an empty one so that callers always get a usable
// Formatter. Channels should call this once at construction time and reuse
// the returned Formatter across Send calls; the Formatter is safe for
// concurrent use.
func NewFormatter(r i18n.Registry, logger *zap.Logger) i18n.Formatter {
	if logger == nil {
		logger = zap.NewNop()
	}
	if r == nil {
		r = i18n.NewRegistry(logger)
	}
	return i18n.NewDefaultFormatter(r, logger)
}

// Build renders evt with default options (Locale=i18n.DefaultLocale, Style=detailed) and
// the given frontendBaseURL. It is the convenience entry point for channels
// that do not need per-recipient locale customization. Channels that need to
// honor a recipient's locale or style preference should call BuildWithOpts
// instead.
func Build(evt alert.Event, frontendBaseURL string, f i18n.Formatter) i18n.FormattedMessage {
	if f == nil {
		f = NewFormatter(nil, nil)
	}
	return f.Format(context.Background(), evt, i18n.FormatOptions{
		Locale:          i18n.DefaultLocale,
		Style:           i18n.StyleDetailed,
		FrontendBaseURL: frontendBaseURL,
	})
}

// BuildWithOpts is the primary rendering entry point for channels that need
// to honor per-recipient locale, style, timezone, or frontend base URL
// settings. When f is nil, a default Formatter backed by an empty Registry
// is used; this produces English output with the built-in fallback keys,
// which is useful for unit tests but not for production.
func BuildWithOpts(evt alert.Event, opts i18n.FormatOptions, f i18n.Formatter) i18n.FormattedMessage {
	if f == nil {
		f = NewFormatter(nil, nil)
	}
	return f.Format(context.Background(), evt, opts)
}
