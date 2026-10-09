// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package cache

import (
	"context"
	"testing"
	"time"
)

type testUser struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

func TestGetJSON_Found(t *testing.T) {
	c := NewLRU(1024, 5*time.Minute)
	user := testUser{ID: 1, Name: "alice"}
	SetJSON(context.Background(), c, "user:1", user)

	result, ok := GetJSON[testUser](context.Background(), c, "user:1")
	if !ok {
		t.Fatal("expected to find key")
	}
	if result.ID != 1 || result.Name != "alice" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestGetJSON_NotFound(t *testing.T) {
	c := NewLRU(1024, 5*time.Minute)
	_, ok := GetJSON[testUser](context.Background(), c, "nonexistent")
	if ok {
		t.Fatal("expected not found")
	}
}

func TestSetJSONWithTTL(t *testing.T) {
	c := NewLRU(1024, 5*time.Minute)
	user := testUser{ID: 2, Name: "bob"}
	SetJSONWithTTL(context.Background(), c, "user:2", user, 50*time.Millisecond)

	result, ok := GetJSON[testUser](context.Background(), c, "user:2")
	if !ok {
		t.Fatal("expected to find key")
	}
	if result.Name != "bob" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestGetJSON_InvalidJSON(t *testing.T) {
	c := NewLRU(1024, 5*time.Minute)
	c.Set(context.Background(), "bad", []byte("not json at all"))

	_, ok := GetJSON[testUser](context.Background(), c, "bad")
	if ok {
		t.Fatal("expected false for invalid JSON")
	}
}
