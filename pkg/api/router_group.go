// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package api

import (
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/route"
)

// RouterGroup wraps a Hertz route.RouterGroup with convenience methods.
type RouterGroup struct {
	inner *route.RouterGroup
}

// newRouterGroup creates a RouterGroup from a Hertz route.RouterGroup.
func newRouterGroup(inner *route.RouterGroup) *RouterGroup {
	return &RouterGroup{inner: inner}
}

// GET registers a GET handler on the given path with optional inline
// middleware handlers that run before the final handler.
func (g *RouterGroup) GET(path string, handlers ...app.HandlerFunc) {
	g.inner.GET(path, handlers...)
}

// POST registers a POST handler on the given path with optional inline
// middleware handlers that run before the final handler.
func (g *RouterGroup) POST(path string, handlers ...app.HandlerFunc) {
	g.inner.POST(path, handlers...)
}

// PUT registers a PUT handler on the given path with optional inline
// middleware handlers that run before the final handler.
func (g *RouterGroup) PUT(path string, handlers ...app.HandlerFunc) {
	g.inner.PUT(path, handlers...)
}

// DELETE registers a DELETE handler on the given path with optional inline
// middleware handlers that run before the final handler.
func (g *RouterGroup) DELETE(path string, handlers ...app.HandlerFunc) {
	g.inner.DELETE(path, handlers...)
}

// Group creates a sub-group with the given prefix and optional middleware.
func (g *RouterGroup) Group(prefix string, middlewares ...app.HandlerFunc) *RouterGroup {
	sub := g.inner.Group(prefix, middlewares...)
	return newRouterGroup(sub)
}

// Use attaches middleware(s) to the group.
func (g *RouterGroup) Use(middlewares ...app.HandlerFunc) {
	g.inner.Use(middlewares...)
}
