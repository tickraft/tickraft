// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package telemetry

import (
	"context"
	"fmt"
	"net/http"
	"sync"
)

// HTTPListener is a passive telemetry receiver that mounts onto an existing
// HTTP server. Instead of binding its own socket, it exposes a net/http
// handler that the API router registers on a specific path (e.g.,
// POST /api/v1/telemetry). The listener receives telemetry via HTTP
// requests and forwards it to the ingest callback.
//
// A passive monitoring point (MonitorPoint with Mode=ModePassive and
// Type="webhook") corresponds to an HTTPListener configuration. The
// MonitorPoint.Config field stores the listener-specific settings (path,
// auth method); the HTTPListener implementation provides the runtime
// handler that processes incoming requests.
//
// The builtin webhook listener (pkg/telemetry/http) implements this
// interface. callers may provide additional HTTP listeners that
// expose different endpoint shapes (e.g., ingestion APIs with richer
// authentication).
//
// Implementations must be safe for concurrent use.
type HTTPListener interface {
	// Type returns the listener type identifier (e.g., "webhook").
	Type() string
	// Handler returns the HTTP handler for the telemetry endpoint. The
	// ingest callback is invoked for each received telemetry; it is
	// typically wired to telemetry.Collector.Submit by the caller.
	// The handler must not close the ingest callback or retain it
	// beyond the request lifetime.
	//
	// The callback reports rejections by returning an error wrapping
	// ErrIngestRejected (mapped to 429) or a non-sentinel error (logged,
	// best-effort ingest still acknowledged).
	Handler(ingest func(context.Context, *Telemetry) error) http.HandlerFunc
}

// ProtocolListener is a passive telemetry listener that binds its own
// protocol-specific listener socket (e.g., Syslog UDP, SNMP UDP, MQTT TCP).
// It owns its full lifecycle (Start/Stop) and invokes the ingest callback
// for each received message.
//
// A passive monitoring point (MonitorPoint with Mode=ModePassive) whose
// Type matches a ProtocolListener's Type() corresponds to that listener's
// configuration. The pro edition persists these configurations in the
// sys_monitor_point table and starts the matching ProtocolListener for each
// enabled passive point.
//
// The CE build does not ship any ProtocolListener implementations: all
// implementations (Syslog, SNMP, MQTT) live in the pro repo
// (tickraft-x internal/telemetry) and register through this SPI at
// startup. The SPI lives in the kernel so pro listeners plug in via
// the ListenerRegistry without modifying core code.
//
// Implementations must be safe for concurrent use and support graceful
// cancellation via the context passed to Start.
type ProtocolListener interface {
	// Type returns the listener type identifier (e.g., "syslog", "snmp",
	// "mqtt").
	Type() string
	// Start begins receiving data and invokes the ingest callback for each
	// received message. Start is idempotent and safe to call once per
	// instance. The listener must respect ctx cancellation and stop
	// receiving when ctx is done.
	Start(ctx context.Context, ingest func(context.Context, *Telemetry)) error
	// Stop gracefully shuts down the listener, releasing all resources
	// (sockets, goroutines, connections). Stop is idempotent and safe to
	// call multiple times.
	Stop(ctx context.Context) error
}

// ListenerRegistry manages ProtocolListener registration and lookup. It
// is the SPI registration point for passive telemetry receivers: callers
// register protocol listeners (Syslog, SNMP, MQTT) via this registry at
// startup, and the Engine starts and stops them alongside the listener
// pipeline. HTTP listeners need no registry — they expose a plain handler
// that the API router mounts directly.
//
// The registry is safe for concurrent use.
type ListenerRegistry struct {
	mu                sync.RWMutex
	protocolListeners map[string]ProtocolListener
}

// NewListenerRegistry creates an empty listener registry.
func NewListenerRegistry() *ListenerRegistry {
	return &ListenerRegistry{
		protocolListeners: make(map[string]ProtocolListener),
	}
}

// RegisterProtocol adds a ProtocolListener. Returns an error if a listener
// with the same Type() is already registered.
func (r *ListenerRegistry) RegisterProtocol(l ProtocolListener) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	t := l.Type()
	if _, exists := r.protocolListeners[t]; exists {
		return fmt.Errorf("telemetry: protocol listener for %q already registered", t)
	}
	r.protocolListeners[t] = l
	return nil
}

// ListProtocol returns all registered ProtocolListeners. The order is not
// guaranteed.
func (r *ListenerRegistry) ListProtocol() []ProtocolListener {
	r.mu.RLock()
	defer r.mu.RUnlock()
	listeners := make([]ProtocolListener, 0, len(r.protocolListeners))
	for _, l := range r.protocolListeners {
		listeners = append(listeners, l)
	}
	return listeners
}
