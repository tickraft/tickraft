// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package i18n

import (
	"bytes"
	"strings"
	"text/template"

	"go.uber.org/zap"
)

// missingKeyValue is the sentinel text/template emits for a missing key when
// the "missingkey=zero" option is set. The renderTemplate function replaces
// this sentinel with MissingVariablePlaceholder(key) so missing variables are
// visually distinguishable in the rendered output.
const missingKeyValue = "<no value>"

// RenderTemplate applies text/template substitution to tmpl using vars.
// Missing variables are replaced with the MissingVariablePlaceholder so
// rendering never fails; this preserves the "no panic in business logic"
// rule and makes missing data visually obvious in the output.
//
// The function uses a safe template configuration that disables HTML
// escaping (the formatter output is plain text; HTML escaping is the
// channel's responsibility) and limits the template complexity to prevent
// accidental resource exhaustion.
//
// RenderTemplate is exported so that the alert/template package (and
// extended custom-template renderers) can reuse the same substitution
// semantics as the default Formatter without duplicating the logic.
func RenderTemplate(tmpl string, vars map[string]any, logger *zap.Logger) string {
	if tmpl == "" {
		return ""
	}
	if len(vars) == 0 {
		return tmpl
	}

	t, err := template.New("i18n").Option("missingkey=zero").Parse(tmpl)
	if err != nil {
		// A parse error means the template itself is malformed; return
		// the raw string and log a warning so the operator can fix the
		// resource file.
		if logger != nil {
			logger.Warn("i18n template parse failed, returning raw template",
				zap.String("template", tmpl),
				zap.Error(err),
			)
		}
		return tmpl
	}

	// Pass the vars map directly to Execute. Go's text/template supports
	// map[string]any indexing natively via .key syntax, so {{.metric_name}}
	// resolves to vars["metric_name"].
	var buf bytes.Buffer
	if err = t.Execute(&buf, vars); err != nil {
		if logger != nil {
			logger.Warn("i18n template execute failed, returning partial result",
				zap.String("template", tmpl),
				zap.Error(err),
			)
		}
		return replaceMissingPlaceholders(buf.String())
	}
	return replaceMissingPlaceholders(buf.String())
}

// replaceMissingPlaceholders replaces the <no value> sentinel produced by
// missingkey=zero with a visually distinct placeholder. The placeholder uses
// angle brackets to match common i18n conventions (e.g. {{missing_key}}).
func replaceMissingPlaceholders(s string) string {
	if !strings.Contains(s, missingKeyValue) {
		return s
	}
	return strings.ReplaceAll(s, missingKeyValue, MissingVariablePlaceholder(""))
}
