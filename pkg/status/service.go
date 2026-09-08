// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package status

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

// defaultCacheTTL is how long a successful public render is reused before
// the aggregation runs again. It bounds the load an unauthenticated
// endpoint can put on the monitor and probe stores.
const defaultCacheTTL = 30 * time.Second

// Compile-time interface compliance check.
var _ Service = (*StatusService)(nil)

// StatusService aggregates monitor points, their latest probe records,
// and the injected infrastructure probes into the public status view.
//
//nolint:revive // intentional stutter: mirrors the <Domain>Service convention
type StatusService struct {
	store         *Store
	monitors      MonitorLister
	probes        ProbeReader
	systemProbes  []SystemProbe
	logger        *zap.Logger
	cacheTTL      time.Duration
	mu            sync.Mutex
	cached        *PublicView
	cacheExpires  time.Time
	configVersion int64
}

// Option customizes the status service.
type Option func(*StatusService)

// WithCacheTTL overrides the public view cache TTL. A non-positive value
// disables caching.
func WithCacheTTL(d time.Duration) Option {
	return func(s *StatusService) { s.cacheTTL = d }
}

// WithSystemProbe appends an infrastructure health check rendered as an
// extra component after the monitor-derived ones.
func WithSystemProbe(p SystemProbe) Option {
	return func(s *StatusService) { s.systemProbes = append(s.systemProbes, p) }
}

// NewService creates a status Service over the configuration store and
// the aggregation data sources. monitors and probes may be nil, in which
// case monitor-derived components render as unknown.
func NewService(store *Store, monitors MonitorLister, probes ProbeReader, logger *zap.Logger, opts ...Option) Service {
	s := &StatusService{
		store:    store,
		monitors: monitors,
		probes:   probes,
		logger:   logger,
		cacheTTL: defaultCacheTTL,
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// GetConfig returns the current status page configuration.
func (s *StatusService) GetConfig(ctx context.Context) (*Config, error) {
	return s.store.Get(ctx)
}

// UpdateConfig validates and persists the configuration and invalidates
// the cached public view so the next render reflects the change
// immediately.
func (s *StatusService) UpdateConfig(ctx context.Context, cfg *Config) (*Config, error) {
	if err := validateConfig(cfg); err != nil {
		return nil, err
	}
	updated, err := s.store.Update(ctx, cfg)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.cached = nil
	s.configVersion++
	s.mu.Unlock()
	return updated, nil
}

// validateConfig checks the configuration payload.
func validateConfig(cfg *Config) error {
	if cfg == nil {
		return &ErrValidation{Msg: "config is required"}
	}
	if cfg.Title == "" {
		return &ErrValidation{Msg: "title is required"}
	}
	if len(cfg.Title) > 255 {
		return &ErrValidation{Msg: "title exceeds 255 characters"}
	}
	if len(cfg.Description) > 1024 {
		return &ErrValidation{Msg: "description exceeds 1024 characters"}
	}
	seen := make(map[string]bool, len(cfg.Components))
	for i, c := range cfg.Components {
		if c.Name == "" {
			return &ErrValidation{Msg: fmt.Sprintf("components[%d]: name is required", i)}
		}
		if seen[c.Name] {
			return &ErrValidation{Msg: fmt.Sprintf("components[%d]: duplicate name %s", i, c.Name)}
		}
		seen[c.Name] = true
	}
	return nil
}

// PublicView renders the aggregated page. Successful renders are cached
// for the configured TTL; configuration updates invalidate the cache.
func (s *StatusService) PublicView(ctx context.Context) (*PublicView, error) {
	cfg, err := s.store.Get(ctx)
	if err != nil {
		return nil, err
	}
	if !cfg.Enabled {
		return nil, ErrDisabled
	}

	s.mu.Lock()
	version := s.configVersion
	if s.cached != nil && time.Now().Before(s.cacheExpires) {
		view := s.cached
		s.mu.Unlock()
		return view, nil
	}
	s.mu.Unlock()

	view, err := s.render(ctx, cfg)
	if err != nil {
		return nil, err
	}

	// Only cache the render when the configuration did not change while
	// aggregating; otherwise the next call re-renders with fresh data.
	s.mu.Lock()
	if s.configVersion == version {
		s.cached = view
		s.cacheExpires = time.Now().Add(s.cacheTTL)
	}
	s.mu.Unlock()
	return view, nil
}

// render aggregates the monitor topology into the public view via the
// package-level RenderComponents seam.
func (s *StatusService) render(ctx context.Context, cfg *Config) (*PublicView, error) {
	comps, err := RenderComponents(ctx, Sources{
		Monitors: s.monitors,
		Probes:   s.probes,
		Logger:   s.logger,
	}, s.systemProbes, cfg.Components)
	if err != nil {
		return nil, err
	}
	return &PublicView{
		Title:       cfg.Title,
		Description: cfg.Description,
		UpdatedAt:   time.Now(),
		Components:  comps,
		Overall:     Overall(comps),
	}, nil
}
