// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package scheduler

import (
	"strconv"
)

// ShardStrategy determines whether a task is owned by the current scheduler
// instance in a sharded deployment.
// Implementations must be safe for concurrent use.
type ShardStrategy interface {
	// Owns returns true if this node owns the given task ID.
	Owns(taskID int64) bool
}

// ShardManager manages task sharding across multiple scheduler instances.
// It delegates ownership decisions to a ShardStrategy.
// A nil ShardManager or a nil strategy means no sharding: all tasks are owned.
type ShardManager struct {
	strategy ShardStrategy
}

// NewShardManager creates a ShardManager that uses ModuloShardStrategy with
// the given total shard count and this node's 0-based shard index.
// A total of 0 or 1 disables sharding (all tasks are owned).
func NewShardManager(total, index int) *ShardManager {
	return &ShardManager{strategy: NewModuloShardStrategy(total, index)}
}

// NewShardManagerWithStrategy creates a ShardManager backed by the given
// strategy. A nil strategy disables sharding.
func NewShardManagerWithStrategy(strategy ShardStrategy) *ShardManager {
	return &ShardManager{strategy: strategy}
}

// Owns returns true if this node owns the given task ID.
// When no strategy is configured, all tasks are owned.
func (s *ShardManager) Owns(taskID int64) bool {
	if s == nil || s.strategy == nil {
		return true
	}
	return s.strategy.Owns(taskID)
}

// ModuloShardStrategy assigns tasks to shards using simple modulo arithmetic:
// the owning shard is taskID % total.
type ModuloShardStrategy struct {
	total int
	index int
}

// NewModuloShardStrategy creates a ModuloShardStrategy.
// total is the total number of shards; index is this node's 0-based shard
// index. If total <= 1, all tasks are owned by index 0.
func NewModuloShardStrategy(total, index int) *ModuloShardStrategy {
	return &ModuloShardStrategy{total: total, index: index}
}

// Owns returns true if this node owns the given task ID.
func (s *ModuloShardStrategy) Owns(taskID int64) bool {
	if s.total <= 1 {
		return true
	}
	return int(taskID%int64(s.total)) == s.index
}

// ConsistentHashShardStrategy assigns tasks to shards using consistent
// hashing with FNV-1a. This provides better stability when shards are added
// or removed: only the tasks mapped to the changed shard need reassignment.
type ConsistentHashShardStrategy struct {
	total int
	index int
}

// NewConsistentHashShardStrategy creates a ConsistentHashShardStrategy.
// total is the total number of shards; index is this node's 0-based shard
// index. If total <= 1, all tasks are owned by index 0.
func NewConsistentHashShardStrategy(total, index int) *ConsistentHashShardStrategy {
	return &ConsistentHashShardStrategy{total: total, index: index}
}

// Owns returns true if this node owns the given task ID.
// It hashes the task ID with FNV-1a and maps it to a shard via modulo.
func (s *ConsistentHashShardStrategy) Owns(taskID int64) bool {
	if s.total <= 1 {
		return true
	}
	//nolint:gosec // modulo result is bounded by the shard count, always fits int
	return int(fnv1a64(strconv.FormatInt(taskID, 10))%uint64(s.total)) == s.index
}

// FNV-1a 64-bit constants, identical to hash/fnv's offset basis and prime.
const (
	fnv64Offset = 14695981039346656037
	fnv64Prime  = 1099511628211
)

// fnv1a64 computes the 64-bit FNV-1a hash of s without allocating a hasher.
// It produces the same value as hash/fnv.New64a().Write([]byte(s)).Sum64(),
// keeping the hot-path ConsistentHashShardStrategy.Owns allocation-free.
func fnv1a64(s string) uint64 {
	h := uint64(fnv64Offset)
	for i := range len(s) {
		h ^= uint64(s[i])
		h *= fnv64Prime
	}
	return h
}
