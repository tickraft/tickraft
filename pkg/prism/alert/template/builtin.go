// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package template

import (
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/prism/alert/template/builtin"
)

// templateFile is the intermediate struct used to unmarshal a TOML template
// file. It mirrors the Template struct but uses TOML-compatible field tags.
type templateFile struct {
	ID           string                       `toml:"id"`
	Name         string                       `toml:"name"`
	Description  string                       `toml:"description"`
	AlertType    string                       `toml:"alert_type"`
	Variables    []string                     `toml:"variables"`
	Styles       []string                     `toml:"styles"`
	ChannelHints []string                     `toml:"channel_hints"`
	Translations map[string]map[string]string `toml:"translations"`
}

// loadBuiltinTemplates walks the builtin embed.FS, parses each .toml file
// into a Template, and returns the sorted slice. Parse failures are logged
// via the provided logger and the offending file is skipped so a single
// malformed template does not break the entire library. A nil logger is
// replaced with a no-op logger.
//
// The returned templates are not validated; callers (e.g. NewBuiltinLibrary)
// are responsible for invoking Validate before registration.
func loadBuiltinTemplates(logger *zap.Logger) []Template {
	if logger == nil {
		logger = zap.NewNop()
	}

	fsys := builtin.EmbeddedFS()
	var templates []Template

	entries, err := fs.ReadDir(fsys, "templates")
	if err != nil {
		// This should never happen with an embed.FS, but handle it
		// defensively by returning an empty slice.
		logger.Warn("template builtin: failed to read embedded templates directory",
			zap.Error(err),
		)
		return nil
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.ToLower(filepath.Ext(name)) != ".toml" {
			continue
		}
		path := filepath.Join("templates", name)
		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			logger.Warn("template builtin: failed to read embedded template file",
				zap.String("path", path),
				zap.Error(err),
			)
			continue
		}

		var tf templateFile
		if err := toml.Unmarshal(data, &tf); err != nil {
			logger.Warn("template builtin: failed to parse template file",
				zap.String("path", path),
				zap.Error(err),
			)
			continue
		}

		templates = append(templates, Template{
			ID:           tf.ID,
			Name:         tf.Name,
			Description:  tf.Description,
			AlertType:    tf.AlertType,
			Variables:    tf.Variables,
			Translations: tf.Translations,
			Styles:       tf.Styles,
			ChannelHints: tf.ChannelHints,
		})
	}

	sort.Slice(templates, func(i, j int) bool {
		return templates[i].ID < templates[j].ID
	})

	return templates
}
