// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package email

import (
	"bytes"
	"context"
	"fmt"
	"mime"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/i18n"
	"github.com/tickraft/tickraft/pkg/prism/alert"
	"github.com/tickraft/tickraft/pkg/prism/alert/template"
)

// buildMessage constructs the raw SMTP message bytes (headers + body) from
// the alert event and channel configuration. When cfg.Library is non-nil and
// evt.TemplateID is non-empty, the message is rendered via the template
// Library; otherwise the cfg.Formatter is used. When both are nil, a default
// Formatter backed by the built-in i18n bundle is used so that
// deployments produce localized output out of the box.
func buildMessage(ctx context.Context, evt alert.Event, cfg Config, logger *zap.Logger) []byte {
	formatted := renderAlert(ctx, evt, cfg, logger)

	subject := formatted.Title
	if subject == "" {
		subject = formatSubject(evt)
	}

	plainBody := formatPlainText(evt, formatted)
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "From: %s\r\n", cfg.From)
	fmt.Fprintf(&buf, "To: %s\r\n", strings.Join(cfg.To, ", "))
	fmt.Fprintf(&buf, "Subject: %s\r\n", mime.QEncoding.Encode("UTF-8", subject))
	fmt.Fprintf(&buf, "MIME-Version: 1.0\r\n")
	fmt.Fprintf(&buf, "Date: %s\r\n", time.Now().Format(time.RFC1123Z))
	fmt.Fprintf(&buf, "Message-ID: <%d.%d@tickraft>\r\n", evt.AssetID, time.Now().UnixNano())

	if cfg.HTMLMode {
		htmlBody := formatHTML(evt, formatted)
		boundary := fmt.Sprintf("tickraft-boundary-%d", time.Now().UnixNano())
		fmt.Fprintf(&buf, "Content-Type: multipart/alternative; boundary=%s\r\n", boundary)
		fmt.Fprintf(&buf, "\r\n")

		fmt.Fprintf(&buf, "--%s\r\n", boundary)
		fmt.Fprintf(&buf, "Content-Type: text/plain; charset=UTF-8\r\n")
		fmt.Fprintf(&buf, "Content-Transfer-Encoding: 8bit\r\n")
		fmt.Fprintf(&buf, "\r\n")
		buf.WriteString(plainBody)
		buf.WriteString("\r\n")

		fmt.Fprintf(&buf, "--%s\r\n", boundary)
		fmt.Fprintf(&buf, "Content-Type: text/html; charset=UTF-8\r\n")
		fmt.Fprintf(&buf, "Content-Transfer-Encoding: 8bit\r\n")
		fmt.Fprintf(&buf, "\r\n")
		buf.WriteString(htmlBody)
		buf.WriteString("\r\n")

		fmt.Fprintf(&buf, "--%s--\r\n", boundary)
	} else {
		fmt.Fprintf(&buf, "Content-Type: text/plain; charset=UTF-8\r\n")
		fmt.Fprintf(&buf, "Content-Transfer-Encoding: 8bit\r\n")
		fmt.Fprintf(&buf, "\r\n")
		buf.WriteString(plainBody)
	}

	return buf.Bytes()
}

// buildDefaultFormatter constructs a Formatter backed by the built-in i18n
// asset bundle. It is used by New when no custom Formatter is injected so
// that deployments produce localized output out of the box
// without re-loading the bundle on every Send call.
func buildDefaultFormatter(logger *zap.Logger) i18n.Formatter {
	if logger == nil {
		logger = zap.NewNop()
	}
	loader := i18n.NewLoader(logger)
	registry := i18n.NewRegistry(logger)
	if err := loader.LoadToRegistry(i18n.EmbeddedFS(), registry); err != nil {
		logger.Warn("email: load builtin i18n resources failed",
			zap.Error(err),
		)
	}
	return i18n.NewDefaultFormatter(registry, logger)
}

// renderAlert produces the localized FormattedMessage for the alert. When
// cfg.Library is non-nil and evt.TemplateID is non-empty, it calls
// Library.Render; otherwise it falls back to cfg.Formatter.Format. When
// both are nil, a default Formatter backed by the built-in i18n bundle is
// constructed so that callers who build a Config directly (without going
// through New) still get localized output without explicit injection.
func renderAlert(ctx context.Context, evt alert.Event, cfg Config, logger *zap.Logger) i18n.FormattedMessage {
	locale := evt.Locale
	if locale == "" {
		locale = i18n.DefaultLocale
	}
	opts := i18n.FormatOptions{
		Locale: locale,
		Style:  i18n.StyleDetailed,
	}

	// Template-based rendering takes precedence when both Library and
	// TemplateID are available.
	if cfg.Library != nil && evt.TemplateID != "" {
		r := template.NewRenderer(cfg.Library, nil, logger)
		msg, err := r.Render(ctx, evt, template.RenderOptions{
			TemplateID: evt.TemplateID,
			Locale:     locale,
			Style:      template.StyleDetailed,
		})
		if err == nil {
			return msg
		}
		if logger != nil {
			logger.Warn("email: template render failed, falling back to formatter",
				zap.String("template_id", evt.TemplateID),
				zap.Error(err),
			)
		}
	}

	// Formatter-based rendering. New normally caches a default Formatter
	// at construction time; the lazy fallback here is a safety net for
	// callers who construct Config directly.
	formatter := cfg.Formatter
	if formatter == nil {
		formatter = buildDefaultFormatter(logger)
	}
	return formatter.Format(ctx, evt, opts)
}

// formatSubject builds a fallback email subject from the alert event when no
// localized title is available from the Formatter or Library. It uses the
// primary (most severe) violation to ensure the subject reflects the most
// important issue in multi-violation events.
func formatSubject(evt alert.Event) string {
	var v alert.Violation
	if p := alert.PrimaryViolation(evt); p != nil {
		v = *p
	}
	switch evt.Type {
	case alert.TypeMetric:
		if v.Metric != nil && v.Metric.Name != "" {
			return fmt.Sprintf("[Alert] %s %.2f (threshold %.2f) - asset %d",
				v.Metric.Name, v.Metric.Value, v.Metric.Threshold, evt.AssetID)
		}
		return fmt.Sprintf("[Alert] metric alert - asset %d", evt.AssetID)
	case alert.TypeLog:
		if v.Log != nil && v.Log.Keyword != "" {
			return fmt.Sprintf("[Alert] log keyword %q matched - asset %d", v.Log.Keyword, evt.AssetID)
		}
		return fmt.Sprintf("[Alert] log alert - asset %d", evt.AssetID)
	case alert.TypeHeartbeat:
		return fmt.Sprintf("[Alert] heartbeat lost - asset %d", evt.AssetID)
	case alert.TypeStatus:
		if v.Status != nil {
			return fmt.Sprintf("[Alert] status %s -> %s - asset %d",
				v.Status.PrevStatus, v.Status.CurrStatus, evt.AssetID)
		}
		return fmt.Sprintf("[Alert] status change - asset %d", evt.AssetID)
	default:
		return fmt.Sprintf("[Alert] %s - asset %d", evt.Type, evt.AssetID)
	}
}

// formatPlainText builds a plain-text email body containing the localized
// alert message followed by the structured fields. The localized
// description (when available) is placed at the top as a summary; the
// structured fields always follow so that automated parsers and tests can
// rely on the stable field labels.
// All violations in the event are rendered, not just the first one.
func formatPlainText(evt alert.Event, formatted i18n.FormattedMessage) string {
	var buf bytes.Buffer

	if formatted.Description != "" {
		buf.WriteString(formatted.Description)
		buf.WriteString("\n\n")
	}

	fmt.Fprintf(&buf, "Alert type: %s\n", evt.Type)
	fmt.Fprintf(&buf, "Asset ID: %d\n", evt.AssetID)
	fmt.Fprintf(&buf, "Tenant ID: %d\n", evt.TenantID)
	if formatted.Timestamp != "" {
		fmt.Fprintf(&buf, "Time: %s\n", formatted.Timestamp)
	} else {
		fmt.Fprintf(&buf, "Time: %s\n", evt.Timestamp.Format(time.RFC3339))
	}

	// Render all violations
	for i, v := range evt.Violations {
		if len(evt.Violations) > 1 {
			fmt.Fprintf(&buf, "\n--- Violation %d ---\n", i+1)
		}
		renderViolationPlainText(&buf, v, evt.Type)
	}

	if formatted.AssetLink != "" {
		fmt.Fprintf(&buf, "\nAsset link: %s\n", formatted.AssetLink)
	}

	return buf.String()
}

// renderViolationPlainText writes a single violation as plain text fields.
func renderViolationPlainText(buf *bytes.Buffer, v alert.Violation, eventType alert.Type) {
	if eventType == alert.TypeMetric && v.Metric != nil {
		fmt.Fprintf(buf, "Metric name: %s\n", v.Metric.Name)
		fmt.Fprintf(buf, "Metric value: %.2f\n", v.Metric.Value)
		fmt.Fprintf(buf, "Threshold: %.2f\n", v.Metric.Threshold)
		if len(v.Metric.Metrics) > 0 {
			buf.WriteString("Related resources:\n")
			for k, rv := range v.Metric.Metrics {
				fmt.Fprintf(buf, "  %s: %.2f\n", k, rv)
			}
		}
	}
	if eventType == alert.TypeLog {
		fmt.Fprintf(buf, "Level: %s\n", v.Severity)
		if v.Log != nil {
			fmt.Fprintf(buf, "Keyword: %s\n", v.Log.Keyword)
			fmt.Fprintf(buf, "Content: %s\n", v.Log.Content)
		}
		fmt.Fprintf(buf, "Source IP: %s\n", v.Source)
	}
	if eventType == alert.TypeHeartbeat || eventType == alert.TypeStatus {
		fmt.Fprintf(buf, "Severity: %s\n", v.Severity)
		if v.Status != nil {
			fmt.Fprintf(buf, "Previous status: %s\n", v.Status.PrevStatus)
			fmt.Fprintf(buf, "Current status: %s\n", v.Status.CurrStatus)
		}
		if v.Message != "" {
			fmt.Fprintf(buf, "Message: %s\n", v.Message)
		}
	}
}

// formatHTML builds an HTML email body with a table of alert fields. The root
// div carries a dir attribute derived from formatted.Direction so that RTL
// locales (ar, he) render correctly in mail clients.
// All violations in the event are rendered, not just the first one.
func formatHTML(evt alert.Event, formatted i18n.FormattedMessage) string {
	var buf bytes.Buffer
	dir := string(formatted.Direction)
	if dir == "" {
		dir = "ltr"
	}
	fmt.Fprintf(&buf, `<div dir="%s" style="font-family: Arial, sans-serif; font-size: 14px; color: #333;">`, dir)

	if formatted.Description != "" {
		fmt.Fprintf(&buf, `<p style="margin: 0 0 12px;">%s</p>`, escapeHTML(formatted.Description))
	}

	buf.WriteString(`<table style="border-collapse: collapse; width: 100%; max-width: 600px;">`)
	writeHTMLRow(&buf, "Alert type", string(evt.Type))
	writeHTMLRow(&buf, "Asset ID", fmt.Sprintf("%d", evt.AssetID))
	writeHTMLRow(&buf, "Tenant ID", fmt.Sprintf("%d", evt.TenantID))
	if formatted.Timestamp != "" {
		writeHTMLRow(&buf, "Time", formatted.Timestamp)
	} else {
		writeHTMLRow(&buf, "Time", evt.Timestamp.Format(time.RFC3339))
	}

	// Render all violations
	for i, v := range evt.Violations {
		if len(evt.Violations) > 1 {
			fmt.Fprintf(&buf, `<tr><td colspan="2" style="border: 1px solid #ddd; padding: 8px; `+
				`background: #e6f7ff; font-weight: bold;">Violation %d</td></tr>`, i+1)
		}
		renderViolationHTML(&buf, v, evt.Type)
	}

	// Append localized fields from the FormattedMessage.
	for k, v := range formatted.Fields {
		writeHTMLRow(&buf, escapeHTML(k), escapeHTML(v))
	}

	buf.WriteString("</table>")

	if formatted.AssetLink != "" {
		fmt.Fprintf(&buf, `<p style="margin: 12px 0 0;"><a href="%s" style="color: #1890ff;">%s</a></p>`,
			escapeHTML(formatted.AssetLink), escapeHTML(formatted.AssetLink))
	}

	buf.WriteString("</div>")
	return buf.String()
}

// renderViolationHTML writes a single violation as HTML table rows.
func renderViolationHTML(buf *bytes.Buffer, v alert.Violation, eventType alert.Type) {
	if eventType == alert.TypeMetric && v.Metric != nil {
		writeHTMLRow(buf, "Metric name", v.Metric.Name)
		writeHTMLRow(buf, "Metric value", fmt.Sprintf("%.2f", v.Metric.Value))
		writeHTMLRow(buf, "Threshold", fmt.Sprintf("%.2f", v.Metric.Threshold))
		if len(v.Metric.Metrics) > 0 {
			for k, rv := range v.Metric.Metrics {
				writeHTMLRow(buf, "  "+escapeHTML(k), fmt.Sprintf("%.2f", rv))
			}
		}
	}
	if eventType == alert.TypeLog {
		writeHTMLRow(buf, "Level", v.Severity)
		if v.Log != nil {
			writeHTMLRow(buf, "Keyword", v.Log.Keyword)
			writeHTMLRow(buf, "Content", v.Log.Content)
		}
		writeHTMLRow(buf, "Source IP", v.Source)
	}
	if eventType == alert.TypeHeartbeat || eventType == alert.TypeStatus {
		writeHTMLRow(buf, "Severity", v.Severity)
		if v.Status != nil {
			writeHTMLRow(buf, "Previous status", v.Status.PrevStatus)
			writeHTMLRow(buf, "Current status", v.Status.CurrStatus)
		}
		if v.Message != "" {
			writeHTMLRow(buf, "Message", v.Message)
		}
	}
}

// writeHTMLRow writes a key-value row to the HTML table buffer.
func writeHTMLRow(buf *bytes.Buffer, key, value string) {
	fmt.Fprintf(buf, `<tr><td style="border: 1px solid #ddd; padding: 8px; background: #f5f5f5; `+
		`font-weight: bold;">%s</td><td style="border: 1px solid #ddd; padding: 8px;">%s</td></tr>`,
		escapeHTML(key), escapeHTML(value))
}

// escapeHTML replaces HTML special characters with their entity equivalents.
func escapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	return s
}
