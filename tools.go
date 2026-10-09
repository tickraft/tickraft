// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

//go:build tools

// Package tools pins lint-time dependencies so `go mod tidy` keeps them.
// This file is excluded from normal builds via the tools build tag.
package tools

import (
	// Used by .golangci/gorules.go (gocritic ruleguard custom rules).
	_ "github.com/quasilyte/go-ruleguard/dsl"
)
