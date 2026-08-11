// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/tickraft/tickraft/pkg/config"
)

// newConfigCmd creates the "config" parent subcommand for configuration management.
func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config [command]",
		Short: "Inspect and validate configuration files",
		Long: `Inspect and validate tickraft configuration files.

Use "config validate" to check a config file for syntax errors, missing required
fields, and invalid values before starting the server.`,
	}

	cmd.AddCommand(newConfigValidateCmd())

	return cmd
}

// newConfigValidateCmd creates the "config validate" subcommand. It loads the
// configuration file referenced by the global --config flag, performs
// environment variable interpolation, applies defaults, and runs all
// validation checks.
func newConfigValidateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Check a config file for syntax and schema errors",
		Long: `Load and validate the configuration file specified by --config.
Reports syntax errors, missing required fields, and invalid values.
Returns a non-zero exit code on failure.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath, err := cmd.Root().PersistentFlags().GetString("config")
			if err != nil {
				return fmt.Errorf("get config flag: %w", err)
			}

			if configPath == "" {
				fmt.Println("config file not specified, nothing to validate")
				return nil
			}

			cfg, err := config.Load(configPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "config validation failed: %v\n", err)
				return err
			}

			if err := cfg.Validate(); err != nil {
				fmt.Fprintf(os.Stderr, "config validation failed: %v\n", err)
				return err
			}

			fmt.Println("config validation passed")
			return nil
		},
	}

	return cmd
}
