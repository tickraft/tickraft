// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package router provides the central route registration entry point for the
// tickraft server. It bridges the handler package (which has zero
// auth awareness) with the auth/jwt packages by building middleware instances
// and an auth service adapter, then injecting them via RouteOption values.
package router

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/tickraft/tickraft/pkg/api"
	"github.com/tickraft/tickraft/pkg/api/handler"
	"github.com/tickraft/tickraft/pkg/api/handler/alert"
	"github.com/tickraft/tickraft/pkg/api/handler/asset"
	authapi "github.com/tickraft/tickraft/pkg/api/handler/auth"
	"github.com/tickraft/tickraft/pkg/api/handler/certificates"
	"github.com/tickraft/tickraft/pkg/api/handler/channel"
	"github.com/tickraft/tickraft/pkg/api/handler/healthz"
	"github.com/tickraft/tickraft/pkg/api/handler/i18n"
	"github.com/tickraft/tickraft/pkg/api/handler/readyz"
	"github.com/tickraft/tickraft/pkg/api/handler/remediation"
	"github.com/tickraft/tickraft/pkg/api/handler/system"
	"github.com/tickraft/tickraft/pkg/api/handler/task"
	"github.com/tickraft/tickraft/pkg/api/handler/telemetry"
	wsapi "github.com/tickraft/tickraft/pkg/api/handler/ws"
	"github.com/tickraft/tickraft/pkg/api/middleware"
	"github.com/tickraft/tickraft/pkg/auth"
	"github.com/tickraft/tickraft/pkg/auth/apikey"
	"github.com/tickraft/tickraft/pkg/auth/jwt"
	"github.com/tickraft/tickraft/pkg/user"
)

// apiKeyCacheEntry is a cached API key lookup result with its expiry.
type apiKeyCacheEntry struct {
	info      *apikey.Info
	expiresAt time.Time
}

// serviceAdapter wraps *auth.Service to satisfy the authapi.Service
// interface. It converts jwt.TokenPair to authapi.TokenPair so that
// the handler package never needs to import pkg/auth or pkg/auth/jwt.
type serviceAdapter struct {
	service *auth.Service
}

// Login authenticates a user and returns a handler-local TokenPair.
func (a *serviceAdapter) Login(ctx context.Context, username, password string) (*authapi.TokenPair, error) {
	res, err := a.service.Login(ctx, username, password)
	if err != nil {
		return nil, err
	}
	// When MFA is required, TokenPair is nil; return an empty token pair
	// carrying only the MFA signals so the frontend can redirect to the
	// MFALogin flow.
	if res.MFARequired {
		return &authapi.TokenPair{
			MFARequired: true,
			MFATicket:   res.MFATicket,
		}, nil
	}
	return &authapi.TokenPair{
		AccessToken:        res.AccessToken,
		RefreshToken:       res.RefreshToken,
		MustChangePassword: res.MustChangePassword,
	}, nil
}

// Logout blacklists the access token and optionally the refresh token.
func (a *serviceAdapter) Logout(ctx context.Context, accessJTI string, accessExpireAt time.Time, refreshToken string) error {
	return a.service.Logout(ctx, accessJTI, accessExpireAt, refreshToken)
}

// RefreshToken validates a refresh token and returns a handler-local TokenPair.
func (a *serviceAdapter) RefreshToken(ctx context.Context, refreshToken string) (*authapi.TokenPair, error) {
	tp, err := a.service.RefreshToken(ctx, refreshToken)
	if err != nil {
		return nil, err
	}
	return &authapi.TokenPair{
		AccessToken:  tp.AccessToken,
		RefreshToken: tp.RefreshToken,
	}, nil
}

// ChangePassword changes the user's password.
func (a *serviceAdapter) ChangePassword(ctx context.Context, userID int64, oldPassword, newPassword, currentJTI string) error {
	return a.service.ChangePassword(ctx, userID, oldPassword, newPassword, currentJTI)
}

// CreateAPIKey generates a new API key and returns the raw key plus metadata.
func (a *serviceAdapter) CreateAPIKey(ctx context.Context, name string, expiredAt *time.Time) (string, *user.APIKey, error) {
	return a.service.CreateAPIKey(ctx, name, expiredAt)
}

// ListAPIKeys returns a page of API keys together with the total count.
func (a *serviceAdapter) ListAPIKeys(ctx context.Context, page, size int) ([]user.APIKey, int64, error) {
	return a.service.ListAPIKeys(ctx, page, size)
}

// RevokeAPIKey revokes an API key by ID.
func (a *serviceAdapter) RevokeAPIKey(ctx context.Context, id int64) error {
	return a.service.RevokeAPIKey(ctx, id)
}

// GetProfile retrieves the profile of the current user identified by userID.
// It delegates to the auth service and projects the user.User into a
// handler-layer UserProfile.
func (a *serviceAdapter) GetProfile(ctx context.Context, userID int64) (*authapi.UserProfile, error) {
	u, err := a.service.GetProfile(ctx, userID)
	if err != nil {
		return nil, err
	}
	return userToProfile(u), nil
}

// UpdateProfile updates the profile of the current user identified by userID.
// It delegates to the auth service and projects the updated user.User into a
// handler-layer UserProfile.
func (a *serviceAdapter) UpdateProfile(ctx context.Context, userID int64, req *authapi.UpdateProfileRequest) (*authapi.UserProfile, error) {
	if req == nil {
		return nil, handler.ErrInvalidRequest
	}
	u, err := a.service.UpdateProfile(ctx, userID, req.Nickname, req.Email, req.Language, req.AlertFormatStyle)
	if err != nil {
		return nil, err
	}
	return userToProfile(u), nil
}

// userToProfile converts a user.User into a handler-layer UserProfile DTO.
func userToProfile(u *user.User) *authapi.UserProfile {
	return &authapi.UserProfile{
		ID:               u.ID,
		Username:         u.Username,
		Nickname:         u.Nickname,
		Email:            u.Email,
		Role:             u.Role,
		Language:         u.Language,
		AlertFormatStyle: u.AlertFormatStyle,
	}
}

// denyAllAssetKeys is a fail-closed asset key getter used when no
// concrete getter is provided. It rejects all asset keys so that
// telemetry report endpoints cannot be accessed without proper configuration.
func denyAllAssetKeys(_ context.Context, _ string) (bool, error) {
	return false, nil
}

// RegisterOption configures route registration with additional handlers and
// services beyond the always-present auth middleware. It uses a variadic
// pattern so existing callers that pass no options continue to work.
type RegisterOption func(*registerConfig)

// registerConfig holds handlers and services injected via RegisterOption.
type registerConfig struct {
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
	templateHandler        *telemetry.TemplateHandler
	healthzHandler         *healthz.Handler
	readyzHandler          *readyz.Handler
	certificateHandler     *certificates.Handler
	i18nHandler            *i18n.Handler
	wsHandler              *wsapi.Handler
}

// WithTaskService provides the task.Service implementation for task
// handlers. When omitted, the task route group is not registered; the
// caller must inject a concrete service to persist tasks and drive
// scheduling.
func WithTaskService(svc task.Service) RegisterOption {
	return func(c *registerConfig) { c.taskSvc = svc }
}

// WithAlertService provides the alert.Service implementation for alert
// handlers. When omitted, the handler package falls back to an in-memory
// implementation.
func WithAlertService(svc alert.Service) RegisterOption {
	return func(c *registerConfig) { c.alertSvc = svc }
}

// WithChannelService provides the channel.Service implementation for
// notification channel handlers. When omitted, the handler package falls
// back to an in-memory implementation.
func WithChannelService(svc channel.Service) RegisterOption {
	return func(c *registerConfig) { c.channelSvc = svc }
}

// WithRemediationRuleService provides the remediation.Service
// implementation for self-healing rule handlers. When omitted, the handler
// package falls back to an in-memory implementation.
func WithRemediationRuleService(svc remediation.Service) RegisterOption {
	return func(c *registerConfig) { c.remediationRuleSvc = svc }
}

// WithSystemService provides the system.Service implementation for system
// handlers (config and info endpoints under /api/v1/system). Must always
// be provided in production; when omitted, the system route group
// handlers will panic on first request.
func WithSystemService(svc system.Service) RegisterOption {
	return func(c *registerConfig) { c.systemSvc = svc }
}

// WithAssetHandler provides the AssetHandler for the asset
// management API at /api/v1/assets. When omitted, the asset
// route group is not registered.
func WithAssetHandler(h *asset.Handler) RegisterOption {
	return func(c *registerConfig) { c.assetHandler = h }
}

// WithTelemetryService provides the telemetry.Service implementation for the
// telemetry collection task CRUD API at /api/v1/telemetry. When omitted, the
// telemetry CRUD route group is not registered.
func WithTelemetryService(svc telemetry.Service) RegisterOption {
	return func(c *registerConfig) { c.telemetrySvc = svc }
}

// WithTelemetryReportHandler provides the ReportHandler for the
// distributed telemetry report endpoints at /api/v1/telemetry/heartbeat,
// /api/v1/telemetry/metrics, and /api/v1/telemetry/logs. When omitted, the
// report route group is not registered.
func WithTelemetryReportHandler(h telemetry.ReportHandler) RegisterOption {
	return func(c *registerConfig) { c.telemetryReportHandler = h }
}

// WithTelemetryDataStores provides the MetricStore and LogStore used by the
// telemetry handler's history/logs endpoints. Both stores may be nil.
func WithTelemetryDataStores(metricStore telemetry.MetricStoreInjector, logStore telemetry.LogStoreInjector) RegisterOption {
	return func(c *registerConfig) {
		c.telemetryMetricStore = metricStore
		c.telemetryLogStore = logStore
	}
}

// WithTemplateHandler provides the TemplateHandler for the telemetry
// template management API at /api/v1/telemetry/templates. When omitted, the
// template route group is not registered.
func WithTemplateHandler(h *telemetry.TemplateHandler) RegisterOption {
	return func(c *registerConfig) { c.templateHandler = h }
}

// WithHealthzHandler provides the HealthzHandler for the /healthz endpoint.
// When omitted, a default stub returning 200 without dependency checks is
// used.
func WithHealthzHandler(h *healthz.Handler) RegisterOption {
	return func(c *registerConfig) { c.healthzHandler = h }
}

// WithReadyzHandler provides the ReadyHandler for the /readyz endpoint.
// When omitted, a default stub returning 200 without dependency checks is
// used.
func WithReadyzHandler(h *readyz.Handler) RegisterOption {
	return func(c *registerConfig) { c.readyzHandler = h }
}

// WithCertificateHandler provides the CertificateHandler for the
// POST /api/v1/system/certificates/reload endpoint. When omitted, the
// certificate reload route is not registered. The handler wraps an
// *api.Server so it must be created after the server is constructed; the
// start command (cmd/tickraft/start.go) wires it when TLS is enabled.
func WithCertificateHandler(h *certificates.Handler) RegisterOption {
	return func(c *registerConfig) { c.certificateHandler = h }
}

// WithI18nHandler provides the I18nHandler for the locale listing API
// at /api/v1/i18n/locales. When omitted, the i18n route group is not
// registered.
// WithWSHandler provides the WebSocket handler for the /ws realtime
// push endpoint. When omitted, the route is not registered.
func WithWSHandler(h *wsapi.Handler) RegisterOption {
	return func(c *registerConfig) { c.wsHandler = h }
}

func WithI18nHandler(h *i18n.Handler) RegisterOption {
	return func(c *registerConfig) { c.i18nHandler = h }
}

// RegisterRoutes builds all auth middleware and an auth service adapter, then
// delegates to handler.RegisterRoutes with the appropriate RouteOption values.
//
// Parameters:
//   - server: the API server to register routes on.
//   - jwtMgr: the JWT manager for token validation.
//   - service: the auth service for login, password, and API key operations.
//   - assetKeyGetter: validates the X-Tickraft-Asset-Key header for
//     telemetry report endpoints. If nil, a fail-closed stub is used.
//   - opts: optional RegisterOption values to inject the task service,
//     alert service, system service, telemetry service, telemetry report
//     handler, asset handler, and healthz handler.
func RegisterRoutes(
	server *api.Server,
	jwtMgr *jwt.JWT,
	service *auth.Service,
	assetKeyGetter func(ctx context.Context, key string) (bool, error),
	opts ...RegisterOption,
) error {
	if server == nil {
		return fmt.Errorf("router: server is nil")
	}
	if jwtMgr == nil {
		return fmt.Errorf("router: jwt manager is nil")
	}
	if service == nil {
		return fmt.Errorf("router: auth service is nil")
	}

	getter := assetKeyGetter
	if getter == nil {
		getter = denyAllAssetKeys
	}

	// Build API key keyGetter for combined auth middleware. A short-TTL
	// cache fronts the per-request DB lookup: API key metadata changes
	// rarely, and the lookup sits on every authenticated request with an
	// API key. Revocations take effect within the TTL window.
	const apiKeyCacheTTL = 30 * time.Second
	var (
		apiKeyCacheMu sync.Mutex
		apiKeyCache   = make(map[string]apiKeyCacheEntry)
	)
	keyGetter := func(ctx context.Context, keyHash string) (*apikey.Info, error) {
		now := time.Now()
		apiKeyCacheMu.Lock()
		if e, ok := apiKeyCache[keyHash]; ok && now.Before(e.expiresAt) {
			apiKeyCacheMu.Unlock()
			return e.info, nil
		}
		apiKeyCacheMu.Unlock()

		stored, err := service.GetAPIKeyByHash(ctx, keyHash)
		if err != nil {
			return nil, err
		}
		info := &apikey.Info{
			ID:        stored.ID,
			Name:      stored.Name,
			KeyPrefix: stored.KeyPrefix,
			KeyHash:   stored.KeyHash,
			Status:    stored.Status,
			CreatedAt: stored.CreatedAt,
			ExpiredAt: stored.ExpiredAt,
		}

		apiKeyCacheMu.Lock()
		// Bound the cache to avoid unbounded growth under key churn.
		if len(apiKeyCache) >= 1024 {
			apiKeyCache = make(map[string]apiKeyCacheEntry)
		}
		apiKeyCache[keyHash] = apiKeyCacheEntry{info: info, expiresAt: now.Add(apiKeyCacheTTL)}
		apiKeyCacheMu.Unlock()
		return info, nil
	}

	// Build middleware instances using the auth/jwt packages.
	authMW := middleware.NewAnyAuth(jwtMgr, keyGetter)
	assetKeyMW := middleware.NewAssetKeyMiddleware(getter)

	// Wrap *auth.Service in the adapter to satisfy authapi.Service.
	adapter := &serviceAdapter{service: service}

	// Apply registrations.
	rc := &registerConfig{}
	for _, opt := range opts {
		opt(rc)
	}

	// Validate that all required domain services are injected. The
	// open-source edition runs as a single standalone process where every
	// component (server, worker, prism) is co-located, so every service
	// must be non-nil. A nil value indicates a wiring bug that would leave
	// the deployment with incomplete routes.
	var missing []string
	if rc.taskSvc == nil {
		missing = append(missing, "task service")
	}
	if rc.alertSvc == nil {
		missing = append(missing, "alert service")
	}
	if rc.channelSvc == nil {
		missing = append(missing, "channel service")
	}
	if rc.remediationRuleSvc == nil {
		missing = append(missing, "remediation rule service")
	}
	if rc.systemSvc == nil {
		missing = append(missing, "system service")
	}
	if rc.telemetrySvc == nil {
		missing = append(missing, "telemetry service")
	}
	if rc.telemetryReportHandler == nil {
		missing = append(missing, "telemetry report handler")
	}
	if rc.assetHandler == nil {
		missing = append(missing, "asset handler")
	}
	if len(missing) > 0 {
		return fmt.Errorf("router: missing required services: %s",
			strings.Join(missing, ", "))
	}

	// Build the RouteOption list with all validated services guaranteed
	// non-nil. Genuinely optional handlers (healthz, readyz, certificates,
	// i18n) remain conditional.
	handlerOpts := []handler.RouteOption{
		handler.WithJWTAuth(authMW),
		handler.WithAssetKeyAuth(assetKeyMW),
		handler.WithAuthService(adapter),
		handler.WithTaskService(rc.taskSvc),
		handler.WithAlertService(rc.alertSvc),
		handler.WithChannelService(rc.channelSvc),
		handler.WithRemediationRuleService(rc.remediationRuleSvc),
		handler.WithSystemService(rc.systemSvc),
		handler.WithTelemetryService(rc.telemetrySvc),
		handler.WithTelemetryReportHandler(rc.telemetryReportHandler),
		handler.WithTelemetryDataStores(rc.telemetryMetricStore, rc.telemetryLogStore),
		handler.WithAssetHandler(rc.assetHandler),
	}
	if rc.healthzHandler != nil {
		handlerOpts = append(handlerOpts, handler.WithHealthzHandler(rc.healthzHandler))
	}
	if rc.readyzHandler != nil {
		handlerOpts = append(handlerOpts, handler.WithReadyHandler(rc.readyzHandler))
	}
	if rc.certificateHandler != nil {
		handlerOpts = append(handlerOpts, handler.WithCertificateHandler(rc.certificateHandler))
	}
	if rc.templateHandler != nil {
		handlerOpts = append(handlerOpts, handler.WithTemplateHandler(rc.templateHandler))
	}
	if rc.i18nHandler != nil {
		handlerOpts = append(handlerOpts, handler.WithI18nHandler(rc.i18nHandler))
	}
	if rc.wsHandler != nil {
		handlerOpts = append(handlerOpts, handler.WithWSHandler(rc.wsHandler))
	}

	// Register all routes via the handler package with injected middleware.
	if err := handler.RegisterRoutes(server, handlerOpts...); err != nil {
		return fmt.Errorf("router: %w", err)
	}

	return nil
}

// Compile-time assertion that serviceAdapter satisfies authapi.Service.
var _ authapi.Service = (*serviceAdapter)(nil)
