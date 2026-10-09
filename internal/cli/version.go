// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package cli

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

// Build metadata injected via -ldflags at link time.
var (
	// version holds the semantic version of the build.
	version = "dev"
	// gitCommit holds the git commit hash of the build.
	gitCommit = "none"
	// buildTime holds the timestamp when the binary was built.
	buildTime = "unknown"
	// edition identifies the product form. It is injected via -ldflags
	// (main.edition). When not injected it defaults to "community", matching
	// the standalone runtime artifact defined in the deployment-delivery
	// design document.
	edition = "community"
)

// printVersion writes the version information to stdout in the multi-line
// format defined by the deployment-delivery design document (section 1.5).
// The first line is always "tickraft version <version>" so that
// `tickraft --version | awk '/^tickraft version/{print $3}'` extracts the
// version number reliably.
func printVersion() {
	fmt.Printf("tickraft version %s\n", version)
	fmt.Printf("edition: %s\n", edition)
	fmt.Printf("commit: %s\n", gitCommit)
	fmt.Printf("built: %s\n", buildTime)
	fmt.Printf("go: %s\n", runtime.Version())
	fmt.Printf("os/arch: %s/%s\n", runtime.GOOS, runtime.GOARCH)
}

// newVersionCmd creates the "version" subcommand that prints build metadata.
func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version, build, and edition details",
		Long: `Print version, edition, git commit, build time, Go runtime, and OS/arch
information. Useful for bug reports and verifying which binary is installed.`,
		Run: func(cmd *cobra.Command, _ []string) {
			printVersion()
		},
	}
}
