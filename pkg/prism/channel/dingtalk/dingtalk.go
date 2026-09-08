// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package dingtalk implements a DingTalk robot notification channel for the
// tickraft alerting pipeline.
//
// A Channel POSTs alert events as DingTalk robot messages to a configured
// webhook endpoint. When a Secret is configured, each request is signed
// with HMAC-SHA256 and the timestamp and signature are appended as query
// parameters. The channel integrates a circuit breaker (to avoid hammering
// a degraded endpoint) and a retry mechanism with exponential backoff and
// full jitter (to tolerate transient failures). 5xx responses and network
// errors are retried; 4xx responses and DingTalk API errors (errcode != 0)
// fail fast.
package dingtalk

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
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

// typeName is the canonical channel type identifier used by Name and by
// delivery tracking records.
const typeName = "dingtalk"

// Channel sends alert notifications as DingTalk robot messages. It
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

// Name implements alert.Channel.
func (c *Channel) Name() string { return typeName }

// Send implements alert.Channel. It formats the alert as a DingTalk
// message and POSTs it to the configured webhook URL. When a Secret is
// configured, each attempt is individually signed with the current
// timestamp.
//
// The send is protected by a circuit breaker: when the breaker is open
// Send short-circuits with channel.ErrCircuitOpen. Transient failures
// (5xx and network errors) are retried with exponential backoff and full
// jitter; 4xx responses and DingTalk API errors (errcode != 0) fail
// immediately without retrying. On success the breaker is reset; on
// failure the breaker records a failure and a *channel.SendError is
// returned indicating whether the failure is retryable.
func (c *Channel) Send(ctx context.Context, evt alert.Event) error {
	if c.cfg.WebhookURL == "" {
		return channel.NewSendError(c.Name(), false, errors.New("dingtalk: webhook url not configured"))
	}
	if !c.circuit.Allow() {
		c.logger.Debug("dingtalk send suppressed: circuit breaker open")
		return channel.ErrCircuitOpen
	}

	body, err := c.buildMessage(ctx, evt)
	if err != nil {
		c.circuit.RecordFailure()
		return channel.NewSendError(c.Name(), false, fmt.Errorf("build message: %w", err))
	}

	err = c.retry.Do(ctx, func() error {
		timestamp := time.Now().UnixMilli()
		fullURL, urlErr := c.buildURL(timestamp)
		if urlErr != nil {
			return fmt.Errorf("build url: %w", urlErr)
		}
		return c.doSend(ctx, fullURL, body)
	})
	if err != nil {
		c.circuit.RecordFailure()
		c.logger.Warn("dingtalk send failed",
			zap.Error(err),
			zap.Bool("retryable", isRetryableSendErr(err)),
		)
		return channel.NewSendError(c.Name(), isRetryableSendErr(err), err)
	}
	c.circuit.RecordSuccess()
	return nil
}

// buildMessage formats the alert as a DingTalk message JSON payload
// according to the configured MessageType. The alert is rendered through
// the shared format.Render helper (template Library or i18n Formatter)
// before being adapted to the DingTalk text or markdown format.
func (c *Channel) buildMessage(ctx context.Context, evt alert.Event) ([]byte, error) {
	switch c.cfg.MessageType {
	case MessageTypeMarkdown:
		return c.formatMarkdownMessage(ctx, evt)
	default:
		return c.formatTextMessage(ctx, evt)
	}
}

// doSend performs a single HTTP POST with the pre-marshaled body. It
// returns an *httpError for non-2xx responses and network errors so the
// retry predicate can classify retryability. DingTalk API errors
// (errcode != 0) are returned as plain (non-retryable) errors.
func (c *Channel) doSend(ctx context.Context, fullURL string, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, bytes.NewReader(body))
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

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &httpError{
			statusCode: resp.StatusCode,
			err:        fmt.Errorf("status %d from %s", resp.StatusCode, c.cfg.WebhookURL),
		}
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return &httpError{statusCode: 0, err: fmt.Errorf("read response: %w", err)}
	}

	var dr dingtalkResponse
	if err := sonic.Unmarshal(respBody, &dr); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}
	if dr.ErrCode != 0 {
		return fmt.Errorf("dingtalk api error: code %d: %s", dr.ErrCode, dr.ErrMsg)
	}
	return nil
}

// sign computes the DingTalk HMAC-SHA256 signature for the given
// timestamp and secret. The signature is
// base64(hmac-sha256(secret, timestamp + "\n" + secret)).
func (c *Channel) sign(timestamp int64, secret string) (string, error) {
	stringToSign := fmt.Sprintf("%d\n%s", timestamp, secret)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(stringToSign))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil)), nil
}

// buildURL appends the timestamp and signature query parameters to the
// webhook URL. When no Secret is configured, the webhook URL is returned
// as-is.
func (c *Channel) buildURL(timestamp int64) (string, error) {
	if c.cfg.Secret == "" {
		return c.cfg.WebhookURL, nil
	}
	signature, err := c.sign(timestamp, c.cfg.Secret)
	if err != nil {
		return "", fmt.Errorf("compute signature: %w", err)
	}
	u, err := url.Parse(c.cfg.WebhookURL)
	if err != nil {
		return "", fmt.Errorf("parse webhook url: %w", err)
	}
	q := u.Query()
	q.Set("timestamp", strconv.FormatInt(timestamp, 10))
	q.Set("sign", signature)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// renderAlert converts an alert event into a canonical format.Message using
// the two-tier dispatch in format.Render: template-based (Library) or
// formatter-based (Formatter). The rendered Message is then adapted by
// the formatTextMessage and formatMarkdownMessage helpers into
// DingTalk-specific payloads.
func (c *Channel) renderAlert(ctx context.Context, evt alert.Event) format.Message {
	return format.Render(ctx, evt, format.RenderOptions{
		Formatter:       c.formatter,
		Library:         c.library,
		Registry:        c.registry,
		Logger:          c.logger,
		FrontendBaseURL: c.cfg.FrontendBaseURL,
		Scope:           c.cfg.Scope,
	})
}

// formatTextMessage formats the alert as a DingTalk text message JSON
// payload. The alert is first rendered to a canonical format.Message via
// format.Render, then adapted to the DingTalk text format. When a keyword
// filter is configured the keyword is appended to the content so
// keyword-protected robots accept the message.
func (c *Channel) formatTextMessage(ctx context.Context, evt alert.Event) ([]byte, error) {
	msg := c.renderAlert(ctx, evt)
	content := format.RenderText(msg)
	if c.cfg.Keyword != "" {
		content += "\n" + c.cfg.Keyword
	}
	payload := textMessage{
		MsgType: MessageTypeText,
		Text: textContent{
			Content: content,
		},
	}
	return sonic.Marshal(payload)
}

// formatMarkdownMessage formats the alert as a DingTalk markdown message
// JSON payload. The alert is first rendered to a canonical format.Message
// via format.Render, then adapted to the DingTalk markdown format. When a
// keyword filter is configured the keyword is appended to the text so
// keyword-protected robots accept the message.
func (c *Channel) formatMarkdownMessage(ctx context.Context, evt alert.Event) ([]byte, error) {
	msg := c.renderAlert(ctx, evt)
	text := format.RenderMarkdown(msg, format.MarkdownStyle{
		HeaderFormat: "### Tickraft Alert: %s\n\n",
		LinePrefix:   "- ",
		LinkFormat:   "- [View Asset](%s)\n",
	})
	if c.cfg.Keyword != "" {
		text += "\n" + c.cfg.Keyword
	}
	payload := markdownMessage{
		MsgType: MessageTypeMarkdown,
		Markdown: markdownContent{
			Title: "Tickraft Alert",
			Text:  text,
		},
	}
	return sonic.Marshal(payload)
}

// textMessage is the DingTalk text message payload.
type textMessage struct {
	MsgType string      `json:"msgtype"`
	Text    textContent `json:"text"`
}

// textContent holds the text body of a DingTalk text message.
type textContent struct {
	Content string `json:"content"`
}

// markdownMessage is the DingTalk markdown message payload.
type markdownMessage struct {
	MsgType  string          `json:"msgtype"`
	Markdown markdownContent `json:"markdown"`
}

// markdownContent holds the title and markdown body of a DingTalk markdown
// message.
type markdownContent struct {
	Title string `json:"title"`
	Text  string `json:"text"`
}

// dingtalkResponse is the DingTalk API response body.
type dingtalkResponse struct {
	ErrCode int    `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
}

// httpError wraps an HTTP failure with the response status code so the
// retry predicate can distinguish retryable (5xx, network) from
// non-retryable (4xx) failures. A statusCode of zero denotes a network
// error.
type httpError struct {
	statusCode int
	err        error
}

// Error implements the error interface.
func (e *httpError) Error() string {
	if e.statusCode > 0 {
		return fmt.Sprintf("dingtalk: status %d: %v", e.statusCode, e.err)
	}
	return fmt.Sprintf("dingtalk: %v", e.err)
}

// Unwrap returns the underlying error.
func (e *httpError) Unwrap() error { return e.err }

// StatusCode reports the HTTP status of the failed response.
// Zero denotes a network error. Consumed by the delivery tracking
// decorator via tracking.ResponseCodeOf.
func (e *httpError) StatusCode() int { return e.statusCode }

// isRetryableSendErr reports whether err represents a retryable DingTalk
// failure. Network errors (statusCode 0) and 5xx responses are retryable;
// 4xx responses and any non-httpError (including DingTalk API errors) are
// not.
func isRetryableSendErr(err error) bool {
	var he *httpError
	if errors.As(err, &he) {
		return he.statusCode == 0 || he.statusCode >= 500
	}
	return false
}
