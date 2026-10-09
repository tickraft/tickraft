// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package service

import (
	"context"
	"errors"
	"fmt"

	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/auth"
	"github.com/tickraft/tickraft/pkg/config"
	"github.com/tickraft/tickraft/pkg/db"
	"github.com/tickraft/tickraft/pkg/prism/alert"
	"github.com/tickraft/tickraft/pkg/prism/remediation"
	"github.com/tickraft/tickraft/pkg/user"
)

// RunMigrate opens the database from dbCfg, runs the schema migrations owned
// by the domain packages, and logs the result. displayDSN is used only for
// the success log line.
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

	if err = user.Migrate(ctx, dbc); err != nil {
		return fmt.Errorf("migrate user tables: %w", err)
	}

	if err = auth.Migrate(ctx, dbc); err != nil {
		return fmt.Errorf("migrate auth tables: %w", err)
	}

	if err = alert.NewStore(dbc, alert.NewCompiler()).Migrate(ctx); err != nil {
		return fmt.Errorf("migrate rule table: %w", err)
	}

	if err = alert.Migrate(ctx, dbc); err != nil {
		return fmt.Errorf("migrate alert tables: %w", err)
	}

	if err = remediation.NewStore(dbc).Migrate(ctx); err != nil {
		return fmt.Errorf("migrate remediation tables: %w", err)
	}

	zap.L().Info("database migration completed successfully",
		zap.String("dsn", db.Redact(displayDSN)),
	)
	return nil
}

// RunMigrateFromDSN parses the DSN string, then opens the database and runs
// the schema migrations. It is intended for the --dsn CLI flag path.
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
// configuration, then opens the database and runs the schema migrations. It
// is intended for the --config CLI flag path.
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
