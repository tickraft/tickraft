// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package discord implements a Discord webhook notification channel for the
// tickraft alerting pipeline.
//
// A Channel POSTs alert events as Discord webhook embed payloads to a
// configured webhook URL. Each alert is rendered as a rich embed with a
// title, description, level-derived color, timestamp, footer, and
// structured fields. The channel integrates a circuit breaker (to avoid
// hammering a degraded endpoint) and a retry mechanism with exponential
// backoff and full jitter (to tolerate transient failures). 5xx responses,
// 429 rate limits, and network errors are retried; 4xx responses fail fast.
package discord

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/bytedance/sonic"
	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/circuitbreaker"
	"github.com/tickraft/tickraft/pkg/i18n"
	"github.com/tickraft/tickraft/pkg/prism/alert"
	"github.com/tickraft/tickraft/pkg/prism/alert/template"
	"github.com/tickraft/tickraft/pkg/prism/channel"
	"github.com/tickraft/tickraft/pkg/retry"

	"github.com/tickraft/tickraft/pkg/prism/channel/format"
)

// Embed color constants, expressed as decimal RGB integers as required by
// the Discord embed schema. critical is red, warning is yellow, info is
// green.
const (
	colorCritical = 0xED4245
	colorWarning  = 0xFEE75C
	colorInfo     = 0x57F287
)

// footerText is the static footer rendered on every Discord embed.
const footerText = "Tickraft Alert"

// Channel sends alert notifications to Discord via a webhook. It satisfies
// the alert.Channel interface.
type Channel struct {
	cfg        Config
	httpClient *http.Client
	retry      *retry.Retry
	circuit    *circuitbreaker.CircuitBreaker
	logger     *zap.Logger
	formatter  i18n.Formatter
	library    template.Library
	registry   i18n.Registry
}

// Compile-time assertion that Channel implements alert.Channel.
var _ alert.Channel = (*Channel)(nil)

// typeName is the canonical channel type identifier used by Name and by
// delivery tracking records.
const typeName = "discord"

// Name implements alert.Channel.
func (c *Channel) Name() string { return typeName }

// Send implements alert.Channel. It formats the alert as a Discord embed
// and POSTs it to the configured webhook URL.
//
// The send is protected by a circuit breaker: when the breaker is open Send
// short-circuits with channel.ErrCircuitOpen. Transient failures (5xx, 429
// rate limits, and network errors) are retried with exponential backoff and
// full jitter; 4xx responses fail immediately without retrying. On success
// the breaker is reset; on failure the breaker records a failure and a
// *channel.SendError is returned indicating whether the failure is
// retryable.
func (c *Channel) Send(ctx context.Context, evt alert.Event) error {
	if c.cfg.WebhookURL == "" {
		return channel.NewSendError(c.Name(), false, errors.New("discord: webhook url not configured"))
	}
	if !c.circuit.Allow() {
		c.logger.Debug("discord send suppressed: circuit breaker open")
		return channel.ErrCircuitOpen
	}
	body, err := c.formatPayload(ctx, evt)
	if err != nil {
		c.circuit.RecordFailure()
		return channel.NewSendError(c.Name(), false, fmt.Errorf("format payload: %w", err))
	}
	err = c.retry.Do(ctx, func() error {
		return c.doSend(ctx, body)
	})
	if err != nil {
		c.circuit.RecordFailure()
		c.logger.Warn("discord send failed",
			zap.Error(err),
			zap.Bool("retryable", isRetryableSendErr(err)),
		)
		return channel.NewSendError(c.Name(), isRetryableSendErr(err), err)
	}
	c.circuit.RecordSuccess()
	return nil
}

// doSend performs a single HTTP POST with the pre-marshaled body. It
// returns an *httpError for non-2xx responses and network errors so the
// retry predicate can classify retryability. The webhook URL is redacted
// in error messages so the embedded token is never logged in plaintext.
func (c *Channel) doSend(ctx context.Context, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return &httpError{statusCode: 0, err: fmt.Errorf("build request: %w", err)}
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return &httpError{statusCode: 0, err: err}
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()
	respBody, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return &httpError{
			statusCode: resp.StatusCode,
			err:        fmt.Errorf("read response: %w", readErr),
		}
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	return &httpError{
		statusCode: resp.StatusCode,
		err: fmt.Errorf("status %d from %s: %s",
			resp.StatusCode, redactWebhookURL(c.cfg.WebhookURL), strings.TrimSpace(string(respBody))),
	}
}

// httpError wraps an HTTP failure with the response status code so the
// retry predicate can distinguish retryable (5xx, 429, network) from
// non-retryable (4xx) failures. A statusCode of zero denotes a network
// error.
type httpError struct {
	statusCode int
	err        error
}

// Error implements the error interface.
func (e *httpError) Error() string {
	if e.statusCode > 0 {
		return fmt.Sprintf("discord: status %d: %v", e.statusCode, e.err)
	}
	return fmt.Sprintf("discord: %v", e.err)
}

// Unwrap returns the underlying error.
func (e *httpError) Unwrap() error { return e.err }

// StatusCode reports the HTTP status of the failed response.
// Zero denotes a network error. Consumed by the delivery tracking
// decorator via tracking.ResponseCodeOf.
func (e *httpError) StatusCode() int { return e.statusCode }

// isRetryableSendErr reports whether err represents a retryable Discord
// failure. Network errors (statusCode 0), 429 rate-limit responses, and 5xx
// responses are retryable; 4xx responses and any non-httpError are not.
func isRetryableSendErr(err error) bool {
	var he *httpError
	if errors.As(err, &he) {
		if he.statusCode == 0 {
			return true
		}
		if he.statusCode == 429 {
			return true
		}
		return he.statusCode >= 500
	}
	return false
}

// redactWebhookURL returns a copy of the Discord webhook URL with the
// secret token path segment masked so it can be safely included in logs
// and error messages. Discord webhook URLs have the form
// ".../api/webhooks/<id>/<token>"; the final path segment is the secret
// token and is replaced with "***". URLs that cannot be parsed or that
// lack a multi-segment path are reduced to their scheme and host to avoid
// leaking any segment.
func redactWebhookURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "discord:unparseable-webhook-url"
	}
	segs := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(segs) < 2 {
		return u.Scheme + "://" + u.Host
	}
	segs[len(segs)-1] = "***"
	u.Path = "/" + strings.Join(segs, "/")
	// Build the URL manually rather than via u.String(), which would
	// percent-encode the "***" mask into "%2A%2A%2A" and defeat the
	// readability of the redacted form in logs and error messages.
	return u.Scheme + "://" + u.Host + u.Path
}

// ---------------------------------------------------------------------------
// Embed payload formatting
// ---------------------------------------------------------------------------

// payload is the JSON body sent to a Discord webhook.
type payload struct {
	Username  string  `json:"username,omitempty"`
	AvatarURL string  `json:"avatar_url,omitempty"`
	Embeds    []embed `json:"embeds"`
}

// embed is a single Discord rich embed object.
type embed struct {
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Color       int     `json:"color"`
	Timestamp   string  `json:"timestamp"`
	Footer      footer  `json:"footer"`
	Fields      []field `json:"fields,omitempty"`
}

// footer is the footer object of a Discord embed.
type footer struct {
	Text string `json:"text"`
}

// field is a single Discord embed field.
type field struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline"`
}

// formatPayload builds a Discord-compatible webhook payload from the alert
// event. It uses the shared format Render helper to produce a canonical
// Message (via template Library or i18n Formatter), then adapts it into a
// single embed with a level-derived color, the alert timestamp, a static
// footer, and structured fields. The optional Username and AvatarURL
// overrides are applied at the payload level when set.
func (c *Channel) formatPayload(ctx context.Context, evt alert.Event) ([]byte, error) {
	msg := format.Render(ctx, evt, format.RenderOptions{
		Formatter:       c.formatter,
		Library:         c.library,
		Registry:        c.registry,
		Logger:          c.logger,
		FrontendBaseURL: c.cfg.FrontendBaseURL,
		Scope:           c.cfg.Scope,
	})

	emb := embed{
		Title:       msg.Title,
		Description: msg.Description,
		Color:       levelColor(msg.Level),
		Timestamp:   msg.Timestamp.Format(time.RFC3339),
		Footer:      footer{Text: footerText},
		Fields:      buildEmbedFields(msg),
	}

	p := payload{Embeds: []embed{emb}}
	if c.cfg.Username != "" {
		p.Username = c.cfg.Username
	}
	if c.cfg.AvatarURL != "" {
		p.AvatarURL = c.cfg.AvatarURL
	}
	return sonic.Marshal(p)
}

// buildEmbedFields converts the canonical Message fields into Discord embed
// fields. Field keys are sorted for deterministic output, empty values are
// omitted, and a non-empty AssetLink is appended as a non-inline field.
func buildEmbedFields(msg format.Message) []field {
	keys := make([]string, 0, len(msg.Fields))
	for k := range msg.Fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fields := make([]field, 0, len(keys)+1)
	for _, k := range keys {
		v := msg.Fields[k]
		if v == "" {
			continue
		}
		fields = append(fields, field{Name: k, Value: v, Inline: true})
	}
	if msg.AssetLink != "" {
		fields = append(fields, field{Name: "Asset Link", Value: msg.AssetLink, Inline: false})
	}
	return fields
}

// levelColor maps an alert level string to a Discord embed color. critical
// (and its severe aliases such as error and fatal) is red, warning is
// yellow, info is green; any unrecognized level falls back to green.
func levelColor(level string) int {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "critical", "fatal", "error", "severe", "emergency":
		return colorCritical
	case "warning", "warn":
		return colorWarning
	case "info", "information", "notice":
		return colorInfo
	default:
		return colorInfo
	}
}
