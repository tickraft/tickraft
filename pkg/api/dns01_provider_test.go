// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package api

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/tickraft/tickraft/pkg/cert"
)

// stubDNSProvider is a test double for cert.DNSProvider that records its
// calls and returns configurable errors.
type stubDNSProvider struct {
	presentCalls []presentCall
	cleanupCalls []presentCall
	timeout      time.Duration
	interval     time.Duration
	presentErr   error
}

type presentCall struct {
	domain string
	token  string
	value  string
}

func (s *stubDNSProvider) Present(_ context.Context, domain, token, value string) error {
	s.presentCalls = append(s.presentCalls, presentCall{domain, token, value})
	return s.presentErr
}

func (s *stubDNSProvider) CleanUp(_ context.Context, domain, token, value string) error {
	s.cleanupCalls = append(s.cleanupCalls, presentCall{domain, token, value})
	return nil
}

func (s *stubDNSProvider) Timeout() (time.Duration, time.Duration) {
	return s.interval, s.timeout
}

// Compile-time assertion that stubDNSProvider satisfies cert.DNSProvider.
var _ cert.DNSProvider = (*stubDNSProvider)(nil)

func TestDNS01Provider_ChallengeType(t *testing.T) {
	p := NewDNS01Provider(&stubDNSProvider{})
	if got := p.ChallengeType(); got != ACMEChallengeDNS01 {
		t.Errorf("ChallengeType() = %q, want %q", got, ACMEChallengeDNS01)
	}
}

func TestDNS01Provider_FulfillChallenge_DelegatesToPresent(t *testing.T) {
	stub := &stubDNSProvider{}
	p := NewDNS01Provider(stub)

	params := ACMEChallengeParams{
		Domain:   "example.com",
		Token:    "tok-123",
		Response: "resp-456",
	}
	cleanup, err := p.FulfillChallenge(context.Background(), params)
	if err != nil {
		t.Fatalf("FulfillChallenge failed: %v", err)
	}
	if len(stub.presentCalls) != 1 {
		t.Fatalf("presentCalls = %d, want 1", len(stub.presentCalls))
	}
	got := stub.presentCalls[0]
	if got.domain != "example.com" || got.token != "tok-123" || got.value != "resp-456" {
		t.Errorf("Present call = %+v, want domain=example.com token=tok-123 value=resp-456", got)
	}
	if len(stub.cleanupCalls) != 0 {
		t.Errorf("cleanupCalls before cleanup = %d, want 0", len(stub.cleanupCalls))
	}

	cleanup()
	if len(stub.cleanupCalls) != 1 {
		t.Fatalf("cleanupCalls after cleanup = %d, want 1", len(stub.cleanupCalls))
	}
	gotCleanup := stub.cleanupCalls[0]
	if gotCleanup.domain != "example.com" || gotCleanup.token != "tok-123" || gotCleanup.value != "resp-456" {
		t.Errorf("CleanUp call = %+v, want domain=example.com token=tok-123 value=resp-456", gotCleanup)
	}
}

func TestDNS01Provider_FulfillChallenge_PropagatesPresentError(t *testing.T) {
	stub := &stubDNSProvider{presentErr: cert.ErrDNSChallengeNotConfigured}
	p := NewDNS01Provider(stub)

	_, err := p.FulfillChallenge(context.Background(), ACMEChallengeParams{
		Domain: "example.com",
		Token:  "tok",
	})
	if !errors.Is(err, cert.ErrDNSChallengeNotConfigured) {
		t.Errorf("FulfillChallenge error = %v, want ErrDNSChallengeNotConfigured", err)
	}
}

func TestDNS01Provider_Timeout_Delegates(t *testing.T) {
	stub := &stubDNSProvider{interval: 5 * time.Second, timeout: 2 * time.Minute}
	p := NewDNS01Provider(stub)

	interval, timeout := p.Timeout()
	if interval != 5*time.Second {
		t.Errorf("interval = %v, want 5s", interval)
	}
	if timeout != 2*time.Minute {
		t.Errorf("timeout = %v, want 2m", timeout)
	}
}

func TestDNS01Provider_NilFallsBackToNoop(t *testing.T) {
	p := NewDNS01Provider(nil)
	_, err := p.FulfillChallenge(context.Background(), ACMEChallengeParams{
		Domain: "example.com",
		Token:  "tok",
	})
	if !errors.Is(err, cert.ErrDNSChallengeNotConfigured) {
		t.Errorf("FulfillChallenge with nil provider error = %v, want ErrDNSChallengeNotConfigured", err)
	}
}

func TestDNS01Provider_SatisfiesACMEProvider(t *testing.T) {
	// Compile-time assertion that *DNS01Provider satisfies ACMEProvider.
	var _ ACMEProvider = (*DNS01Provider)(nil)
}
