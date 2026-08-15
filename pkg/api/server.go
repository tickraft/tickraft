// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package api

import (
	"context"
	"crypto/tls"
	"fmt"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/config"
	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/hertz-contrib/http2/factory"
	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/api/hlogzap"
	"github.com/tickraft/tickraft/pkg/api/middleware"
)

// SetLogger bridges the Hertz hlog system to the given zap logger. It must
// be called before NewServer so the hlog.SetLevel call inside NewServer
// targets the bridged logger rather than the default stderr-based writer.
//
// After this call, all hlog output (middleware recovery logs, access logs,
// TLS reload warnings, ACME events, shutdown plugin errors) flows through
// the zap logger, satisfying the workspace rule that all logs must go
// through zap.
func SetLogger(logger *zap.Logger) {
	hlog.SetLogger(hlogzap.NewLogger(logger))
}

// Server wraps a Hertz engine and manages plugins, middleware, and lifecycle.
type Server struct {
	hertz       *server.Hertz
	config      ServerConfig
	plugins     []Plugin
	vhostConfig *VirtualHostConfig
	vhostRouter *VirtualHostRouter

	// tlsHolder holds the active *tls.Config when TLSEnabled is true. Storing
	// the pointer atomically lets the GetCertificate / GetConfigForClient
	// callbacks consult the latest certificate on every TLS handshake without
	// taking a mutex on the hot path. See ReloadTLSConfig and
	// startTLSFileWatcher in tls.go.
	tlsHolder tlsConfigHolder

	// stopTLSWatcher stops the fsnotify watcher goroutine started by
	// startTLSFileWatcher. It is nil when TLS is disabled or the watcher
	// failed to start; Shutdown skips it in that case.
	stopTLSWatcher func()
}

// NewServer creates a Server with the given configuration.
func NewServer(cfg ServerConfig) *Server {
	opts := []config.Option{
		server.WithHostPorts(cfg.Addr),
		server.WithReadTimeout(cfg.ReadTimeout),
		server.WithWriteTimeout(cfg.WriteTimeout),
	}
	if cfg.MaxHeaderBytes > 0 {
		opts = append(opts, server.WithMaxHeaderBytes(cfg.MaxHeaderBytes))
	}
	if cfg.TLSEnabled {
		// Install a placeholder *tls.Config. The GetCertificate and
		// GetConfigForClient callbacks installed by ReloadTLSConfig read the
		// live certificate from s.tlsHolder on every handshake, so this
		// initial config only needs to enable TLS so Hertz switches to the
		// standard TLS-capable transporter. ReloadTLSConfig must be called
		// before Start for the live certificate to take effect; the start
		// command (cmd/tickraft/start.go) does this as part of API-server
		// wiring.
		//
		// HTTP/2 over TLS (h2) is enabled via ALPN: WithALPN(true) tells
		// Hertz to negotiate the application protocol during the TLS
		// handshake. The h2 protocol factory is registered on the engine
		// below (see the AddProtocol call after server.New) so h2-capable
		// clients upgrade automatically while HTTP/1.1-only clients
		// continue to work.
		opts = append(opts, server.WithTLS(&tls.Config{
			MinVersion: tls.VersionTLS12,
			// NextProtos is populated by Hertz once ALPN is enabled.
		}), server.WithALPN(true))
	}

	// Use server.New (without default recovery) so we can control
	// the exact middleware order ourselves.
	h := server.New(opts...)

	// Register the HTTP/2 (h2) protocol factory when TLS is enabled so
	// h2-capable clients upgrade automatically via ALPN negotiation while
	// HTTP/1.1-only clients keep working. WithALPN(true) above lets the
	// TLS handshake negotiate the application protocol; AddProtocol
	// installs the server-side HTTP/2 handler that ALPN selects. The
	// runtime tls.Config built by buildTLSConfig advertises
	// NextProtos ["h2","http/1.1"], so a client offering both protocols
	// is served over h2 while HTTP/1.1-only clients fall back to HTTP/1.1.
	if cfg.TLSEnabled {
		h.AddProtocol("h2", factory.NewServerFactory())
	}

	// Apply log level based on mode.
	switch cfg.Mode {
	case "release":
		hlog.SetLevel(hlog.LevelWarn)
	default:
		hlog.SetLevel(hlog.LevelDebug)
	}

	s := &Server{
		hertz:   h,
		config:  cfg,
		plugins: nil,
	}

	if cfg.VirtualHosts != nil {
		s.vhostConfig = cfg.VirtualHosts
		s.vhostRouter = NewVirtualHostRouter(*cfg.VirtualHosts)
	}

	return s
}

// Group creates a root-level route group with the given prefix.
func (s *Server) Group(prefix string) *RouterGroup {
	rg := s.hertz.Group(prefix)
	return newRouterGroup(rg)
}

// RegisterPlugin appends a plugin to the internal list.
func (s *Server) RegisterPlugin(p Plugin) {
	s.plugins = append(s.plugins, p)
}

// DefaultGroup returns the default route group used when VirtualHost is
// disabled or no host match is found. When VirtualHost is disabled, this
// returns a root-level group equivalent to Group("").
func (s *Server) DefaultGroup() *RouterGroup {
	return s.Group("")
}

// VhostRouter returns the VirtualHost router, or nil when VirtualHost is
// disabled.
func (s *Server) VhostRouter() *VirtualHostRouter {
	return s.vhostRouter
}

// Start applies all middleware in order, registers plugin routes, calls
// OnStart for each plugin, then starts the Hertz engine.
//
// Middleware order: RequestID -> AccessLog -> Recovery -> CORS (if enabled)
// -> TrustedProxy (if configured) -> Plugin middlewares -> route-group middleware.
//
// When TLSEnabled is true Start also kicks off the certificate file watcher
// (see startTLSFileWatcher). Callers that want to bootstrap TLS before serving
// traffic should call ReloadTLSConfig first; the start command does this as
// part of API-server wiring so the placeholder *tls.Config installed by
// NewServer is replaced before any handshake occurs.
//
// The runtime runs exclusively in single-port mode: the server
// listens on ServerConfig.Addr and serves the API, telemetry webhooks, SPA
// frontend, and health checks from that one port via path-based routing.
func (s *Server) Start() error {
	s.applyMiddleware(s.hertz)

	rootGroup := s.hertz.Group("")
	wrappedRoot := newRouterGroup(rootGroup)
	for _, p := range s.plugins {
		p.RegisterRoutes(wrappedRoot)
	}

	ctx := context.Background()
	for _, p := range s.plugins {
		if err := p.OnStart(ctx); err != nil {
			return fmt.Errorf("plugin %q OnStart failed: %w", p.Name(), err)
		}
	}

	// Start the certificate file watcher only after the engine is fully wired
	// so a reload can never race with route registration. The watcher is
	// best-effort: it logs failures but never aborts startup, leaving manual
	// reload via the API as a reliable fallback.
	if s.config.TLSEnabled {
		s.stopTLSWatcher = s.startTLSFileWatcher()
	}

	return s.hertz.Run()
}

// applyMiddleware applies the built-in and trusted-proxy middleware to the
// given Hertz engine.
//
// Middleware order (request phase, outer to inner):
// Gzip → RequestID → AccessLog → Recovery → CORS → TrustedProxy → Locale
// → Plugin middlewares → route-group middleware.
//
// Gzip is registered first so its response-phase code (post-compression)
// runs last, after every downstream middleware has finalized the response
// body and headers.
func (s *Server) applyMiddleware(h *server.Hertz) {
	h.Use(middleware.Gzip())
	h.Use(middleware.RequestID())
	if s.config.EnableLog {
		h.Use(middleware.AccessLog())
	}
	h.Use(middleware.Recovery())
	if s.config.EnableCORS {
		h.Use(middleware.CORS(s.config.AllowedOrigins))
	}
	if len(s.config.TrustedProxies) > 0 {
		h.Use(middleware.NewTrustedProxyMiddleware(s.config.TrustedProxies))
	}
	h.Use(middleware.NewLocaleMiddleware())
	for _, p := range s.plugins {
		for _, mw := range p.Middlewares() {
			h.Use(mw)
		}
	}
}

// Shutdown calls OnStop in reverse order for each plugin, stops the TLS file
// watcher if it was started, then shuts down the Hertz engine gracefully.
func (s *Server) Shutdown(ctx context.Context) error {
	for i := len(s.plugins) - 1; i >= 0; i-- {
		if err := s.plugins[i].OnStop(ctx); err != nil {
			hlog.SystemLogger().Errorf("plugin %q OnStop failed: %v", s.plugins[i].Name(), err)
		}
	}

	if s.stopTLSWatcher != nil {
		s.stopTLSWatcher()
		s.stopTLSWatcher = nil
	}

	return s.hertz.Shutdown(ctx)
}
