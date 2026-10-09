// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package db

import (
	"regexp"
	"strings"
)

// redactedPlaceholder is the mask substituted for sensitive values when
// redacting a DSN for logging.
const redactedPlaceholder = "***"

// sensitiveQueryRegex matches query parameters whose names are considered
// secret. It works on both "?key=value" and "&key=value" forms and is
// case-insensitive.
var sensitiveQueryRegex = regexp.MustCompile(`(?i)([?&](?:password|pass|passwd|key|_key|secret|token|apikey)=)[^&]*`)

// mysqlUserinfoPasswordRegex matches the password in a MySQL driver-native
// DSN userinfo segment "user:password@" at the start of the string. The
// username character class excludes ":" and "@" so that the scheme separator
// in URL-style DSNs is not mistaken for the userinfo separator.
var mysqlUserinfoPasswordRegex = regexp.MustCompile(`^([^:@]*):[^@]*@`)

// urlUserinfoPasswordRegex matches the password in a URL-style userinfo
// segment "://user:password@". Group 1 captures the "://user" prefix.
var urlUserinfoPasswordRegex = regexp.MustCompile(`(://[^:@]*):[^@]*@`)

// postgresKeywordPasswordRegex matches the value of the "password" key in a
// PostgreSQL keyword/value DSN (whitespace-separated key=value pairs).
var postgresKeywordPasswordRegex = regexp.MustCompile(`(?i)(\bpassword=)\S+`)

// redactQueryParams masks the values of sensitive query parameters in a DSN
// string. It handles both "?key=value" and "&key=value" forms.
func redactQueryParams(dsn string) string {
	return sensitiveQueryRegex.ReplaceAllString(dsn, "${1}"+redactedPlaceholder)
}

// isMySQLNativeDSN reports whether dsn matches the go-sql-driver/mysql native
// form "user[:password]@tcp(host:port)/db" (or unix socket variant).
func isMySQLNativeDSN(dsn string) bool {
	return strings.Contains(dsn, "@tcp(") || strings.Contains(dsn, "@unix(")
}

// Redact returns a copy of the DSN with sensitive fields masked so that the
// result is safe to log. The DSN scheme determines which driver-specific
// redactor is applied; unrecognized schemes (including SQLite bare paths)
// are returned unchanged.
//
// This is a generic string-manipulation utility — it does not import or
// depend on any database driver. It recognizes PostgreSQL and MySQL DSN
// forms (both URL-style and native) so that downstream services (atlas,
// atrium, callers) can share a single implementation rather than
// duplicating the redaction regexes.
func Redact(dsn string) string {
	if dsn == "" {
		return ""
	}
	lower := strings.ToLower(dsn)
	if strings.HasPrefix(lower, "postgres://") || strings.HasPrefix(lower, "postgresql://") {
		return redactPostgreSQLDSN(dsn)
	}
	if strings.HasPrefix(lower, "mysql://") || isMySQLNativeDSN(dsn) {
		return redactMySQLDSN(dsn)
	}
	return dsn
}

// redactMySQLDSN returns a copy of a MySQL DSN with sensitive fields masked
// so that the result is safe to log.
//
// Both the URL form "mysql://user:password@host:port/db?params" and the
// driver-native form "user:password@tcp(host:port)/db?params" are supported.
// The password in the userinfo segment and sensitive query parameters
// (password, pass, passwd, key, _key, secret, token, apikey) are masked.
// If there is no password in the userinfo (no ":" before "@"), that segment
// is left unchanged.
func redactMySQLDSN(dsn string) string {
	if dsn == "" {
		return ""
	}
	masked := redactURLUserinfoPassword(dsn)
	if masked == dsn {
		masked = mysqlUserinfoPasswordRegex.ReplaceAllString(dsn, "${1}:"+redactedPlaceholder+"@")
	}
	if strings.Contains(masked, "?") {
		masked = redactQueryParams(masked)
	}
	return masked
}

// redactPostgreSQLDSN returns a copy of a PostgreSQL DSN with sensitive
// fields masked so that the result is safe to log.
//
// Two DSN forms are supported:
//
//   - URL form: "postgres://user:password@host:port/database?params" or
//     "postgresql://user:password@host:port/database?params". Both the
//     userinfo password and sensitive query parameters are masked.
//   - Keyword/value form: "host=localhost password=secret dbname=postgres".
//     Only the value of the "password" key is masked.
func redactPostgreSQLDSN(dsn string) string {
	if dsn == "" {
		return ""
	}
	lower := strings.ToLower(dsn)
	if strings.HasPrefix(lower, "postgres://") || strings.HasPrefix(lower, "postgresql://") {
		masked := redactURLUserinfoPassword(dsn)
		if strings.Contains(masked, "?") {
			masked = redactQueryParams(masked)
		}
		return masked
	}
	return postgresKeywordPasswordRegex.ReplaceAllString(dsn, "${1}"+redactedPlaceholder)
}

// redactURLUserinfoPassword masks the password portion of a URL-style
// userinfo segment "://user:password@". If there is no password (no ":"
// between "://" and "@") the input is returned unchanged.
func redactURLUserinfoPassword(dsn string) string {
	return urlUserinfoPasswordRegex.ReplaceAllString(dsn, "${1}:"+redactedPlaceholder+"@")
}
