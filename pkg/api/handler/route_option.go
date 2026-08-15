// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package handler

import (
	"github.com/cloudwego/hertz/pkg/app"

	"github.com/tickraft/tickraft/pkg/api/handler/alert"
	"github.com/tickraft/tickraft/pkg/api/handler/asset"
	"github.com/tickraft/tickraft/pkg/api/handler/auth"
	"github.com/tickraft/tickraft/pkg/api/handler/certificates"
	"github.com/tickraft/tickraft/pkg/api/handler/channel"
	"github.com/tickraft/tickraft/pkg/api/handler/healthz"
	"github.com/tickraft/tickraft/pkg/api/handler/i18n"
	"github.com/tickraft/tickraft/pkg/api/handler/readyz"
	"github.com/tickraft/tickraft/pkg/api/handler/remediation"
	"github.com/tickraft/tickraft/pkg/api/handler/system"
	"github.com/tickraft/tickraft/pkg/api/handler/task"
	"github.com/tickraft/tickraft/pkg/api/handler/telemetry"
	"github.com/tickraft/tickraft/pkg/api/handler/ws"
)

// RouteOption configures route registration with middleware and services.
// The handler package uses opaque app.HandlerFunc values for middleware,
// avoiding any direct import of pkg/auth or pkg/auth/jwt.
type RouteOption func(*routeConfig)

// routeConfig holds the middleware and services injected via options.
// Service interface types are defined in their respective sub-packages
// (e.g. auth.Service, task.Service) so each domain is self-contained.
type routeConfig struct {
	jwtMiddleware          app.HandlerFunc
	assetKeyMiddleware     app.HandlerFunc
	authService            auth.Service
	taskSvc                task.Service
	alertSvc               alert.Service
	channelSvc             channel.Service
	remediationRuleSvc     remediation.Service
	systemSvc              system.Service
	telemetrySvc           telemetry.Service
	telemetryReportHandler telemetry.ReportHandler
	telemetryMetricStore   telemetry.MetricStoreInjector
	telemetryLogStore      telemetry.LogStoreInjector
	assetHandler           *asset.Handler
	healthzHandler         *healthz.Handler
	readyzHandler          *readyz.Handler
	certificateHandler     *certificates.Handler
	templateHandler        *telemetry.TemplateHandler
	i18nHandler            *i18n.Handler
	wsHandler              *ws.Handler
}

// WithJWTAuth provides the JWT authentication middleware.
// The caller builds the middleware using middleware.NewJWTAuth and
// passes the resulting app.HandlerFunc here.
func WithJWTAuth(mw app.HandlerFunc) RouteOption {
	return func(c *routeConfig) { c.jwtMiddleware = mw }
}

// WithAssetKeyAuth provides the asset key authentication middleware
// for telemetry report endpoints.
func WithAssetKeyAuth(mw app.HandlerFunc) RouteOption {
	return func(c *routeConfig) { c.assetKeyMiddleware = mw }
}

// WithAuthService provides the Service implementation for auth handlers.
func WithAuthService(svc auth.Service) RouteOption {
	return func(c *routeConfig) { c.authService = svc }
}

// WithTaskService provides the Service implementation for scheduler handlers.
func WithTaskService(svc task.Service) RouteOption {
	return func(c *routeConfig) { c.taskSvc = svc }
}

// WithAlertService provides the Service implementation for alert handlers.
func WithAlertService(svc alert.Service) RouteOption {
	return func(c *routeConfig) { c.alertSvc = svc }
}

// WithChannelService provides the Service implementation for
// notification channel handlers. When omitted, the handler package falls
// back to an in-memory implementation.
func WithChannelService(svc channel.Service) RouteOption {
	return func(c *routeConfig) { c.channelSvc = svc }
}

// WithRemediationRuleService provides the Service
// implementation for self-healing rule handlers. When omitted, the handler
// package falls back to an in-memory implementation.
func WithRemediationRuleService(svc remediation.Service) RouteOption {
	return func(c *routeConfig) { c.remediationRuleSvc = svc }
}

// WithSystemService provides the Service implementation for system handlers.
func WithSystemService(svc system.Service) RouteOption {
	return func(c *routeConfig) { c.systemSvc = svc }
}

// WithAssetHandler provides the AssetHandler for the asset
// management API at /api/v1/assets. When not provided, the
// asset route group is not registered.
func WithAssetHandler(h *asset.Handler) RouteOption {
	return func(c *routeConfig) { c.assetHandler = h }
}

// WithTelemetryService provides the Service implementation for the
// telemetry collection task CRUD API at /api/v1/telemetry. When omitted, the
// telemetry CRUD route group is not registered.
func WithTelemetryService(svc telemetry.Service) RouteOption {
	return func(c *routeConfig) { c.telemetrySvc = svc }
}

// WithTelemetryReportHandler provides the ReportHandler for the
// unified telemetry report endpoint at POST /api/v1/telemetry. When omitted,
// the report route group is not registered.
func WithTelemetryReportHandler(h telemetry.ReportHandler) RouteOption {
	return func(c *routeConfig) { c.telemetryReportHandler = h }
}

// WithTelemetryDataStores provides the MetricStore and LogStore used by the
// telemetry handler's history/logs endpoints. Both stores may be nil to
// disable the corresponding query path.
func WithTelemetryDataStores(metricStore telemetry.MetricStoreInjector, logStore telemetry.LogStoreInjector) RouteOption {
	return func(c *routeConfig) {
		c.telemetryMetricStore = metricStore
		c.telemetryLogStore = logStore
	}
}

// WithHealthzHandler provides the HealthzHandler for the /healthz endpoint.
// When not provided, a default handler returning 200 with {"status":"ok"}
// is used (no dependency checks).
func WithHealthzHandler(h *healthz.Handler) RouteOption {
	return func(c *routeConfig) { c.healthzHandler = h }
}

// WithReadyHandler provides the ReadyHandler for the /readyz endpoint.
// When not provided, a default handler returning 200 with {"status":"ready"}
// is used (no dependency checks).
func WithReadyHandler(h *readyz.Handler) RouteOption {
	return func(c *routeConfig) { c.readyzHandler = h }
}

// WithCertificateHandler provides the CertificateHandler for the
// /api/v1/system/certificates/reload endpoint. When not provided, the
// certificate reload route is not registered.
func WithCertificateHandler(h *certificates.Handler) RouteOption {
	return func(c *routeConfig) { c.certificateHandler = h }
}

// WithTemplateHandler provides the TemplateHandler for the telemetry
// template management API at /api/v1/telemetry/templates. When omitted,
// the template route group is not registered.
func WithTemplateHandler(h *telemetry.TemplateHandler) RouteOption {
	return func(c *routeConfig) { c.templateHandler = h }
}

// WithI18nHandler provides the I18nHandler for the locale listing API
// at /api/v1/i18n/locales. When omitted, the i18n route group is not
// registered.
func WithI18nHandler(h *i18n.Handler) RouteOption {
	return func(c *routeConfig) { c.i18nHandler = h }
}

// WithWSHandler provides the WebSocket handler for the /ws realtime
// push endpoint. When omitted, the route is not registered.
func WithWSHandler(h *ws.Handler) RouteOption {
	return func(c *routeConfig) { c.wsHandler = h }
}
