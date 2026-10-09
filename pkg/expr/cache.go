// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package expr

import (
	"container/list"
	"reflect"
	"sync"
)

// defaultCacheCapacity is the program count held by a ProgramCache
// constructed without an explicit capacity.
const defaultCacheCapacity = 512

// cacheEntry is the LRU list payload: the cache key plus the compiled
// program it maps to.
type cacheEntry struct {
	key     string
	program *Program
}

// ProgramCache is a bounded LRU cache of compiled programs keyed by
// (env type name, expression text). It replaces the unbounded
// per-rule compile caches the consumer surfaces used to grow, and is
// safe for concurrent use.
//
// Compile failures are intentionally not cached: a failed compile is
// cheap to re-attempt relative to the risk of caching a transient
// error, and entry validation keeps bad expressions out of the system
// in the first place.
type ProgramCache struct {
	compiler *Compiler
	capacity int

	mu      sync.Mutex
	entries map[string]*list.Element
	lru     *list.List // front = most recently used
}

// NewProgramCache creates a ProgramCache with the given capacity.
// A non-positive capacity falls back to defaultCacheCapacity (512).
func NewProgramCache(capacity int) *ProgramCache {
	if capacity <= 0 {
		capacity = defaultCacheCapacity
	}
	return &ProgramCache{
		compiler: NewCompiler(),
		capacity: capacity,
		entries:  make(map[string]*list.Element),
		lru:      list.New(),
	}
}

// cacheKey builds the composite cache key: the env value's type name
// (package-qualified, so identically named envs never collide) plus the
// expression text, separated by a NUL byte that cannot appear in
// expressions.
func cacheKey(env any, expression string) string {
	return reflect.TypeOf(env).String() + "\x00" + expression
}

// entryOf returns the cacheEntry payload of a list element. Entries are
// only ever pushed by Compile with this exact type, so the assertion
// cannot fail; the comma-ok form keeps it panic-free regardless.
func entryOf(el *list.Element) *cacheEntry {
	entry, _ := el.Value.(*cacheEntry)
	return entry
}

// Compile returns the cached program for (env type, expression),
// compiling it on first use. A cache hit moves the entry to the front
// of the LRU list; inserting beyond capacity evicts the least recently
// used entry.
func (c *ProgramCache) Compile(env any, expression string) (*Program, error) {
	if isNilEnv(env) {
		return nil, ErrNilEnv
	}
	key := cacheKey(env, expression)

	c.mu.Lock()
	if el, ok := c.entries[key]; ok {
		c.lru.MoveToFront(el)
		c.mu.Unlock()
		return entryOf(el).program, nil
	}
	c.mu.Unlock()

	program, err := c.compiler.Compile(env, expression)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	// Another goroutine may have compiled the same key concurrently;
	// prefer the published entry so both callers share one program.
	if el, ok := c.entries[key]; ok {
		c.lru.MoveToFront(el)
		return entryOf(el).program, nil
	}
	el := c.lru.PushFront(&cacheEntry{key: key, program: program})
	c.entries[key] = el
	for c.lru.Len() > c.capacity {
		oldest := c.lru.Back()
		if oldest == nil {
			break
		}
		c.lru.Remove(oldest)
		delete(c.entries, entryOf(oldest).key)
	}
	return program, nil
}

// Eval evaluates the expression against env, compiling (and caching)
// the program on first use. It is the single-call entry point for
// surfaces that judge per-event, such as execution-result judgment.
func (c *ProgramCache) Eval(expression string, env any) (bool, error) {
	program, err := c.Compile(env, expression)
	if err != nil {
		return false, err
	}
	return RunBool(program, env)
}

// Len returns the number of cached programs.
func (c *ProgramCache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lru.Len()
}
