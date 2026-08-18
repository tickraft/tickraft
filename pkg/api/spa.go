// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package api

import (
	"context"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"github.com/tickraft/tickraft/pkg/api/httputil"
	"github.com/tickraft/tickraft/pkg/errdefs"
)

// RegisterSPA registers single-page application static routes on the given
// Server. It serves static assets from distFS and falls back to index.html
// for unknown non-API paths (client-side routing).
//
// Routes registered:
//   - GET /assets/* — static files from the "assets" subdirectory of distFS.
//   - GET /favicon.ico — served from the root of distFS.
//   - NoRoute handler — falls back to index.html for paths that do not start
//     with /api/ or /health (SPA client-side routing). API paths return 404.
//
// distFS should be the root of the frontend build output (e.g. the value
// returned by web.DistFS()).
func RegisterSPA(s *Server, distFS fs.FS) error {
	if s == nil || s.hertz == nil || distFS == nil {
		return nil
	}

	indexHTML, err := fs.ReadFile(distFS, "index.html")
	if err != nil {
		return err
	}

	assetsFS, err := fs.Sub(distFS, "assets")
	if err != nil {
		return err
	}

	s.hertz.GET("/assets/*filepath", serveStatic(assetsFS, "/assets/"))

	favicon, faviconErr := fs.ReadFile(distFS, "favicon.ico")
	if faviconErr == nil {
		s.hertz.GET("/favicon.ico", func(ctx context.Context, arc *app.RequestContext) {
			arc.Data(consts.StatusOK, "image/x-icon", favicon)
		})
	}

	faviconSVG, svgErr := fs.ReadFile(distFS, "favicon.svg")
	if svgErr == nil {
		s.hertz.GET("/favicon.svg", func(ctx context.Context, arc *app.RequestContext) {
			arc.Data(consts.StatusOK, "image/svg+xml", faviconSVG)
		})
	}

	s.hertz.NoRoute(func(ctx context.Context, arc *app.RequestContext) {
		p := string(arc.URI().Path())
		if isAPIPath(p) {
			httputil.FailWithCode(arc, http.StatusNotFound, errdefs.CodeNotFound, "not found")
			return
		}
		arc.Data(consts.StatusOK, "text/html; charset=utf-8", indexHTML)
	})

	return nil
}

// serveStatic returns a handler that serves files from rootfs under the given
// urlPrefix. The prefix is stripped from the request path before looking up
// the file in rootfs. Missing files yield 404.
func serveStatic(rootfs fs.FS, urlPrefix string) app.HandlerFunc {
	return func(ctx context.Context, arc *app.RequestContext) {
		p := string(arc.URI().Path())
		if !strings.HasPrefix(p, urlPrefix) {
			httputil.FailWithCode(arc, http.StatusNotFound, errdefs.CodeNotFound, "not found")
			return
		}
		rel := strings.TrimPrefix(p, urlPrefix)
		rel = strings.Trim(rel, "/")
		if rel == "" {
			httputil.FailWithCode(arc, http.StatusNotFound, errdefs.CodeNotFound, "not found")
			return
		}
		cleaned := path.Clean(rel)
		if strings.HasPrefix(cleaned, "..") {
			httputil.FailWithCode(arc, http.StatusNotFound, errdefs.CodeNotFound, "not found")
			return
		}
		data, err := fs.ReadFile(rootfs, cleaned)
		if err != nil {
			httputil.FailWithCode(arc, http.StatusNotFound, errdefs.CodeNotFound, "not found")
			return
		}
		arc.Data(consts.StatusOK, contentTypeFor(cleaned), data)
	}
}

// contentTypeJSON is the Content-Type used for JSON payloads and source-map
// files served by the SPA handler.
const contentTypeJSON = "application/json; charset=utf-8"

// contentTypeOctetStream is the fallback Content-Type for assets with an
// unknown or missing extension.
const contentTypeOctetStream = "application/octet-stream"

// contentTypeFor returns a content type for common frontend asset extensions.
// Falls back to application/octet-stream for unknown types.
func contentTypeFor(name string) string {
	switch {
	case strings.HasSuffix(name, ".html"), strings.HasSuffix(name, ".htm"):
		return "text/html; charset=utf-8"
	case strings.HasSuffix(name, ".css"):
		return "text/css; charset=utf-8"
	case strings.HasSuffix(name, ".js"), strings.HasSuffix(name, ".mjs"):
		return "application/javascript; charset=utf-8"
	case strings.HasSuffix(name, ".json"):
		return contentTypeJSON
	case strings.HasSuffix(name, ".svg"):
		return "image/svg+xml"
	case strings.HasSuffix(name, ".png"):
		return "image/png"
	case strings.HasSuffix(name, ".jpg"), strings.HasSuffix(name, ".jpeg"):
		return "image/jpeg"
	case strings.HasSuffix(name, ".gif"):
		return "image/gif"
	case strings.HasSuffix(name, ".ico"):
		return "image/x-icon"
	case strings.HasSuffix(name, ".woff"):
		return "font/woff"
	case strings.HasSuffix(name, ".woff2"):
		return "font/woff2"
	case strings.HasSuffix(name, ".ttf"):
		return "font/ttf"
	case strings.HasSuffix(name, ".eot"):
		return "application/vnd.ms-fontobject"
	case strings.HasSuffix(name, ".map"):
		return contentTypeJSON
	default:
		return contentTypeOctetStream
	}
}

// isAPIPath reports whether the given path should be handled by the API
// rather than the SPA fallback. API paths start with /api/ or are exactly
// /health or /healthz.
func isAPIPath(p string) bool {
	if strings.HasPrefix(p, "/api/") {
		return true
	}
	return p == "/health" || p == "/healthz"
}
