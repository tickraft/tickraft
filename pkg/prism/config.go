// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package prism

import (
	"context"
	"fmt"
	"time"

	"github.com/tickraft/tickraft/pkg/asset"
	"github.com/tickraft/tickraft/pkg/event"
	"github.com/tickraft/tickraft/pkg/prism/alert"
	"github.com/tickraft/tickraft/pkg/prism/channel"
	"github.com/tickraft/tickraft/pkg/prism/governance"
	"github.com/tickraft/tickraft/pkg/prism/remediation"
	"github.com/tickraft/tickraft/pkg/prism/rule"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// Config is the unified configuration for the prism engine and all its
// sub-components. It is the single entry point for the orchestration that
// was previously scattered across internal/service/prism.go.
type Config struct {
	// DB is the database connection used for all prism stores.
	DB *gorm.DB
	// Bus is the event bus the engine subscribes to.
	Bus event.Bus
	// Logger is the structured logger.
	Logger *zap.Logger
	// NotificationPoolSize is the worker pool size for sending notifications.
	// A non-positive value defaults to 8.
	NotificationPoolSize int
	// Guards is the governance guard chain (SPI injection).
	Guards []governance.Guard
	// PostGuardHook is invoked after guards pass, before rule evaluation.
	PostGuardHook PostGuardHook
	// DeadLetterHandler receives notifications that could not be dispatched
	// because the worker pool rejected them. When nil, rejected notifications
	// are logged and dropped.
	DeadLetterHandler DeadLetterHandler
	// OnAlert is the callback invoked when an alert matches rules.
	// When nil, NewFromConfig wires a default callback that persists
	// records to the alert RecordStore.
	OnAlert OnAlertFunc
	// RuleConfig configures the rule matching engine.
	// When RuleConfig.Store is nil, no rule engine is registered.
	RuleConfig rule.Config
	// AssetStore is used by the rule MetricMatcher for asset enrichment.
	AssetStore asset.Store
	// RemediationOperators registers additional remediation action
	// operators (beyond the default LocalOperator) with the remediation
	// engine started by NewFromConfig. Each operator's Name must match a
	// Rule.ExecutorType value accepted by the remediation rule API.
	RemediationOperators []remediation.Operator
}

// NewFromConfig creates a fully wired prism Engine from the given Config.
// It creates and migrates all stores (rule, alert record, channel,
// remediation), constructs the dispatch Engine, loads channels from the
// ChannelConfig, registers the rule engine, and returns the Engine with
// all stores accessible via accessor methods.
//
// The returned Engine is NOT started; the caller must call Start(ctx)
// to begin event bus subscription.
func NewFromConfig(ctx context.Context, cfg Config) (*Engine, error) {
	if cfg.DB == nil {
		return nil, fmt.Errorf("prism: config DB is required")
	}
	if cfg.Bus == nil {
		return nil, fmt.Errorf("prism: config Bus is required")
	}

	logger := cfg.Logger
	if logger == nil {
		logger = zap.NewNop()
	}

	// Create and migrate stores.
	ruleStore := rule.NewStore(cfg.DB, rule.NewCompiler())
	if err := ruleStore.Migrate(ctx); err != nil {
		return nil, fmt.Errorf("prism: migrate rule store: %w", err)
	}

	recordStore := alert.NewRecordStore(cfg.DB)
	if err := alert.Migrate(ctx, cfg.DB); err != nil {
		return nil, fmt.Errorf("prism: migrate alert store: %w", err)
	}

	channelStore := channel.NewStore(cfg.DB)
	if err := channelStore.Migrate(ctx); err != nil {
		return nil, fmt.Errorf("prism: migrate channel store: %w", err)
	}

	remediationStore := remediation.NewStore(cfg.DB)
	if err := remediationStore.Migrate(ctx); err != nil {
		return nil, fmt.Errorf("prism: migrate remediation store: %w", err)
	}

	// OnAlert callback: use caller-provided or default to record persistence.
	onAlert := cfg.OnAlert
	if onAlert == nil {
		onAlert = func(ctx context.Context, evt alert.Event) {
			if err := alert.RecordAlert(ctx, recordStore, evt); err != nil {
				logger.Warn("persist alert record",
					zap.String("type", string(evt.Type)),
					zap.Int64("asset_id", evt.AssetID),
					zap.Error(err),
				)
			}
		}
	}

	// Construct the dispatch engine.
	engineOpts := []Option{
		WithEventBus(cfg.Bus),
		WithLogger(logger),
		WithNotificationPoolSize(cfg.NotificationPoolSize),
		WithOnAlert(onAlert),
	}
	if len(cfg.Guards) > 0 {
		engineOpts = append(engineOpts, WithGuards(cfg.Guards...))
	}
	if cfg.PostGuardHook != nil {
		engineOpts = append(engineOpts, WithPostGuardHook(cfg.PostGuardHook))
	}
	if cfg.DeadLetterHandler != nil {
		engineOpts = append(engineOpts, WithDeadLetterHandler(cfg.DeadLetterHandler))
	}

	engine, err := New(engineOpts...)
	if err != nil {
		return nil, fmt.Errorf("prism: create engine: %w", err)
	}

	// Load enabled channels from the database into the dispatch engine.
	enabledRecords, err := channelStore.ListEnabled(ctx)
	if err != nil {
		return nil, fmt.Errorf("prism: list enabled channels: %w", err)
	}
	channels, err := BuildChannelsFromRecords(enabledRecords)
	if err != nil {
		logger.Warn("prism: some channels failed to build", zap.Error(err))
	}
	for _, ch := range channels {
		engine.AddChannel(ch)
	}

	// Register the rule engine when enabled.
	ruleCfg := cfg.RuleConfig
	if ruleCfg.Logger == nil {
		ruleCfg.Logger = logger
	}
	if ruleCfg.Store == nil {
		ruleCfg.Store = ruleStore
	}
	if cfg.AssetStore != nil {
		ruleCfg.AssetStore = cfg.AssetStore
	}

	ruleEng, err := rule.Register(ctx, engine, ruleCfg)
	if err != nil {
		return nil, fmt.Errorf("prism: register rule engine: %w", err)
	}

	// Wire orchestration fields onto the Engine.
	engine.ruleStore = ruleStore
	engine.recordStore = recordStore
	engine.channelStore = channelStore
	engine.remediationStore = remediationStore
	engine.ruleEngine = ruleEng

	// Create the remediation engine. It subscribes to the same telemetry
	// and asset events as the alert pipeline and dispatches matching
	// remediation rules to their operators, persisting each run to the
	// remediation record store. Started and stopped with the Engine.
	remediationMgr, err := remediation.New(
		remediation.WithEventBus(cfg.Bus),
		remediation.WithStore(remediationStore),
		remediation.WithRecordStore(remediationStore),
		remediation.WithLogger(logger),
		remediation.WithOperators(cfg.RemediationOperators...),
	)
	if err != nil {
		return nil, fmt.Errorf("prism: create remediation engine: %w", err)
	}
	engine.remediationMgr = remediationMgr
	if ruleEng != nil {
		engine.ruleEngineStopFn = func(stopCtx context.Context) error {
			return ruleEng.Stop(stopCtx)
		}
	}

	return engine, nil
}

// DefaultGuards returns the baseline governance guard chain for the
// standalone open-source deployment: a single Dedup guard that suppresses
// exact-duplicate alerts within the given window. The callers
// replaces this with the full governance chain (silence → aggregator →
// suppressor → storm) via Config.Guards.
func DefaultGuards(logger *zap.Logger) []governance.Guard {
	return []governance.Guard{
		governance.NewDedup(60*time.Second, logger),
	}
}
