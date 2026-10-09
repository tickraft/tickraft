// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// migrateConfigYAML is a sqlite-backed config used by migrate tests. The %s
// placeholder is replaced with the temp DB path each test wants to use.
// A bare file path (no scheme) is treated as SQLite3 by the storage layer.
const migrateConfigYAML = `
database:
  dsn: "%s"
auth:
  jwt_secret: "test-secret-key-with-at-least-32-bytes-long-enough"
`

// writeMigrateConfig writes migrateConfigYAML to a temp file with the given DB
// path substituted in, and returns the config file path.
func writeMigrateConfig(t *testing.T, dbPath string) string {
	t.Helper()
	return writeTempConfig(t, fmt.Sprintf(migrateConfigYAML, dbPath))
}

// runMigrateCmd executes "tickraft migrate" with the given extra args and
// returns the execution error.
func runMigrateCmd(t *testing.T, args ...string) error {
	t.Helper()
	root := NewRootCmd()
	root.SetArgs(append([]string{"migrate"}, args...))
	return root.Execute()
}

// TestMigrate_UsesConfigDB verifies that when --config is provided, the
// database connection is taken from the config file's dsn field (a bare path
// treated as SQLite3), not from the flag defaults.
func TestMigrate_UsesConfigDB(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "from_config.db")
	cfgPath := writeMigrateConfig(t, dbPath)

	if err := runMigrateCmd(t, "--config", cfgPath); err != nil {
		t.Fatalf("migrate failed: %v", err)
	}

	if _, err := os.Stat(dbPath); err != nil {
		t.Errorf("expected DB file created from config at %s: %v", dbPath, err)
	}
}

// TestMigrate_FlagDSNOverridesConfig verifies that an explicitly set --dsn
// flag overrides the DSN from the config file.
func TestMigrate_FlagDSNOverridesConfig(t *testing.T) {
	dir := t.TempDir()
	configDBPath := filepath.Join(dir, "from_config.db")
	flagDBPath := filepath.Join(dir, "from_flag.db")
	cfgPath := writeMigrateConfig(t, configDBPath)

	if err := runMigrateCmd(t, "--config", cfgPath, "--dsn", flagDBPath); err != nil {
		t.Fatalf("migrate failed: %v", err)
	}

	if _, err := os.Stat(flagDBPath); err != nil {
		t.Errorf("expected DB file created from flag override at %s: %v", flagDBPath, err)
	}
	if _, err := os.Stat(configDBPath); err == nil {
		t.Errorf("config DB file should NOT have been created at %s when --dsn is set", configDBPath)
	}
}

// TestMigrate_NoConfigNoDSNReturnsError verifies that without --config and
// without --dsn, the migrate command returns an error because the DSN is
// required.
func TestMigrate_NoConfigNoDSNReturnsError(t *testing.T) {
	if err := runMigrateCmd(t); err == nil {
		t.Fatal("expected error when neither --config nor --dsn is provided, got nil")
	}
}

// TestMigrate_NoConfigExplicitDSN verifies that without --config, the migrate
// command uses an explicitly provided --dsn flag value.
func TestMigrate_NoConfigExplicitDSN(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "from_flag.db")

	if err := runMigrateCmd(t, "--dsn", dbPath); err != nil {
		t.Fatalf("migrate failed: %v", err)
	}

	if _, err := os.Stat(dbPath); err != nil {
		t.Errorf("expected DB file created from --dsn at %s: %v", dbPath, err)
	}
}
