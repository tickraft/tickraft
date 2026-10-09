// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package telegram implements a Telegram Bot API notification channel for
// the tickraft alerting pipeline.
//
// A Channel POSTs alert events as MarkdownV2 messages to a configured
// Telegram chat via the Bot API sendMessage method. The bot token is the
// credential and is embedded in the request URL path
// (https://api.telegram.org/bot<token>/sendMessage); it is never logged in
// plaintext (see redactToken).
//
// The channel integrates a circuit breaker (to avoid hammering a degraded
// endpoint) and a retry mechanism with exponential backoff and full jitter
// (to tolerate transient failures). Telegram may surface failures at two
// layers: as a non-2xx HTTP status (429 rate limit and 5xx are retryable),
// or as a 2xx HTTP status whose JSON body reports ok=false with an
// error_code (429 and 5xx error_codes are retryable; 4xx error_codes are
// not). Network errors are always retryable; 4xx HTTP responses fail fast.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
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

// parseMode is the Telegram parse mode used for alert messages.
const parseMode = "MarkdownV2"

// Channel sends alert notifications to Telegram via the Bot API. It
// satisfies the alert.Channel interface.
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
const typeName = "telegram"

// Name implements alert.Channel.
func (c *Channel) Name() string { return typeName }

// Send implements alert.Channel. It formats the alert as a Telegram
// MarkdownV2 message and POSTs it to the Bot API sendMessage endpoint for
// the configured chat.
//
// The send is protected by a circuit breaker: when the breaker is open Send
// short-circuits with channel.ErrCircuitOpen. Transient failures (network
// errors, 429 rate limits, 5xx HTTP responses, and Telegram API bodies with
// error_code 429 or >=500) are retried with exponential backoff and full
// jitter; 4xx HTTP responses and Telegram API bodies with 4xx error_codes
// fail immediately without retrying. On success the breaker is reset; on
// failure the breaker records a failure and a *channel.SendError is
// returned indicating whether the failure is retryable.
func (c *Channel) Send(ctx context.Context, evt alert.Event) error {
	if c.cfg.BotToken == "" || c.cfg.ChatID == "" {
		return channel.NewSendError(c.Name(), false, errors.New("telegram: bot token or chat id not configured"))
	}
	if !c.circuit.Allow() {
		c.logger.Debug("telegram send suppressed: circuit breaker open")
		return channel.ErrCircuitOpen
	}
	text := c.formatMessage(ctx, evt)
	body, err := sonic.Marshal(c.buildPayload(text))
	if err != nil {
		c.circuit.RecordFailure()
		return channel.NewSendError(c.Name(), false, fmt.Errorf("marshal payload: %w", err))
	}
	err = c.retry.Do(ctx, func() error {
		return c.doSend(ctx, body)
	})
	if err != nil {
		c.circuit.RecordFailure()
		c.logger.Warn("telegram send failed",
			zap.Error(err),
			zap.Bool("retryable", isRetryableSendErr(err)),
		)
		return channel.NewSendError(c.Name(), isRetryableSendErr(err), err)
	}
	c.circuit.RecordSuccess()
	return nil
}

// doSend performs a single HTTP POST with the pre-marshaled payload to the
// Bot API sendMessage endpoint. It returns an *httpError for non-2xx
// responses, Telegram API rejections (2xx with ok=false), and network
// errors so the retry predicate can classify retryability. The bot token is
// redacted in error messages so it is never logged in plaintext.
func (c *Channel) doSend(ctx context.Context, body []byte) error {
	url := fmt.Sprintf("%s/bot%s/sendMessage", c.cfg.APIBase, c.cfg.BotToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
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

	// Parse the Telegram response envelope best-effort. Telegram returns a
	// JSON object for both 2xx success/failure and most non-2xx responses;
	// a parse failure leaves the zero-value envelope, which is handled
	// gracefully below.
	var tg telegramResponse
	_ = sonic.Unmarshal(respBody, &tg)

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if tg.OK {
			return nil
		}
		desc := strings.TrimSpace(tg.Description)
		if desc == "" {
			desc = strings.TrimSpace(string(respBody))
		}
		return &httpError{
			statusCode: resp.StatusCode,
			errorCode:  tg.ErrorCode,
			err:        fmt.Errorf("telegram api rejected: %s", desc),
		}
	}

	desc := strings.TrimSpace(tg.Description)
	if desc == "" {
		desc = strings.TrimSpace(string(respBody))
	}
	return &httpError{
		statusCode: resp.StatusCode,
		errorCode:  tg.ErrorCode,
		err: fmt.Errorf("status %d from %s/bot%s/sendMessage: %s",
			resp.StatusCode, c.cfg.APIBase, redactToken(c.cfg.BotToken), desc),
	}
}

// httpError wraps a Telegram send failure with both the HTTP response
// status code and the Telegram API error_code (from the response body) so
// the retry predicate can classify retryability across both layers. A
// statusCode of zero denotes a network error. An errorCode of zero means
// no Telegram error_code was present in the body.
type httpError struct {
	// statusCode is the HTTP response status code; 0 for network errors.
	statusCode int
	// errorCode is the Telegram API error_code parsed from the response
	// body, if any.
	errorCode int
	// err is the underlying error detail.
	err error
}

// Error implements the error interface.
func (e *httpError) Error() string {
	if e.statusCode > 0 {
		return fmt.Sprintf("telegram: status %d: %v", e.statusCode, e.err)
	}
	return fmt.Sprintf("telegram: %v", e.err)
}

// Unwrap returns the underlying error.
func (e *httpError) Unwrap() error { return e.err }

// StatusCode reports the HTTP status of the failed response.
// Zero denotes a network error. Consumed by the delivery tracking
// decorator via tracking.ResponseCodeOf.
func (e *httpError) StatusCode() int { return e.statusCode }

// isRetryableSendErr reports whether err represents a retryable Telegram
// failure.
//
//   - Network errors (statusCode 0) are retryable.
//   - For 2xx HTTP responses carrying a Telegram ok=false body, retryability
//     is decided by the API error_code: 429 (rate limit) and 5xx are
//     retryable; 4xx error_codes are not.
//   - For non-2xx HTTP responses, retryability is decided by the HTTP
//     status: 429 and 5xx are retryable; other 4xx are not.
//
// Any error that is not an *httpError is treated as non-retryable.
func isRetryableSendErr(err error) bool {
	var he *httpError
	if !errors.As(err, &he) {
		return false
	}
	if he.statusCode == 0 {
		return true
	}
	if he.statusCode >= 200 && he.statusCode < 300 {
		return he.errorCode == 429 || he.errorCode >= 500
	}
	return he.statusCode == 429 || he.statusCode >= 500
}

// redactToken returns a copy of the bot token with all but the first 8
// characters masked, so it can be safely included in logs and error
// messages. A token shorter than 8 characters is reduced to "***" to avoid
// leaking any of an unusually short secret.
func redactToken(token string) string {
	if len(token) <= 8 {
		return "***"
	}
	return token[:8] + "***"
}

// ---------------------------------------------------------------------------
// Payload formatting
// ---------------------------------------------------------------------------

// payload is the JSON body sent to the Telegram Bot API sendMessage
// endpoint.
type payload struct {
	ChatID    string `json:"chat_id"`
	Text      string `json:"text"`
	ParseMode string `json:"parse_mode"`
}

// telegramResponse is the Telegram Bot API response envelope.
type telegramResponse struct {
	OK          bool            `json:"ok"`
	ErrorCode   int             `json:"error_code,omitempty"`
	Description string          `json:"description,omitempty"`
	Result      json.RawMessage `json:"result,omitempty"`
}

// buildPayload constructs the sendMessage JSON payload struct for the given
// message text.
func (c *Channel) buildPayload(text string) payload {
	return payload{
		ChatID:    c.cfg.ChatID,
		Text:      text,
		ParseMode: parseMode,
	}
}

// formatMessage builds a Telegram MarkdownV2 message from the alert event.
// It uses the shared format.Render helper to produce a canonical Message
// (via template Library or i18n Formatter), then adapts it into MarkdownV2
// text with a bold title, level/time/description lines, and an optional
// asset deep link. All dynamic text is escaped per Telegram MarkdownV2
// rules.
func (c *Channel) formatMessage(ctx context.Context, evt alert.Event) string {
	msg := format.Render(ctx, evt, format.RenderOptions{
		Formatter:       c.formatter,
		Library:         c.library,
		Registry:        c.registry,
		Logger:          c.logger,
		FrontendBaseURL: c.cfg.FrontendBaseURL,
		Scope:           c.cfg.Scope,
	})

	var sb strings.Builder
	sb.WriteString("*")
	sb.WriteString(escapeMDV2(msg.Title))
	sb.WriteString("*\n\n")
	sb.WriteString("*Level:* ")
	sb.WriteString(escapeMDV2(msg.Level))
	sb.WriteString("\n*Time:* ")
	sb.WriteString(escapeMDV2(msg.Timestamp.Format(time.RFC3339)))
	sb.WriteString("\n")
	sb.WriteString(escapeMDV2(msg.Description))
	if msg.AssetLink != "" {
		sb.WriteString("\n[")
		sb.WriteString(escapeMDV2("View Asset"))
		sb.WriteString("](")
		sb.WriteString(escapeMDV2URL(msg.AssetLink))
		sb.WriteString(")")
	}
	return sb.String()
}

// mdv2Specials are the characters that must be escaped with a backslash in
// Telegram MarkdownV2 plain text and link text.
const mdv2Specials = "_*[]()~`>#+-=|{}.!"

// escapeMDV2 escapes every Telegram MarkdownV2 special character in s with
// a preceding backslash so the result is treated as literal text.
func escapeMDV2(s string) string {
	var sb strings.Builder
	sb.Grow(len(s))
	for _, r := range s {
		if strings.ContainsRune(mdv2Specials, r) {
			sb.WriteByte('\\')
		}
		sb.WriteRune(r)
	}
	return sb.String()
}

// escapeMDV2URL escapes the characters that Telegram MarkdownV2 requires to
// be escaped inside the URL part of an inline link: ')' and '\'. Other
// characters (including '.' and '/') are left intact so URLs remain valid.
func escapeMDV2URL(s string) string {
	var sb strings.Builder
	sb.Grow(len(s))
	for _, r := range s {
		if r == ')' || r == '\\' {
			sb.WriteByte('\\')
		}
		sb.WriteRune(r)
	}
	return sb.String()
}
