// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package mqtt

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/bytedance/sonic"

	"github.com/tickraft/tickraft/pkg/executor"
	"github.com/tickraft/tickraft/pkg/types"
)

// stubServer is a minimal packet-level MQTT v5 server that covers the
// probe surface: it answers CONNECT with a success CONNACK, answers
// SUBSCRIBE with a granted SUBACK, and optionally publishes one message.
// It is intentionally not a conformant MQTT server.
type stubServer struct {
	ln      net.Listener
	topic   string
	payload []byte
	publish bool
}

// newStubServer starts a stub server listening on 127.0.0.1:0. Call
// close on the returned server when done.
func newStubServer(t *testing.T, topic string, payload []byte, publish bool) *stubServer {
	t.Helper()
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s := &stubServer{ln: ln, topic: topic, payload: payload, publish: publish}
	go s.serve()
	t.Cleanup(func() { _ = ln.Close() })
	return s
}

func (s *stubServer) addr() string { return s.ln.Addr().String() }

func (s *stubServer) serve() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handle(conn)
	}
}

func (s *stubServer) handle(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	for {
		packetType, body, err := readPacket(conn)
		if err != nil {
			return
		}
		switch packetType {
		case 1: // CONNECT -> CONNACK (success, no properties)
			if _, err := conn.Write([]byte{0x20, 0x03, 0x00, 0x00, 0x00}); err != nil {
				return
			}
		case 8: // SUBSCRIBE -> SUBACK (granted QoS 0)
			if len(body) < 2 {
				return
			}
			suback := []byte{0x90, 0x04, body[0], body[1], 0x00, 0x00}
			if _, err := conn.Write(suback); err != nil {
				return
			}
			if s.publish {
				go func() {
					// Give the client a moment to finish the subscribe
					// round trip before the publish arrives.
					time.Sleep(50 * time.Millisecond)
					_ = writePublish(conn, s.topic, s.payload)
				}()
			}
		case 12: // PINGREQ -> PINGRESP
			if _, err := conn.Write([]byte{0xD0, 0x00}); err != nil {
				return
			}
		case 14: // DISCONNECT
			return
		}
	}
}

// readPacket reads one MQTT packet and returns its type (the high nibble
// of the first byte) and body (the remaining-length payload).
func readPacket(r io.Reader) (packetType byte, body []byte, err error) {
	var header [1]byte
	if _, err = io.ReadFull(r, header[:]); err != nil {
		return 0, nil, err
	}
	// Remaining length: MQTT variable-length encoding (up to 4 bytes).
	var multiplier, length uint32
	for {
		var b [1]byte
		if _, err = io.ReadFull(r, b[:]); err != nil {
			return 0, nil, err
		}
		length |= uint32(b[0]&0x7F) << multiplier
		if b[0]&0x80 == 0 {
			break
		}
		multiplier += 7
		if multiplier > 21 {
			return 0, nil, fmt.Errorf("malformed remaining length")
		}
	}
	body = make([]byte, length)
	if _, err = io.ReadFull(r, body); err != nil {
		return 0, nil, err
	}
	return header[0] >> 4, body, nil
}

// writePublish writes a QoS 0 MQTT v5 PUBLISH packet.
func writePublish(w io.Writer, topic string, payload []byte) error {
	body := make([]byte, 0, 2+len(topic)+1+len(payload))
	body = binary.BigEndian.AppendUint16(body, uint16(len(topic)))
	body = append(body, topic...)
	body = append(body, 0x00) // properties length
	body = append(body, payload...)
	packet := append([]byte{0x30, byte(len(body))}, body...)
	_, err := w.Write(packet)
	return err
}

// runProbe executes the executor with a JSON config built from the given
// fields and returns the result.
func runProbe(t *testing.T, e *Executor, cfg string) *executor.Result {
	t.Helper()
	result, err := e.Execute(t.Context(), executor.ExecutionRequest{Config: cfg})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result == nil {
		t.Fatal("execute: nil result")
	}
	return result
}

func marshalConfig(t *testing.T, cfg config) string {
	t.Helper()
	data, err := sonic.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	return string(data)
}

func TestNameAndCapabilities(t *testing.T) {
	e := New(5 * time.Second)
	if e.Name() != "mqtt_probe" {
		t.Fatalf("Name() = %q, want mqtt_probe", e.Name())
	}
	if !executor.HasCap(e.Capabilities(), executor.CapProbe) {
		t.Fatal("Capabilities() should include CapProbe")
	}
	if e.Capabilities() != executor.CapProbe {
		t.Fatalf("Capabilities() = %d, want probe-only", e.Capabilities())
	}
}

func TestProbeValidation(t *testing.T) {
	e := New(5 * time.Second)

	r := runProbe(t, e, marshalConfig(t, config{Topic: "sensors/temp"}))
	if r.Status != types.AssetStatusAbnormal || r.ErrorMsg != "address is required" {
		t.Fatalf("missing address: status=%v error=%q", r.Status, r.ErrorMsg)
	}

	r = runProbe(t, e, marshalConfig(t, config{Address: "127.0.0.1:1883"}))
	if r.Status != types.AssetStatusAbnormal || r.ErrorMsg != "topic is required" {
		t.Fatalf("missing topic: status=%v error=%q", r.Status, r.ErrorMsg)
	}
}

func TestProbeDialFailure(t *testing.T) {
	// Reserve a port and close the listener so dialing it is refused.
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	e := New(2 * time.Second)
	r := runProbe(t, e, marshalConfig(t, config{Address: addr, Topic: "t"}))
	if r.Status != types.AssetStatusAbnormal {
		t.Fatalf("dial failure: status=%v, want abnormal", r.Status)
	}
	if !strings.Contains(r.ErrorMsg, "mqtt dial failed") {
		t.Fatalf("dial failure: error=%q, want dial failure message", r.ErrorMsg)
	}
}

func TestProbeMessageReceivedAndPayloadMatch(t *testing.T) {
	srv := newStubServer(t, "sensors/temp", []byte("temp=21.5 zone=a"), true)
	e := New(5 * time.Second)

	cfg := marshalConfig(t, config{
		Address:       srv.addr(),
		Topic:         "sensors/temp",
		ExpectPayload: "zone=a",
		WaitSeconds:   3,
	})
	r := runProbe(t, e, cfg)
	if r.Status != types.AssetStatusNormal {
		t.Fatalf("success case: status=%v error=%q", r.Status, r.ErrorMsg)
	}
	if r.Metrics["connect_ms"] < 0 {
		t.Fatalf("connect_ms = %v, want >= 0", r.Metrics["connect_ms"])
	}
	if r.Metrics["message_received"] != 1 {
		t.Fatalf("message_received = %v, want 1", r.Metrics["message_received"])
	}
	if r.Metrics["payload_match"] != 1 {
		t.Fatalf("payload_match = %v, want 1", r.Metrics["payload_match"])
	}
	if r.Body != "temp=21.5 zone=a" {
		t.Fatalf("Body = %q, want published payload", r.Body)
	}
}

func TestProbePayloadMismatch(t *testing.T) {
	srv := newStubServer(t, "sensors/temp", []byte("temp=21.5"), true)
	e := New(5 * time.Second)

	cfg := marshalConfig(t, config{
		Address:       srv.addr(),
		Topic:         "sensors/temp",
		ExpectPayload: "zone=a",
		WaitSeconds:   3,
	})
	r := runProbe(t, e, cfg)
	if r.Status != types.AssetStatusAbnormal {
		t.Fatalf("mismatch case: status=%v, want abnormal", r.Status)
	}
	if r.Metrics["message_received"] != 1 {
		t.Fatalf("message_received = %v, want 1", r.Metrics["message_received"])
	}
	if r.Metrics["payload_match"] != 0 {
		t.Fatalf("payload_match = %v, want 0", r.Metrics["payload_match"])
	}
	if !strings.Contains(r.ErrorMsg, "payload mismatch") {
		t.Fatalf("mismatch case: error=%q", r.ErrorMsg)
	}
}

func TestProbeNoMessage(t *testing.T) {
	srv := newStubServer(t, "sensors/temp", nil, false)
	e := New(5 * time.Second)

	cfg := marshalConfig(t, config{Address: srv.addr(), Topic: "sensors/temp", WaitSeconds: 1})
	r := runProbe(t, e, cfg)
	if r.Status != types.AssetStatusAbnormal {
		t.Fatalf("no-message case: status=%v, want abnormal", r.Status)
	}
	if r.Metrics["message_received"] != 0 {
		t.Fatalf("message_received = %v, want 0", r.Metrics["message_received"])
	}
	if r.Metrics["connect_ms"] <= 0 && r.Metrics["connect_ms"] != 0 {
		t.Fatalf("connect_ms = %v, want present", r.Metrics["connect_ms"])
	}
	if !strings.Contains(r.ErrorMsg, "no message received") {
		t.Fatalf("no-message case: error=%q", r.ErrorMsg)
	}
}

func TestParseAddress(t *testing.T) {
	for _, tc := range []struct {
		in      string
		scheme  string
		host    string
		wantErr bool
	}{
		{in: "127.0.0.1:1883", scheme: "tcp", host: "127.0.0.1:1883"},
		{in: "tcp://127.0.0.1:1883", scheme: "tcp", host: "127.0.0.1:1883"},
		{in: "mqtt://broker.local:1883", scheme: "mqtt", host: "broker.local:1883"},
		{in: "mqtts://broker.local:8883", scheme: "mqtts", host: "broker.local:8883"},
		{in: "ssl://broker.local:8883", scheme: "ssl", host: "broker.local:8883"},
		{in: "http://bad scheme", wantErr: true},
	} {
		u, err := parseAddress(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("parseAddress(%q): expected error", tc.in)
			}
			continue
		}
		if err != nil {
			t.Fatalf("parseAddress(%q): %v", tc.in, err)
		}
		if u.Scheme != tc.scheme || u.Host != tc.host {
			t.Fatalf("parseAddress(%q) = %s/%s, want %s/%s", tc.in, u.Scheme, u.Host, tc.scheme, tc.host)
		}
	}
}

func TestClientIDGeneration(t *testing.T) {
	if got := clientID("fixed"); got != "fixed" {
		t.Fatalf("clientID(fixed) = %q, want fixed", got)
	}
	first := clientID("")
	second := clientID("")
	if first == "" || second == "" {
		t.Fatal("generated client id is empty")
	}
	if first == second {
		t.Fatalf("generated client ids collide: %q", first)
	}
	if !strings.HasPrefix(first, "tickraft-probe-") {
		t.Fatalf("generated client id %q lacks probe prefix", first)
	}
}
