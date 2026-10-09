// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package i18n

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/fsnotify/fsnotify"
	"go.uber.org/zap"
)

// jsonExt is the file extension that selects the JSON resource parser.
const jsonExt = ".json"

// tomlExt is the file extension that selects the TOML resource parser.
const tomlExt = ".toml"

// Loader loads locale resource files from an fs.FS into a Registry. It
// supports both TOML (used by the backend) and JSON (used by tooling) file
// formats, dispatched by file extension.
//
// The Loader is stateless: each Load call parses the provided filesystem and
// registers the resulting bundles into the target Registry. Hot-reload is
// provided by Watch, which monitors a host directory via fsnotify and
// re-invokes Load on the relevant file when it changes.
type Loader struct {
	logger *zap.Logger
}

// NewLoader creates a Loader. A nil logger is replaced with a no-op logger.
func NewLoader(logger *zap.Logger) *Loader {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Loader{logger: logger}
}

// Load walks the provided filesystem and validates each .toml or .json
// resource file by parsing it. Files that fail to parse are reported via a
// warning log and counted in the returned error. Load does not register
// bundles into any Registry; callers that need registration should use
// LoadToRegistry. An empty filesystem is not an error.
//
// The expected file layout is:
//
//	<root>/
//	  en.toml
//	  zh-Hans.toml
//
// Files whose stem does not parse as a valid BCP 47 tag are skipped with a
// warning.
func (l *Loader) Load(fsys fs.FS) error {
	return l.LoadInto(fsys, nil)
}

// LoadInto walks the provided filesystem, parses each .toml or .json resource
// file, and merges the resulting key/value pairs into the target Bundle when
// target is a *MessageMap whose locale tag matches the file's stem. Files
// whose stem does not match the target's locale are skipped silently.
//
// LoadInto is used by callers to merge additional resource packs
// onto the base packs at startup without copying keys manually.
// Callers that need to register bundles into a Registry should use
// LoadToRegistry instead.
func (l *Loader) LoadInto(fsys fs.FS, target Bundle) error {
	var failed int
	err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			failed++
			l.logger.Warn("i18n loader walk error",
				zap.String("path", path),
				zap.Error(err),
			)
			return nil
		}
		if d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != tomlExt && ext != jsonExt {
			return nil
		}

		stem := strings.TrimSuffix(filepath.Base(path), ext)
		loc := Parse(stem)
		// Detect Parse fallback: if the parsed language doesn't match
		// the stem's first subtag, the stem was invalid.
		stemFirst := strings.ToLower(strings.SplitN(stem, "-", 2)[0])
		if loc.Language != stemFirst {
			l.logger.Warn("i18n loader skipping file with invalid locale stem",
				zap.String("path", path),
				zap.String("stem", stem),
			)
			return nil
		}

		values, err := parseResourceFile(fsys, path, ext)
		if err != nil {
			failed++
			l.logger.Warn("i18n loader failed to parse resource file",
				zap.String("path", path),
				zap.Error(err),
			)
			return nil
		}

		// Merge into the target only when the target is a *MessageMap
		// with a matching locale tag. Other files are validated but
		// not merged, allowing LoadInto to double as a validation pass.
		if target != nil {
			if mm, ok := target.(*MessageMap); ok && mm.Locale() == loc.Tag {
				mm.Merge(NewMessageMap(loc.Tag, values))
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("i18n loader: walk: %w", err)
	}
	if failed > 0 {
		return fmt.Errorf("i18n loader: %d file(s) failed to parse", failed)
	}
	return nil
}

// LoadToRegistry walks the provided filesystem, parses each .toml or .json
// resource file, and registers a Bundle for each locale into the target
// Registry under its filename stem (normalized to standard BCP 47 casing).
// This is the canonical entry point used by the kernel and
// callers at startup.
func (l *Loader) LoadToRegistry(fsys fs.FS, r Registry) error {
	if r == nil {
		return errors.New("i18n loader: registry is nil")
	}
	var failed int
	err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			failed++
			l.logger.Warn("i18n loader walk error",
				zap.String("path", path),
				zap.Error(err),
			)
			return nil
		}
		if d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != tomlExt && ext != jsonExt {
			return nil
		}

		stem := strings.TrimSuffix(filepath.Base(path), ext)
		loc := Parse(stem)
		// Detect Parse fallback: if the parsed language doesn't match
		// the stem's first subtag, the stem was invalid.
		stemFirst := strings.ToLower(strings.SplitN(stem, "-", 2)[0])
		if loc.Language != stemFirst {
			l.logger.Warn("i18n loader skipping file with invalid locale stem",
				zap.String("path", path),
				zap.String("stem", stem),
			)
			return nil
		}

		values, err := parseResourceFile(fsys, path, ext)
		if err != nil {
			failed++
			l.logger.Warn("i18n loader failed to parse resource file",
				zap.String("path", path),
				zap.Error(err),
			)
			return nil
		}

		r.Register(loc.Tag, NewMessageMap(loc.Tag, values))
		return nil
	})
	if err != nil {
		return fmt.Errorf("i18n loader: walk: %w", err)
	}
	if failed > 0 {
		return fmt.Errorf("i18n loader: %d file(s) failed to parse", failed)
	}
	return nil
}

// parseResourceFile opens the file from fsys, parses it according to ext,
// and returns the resulting key/value map. TOML files may use either a flat
// "key = value" layout or a nested table layout; nested tables are joined
// with dots to form the canonical key path (e.g. [alert.metric] title="..."
// becomes "alert.metric.title").
func parseResourceFile(fsys fs.FS, path, ext string) (map[string]string, error) {
	data, err := fs.ReadFile(fsys, path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return parseResourceBytes(data, ext)
}

// parseResourceBytes parses raw resource bytes according to ext. It is the
// shared parsing path used by both LoadToRegistry (which reads via fs.FS)
// and the hot-reload watcher (which reads from the host filesystem).
func parseResourceBytes(data []byte, ext string) (map[string]string, error) {
	out := make(map[string]string)
	switch ext {
	case tomlExt:
		var anyMap map[string]any
		if err := toml.Unmarshal(data, &anyMap); err != nil {
			return nil, fmt.Errorf("parse toml: %w", err)
		}
		flattenAny("", anyMap, out)
		return out, nil
	case jsonExt:
		parsed, err := parseJSON(data)
		if err != nil {
			return nil, fmt.Errorf("parse json: %w", err)
		}
		return parsed, nil
	default:
		return nil, fmt.Errorf("unsupported extension %q", ext)
	}
}

// openOSFileBytes reads the entire contents of a host-filesystem file. It
// is used by the hot-reload watcher, which receives absolute paths from
// fsnotify that must not be re-joined with the watch directory.
func openOSFileBytes(path string) ([]byte, error) {
	return os.ReadFile(path) //nolint:gosec // locale file path from controlled registry
}

// Watch monitors dir for changes to .toml or .json resource files and
// re-registers the affected locale's bundle into r. The watch loop runs in
// a goroutine until ctx is cancelled or an unrecoverable watcher error
// occurs. The returned error is non-nil only when the watcher fails to
// start; runtime errors are logged and the watch continues.
//
// Atomic bundle replacement is guaranteed by the Registry's lock-protected
// Register method: in-flight Translator reads against the previous bundle
// complete before the new bundle is published, so consumers never observe a
// partially-loaded bundle.
func (l *Loader) Watch(ctx context.Context, dir string, r Registry) error {
	if r == nil {
		return errors.New("i18n loader: registry is nil")
	}
	if dir == "" {
		return errors.New("i18n loader: watch directory is empty")
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("i18n loader: create watcher: %w", err)
	}
	// Ensure the watcher is closed on return regardless of how we exit.
	// ignored because: best-effort cleanup in defer; the watcher is no longer
	// used after Watch returns and a Close error has no actionable recovery.
	defer func() {
		_ = watcher.Close()
	}()

	if err := watcher.Add(dir); err != nil {
		return fmt.Errorf("i18n loader: watch directory %s: %w", dir, err)
	}

	l.logger.Info("i18n loader watching directory",
		zap.String("dir", dir),
	)

	// debounce coalesces rapid write events for the same file into a single
	// reload. This avoids double-reloads when editors save via temp file
	// rename (which produces Write + Create events in quick succession).
	const debounce = 200 * time.Millisecond
	pending := &pendingEvents{events: make(map[string]time.Time)}

	reload := func(path string) {
		ext := strings.ToLower(filepath.Ext(path))
		if ext != tomlExt && ext != jsonExt {
			return
		}
		stem := strings.TrimSuffix(filepath.Base(path), ext)
		loc := Parse(stem)
		stemFirst := strings.ToLower(strings.SplitN(stem, "-", 2)[0])
		if loc.Language != stemFirst {
			return
		}

		// The watcher emits absolute or dir-relative paths; read directly
		// from the host filesystem instead of going through osDirFS, which
		// would double-join the directory prefix.
		data, err := openOSFileBytes(path)
		if err != nil {
			l.logger.Warn("i18n loader hot-reload read failed, keeping previous bundle",
				zap.String("path", path),
				zap.Error(err),
			)
			return
		}
		values, err := parseResourceBytes(data, ext)
		if err != nil {
			l.logger.Warn("i18n loader hot-reload parse failed, keeping previous bundle",
				zap.String("path", path),
				zap.Error(err),
			)
			return
		}
		r.Register(loc.Tag, NewMessageMap(loc.Tag, values))
		l.logger.Info("i18n loader hot-reload replaced bundle",
			zap.String("locale", loc.Tag),
			zap.Int("keys", len(values)),
		)
	}

	// goroutine lifecycle: bound to ctx, exits on ctx.Done() or when the
	// fsnotify watcher channels are closed (which happens when watcher.Close
	// runs in the deferred cleanup above).
	go l.watchLoop(ctx, watcher, pending, reload, debounce)

	<-ctx.Done()
	return nil
}

// pendingEvents is the set of files with reloads awaiting debounce,
// guarded by mu. Watch owns the instance and shares it with the watch
// loop goroutine through a pointer, so the mutex is never copied.
type pendingEvents struct {
	mu     sync.Mutex
	events map[string]time.Time
}

// watchLoop consumes fsnotify events until ctx is cancelled or the watcher
// channels are closed. It coalesces rapid write events for the same file
// into a single reload: events are recorded in pending (guarded by mu) and
// reloaded once they have aged past debounce.
func (l *Loader) watchLoop(
	ctx context.Context,
	watcher *fsnotify.Watcher,
	pending *pendingEvents,
	reload func(string),
	debounce time.Duration,
) {
	debounceTimer := time.NewTimer(debounce)
	debounceTimer.Stop()
	defer debounceTimer.Stop()

	for {
		select {
		case <-ctx.Done():
			l.logger.Info("i18n loader watch stopped")
			return
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) == 0 {
				continue
			}
			pending.mu.Lock()
			pending.events[event.Name] = time.Now()
			debounceTimer.Reset(debounce)
			pending.mu.Unlock()
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			l.logger.Warn("i18n loader watcher error",
				zap.Error(err),
			)
		case <-debounceTimer.C:
			pending.mu.Lock()
			now := time.Now()
			for path, t := range pending.events {
				if now.Sub(t) >= debounce {
					delete(pending.events, path)
					reload(path)
				}
			}
			if len(pending.events) > 0 {
				debounceTimer.Reset(debounce)
			}
			pending.mu.Unlock()
		}
	}
}

// osDirFS adapts a host directory path to an fs.FS for parseResourceFile.
// It is a minimal implementation that only supports the Open+Read path
// used by parseResourceFile; it is not a general-purpose fs.FS.
type osDirFS struct {
	root string
}

func (o osDirFS) Open(name string) (fs.File, error) {
	full := filepath.Join(o.root, name)
	return openOSFile(full)
}
