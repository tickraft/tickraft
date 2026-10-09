// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package cache

import (
	"context"
	"time"

	"github.com/bytedance/sonic"
	"go.uber.org/zap"
)

// GetJSON retrieves a cached value and unmarshals it into T.
// Returns the zero value of T and false if the key is not found or
// unmarshaling fails.
//
// The context is forwarded to the underlying [Cache.Get] call.
func GetJSON[T any](ctx context.Context, c *LRUCache, key string) (T, bool) {
	data, ok := c.Get(ctx, key)
	if !ok {
		var zero T
		return zero, false
	}
	var result T
	if err := sonic.Unmarshal(data, &result); err != nil {
		var zero T
		return zero, false
	}
	return result, true
}

// SetJSON marshals a value and stores it with the default TTL.
// The context is forwarded to the underlying [Cache.Set] call.
func SetJSON[T any](ctx context.Context, c *LRUCache, key string, value T) {
	data, err := sonic.Marshal(value)
	if err != nil {
		zap.L().Error("cache: failed to marshal value", zap.String("key", key), zap.Error(err))
		return
	}
	c.Set(ctx, key, data)
}

// SetJSONWithTTL marshals a value and stores it with a custom TTL.
// The context is forwarded to the underlying [Cache.SetWithTTL] call.
func SetJSONWithTTL[T any](ctx context.Context, c *LRUCache, key string, value T, ttl time.Duration) {
	data, err := sonic.Marshal(value)
	if err != nil {
		zap.L().Error("cache: failed to marshal value", zap.String("key", key), zap.Error(err))
		return
	}
	c.SetWithTTL(ctx, key, data, ttl)
}
