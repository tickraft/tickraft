// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// This file implements the HTTP-01 challenge provider. The
// provider stores challenge responses in an in-memory map keyed by token,
// exposes them via a Hertz handler at /.well-known/acme-challenge/:token, and
// cleans them up after the challenge is validated. It is registered as the
// default provider for ACMEChallengeHTTP01 by init, so the runtime
// can issue certificates via ACME HTTP-01 out of the box; extended
// editions replace it (or register an additional DNS-01 provider) via
// SetACMEProvider.

package api

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
)

// HTTP01ChallengePath is the URL path prefix under which HTTP-01 challenge
// responses are served, per RFC 8555 §8.3. Callers append the path parameter
// (e.g. ":token" in Hertz route syntax) when registering the handler.
const HTTP01ChallengePath = "/.well-known/acme-challenge/"

// http01TokenTTL is the maximum time a stored challenge response is kept
// before being garbage-collected. The TTL is generous (1 hour) so a slow ACME
// server validation does not race with cleanup; the cleanup callback returned
// from FulfillChallenge removes the token immediately on success.
const http01TokenTTL = time.Hour

// HTTP01Provider is the ACME HTTP-01 challenge provider. It
// stores challenge responses in an in-memory map and serves them via the
// Handler method, which the start command registers on the Hertz server.
//
// The provider is safe for concurrent use: multiple domains may be challenged
// in parallel, and the handler may be invoked concurrently with FulfillChallenge.
type HTTP01Provider struct {
	mu     sync.RWMutex
	tokens map[string]http01Entry
}

type http01Entry struct {
	response  string
	expiresAt time.Time
}

// NewHTTP01Provider returns a fresh HTTP01Provider with an empty token store.
func NewHTTP01Provider() *HTTP01Provider {
	return &HTTP01Provider{
		tokens: make(map[string]http01Entry),
	}
}

// ChallengeType returns ACMEChallengeHTTP01.
func (p *HTTP01Provider) ChallengeType() ACMEChallenge {
	return ACMEChallengeHTTP01
}

// FulfillChallenge stores the challenge response so the HTTP handler can
// serve it when the ACME server polls /.well-known/acme-challenge/<token>.
// The returned cleanup function removes the token from the store; it is safe
// to call after the challenge has been validated or after a failure.
func (p *HTTP01Provider) FulfillChallenge(_ context.Context, params ACMEChallengeParams) (func(), error) {
	if params.Token == "" {
		return nil, fmt.Errorf("http-01: token is required")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.tokens[params.Token] = http01Entry{
		response:  params.Response,
		expiresAt: time.Now().Add(http01TokenTTL),
	}
	return func() {
		p.mu.Lock()
		delete(p.tokens, params.Token)
		p.mu.Unlock()
	}, nil
}

// Handler is the Hertz handler that serves HTTP-01 challenge responses at
// /.well-known/acme-challenge/:token. The start command registers it on the
// root route group (see cmd/tickraft/start.go) so ACME validations reach it
// on the same port as the API.
//
// Tokens that have expired (see http01TokenTTL) or that were never stored
// return 404, matching the ACME server's expectation that an unhandled
// challenge responds with a 404.
func (p *HTTP01Provider) Handler(ctx context.Context, arc *app.RequestContext) {
	token := arc.Param("token")
	if token == "" {
		arc.String(http.StatusNotFound, "not found")
		return
	}
	p.mu.RLock()
	entry, ok := p.tokens[token]
	p.mu.RUnlock()
	if !ok || time.Now().After(entry.expiresAt) {
		arc.String(http.StatusNotFound, "not found")
		return
	}
	arc.String(http.StatusOK, entry.response)
}

// Sweep removes expired tokens from the store. It is safe for concurrent use
// and is intended to be called periodically (e.g. once per hour) to bound
// memory usage in long-running processes. The start command may wire it into
// the operator maintenance loop.
func (p *HTTP01Provider) Sweep(now time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for token, entry := range p.tokens {
		if now.After(entry.expiresAt) {
			delete(p.tokens, token)
		}
	}
}

// Compile-time assertion that HTTP01Provider satisfies ACMEProvider.
var _ ACMEProvider = (*HTTP01Provider)(nil)

// init registers the default HTTP-01 challenge provider. callers
// may replace it (or register additional providers) via SetACMEProvider at
// startup; the last registration wins per challenge type.
func init() {
	SetACMEProvider(NewHTTP01Provider())
}
