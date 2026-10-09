// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package config defines the unified YAML configuration format for the
// tickraft single-process deployment. The runtime runs the API server,
// worker engines, and prism together in one process; there is no separate
// operator subcommand in this edition.
//
// A configuration file is loaded via Load or LoadFromBytes. Before YAML
// unmarshaling, environment variable interpolation is performed on the raw
// bytes so that secrets such as database DSNs and JWT secrets can be injected
// from the environment using the ${VAR} or ${VAR:-default} syntax.
//
// After loading, call Validate to verify required fields, enum values, and
// duration/port formats before using the configuration.
package config
