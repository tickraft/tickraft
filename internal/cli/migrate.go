// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tickraft/tickraft/internal/service"
)

// newMigrateCmd creates the "migrate" subcommand that runs database migrations.
// It inherits the root command's --config persistent flag, allowing the
// database connection to be resolved from a config file. The --dsn flag is
// retained and, when explicitly set, overrides the
// database configuration from the config file. The database driver is derived
// from the DSN scheme (sqlite://, sqlite3://, or a bare path) or from the
// database.driver field when using the direct-fields config path.
func newMigrateCmd() *cobra.Command {
	var dsn string

	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Create or update database tables",
		Long: `Create or update database tables to match the current schema.

The database connection can be configured in two ways:
  - --dsn: a DSN string (sqlite://, sqlite3://, or a bare path). When set,
    this takes precedence over the config file.
  - --config: a YAML config file. The database section supports both the DSN
    path (database.dsn) and the direct-fields path (database.driver,
    database.address, database.params). When database.dsn is non-empty it
    takes precedence over the direct fields.

At least one of --dsn or --config must be provided.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMigrate(cmd, dsn)
		},
	}

	cmd.Flags().StringVar(&dsn, "dsn", "", "database data source name (sqlite://, sqlite3://, or bare path); overrides the config file database section when set")

	return cmd
}

// runMigrate resolves the database configuration via flags and delegates to
// the service layer. The --dsn flag takes precedence over --config; when
// neither is provided, an error is returned.
func runMigrate(cmd *cobra.Command, dsn string) error {
	ctx := context.Background()

	configPath, err := cmd.Flags().GetString("config")
	if err != nil {
		return fmt.Errorf("get config flag: %w", err)
	}

	// When --dsn is explicitly set, it takes precedence over the config file.
	if cmd.Flags().Changed("dsn") {
		return service.RunMigrateFromDSN(ctx, dsn)
	}

	// --dsn not set; fall back to the config file.
	if configPath != "" {
		return service.RunMigrateFromConfig(ctx, configPath)
	}

	return errors.New("db: database configuration is required (use --dsn or --config to provide it)")
}
