// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package telemetry

import (
	"fmt"
	"sync"

	"github.com/tickraft/tickraft/pkg/types"
)

// ProcessorRegistry manages processor registration and lookup.
type ProcessorRegistry struct {
	mu         sync.RWMutex
	processors map[types.AssetType]Processor
}

// NewProcessorRegistry creates an empty processor registry.
func NewProcessorRegistry() *ProcessorRegistry {
	return &ProcessorRegistry{processors: make(map[types.AssetType]Processor)}
}

// Register adds a processor. Returns error if type already registered.
func (r *ProcessorRegistry) Register(processor Processor) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	t := processor.Type()
	if _, exists := r.processors[t]; exists {
		return fmt.Errorf("telemetry: processor for %q already registered", t)
	}
	r.processors[t] = processor
	return nil
}

// Lookup returns the processor for the given asset type.
func (r *ProcessorRegistry) Lookup(assetType types.AssetType) (Processor, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.processors[assetType]
	if !ok {
		return nil, fmt.Errorf("telemetry: processor for %q not found", assetType)
	}
	return p, nil
}
