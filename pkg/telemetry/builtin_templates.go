// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package telemetry

import (
	"embed"
	"encoding/json"
	"fmt"

	"github.com/bytedance/sonic"
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

// readBuiltinTemplate reads and parses a single embedded template file.
func readBuiltinTemplate(name string) (builtinTemplateFile, error) {
	data, err := builtinTemplateFS.ReadFile("templates/" + name)
	if err != nil {
		return builtinTemplateFile{}, fmt.Errorf("telemetry: read builtin template %q: %w", name, err)
	}
	var t builtinTemplateFile
	if err := sonic.Unmarshal(data, &t); err != nil {
		return builtinTemplateFile{}, fmt.Errorf("telemetry: parse builtin template %q: %w", name, err)
	}
	return t, nil
}
