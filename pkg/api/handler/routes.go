// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package handler

import (
	"fmt"
	"strings"

	"github.com/tickraft/tickraft/pkg/api"
	"github.com/tickraft/tickraft/pkg/api/handler/alert"
	"github.com/tickraft/tickraft/pkg/api/handler/auth"
	"github.com/tickraft/tickraft/pkg/api/handler/channel"
	"github.com/tickraft/tickraft/pkg/api/handler/healthz"
	"github.com/tickraft/tickraft/pkg/api/handler/readyz"
	"github.com/tickraft/tickraft/pkg/api/handler/remediation"
	"github.com/tickraft/tickraft/pkg/api/handler/system"
	"github.com/tickraft/tickraft/pkg/api/handler/task"
	"github.com/tickraft/tickraft/pkg/api/handler/telemetry"
	"github.com/tickraft/tickraft/pkg/api/middleware"
)

// RegisterRoutes registers all routes on the given server.
//
// Middleware and services are injected via RouteOption values, keeping the
// handler package free of any dependency on pkg/auth or pkg/auth/jwt.
// The caller (internal/api/router/router.go) builds the JWT, API-key, and
// asset-key middleware using those packages and passes the resulting
// app.HandlerFunc values here.
//
// Each domain sub-package exposes its own Handler constructor
// (e.g. auth.NewHandler, task.NewHandler). This function is the composition
// root that wires services into handlers and registers routes.
//
// Required services (authService, taskSvc, alertSvc, systemSvc, telemetrySvc)
// must be non-nil; this function returns an error listing all missing
// services so the caller can fail startup instead of silently registering
// routes backed by nil services.
func RegisterRoutes(server *api.Server, opts ...RouteOption) error {
	cfg := &routeConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	// Validate required core domain services. These services back routes
	// that are always registered regardless of deployment mode, so a nil
	// value would produce routes that panic on first request.
	var missing []string
	if cfg.authService == nil {
		missing = append(missing, "auth service")
	}
	if cfg.taskSvc == nil {
		missing = append(missing, "task service")
	}
	if cfg.alertSvc == nil {
		missing = append(missing, "alert service")
	}
	if cfg.systemSvc == nil {
		missing = append(missing, "system service")
	}
	if cfg.telemetrySvc == nil {
		missing = append(missing, "telemetry service")
	}
	if cfg.jwtMiddleware == nil {
		missing = append(missing, "jwt middleware")
	}
	if len(missing) > 0 {
		return fmt.Errorf("handler: required services not injected: %s", strings.Join(missing, ", "))
	}

	// --- Construct domain handlers ---
	authH := auth.NewHandler(cfg.authService)
	taskH := task.NewHandler(cfg.taskSvc)
	alertH := alert.NewHandler(cfg.alertSvc)
	channelH := channel.NewHandler(cfg.channelSvc)
	remediationH := remediation.NewHandler(cfg.remediationRuleSvc)
	systemH := system.NewHandler(cfg.systemSvc, cfg.authService)
	telemetryH := telemetry.NewHandler(cfg.telemetrySvc)
	telemetryH.SetDataStores(cfg.telemetryMetricStore, cfg.telemetryLogStore)

	// --- Health probes (standalone, no auth) ---

	// Cluster-level health probe. When a concrete HealthzHandler is injected
	// it probes DB/Cache dependencies; otherwise the default stub returns
	// 200 without checks.
	root := server.Group("")
	if cfg.healthzHandler != nil {
		root.GET("/healthz", cfg.healthzHandler.Healthz)
	} else {
		root.GET("/healthz", healthz.DefaultHealthz)
	}

	// Cluster-level readiness probe. When a concrete ReadyHandler is
	// injected it probes DB/Cache dependencies in parallel with a
	// per-check timeout; otherwise the default stub returns 200 without
	// checks. /readyz returns 503 when any dependency is down so a load
	// balancer can route traffic away from a not-yet-ready instance.
	if cfg.readyzHandler != nil {
		root.GET("/readyz", cfg.readyzHandler.Ready)
	} else {
		root.GET("/readyz", readyz.DefaultReady)
	}

	// --- Auth module (public) ---
	authPublic := server.Group("/api/v1/auth")
	authPublic.POST("/login", authH.Login)
	authPublic.POST("/refresh", authH.Refresh)

	// --- Auth module (JWT required) ---
	authJWT := server.Group("/api/v1/auth")
	authJWT.Use(cfg.jwtMiddleware)
	authJWT.POST("/logout", authH.Logout)
	authJWT.PUT("/password", authH.ChangePassword)
	authJWT.GET("/apikeys", middleware.RequirePermission(middleware.ActionRead, "*"), authH.ListAPIKeys)
	authJWT.POST("/apikeys", middleware.RequirePermission(middleware.ActionWrite, "*"), authH.CreateAPIKey)
	authJWT.DELETE("/apikeys/:id", middleware.RequirePermission(middleware.ActionDelete, "*"), authH.RevokeAPIKey)

	// --- Task module (JWT required) ---
	taskGroup := server.Group("/api/v1/tasks")
	taskGroup.Use(cfg.jwtMiddleware)
	taskGroup.GET("", middleware.RequirePermission(middleware.ActionRead, "task"), taskH.ListTasks)
	taskGroup.GET("/:id", middleware.RequirePermission(middleware.ActionRead, "task"), taskH.GetTask)
	taskGroup.POST("", middleware.RequirePermission(middleware.ActionWrite, "task"), taskH.CreateTask)
	taskGroup.PUT("/:id", middleware.RequirePermission(middleware.ActionWrite, "task"), taskH.UpdateTask)
	taskGroup.DELETE("/:id", middleware.RequirePermission(middleware.ActionDelete, "task"), taskH.DeleteTask)
	taskGroup.POST("/:id/trigger", middleware.RequirePermission(middleware.ActionWrite, "task"), taskH.TriggerTask)
	taskGroup.POST("/:id/copy", middleware.RequirePermission(middleware.ActionWrite, "task"), taskH.CopyTask)
	taskGroup.POST("/:id/pause", middleware.RequirePermission(middleware.ActionWrite, "task"), taskH.PauseTask)
	taskGroup.POST("/:id/resume", middleware.RequirePermission(middleware.ActionWrite, "task"), taskH.ResumeTask)

	// --- Task execution records (JWT required) ---
	taskGroup.GET("/:id/executions", middleware.RequirePermission(middleware.ActionRead, "task"), taskH.ListExecutions)
	taskGroup.GET("/:id/executions/:execId", middleware.RequirePermission(middleware.ActionRead, "task"), taskH.GetExecution)

	// --- Task statistics (JWT required) ---
	taskStatsGroup := server.Group("/api/v1/tasks")
	taskStatsGroup.Use(cfg.jwtMiddleware)
	taskStatsGroup.GET("/stats", middleware.RequirePermission(middleware.ActionRead, "task"), taskH.GetExecutionStats)

	// --- Alert module (JWT required) ---
	alertRuleGroup := server.Group("/api/v1/prism/alert/rules")
	alertRuleGroup.Use(cfg.jwtMiddleware)
	alertRuleGroup.GET("", middleware.RequirePermission(middleware.ActionRead, "alert"), alertH.ListAlertRules)
	alertRuleGroup.GET("/:id", middleware.RequirePermission(middleware.ActionRead, "alert"), alertH.GetAlertRule)
	alertRuleGroup.POST("", middleware.RequirePermission(middleware.ActionWrite, "alert"), alertH.CreateAlertRule)
	alertRuleGroup.PUT("/:id", middleware.RequirePermission(middleware.ActionWrite, "alert"), alertH.UpdateAlertRule)
	alertRuleGroup.DELETE("/:id", middleware.RequirePermission(middleware.ActionDelete, "alert"), alertH.DeleteAlertRule)

	alertRecordGroup := server.Group("/api/v1/prism/alert/records")
	alertRecordGroup.Use(cfg.jwtMiddleware)
	alertRecordGroup.GET("", middleware.RequirePermission(middleware.ActionRead, "alert"), alertH.ListAlertRecords)
	alertRecordGroup.GET("/:id", middleware.RequirePermission(middleware.ActionRead, "alert"), alertH.GetAlertRecord)
	alertRecordGroup.PUT("/:id/acknowledge", middleware.RequirePermission(middleware.ActionWrite, "alert"), alertH.AcknowledgeAlertRecord)
	alertRecordGroup.PUT("/:id/resolve", middleware.RequirePermission(middleware.ActionWrite, "alert"), alertH.ResolveAlertRecord)

	// --- Notification channel module (JWT required) ---
	// Only registered when a ChannelService is injected. This follows the
	// same pattern as asset and telemetry routes: when no service is
	// provided, the route group is not registered (an external plugin
	// may provide its own implementation).
	if cfg.channelSvc != nil {
		channelGroup := server.Group("/api/v1/prism/channels")
		channelGroup.Use(cfg.jwtMiddleware)
		channelGroup.GET("", middleware.RequirePermission(middleware.ActionRead, "*"), channelH.ListChannels)
		channelGroup.GET("/:id", middleware.RequirePermission(middleware.ActionRead, "*"), channelH.GetChannel)
		channelGroup.POST("", middleware.RequirePermission(middleware.ActionWrite, "*"), channelH.CreateChannel)
		channelGroup.PUT("/:id", middleware.RequirePermission(middleware.ActionWrite, "*"), channelH.UpdateChannel)
		channelGroup.DELETE("/:id", middleware.RequirePermission(middleware.ActionDelete, "*"), channelH.DeleteChannel)
		channelGroup.POST("/:id/test", middleware.RequirePermission(middleware.ActionWrite, "*"), channelH.TestChannel)
	}

	// --- Remediation rule module (JWT required) ---
	// Only registered when a RemediationRuleService is injected. See the
	// channel module comment above for the rationale.
	if cfg.remediationRuleSvc != nil {
		remediationRuleGroup := server.Group("/api/v1/prism/remediation/rules")
		remediationRuleGroup.Use(cfg.jwtMiddleware)
		remediationRuleGroup.GET("", middleware.RequirePermission(middleware.ActionRead, "*"), remediationH.ListRemediationRules)
		remediationRuleGroup.GET("/:id", middleware.RequirePermission(middleware.ActionRead, "*"), remediationH.GetRemediationRule)
		remediationRuleGroup.POST("", middleware.RequirePermission(middleware.ActionWrite, "*"), remediationH.CreateRemediationRule)
		remediationRuleGroup.PUT("/:id", middleware.RequirePermission(middleware.ActionWrite, "*"), remediationH.UpdateRemediationRule)
		remediationRuleGroup.DELETE("/:id", middleware.RequirePermission(middleware.ActionDelete, "*"), remediationH.DeleteRemediationRule)

		// Remediation dispatch records. Registered alongside the rule
		// routes because both are injected via the remediation rule
		// service option; the path is a sibling of /rules to avoid
		// conflicting with the /rules/:id wildcard.
		remediationRecordGroup := server.Group("/api/v1/prism/remediation/records")
		remediationRecordGroup.Use(cfg.jwtMiddleware)
		remediationRecordGroup.GET("", middleware.RequirePermission(middleware.ActionRead, "*"), remediationH.ListRemediationRecords)
	}

	// --- System module (JWT required) ---
	systemGroup := server.Group("/api/v1/system")
	systemGroup.Use(cfg.jwtMiddleware)
	systemGroup.GET("/config", middleware.RequirePermission(middleware.ActionRead, "*"), systemH.GetSystemConfig)
	systemGroup.PUT("/config", middleware.RequirePermission(middleware.ActionWrite, "*"), systemH.UpdateSystemConfig)
	systemGroup.GET("/info", middleware.RequirePermission(middleware.ActionRead, "*"), systemH.GetSystemInfo)
	systemGroup.GET("/stats", middleware.RequirePermission(middleware.ActionRead, "*"), systemH.GetGlobalStats)
	systemGroup.GET("/profile", middleware.RequirePermission(middleware.ActionRead, "*"), systemH.GetProfile)
	systemGroup.PUT("/profile", middleware.RequirePermission(middleware.ActionWrite, "*"), systemH.UpdateProfile)

	// --- Certificate management (JWT required, optional injection) ---
	if cfg.certificateHandler != nil {
		systemGroup.POST("/certificates/reload", middleware.RequirePermission(middleware.ActionWrite, "*"), cfg.certificateHandler.Reload)
	}

	// --- Asset management (JWT required) ---
	if cfg.assetHandler != nil {
		assetGroup := server.Group("/api/v1/assets")
		assetGroup.Use(cfg.jwtMiddleware)
		assetGroup.GET("", middleware.RequirePermission(middleware.ActionRead, "device"), cfg.assetHandler.ListAssets)
		assetGroup.GET("/:id", middleware.RequirePermission(middleware.ActionRead, "device"), cfg.assetHandler.GetAsset)
		assetGroup.POST("", middleware.RequirePermission(middleware.ActionWrite, "device"), cfg.assetHandler.CreateAsset)
		assetGroup.PUT("/:id", middleware.RequirePermission(middleware.ActionWrite, "device"), cfg.assetHandler.UpdateAsset)
		assetGroup.DELETE("/:id", middleware.RequirePermission(middleware.ActionDelete, "device"), cfg.assetHandler.DeleteAsset)
		assetGroup.PUT("/:id/status", middleware.RequirePermission(middleware.ActionWrite, "device"), cfg.assetHandler.UpdateAssetStatus)
		assetGroup.POST("/:id/probe", middleware.RequirePermission(middleware.ActionWrite, "device"), cfg.assetHandler.ProbeAsset)
	}

	// --- Telemetry prober/listener type metadata (JWT required) ---
	telemetryMetaGroup := server.Group("/api/v1/telemetry")
	telemetryMetaGroup.Use(cfg.jwtMiddleware)
	telemetryMetaGroup.GET("/probers", middleware.RequirePermission(middleware.ActionRead, "*"), telemetryH.ListProbers)
	telemetryMetaGroup.GET("/listeners", middleware.RequirePermission(middleware.ActionRead, "*"), telemetryH.ListListeners)

	// --- Telemetry monitor CRUD and templates (JWT required) ---
	if cfg.telemetrySvc != nil || cfg.templateHandler != nil {
		telemetryGroup := server.Group("/api/v1/telemetry")
		telemetryGroup.Use(cfg.jwtMiddleware)
		if cfg.telemetrySvc != nil {
			telemetryGroup.GET("/monitors", middleware.RequirePermission(middleware.ActionRead, "device"), telemetryH.ListTelemetry)
			telemetryGroup.GET("/monitors/:id", middleware.RequirePermission(middleware.ActionRead, "device"), telemetryH.GetTelemetry)
			telemetryGroup.POST("/monitors", middleware.RequirePermission(middleware.ActionWrite, "device"), telemetryH.CreateTelemetry)
			telemetryGroup.PUT("/monitors/:id", middleware.RequirePermission(middleware.ActionWrite, "device"), telemetryH.UpdateTelemetry)
			telemetryGroup.DELETE("/monitors/:id", middleware.RequirePermission(middleware.ActionDelete, "device"), telemetryH.DeleteTelemetry)
			telemetryGroup.GET("/monitors/:id/status", middleware.RequirePermission(middleware.ActionRead, "device"), telemetryH.GetMonitorStatus)
			telemetryGroup.GET("/monitors/:id/history", middleware.RequirePermission(middleware.ActionRead, "device"), telemetryH.GetMonitorHistory)
			telemetryGroup.POST("/monitors/:id/probe", middleware.RequirePermission(middleware.ActionWrite, "device"), telemetryH.ProbeMonitor)
			telemetryGroup.GET("/monitors/:id/logs", middleware.RequirePermission(middleware.ActionRead, "device"), telemetryH.GetMonitorLogs)
			telemetryGroup.PUT("/monitors/:id/enable", middleware.RequirePermission(middleware.ActionWrite, "device"), telemetryH.EnableMonitor)
			telemetryGroup.PUT("/monitors/:id/disable", middleware.RequirePermission(middleware.ActionWrite, "device"), telemetryH.DisableMonitor)
		}
		if cfg.templateHandler != nil {
			th := cfg.templateHandler
			telemetryGroup.GET("/templates", middleware.RequirePermission(middleware.ActionRead, "*"), th.ListTemplates)
			telemetryGroup.GET("/templates/builtin", middleware.RequirePermission(middleware.ActionRead, "*"), th.ListBuiltinTemplates)
			telemetryGroup.GET("/templates/:id", middleware.RequirePermission(middleware.ActionRead, "*"), th.GetTemplate)
			telemetryGroup.POST("/templates", middleware.RequirePermission(middleware.ActionWrite, "*"), th.CreateTemplate)
			telemetryGroup.PUT("/templates/:id", middleware.RequirePermission(middleware.ActionWrite, "*"), th.UpdateTemplate)
			telemetryGroup.DELETE("/templates/:id", middleware.RequirePermission(middleware.ActionDelete, "*"), th.DeleteTemplate)
			telemetryGroup.POST("/templates/:id/apply", middleware.RequirePermission(middleware.ActionWrite, "*"), th.ApplyTemplate)
		}
	}

	// --- Telemetry unified report endpoint (AssetKey required) ---
	if cfg.telemetryReportHandler != nil {
		reportGroup := server.Group("/api/v1/telemetry")
		if cfg.assetKeyMiddleware != nil {
			reportGroup.Use(cfg.assetKeyMiddleware)
		}
		reportGroup.POST("", cfg.telemetryReportHandler.Report)
	}

	// --- WebSocket realtime push (query-token auth) ---
	if cfg.wsHandler != nil {
		root.GET("/ws", cfg.wsHandler.ServeHTTP)
	}

	// --- i18n locale listing (public, no auth) ---
	if cfg.i18nHandler != nil {
		i18nGroup := server.Group("/api/v1/i18n")
		i18nGroup.GET("/locales", cfg.i18nHandler.ListLocales)
	}

	return nil
}
