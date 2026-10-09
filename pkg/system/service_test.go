// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package system

import (
	"context"
	"errors"
	"testing"

	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/db"
	"github.com/tickraft/tickraft/pkg/errdefs"
)

func newTestService(t *testing.T) *SystemService {
	t.Helper()
	gdb, err := db.Open(context.Background(), db.Config{Driver: "sqlite3", Addr: ":memory:"})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	svc := NewSystemService(gdb, zap.NewNop(), nil, nil, nil)
	if err := svc.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return svc
}

// TestMigrateSeedsNetworkEnvironmentInternet pins the seeded default:
// full interactive rendering, so the column addition is behavior-neutral
// for existing deployments (the ALTER backfills the same default).
func TestMigrateSeedsNetworkEnvironmentInternet(t *testing.T) {
	svc := newTestService(t)

	cfg, err := svc.GetConfig(context.Background())
	if err != nil {
		t.Fatalf("get config: %v", err)
	}
	if cfg.NetworkEnvironment != NetworkEnvironmentInternet {
		t.Fatalf("seeded network_environment = %q, want %q", cfg.NetworkEnvironment, NetworkEnvironmentInternet)
	}
	if cfg.PlainNotificationOnly() {
		t.Error("internet environment must not degrade to plain notifications")
	}
}

func TestUpdateConfigNetworkEnvironment(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	// Invalid values are rejected as bad requests.
	if _, err := svc.UpdateConfig(ctx, &Config{NetworkEnvironment: "dmz"}); !errors.Is(
		err, errdefs.ErrInvalidArgument) {
		t.Fatalf("invalid environment err = %v, want ErrInvalidArgument", err)
	}

	// An omitted value (older clients PUT the whole Config back) reads as
	// internet instead of persisting an empty string.
	updated, err := svc.UpdateConfig(ctx, &Config{})
	if err != nil {
		t.Fatalf("update with empty environment: %v", err)
	}
	if updated.NetworkEnvironment != NetworkEnvironmentInternet {
		t.Errorf("empty environment persisted as %q, want internet", updated.NetworkEnvironment)
	}

	// Isolated persists and flips the plain-notification policy.
	updated, err = svc.UpdateConfig(ctx, &Config{NetworkEnvironment: NetworkEnvironmentIsolated})
	if err != nil {
		t.Fatalf("update to isolated: %v", err)
	}
	if !updated.PlainNotificationOnly() {
		t.Error("isolated environment must degrade to plain notifications")
	}
	if env := ReadNetworkEnvironment(ctx, svc.dbc); env != NetworkEnvironmentIsolated {
		t.Errorf("ReadNetworkEnvironment = %q, want isolated", env)
	}
}

// configNotifierFunc adapts a closure to ConfigNotifier for tests.
type configNotifierFunc func(ctx context.Context)

func (f configNotifierFunc) ConfigUpdated(ctx context.Context) { f(ctx) }

func TestUpdateConfigNotifiesAfterPersist(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	notified := 0
	svc.WithConfigNotifier(configNotifierFunc(func(inner context.Context) {
		notified++
		// The callback sees the persisted state, not the request.
		if env := ReadNetworkEnvironment(inner, svc.dbc); env != NetworkEnvironmentIsolated {
			t.Errorf("notifier read network_environment = %q, want isolated", env)
		}
	}))

	if _, err := svc.UpdateConfig(ctx, &Config{NetworkEnvironment: NetworkEnvironmentIsolated}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if notified != 1 {
		t.Errorf("notifier calls = %d, want 1", notified)
	}

	// A rejected update must not notify.
	if _, err := svc.UpdateConfig(ctx, &Config{NetworkEnvironment: "dmz"}); err == nil {
		t.Fatal("invalid update unexpectedly succeeded")
	}
	if notified != 1 {
		t.Errorf("notifier calls after rejected update = %d, want 1", notified)
	}
}

// TestReadNetworkEnvironmentBestEffort verifies the read degrades to ""
// (read as internet) when the table does not exist yet — prism starts
// before the API server migrates the system domain.
func TestReadNetworkEnvironmentBestEffort(t *testing.T) {
	gdb, err := db.Open(context.Background(), db.Config{Driver: "sqlite3", Addr: ":memory:"})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}

	if env := ReadNetworkEnvironment(context.Background(), gdb); env != "" {
		t.Errorf("missing table environment = %q, want empty", env)
	}
}
