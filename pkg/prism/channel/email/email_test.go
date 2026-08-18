// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package email

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/smtp"
	"net/textproto"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/i18n"
	"github.com/tickraft/tickraft/pkg/prism/alert"
	"github.com/tickraft/tickraft/pkg/prism/alert/template"
	"github.com/tickraft/tickraft/pkg/prism/channel"
)

// ---------------------------------------------------------------------------
// Mock SMTP server
// ---------------------------------------------------------------------------

// mockServer is a minimal SMTP server for testing. It supports plaintext,
// implicit TLS, and STARTTLS connections; PLAIN, LOGIN, and CRAM-MD5 auth;
// and records the last received message for assertions. The first
// failFirst connections are closed immediately to simulate transient
// failures for retry testing.
type mockServer struct {
	listener        net.Listener
	ctx             context.Context
	tlsConfig       *tls.Config
	supportStartTLS bool
	implicitTLS     bool

	mu        sync.Mutex
	closed    bool
	connCount int
	failFirst int
	lastFrom  string
	lastTo    []string
	lastData  []byte
	authMech  string
	authUser  string
	authPass  string
}

// newMockServer creates and starts a mock SMTP server. If tlsConfig is
// non-nil and implicitTLS is true, the server listens with TLS. If
// tlsConfig is non-nil and supportStartTLS is true, the server advertises
// and supports STARTTLS upgrade. The server is closed automatically on test
// cleanup.
func newMockServer(t *testing.T, tlsConfig *tls.Config, implicitTLS, supportStartTLS bool) *mockServer {
	t.Helper()
	var ln net.Listener
	var err error
	if implicitTLS && tlsConfig != nil {
		ln, err = tls.Listen("tcp", "127.0.0.1:0", tlsConfig)
	} else {
		var lc net.ListenConfig
		ln, err = lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	}
	if err != nil {
		t.Fatalf("mock server listen: %v", err)
	}
	s := &mockServer{
		listener:        ln,
		ctx:             t.Context(),
		tlsConfig:       tlsConfig,
		supportStartTLS: supportStartTLS,
		implicitTLS:     implicitTLS,
	}
	go s.serve()
	t.Cleanup(s.close)
	return s
}

func (s *mockServer) serve() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handle(conn)
	}
}

func (s *mockServer) close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.mu.Unlock()
	_ = s.listener.Close()
}

func (s *mockServer) host() string {
	host, _, _ := net.SplitHostPort(s.listener.Addr().String())
	return host
}

func (s *mockServer) port() int {
	_, portStr, _ := net.SplitHostPort(s.listener.Addr().String())
	port := 0
	_, _ = fmt.Sscanf(portStr, "%d", &port)
	return port
}

func (s *mockServer) setFailFirst(n int) {
	s.mu.Lock()
	s.failFirst = n
	s.mu.Unlock()
}

func (s *mockServer) getLastData() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastData
}

func (s *mockServer) getLastFrom() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastFrom
}

func (s *mockServer) getLastTo() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.lastTo))
	copy(out, s.lastTo)
	return out
}

func (s *mockServer) getAuthInfo() (mech, user, pass string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.authMech, s.authUser, s.authPass
}

func (s *mockServer) handle(conn net.Conn) {
	defer conn.Close()

	s.mu.Lock()
	connNum := s.connCount
	s.connCount++
	failThis := connNum < s.failFirst
	s.mu.Unlock()

	if failThis {
		// Close immediately to simulate a transient connection failure.
		return
	}

	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)

	send := func(format string, args ...any) {
		fmt.Fprintf(writer, format, args...)
		_ = writer.Flush()
	}

	send("220 mock ESMTP ready\r\n")

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		upper := strings.ToUpper(line)

		switch {
		case strings.HasPrefix(upper, "EHLO"), strings.HasPrefix(upper, "HELO"):
			s.writeExtensions(send)

		case strings.HasPrefix(upper, "STARTTLS"):
			send("220 Ready to start TLS\r\n")
			tlsConn := tls.Server(conn, s.tlsConfig)
			if err := tlsConn.HandshakeContext(s.ctx); err != nil {
				return
			}
			conn = tlsConn
			reader = bufio.NewReader(conn)
			writer = bufio.NewWriter(conn)

		case strings.HasPrefix(upper, "AUTH"):
			s.handleAuth(line, reader, send)

		case strings.HasPrefix(upper, "MAIL FROM:"):
			from := extractAddr(line, "MAIL FROM:")
			s.mu.Lock()
			s.lastFrom = from
			s.mu.Unlock()
			send("250 OK\r\n")

		case strings.HasPrefix(upper, "RCPT TO:"):
			to := extractAddr(line, "RCPT TO:")
			s.mu.Lock()
			s.lastTo = append(s.lastTo, to)
			s.mu.Unlock()
			send("250 OK\r\n")

		case strings.HasPrefix(upper, "DATA"):
			send("354 End data with <CR><LF>.<CR><LF>\r\n")
			data := s.readData(reader)
			s.mu.Lock()
			s.lastData = data
			s.mu.Unlock()
			send("250 OK message accepted\r\n")

		case strings.HasPrefix(upper, "QUIT"):
			send("221 Bye\r\n")
			return

		case strings.HasPrefix(upper, "RSET"):
			s.mu.Lock()
			s.lastFrom = ""
			s.lastTo = nil
			s.mu.Unlock()
			send("250 OK\r\n")

		case strings.HasPrefix(upper, "NOOP"):
			send("250 OK\r\n")

		default:
			send("500 Unrecognized command\r\n")
		}
	}
}

func (s *mockServer) writeExtensions(send func(string, ...any)) {
	exts := []string{"mock ESMTP", "AUTH PLAIN LOGIN CRAM-MD5"}
	if s.supportStartTLS && s.tlsConfig != nil {
		exts = append(exts, "STARTTLS")
	}
	for i, ext := range exts {
		sep := "-"
		if i == len(exts)-1 {
			sep = " "
		}
		send("250%s%s\r\n", sep, ext)
	}
}

func (s *mockServer) handleAuth(line string, reader *bufio.Reader, send func(string, ...any)) {
	parts := strings.Fields(line)
	if len(parts) < 2 {
		send("501 Syntax error in parameters\r\n")
		return
	}
	mech := strings.ToUpper(parts[1])
	s.mu.Lock()
	s.authMech = mech
	s.mu.Unlock()

	switch mech {
	case "PLAIN":
		if len(parts) >= 3 {
			decoded, err := base64.StdEncoding.DecodeString(parts[2])
			if err == nil {
				fields := bytes.Split(decoded, []byte{0})
				if len(fields) >= 3 {
					s.mu.Lock()
					s.authUser = string(fields[1])
					s.authPass = string(fields[2])
					s.mu.Unlock()
				}
			}
		}
		send("235 OK authenticated\r\n")

	case "LOGIN":
		if len(parts) >= 3 {
			// Initial response provided (username).
			decoded, _ := base64.StdEncoding.DecodeString(parts[2])
			s.mu.Lock()
			s.authUser = string(decoded)
			s.mu.Unlock()
			send("334 %s\r\n", base64.StdEncoding.EncodeToString([]byte("Password:")))
			resp, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			decoded2, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(resp))
			s.mu.Lock()
			s.authPass = string(decoded2)
			s.mu.Unlock()
			send("235 OK authenticated\r\n")
		} else {
			// No initial response; prompt for username then password.
			send("334 %s\r\n", base64.StdEncoding.EncodeToString([]byte("Username:")))
			resp, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			decoded, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(resp))
			s.mu.Lock()
			s.authUser = string(decoded)
			s.mu.Unlock()
			send("334 %s\r\n", base64.StdEncoding.EncodeToString([]byte("Password:")))
			resp, err = reader.ReadString('\n')
			if err != nil {
				return
			}
			decoded2, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(resp))
			s.mu.Lock()
			s.authPass = string(decoded2)
			s.mu.Unlock()
			send("235 OK authenticated\r\n")
		}

	case "CRAM-MD5":
		challenge := "<test-challenge@mock>"
		send("334 %s\r\n", base64.StdEncoding.EncodeToString([]byte(challenge)))
		resp, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		decoded, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(resp))
		fields := strings.SplitN(string(decoded), " ", 2)
		if len(fields) >= 1 {
			s.mu.Lock()
			s.authUser = fields[0]
			s.mu.Unlock()
		}
		send("235 OK authenticated\r\n")

	default:
		send("504 Unrecognized authentication type\r\n")
	}
}

// readData reads the DATA payload until the lone dot terminator.
func (s *mockServer) readData(reader *bufio.Reader) []byte {
	var buf bytes.Buffer
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return buf.Bytes()
		}
		trimmed := strings.TrimRight(line, "\r\n")
		if trimmed == "." {
			return buf.Bytes()
		}
		if strings.HasPrefix(trimmed, "..") {
			trimmed = trimmed[1:]
		}
		buf.WriteString(trimmed)
		buf.WriteString("\r\n")
	}
}

// extractAddr extracts the email address from a MAIL FROM or RCPT TO command.
func extractAddr(line, prefix string) string {
	addr := strings.TrimSpace(line[len(prefix):])
	addr = strings.Trim(addr, "<>")
	return addr
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// generateTestCert creates a self-signed ECDSA certificate for 127.0.0.1.
func generateTestCert(t *testing.T) tls.Certificate {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate ECDSA key: %v", err)
	}
	certTmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:     []string{"127.0.0.1", "localhost"},
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, &certTmpl, &certTmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: priv}
}

// testAlert returns a sample metric alert event for testing.
func testAlert() alert.Event {
	return alert.Event{
		Type:      alert.TypeMetric,
		AssetID:   42,
		TenantID:  1,
		Timestamp: time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC),
		Violations: []alert.Violation{{
			Kind: alert.ViolationKindMetric,
			Metric: &alert.MetricContext{
				Name:      "cpu_usage",
				Value:     95.5,
				Threshold: 80.0,
				Metrics:   map[string]float64{"memory": 70.2},
			},
		}},
	}
}

// testLogAlert returns a sample log alert event for testing.
func testLogAlert() alert.Event {
	return alert.Event{
		Type:      alert.TypeLog,
		AssetID:   7,
		TenantID:  2,
		Timestamp: time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC),
		Violations: []alert.Violation{{
			Kind:     alert.ViolationKindLog,
			Severity: "ERROR",
			Log:      &alert.LogContext{Keyword: "panic", Content: "nil pointer dereference"},
			Source:   "10.0.0.1",
		}},
	}
}

// insecureTLSConfig returns a TLS config that skips certificate verification,
// suitable for testing with self-signed certificates.
func insecureTLSConfig() *tls.Config {
	return &tls.Config{InsecureSkipVerify: true}
}

// channelOpts builds common channel options for tests pointing at the mock
// server with fast retry settings.
func channelOpts(s *mockServer, extra ...Option) []Option {
	opts := make([]Option, 0, 6+len(extra))
	opts = append(opts,
		WithHost(s.host()),
		WithPort(s.port()),
		WithFrom("alert@tickraft.dev"),
		WithTo("ops@tickraft.dev"),
		WithRetry(3, time.Millisecond),
		WithCircuitBreaker(5, 30*time.Second),
	)
	opts = append(opts, extra...)
	return opts
}

// ---------------------------------------------------------------------------
// Config validation tests
// ---------------------------------------------------------------------------

func TestValidate(t *testing.T) {
	validConfig := Config{
		Host:     "smtp.example.com",
		Port:     587,
		From:     "alert@example.com",
		To:       []string{"ops@example.com"},
		TLSMode:  TLSModeStartTLS,
		AuthType: AuthTypePlain,
	}

	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{
			name:    "valid config",
			mutate:  func(c *Config) {},
			wantErr: "",
		},
		{
			name:    "host empty",
			mutate:  func(c *Config) { c.Host = "" },
			wantErr: "host is required",
		},
		{
			name:    "host whitespace only",
			mutate:  func(c *Config) { c.Host = "   " },
			wantErr: "host is required",
		},
		{
			name:    "port zero",
			mutate:  func(c *Config) { c.Port = 0 },
			wantErr: "port must be between 1 and 65535",
		},
		{
			name:    "port negative",
			mutate:  func(c *Config) { c.Port = -1 },
			wantErr: "port must be between 1 and 65535",
		},
		{
			name:    "port too large",
			mutate:  func(c *Config) { c.Port = 70000 },
			wantErr: "port must be between 1 and 65535",
		},
		{
			name:    "from empty",
			mutate:  func(c *Config) { c.From = "" },
			wantErr: "from address is required",
		},
		{
			name:    "from invalid email",
			mutate:  func(c *Config) { c.From = "not-an-email" },
			wantErr: "invalid from address",
		},
		{
			name:    "to empty slice",
			mutate:  func(c *Config) { c.To = nil },
			wantErr: "at least one recipient",
		},
		{
			name:    "to invalid email",
			mutate:  func(c *Config) { c.To = []string{"bad-email"} },
			wantErr: "invalid to address",
		},
		{
			name:    "password without username",
			mutate:  func(c *Config) { c.Username = ""; c.Password = "secret" },
			wantErr: "password set without username",
		},
		{
			name:    "username without password",
			mutate:  func(c *Config) { c.Username = "user"; c.Password = "" },
			wantErr: "username set without password",
		},
		{
			name:    "no auth with empty credentials",
			mutate:  func(c *Config) { c.Username = ""; c.Password = "" },
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig
			tt.mutate(&cfg)
			err := cfg.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("Validate(): unexpected error: %v", err)
				}
			} else {
				if err == nil {
					t.Errorf("Validate(): expected error containing %q, got nil", tt.wantErr)
				} else if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("Validate(): error = %q, want substring %q", err.Error(), tt.wantErr)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// New constructor tests
// ---------------------------------------------------------------------------

func TestNewDefaults(t *testing.T) {
	cfg := Config{
		Host: "smtp.example.com",
		Port: 0, // should default to 25
		From: "alert@example.com",
		To:   []string{"ops@example.com"},
	}
	ch, err := New(cfg)
	if err != nil {
		t.Fatalf("New(): unexpected error: %v", err)
	}
	if ch == nil {
		t.Fatal("New(): returned nil channel")
	}
	if ch.config.Port != 25 {
		t.Errorf("Port: got %d, want 25", ch.config.Port)
	}
	if ch.config.RetryMaxAttempts != 3 {
		t.Errorf("RetryMaxAttempts: got %d, want 3", ch.config.RetryMaxAttempts)
	}
	if ch.config.RetryBaseInterval != time.Second {
		t.Errorf("RetryBaseInterval: got %v, want 1s", ch.config.RetryBaseInterval)
	}
	if ch.config.CircuitFailureThreshold != 5 {
		t.Errorf("CircuitFailureThreshold: got %d, want 5", ch.config.CircuitFailureThreshold)
	}
	if ch.config.CircuitCooldown != 30*time.Second {
		t.Errorf("CircuitCooldown: got %v, want 30s", ch.config.CircuitCooldown)
	}
	if ch.Name() != "email" {
		t.Errorf("Name(): got %q, want %q", ch.Name(), "email")
	}
}

func TestNewWithOptions(t *testing.T) {
	ch, err := New(
		Config{
			From: "alert@example.com",
			To:   []string{"ops@example.com"},
		},
		WithHost("smtp.example.com"),
		WithPort(465),
		WithCredentials("user", "pass"),
		WithTLSMode(TLSModeImplicit),
		WithAuthType(AuthTypeLogin),
		WithHTMLMode(true),
		WithRetry(5, 2*time.Second),
		WithCircuitBreaker(10, 60*time.Second),
	)
	if err != nil {
		t.Fatalf("New(): unexpected error: %v", err)
	}
	if ch.config.Host != "smtp.example.com" {
		t.Errorf("Host: got %q", ch.config.Host)
	}
	if ch.config.Port != 465 {
		t.Errorf("Port: got %d, want 465", ch.config.Port)
	}
	if ch.config.Username != "user" || ch.config.Password != "pass" {
		t.Errorf("Credentials: got %q/%q", ch.config.Username, ch.config.Password)
	}
	if ch.config.TLSMode != TLSModeImplicit {
		t.Errorf("TLSMode: got %s", ch.config.TLSMode)
	}
	if ch.config.AuthType != AuthTypeLogin {
		t.Errorf("AuthType: got %s", ch.config.AuthType)
	}
	if !ch.config.HTMLMode {
		t.Errorf("HTMLMode: got false, want true")
	}
	if ch.config.RetryMaxAttempts != 5 {
		t.Errorf("RetryMaxAttempts: got %d, want 5", ch.config.RetryMaxAttempts)
	}
	if ch.config.RetryBaseInterval != 2*time.Second {
		t.Errorf("RetryBaseInterval: got %v, want 2s", ch.config.RetryBaseInterval)
	}
	if ch.config.CircuitFailureThreshold != 10 {
		t.Errorf("CircuitFailureThreshold: got %d, want 10", ch.config.CircuitFailureThreshold)
	}
	if ch.config.CircuitCooldown != 60*time.Second {
		t.Errorf("CircuitCooldown: got %v, want 60s", ch.config.CircuitCooldown)
	}
}

func TestNewValidationFailure(t *testing.T) {
	_, err := New(Config{})
	if err == nil {
		t.Fatal("New(Config{}): expected error, got nil")
	}
	if !strings.Contains(err.Error(), "host is required") {
		t.Errorf("New(Config{}): error = %q, want 'host is required'", err.Error())
	}
}

func TestNewCopiesRecipientSlice(t *testing.T) {
	original := []string{"ops@example.com"}
	cfg := Config{
		Host: "smtp.example.com",
		Port: 25,
		From: "alert@example.com",
		To:   original,
	}
	ch, err := New(cfg)
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	// Mutate the original slice; the channel's copy should be unaffected.
	original[0] = "mutated@example.com"
	if ch.config.To[0] != "ops@example.com" {
		t.Errorf("recipient slice was not copied: got %q", ch.config.To[0])
	}
}

// ---------------------------------------------------------------------------
// Send: plaintext email success
// ---------------------------------------------------------------------------

func TestSendPlainText(t *testing.T) {
	srv := newMockServer(t, nil, false, false)
	ch, err := New(Config{}, channelOpts(srv)...)
	if err != nil {
		t.Fatalf("New(): %v", err)
	}

	if err := ch.Send(context.Background(), testAlert()); err != nil {
		t.Fatalf("Send(): unexpected error: %v", err)
	}

	if got := srv.getLastFrom(); got != "alert@tickraft.dev" {
		t.Errorf("From: got %q, want %q", got, "alert@tickraft.dev")
	}
	if got := srv.getLastTo(); len(got) != 1 || got[0] != "ops@tickraft.dev" {
		t.Errorf("To: got %v, want [ops@tickraft.dev]", got)
	}

	data := string(srv.getLastData())
	if !strings.Contains(data, "Content-Type: text/plain; charset=UTF-8") {
		t.Errorf("data missing text/plain content type")
	}
	if !strings.Contains(data, "Alert type: metric") {
		t.Errorf("data missing alert type field")
	}
	if !strings.Contains(data, "Asset ID: 42") {
		t.Errorf("data missing asset ID field")
	}
	if !strings.Contains(data, "cpu_usage") {
		t.Errorf("data missing metric name")
	}
}

// ---------------------------------------------------------------------------
// Send: HTML email success
// ---------------------------------------------------------------------------

func TestSendHTML(t *testing.T) {
	srv := newMockServer(t, nil, false, false)
	ch, err := New(Config{}, channelOpts(srv, WithHTMLMode(true))...)
	if err != nil {
		t.Fatalf("New(): %v", err)
	}

	if err := ch.Send(context.Background(), testAlert()); err != nil {
		t.Fatalf("Send(): unexpected error: %v", err)
	}

	data := string(srv.getLastData())
	if !strings.Contains(data, "Content-Type: multipart/alternative") {
		t.Errorf("data missing multipart/alternative content type")
	}
	if !strings.Contains(data, "text/plain; charset=UTF-8") {
		t.Errorf("data missing plain text part")
	}
	if !strings.Contains(data, "text/html; charset=UTF-8") {
		t.Errorf("data missing HTML part")
	}
	if !strings.Contains(data, "<table") {
		t.Errorf("data missing HTML table")
	}
}

// ---------------------------------------------------------------------------
// Send: implicit TLS success
// ---------------------------------------------------------------------------

func TestSendImplicitTLS(t *testing.T) {
	cert := generateTestCert(t)
	tlsCfg := &tls.Config{Certificates: []tls.Certificate{cert}}
	srv := newMockServer(t, tlsCfg, true, false)
	ch, err := New(Config{}, channelOpts(srv,
		WithTLSMode(TLSModeImplicit),
		withTLSConfig(insecureTLSConfig()),
	)...)
	if err != nil {
		t.Fatalf("New(): %v", err)
	}

	if err := ch.Send(context.Background(), testAlert()); err != nil {
		t.Fatalf("Send(): unexpected error: %v", err)
	}

	if got := srv.getLastFrom(); got != "alert@tickraft.dev" {
		t.Errorf("From: got %q", got)
	}
	data := string(srv.getLastData())
	if !strings.Contains(data, "Alert type: metric") {
		t.Errorf("data missing alert type")
	}
}

// ---------------------------------------------------------------------------
// Send: STARTTLS success
// ---------------------------------------------------------------------------

func TestSendStartTLS(t *testing.T) {
	cert := generateTestCert(t)
	tlsCfg := &tls.Config{Certificates: []tls.Certificate{cert}}
	srv := newMockServer(t, tlsCfg, false, true)
	ch, err := New(Config{}, channelOpts(srv,
		WithTLSMode(TLSModeStartTLS),
		withTLSConfig(insecureTLSConfig()),
	)...)
	if err != nil {
		t.Fatalf("New(): %v", err)
	}

	if err := ch.Send(context.Background(), testAlert()); err != nil {
		t.Fatalf("Send(): unexpected error: %v", err)
	}

	data := string(srv.getLastData())
	if !strings.Contains(data, "Alert type: metric") {
		t.Errorf("data missing alert type")
	}
}

// ---------------------------------------------------------------------------
// Send: PLAIN auth success
// ---------------------------------------------------------------------------

func TestSendPlainAuth(t *testing.T) {
	srv := newMockServer(t, nil, false, false)
	ch, err := New(Config{}, channelOpts(srv,
		WithCredentials("testuser", "testpass"),
		WithAuthType(AuthTypePlain),
	)...)
	if err != nil {
		t.Fatalf("New(): %v", err)
	}

	if err := ch.Send(context.Background(), testAlert()); err != nil {
		t.Fatalf("Send(): unexpected error: %v", err)
	}

	mech, user, pass := srv.getAuthInfo()
	if mech != "PLAIN" {
		t.Errorf("auth mechanism: got %q, want PLAIN", mech)
	}
	if user != "testuser" {
		t.Errorf("auth user: got %q, want testuser", user)
	}
	if pass != "testpass" {
		t.Errorf("auth pass: got %q, want testpass", pass)
	}
}

// ---------------------------------------------------------------------------
// Send: LOGIN auth success
// ---------------------------------------------------------------------------

func TestSendLoginAuth(t *testing.T) {
	srv := newMockServer(t, nil, false, false)
	ch, err := New(Config{}, channelOpts(srv,
		WithCredentials("loginuser", "loginpass"),
		WithAuthType(AuthTypeLogin),
	)...)
	if err != nil {
		t.Fatalf("New(): %v", err)
	}

	if err := ch.Send(context.Background(), testAlert()); err != nil {
		t.Fatalf("Send(): unexpected error: %v", err)
	}

	mech, user, pass := srv.getAuthInfo()
	if mech != "LOGIN" {
		t.Errorf("auth mechanism: got %q, want LOGIN", mech)
	}
	if user != "loginuser" {
		t.Errorf("auth user: got %q, want loginuser", user)
	}
	if pass != "loginpass" {
		t.Errorf("auth pass: got %q, want loginpass", pass)
	}
}

// ---------------------------------------------------------------------------
// Send: CRAM-MD5 auth success
// ---------------------------------------------------------------------------

func TestSendCramMD5Auth(t *testing.T) {
	srv := newMockServer(t, nil, false, false)
	ch, err := New(Config{}, channelOpts(srv,
		WithCredentials("cramuser", "crampass"),
		WithAuthType(AuthTypeCramMD5),
	)...)
	if err != nil {
		t.Fatalf("New(): %v", err)
	}

	if err := ch.Send(context.Background(), testAlert()); err != nil {
		t.Fatalf("Send(): unexpected error: %v", err)
	}

	mech, user, _ := srv.getAuthInfo()
	if mech != "CRAM-MD5" {
		t.Errorf("auth mechanism: got %q, want CRAM-MD5", mech)
	}
	if user != "cramuser" {
		t.Errorf("auth user: got %q, want cramuser", user)
	}
}

// ---------------------------------------------------------------------------
// Send: no auth (TLSModeNone) success
// ---------------------------------------------------------------------------

func TestSendNoAuth(t *testing.T) {
	srv := newMockServer(t, nil, false, false)
	ch, err := New(Config{}, channelOpts(srv)...)
	if err != nil {
		t.Fatalf("New(): %v", err)
	}

	if err := ch.Send(context.Background(), testAlert()); err != nil {
		t.Fatalf("Send(): unexpected error: %v", err)
	}

	mech, _, _ := srv.getAuthInfo()
	if mech != "" {
		t.Errorf("auth mechanism: got %q, want empty (no auth)", mech)
	}
}

// ---------------------------------------------------------------------------
// Send: retry then success (first N connections fail)
// ---------------------------------------------------------------------------

func TestSendRetryThenSuccess(t *testing.T) {
	srv := newMockServer(t, nil, false, false)
	srv.setFailFirst(2)
	ch, err := New(Config{}, channelOpts(srv, WithRetry(3, time.Millisecond))...)
	if err != nil {
		t.Fatalf("New(): %v", err)
	}

	if err := ch.Send(context.Background(), testAlert()); err != nil {
		t.Fatalf("Send(): expected success after retry, got: %v", err)
	}

	data := string(srv.getLastData())
	if !strings.Contains(data, "Alert type: metric") {
		t.Errorf("data missing alert type after retry")
	}
}

// ---------------------------------------------------------------------------
// Send: retry exhausted failure
// ---------------------------------------------------------------------------

func TestSendRetryExhausted(t *testing.T) {
	srv := newMockServer(t, nil, false, false)
	srv.setFailFirst(100)
	ch, err := New(Config{}, channelOpts(srv, WithRetry(3, time.Millisecond))...)
	if err != nil {
		t.Fatalf("New(): %v", err)
	}

	err = ch.Send(context.Background(), testAlert())
	if err == nil {
		t.Fatal("Send(): expected error, got nil")
	}

	var se *channel.SendError
	if !errors.As(err, &se) {
		t.Fatalf("Send(): expected *SendError, got %T: %v", err, err)
	}
	if !se.Retryable {
		t.Errorf("SendError.Retryable: got false, want true")
	}
	if se.ChannelName != "email" {
		t.Errorf("SendError.ChannelName: got %q, want %q", se.ChannelName, "email")
	}
}

// ---------------------------------------------------------------------------
// Send: circuit breaker open returns ErrCircuitOpen
// ---------------------------------------------------------------------------

func TestSendCircuitOpen(t *testing.T) {
	srv := newMockServer(t, nil, false, false)
	srv.setFailFirst(100)
	ch, err := New(Config{}, channelOpts(srv,
		WithRetry(1, time.Millisecond),
		WithCircuitBreaker(1, 30*time.Second),
	)...)
	if err != nil {
		t.Fatalf("New(): %v", err)
	}

	// First send: fails and opens the breaker (threshold=1, no retry).
	firstErr := ch.Send(context.Background(), testAlert())
	if firstErr == nil {
		t.Fatal("first Send(): expected error, got nil")
	}

	// Second send: breaker is open.
	secondErr := ch.Send(context.Background(), testAlert())
	if !errors.Is(secondErr, channel.ErrCircuitOpen) {
		t.Errorf("second Send(): expected ErrCircuitOpen, got %v", secondErr)
	}
}

// ---------------------------------------------------------------------------
// Send: context cancellation
// ---------------------------------------------------------------------------

func TestSendContextCancelled(t *testing.T) {
	srv := newMockServer(t, nil, false, false)
	srv.setFailFirst(100)
	ch, err := New(Config{}, channelOpts(srv, WithRetry(3, 5*time.Second))...)
	if err != nil {
		t.Fatalf("New(): %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err = ch.Send(ctx, testAlert())
	if err == nil {
		t.Fatal("Send(): expected error, got nil")
	}
}

// ---------------------------------------------------------------------------
// Send: log alert formatting
// ---------------------------------------------------------------------------

func TestSendLogAlert(t *testing.T) {
	srv := newMockServer(t, nil, false, false)
	ch, err := New(Config{}, channelOpts(srv)...)
	if err != nil {
		t.Fatalf("New(): %v", err)
	}

	if err := ch.Send(context.Background(), testLogAlert()); err != nil {
		t.Fatalf("Send(): unexpected error: %v", err)
	}

	data := string(srv.getLastData())
	if !strings.Contains(data, "Alert type: log") {
		t.Errorf("data missing log alert type")
	}
	if !strings.Contains(data, "panic") {
		t.Errorf("data missing keyword")
	}
	if !strings.Contains(data, "nil pointer dereference") {
		t.Errorf("data missing content")
	}
	if !strings.Contains(data, "10.0.0.1") {
		t.Errorf("data missing source IP")
	}
}

// ---------------------------------------------------------------------------
// loginAuth unit tests
// ---------------------------------------------------------------------------

func TestLoginAuth(t *testing.T) {
	a := &loginAuth{username: "user", password: "pass"}

	mech, resp, err := a.Start(&smtp.ServerInfo{Name: "localhost"})
	if err != nil {
		t.Fatalf("Start(): unexpected error: %v", err)
	}
	if mech != "LOGIN" {
		t.Errorf("Start() mech: got %q, want LOGIN", mech)
	}
	if resp != nil {
		t.Errorf("Start() resp: got %v, want nil", resp)
	}

	// First Next: should return username.
	resp, err = a.Next(nil, true)
	if err != nil {
		t.Fatalf("Next() #1: unexpected error: %v", err)
	}
	if string(resp) != "user" {
		t.Errorf("Next() #1: got %q, want %q", string(resp), "user")
	}

	// Second Next: should return password.
	resp, err = a.Next(nil, true)
	if err != nil {
		t.Fatalf("Next() #2: unexpected error: %v", err)
	}
	if string(resp) != "pass" {
		t.Errorf("Next() #2: got %q, want %q", string(resp), "pass")
	}

	// Third Next: should error.
	_, err = a.Next(nil, true)
	if err == nil {
		t.Fatal("Next() #3: expected error, got nil")
	}
}

func TestLoginAuthNextNoMore(t *testing.T) {
	a := &loginAuth{username: "user", password: "pass"}
	resp, err := a.Next(nil, false)
	if err != nil {
		t.Fatalf("Next(more=false): unexpected error: %v", err)
	}
	if resp != nil {
		t.Errorf("Next(more=false): got %v, want nil", resp)
	}
}

// ---------------------------------------------------------------------------
// isRetryableErr unit tests
// ---------------------------------------------------------------------------

func TestIsRetryableErr(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"context cancelled", context.Canceled, false},
		{"context deadline", context.DeadlineExceeded, false},
		{"io.EOF", io.EOF, true},
		{"network error", &net.OpError{Op: "dial", Err: errors.New("refused")}, true},
		{"textproto 4xx", &textproto.Error{Code: 450}, true},
		{"textproto 5xx", &textproto.Error{Code: 535}, false},
		{"unencrypted connection", errors.New("smtp: unencrypted connection"), false},
		{"unknown error", errors.New("something went wrong"), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isRetryableErr(tt.err); got != tt.want {
				t.Errorf("isRetryableErr(%v): got %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Formatting unit tests
// ---------------------------------------------------------------------------

func TestFormatSubject(t *testing.T) {
	tests := []struct {
		name  string
		alert alert.Event
		want  string
	}{
		{
			name:  "metric with name",
			alert: testAlert(),
			want:  "[Alert] cpu_usage 95.50 (threshold 80.00) - asset 42",
		},
		{
			name: "metric without name",
			alert: alert.Event{
				Type:    alert.TypeMetric,
				AssetID: 1,
			},
			want: "[Alert] metric alert - asset 1",
		},
		{
			name:  "log with keyword",
			alert: testLogAlert(),
			want:  `[Alert] log keyword "panic" matched - asset 7`,
		},
		{
			name: "log without keyword",
			alert: alert.Event{
				Type:    alert.TypeLog,
				AssetID: 3,
			},
			want: "[Alert] log alert - asset 3",
		},
		{
			name: "unknown type",
			alert: alert.Event{
				Type:    "custom",
				AssetID: 9,
			},
			want: "[Alert] custom - asset 9",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatSubject(tt.alert)
			if got != tt.want {
				t.Errorf("formatSubject(): got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatPlainText(t *testing.T) {
	text := formatPlainText(testAlert(), i18n.FormattedMessage{})
	if !strings.Contains(text, "Alert type: metric") {
		t.Errorf("missing alert type")
	}
	if !strings.Contains(text, "cpu_usage") {
		t.Errorf("missing metric name")
	}
	if !strings.Contains(text, "memory: 70.20") {
		t.Errorf("missing resources")
	}

	logText := formatPlainText(testLogAlert(), i18n.FormattedMessage{})
	if !strings.Contains(logText, "Alert type: log") {
		t.Errorf("missing log alert type")
	}
	if !strings.Contains(logText, "Source IP: 10.0.0.1") {
		t.Errorf("missing source IP")
	}
}

func TestFormatHTML(t *testing.T) {
	html := formatHTML(testAlert(), i18n.FormattedMessage{})
	if !strings.Contains(html, "<table") {
		t.Errorf("missing table element")
	}
	if !strings.Contains(html, "cpu_usage") {
		t.Errorf("missing metric name")
	}
	if !strings.Contains(html, "</table>") {
		t.Errorf("missing closing table tag")
	}
}

func TestEscapeHTML(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"plain", "plain"},
		{"<b>bold</b>", "&lt;b&gt;bold&lt;/b&gt;"},
		{`"quoted"`, "&quot;quoted&quot;"},
		{"a & b", "a &amp; b"},
	}
	for _, tt := range tests {
		if got := escapeHTML(tt.input); got != tt.want {
			t.Errorf("escapeHTML(%q): got %q, want %q", tt.input, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// TLSMode and AuthType String() tests
// ---------------------------------------------------------------------------

func TestTLSModeString(t *testing.T) {
	tests := []struct {
		mode TLSMode
		want string
	}{
		{TLSModeNone, "none"},
		{TLSModeImplicit, "implicit"},
		{TLSModeStartTLS, "starttls"},
		{TLSMode(99), "unknown(99)"},
	}
	for _, tt := range tests {
		if got := tt.mode.String(); got != tt.want {
			t.Errorf("TLSMode(%d).String(): got %q, want %q", int(tt.mode), got, tt.want)
		}
	}
}

func TestAuthTypeString(t *testing.T) {
	tests := []struct {
		authType AuthType
		want     string
	}{
		{AuthTypePlain, "plain"},
		{AuthTypeLogin, "login"},
		{AuthTypeCramMD5, "cram-md5"},
		{AuthType(99), "unknown(99)"},
	}
	for _, tt := range tests {
		if got := tt.authType.String(); got != tt.want {
			t.Errorf("AuthType(%d).String(): got %q, want %q", int(tt.authType), got, tt.want)
		}
	}
}

func TestTLSModeValid(t *testing.T) {
	if !TLSModeNone.valid() {
		t.Errorf("TLSModeNone.valid(): got false, want true")
	}
	if !TLSModeImplicit.valid() {
		t.Errorf("TLSModeImplicit.valid(): got false, want true")
	}
	if !TLSModeStartTLS.valid() {
		t.Errorf("TLSModeStartTLS.valid(): got false, want true")
	}
	if TLSMode(99).valid() {
		t.Errorf("TLSMode(99).valid(): got true, want false")
	}
}

func TestAuthTypeValid(t *testing.T) {
	if !AuthTypePlain.valid() {
		t.Errorf("AuthTypePlain.valid(): got false, want true")
	}
	if !AuthTypeLogin.valid() {
		t.Errorf("AuthTypeLogin.valid(): got false, want true")
	}
	if !AuthTypeCramMD5.valid() {
		t.Errorf("AuthTypeCramMD5.valid(): got false, want true")
	}
	if AuthType(99).valid() {
		t.Errorf("AuthType(99).valid(): got true, want false")
	}
}

// ---------------------------------------------------------------------------
// buildMessage tests
// ---------------------------------------------------------------------------

func TestBuildMessagePlainText(t *testing.T) {
	cfg := Config{
		From: "alert@example.com",
		To:   []string{"ops@example.com"},
	}
	msg := buildMessage(context.Background(), testAlert(), cfg, zap.NewNop())
	s := string(msg)

	if !strings.Contains(s, "From: alert@example.com\r\n") {
		t.Errorf("missing From header")
	}
	if !strings.Contains(s, "To: ops@example.com\r\n") {
		t.Errorf("missing To header")
	}
	if !strings.Contains(s, "Subject:") {
		t.Errorf("missing Subject header")
	}
	if !strings.Contains(s, "MIME-Version: 1.0\r\n") {
		t.Errorf("missing MIME-Version header")
	}
	if !strings.Contains(s, "Content-Type: text/plain; charset=UTF-8\r\n") {
		t.Errorf("missing plain text content type")
	}
	if strings.Contains(s, "multipart/alternative") {
		t.Errorf("should not contain multipart in plain text mode")
	}
}

func TestBuildMessageHTML(t *testing.T) {
	cfg := Config{
		From:     "alert@example.com",
		To:       []string{"ops@example.com"},
		HTMLMode: true,
	}
	msg := buildMessage(context.Background(), testAlert(), cfg, zap.NewNop())
	s := string(msg)

	if !strings.Contains(s, "Content-Type: multipart/alternative") {
		t.Errorf("missing multipart/alternative content type")
	}
	if !strings.Contains(s, "text/plain; charset=UTF-8") {
		t.Errorf("missing plain text part in multipart")
	}
	if !strings.Contains(s, "text/html; charset=UTF-8") {
		t.Errorf("missing HTML part in multipart")
	}
	if !strings.Contains(s, "--tickraft-boundary-") {
		t.Errorf("missing boundary marker")
	}
}

// ---------------------------------------------------------------------------
// i18n rendering tests
// ---------------------------------------------------------------------------

// TestBuildMessage_DefaultFormatterProducesLocalizedOutput verifies that
// buildMessage with no injected Formatter still produces localized output
// via the default Formatter backed by the built-in i18n bundle.
func TestBuildMessage_DefaultFormatterProducesLocalizedOutput(t *testing.T) {
	cfg := Config{
		From: "alert@example.com",
		To:   []string{"ops@example.com"},
	}
	evt := testAlert()
	evt.Locale = "zh-Hans"
	msg := buildMessage(context.Background(), evt, cfg, zap.NewNop())
	s := string(msg)

	// The default Formatter should produce a Chinese title containing the
	// metric name. The exact wording depends on the zh-Hans asset bundle.
	if !strings.Contains(s, "cpu_usage") {
		t.Errorf("zh-Hans message should contain metric name: %q", s)
	}
	// Structured fields should still be present.
	if !strings.Contains(s, "Alert type: metric") {
		t.Errorf("zh-Hans message should contain structured 'Alert type' field: %q", s)
	}
}

// TestBuildMessage_CustomFormatter verifies that an injected Formatter is
// used instead of the default one.
func TestBuildMessage_CustomFormatter(t *testing.T) {
	custom := &mockFormatter{
		msg: i18n.FormattedMessage{
			Title:       "CUSTOM TITLE",
			Description: "Custom description",
			Level:       "Critical",
			Direction:   i18n.LTR,
		},
	}
	cfg := Config{
		From:      "alert@example.com",
		To:        []string{"ops@example.com"},
		Formatter: custom,
	}
	msg := buildMessage(context.Background(), testAlert(), cfg, zap.NewNop())
	s := string(msg)

	if !strings.Contains(s, "CUSTOM TITLE") {
		t.Errorf("custom formatter title not found in message: %q", s)
	}
	if !strings.Contains(s, "Custom description") {
		t.Errorf("custom formatter description not found in message: %q", s)
	}
}

// TestBuildMessage_RTLDirection verifies that the HTML body wraps content
// in a div with dir="rtl" when the formatted message has RTL direction.
func TestBuildMessage_RTLDirection(t *testing.T) {
	custom := &mockFormatter{
		msg: i18n.FormattedMessage{
			Title:       "تنبيه",
			Description: "وصف التنبيه",
			Direction:   i18n.RTL,
		},
	}
	cfg := Config{
		From:      "alert@example.com",
		To:        []string{"ops@example.com"},
		HTMLMode:  true,
		Formatter: custom,
	}
	msg := buildMessage(context.Background(), testAlert(), cfg, zap.NewNop())
	s := string(msg)

	if !strings.Contains(s, `dir="rtl"`) {
		t.Errorf("RTL HTML should contain dir=\"rtl\": %q", s)
	}
}

// TestBuildMessage_LTRDirection verifies that the HTML body wraps content
// in a div with dir="ltr" for LTR locales.
func TestBuildMessage_LTRDirection(t *testing.T) {
	custom := &mockFormatter{
		msg: i18n.FormattedMessage{
			Title:       "Alert",
			Description: "Description",
			Direction:   i18n.LTR,
		},
	}
	cfg := Config{
		From:      "alert@example.com",
		To:        []string{"ops@example.com"},
		HTMLMode:  true,
		Formatter: custom,
	}
	msg := buildMessage(context.Background(), testAlert(), cfg, zap.NewNop())
	s := string(msg)

	if !strings.Contains(s, `dir="ltr"`) {
		t.Errorf("LTR HTML should contain dir=\"ltr\": %q", s)
	}
}

// TestBuildMessage_TemplateRendering verifies that when a Library and
// TemplateID are provided, buildMessage uses Library.Render.
func TestBuildMessage_TemplateRendering(t *testing.T) {
	cfg := Config{
		From: "alert@example.com",
		To:   []string{"ops@example.com"},
	}
	// Use the built-in template library which has cpu_high.
	lib := template.NewBuiltinLibrary(zap.NewNop())
	cfg.Library = lib

	evt := testAlert()
	evt.TemplateID = "cpu_high"
	evt.Locale = "en-US"

	msg := buildMessage(context.Background(), evt, cfg, zap.NewNop())
	s := string(msg)

	// The template-rendered title should contain "cpu_usage" since the
	// cpu_high template uses {{.metric_name}}.
	if !strings.Contains(s, "cpu_usage") {
		t.Errorf("template-rendered message should contain metric name: %q", s)
	}
}

// TestBuildMessage_TemplateRenderFallback verifies that when Library.Render
// fails (e.g. template not found), buildMessage falls back to the Formatter.
func TestBuildMessage_TemplateRenderFallback(t *testing.T) {
	cfg := Config{
		From: "alert@example.com",
		To:   []string{"ops@example.com"},
	}
	lib := template.NewBuiltinLibrary(zap.NewNop())
	cfg.Library = lib

	evt := testAlert()
	evt.TemplateID = "nonexistent_template"
	evt.Locale = "en-US"

	msg := buildMessage(context.Background(), evt, cfg, zap.NewNop())
	s := string(msg)

	// Should still produce a valid message via the default Formatter.
	if !strings.Contains(s, "Alert type: metric") {
		t.Errorf("fallback message should contain 'Alert type' field: %q", s)
	}
}

// TestBuildMessage_AlertLocalePriority verifies that alert.Locale is used
// over the default locale when non-empty.
func TestBuildMessage_AlertLocalePriority(t *testing.T) {
	cfg := Config{
		From: "alert@example.com",
		To:   []string{"ops@example.com"},
	}
	evt := testAlert()
	evt.Locale = "zh-Hans"

	msg := buildMessage(context.Background(), evt, cfg, zap.NewNop())
	s := string(msg)

	// The zh-Hans Formatter should produce a Chinese description. The
	// description uses "指标" (metric) and "当前值" (current value) which
	// are present in the zh-Hans asset bundle.
	if !strings.Contains(s, "指标") {
		t.Errorf("zh-Hans locale should produce Chinese description containing '指标': %q", s)
	}
	if !strings.Contains(s, "当前值") {
		t.Errorf("zh-Hans locale should produce Chinese description containing '当前值': %q", s)
	}
}

// TestBuildMessage_NilFormatterAndLibrary verifies that buildMessage does
// not panic when both Formatter and Library are nil.
func TestBuildMessage_NilFormatterAndLibrary(t *testing.T) {
	cfg := Config{
		From: "alert@example.com",
		To:   []string{"ops@example.com"},
	}
	msg := buildMessage(context.Background(), testAlert(), cfg, zap.NewNop())
	s := string(msg)

	if !strings.Contains(s, "From: alert@example.com") {
		t.Errorf("missing From header with nil Formatter/Library: %q", s)
	}
	if !strings.Contains(s, "Alert type: metric") {
		t.Errorf("missing alert type with nil Formatter/Library: %q", s)
	}
}

// TestRenderAlert_DefaultFormatter verifies renderAlert constructs a default
// Formatter when none is injected.
func TestRenderAlert_DefaultFormatter(t *testing.T) {
	cfg := Config{}
	msg := renderAlert(context.Background(), testAlert(), cfg, zap.NewNop())

	if msg.Title == "" {
		t.Error("default Formatter should produce non-empty title")
	}
	if msg.Direction == "" {
		t.Error("default Formatter should produce non-empty direction")
	}
}

// TestRenderAlert_TemplateLibrary verifies renderAlert uses the template
// Library when TemplateID is set.
func TestRenderAlert_TemplateLibrary(t *testing.T) {
	lib := template.NewBuiltinLibrary(zap.NewNop())
	cfg := Config{Library: lib}

	evt := testAlert()
	evt.TemplateID = "cpu_high"
	evt.Locale = "zh-Hans"

	msg := renderAlert(context.Background(), evt, cfg, zap.NewNop())

	if msg.Title == "" {
		t.Error("template render should produce non-empty title")
	}
}

// mockFormatter is a test double for i18n.Formatter that returns a fixed
// FormattedMessage regardless of input.
type mockFormatter struct {
	msg i18n.FormattedMessage
}

func (m *mockFormatter) Format(_ context.Context, _ alert.Event, _ i18n.FormatOptions) i18n.FormattedMessage {
	return m.msg
}

// ---------------------------------------------------------------------------
// buildAuth tests
// ---------------------------------------------------------------------------

func TestBuildAuth(t *testing.T) {
	ch := &Channel{
		config: Config{
			Username: "user",
			Password: "pass",
			Host:     "127.0.0.1",
		},
	}

	// PLAIN
	ch.config.AuthType = AuthTypePlain
	auth := ch.buildAuth()
	if auth == nil {
		t.Fatal("buildAuth(PLAIN): got nil")
	}

	// LOGIN
	ch.config.AuthType = AuthTypeLogin
	auth = ch.buildAuth()
	if auth == nil {
		t.Fatal("buildAuth(LOGIN): got nil")
	}

	// CRAM-MD5
	ch.config.AuthType = AuthTypeCramMD5
	auth = ch.buildAuth()
	if auth == nil {
		t.Fatal("buildAuth(CRAM-MD5): got nil")
	}

	// Unknown
	ch.config.AuthType = AuthType(99)
	auth = ch.buildAuth()
	if auth != nil {
		t.Errorf("buildAuth(unknown): got non-nil, want nil")
	}
}
