// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package scheduler

import (
	"testing"
)

// --- ShardManager Tests ---

func TestShardManagerNoSharding(t *testing.T) {
	// total <= 1 means no sharding; all tasks are owned.
	sm := NewShardManager(0, 0)
	for _, id := range []int64{0, 1, 2, 100, 9999} {
		if !sm.Owns(id) {
			t.Errorf("NewShardManager(0,0).Owns(%d) = false, want true (no sharding)", id)
		}
	}

	sm = NewShardManager(1, 0)
	for _, id := range []int64{0, 1, 2, 100, 9999} {
		if !sm.Owns(id) {
			t.Errorf("NewShardManager(1,0).Owns(%d) = false, want true (no sharding)", id)
		}
	}
}

func TestShardManagerNilSafe(t *testing.T) {
	// A nil ShardManager should own all tasks (no sharding).
	var sm *ShardManager
	for _, id := range []int64{0, 1, 2, 100} {
		if !sm.Owns(id) {
			t.Errorf("nil ShardManager.Owns(%d) = false, want true", id)
		}
	}
}

func TestShardManagerWithNilStrategy(t *testing.T) {
	sm := NewShardManagerWithStrategy(nil)
	for _, id := range []int64{0, 1, 2, 100} {
		if !sm.Owns(id) {
			t.Errorf("ShardManager(nil strategy).Owns(%d) = false, want true", id)
		}
	}
}

// --- ModuloShardStrategy Tests ---

func TestModuloShardStrategyNoSharding(t *testing.T) {
	s := NewModuloShardStrategy(0, 0)
	if !s.Owns(42) {
		t.Error("NewModuloShardStrategy(0,0).Owns(42) = false, want true")
	}

	s = NewModuloShardStrategy(1, 0)
	if !s.Owns(42) {
		t.Error("NewModuloShardStrategy(1,0).Owns(42) = false, want true")
	}
}

func TestModuloShardStrategyOwnership(t *testing.T) {
	total := 4
	// Verify that every task ID is owned by exactly one shard.
	for id := range int64(100) {
		ownedCount := 0
		ownerIndex := -1
		for index := range total {
			s := NewModuloShardStrategy(total, index)
			if s.Owns(id) {
				ownedCount++
				ownerIndex = index
			}
		}
		if ownedCount != 1 {
			t.Errorf("task %d owned by %d shards, want exactly 1", id, ownedCount)
		}
		expectedOwner := int(id % int64(total))
		if ownerIndex != expectedOwner {
			t.Errorf("task %d owner = %d, want %d", id, ownerIndex, expectedOwner)
		}
	}
}

func TestModuloShardStrategySpecificIDs(t *testing.T) {
	// total=3: task 0 -> shard 0, task 1 -> shard 1, task 2 -> shard 2,
	// task 3 -> shard 0, task 4 -> shard 1, ...
	tests := []struct {
		total int
		index int
		id    int64
		want  bool
	}{
		{3, 0, 0, true},
		{3, 1, 0, false},
		{3, 2, 0, false},
		{3, 0, 1, false},
		{3, 1, 1, true},
		{3, 0, 3, true},
		{3, 1, 4, true},
		{3, 2, 5, true},
		{5, 0, 10, true},
		{5, 0, 11, false},
	}
	for _, tt := range tests {
		s := NewModuloShardStrategy(tt.total, tt.index)
		got := s.Owns(tt.id)
		if got != tt.want {
			t.Errorf("ModuloShardStrategy(%d,%d).Owns(%d) = %v, want %v",
				tt.total, tt.index, tt.id, got, tt.want)
		}
	}
}

// --- ConsistentHashShardStrategy Tests ---

func TestConsistentHashShardStrategyNoSharding(t *testing.T) {
	s := NewConsistentHashShardStrategy(0, 0)
	if !s.Owns(42) {
		t.Error("NewConsistentHashShardStrategy(0,0).Owns(42) = false, want true")
	}

	s = NewConsistentHashShardStrategy(1, 0)
	if !s.Owns(42) {
		t.Error("NewConsistentHashShardStrategy(1,0).Owns(42) = false, want true")
	}
}

func TestConsistentHashShardStrategyCoverage(t *testing.T) {
	total := 5
	// Verify that every task ID is owned by exactly one shard.
	for id := range int64(200) {
		ownedCount := 0
		for index := range total {
			s := NewConsistentHashShardStrategy(total, index)
			if s.Owns(id) {
				ownedCount++
			}
		}
		if ownedCount != 1 {
			t.Errorf("task %d owned by %d shards, want exactly 1", id, ownedCount)
		}
	}
}

func TestConsistentHashShardStrategyDeterministic(t *testing.T) {
	s := NewConsistentHashShardStrategy(4, 2)
	// The same task ID must always produce the same ownership result.
	first := s.Owns(12345)
	for range 10 {
		if got := s.Owns(12345); got != first {
			t.Errorf("ConsistentHashShardStrategy.Owns(12345) not deterministic: got %v, want %v", got, first)
		}
	}
}

func TestConsistentHashShardStrategyDistribution(t *testing.T) {
	// With enough task IDs, each shard should own a non-trivial fraction.
	// This is a sanity check, not a strict statistical test.
	total := 4
	counts := make([]int, total)
	for id := range int64(1000) {
		for index := range total {
			s := NewConsistentHashShardStrategy(total, index)
			if s.Owns(id) {
				counts[index]++
				break
			}
		}
	}
	for i, c := range counts {
		// Each shard should own at least 10% of tasks (expected ~25%).
		if c < 100 {
			t.Errorf("shard %d owns %d tasks, expected at least 100", i, c)
		}
	}
}

// --- Compile-time interface assertions ---

var (
	_ ShardStrategy = (*ModuloShardStrategy)(nil)
	_ ShardStrategy = (*ConsistentHashShardStrategy)(nil)
)

// --- Benchmarks ---

// BenchmarkConsistentHashShardStrategyOwns measures the hot-path Owns call.
// With the stateless FNV-1a implementation this is allocation-free.
func BenchmarkConsistentHashShardStrategyOwns(b *testing.B) {
	s := NewConsistentHashShardStrategy(16, 3)
	b.ReportAllocs()
	for i := range b.N {
		_ = s.Owns(int64(i))
	}
}
