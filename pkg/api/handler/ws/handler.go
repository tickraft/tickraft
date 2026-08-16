// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package ws implements the /ws realtime push endpoint. Authenticated
// clients receive a JSON stream of system events (task lifecycle, asset
// status changes, telemetry threshold breaches, alert and remediation
// events) sourced from the shared event bus.
package ws

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	hws "github.com/hertz-contrib/websocket"
	"github.com/tickraft/tickraft/pkg/api/httputil"
	"github.com/tickraft/tickraft/pkg/auth/jwt"
	"github.com/tickraft/tickraft/pkg/errdefs"
	"github.com/tickraft/tickraft/pkg/event"
	"go.uber.org/zap"
)

const (
	// maxConnections caps the number of concurrent WebSocket clients.
	// The endpoint is an operator convenience, not a fan-out bus.
	maxConnections = 64
	// clientSendBuffer bounds the per-client outbound queue. A client
	// that cannot drain its queue is disconnected rather than allowed
	// to grow server memory without bound.
	clientSendBuffer = 64
	// readWait is the maximum time between client messages (including
	// pings) before the server drops the connection.
	readWait = 120 * time.Second
	// writeWait bounds each outbound write.
	writeWait = 10 * time.Second
)

// broadcastEventTypes lists the event types forwarded to WebSocket
// clients. Subscriptions are shared: every client sees every event of
// these types; the endpoint is admin/operator-facing.
var broadcastEventTypes = []event.Type{
	event.TypeTaskCreated,
	event.TypeTaskUpdated,
	event.TypeTaskDeleted,
	event.TypeTaskPaused,
	event.TypeTaskResumed,
	event.TypeExecutionTriggered,
	event.TypeExecutionStarted,
	event.TypeExecutionCompleted,
	event.TypeExecutionProgressed,
	event.TypeAssetStatusChanged,
	event.TypeAssetFaultDetected,
	event.TypeTelemetryMetricExceeded,
	event.TypeTelemetryLogMatched,
	event.TypeAlertTriggered,
	event.TypeAlertAcknowledged,
	event.TypeAlertResolved,
	event.TypeAlertSuppressed,
	event.TypeAlertNotified,
	event.TypeRemediationTriggered,
	event.TypeRemediationStarted,
	event.TypeRemediationCompleted,
	event.TypeRemediationSkipped,
}

// clientMessage is the inbound message schema. The only defined message
// type is "ping"; unknown types are ignored.
type clientMessage struct {
	Type string `json:"type"`
}

// serverMessage is the outbound event envelope pushed to clients.
type serverMessage struct {
	Type      string       `json:"type"`
	EventID   string       `json:"event_id"`
	Timestamp time.Time    `json:"timestamp"`
	TenantID  string       `json:"tenant_id,omitempty"`
	Data      eventPayload `json:"data"`
}

// eventPayload carries the raw event payload without re-encoding it.
type eventPayload json.RawMessage

// MarshalJSON implements json.Marshaler so the payload is embedded
// verbatim rather than base64-encoded.
func (p eventPayload) MarshalJSON() ([]byte, error) {
	if len(p) == 0 {
		return []byte("null"), nil
	}
	return p, nil
}

// Handler serves the /ws endpoint.
type Handler struct {
	jwtMgr *jwt.JWT
	bus    event.Bus
	logger *zap.Logger

	upgrader hws.HertzUpgrader

	mu        sync.Mutex
	clients   map[*clientConn]struct{}
	connCount int
	subs      []event.Subscription
}

// clientConn couples the WebSocket connection with its outbound queue.
type clientConn struct {
	conn *hws.Conn
	send chan []byte
	// dead is closed (once, via kill) when the client is dropped.
	// writeLoop and readLoop observe it to shut down; send is never
	// closed so senders can never panic on a closed channel.
	dead chan struct{}
	once sync.Once
}

// kill marks the client as dropped. Idempotent.
func (c *clientConn) kill() {
	c.once.Do(func() { close(c.dead) })
}

// NewHandler creates a WS Handler. Call Start to subscribe to the event
// bus; Stop unsubscribes and disconnects all clients.
func NewHandler(jwtMgr *jwt.JWT, bus event.Bus, logger *zap.Logger) *Handler {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Handler{
		jwtMgr:  jwtMgr,
		bus:     bus,
		logger:  logger,
		clients: make(map[*clientConn]struct{}),
	}
}

// Start subscribes the handler to the broadcast event types on the bus.
// It returns an error if any subscription fails; partial subscriptions
// are rolled back.
func (h *Handler) Start(ctx context.Context) error {
	if h.bus == nil {
		return nil
	}
	subs := make([]event.Subscription, 0, len(broadcastEventTypes))
	for _, t := range broadcastEventTypes {
		sub, err := h.bus.Subscribe(t, func(_ context.Context, env event.Envelope) error {
			h.broadcast(env)
			return nil
		})
		if err != nil {
			for _, s := range subs {
				s.Cancel()
			}
			return err
		}
		subs = append(subs, sub)
	}
	h.mu.Lock()
	h.subs = subs
	h.mu.Unlock()
	h.logger.Info("ws handler started", zap.Int("event_types", len(subs)))
	return nil
}

// Stop unsubscribes from the bus and closes all client connections.
func (h *Handler) Stop() {
	h.mu.Lock()
	subs := h.subs
	h.subs = nil
	clients := h.clients
	h.clients = make(map[*clientConn]struct{})
	h.connCount = 0
	h.mu.Unlock()

	for _, s := range subs {
		s.Cancel()
	}
	for c := range clients {
		// kill (never close) so concurrent senders on c.send cannot
		// panic on a closed channel; writeLoop closes the connection.
		c.kill()
	}
}

// ServeHTTP implements the Hertz handler for GET /ws.
func (h *Handler) ServeHTTP(_ context.Context, arc *app.RequestContext) {
	// Authenticate via the access token query parameter before upgrading.
	token := string(arc.QueryArgs().Peek("token"))
	if token == "" {
		httputil.FailWithCode(arc, http.StatusUnauthorized, errdefs.CodeUnauthorized, "missing token")
		arc.Abort()
		return
	}
	claims, err := h.jwtMgr.ValidateToken(token, jwt.TokenTypeAccess)
	if err != nil || claims == nil {
		httputil.FailWithCode(arc, http.StatusUnauthorized, errdefs.CodeUnauthorized, "invalid token")
		arc.Abort()
		return
	}

	h.mu.Lock()
	full := h.connCount >= maxConnections
	h.mu.Unlock()
	if full {
		httputil.FailWithCode(arc, http.StatusServiceUnavailable, errdefs.CodeInternal, "too many websocket connections")
		arc.Abort()
		return
	}

	err = h.upgrader.Upgrade(arc, func(conn *hws.Conn) {
		c := &clientConn{conn: conn, send: make(chan []byte, clientSendBuffer), dead: make(chan struct{})}
		if !h.addClient(c) {
			_ = conn.WriteMessage(hws.TextMessage, []byte(`{"type":"error","message":"too many connections"}`))
			return
		}

		done := make(chan struct{})
		go h.writeLoop(c, done)
		h.readLoop(c)
		close(done)

		h.removeClient(c)
	})
	if err != nil {
		h.logger.Warn("ws upgrade failed", zap.Error(err))
	}
}

// addClient registers a client if capacity allows.
func (h *Handler) addClient(c *clientConn) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.connCount >= maxConnections {
		return false
	}
	h.connCount++
	h.clients[c] = struct{}{}
	return true
}

// removeClient unregisters a client and decrements the connection count.
func (h *Handler) removeClient(c *clientConn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.clients[c]; ok {
		h.connCount--
		delete(h.clients, c)
	}
	if h.connCount < 0 {
		h.connCount = 0
	}
}

// broadcast marshals the envelope and queues it on every client's
// outbound channel. Clients with a full queue are dropped.
func (h *Handler) broadcast(env event.Envelope) {
	payload, err := json.Marshal(env.Payload)
	if err != nil {
		payload = []byte("null")
	}
	msg, err := json.Marshal(serverMessage{
		Type:      string(env.Type),
		EventID:   env.EventID,
		Timestamp: env.Timestamp,
		TenantID:  env.TenantID,
		Data:      eventPayload(payload),
	})
	if err != nil {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		select {
		case c.send <- msg:
		default:
			// Slow client: drop it. kill unblocks writeLoop (which
			// closes the connection) and readLoop follows.
			delete(h.clients, c)
			c.kill()
		}
	}
}

// readLoop is the per-client inbound loop. It enforces the read
// deadline and answers ping messages with pong, matching the frontend
// useWebSocket protocol.
func (h *Handler) readLoop(c *clientConn) {
	defer func() { _ = c.conn.Close() }()
	_ = c.conn.SetReadDeadline(time.Now().Add(readWait))
	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		_ = c.conn.SetReadDeadline(time.Now().Add(readWait))
		var msg clientMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}
		if msg.Type == "ping" {
			select {
			case c.send <- []byte(`{"type":"pong"}`):
			default:
			}
		}
	}
}

// writeLoop is the per-client outbound loop. It exits when the client is
// dropped (dead closed, which also happens when Stop closes send) or the
// connection fails; on exit it closes the connection so the blocked
// readLoop unblocks too.
func (h *Handler) writeLoop(c *clientConn, done <-chan struct{}) {
	defer func() { _ = c.conn.Close() }()
	for {
		select {
		case <-done:
			return
		case <-c.dead:
			return
		case msg, ok := <-c.send:
			if !ok {
				return
			}
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(hws.TextMessage, msg); err != nil {
				c.kill()
				return
			}
		}
	}
}
