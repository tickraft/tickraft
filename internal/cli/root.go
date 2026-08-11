// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package cli implements the cobra command tree for the tickraft binary.
//
// It aggregates all subcommands (start, migrate, version, config, cert) and
// registers persistent flags shared across subcommands. The standalone
// runtime registers its subcommands directly here; the callers
// builds its own cobra root command when it needs to inject additional
// subcommands.
package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// NewRootCmd creates and returns the root cobra command for the tickraft CLI.
// It aggregates all subcommands (start, migrate, version, config) and
// registers persistent flags shared across subcommands. In the standalone
// runtime the "start" command runs exclusively in standalone mode (single
// process, single HTTP port); subcommand-based startup is an optional
// feature that replaces this command via replaceCommand.
//
// Subcommands are registered directly here. The callers builds
// its own cobra root command if it needs to inject additional subcommands.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use: "tickraft [command]",
		Long: `Tickraft — infrastructure monitoring and task scheduling platform.

Schedules probes and executors to monitor service health, collects metrics via
webhook listeners and active probers, and alerts through configurable channels.
All components run in a single process.`,
		Short: "Infrastructure monitoring and task scheduling platform",
		RunE: func(cmd *cobra.Command, _ []string) error {
			// When --version is set, print version information and exit.
			if showVersion, err := cmd.Flags().GetBool("version"); err != nil {
				return fmt.Errorf("get version flag: %w", err)
			} else if showVersion {
				printVersion()
				return nil
			}

			// No subcommand provided and no --version: show help.
			return cmd.Help()
		},
	}

	root.PersistentFlags().StringP("config", "c", "", "configure file path")
	root.PersistentFlags().String("log-mode", "debug", "logging mode: debug or release")
	root.Flags().BoolP("version", "v", false, "show version information and exit")

	root.AddCommand(newStartCmd())
	root.AddCommand(newMigrateCmd())
	root.AddCommand(newVersionCmd())
	root.AddCommand(newConfigCmd())
	root.AddCommand(newCertCmd())

	root.SetUsageTemplate(`Usage:{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [command] [flags]{{else if .Runnable}}
  {{.UseLine}}{{end}}{{if gt (len .Aliases) 0}}

Aliases:
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

Examples:
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}{{$cmds := .Commands}}{{if eq (len .Groups) 0}}

Available Commands:{{range $cmds}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{else}}{{range $group := .Groups}}

{{.Title}}{{range $cmds}}{{if (and (eq .GroupID $group.ID) (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{if not .AllChildCommandsHaveGroup}}

Additional Commands:{{range $cmds}}{{if (and (eq .GroupID "") (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

Flags:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

Global Flags:
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasHelpSubCommands}}

Additional help topics:{{range .Commands}}{{if .IsAdditionalHelpTopicCommand}}
  {{rpad .CommandPath .CommandPathPadding}} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableSubCommands}}

Use "{{.CommandPath}} [command] --help" for more information about a command.{{end}}
`)

	return root
}
