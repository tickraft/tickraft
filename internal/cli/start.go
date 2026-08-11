// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/tickraft/tickraft/internal/service"
	"github.com/tickraft/tickraft/pkg/config"
)

// newStartCmd creates the "start" command. The standalone runtime only
// supports standalone mode: a single process that starts the API server,
// worker engines, prism, and operator together on a single HTTP port.
//
// The start command is a parent that runs standalone mode when invoked
// without a subcommand, and it also registers an explicit "standalone"
// subcommand as an equivalent entry point. Both `tickraft start` and
// `tickraft start standalone` are perfectly equivalent. Distributed
// subcommand-based startup (server/worker/prism/operator) is an
// optional feature and is not registered here.
func newStartCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start tickraft service",
		Long: `Start the tickraft service.

All components (API server, worker, collector, and prism) run in a single process
on a single HTTP port, so no extra services or deployment modes are required.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			cfg, err := loadConfig(cmd)
			if err != nil {
				return err
			}

			return service.Start(ctx, cfg)
		},
	}

	cmd.Flags().String("addr", "", "override listen address")
	cmd.Flags().String("dsn", "", "override database DSN")

	return cmd
}

// loadConfig resolves the configuration from the global --config flag, with
// the --log-mode persistent flag overriding the config file's logging.mode.
// When no config file is provided, a sensible default configuration is used.
func loadConfig(cmd *cobra.Command) (*config.Config, error) {
	configPath, err := cmd.Flags().GetString("config")
	if err != nil {
		return nil, fmt.Errorf("get config flag: %w", err)
	}

	var cfg *config.Config
	if configPath != "" {
		if cfg, err = config.Load(configPath); err != nil {
			return nil, fmt.Errorf("load config: %w", err)
		}

		if err = cfg.Validate(); err != nil {
			return nil, fmt.Errorf("validate config: %w", err)
		}
	} else {
		cfg = defaultConfig()
	}

	// The --log-mode persistent flag overrides the config file value.
	if cmd.Flags().Changed("log-mode") {
		if cfg.Logger.Mode, err = cmd.Flags().GetString("log-mode"); err != nil {
			return nil, fmt.Errorf("get logging mode flag: %w", err)
		}
	}

	// The --addr flag overrides the config file's server.addr.
	if cmd.Flags().Changed("addr") {
		if cfg.Server.Addr, err = cmd.Flags().GetString("addr"); err != nil {
			return nil, fmt.Errorf("get addr flag: %w", err)
		}
	}

	// The --dsn flag overrides the config file's
	// database.dsn.
	if cmd.Flags().Changed("dsn") {
		if cfg.Database.DSN, err = cmd.Flags().GetString("dsn"); err != nil {
			return nil, fmt.Errorf("get database DSN flag: %w", err)
		}
	}

	return cfg, nil
}

// defaultConfig builds a configuration suitable for local development when
// no config file is provided. All services have sensible defaults so the
// system can start with zero configuration. The JWT secret is resolved from
// the TICKRAFT_JWT_SECRET environment variable; when unset a random secret
// is generated (single-process deployments restart with a new secret each
// time, requiring re-login — for persistent sessions set the env var).
func defaultConfig() *config.Config {
	cfg := &config.Config{}
	cfg.SetDefaults()
	cfg.Database.DSN = "sqlite://tickraft.db"
	cfg.Auth.JWTSecret = os.Getenv("TICKRAFT_JWT_SECRET")
	if cfg.Auth.JWTSecret == "" {
		cfg.Auth.JWTSecret = generateRandomSecret()
	}
	return cfg
}

// generateRandomSecret produces a 32-byte hex-encoded random string suitable
// for JWT signing. It panics if the system CSPRNG fails.
func generateRandomSecret() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("generate random jwt secret: %v", err))
	}
	return hex.EncodeToString(b)
}
