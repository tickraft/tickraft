// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// This file implements TLS configuration construction and hot-reload. The
// hot-reload mechanism keeps a single atomic *tls.Config pointer that the
// running server consults via the GetCertificate / GetConfigForClient
// callbacks; reloading the certificate therefore requires no restart and no
// Hertz-internal mutation. A fsnotify watcher triggers automatic reload when
// the certificate or key file changes on disk; manual reload is exposed via
// Server.ReloadTLSConfig (called by the POST
// /api/v1/system/certificates/reload endpoint).

package api

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/fsnotify/fsnotify"
)

// Sentinel errors for TLS construction and reload. Callers may use errors.Is
// to detect a specific failure.
var (
	// ErrTLSDisabled is returned when ReloadTLSConfig or buildTLSConfig is
	// called on a server whose TLSEnabled is false.
	ErrTLSDisabled = errors.New("tls is disabled")

	// ErrTLSCertFileMissing is returned when TLSCertFile is empty.
	ErrTLSCertFileMissing = errors.New("tls_cert_file is required")

	// ErrTLSKeyFileMissing is returned when TLSKeyFile is empty.
	ErrTLSKeyFileMissing = errors.New("tls_key_file is required")

	// ErrTLSCertLoadFailed is returned when the certificate/key pair cannot be
	// loaded by crypto/tls.LoadX509KeyPair.
	ErrTLSCertLoadFailed = errors.New("tls certificate load failed")

	// ErrTLSClientCALoadFailed is returned when the client CA file cannot be
	// parsed as a PEM-encoded certificate pool.
	ErrTLSClientCALoadFailed = errors.New("tls client CA load failed")

	// ErrTLSInvalidMinVersion is returned when TLSMinVersion is not "1.2" or
	// "1.3".
	ErrTLSInvalidMinVersion = errors.New("tls_min_version must be 1.2 or 1.3")

	// ErrTLSInvalidClientAuth is returned when TLSClientAuth is not one of the
	// accepted modes.
	ErrTLSInvalidClientAuth = errors.New("tls_client_auth is invalid")
)

// tlsConfigHolder is an atomic pointer to the active *tls.Config. Storing the
// pointer atomically lets the GetCertificate / GetConfigForClient callbacks
// read the latest certificate without taking a mutex on the hot path.
type tlsConfigHolder struct {
	value atomic.Pointer[tls.Config]
}

// load stores cfg as the active TLS configuration. Storing nil clears the
// active configuration.
func (h *tlsConfigHolder) load(cfg *tls.Config) {
	h.value.Store(cfg)
}

// current returns the active TLS configuration, or nil if none has been
// loaded.
func (h *tlsConfigHolder) current() *tls.Config {
	return h.value.Load()
}

// tlsWatcher wraps a fsnotify watcher and the goroutine lifecycle of the
// automatic reload loop. It is created by startTLSFileWatcher and stopped by
// its stop method.
type tlsWatcher struct {
	watcher *fsnotify.Watcher
	stopCh  chan struct{}
	doneCh  chan struct{}
}

// stop closes the watcher and signals the reload goroutine to exit, then waits
// for the goroutine to acknowledge. It is safe to call multiple times; the
// first call performs the actual shutdown.
func (w *tlsWatcher) stop() {
	close(w.stopCh)
	if w.watcher != nil {
		// ignored because: fsnotify.Watcher.Close errors are not actionable on
		// the shutdown path. The watcher is being torn down regardless and the
		// doneCh wait below guarantees the reload goroutine has exited.
		_ = w.watcher.Close()
	}
	<-w.doneCh
}

// ReloadTLSConfig atomically rebuilds and replaces the active TLS
// configuration from the certificate and key files referenced by
// s.config. It is called automatically by the fsnotify watcher when either
// file changes, and manually by the certificate-reload API endpoint. The
// caller is responsible for any logging; this method returns the certificate
// fingerprint (SHA-256) of the newly loaded leaf certificate so callers (e.g.
// the reload endpoint) can return it to the operator.
//
// The method is safe for concurrent use: the underlying tlsConfigHolder uses
// an atomic pointer, and the rebuild itself is stateless.
func (s *Server) ReloadTLSConfig() (string, error) {
	cfg, fingerprint, err := s.buildTLSConfig()
	if err != nil {
		return "", err
	}

	// Install a GetCertificate callback that always consults the atomic
	// holder. Hertz reads options.TLS once at construction time, but the
	// GetCertificate callback is invoked on every TLS handshake, so routing
	// through the holder gives us live reload without touching Hertz internals.
	cfg.GetCertificate = func(_ *tls.ClientHelloInfo) (*tls.Certificate, error) {
		active := s.tlsHolder.current()
		if active == nil || len(active.Certificates) == 0 {
			return nil, ErrTLSDisabled
		}
		return &active.Certificates[0], nil
	}
	cfg.GetConfigForClient = func(_ *tls.ClientHelloInfo) (*tls.Config, error) {
		return s.tlsHolder.current(), nil
	}

	s.tlsHolder.load(cfg)
	return fingerprint, nil
}

// buildTLSConfig constructs a fresh *tls.Config from s.config. It loads the
// certificate and key pair, applies the minimum version, cipher suites, and
// client-auth settings, and returns the SHA-256 fingerprint of the leaf
// certificate. It does not mutate s.tlsHolder; the caller (typically
// ReloadTLSConfig) is responsible for atomically publishing the new config.
//
// Validation mirrors ServerConfig.Validate but is repeated here so that a
// runtime reload fails fast on a misconfigured file path rather than producing
// a permissive TLS config.
func (s *Server) buildTLSConfig() (*tls.Config, string, error) {
	cfg := s.config
	if !cfg.TLSEnabled {
		return nil, "", ErrTLSDisabled
	}
	if cfg.TLSCertFile == "" {
		return nil, "", ErrTLSCertFileMissing
	}
	if cfg.TLSKeyFile == "" {
		return nil, "", ErrTLSKeyFileMissing
	}

	cert, err := tls.LoadX509KeyPair(cfg.TLSCertFile, cfg.TLSKeyFile)
	if err != nil {
		return nil, "", fmt.Errorf("load key pair %q + %q: %w",
			cfg.TLSCertFile, cfg.TLSKeyFile, err)
	}
	if len(cert.Certificate) == 0 {
		return nil, "", ErrTLSCertLoadFailed
	}

	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return nil, "", fmt.Errorf("parse leaf certificate: %w", err)
	}
	fingerprint := certFingerprintSHA256(leaf)

	minVersion, err := parseTLSMinVersion(cfg.TLSMinVersion)
	if err != nil {
		return nil, "", err
	}
	cipherSuites, err := parseTLSCipherSuites(cfg.TLSCipherSuites)
	if err != nil {
		return nil, "", err
	}
	clientAuth, err := parseTLSClientAuth(cfg.TLSClientAuth)
	if err != nil {
		return nil, "", err
	}

	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   minVersion,
		CipherSuites: cipherSuites,
		ClientAuth:   clientAuth,
		NextProtos:   []string{"h2", "http/1.1"},
	}

	if cfg.TLSClientCAFile != "" {
		pool, err := loadCABundle(cfg.TLSClientCAFile)
		if err != nil {
			return nil, "", fmt.Errorf("load client CA %q: %w", cfg.TLSClientCAFile, err)
		}
		tlsCfg.ClientCAs = pool
	}

	return tlsCfg, fingerprint, nil
}

// startTLSFileWatcher starts a fsnotify watcher on the certificate and key
// files referenced by s.config. When either file is written, created, or
// renamed, the watcher triggers a ReloadTLSConfig call. The returned stop
// function closes the watcher and waits for the reload goroutine to exit; it
// is safe to call when the watcher is nil (TLS disabled).
//
// The watcher is best-effort: a failure to add a watch or to reload the
// certificate is logged at warning level but does not abort startup, so a
// transient fsnotify failure cannot take an otherwise-healthy server down.
// Operators may always trigger a manual reload via the
// POST /api/v1/system/certificates/reload endpoint.
func (s *Server) startTLSFileWatcher() (stop func()) {
	cfg := s.config
	if !cfg.TLSEnabled || cfg.TLSCertFile == "" || cfg.TLSKeyFile == "" {
		return func() {}
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		hlog.SystemLogger().Warnf("tls watcher: create fsnotify watcher: %v "+
			"(auto-reload disabled; use the reload API)", err)
		return func() {}
	}

	w := &tlsWatcher{
		watcher: watcher,
		stopCh:  make(chan struct{}),
		doneCh:  make(chan struct{}),
	}

	// Watch the parent directories as well as the files themselves: when a
	// tool rotates the certificate by atomically renaming a new file over the
	// old one, the inode-level file watch fires only once and may miss
	// subsequent rotations, whereas a directory watch reliably catches every
	// rename into the directory.
	watchTargets := uniqueParentDirs(cfg.TLSCertFile, cfg.TLSKeyFile)
	for _, target := range append(watchTargets, cfg.TLSCertFile, cfg.TLSKeyFile) {
		if target == "" {
			continue
		}
		if err := watcher.Add(target); err != nil {
			hlog.SystemLogger().Warnf("tls watcher: add watch %q: %v (manual reload still available)", target, err)
		}
	}

	// goroutine lifecycle: bounded — owns the fsnotify event loop. Exits when
	// stop() closes stopCh, when the watcher.Events channel is closed, or when
	// the watcher.Errors channel is closed. Coordinated via doneCh so callers
	// can wait for full teardown.
	go s.runTLSWatcherLoop(w)

	return w.stop
}

// runTLSWatcherLoop consumes fsnotify events and triggers a reload on
// certificate or key changes. A short debounce window collapses bursts of
// write events (common during atomic renames) into a single reload. The loop
// exits when stop() is called or the watcher is closed.
func (s *Server) runTLSWatcherLoop(w *tlsWatcher) {
	defer close(w.doneCh)

	const debounce = 200 * time.Millisecond
	var debounceTimer *time.Timer

	for {
		select {
		case <-w.stopCh:
			return
		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			if !isTLSFileEvent(s.config, event) {
				continue
			}
			// Reset (or start) the debounce timer so a burst of events
			// produces a single reload.
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			debounceTimer = time.AfterFunc(debounce, func() {
				if _, err := s.ReloadTLSConfig(); err != nil {
					hlog.SystemLogger().Warnf("tls watcher: auto reload failed: %v", err)
				} else {
					hlog.SystemLogger().Info("tls watcher: certificate reloaded")
				}
			})
		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			hlog.SystemLogger().Warnf("tls watcher: error: %v", err)
		}
	}
}

// isTLSFileEvent reports whether a fsnotify event affects the configured
// certificate or key file. Operations that do not change the file contents
// (chmod on a non-existent file) are ignored.
func isTLSFileEvent(cfg ServerConfig, event fsnotify.Event) bool {
	if event.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Rename) == 0 {
		return false
	}
	cleaned := filepath.Clean(event.Name)
	return cleaned == filepath.Clean(cfg.TLSCertFile) ||
		cleaned == filepath.Clean(cfg.TLSKeyFile)
}

// uniqueParentDirs returns the parent directories of the given file paths,
// de-duplicated and cleaned. Empty inputs are skipped.
func uniqueParentDirs(paths ...string) []string {
	seen := make(map[string]struct{})
	var dirs []string
	for _, p := range paths {
		if p == "" {
			continue
		}
		dir := filepath.Clean(filepath.Dir(p))
		if _, ok := seen[dir]; ok {
			continue
		}
		seen[dir] = struct{}{}
		dirs = append(dirs, dir)
	}
	return dirs
}

// parseTLSMinVersion translates the string form ("1.2" or "1.3") to the
// crypto/tls constant. An empty string defaults to TLS 1.2 to match
// DefaultTLSMinVersion.
func parseTLSMinVersion(s string) (uint16, error) {
	switch s {
	case "", DefaultTLSMinVersion:
		return tls.VersionTLS12, nil
	case tlsMinVersion13:
		return tls.VersionTLS13, nil
	default:
		return 0, fmt.Errorf("tls_min_version %q: %w", s, ErrTLSInvalidMinVersion)
	}
}

// parseTLSCipherSuites translates cipher-suite names to crypto/tls IDs. Names
// that crypto/tls does not recognise are rejected so that a typo in the config
// cannot silently weaken the cipher whitelist. An empty input returns nil,
// letting crypto/tls pick its default suite list (forward-secret only).
func parseTLSCipherSuites(names []string) ([]uint16, error) {
	if len(names) == 0 {
		return nil, nil
	}
	known := make(map[string]uint16, len(tls.CipherSuites()))
	for _, suite := range tls.CipherSuites() {
		known[suite.Name] = suite.ID
	}
	out := make([]uint16, 0, len(names))
	for _, name := range names {
		id, ok := known[name]
		if !ok {
			return nil, fmt.Errorf("unknown cipher suite %q", name)
		}
		out = append(out, id)
	}
	return out, nil
}

// parseTLSClientAuth translates the string client-auth mode to the
// crypto/tls.ClientAuthType constant. An empty string defaults to
// NoClientCert to match DefaultTLSClientAuth.
func parseTLSClientAuth(s string) (tls.ClientAuthType, error) {
	switch s {
	case "", DefaultTLSClientAuth:
		return tls.NoClientCert, nil
	case clientAuthRequestCert:
		return tls.RequestClientCert, nil
	case clientAuthRequireAnyCert:
		return tls.RequireAnyClientCert, nil
	case clientAuthVerifyIfGiven:
		return tls.VerifyClientCertIfGiven, nil
	case clientAuthRequireAndVerify:
		return tls.RequireAndVerifyClientCert, nil
	default:
		return 0, fmt.Errorf("tls_client_auth %q: %w", s, ErrTLSInvalidClientAuth)
	}
}

// loadCABundle reads a PEM-encoded CA bundle from path and returns it as a
// *x509.CertPool. An empty file is a configuration error and returns an error.
func loadCABundle(path string) (*x509.CertPool, error) {
	data, err := os.ReadFile(path) //nolint:gosec // CA bundle path from operator config
	if err != nil {
		return nil, fmt.Errorf("read CA bundle: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(data) {
		return nil, ErrTLSClientCALoadFailed
	}
	return pool, nil
}

// certFingerprintSHA256 returns the SHA-256 fingerprint of a certificate as a
// lowercase hex string. The fingerprint is stable for a given certificate and
// is suitable for returning to operators from the reload API so they can
// verify that a reload picked up the expected certificate.
func certFingerprintSHA256(cert *x509.Certificate) string {
	if cert == nil || len(cert.Raw) == 0 {
		return ""
	}
	sum := sha256.Sum256(cert.Raw)
	return hex.EncodeToString(sum[:])
}
