// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package web embeds the front-end static assets for the
// tickraft binary.
//
// The dist directory is populated by the build pipeline (Makefile or
// Dockerfile) before the Go binary is compiled, and a .gitkeep placeholder
// keeps the directory tracked by git when empty.
//
// [DistFS] returns the embedded front-end filesystem rooted at "dist".
// Callers that want to serve the SPA on the same listener as the API
// (single-port deployments) can mount the returned fs.FS directly on their
// mux.
//
//	cfs, err := web.DistFS()
//	if err != nil {
//	    return err
//	}
//	mux.Handle("/", http.FileServer(http.FS(cfs)))
//
// This package is internal to the default deployment. The extended
// edition has its own internal/web package for embedding its
// own front-end build output.
package web
