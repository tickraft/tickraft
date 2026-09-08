// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package template

import (
	"errors"
	"fmt"
	"regexp"

	"github.com/tickraft/tickraft/pkg/i18n"
)

// variableNamePattern is the regex a template variable name must match:
// lowercase letter followed by lowercase letters, digits, or underscores.
var variableNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// templateIDPattern is the regex a template ID must match: lowercase letter
// or underscore followed by lowercase letters, digits, or underscores. This
// matches the lower_snake_case convention.
var templateIDPattern = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

// Validate checks that a Template is well-formed. It verifies:
//
//   - ID is non-empty and matches templateIDPattern.
//   - Name is non-empty.
//   - AlertType is one of the recognized values.
//   - Every variable name matches variableNamePattern.
//   - Styles are recognized and non-empty.
//   - At least one translation exists (the default locale i18n.DefaultLocale
//     is required).
//   - Every translation has a title and description key for every declared
//     style.
//
// Validate is called by NewBuiltinLibrary before registering each template;
// extended custom-template stores should call it before persisting a
// user-created template.
func Validate(t Template) error {
	if err := validateIdentity(t); err != nil {
		return err
	}
	if err := validateVariables(t); err != nil {
		return err
	}
	if err := validateStyles(t); err != nil {
		return err
	}
	return validateTranslations(t)
}

// validateIdentity checks the template ID, Name, and AlertType.
func validateIdentity(t Template) error {
	if t.ID == "" {
		return errEmptyTemplateID
	}
	if !templateIDPattern.MatchString(t.ID) {
		return fmt.Errorf("template: ID %q must match %s", t.ID, templateIDPattern.String())
	}
	if t.Name == "" {
		return errors.New("template: Name is required")
	}
	if !IsValidAlertType(t.AlertType) {
		return fmt.Errorf("template: invalid AlertType %q", t.AlertType)
	}
	if !IsValidNetworkScope(t.NetworkScope) {
		return fmt.Errorf("template: invalid NetworkScope %q", t.NetworkScope)
	}
	return nil
}

// validateVariables checks that every declared variable name matches
// variableNamePattern.
func validateVariables(t Template) error {
	for _, v := range t.Variables {
		if !variableNamePattern.MatchString(v) {
			return fmt.Errorf("template: invalid variable name %q (must match %s)", v, variableNamePattern.String())
		}
	}
	return nil
}

// validateStyles checks that at least one style is declared and every
// style is recognized.
func validateStyles(t Template) error {
	if len(t.Styles) == 0 {
		return errors.New("template: at least one Style is required")
	}
	for _, s := range t.Styles {
		if !IsValidStyle(s) {
			return fmt.Errorf("template: invalid Style %q", s)
		}
	}
	return nil
}

// validateTranslations checks that at least one translation exists, the
// default locale (i18n.DefaultLocale) translation is mandatory so that
// fallback always produces readable output, and every translation has a
// title and description key for every declared style.
func validateTranslations(t Template) error {
	if len(t.Translations) == 0 {
		return errors.New("template: at least one translation is required")
	}
	// The default locale (i18n.DefaultLocale) translation is mandatory so
	// that fallback always produces readable output.
	if _, ok := t.Translations[i18n.DefaultLocale]; !ok {
		return fmt.Errorf("template: translation for default locale %q is required", i18n.DefaultLocale)
	}
	for locale, tr := range t.Translations {
		for _, style := range t.Styles {
			titleKey := "title." + style
			descKey := "description." + style
			if _, ok := tr[titleKey]; !ok {
				return fmt.Errorf("template: locale %q missing key %q", locale, titleKey)
			}
			if _, ok := tr[descKey]; !ok {
				return fmt.Errorf("template: locale %q missing key %q", locale, descKey)
			}
		}
	}
	return nil
}
