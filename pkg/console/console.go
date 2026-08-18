// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package console

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"
)

// defaultReadHeaderTimeout bounds header reads to mitigate slowloris-style
// resource exhaustion on the static-file listener.
const defaultReadHeaderTimeout = 5 * time.Second

// defaultShutdownTimeout is the maximum time allowed for in-flight static
// requests to drain when the Console listener is shutting down.
const defaultShutdownTimeout = 5 * time.Second

// indexFileName is the SPA entrypoint served for unmatched routes so that
// client-side routing (history mode) works without server-side route config.
const indexFileName = "index.html"

// Start starts a static file server for the SPA console on the given addr.
// rootDir is the path to the built SPA assets. If rootDir is empty the
// function is a no-op and returns nil, allowing callers to conditionally
// enable the Console listener (e.g. when only the embedded dist is
// available the API listener already serves the SPA and a separate Console
// listener is unnecessary).
//
// The server implements SPA fallback: any request that does not match a
// static file under rootDir is served index.html so client-side routing
// (history mode) works without per-route server configuration. Requests for
// files outside rootDir are rejected with 404 to prevent path traversal.
//
// The server blocks until ctx is cancelled or it fails to start. When ctx
// is cancelled the server is shut down gracefully with a bounded timeout
// (defaultShutdownTimeout); in-flight requests are allowed to drain before
// the listener is closed. ctx cancellation yields nil; any other error
// reflects a startup or runtime failure.
//
// Callers should run this in a goroutine managed by an errgroup so a
// startup failure on any listener aborts the whole role.
func Start(ctx context.Context, addr, rootDir string) error {
	if rootDir == "" {
		// Console disabled — caller may serve the SPA via the API listener
		// (e.g. embedded dist via api.RegisterSPA) and choose not to expose
		// a dedicated Console port. No-op rather than error so the caller
		// can unconditionally wire this into an errgroup.
		return nil
	}

	// Resolve to an absolute path so SPA fallback can safely compute
	// relative paths and detect traversal attempts. A missing or
	// inaccessible rootDir is a deployment misconfiguration that should
	// surface as a startup error rather than a silent no-op.
	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return err
	}
	info, err := os.Stat(absRoot)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("console: root is not a directory: " + absRoot)
	}

	// Pre-load index.html so SPA fallback can serve it without re-reading
	// the file on every miss. A missing index.html means the SPA build is
	// incomplete; surface it as a startup error rather than serving 404s
	// at runtime.
	//nolint:gosec // path joined from the configured static root
	indexBytes, err := os.ReadFile(filepath.Join(absRoot, indexFileName))
	if err != nil {
		return err
	}

	fileServer := http.FileServer(http.Dir(absRoot))

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		serveSPA(w, r, absRoot, fileServer, indexBytes)
	})

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: defaultReadHeaderTimeout,
	}

	errCh := make(chan error, 1)
	// goroutine lifecycle: bounded — runs srv.ListenAndServe and exits
	// after it returns (either on a server error or after srv.Shutdown
	// is invoked by the caller on ctx.Done). Result is delivered on errCh,
	// which is buffered and drained by the caller before Start returns.
	go func() {
		if listenErr := srv.ListenAndServe(); listenErr != nil && !errors.Is(listenErr, http.ErrServerClosed) {
			errCh <- listenErr
			return
		}
		errCh <- nil
	}()

	zap.L().Info("console server started",
		zap.String("addr", addr),
		zap.String("root_dir", absRoot),
	)

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), defaultShutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return err
		}
		// Drain the goroutine result; the channel is buffered so this
		// never blocks.
		<-errCh
		return nil
	case err := <-errCh:
		return err
	}
}

// serveSPA implements the SPA fallback contract: requests that match a
// static file under absRoot are served directly via fileServer; requests
// that do not match a file (typical for client-side routes such as
// /dashboard or /settings) are served index.html so the SPA router can
// take over. Path traversal attempts are blocked by http.FileServer's
// built-in sanitization before the fallback ever runs.
func serveSPA(w http.ResponseWriter, r *http.Request, absRoot string, fileServer http.Handler, indexBytes []byte) {
	// Check whether the request maps to an existing static file. Using
	// os.Stat on the joined path (after cleaning) is the standard pattern
	// for SPA fallback because http.FileServer itself only serves files,
	// it cannot fall back to index.html on its own.
	rel := strings.TrimPrefix(r.URL.Path, "/")
	if rel == "" {
		serveIndex(w, indexBytes)
		return
	}
	cleaned := filepath.Clean(rel)
	if strings.HasPrefix(cleaned, "..") {
		http.NotFound(w, r)
		return
	}
	target := filepath.Join(absRoot, cleaned)
	info, err := os.Stat(target)
	if err == nil && !info.IsDir() {
		fileServer.ServeHTTP(w, r)
		return
	}
	// Directory requests are ambiguous for an SPA; fall back to index.html
	// so client-side routing handles them. This matches the behavior of
	// most SPA hosting setups (e.g. nginx try_files).
	serveIndex(w, indexBytes)
}

// serveIndex writes the cached index.html with the standard SPA content
// type. The bytes are loaded once at startup so this is allocation-free
// per request.
func serveIndex(w http.ResponseWriter, indexBytes []byte) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	// Write errors here indicate a broken client connection; headers are
	// already sent so there is no useful recovery action. Suppress to keep
	// the SPA fallback path simple.
	// ignored because: broken client connection; headers already sent and
	// no actionable recovery exists on the SPA fallback path.
	_, _ = w.Write(indexBytes)
}
