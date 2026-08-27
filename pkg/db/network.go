// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package db

import (
	"fmt"
	"net/url"
	"strings"
)

// ParseNetworkDSN builds a Config for a network database (MySQL or
// PostgreSQL) from the "rest" portion of a DSN (the part after "scheme://").
//
// scheme is the original URL scheme (e.g. "mysql", "postgres"); it is used
// only to reassemble a valid URL for net/url parsing. driver is the canonical
// Config.Driver value to set on the returned Config (e.g. "mysql",
// "postgresql").
//
// The DSN is split as follows:
//   - userinfo (user[:pass]) is placed into cfg.Credential
//   - host:port is placed into cfg.Addr
//   - the leading "/db" path segment is placed into cfg.Params["database"]
//   - the query string is parsed into the remaining cfg.Params entries
//
// This is a generic URL-parsing helper — it does not import or depend on any
// database driver. Downstream services (callers, atlas, atrium) use it to
// avoid duplicating the same network-DSN parsing logic.
func ParseNetworkDSN(scheme, driver, rest string) (Config, error) {
	addressPart, params := SplitURLQuery(rest)
	if addressPart == "" {
		return Config{}, fmt.Errorf("db: parse %s url: %w", driver, ErrDSNRequired)
	}
	u, err := url.Parse(scheme + "://" + addressPart)
	if err != nil {
		return Config{}, fmt.Errorf("db: parse %s url: %w", driver, err)
	}
	cfg := Config{Driver: driver, Addr: u.Host, Params: params}
	if cfg.Params == nil {
		cfg.Params = make(map[string]string)
	}
	if u.User != nil {
		cfg.Credential.Username = u.User.Username()
		if pass, ok := u.User.Password(); ok && pass != "" {
			cfg.Credential.Password = pass
		}
	}
	if path := strings.TrimPrefix(u.Path, "/"); path != "" {
		cfg.Params["database"] = path
	}
	return cfg, nil
}
