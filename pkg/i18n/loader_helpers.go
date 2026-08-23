// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package i18n

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/bytedance/sonic"
)

// flattenAny recursively walks the value, joining keys with dots. Non-map
// leaves are stringified via fmt.Sprint. It is used by both the TOML and
// JSON parsers to convert nested structures into a flat map[string]string.
//
// Example:
//
//	{
//	  "alert": {
//	    "metric": {"title": "CPU Alert", "value": 42}
//	  }
//	}
//
// becomes:
//
//	{
//	  "alert.metric.title": "CPU Alert",
//	  "alert.metric.value": "42"
//	}
func flattenAny(prefix string, in map[string]any, out map[string]string) {
	for k, v := range in {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}
		switch child := v.(type) {
		case map[string]any:
			flattenAny(key, child, out)
		case string:
			out[key] = child
		default:
			out[key] = fmt.Sprint(v)
		}
	}
}

// parseJSON parses JSON bytes into a flattened map[string]string.
func parseJSON(data []byte) (map[string]string, error) {
	var anyMap map[string]any
	if err := sonic.Unmarshal(data, &anyMap); err != nil {
		return nil, err
	}
	out := make(map[string]string, len(anyMap))
	flattenAny("", anyMap, out)
	return out, nil
}

// openOSFile opens a file on the host filesystem and returns it as an fs.File.
// It is used by osDirFS.Open to satisfy parseResourceFile's fs.FS contract.
func openOSFile(full string) (fs.File, error) {
	cleaned := filepath.Clean(full)
	return os.Open(cleaned)
}
