// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package service

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/executor/http"
	"github.com/tickraft/tickraft/pkg/executor/webhook"
	"github.com/tickraft/tickraft/pkg/i18n"
	"github.com/tickraft/tickraft/pkg/prism"
	"github.com/tickraft/tickraft/pkg/prism/alert"
	"github.com/tickraft/tickraft/pkg/prism/alert/template"
	"github.com/tickraft/tickraft/pkg/prism/channel"
	"github.com/tickraft/tickraft/pkg/prism/remediation"
	"github.com/tickraft/tickraft/pkg/types"
)

// startPrismEngine creates and starts the prism engine via NewFromConfig,
// which handles all store creation, migration, channel loading from DB, and
// rule engine registration in one call. The fully wired Engine is stored on
// the runtime so that startAPIServer can access its stores via accessor methods.
//
// The callers may override the governance guard chain, rule
// configuration, or OnAlert callback by constructing a prism.Config
// directly and calling prism.NewFromConfig instead of using this helper.
func startPrismEngine(
	ctx context.Context,
	rt *runtime,
	notificationPoolSize int,
) (stopFunc, error) {
	engine, err := prism.NewFromConfig(ctx, prism.Config{
		DB:                   rt.dbc,
		Bus:                  rt.eventBus(),
		Logger:               rt.logger,
		NotificationPoolSize: notificationPoolSize,
		Guards:               prism.DefaultGuards(rt.logger),
		ChannelBuild:         channelBuildOptions(rt),
		ChannelEncryptionKey: channelEncryptionKey(),
		RuleConfig: alert.Config{
			Logger:       rt.logger,
			AssetStore:   rt.assetStore,
			EvalInterval: rt.cfg.Prism.EvalInterval.Duration(),
		},
		AssetStore: rt.assetStore,
		// Remediation actions reuse the built-in executors: webhook and
		// http are wrapped as remediation operators, and "local" is
		// registered by default inside the remediation engine. The
		// operator names must match the executor_type values accepted by
		// the remediation rule API.
		RemediationOperators: []remediation.Operator{
			remediation.NewExecutorOperator(string(types.ExecutorWebhook),
				webhook.New(webhook.WithLogger(rt.logger)), rt.logger),
			remediation.NewExecutorOperator(string(types.ExecutorHTTP),
				http.New(http.WithLogger(rt.logger)), rt.logger),
		},
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

// channelBuildOptions assembles the render collaborators injected into
// every built notification channel: the shared i18n registry, the
// locale-aware alert formatter, and the builtin template library.
func channelBuildOptions(rt *runtime) channel.BuildOptions {
	opts := channel.BuildOptions{Logger: rt.logger}
	if rt.i18nRegistry != nil {
		opts.Formatter = i18n.NewDefaultFormatter(rt.i18nRegistry, rt.logger)
		opts.Registry = rt.i18nRegistry
	}
	opts.Library = template.NewBuiltinLibrary(rt.logger)
	return opts
}

// channelEncryptionKeyEnv is the environment variable holding the 32-byte
// AES-256 key (hex-encoded) used to encrypt sensitive channel config
// fields at rest. When unset, encryption is disabled.
const channelEncryptionKeyEnv = "TICKRAFT_CHANNEL_ENCRYPTION_KEY"

// channelEncryptionKey reads the channel config encryption key from the
// environment. The value is a hex string of exactly 32 bytes; an unset,
// empty, or malformed value disables encryption (logged at startup by the
// caller if needed).
func channelEncryptionKey() []byte {
	raw := strings.TrimSpace(os.Getenv(channelEncryptionKeyEnv))
	if raw == "" {
		return nil
	}
	key, err := hex.DecodeString(raw)
	if err != nil || len(key) != 32 {
		return nil
	}
	return key
}
