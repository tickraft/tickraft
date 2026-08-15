// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/tickraft/tickraft/pkg/config"
	"github.com/tickraft/tickraft/pkg/db"
	"github.com/tickraft/tickraft/pkg/prism/alert"
	"github.com/tickraft/tickraft/pkg/prism/rule"
	"go.uber.org/zap"
)

// RunMigrate opens the database from dbCfg, runs AutoMigrate, and logs the
// result. displayDSN is used only for the success log line.
func RunMigrate(ctx context.Context, dbCfg db.Config, displayDSN string) error {
	dbc, err := db.Open(ctx, dbCfg)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() {
		if sqlDB, e := dbc.DB(); e == nil {
			_ = sqlDB.Close() // best-effort close, error not actionable
		}
	}()

	if err = db.AutoMigrate(ctx, dbc); err != nil {
		return fmt.Errorf("auto migrate: %w", err)
	}

	if err = rule.NewStore(dbc, rule.NewCompiler()).Migrate(ctx); err != nil {
		return fmt.Errorf("migrate rule table: %w", err)
	}

	if err = alert.Migrate(ctx, dbc); err != nil {
		return fmt.Errorf("migrate alert tables: %w", err)
	}

	zap.L().Info("database migration completed successfully",
		zap.String("dsn", db.Redact(displayDSN)),
	)
	return nil
}

// RunMigrateFromDSN parses the DSN string, then opens the database and runs
// AutoMigrate. It is intended for the --dsn CLI flag path.
func RunMigrateFromDSN(ctx context.Context, dsn string) error {
	if dsn == "" {
		return errors.New("db: --dsn is set but empty")
	}

	cfg, err := db.Parse(dsn)
	if err != nil {
		return fmt.Errorf("parse database dsn: %w", err)
	}
	return RunMigrate(ctx, cfg, dsn)
}

// RunMigrateFromConfig loads the config file, resolves the database
// configuration, then opens the database and runs AutoMigrate. It is intended
// for the --config CLI flag path.
func RunMigrateFromConfig(ctx context.Context, configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	dbCfg, err := cfg.Database.ResolveDBConfig()
	if err != nil {
		return fmt.Errorf("resolve database config: %w", err)
	}
	return RunMigrate(ctx, dbCfg, cfg.Database.DSN)
}
