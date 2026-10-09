// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// validConfigYAML is a config that passes validation. Secret fields use env
// var interpolation; tests set the referenced env vars.
const validConfigYAML = `
server:
  addr: ":8080"
  enable_cors: true
  enable_access_log: true
database:
  dsn: "/tmp/tickraft.db"
auth:
  jwt_secret: ${TICKRAFT_TEST_JWT_SECRET}
logging:
  level: "info"
  mode: "debug"
`

// invalidConfigYAML is missing the required database.dsn field.
const invalidConfigYAML = `
server:
  addr: ":8080"
database:
  dsn: ""
auth:
  jwt_secret: "secret"
`

// writeTempConfig writes content to a temp file and returns its path.
func writeTempConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	return path
}

// runValidate executes "tickraft config validate" with the given extra args
// and returns the captured stdout, stderr, and error.
func runValidate(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	root := NewRootCmd()
	root.SetArgs(append([]string{"config", "validate"}, args...))

	oldStdout := os.Stdout
	oldStderr := os.Stderr
	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdout pipe: %v", err)
	}
	rErr, wErr, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stderr pipe: %v", err)
	}
	os.Stdout = wOut
	os.Stderr = wErr

	var outBuf, errBuf bytes.Buffer
	doneOut := make(chan struct{})
	doneErr := make(chan struct{})
	go func() { _, _ = io.Copy(&outBuf, rOut); close(doneOut) }()
	go func() { _, _ = io.Copy(&errBuf, rErr); close(doneErr) }()

	execErr := root.Execute()

	_ = wOut.Close()
	_ = wErr.Close()
	os.Stdout = oldStdout
	os.Stderr = oldStderr
	<-doneOut
	<-doneErr

	return outBuf.String(), errBuf.String(), execErr
}

func TestConfigValidate_NoConfigFlag(t *testing.T) {
	stdout, stderr, err := runValidate(t)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !strings.Contains(stdout, "config file not specified, nothing to validate") {
		t.Errorf("stdout = %q, want substring %q", stdout, "config file not specified, nothing to validate")
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
}

func TestConfigValidate_ValidConfig(t *testing.T) {
	t.Setenv("TICKRAFT_TEST_JWT_SECRET", "test-secret-key-with-at-least-32-bytes")
	path := writeTempConfig(t, validConfigYAML)

	stdout, stderr, err := runValidate(t, "--config", path)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !strings.Contains(stdout, "config validation passed") {
		t.Errorf("stdout = %q, want substring %q", stdout, "config validation passed")
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
}

func TestConfigValidate_InvalidConfig(t *testing.T) {
	path := writeTempConfig(t, invalidConfigYAML)

	stdout, stderr, err := runValidate(t, "--config", path)
	if err == nil {
		t.Fatalf("expected error for invalid config, got nil")
	}
	if !strings.Contains(stderr, "config validation failed") {
		t.Errorf("stderr = %q, want substring %q", stderr, "config validation failed")
	}
	if !strings.Contains(stderr, "database.dsn") {
		t.Errorf("stderr = %q, want substring %q", stderr, "database.dsn")
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
}

func TestConfigValidate_MissingFile(t *testing.T) {
	stdout, stderr, err := runValidate(t, "--config", "/nonexistent/path/config.yaml")
	if err == nil {
		t.Fatalf("expected error for missing file, got nil")
	}
	if !strings.Contains(stderr, "config validation failed") {
		t.Errorf("stderr = %q, want substring %q", stderr, "config validation failed")
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
}

func TestConfigValidate_EnvInterpolation(t *testing.T) {
	t.Setenv("TICKRAFT_TEST_JWT_SECRET", "injected-secret-with-at-least-32-bytes")
	path := writeTempConfig(t, validConfigYAML)

	stdout, _, err := runValidate(t, "--config", path)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !strings.Contains(stdout, "config validation passed") {
		t.Errorf("stdout = %q, want substring %q", stdout, "config validation passed")
	}
}

func TestConfigValidate_MissingEnvVar(t *testing.T) {
	os.Unsetenv("TICKRAFT_TEST_JWT_SECRET")
	path := writeTempConfig(t, validConfigYAML)

	_, stderr, err := runValidate(t, "--config", path)
	if err == nil {
		t.Fatalf("expected error for missing env var, got nil")
	}
	if !strings.Contains(stderr, "TICKRAFT_TEST_JWT_SECRET") {
		t.Errorf("stderr = %q, want substring %q", stderr, "TICKRAFT_TEST_JWT_SECRET")
	}
}
