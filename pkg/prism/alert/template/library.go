// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package template

import (
	"errors"
	"sort"
	"sync"

	"go.uber.org/zap"
)

// Library is a registry of Templates. The kernel ships a built-in library;
// callers may extend it with tenant-specific custom templates by
// wrapping the built-in Library in a Store-backed implementation.
//
// All methods are safe for concurrent use.
type Library interface {
	// Register adds a template to the library. Registering a template with
	// an ID that already exists replaces the previous template; callers
	// that want to detect duplicates should call Get first.
	Register(t Template)
	// Get returns the template with the given ID, or ErrTemplateNotFound.
	Get(id string) (Template, error)
	// List returns all registered templates sorted by ID. The returned
	// slice is a copy and may be modified freely.
	List() []Template
}

// library is the default Library implementation.
type library struct {
	mu        sync.RWMutex
	templates map[string]Template
	logger    *zap.Logger
}

// NewLibrary creates an empty Library. A nil logger is replaced with a
// no-op logger.
func NewLibrary(logger *zap.Logger) Library {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &library{
		templates: make(map[string]Template),
		logger:    logger,
	}
}

// Register implements Library.
func (l *library) Register(t Template) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.templates[t.ID] = t
}

// Get implements Library.
func (l *library) Get(id string) (Template, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	t, ok := l.templates[id]
	if !ok {
		return Template{}, ErrTemplateNotFound
	}
	return t, nil
}

// List implements Library.
func (l *library) List() []Template {
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := make([]Template, 0, len(l.templates))
	for _, t := range l.templates {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out
}

// NewBuiltinLibrary creates a Library pre-populated with the built-in
// templates embedded in the builtin/ subpackage. Each template is validated
// before registration; invalid templates are skipped with a warning log.
// A nil logger is replaced with a no-op logger.
func NewBuiltinLibrary(logger *zap.Logger) Library {
	if logger == nil {
		logger = zap.NewNop()
	}

	//nolint:errcheck
	l := NewLibrary(logger).(*library) // factory always returns *library
	for _, t := range loadBuiltinTemplates(logger) {
		if err := Validate(t); err != nil {
			l.logger.Warn("template library: skipping invalid built-in template",
				zap.String("id", t.ID),
				zap.Error(err),
			)
			continue
		}
		l.Register(t)
	}
	return l
}

// Compile-time assertion that library implements Library.
var _ Library = (*library)(nil)

// errEmptyTemplateID is returned by Validate when a template has an empty ID.
var errEmptyTemplateID = errors.New("template: ID is required")
