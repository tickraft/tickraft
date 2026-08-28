// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package handler

import (
	"github.com/cloudwego/hertz/pkg/app"

	"github.com/tickraft/tickraft/pkg/api/handler/asset"
	"github.com/tickraft/tickraft/pkg/api/handler/auth"
	"github.com/tickraft/tickraft/pkg/api/handler/certificates"
	"github.com/tickraft/tickraft/pkg/api/handler/healthz"
	"github.com/tickraft/tickraft/pkg/api/handler/i18n"
	"github.com/tickraft/tickraft/pkg/api/handler/readyz"
	telemetryapi "github.com/tickraft/tickraft/pkg/api/handler/telemetry"
	"github.com/tickraft/tickraft/pkg/api/handler/ws"
	executorpkg "github.com/tickraft/tickraft/pkg/executor"
	"github.com/tickraft/tickraft/pkg/prism/alert"
	"github.com/tickraft/tickraft/pkg/prism/channel"
	"github.com/tickraft/tickraft/pkg/prism/remediation"
	"github.com/tickraft/tickraft/pkg/system"
	"github.com/tickraft/tickraft/pkg/task"
	"github.com/tickraft/tickraft/pkg/telemetry"
)

// RouteOption configures route registration with middleware and services.
// The handler package uses opaque app.HandlerFunc values for middleware,
// avoiding any direct import of pkg/auth or pkg/auth/jwt.
type RouteOption interface {
	apply(*routeConfig)
}

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
	telemetryReportHandler app.HandlerFunc
	telemetryMetricStore   telemetryapi.MetricStore
	telemetryLogStore      telemetryapi.LogStore
	telemetryProbeRecords  telemetryapi.ProbeRecordStore
	assetHandler           *asset.Handler
	healthzHandler         *healthz.Handler
	readyzHandler          *readyz.Handler
	certificateHandler     *certificates.Handler
	templateHandler        *telemetryapi.TemplateHandler
	i18nHandler            *i18n.Handler
	wsHandler              *ws.Handler
	executorRegistry       *executorpkg.Registry
}

// jwtAuthOption provides the JWT authentication middleware.
type jwtAuthOption struct {
	mw app.HandlerFunc
}

func (o jwtAuthOption) apply(c *routeConfig) { c.jwtMiddleware = o.mw }

// WithJWTAuth provides the JWT authentication middleware.
// The caller builds the middleware using middleware.NewJWTAuth and
// passes the resulting app.HandlerFunc here.
func WithJWTAuth(mw app.HandlerFunc) RouteOption { return jwtAuthOption{mw: mw} }

// assetKeyAuthOption provides the asset key authentication middleware.
type assetKeyAuthOption struct {
	mw app.HandlerFunc
}

func (o assetKeyAuthOption) apply(c *routeConfig) { c.assetKeyMiddleware = o.mw }

// WithAssetKeyAuth provides the asset key authentication middleware
// for telemetry report endpoints.
func WithAssetKeyAuth(mw app.HandlerFunc) RouteOption { return assetKeyAuthOption{mw: mw} }

// authServiceOption provides the Service implementation for auth handlers.
type authServiceOption struct {
	svc auth.Service
}

func (o authServiceOption) apply(c *routeConfig) { c.authService = o.svc }

// WithAuthService provides the Service implementation for auth handlers.
func WithAuthService(svc auth.Service) RouteOption { return authServiceOption{svc: svc} }

// taskServiceOption provides the Service implementation for scheduler handlers.
type taskServiceOption struct {
	svc task.Service
}

func (o taskServiceOption) apply(c *routeConfig) { c.taskSvc = o.svc }

// WithTaskService provides the Service implementation for scheduler handlers.
func WithTaskService(svc task.Service) RouteOption { return taskServiceOption{svc: svc} }

// alertServiceOption provides the Service implementation for alert handlers.
type alertServiceOption struct {
	svc alert.Service
}

func (o alertServiceOption) apply(c *routeConfig) { c.alertSvc = o.svc }

// WithAlertService provides the Service implementation for alert handlers.
func WithAlertService(svc alert.Service) RouteOption { return alertServiceOption{svc: svc} }

// channelServiceOption provides the Service implementation for
// notification channel handlers.
type channelServiceOption struct {
	svc channel.Service
}

func (o channelServiceOption) apply(c *routeConfig) { c.channelSvc = o.svc }

// WithChannelService provides the Service implementation for
// notification channel handlers. When omitted, the channel route group
// is not registered (tests inject an in-memory Service; production
// assemblies always inject the DB-backed one).
func WithChannelService(svc channel.Service) RouteOption {
	return channelServiceOption{svc: svc}
}

// remediationRuleServiceOption provides the Service implementation for
// self-healing rule handlers.
type remediationRuleServiceOption struct {
	svc remediation.Service
}

func (o remediationRuleServiceOption) apply(c *routeConfig) { c.remediationRuleSvc = o.svc }

// WithRemediationRuleService provides the Service
// implementation for self-healing rule handlers. When omitted, the
// remediation route group is not registered (tests inject an in-memory
// Service; production assemblies always inject the DB-backed one).
func WithRemediationRuleService(svc remediation.Service) RouteOption {
	return remediationRuleServiceOption{svc: svc}
}

// systemServiceOption provides the Service implementation for system handlers.
type systemServiceOption struct {
	svc system.Service
}

func (o systemServiceOption) apply(c *routeConfig) { c.systemSvc = o.svc }

// WithSystemService provides the Service implementation for system handlers.
func WithSystemService(svc system.Service) RouteOption { return systemServiceOption{svc: svc} }

// assetHandlerOption provides the AssetHandler for the asset management API.
type assetHandlerOption struct {
	h *asset.Handler
}

func (o assetHandlerOption) apply(c *routeConfig) { c.assetHandler = o.h }

// WithAssetHandler provides the AssetHandler for the asset
// management API at /api/v1/assets. When not provided, the
// asset route group is not registered.
func WithAssetHandler(h *asset.Handler) RouteOption { return assetHandlerOption{h: h} }

// telemetryServiceOption provides the Service implementation for the
// telemetry collection task CRUD API.
type telemetryServiceOption struct {
	svc telemetry.Service
}

func (o telemetryServiceOption) apply(c *routeConfig) { c.telemetrySvc = o.svc }

// WithTelemetryService provides the Service implementation for the
// telemetry collection task CRUD API at /api/v1/telemetry. When omitted, the
// telemetry CRUD route group is not registered.
func WithTelemetryService(svc telemetry.Service) RouteOption {
	return telemetryServiceOption{svc: svc}
}

// telemetryReportHandlerOption provides the Hertz handler for the
// unified telemetry report endpoint.
type telemetryReportHandlerOption struct {
	h app.HandlerFunc
}

func (o telemetryReportHandlerOption) apply(c *routeConfig) { c.telemetryReportHandler = o.h }

// WithTelemetryReportHandler provides the Hertz handler for the unified
// telemetry report endpoint at POST /api/v1/telemetry. When omitted, the
// report route group is not registered.
func WithTelemetryReportHandler(h app.HandlerFunc) RouteOption {
	return telemetryReportHandlerOption{h: h}
}

// telemetryDataStoresOption provides the MetricStore and LogStore used by
// the telemetry handler's history/logs endpoints.
type telemetryDataStoresOption struct {
	metricStore telemetryapi.MetricStore
	logStore    telemetryapi.LogStore
}

func (o telemetryDataStoresOption) apply(c *routeConfig) {
	c.telemetryMetricStore = o.metricStore
	c.telemetryLogStore = o.logStore
}

// WithTelemetryDataStores provides the MetricStore and LogStore used by the
// telemetry handler's history/logs endpoints. Both stores may be nil to
// disable the corresponding query path.
func WithTelemetryDataStores(
	metricStore telemetryapi.MetricStore,
	logStore telemetryapi.LogStore,
) RouteOption {
	return telemetryDataStoresOption{metricStore: metricStore, logStore: logStore}
}

// telemetryProbeRecordsOption provides the ProbeRecordStore used by the
// telemetry handler's status/history/logs endpoints for active points.
type telemetryProbeRecordsOption struct {
	store telemetryapi.ProbeRecordStore
}

func (o telemetryProbeRecordsOption) apply(c *routeConfig) {
	c.telemetryProbeRecords = o.store
}

// WithTelemetryProbeRecords provides the probe record store used by the
// telemetry handler's status/history/logs endpoints for active monitor
// points. A nil store disables the probe-backed query paths.
func WithTelemetryProbeRecords(store telemetryapi.ProbeRecordStore) RouteOption {
	return telemetryProbeRecordsOption{store: store}
}

// healthzHandlerOption provides the HealthzHandler for the /healthz endpoint.
type healthzHandlerOption struct {
	h *healthz.Handler
}

func (o healthzHandlerOption) apply(c *routeConfig) { c.healthzHandler = o.h }

// WithHealthzHandler provides the HealthzHandler for the /healthz endpoint.
// When not provided, a default handler returning 200 with {"status":"ok"}
// is used (no dependency checks).
func WithHealthzHandler(h *healthz.Handler) RouteOption { return healthzHandlerOption{h: h} }

// readyHandlerOption provides the ReadyHandler for the /readyz endpoint.
type readyHandlerOption struct {
	h *readyz.Handler
}

func (o readyHandlerOption) apply(c *routeConfig) { c.readyzHandler = o.h }

// WithReadyHandler provides the ReadyHandler for the /readyz endpoint.
// When not provided, a default handler returning 200 with {"status":"ready"}
// is used (no dependency checks).
func WithReadyHandler(h *readyz.Handler) RouteOption { return readyHandlerOption{h: h} }

// certificateHandlerOption provides the CertificateHandler for the
// certificate reload endpoint.
type certificateHandlerOption struct {
	h *certificates.Handler
}

func (o certificateHandlerOption) apply(c *routeConfig) { c.certificateHandler = o.h }

// WithCertificateHandler provides the CertificateHandler for the
// /api/v1/system/certificates/reload endpoint. When not provided, the
// certificate reload route is not registered.
func WithCertificateHandler(h *certificates.Handler) RouteOption {
	return certificateHandlerOption{h: h}
}

// templateHandlerOption provides the TemplateHandler for the telemetry
// template management API.
type templateHandlerOption struct {
	h *telemetryapi.TemplateHandler
}

func (o templateHandlerOption) apply(c *routeConfig) { c.templateHandler = o.h }

// WithTemplateHandler provides the TemplateHandler for the telemetry
// template management API at /api/v1/telemetry/templates. When omitted,
// the template route group is not registered.
func WithTemplateHandler(h *telemetryapi.TemplateHandler) RouteOption {
	return templateHandlerOption{h: h}
}

// i18nHandlerOption provides the I18nHandler for the locale listing API.
type i18nHandlerOption struct {
	h *i18n.Handler
}

func (o i18nHandlerOption) apply(c *routeConfig) { c.i18nHandler = o.h }

// WithI18nHandler provides the I18nHandler for the locale listing API
// at /api/v1/i18n/locales. When omitted, the i18n route group is not
// registered.
func WithI18nHandler(h *i18n.Handler) RouteOption { return i18nHandlerOption{h: h} }

// wsHandlerOption provides the WebSocket handler for the /ws realtime
// push endpoint.
type wsHandlerOption struct {
	h *ws.Handler
}

func (o wsHandlerOption) apply(c *routeConfig) { c.wsHandler = o.h }

// WithWSHandler provides the WebSocket handler for the /ws realtime
// push endpoint. When omitted, the route is not registered.
func WithWSHandler(h *ws.Handler) RouteOption { return wsHandlerOption{h: h} }

// executorRegistryOption provides the executor registry backing the
// executor type enumeration endpoints (/executors and /telemetry/probers).
type executorRegistryOption struct {
	reg *executorpkg.Registry
}

func (o executorRegistryOption) apply(c *routeConfig) { c.executorRegistry = o.reg }

// WithExecutorRegistry provides the executor registry used to derive the
// executor and prober type lists. When omitted, the enumeration endpoints
// return empty lists.
func WithExecutorRegistry(reg *executorpkg.Registry) RouteOption {
	return executorRegistryOption{reg: reg}
}
