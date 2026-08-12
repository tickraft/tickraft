// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package memory

import (
	"context"
	"sync"
	"time"

	"github.com/tickraft/tickraft/pkg/api/handler"
	"github.com/tickraft/tickraft/pkg/api/handler/system"
)

// memorySystemService is an in-memory implementation of system.Service.
type memorySystemService struct {
	mu      sync.RWMutex
	config  system.Config
	startAt time.Time
}

// NewSystemService returns a new in-memory SystemService seeded with default
// configuration values.
func NewSystemService() system.Service {
	return &memorySystemService{
		config: system.Config{
			LogLevel:      "info",
			DefaultLang:   "zh-Hans",
			RetentionDays: 30,
		},
		startAt: time.Now(),
	}
}

// GetConfig returns the current system configuration.
func (s *memorySystemService) GetConfig(_ context.Context) (*system.Config, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cp := s.config
	return &cp, nil
}

// UpdateConfig updates the system configuration and returns the result.
func (s *memorySystemService) UpdateConfig(_ context.Context, req *system.Config) (*system.Config, error) {
	if req == nil {
		return nil, handler.ErrInvalidRequest
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	s.config = *req
	cp := s.config
	return &cp, nil
}

// GetInfo returns the runtime system information.
func (s *memorySystemService) GetInfo(_ context.Context) (*system.Info, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return &system.Info{
		Version:   "dev",
		BuildTags: "",
		StartTime: s.startAt,
		Uptime:    time.Since(s.startAt).Round(time.Second).String(),
	}, nil
}

// GetGlobalStats returns system-wide aggregate statistics. The in-memory
// service has no access to task, asset, or execution stores and returns a
// zero-valued result.
func (s *memorySystemService) GetGlobalStats(_ context.Context) (*system.GlobalStats, error) {
	return &system.GlobalStats{}, nil
}
