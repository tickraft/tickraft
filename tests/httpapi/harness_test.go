// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package httpapi hosts full-stack HTTP integration tests: a real API server
// (JWT + RBAC middleware included) backed by sqlite :memory: stores, driven
// over a real listener with net/http. The tests verify the API contract the
// frontend relies on: envelope shape, snake_case field names, pagination
// page/page_size handling, auth token lifecycle, and per-domain CRUD flows.
package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"testing"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/tickraft/tickraft/internal/api/router"
	prismsvc "github.com/tickraft/tickraft/internal/api/service/prism"
	"github.com/tickraft/tickraft/internal/api/service/scheduler"
	systemsvc "github.com/tickraft/tickraft/internal/api/service/system"
	telemetrysvc "github.com/tickraft/tickraft/internal/api/service/telemetry"
	cequota "github.com/tickraft/tickraft/internal/quota"
	"github.com/tickraft/tickraft/pkg/api"
	assethandler "github.com/tickraft/tickraft/pkg/api/handler/asset"
	"github.com/tickraft/tickraft/pkg/api/handler/healthz"
	"github.com/tickraft/tickraft/pkg/api/handler/readyz"
	telemetryhandler "github.com/tickraft/tickraft/pkg/api/handler/telemetry"
	"github.com/tickraft/tickraft/pkg/asset"
	"github.com/tickraft/tickraft/pkg/auth"
	"github.com/tickraft/tickraft/pkg/auth/jwt"
	"github.com/tickraft/tickraft/pkg/auth/password"
	"github.com/tickraft/tickraft/pkg/cache"
	"github.com/tickraft/tickraft/pkg/db"
	"github.com/tickraft/tickraft/pkg/event"
	"github.com/tickraft/tickraft/pkg/executor"
	httpprober "github.com/tickraft/tickraft/pkg/executor/http"
	"github.com/tickraft/tickraft/pkg/executor/icmp"
	"github.com/tickraft/tickraft/pkg/executor/local"
	"github.com/tickraft/tickraft/pkg/executor/tcp"
	"github.com/tickraft/tickraft/pkg/executor/webhook"
	"github.com/tickraft/tickraft/pkg/prism"
	"github.com/tickraft/tickraft/pkg/task"
	"github.com/tickraft/tickraft/pkg/telemetry"
	telemetryhttp "github.com/tickraft/tickraft/pkg/telemetry/http"
	"github.com/tickraft/tickraft/pkg/user"
)

const (
	adminUsername    = "admin"
	adminPassword    = "Admin-Password-123"
	viewerUsername   = "viewer_user"
	viewerPassword   = "Viewer-Password-123"
	developerName    = "developer_user"
	developerPasswd  = "Developer-Password-123"
	testJWTSecret    = "httpapi-test-secret-0123456789abcdef" // >= 32 bytes
	maintenanceAdmin = 2 * time.Second
)

// harness bundles the running server with the underlying stores so tests can
// seed state directly when the API surface cannot (e.g. alert records).
type harness struct {
	t           *testing.T
	baseURL     string
	client      *http.Client
	dbc         *gorm.DB
	authz       *auth.Service
	jwtMgr      *jwt.JWT
	prismEngine *prism.Engine
	schedEngine *task.Service
	execRunner  executor.Runner
	assetStore  asset.Store
}

var h *harness

func TestMain(m *testing.M) {
	code := m.Run()
	if h != nil {
		h.shutdown()
	}
	os.Exit(code)
}

// newHarness boots one shared server for the whole package. Tests run
// sequentially, so shared state is acceptable; each test logs in with its
// own credentials and creates its own resources.
func newHarness(t *testing.T) *harness {
	if h != nil {
		// Refresh the cached test handle so Fatalf calls land on the
		// currently running test instead of the one that booted the server.
		h.t = t
		return h
	}
	t.Helper()
	ctx := context.Background()
	logger := zap.NewNop()

	// Register the CE default quota provider so quota enforcement (e.g. the
	// remediation rule ceiling) is active, matching production wiring.
	cequota.Register()

	dbc, err := db.Open(ctx, db.Config{Driver: "sqlite3", Addr: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(ctx, dbc); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	if _, err := db.EnsureAdminUser(ctx, dbc, adminUsername, adminPassword); err != nil {
		t.Fatalf("seed admin: %v", err)
	}

	lru := cache.NewLRU(1024, 5*time.Minute)
	userStore := user.NewStore(dbc, lru)
	seedUser(t, ctx, userStore, viewerUsername, viewerPassword, "viewer@example.com", 0)
	seedUser(t, ctx, userStore, developerName, developerPasswd, "developer@example.com", 1)

	// Auth stack: blacklist + JWT with a fixed secret.
	blacklistStore := auth.NewBlacklistStore(dbc, lru)
	blacklistChecker := func(jti string) (bool, error) {
		return blacklistStore.Exists(context.Background(), jti)
	}
	jwtMgr, err := jwt.New(jwt.Config{Secret: testJWTSecret, Issuer: "tickraft"}, blacklistChecker)
	if err != nil {
		t.Fatalf("create jwt: %v", err)
	}
	apiKeyStore := user.NewAPIKeyStore(dbc, lru)
	authz := auth.NewService(jwtMgr, userStore, apiKeyStore, blacklistStore)

	// Asset store.
	assetStore := asset.NewStore(dbc)
	if err := assetStore.Migrate(ctx); err != nil {
		t.Fatalf("migrate assets: %v", err)
	}

	// Scheduler + executor runner on one shared event bus, mirroring the
	// production worker wiring: the scheduler publishes ExecutionTriggered
	// events that the runner consumes, and the runner persists real
	// execution outcomes through the record store adapter.
	if err := task.Migrate(ctx, dbc); err != nil {
		t.Fatalf("migrate tasks: %v", err)
	}
	taskStore := task.NewStore(dbc)
	execStore := task.NewExecutionStore(dbc)
	reg := executor.NewRegistry()
	for _, e := range []executor.Executor{
		local.New(local.WithLogger(logger)),
		webhook.New(webhook.WithLogger(logger)),
		icmp.New(5 * time.Second),
		tcp.New(5 * time.Second),
		httpprober.New(10 * time.Second),
	} {
		if err := reg.Register(e); err != nil {
			t.Fatalf("register executor %q: %v", e.Name(), err)
		}
	}
	workerBus := event.NewBus()
	execRunner, err := executor.New(
		executor.WithExecutorRegistry(reg),
		executor.WithWorkerPoolSize(4),
		executor.WithEventBus(workerBus),
		executor.WithLogger(logger),
		executor.WithRecordStore(task.NewExecutionRecordStore(execStore)),
	)
	if err != nil {
		t.Fatalf("create executor runner: %v", err)
	}
	if err := execRunner.Start(ctx); err != nil {
		t.Fatalf("start executor runner: %v", err)
	}
	execRunner.SubscribeEvents(ctx)
	schedEngine, err := task.NewService(
		task.WithEventBus(workerBus), task.WithStore(taskStore), task.WithLogger(logger))
	if err != nil {
		t.Fatalf("create scheduler engine: %v", err)
	}
	schedEngine.SubscribeEvents(ctx)

	// Prism engine (creates and migrates its rule/record/channel/remediation
	// stores itself). Start() is skipped: the CRUD surface under test does not
	// require the event subscriptions.
	bus := event.NewBus()
	prismEngine, err := prism.NewFromConfig(ctx, prism.Config{
		DB:     dbc,
		Bus:    bus,
		Logger: logger,
	})
	if err != nil {
		t.Fatalf("create prism engine: %v", err)
	}

	// Telemetry stores + builtin templates.
	if err := dbc.AutoMigrate(
		&telemetry.CollectionConfig{}, &telemetry.StatusHistory{},
		&telemetry.CollectMetric{}, &telemetry.CollectLog{}, &telemetry.Template{},
	); err != nil {
		t.Fatalf("migrate telemetry tables: %v", err)
	}
	if err := telemetry.Migrate(ctx, dbc, logger); err != nil {
		t.Fatalf("migrate telemetry: %v", err)
	}
	if err := telemetry.LoadBuiltinTemplates(dbc); err != nil {
		t.Fatalf("load builtin templates: %v", err)
	}
	monitorStore := telemetry.NewMonitorStore(dbc)
	telemetrySrv := telemetrysvc.NewService(monitorStore, logger)
	templateHandler := telemetryhandler.NewTemplateHandler(
		telemetry.NewTemplateStore(dbc), telemetrySrv)

	// Telemetry report handler with a no-op ingest.
	webhookListener := telemetryhttp.New(
		telemetryhttp.WithStore(assetStore),
		telemetryhttp.WithIngest(func(context.Context, *telemetry.Telemetry) {}),
		telemetryhttp.WithLogger(logger),
	)
	reportAdapter := telemetryhandler.NewTelemetryReportHandlerAdapter(
		webhookListener.ReportHandler(), logger)

	// System service.
	systemSrv := systemsvc.New(dbc, logger, taskStore, execStore, assetStore)
	if err := systemSrv.Migrate(ctx); err != nil {
		t.Fatalf("migrate system service: %v", err)
	}

	// HTTP server on an ephemeral port. api.Server does not expose its
	// listener, so reserve a free port up front (racy in principle, fine in
	// practice for a test process).
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	api.SetLogger(logger)
	cfg := api.ServerConfig{Addr: addr, Mode: "release"}
	cfg.SetDefaults()
	srv := api.NewServer(cfg)

	routeOpts := []router.RegisterOption{
		router.WithTaskService(scheduler.NewTaskService(schedEngine, taskStore, execStore, logger)),
		router.WithAlertService(prismsvc.NewAlertService(
			prismEngine.RuleStore(), prismEngine.RecordStore(), prismEngine.RuleEngine())),
		router.WithChannelService(prismsvc.NewChannelService(prismEngine.ChannelStore(), prismEngine)),
		router.WithRemediationRuleService(prismsvc.NewRemediationService(prismEngine.RemediationStore())),
		router.WithSystemService(systemSrv),
		router.WithTelemetryService(telemetrySrv),
		router.WithTelemetryReportHandler(reportAdapter),
		router.WithTelemetryDataStores(telemetry.NewMetricStore(dbc), telemetry.NewLogStore(dbc)),
		router.WithAssetHandler(assethandler.NewHandler(assetStore, logger)),
		router.WithTemplateHandler(templateHandler),
		router.WithHealthzHandler(healthz.NewHandler(dbc, nil)),
		router.WithReadyzHandler(readyz.NewHandler(dbc, nil)),
	}
	assetKeyGetter := func(ctx context.Context, key string) (bool, error) {
		return assetStore.ExistsByKey(ctx, key)
	}
	if err := router.RegisterRoutes(srv, jwtMgr, authz, assetKeyGetter, routeOpts...); err != nil {
		t.Fatalf("register routes: %v", err)
	}

	go func() { _ = srv.Start() }()

	h = &harness{
		t:           t,
		baseURL:     "http://" + addr,
		client:      &http.Client{Timeout: 10 * time.Second},
		dbc:         dbc,
		authz:       authz,
		jwtMgr:      jwtMgr,
		prismEngine: prismEngine,
		schedEngine: schedEngine,
		execRunner:  execRunner,
		assetStore:  assetStore,
	}
	waitHealthy(t, h.baseURL)
	return h
}

func (hs *harness) shutdown() {
	ctx, cancel := context.WithTimeout(context.Background(), maintenanceAdmin)
	defer cancel()
	_ = hs.schedEngine.Stop(ctx)
	_ = hs.execRunner.Stop(ctx)
	hs.authz.Close()
}

func seedUser(t *testing.T, ctx context.Context, store user.Store, username, pwd, email string, role int64) {
	t.Helper()
	hash, err := password.Hash(pwd)
	if err != nil {
		t.Fatalf("hash password for %s: %v", username, err)
	}
	if _, err := store.Create(ctx, username, hash, email, role); err != nil {
		t.Fatalf("seed user %s: %v", username, err)
	}
}

func waitHealthy(t *testing.T, baseURL string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(baseURL + "/healthz")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("server did not become healthy within 5s")
}

// response is the unified API envelope.
type response struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// do performs an authenticated (when token != "") JSON request and decodes
// the envelope. body may be nil.
func (hs *harness) do(method, path string, body any, token string) (int, *response) {
	hs.t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			hs.t.Fatalf("marshal request body: %v", err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, hs.baseURL+path, reader)
	if err != nil {
		hs.t.Fatalf("build request %s %s: %v", method, path, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := hs.client.Do(req)
	if err != nil {
		hs.t.Fatalf("perform %s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		hs.t.Fatalf("read response %s %s: %v", method, path, err)
	}
	var env response
	if err := json.Unmarshal(raw, &env); err != nil {
		hs.t.Fatalf("decode envelope %s %s (status %d, body %s): %v",
			method, path, resp.StatusCode, string(raw), err)
	}
	return resp.StatusCode, &env
}

// doAPIKey performs a request authenticated with a raw API key via the
// X-Tickraft-API-Key header.
func (hs *harness) doAPIKey(method, path, apiKey string) (int, *response) {
	hs.t.Helper()
	req, err := http.NewRequest(method, hs.baseURL+path, nil)
	if err != nil {
		hs.t.Fatalf("build request %s %s: %v", method, path, err)
	}
	req.Header.Set("X-Tickraft-API-Key", apiKey)
	resp, err := hs.client.Do(req)
	if err != nil {
		hs.t.Fatalf("perform %s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		hs.t.Fatalf("read response %s %s: %v", method, path, err)
	}
	var env response
	if err := json.Unmarshal(raw, &env); err != nil {
		hs.t.Fatalf("decode envelope %s %s (status %d, body %s): %v",
			method, path, resp.StatusCode, string(raw), err)
	}
	return resp.StatusCode, &env
}

// mustOK asserts HTTP 200 with envelope code 0 and decodes data into out.
func (hs *harness) mustOK(status int, env *response, path string, out any) {
	hs.t.Helper()
	if status != http.StatusOK {
		hs.t.Fatalf("%s: expected HTTP 200, got %d (code=%d, message=%s)",
			path, status, env.Code, env.Message)
	}
	if env.Code != 0 {
		hs.t.Fatalf("%s: expected envelope code 0, got %d (%s)", path, env.Code, env.Message)
	}
	if out != nil && len(env.Data) > 0 {
		if err := json.Unmarshal(env.Data, out); err != nil {
			hs.t.Fatalf("%s: decode data: %v", path, err)
		}
	}
}

// login authenticates and returns the access token. It fails the test on
// anything but success.
func (hs *harness) login(username, pwd string) string {
	hs.t.Helper()
	status, env := hs.do("POST", "/api/v1/auth/login",
		map[string]string{"username": username, "password": pwd}, "")
	var pair struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	hs.mustOK(status, env, "login", &pair)
	if pair.AccessToken == "" || pair.RefreshToken == "" {
		hs.t.Fatalf("login: empty token pair returned")
	}
	return pair.AccessToken
}

// pageData decodes a PageData envelope.
type pageData struct {
	Items    []map[string]any `json:"items"`
	Total    int64            `json:"total"`
	Page     int              `json:"page"`
	PageSize int              `json:"page_size"`
}

func (hs *harness) listPage(token, path string) pageData {
	hs.t.Helper()
	status, env := hs.do("GET", path, nil, token)
	var pd pageData
	hs.mustOK(status, env, path, &pd)
	return pd
}
