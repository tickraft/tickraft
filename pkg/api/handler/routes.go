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
	if cfg.jwtMiddleware != nil {
		authJWT.Use(cfg.jwtMiddleware)
	}
	authJWT.POST("/logout", authH.Logout)
	authJWT.PUT("/password", authH.ChangePassword)
	authJWT.GET("/apikeys", authH.ListAPIKeys)
	authJWT.POST("/apikeys", authH.CreateAPIKey)
	authJWT.DELETE("/apikeys/:id", authH.RevokeAPIKey)

	// --- Task module (JWT required) ---
	taskGroup := server.Group("/api/v1/tasks")
	if cfg.jwtMiddleware != nil {
		taskGroup.Use(cfg.jwtMiddleware)
	}
	taskGroup.GET("", taskH.ListTasks)
	taskGroup.GET("/:id", taskH.GetTask)
	taskGroup.POST("", taskH.CreateTask)
	taskGroup.PUT("/:id", taskH.UpdateTask)
	taskGroup.DELETE("/:id", taskH.DeleteTask)
	taskGroup.POST("/:id/trigger", taskH.TriggerTask)
	taskGroup.POST("/:id/copy", taskH.CopyTask)
	taskGroup.POST("/:id/pause", taskH.PauseTask)
	taskGroup.POST("/:id/resume", taskH.ResumeTask)

	// --- Task execution records (JWT required) ---
	taskGroup.GET("/:id/executions", taskH.ListExecutions)
	taskGroup.GET("/:id/executions/:execId", taskH.GetExecution)

	// --- Task statistics (JWT required) ---
	taskStatsGroup := server.Group("/api/v1/tasks")
	if cfg.jwtMiddleware != nil {
		taskStatsGroup.Use(cfg.jwtMiddleware)
	}
	taskStatsGroup.GET("/stats", taskH.GetExecutionStats)

	// --- Alert module (JWT required) ---
	alertRuleGroup := server.Group("/api/v1/prism/alert/rules")
	if cfg.jwtMiddleware != nil {
		alertRuleGroup.Use(cfg.jwtMiddleware)
	}
	alertRuleGroup.GET("", alertH.ListAlertRules)
	alertRuleGroup.GET("/:id", alertH.GetAlertRule)
	alertRuleGroup.POST("", alertH.CreateAlertRule)
	alertRuleGroup.PUT("/:id", alertH.UpdateAlertRule)
	alertRuleGroup.DELETE("/:id", alertH.DeleteAlertRule)

	alertRecordGroup := server.Group("/api/v1/prism/alert/records")
	if cfg.jwtMiddleware != nil {
		alertRecordGroup.Use(cfg.jwtMiddleware)
	}
	alertRecordGroup.GET("", alertH.ListAlertRecords)
	alertRecordGroup.GET("/:id", alertH.GetAlertRecord)
	alertRecordGroup.PUT("/:id/acknowledge", alertH.AcknowledgeAlertRecord)
	alertRecordGroup.PUT("/:id/resolve", alertH.ResolveAlertRecord)

	// --- Notification channel module (JWT required) ---
	// Only registered when a ChannelService is injected. This follows the
	// same pattern as asset and telemetry routes: when no service is
	// provided, the route group is not registered (an external plugin
	// may provide its own implementation).
	if cfg.channelSvc != nil {
		channelGroup := server.Group("/api/v1/prism/channels")
		if cfg.jwtMiddleware != nil {
			channelGroup.Use(cfg.jwtMiddleware)
		}
		channelGroup.GET("", channelH.ListChannels)
		channelGroup.GET("/:id", channelH.GetChannel)
		channelGroup.POST("", channelH.CreateChannel)
		channelGroup.PUT("/:id", channelH.UpdateChannel)
		channelGroup.DELETE("/:id", channelH.DeleteChannel)
		channelGroup.POST("/:id/test", channelH.TestChannel)
	}

	// --- Remediation rule module (JWT required) ---
	// Only registered when a RemediationRuleService is injected. See the
	// channel module comment above for the rationale.
	if cfg.remediationRuleSvc != nil {
		remediationRuleGroup := server.Group("/api/v1/prism/remediation/rules")
		if cfg.jwtMiddleware != nil {
			remediationRuleGroup.Use(cfg.jwtMiddleware)
		}
		remediationRuleGroup.GET("", remediationH.ListRemediationRules)
		remediationRuleGroup.GET("/:id", remediationH.GetRemediationRule)
		remediationRuleGroup.POST("", remediationH.CreateRemediationRule)
		remediationRuleGroup.PUT("/:id", remediationH.UpdateRemediationRule)
		remediationRuleGroup.DELETE("/:id", remediationH.DeleteRemediationRule)
	}

	// --- System module (JWT required) ---
	systemGroup := server.Group("/api/v1/system")
	if cfg.jwtMiddleware != nil {
		systemGroup.Use(cfg.jwtMiddleware)
	}
	systemGroup.GET("/config", systemH.GetSystemConfig)
	systemGroup.PUT("/config", systemH.UpdateSystemConfig)
	systemGroup.GET("/info", systemH.GetSystemInfo)
	systemGroup.GET("/stats", systemH.GetGlobalStats)
	systemGroup.GET("/profile", systemH.GetProfile)
	systemGroup.PUT("/profile", systemH.UpdateProfile)

	// --- Certificate management (JWT required, optional injection) ---
	if cfg.certificateHandler != nil {
		systemGroup.POST("/certificates/reload", cfg.certificateHandler.Reload)
	}

	// --- Asset management (JWT required) ---
	if cfg.assetHandler != nil {
		assetGroup := server.Group("/api/v1/assets")
		if cfg.jwtMiddleware != nil {
			assetGroup.Use(cfg.jwtMiddleware)
		}
		assetGroup.GET("", cfg.assetHandler.ListAssets)
		assetGroup.GET("/:id", cfg.assetHandler.GetAsset)
		assetGroup.POST("", cfg.assetHandler.CreateAsset)
		assetGroup.PUT("/:id", cfg.assetHandler.UpdateAsset)
		assetGroup.DELETE("/:id", cfg.assetHandler.DeleteAsset)
		assetGroup.PUT("/:id/status", cfg.assetHandler.UpdateAssetStatus)
		assetGroup.POST("/:id/probe", cfg.assetHandler.ProbeAsset)
	}

	// --- Telemetry prober/listener type metadata (JWT required) ---
	telemetryMetaGroup := server.Group("/api/v1/telemetry")
	if cfg.jwtMiddleware != nil {
		telemetryMetaGroup.Use(cfg.jwtMiddleware)
	}
	telemetryMetaGroup.GET("/probers", telemetryH.ListProbers)
	telemetryMetaGroup.GET("/listeners", telemetryH.ListListeners)

	// --- Telemetry monitor CRUD and templates (JWT required) ---
	if cfg.telemetrySvc != nil || cfg.templateHandler != nil {
		telemetryGroup := server.Group("/api/v1/telemetry")
		if cfg.jwtMiddleware != nil {
			telemetryGroup.Use(cfg.jwtMiddleware)
		}
		if cfg.telemetrySvc != nil {
			telemetryGroup.GET("/monitors", telemetryH.ListTelemetry)
			telemetryGroup.GET("/monitors/:id", telemetryH.GetTelemetry)
			telemetryGroup.POST("/monitors", telemetryH.CreateTelemetry)
			telemetryGroup.PUT("/monitors/:id", telemetryH.UpdateTelemetry)
			telemetryGroup.DELETE("/monitors/:id", telemetryH.DeleteTelemetry)
			telemetryGroup.GET("/monitors/:id/status", telemetryH.GetMonitorStatus)
			telemetryGroup.GET("/monitors/:id/history", telemetryH.GetMonitorHistory)
			telemetryGroup.POST("/monitors/:id/probe", telemetryH.ProbeMonitor)
			telemetryGroup.GET("/monitors/:id/logs", telemetryH.GetMonitorLogs)
			telemetryGroup.PUT("/monitors/:id/enable", telemetryH.EnableMonitor)
			telemetryGroup.PUT("/monitors/:id/disable", telemetryH.DisableMonitor)
		}
		if cfg.templateHandler != nil {
			th := cfg.templateHandler
			telemetryGroup.GET("/templates", th.ListTemplates)
			telemetryGroup.GET("/templates/builtin", th.ListBuiltinTemplates)
			telemetryGroup.GET("/templates/:id", th.GetTemplate)
			telemetryGroup.POST("/templates", th.CreateTemplate)
			telemetryGroup.PUT("/templates/:id", th.UpdateTemplate)
			telemetryGroup.DELETE("/templates/:id", th.DeleteTemplate)
			telemetryGroup.POST("/templates/:id/apply", th.ApplyTemplate)
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

	// --- i18n locale listing (public, no auth) ---
	if cfg.i18nHandler != nil {
		i18nGroup := server.Group("/api/v1/i18n")
		i18nGroup.GET("/locales", cfg.i18nHandler.ListLocales)
	}

	return nil
}
