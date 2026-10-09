// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package router is the shared composition root for route registration. It
// bridges the handler package (which has zero auth awareness) with the
// auth/jwt packages by building middleware instances and an auth service
// adapter, then injecting them via RegisterOption values.
//
// Required-service validation lives in the handler package's
// validateRouteConfig; this layer only aggregates options and nil-guards the
// optional pieces, so both editions share one wiring contract while edition
// specific capabilities arrive as plain options.
package router

import (
	"context"
	"fmt"

	"github.com/cloudwego/hertz/pkg/app"

	"github.com/tickraft/tickraft/pkg/api"
	"github.com/tickraft/tickraft/pkg/api/handler"
	"github.com/tickraft/tickraft/pkg/api/handler/asset"
	"github.com/tickraft/tickraft/pkg/api/handler/certificates"
	"github.com/tickraft/tickraft/pkg/api/handler/healthz"
	"github.com/tickraft/tickraft/pkg/api/handler/i18n"
	quotaapi "github.com/tickraft/tickraft/pkg/api/handler/quota"
	"github.com/tickraft/tickraft/pkg/api/handler/readyz"
	telemetryapi "github.com/tickraft/tickraft/pkg/api/handler/telemetry"
	wsapi "github.com/tickraft/tickraft/pkg/api/handler/ws"
	"github.com/tickraft/tickraft/pkg/api/middleware"
	"github.com/tickraft/tickraft/pkg/auth"
	jwtauth "github.com/tickraft/tickraft/pkg/auth/jwt"
	"github.com/tickraft/tickraft/pkg/contact"
	"github.com/tickraft/tickraft/pkg/executor"
	"github.com/tickraft/tickraft/pkg/prism/alert"
	"github.com/tickraft/tickraft/pkg/prism/channel"
	"github.com/tickraft/tickraft/pkg/prism/remediation"
	"github.com/tickraft/tickraft/pkg/status"
	"github.com/tickraft/tickraft/pkg/system"
	"github.com/tickraft/tickraft/pkg/task"
	"github.com/tickraft/tickraft/pkg/telemetry"
)

// denyAllAssetKeys is a fail-closed asset key getter used when no concrete
// getter is provided. It rejects all asset keys so that telemetry report
// endpoints cannot be accessed without proper configuration.
func denyAllAssetKeys(_ context.Context, _ string) (bool, error) {
	return false, nil
}

// RegisterOption configures route registration with additional handlers and
// services beyond the always-present auth middleware. It uses a variadic
// pattern so existing callers that pass no options continue to work.
type RegisterOption interface {
	apply(*registerConfig)
}

// registerConfig holds handlers and services injected via RegisterOption.
type registerConfig struct {
	taskService            task.Service
	alertService           alert.Service
	channelService         channel.Service
	contactService         contact.Service
	remediationRuleService remediation.Service
	systemService          system.Service
	telemetryService       telemetry.Service
	telemetryReportHandler app.HandlerFunc
	telemetryMetricStore   telemetryapi.MetricStore
	telemetryLogStore      telemetryapi.LogStore
	telemetryProbeRecords  telemetryapi.ProbeRecordStore
	assetHandler           *asset.Handler
	templateHandler        *telemetryapi.TemplateHandler
	healthzHandler         *healthz.Handler
	readyzHandler          *readyz.Handler
	certificateHandler     *certificates.Handler
	i18nHandler            *i18n.Handler
	wsHandler              *wsapi.Handler
	executorRegistry       *executor.Registry
	statusService          status.Service
	quotaUsageHandler      *quotaapi.Handler

	// apiKeyAuth enables API-key bearer auth alongside JWT. The key lookup
	// is derived from the auth service with a short-TTL cache.
	apiKeyAuth bool
	// revoker, when set, invalidates a user's other sessions after a
	// password change (HA deployments with a shared blacklist).
	revoker RevokeFunc
}

// taskServiceOption provides the task.Service implementation for task
// handlers.
type taskServiceOption struct {
	svc task.Service
}

func (o taskServiceOption) apply(c *registerConfig) { c.taskService = o.svc }

// WithTaskService provides the task.Service implementation for task
// handlers. Required by the handler route validator; when omitted,
// registration fails with a missing-service error.
func WithTaskService(svc task.Service) RegisterOption { return taskServiceOption{svc: svc} }

// alertServiceOption provides the alert.Service implementation for
// alert handlers.
type alertServiceOption struct {
	svc alert.Service
}

func (o alertServiceOption) apply(c *registerConfig) { c.alertService = o.svc }

// WithAlertService provides the alert.Service implementation for alert
// handlers. Required by the handler route validator.
func WithAlertService(svc alert.Service) RegisterOption {
	return alertServiceOption{svc: svc}
}

// channelServiceOption provides the channel.Service implementation for
// notification channel handlers.
type channelServiceOption struct {
	svc channel.Service
}

func (o channelServiceOption) apply(c *registerConfig) { c.channelService = o.svc }

// WithChannelService provides the channel.Service implementation for
// notification channel handlers. When omitted, the channel route group
// is not registered (tests inject an in-memory Service; production
// assemblies always inject the DB-backed one).
func WithChannelService(svc channel.Service) RegisterOption {
	return channelServiceOption{svc: svc}
}

// contactServiceOption provides the contact.Service implementation for the
// notification-only contact directory handlers.
type contactServiceOption struct {
	svc contact.Service
}

func (o contactServiceOption) apply(c *registerConfig) { c.contactService = o.svc }

// WithContactService provides the contact.Service implementation for the
// contact directory handlers at /api/v1/contacts. When omitted, the contact
// route group is not registered (multi-tenant editions inject their own
// tenant-scoped implementation through the same option).
func WithContactService(svc contact.Service) RegisterOption {
	return contactServiceOption{svc: svc}
}

// remediationRuleServiceOption provides the remediation.Service
// implementation for self-healing rule handlers.
type remediationRuleServiceOption struct {
	svc remediation.Service
}

func (o remediationRuleServiceOption) apply(c *registerConfig) {
	c.remediationRuleService = o.svc
}

// WithRemediationRuleService provides the remediation.Service
// implementation for self-healing rule handlers. When omitted, the
// remediation route group is not registered (tests inject an in-memory
// Service; production assemblies always inject the DB-backed one).
func WithRemediationRuleService(svc remediation.Service) RegisterOption {
	return remediationRuleServiceOption{svc: svc}
}

// systemServiceOption provides the system.Service implementation for system
// handlers.
type systemServiceOption struct {
	svc system.Service
}

func (o systemServiceOption) apply(c *registerConfig) { c.systemService = o.svc }

// WithSystemService provides the system.Service implementation for system
// handlers (config and info endpoints under /api/v1/system). Required by the
// handler route validator.
func WithSystemService(svc system.Service) RegisterOption {
	return systemServiceOption{svc: svc}
}

// assetHandlerOption provides the AssetHandler for the asset management API.
type assetHandlerOption struct {
	h *asset.Handler
}

func (o assetHandlerOption) apply(c *registerConfig) { c.assetHandler = o.h }

// WithAssetHandler provides the AssetHandler for the asset management API at
// /api/v1/assets. When omitted, the asset route group is not registered.
func WithAssetHandler(h *asset.Handler) RegisterOption { return assetHandlerOption{h: h} }

// executorRegistryOption provides the executor registry backing the executor
// type enumeration endpoints.
type executorRegistryOption struct {
	reg *executor.Registry
}

func (o executorRegistryOption) apply(c *registerConfig) { c.executorRegistry = o.reg }

// WithExecutorRegistry provides the executor registry used to derive the
// /api/v1/executors and /api/v1/telemetry/probers type lists. When omitted,
// the enumeration endpoints return empty lists.
func WithExecutorRegistry(reg *executor.Registry) RegisterOption {
	return executorRegistryOption{reg: reg}
}

// statusServiceOption provides the status.Service implementation for the
// status page endpoints.
type statusServiceOption struct {
	svc status.Service
}

func (o statusServiceOption) apply(c *registerConfig) { c.statusService = o.svc }

// WithStatusService provides the status.Service implementation for the
// public status page and its configuration management endpoints. When
// omitted, the status route group is not registered.
func WithStatusService(svc status.Service) RegisterOption {
	return statusServiceOption{svc: svc}
}

// telemetryServiceOption provides the telemetry.Service implementation
// for the monitor point CRUD API.
type telemetryServiceOption struct {
	svc telemetry.Service
}

func (o telemetryServiceOption) apply(c *registerConfig) { c.telemetryService = o.svc }

// WithTelemetryService provides the telemetry.Service implementation
// for the monitor point CRUD API at /api/v1/telemetry/monitors. Required
// by the handler route validator.
func WithTelemetryService(svc telemetry.Service) RegisterOption {
	return telemetryServiceOption{svc: svc}
}

// telemetryReportHandlerOption provides the Hertz handler for the unified
// telemetry report endpoint.
type telemetryReportHandlerOption struct {
	h app.HandlerFunc
}

func (o telemetryReportHandlerOption) apply(c *registerConfig) {
	c.telemetryReportHandler = o.h
}

// WithTelemetryReportHandler provides the Hertz handler for the unified
// telemetry report endpoint at POST /api/v1/telemetry. When omitted, the
// report route group is not registered.
func WithTelemetryReportHandler(h app.HandlerFunc) RegisterOption {
	return telemetryReportHandlerOption{h: h}
}

// telemetryDataStoresOption provides the MetricStore and LogStore used by
// the telemetry handler's history/logs endpoints.
type telemetryDataStoresOption struct {
	metricStore telemetryapi.MetricStore
	logStore    telemetryapi.LogStore
}

func (o telemetryDataStoresOption) apply(c *registerConfig) {
	c.telemetryMetricStore = o.metricStore
	c.telemetryLogStore = o.logStore
}

// WithTelemetryDataStores provides the MetricStore and LogStore used by the
// telemetry handler's history/logs endpoints. Both stores may be nil.
func WithTelemetryDataStores(metricStore telemetryapi.MetricStore,
	logStore telemetryapi.LogStore) RegisterOption {
	return telemetryDataStoresOption{metricStore: metricStore, logStore: logStore}
}

// telemetryProbeRecordsOption provides the probe record store used by the
// telemetry handler's status/history/logs endpoints for active points.
type telemetryProbeRecordsOption struct {
	store telemetryapi.ProbeRecordStore
}

func (o telemetryProbeRecordsOption) apply(c *registerConfig) {
	c.telemetryProbeRecords = o.store
}

// WithTelemetryProbeRecords provides the probe record store used by the
// telemetry handler's status/history/logs endpoints for active monitor
// points. A nil store disables the probe-backed query paths.
func WithTelemetryProbeRecords(store telemetryapi.ProbeRecordStore) RegisterOption {
	return telemetryProbeRecordsOption{store: store}
}

// templateHandlerOption provides the TemplateHandler for the telemetry
// template management API.
type templateHandlerOption struct {
	h *telemetryapi.TemplateHandler
}

func (o templateHandlerOption) apply(c *registerConfig) { c.templateHandler = o.h }

// WithTemplateHandler provides the TemplateHandler for the telemetry
// template management API at /api/v1/telemetry/templates. When omitted, the
// template route group is not registered.
func WithTemplateHandler(h *telemetryapi.TemplateHandler) RegisterOption {
	return templateHandlerOption{h: h}
}

// healthzHandlerOption provides the HealthzHandler for the /healthz endpoint.
type healthzHandlerOption struct {
	h *healthz.Handler
}

func (o healthzHandlerOption) apply(c *registerConfig) { c.healthzHandler = o.h }

// WithHealthzHandler provides the HealthzHandler for the /healthz endpoint.
// When omitted, a default stub returning 200 without dependency checks is
// used.
func WithHealthzHandler(h *healthz.Handler) RegisterOption { return healthzHandlerOption{h: h} }

// readyzHandlerOption provides the ReadyHandler for the /readyz endpoint.
type readyzHandlerOption struct {
	h *readyz.Handler
}

func (o readyzHandlerOption) apply(c *registerConfig) { c.readyzHandler = o.h }

// WithReadyzHandler provides the ReadyHandler for the /readyz endpoint. When
// omitted, a default stub returning 200 without dependency checks is used.
func WithReadyzHandler(h *readyz.Handler) RegisterOption { return readyzHandlerOption{h: h} }

// certificateHandlerOption provides the CertificateHandler for the
// certificate reload endpoint.
type certificateHandlerOption struct {
	h *certificates.Handler
}

func (o certificateHandlerOption) apply(c *registerConfig) { c.certificateHandler = o.h }

// WithCertificateHandler provides the CertificateHandler for the
// POST /api/v1/system/certificates/reload endpoint. When omitted, the
// certificate reload route is not registered. The handler wraps an
// *api.Server so it must be created after the server is constructed; the
// start command wires it when TLS is enabled.
func WithCertificateHandler(h *certificates.Handler) RegisterOption {
	return certificateHandlerOption{h: h}
}

// wsHandlerOption provides the WebSocket handler for the /ws realtime push
// endpoint.
type wsHandlerOption struct {
	h *wsapi.Handler
}

func (o wsHandlerOption) apply(c *registerConfig) { c.wsHandler = o.h }

// WithWSHandler provides the WebSocket handler for the /ws realtime push
// endpoint. When omitted, the route is not registered.
func WithWSHandler(h *wsapi.Handler) RegisterOption { return wsHandlerOption{h: h} }

// i18nHandlerOption provides the I18nHandler for the locale listing API.
type i18nHandlerOption struct {
	h *i18n.Handler
}

func (o i18nHandlerOption) apply(c *registerConfig) { c.i18nHandler = o.h }

// WithI18nHandler provides the I18nHandler for the locale listing API at
// /api/v1/i18n/locales. When omitted, the i18n route group is not
// registered.
func WithI18nHandler(h *i18n.Handler) RegisterOption { return i18nHandlerOption{h: h} }

// quotaUsageHandlerOption provides the quota usage aggregate handler.
type quotaUsageHandlerOption struct {
	h *quotaapi.Handler
}

func (o quotaUsageHandlerOption) apply(c *registerConfig) { c.quotaUsageHandler = o.h }

// WithQuotaUsageHandler provides the handler for the quota usage aggregate
// API at /api/v1/quota/usage. When omitted, the route is not registered.
func WithQuotaUsageHandler(h *quotaapi.Handler) RegisterOption {
	return quotaUsageHandlerOption{h: h}
}

// apiKeyAuthOption enables API-key bearer authentication alongside JWT auth.
type apiKeyAuthOption struct{}

func (apiKeyAuthOption) apply(c *registerConfig) { c.apiKeyAuth = true }

// WithAPIKeyAuth enables API-key bearer auth: requests carrying the
// X-Tickraft-Api-Key header are validated against the auth service's API-key
// store (with a short-TTL cache) instead of a JWT. When omitted, only JWT
// bearer auth is accepted.
func WithAPIKeyAuth() RegisterOption { return apiKeyAuthOption{} }

// userRevokerOption provides the RevokeFunc invoked after a successful
// password change.
type userRevokerOption struct {
	revoker RevokeFunc
}

func (o userRevokerOption) apply(c *registerConfig) { c.revoker = o.revoker }

// WithUserRevoker injects a revoker that invalidates all of a user's other
// sessions after a successful password change. When omitted, only the
// current session is logged out.
func WithUserRevoker(revoker RevokeFunc) RegisterOption {
	return userRevokerOption{revoker: revoker}
}

// RegisterRoutes builds the auth middleware and an auth service adapter, then
// delegates to handler.RegisterRoutes with the aggregated RouteOption
// values. Nil optional pieces are skipped; required services are enforced by
// the handler package's route validator, which fails registration when any
// of them is missing.
//
// Parameters:
//   - server: the API server to register routes on.
//   - jwt: the JWT manager for token validation.
//   - authz: the auth service for login, password, and API key operations.
//   - assetKeyGetter: validates the X-Tickraft-Asset-Key header for
//     telemetry report endpoints. If nil, a fail-closed stub is used.
//   - opts: optional RegisterOption values to inject the domain services,
//     handlers, and edition-specific seams.
func RegisterRoutes(
	server *api.Server,
	jwt *jwtauth.JWT,
	authz *auth.Service,
	assetKeyGetter func(ctx context.Context, key string) (bool, error),
	options ...RegisterOption,
) error {
	if err := validateRegisterArgs(server, jwt, authz); err != nil {
		return err
	}

	getter := assetKeyGetter
	if getter == nil {
		getter = denyAllAssetKeys
	}

	rc := &registerConfig{}
	for _, o := range options {
		o.apply(rc)
	}

	// Build middleware instances using the auth/jwt packages. API-key
	// bearer auth is opt-in; without it the JWT middleware runs alone.
	var authMW app.HandlerFunc
	if rc.apiKeyAuth {
		authMW = middleware.NewAnyAuth(jwt, newAPIKeyGetter(authz))
	} else {
		authMW = middleware.NewJWTAuth(jwt, "")
	}
	assetKeyMW := middleware.NewAssetKeyMiddleware(getter)

	// Wrap *auth.Service in the adapter to satisfy the handler SPI.
	adapter := &serviceAdapter{svc: authz, revoker: rc.revoker}

	if err := handler.RegisterRoutes(server, rc.handlerOptions(authMW, assetKeyMW, adapter)...); err != nil {
		return fmt.Errorf("router: %w", err)
	}

	return nil
}

// validateRegisterArgs returns an error when any mandatory RegisterRoutes
// argument is nil. These arguments back routes and middleware that are always
// registered, so a nil value would produce a runtime panic on first use.
func validateRegisterArgs(server *api.Server, jwt *jwtauth.JWT, authz *auth.Service) error {
	if server == nil {
		return fmt.Errorf("router: server is nil")
	}
	if jwt == nil {
		return fmt.Errorf("router: jwt is nil")
	}
	if authz == nil {
		return fmt.Errorf("router: auth service is nil")
	}
	return nil
}

// handlerOptions assembles the handler.RouteOption list from the aggregated
// register config. Nil-guarding every piece keeps the same aggregation
// contract for both editions: kernel-required services (task, alert, system,
// telemetry) surface the handler validator's error when missing; everything
// else stays conditional.
func (rc *registerConfig) handlerOptions(
	authMW, assetKeyMW app.HandlerFunc,
	adapter *serviceAdapter,
) []handler.RouteOption {
	handlerOpts := []handler.RouteOption{
		handler.WithJWTAuth(authMW),
		handler.WithAssetKeyAuth(assetKeyMW),
		handler.WithAuthService(adapter),
	}
	// The With* constructors are plain struct wrappers, so building a
	// disabled option up front is safe; appendIf simply drops it.
	appendIf := func(opt handler.RouteOption, enabled bool) {
		if enabled {
			handlerOpts = append(handlerOpts, opt)
		}
	}
	appendIf(handler.WithTaskService(rc.taskService), rc.taskService != nil)
	appendIf(handler.WithAlertService(rc.alertService), rc.alertService != nil)
	appendIf(handler.WithChannelService(rc.channelService), rc.channelService != nil)
	appendIf(handler.WithContactService(rc.contactService), rc.contactService != nil)
	appendIf(handler.WithRemediationRuleService(rc.remediationRuleService), rc.remediationRuleService != nil)
	appendIf(handler.WithSystemService(rc.systemService), rc.systemService != nil)
	appendIf(handler.WithTelemetryService(rc.telemetryService), rc.telemetryService != nil)
	appendIf(handler.WithTelemetryReportHandler(rc.telemetryReportHandler), rc.telemetryReportHandler != nil)
	appendIf(handler.WithTelemetryDataStores(rc.telemetryMetricStore, rc.telemetryLogStore),
		rc.telemetryMetricStore != nil || rc.telemetryLogStore != nil)
	appendIf(handler.WithTelemetryProbeRecords(rc.telemetryProbeRecords), rc.telemetryProbeRecords != nil)
	appendIf(handler.WithAssetHandler(rc.assetHandler), rc.assetHandler != nil)
	appendIf(handler.WithExecutorRegistry(rc.executorRegistry), rc.executorRegistry != nil)
	appendIf(handler.WithStatusService(rc.statusService), rc.statusService != nil)
	appendIf(handler.WithQuotaUsageHandler(rc.quotaUsageHandler), rc.quotaUsageHandler != nil)
	appendIf(handler.WithHealthzHandler(rc.healthzHandler), rc.healthzHandler != nil)
	appendIf(handler.WithReadyHandler(rc.readyzHandler), rc.readyzHandler != nil)
	appendIf(handler.WithCertificateHandler(rc.certificateHandler), rc.certificateHandler != nil)
	appendIf(handler.WithTemplateHandler(rc.templateHandler), rc.templateHandler != nil)
	appendIf(handler.WithI18nHandler(rc.i18nHandler), rc.i18nHandler != nil)
	appendIf(handler.WithWSHandler(rc.wsHandler), rc.wsHandler != nil)
	return handlerOpts
}
