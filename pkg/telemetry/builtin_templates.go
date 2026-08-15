// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package telemetry

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"sort"

	"gorm.io/gorm"
)

// builtinTemplateFS embeds the built-in template JSON files from the
// templates/ directory so the binary is self-contained without external
// file dependencies at runtime.
//
//go:embed templates/*.json
var builtinTemplateFS embed.FS

// builtinTemplateFile is the on-disk JSON structure of a built-in template.
// The Config field is kept as raw JSON so it can be stored verbatim as a
// string in Template.Config without imposing a fixed schema.
type builtinTemplateFile struct {
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	Category     string          `json:"category"`
	ExecutorType string          `json:"executor_type"`
	Config       json.RawMessage `json:"config"`
}

// builtinTemplateNames lists the built-in template JSON files seeded by the
// CE kernel. Only prober types the CE runtime actually supports are
// included; the pro-edition templates are listed separately in
// proBuiltinTemplateNames for callers (tickraft-x) that support them.
var builtinTemplateNames = []string{
	"icmp-ping.json",
	"http-homepage.json",
	"https-api.json",
	"tcp-database.json",
}

// proBuiltinTemplateNames lists built-in template JSON files whose prober
// types (dns, ssl, redis, mysql) are only available in the pro edition.
// They remain embedded in the shared binary but are not seeded by CE.
var proBuiltinTemplateNames = []string{
	"dns-resolution.json",
	"ssl-certificate.json",
	"redis-connect.json",
	"mysql-connect.json",
}

// LoadBuiltinTemplates loads the CE built-in templates into the database if
// they do not already exist. It is idempotent: templates that already exist
// (by name) are skipped, so it is safe to call on every startup. The
// function also runs AutoMigrate for the template table to ensure the schema
// exists before inserting, and removes any previously seeded pro-edition
// builtin templates so the CE UI only shows templates the CE runtime can
// actually execute.
func LoadBuiltinTemplates(dbc *gorm.DB) error {
	if err := loadTemplates(dbc, builtinTemplateNames); err != nil {
		return err
	}

	// Drop pro-edition builtin templates seeded by older builds; CE cannot
	// run their prober types.
	var proNames []string
	for _, name := range proBuiltinTemplateNames {
		t, err := readBuiltinTemplate(name)
		if err != nil {
			return err
		}
		proNames = append(proNames, t.Name)
	}
	if len(proNames) > 0 {
		if err := dbc.Where("is_builtin = ? AND name IN ?", true, proNames).
			Delete(&Template{}).Error; err != nil {
			return fmt.Errorf("telemetry: remove pro builtin templates: %w", err)
		}
	}
	return nil
}

// LoadAllBuiltinTemplates loads both the CE and the pro-edition built-in
// templates. It is intended for pro-edition runtimes that support the
// dns/ssl/redis/mysql prober types.
func LoadAllBuiltinTemplates(dbc *gorm.DB) error {
	return loadTemplates(dbc, append(append([]string{}, builtinTemplateNames...), proBuiltinTemplateNames...))
}

// loadTemplates seeds the given template files. Templates that already
// exist (by name) are skipped.
func loadTemplates(dbc *gorm.DB, names []string) error {
	if dbc == nil {
		return fmt.Errorf("telemetry: load builtin templates: db is nil")
	}

	if err := dbc.AutoMigrate(&Template{}); err != nil {
		return fmt.Errorf("telemetry: migrate template table: %w", err)
	}

	templates := make([]builtinTemplateFile, 0, len(names))
	for _, name := range names {
		t, err := readBuiltinTemplate(name)
		if err != nil {
			return err
		}
		templates = append(templates, t)
	}

	for _, t := range templates {
		// Check by name to avoid re-inserting on restarts.
		var count int64
		if err := dbc.Model(&Template{}).Where("name = ?", t.Name).Count(&count).Error; err != nil {
			return fmt.Errorf("telemetry: check builtin template %q: %w", t.Name, err)
		}
		if count > 0 {
			continue
		}

		model := Template{
			Name:         t.Name,
			Description:  t.Description,
			Category:     t.Category,
			ExecutorType: t.ExecutorType,
			Config:       string(t.Config),
			IsBuiltin:    true,
		}
		if err := dbc.Create(&model).Error; err != nil {
			return fmt.Errorf("telemetry: insert builtin template %q: %w", t.Name, err)
		}
	}

	return nil
}

// readBuiltinTemplate reads and parses a single embedded template file.
func readBuiltinTemplate(name string) (builtinTemplateFile, error) {
	data, err := builtinTemplateFS.ReadFile("templates/" + name)
	if err != nil {
		return builtinTemplateFile{}, fmt.Errorf("telemetry: read builtin template %q: %w", name, err)
	}
	var t builtinTemplateFile
	if err := json.Unmarshal(data, &t); err != nil {
		return builtinTemplateFile{}, fmt.Errorf("telemetry: parse builtin template %q: %w", name, err)
	}
	return t, nil
}

// ListEmbeddedTemplateFiles returns the names of the embedded template JSON
// files, sorted alphabetically. It is primarily useful for diagnostics and
// tests.
func ListEmbeddedTemplateFiles() ([]string, error) {
	entries, err := fs.ReadDir(builtinTemplateFS, "templates")
	if err != nil {
		return nil, fmt.Errorf("telemetry: read embedded template dir: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}
