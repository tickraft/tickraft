// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package service

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	prismengine "github.com/tickraft/tickraft/pkg/prism"
	"github.com/tickraft/tickraft/pkg/prism/rule"
)

// startPrismEngine creates and starts the prism engine via NewFromConfig,
// which handles all store creation, migration, channel loading from DB, and
// rule engine registration in one call. The fully wired Engine is stored on
// the runtime so that startAPIServer can access its stores via accessor methods.
//
// The callers may override the governance guard chain, rule
// configuration, or OnAlert callback by constructing a prism.Config
// directly and calling prism.NewFromConfig instead of using this helper.
func startPrismEngine(ctx context.Context, rt *runtime,
	notificationPoolSize int,
) (stopFunc, error) {
	engine, err := prismengine.NewFromConfig(ctx, prismengine.Config{
		DB:                   rt.dbc,
		Bus:                  rt.eventBus(),
		Logger:               rt.logger,
		NotificationPoolSize: notificationPoolSize,
		Guards:               prismengine.DefaultGuards(rt.logger),
		RuleConfig: rule.Config{
			Logger:     rt.logger,
			AssetStore: rt.assetStore,
		},
		AssetStore: rt.assetStore,
	})
	if err != nil {
		return nil, fmt.Errorf("start prism: %w", err)
	}

	if err = engine.Start(ctx); err != nil {
		return nil, fmt.Errorf("start prism engine: %w", err)
	}

	rt.prismEngine = engine

	rt.logger.Info("prism engine started",
		zap.Int("channels", len(engine.Channels())),
		zap.Int("rules", len(engine.Rules())),
	)

	return func(ctx context.Context) error {
		return engine.Stop(ctx)
	}, nil
}
