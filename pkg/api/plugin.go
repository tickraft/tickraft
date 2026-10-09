// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package api

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
)

// Plugin is the extension point for extended modules to inject routes,
// global middleware, and lifecycle hooks.
type Plugin interface {
	// Name returns the plugin identifier.
	Name() string

	// RegisterRoutes allows the plugin to register HTTP routes on the root group.
	RegisterRoutes(root *RouterGroup)

	// Middlewares returns middleware(s) to be applied globally, after built-in
	// middleware but before route-group middleware. Return nil or empty if none.
	Middlewares() []app.HandlerFunc

	// OnStart is called after the server starts and all routes are registered.
	OnStart(ctx context.Context) error

	// OnStop is called when the server begins graceful shutdown.
	OnStop(ctx context.Context) error
}
