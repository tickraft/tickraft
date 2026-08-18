// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package executor

import (
	"errors"
	"sync"
)

// Sentinel errors for registry operations.
var (
	// ErrExecutorAlreadyRegistered is returned when registering a duplicate executor name.
	ErrExecutorAlreadyRegistered = errors.New("executor: executor already registered")
	// ErrExecutorNotFound is returned when no executor is registered for the given name.
	ErrExecutorNotFound = errors.New("executor: executor not found")
)

// Registry manages executor registration and lookup.
// It is safe for concurrent use.
type Registry struct {
	mu        sync.RWMutex
	executors map[string]Executor
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{
		executors: make(map[string]Executor),
	}
}

// Register adds an executor to the registry.
// The key is derived from executor.Name().
// It returns ErrExecutorAlreadyRegistered if an executor with the same name exists.
func (r *Registry) Register(executor Executor) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	name := executor.Name()
	if _, exists := r.executors[name]; exists {
		return ErrExecutorAlreadyRegistered
	}
	r.executors[name] = executor
	return nil
}

// Lookup returns the executor for the given name.
// It returns ErrExecutorNotFound if no executor is registered for the name.
func (r *Registry) Lookup(name string) (Executor, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	e, ok := r.executors[name]
	if !ok {
		return nil, ErrExecutorNotFound
	}
	return e, nil
}

// LookupWithOp returns the executor for the given name and verifies that it
// supports the requested Operation. It returns ErrExecutorNotFound if no
// executor is registered for the name, or ErrCapabilityNotSupported if the
// executor lacks the capability required by the operation.
//
// OpProbe requires CapProbe (exact match). OpExecute requires any write
// capability (CapExec, CapMutate, or CapNotify) — i.e. HasWrite semantics,
// not HasCap semantics, because CapWrite is a union mask.
func (r *Registry) LookupWithOp(name string, op Operation) (Executor, error) {
	e, err := r.Lookup(name)
	if err != nil {
		return nil, err
	}
	if !op.isSupportedBy(e.Capabilities()) {
		return nil, ErrCapabilityNotSupported
	}
	return e, nil
}

// Unregister removes an executor by name.
// It returns ErrExecutorNotFound if no executor is registered for the name.
func (r *Registry) Unregister(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.executors[name]; !ok {
		return ErrExecutorNotFound
	}
	delete(r.executors, name)
	return nil
}

// Names returns the names of all registered executors.
// The result order is not deterministic.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.executors))
	for name := range r.executors {
		names = append(names, name)
	}
	return names
}

// Executors returns all registered executors.
// The result order is not deterministic.
func (r *Registry) Executors() []Executor {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]Executor, 0, len(r.executors))
	for _, e := range r.executors {
		result = append(result, e)
	}
	return result
}

// ByCapability returns all executors whose capabilities include the given
// capability mask. The result order is not deterministic.
func (r *Registry) ByCapability(c Capability) []Executor {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]Executor, 0)
	for _, e := range r.executors {
		if HasCap(e.Capabilities(), c) {
			result = append(result, e)
		}
	}
	return result
}

// NamesByCapability returns the names of executors whose capabilities include
// the given capability mask. The result order is not deterministic.
func (r *Registry) NamesByCapability(c Capability) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]string, 0)
	for name, e := range r.executors {
		if HasCap(e.Capabilities(), c) {
			result = append(result, name)
		}
	}
	return result
}
