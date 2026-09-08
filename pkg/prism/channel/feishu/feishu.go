// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package feishu implements a Feishu (飞书/Lark) custom robot notification
// channel for the tickraft alerting pipeline.
//
// A Channel POSTs alert events as Feishu robot messages to a configured
// webhook endpoint. When a Secret is configured, each request body is
// augmented with a fresh timestamp and an HMAC-SHA256 signature so Feishu
// can verify the sender. The channel supports two message formats: a
// plain-text message and an interactive card (the default) that renders
// the alert level, description, timestamp, and an optional deep link to
// the triggering asset.
//
// The channel integrates a circuit breaker (to avoid hammering a degraded
// endpoint) and a retry mechanism with exponential backoff and full
// jitter (to tolerate transient failures). 5xx responses and network
// errors are retried; 4xx responses and Feishu API errors (code != 0)
// fail fast.
package feishu

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
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
const typeName = "feishu"

// cardTagDiv is the Feishu card element tag for text blocks.
const cardTagDiv = "div"

// cardTagPlainText is the Feishu text tag for plain (non-markdown) content.
const cardTagPlainText = "plain_text"

// defaultBaseURL is the feishu open API base URL used by app mode.
const defaultBaseURL = "https://open.feishu.cn"

// appAPIPath constants used by app mode.
const (
	//nolint:gosec // API path constant, not a credential
	pathTenantToken = "/open-apis/auth/v3/tenant_access_token/internal"
	pathMessageSend = "/open-apis/im/v1/messages"
)

// tokenRefreshLeadTime is subtracted from the token's reported expire so
// the channel refreshes proactively before the server-side expiry.
const tokenRefreshLeadTime = 5 * time.Minute

// tokenErrCodes are feishu API codes indicating an invalid or expired
// tenant_access_token. They invalidate the cached token and are retried
// so the next attempt fetches a fresh token.
var tokenErrCodes = map[int]struct{}{
	99991661: {}, // invalid access token
	99991663: {}, // access token expired
	99991668: {}, // wrong app access token
}

// tokenResponse is the JSON body returned by the tenant_access_token API.
type tokenResponse struct {
	Code              int    `json:"code"`
	Msg               string `json:"msg"`
	TenantAccessToken string `json:"tenant_access_token"`
	Expire            int64  `json:"expire"`
}

// Channel sends alert notifications to Feishu. It supports two delivery
// modes configured via Config.Mode: webhook (custom robot, the historical
// default) and app (application messages via the open API). App mode is
// the mode whose cards can carry callback buttons routed to the
// application's event subscription. It satisfies the alert.Channel
// interface.
type Channel struct {
	cfg        Config
	baseURL    string
	httpClient *http.Client
	retry      *retry.Retry
	circuit    *circuitbreaker.CircuitBreaker
	logger     *zap.Logger
	formatter  i18n.Formatter
	library    template.Library
	registry   i18n.Registry

	// tenant_access_token cache (app mode). Protected by tokenMu.
	tokenMu       sync.Mutex
	accessToken   string
	tokenExpireAt time.Time
}

// Compile-time assertion that Channel implements alert.Channel.
var _ alert.Channel = (*Channel)(nil)

// Name implements alert.Channel.
func (c *Channel) Name() string { return typeName }

// Send implements alert.Channel. It formats the alert as a Feishu message
// and delivers it through the configured mode: a signed POST to the
// custom robot webhook (webhook mode; each retry attempt is individually
// signed when a Secret is configured) or an application message via the
// open API with a cached tenant_access_token (app mode).
//
// The send is protected by a circuit breaker: when the breaker is open
// Send short-circuits with channel.ErrCircuitOpen. Transient failures
// (5xx, network errors, and token-related API errors in app mode) are
// retried with exponential backoff and full jitter; 4xx responses and
// Feishu API errors (code != 0) fail immediately without retrying. On
// success the breaker is reset; on failure the breaker records a failure
// and a *channel.SendError is returned indicating whether the failure is
// retryable.
func (c *Channel) Send(ctx context.Context, evt alert.Event) error {
	switch c.cfg.Mode {
	case ModeApp:
		if c.cfg.AppID == "" || c.cfg.AppSecret == "" || c.cfg.ChatID == "" {
			return channel.NewSendError(c.Name(), false, errors.New("feishu: app credentials not configured"))
		}
	case ModeRobot:
		if c.cfg.WebhookURL == "" {
			return channel.NewSendError(c.Name(), false, errors.New("feishu: webhook url not configured"))
		}
	default:
		return channel.NewSendError(c.Name(), false,
			fmt.Errorf("feishu: invalid mode %q", c.cfg.Mode))
	}
	if !c.circuit.Allow() {
		c.logger.Debug("feishu send suppressed: circuit breaker open")
		return channel.ErrCircuitOpen
	}

	body, err := c.formatMessage(ctx, evt)
	if err != nil {
		c.circuit.RecordFailure()
		return channel.NewSendError(c.Name(), false, fmt.Errorf("format message: %w", err))
	}

	err = c.retry.Do(ctx, func() error {
		if c.cfg.Mode == ModeApp {
			return c.doSendApp(ctx, body)
		}
		return c.doSend(ctx, body)
	})
	if err != nil {
		c.circuit.RecordFailure()
		c.logger.Warn("feishu send failed",
			zap.Error(err),
			zap.Bool("retryable", isRetryableSendErr(err)),
		)
		return channel.NewSendError(c.Name(), isRetryableSendErr(err), err)
	}
	c.circuit.RecordSuccess()
	return nil
}

// doSend performs a single HTTP POST with the pre-marshaled body. When a
// Secret is configured, a fresh timestamp and HMAC-SHA256 signature are
// injected into a copy of the body so each attempt is independently
// signed.
//
// It returns an *httpError for non-2xx responses and network errors so
// the retry predicate can classify retryability. Feishu API errors
// (code != 0) and response parse errors are returned as plain
// (non-retryable) errors.
func (c *Channel) doSend(ctx context.Context, body []byte) error {
	sendBody := body
	if c.cfg.Secret != "" {
		timestamp := time.Now().Unix()
		signature, err := c.sign(timestamp, c.cfg.Secret)
		if err != nil {
			return fmt.Errorf("compute signature: %w", err)
		}
		sendBody, err = injectSign(body, timestamp, signature)
		if err != nil {
			return fmt.Errorf("inject signature: %w", err)
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.WebhookURL, bytes.NewReader(sendBody))
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

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &httpError{
			statusCode: resp.StatusCode,
			err: fmt.Errorf("status %d from %s: %s",
				resp.StatusCode, c.cfg.WebhookURL, strings.TrimSpace(string(respBody))),
		}
	}

	var fr feishuResponse
	if err := sonic.Unmarshal(respBody, &fr); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}
	if fr.Code != 0 {
		return fmt.Errorf("feishu api error: code %d: %s", fr.Code, fr.Msg)
	}
	return nil
}

// doSendApp delivers one application message through the open API. The
// tenant_access_token is fetched (or served from cache) per attempt so a
// token invalidated between retries transparently refreshes. It returns
// an *appError for non-2xx responses, network errors, and non-zero API
// codes so the retry predicate can classify retryability.
func (c *Channel) doSendApp(ctx context.Context, body []byte) error {
	token, err := c.appToken(ctx)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.apiURL(pathMessageSend)+"?receive_id_type=chat_id", bytes.NewReader(body))
	if err != nil {
		return &appError{statusCode: 0, err: fmt.Errorf("build request: %w", err)}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	respBody, status, err := c.doRequest(req)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return &appError{statusCode: status, err: fmt.Errorf(
			"status %d from %s: %s", status, pathMessageSend, strings.TrimSpace(string(respBody)))}
	}
	var fr feishuResponse
	if err := sonic.Unmarshal(respBody, &fr); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}
	if fr.Code != 0 {
		if _, ok := tokenErrCodes[fr.Code]; ok {
			c.invalidateToken()
		}
		return &appError{apiCode: fr.Code, err: fmt.Errorf("feishu api error: code %d: %s", fr.Code, fr.Msg)}
	}
	return nil
}

// appToken returns a valid tenant_access_token, fetching and caching a
// fresh one when the cache is empty or within tokenRefreshLeadTime of
// expiry.
func (c *Channel) appToken(ctx context.Context) (string, error) {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()
	if c.accessToken != "" && time.Now().Before(c.tokenExpireAt) {
		return c.accessToken, nil
	}
	reqBody, err := sonic.Marshal(map[string]string{
		"app_id":     c.cfg.AppID,
		"app_secret": c.cfg.AppSecret,
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.apiURL(pathTenantToken), bytes.NewReader(reqBody))
	if err != nil {
		return "", &appError{statusCode: 0, err: fmt.Errorf("build request: %w", err)}
	}
	req.Header.Set("Content-Type", "application/json")
	respBody, status, err := c.doRequest(req)
	if err != nil {
		return "", err
	}
	var tr tokenResponse
	if err := sonic.Unmarshal(respBody, &tr); err != nil {
		return "", fmt.Errorf("parse token response: %w", err)
	}
	if status != http.StatusOK || tr.Code != 0 || tr.TenantAccessToken == "" {
		return "", &appError{statusCode: status, apiCode: tr.Code,
			err: fmt.Errorf("feishu token error: status %d code %d: %s", status, tr.Code, tr.Msg)}
	}
	c.accessToken = tr.TenantAccessToken
	expire := time.Duration(tr.Expire)*time.Second - tokenRefreshLeadTime
	if expire <= 0 {
		expire = time.Minute
	}
	c.tokenExpireAt = time.Now().Add(expire)
	return c.accessToken, nil
}

// invalidateToken drops the cached tenant_access_token so the next
// attempt fetches a fresh one.
func (c *Channel) invalidateToken() {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()
	c.accessToken = ""
	c.tokenExpireAt = time.Time{}
}

// apiURL joins the base URL with an open API path.
func (c *Channel) apiURL(path string) string {
	base := c.baseURL
	if base == "" {
		base = defaultBaseURL
	}
	return strings.TrimRight(base, "/") + path
}

// doRequest executes one HTTP request and drains and closes the response
// body, returning it with the status code.
func (c *Channel) doRequest(req *http.Request) (respBody []byte, statusCode int, err error) {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, &appError{statusCode: 0, err: err}
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()
	respBody, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, resp.StatusCode, &appError{
			statusCode: resp.StatusCode,
			err:        fmt.Errorf("read response: %w", readErr),
		}
	}
	return respBody, resp.StatusCode, nil
}

// sign computes the Feishu HMAC-SHA256 signature for the given timestamp
// and secret. The signature is
// base64(hmac-sha256(secret, timestamp + "\n" + secret)) where timestamp
// is the Unix time in seconds rendered as a string.
func (c *Channel) sign(timestamp int64, secret string) (string, error) {
	stringToSign := fmt.Sprintf("%d\n%s", timestamp, secret)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(stringToSign))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil)), nil
}

// injectSign augments a pre-marshaled Feishu payload with the timestamp
// and sign fields required by signature verification. Existing fields are
// preserved verbatim by round-tripping through a raw JSON map, so nested
// structures (such as the interactive card) are left untouched.
func injectSign(body []byte, timestamp int64, sign string) ([]byte, error) {
	m := make(map[string]json.RawMessage)
	if err := sonic.Unmarshal(body, &m); err != nil {
		return nil, fmt.Errorf("unmarshal body: %w", err)
	}
	tsBytes, err := sonic.Marshal(strconv.FormatInt(timestamp, 10))
	if err != nil {
		return nil, err
	}
	signBytes, err := sonic.Marshal(sign)
	if err != nil {
		return nil, err
	}
	m["timestamp"] = tsBytes
	m["sign"] = signBytes
	return sonic.Marshal(m)
}

// formatMessage builds a Feishu-compatible JSON payload from the alert
// event according to the configured mode and MessageType. It uses the
// shared format.Render helper to produce a canonical Message (via
// template Library or i18n Formatter), then adapts it to the Feishu text
// or interactive card schema: the robot webhook envelope in webhook mode,
// or the application message envelope (content as a JSON string) in app
// mode.
func (c *Channel) formatMessage(ctx context.Context, evt alert.Event) ([]byte, error) {
	msg := format.Render(ctx, evt, format.RenderOptions{
		Formatter:       c.formatter,
		Library:         c.library,
		Registry:        c.registry,
		Logger:          c.logger,
		FrontendBaseURL: c.cfg.FrontendBaseURL,
		Scope:           c.cfg.Scope,
	})
	if c.cfg.PlainNotificationOnly || c.cfg.MessageType == MessageTypeText {
		return c.formatTextMessage(msg)
	}
	return c.formatInteractiveMessage(msg, evt)
}

// formatTextMessage renders the canonical Message as a Feishu text
// message. In plain-notification (L0) mode — and whenever the scope
// policy left an intranet-address note on the message — the resource link
// is annotated with the localized intranet-address hint so recipients
// understand why the link only resolves inside the deployment network.
// The envelope depends on the mode: the robot webhook payload in webhook
// mode, the application message payload (content as a JSON string) in
// app mode.
func (c *Channel) formatTextMessage(msg format.Message) ([]byte, error) {
	var sb strings.Builder
	sb.WriteString("[Tickraft Alert] ")
	sb.WriteString(msg.Title)
	sb.WriteString("\n")
	fmt.Fprintf(&sb, "Level: %s\n", msg.Level)
	if msg.Description != "" {
		fmt.Fprintf(&sb, "Detail: %s\n", msg.Description)
	}
	fmt.Fprintf(&sb, "Time: %s\n", msg.Timestamp.Format(time.RFC3339))
	for k, v := range msg.Fields {
		fmt.Fprintf(&sb, "%s: %s\n", k, v)
	}
	if msg.AssetLink != "" {
		switch {
		case c.cfg.PlainNotificationOnly:
			fmt.Fprintf(&sb, "Link: %s (%s)\n", msg.AssetLink,
				format.IntranetLinkHint(c.registry, msg.Locale))
		case msg.LinkNote != "":
			fmt.Fprintf(&sb, "Link: %s (%s)\n", msg.AssetLink, msg.LinkNote)
		default:
			fmt.Fprintf(&sb, "Link: %s\n", msg.AssetLink)
		}
	}
	content, err := sonic.Marshal(textContent{Text: sb.String()})
	if err != nil {
		return nil, err
	}
	return c.wrapMessage(MessageTypeText, content)
}

// formatInteractiveMessage renders the canonical Message as a Feishu
// interactive card. The card header carries the alert title and the body
// lists the level, description, timestamp, and an optional asset link
// button when a FrontendBaseURL is configured. In app mode with the
// interactive-card policy enabled, the card additionally carries callback
// buttons (action verb plus EventID in the button value) instead of the
// link-only action; custom-robot cards cannot receive callbacks, so
// webhook mode keeps the link button.
func (c *Channel) formatInteractiveMessage(msg format.Message, evt alert.Event) ([]byte, error) {
	elements := []cardElement{
		{Tag: cardTagDiv, Text: &cardText{Tag: "lark_md", Content: fmt.Sprintf("**Level:** %s", msg.Level)}},
	}
	if msg.Description != "" {
		elements = append(elements, cardElement{
			Tag:  cardTagDiv,
			Text: &cardText{Tag: "lark_md", Content: msg.Description},
		})
	}
	elements = append(elements, cardElement{
		Tag:  cardTagDiv,
		Text: &cardText{Tag: "lark_md", Content: fmt.Sprintf("**Time:** %s", msg.Timestamp.Format(time.RFC3339))},
	})
	if buttons := c.callbackButtons(evt); len(buttons) > 0 {
		actions := make([]cardAction, 0, len(buttons)+1)
		if msg.AssetLink != "" {
			actions = append(actions, cardAction{
				Tag:  "button",
				Text: cardText{Tag: cardTagPlainText, Content: "View Asset"},
				URL:  msg.AssetLink,
				Type: "default",
			})
		}
		for _, b := range buttons {
			actions = append(actions, cardAction{
				Tag:  "button",
				Text: cardText{Tag: cardTagPlainText, Content: b.Label},
				Type: buttonStyle(b.Action),
				Value: map[string]string{
					"action":   b.Action,
					"event_id": evt.EventID,
				},
			})
		}
		elements = append(elements, cardElement{Tag: "action", Actions: actions})
	} else if msg.AssetLink != "" {
		elements = append(elements, cardElement{
			Tag: "action",
			Actions: []cardAction{{
				Tag:  "button",
				Text: cardText{Tag: cardTagPlainText, Content: "View Asset"},
				URL:  msg.AssetLink,
				Type: "default",
			}},
		})
	}
	card, err := sonic.Marshal(cardPayload{
		Header:   cardHeader{Title: cardText{Tag: cardTagPlainText, Content: msg.Title}},
		Elements: elements,
	})
	if err != nil {
		return nil, err
	}
	return c.wrapMessage(MessageTypeInteractive, card)
}

// callbackButtons returns the interaction buttons for evt, but only in
// app mode: robot webhook cards cannot receive button callbacks, so
// rendering them there would be misleading.
func (c *Channel) callbackButtons(evt alert.Event) []format.ActionButton {
	if c.cfg.Mode != ModeApp {
		return nil
	}
	return c.cfg.Interaction.ButtonsFor(evt)
}

// buttonStyle maps a lifecycle action verb onto a Feishu button visual
// type. Unknown verbs get the default style.
func buttonStyle(action string) string {
	switch action {
	case format.ActionAcknowledge:
		return "primary"
	case format.ActionSilence:
		return "danger"
	default:
		return "default"
	}
}

// wrapMessage envelopes a marshaled message content (text content or card
// JSON) in the mode-specific request body: the robot webhook payload
// carries the content inline, while the application message payload
// carries it as a JSON string under content.
func (c *Channel) wrapMessage(msgType MessageType, content []byte) ([]byte, error) {
	if c.cfg.Mode != ModeApp {
		switch msgType {
		case MessageTypeText:
			return sonic.Marshal(textMessage{MsgType: msgType, Content: json.RawMessage(content)})
		default:
			return sonic.Marshal(interactiveMessage{MsgType: msgType, Card: json.RawMessage(content)})
		}
	}
	body := appMessage{
		ReceiveID: c.cfg.ChatID,
		MsgType:   msgType,
		Content:   string(content),
	}
	return sonic.Marshal(body)
}

// textMessage is the Feishu robot webhook text message payload.
type textMessage struct {
	MsgType MessageType     `json:"msg_type"`
	Content json.RawMessage `json:"content"`
}

// interactiveMessage is the Feishu robot webhook interactive card payload.
type interactiveMessage struct {
	MsgType MessageType     `json:"msg_type"`
	Card    json.RawMessage `json:"card"`
}

// appMessage is the Feishu application message payload; content holds
// the message body (text content or card JSON) as a JSON string.
type appMessage struct {
	ReceiveID string      `json:"receive_id"`
	MsgType   MessageType `json:"msg_type"`
	Content   string      `json:"content"`
}

// textContent holds the text body of a Feishu text message.
type textContent struct {
	Text string `json:"text"`
}

// cardPayload holds the header and elements of a Feishu interactive card.
type cardPayload struct {
	Header   cardHeader    `json:"header"`
	Elements []cardElement `json:"elements"`
}

// cardHeader holds the card title.
type cardHeader struct {
	Title cardText `json:"title"`
}

// cardText is a Feishu text composition object.
type cardText struct {
	Tag     string `json:"tag"`
	Content string `json:"content"`
}

// cardElement is a single Feishu card element. For div elements only
// Text is set; for action elements only Actions is set.
type cardElement struct {
	Tag     string       `json:"tag"`
	Text    *cardText    `json:"text,omitempty"`
	Actions []cardAction `json:"actions,omitempty"`
}

// cardAction is a single action (e.g. a link button) inside an action
// element. URL buttons navigate; Value buttons carry the callback
// payload (action verb plus event ID) delivered to the application's
// event subscription when clicked.
type cardAction struct {
	Tag   string            `json:"tag"`
	Text  cardText          `json:"text"`
	URL   string            `json:"url,omitempty"`
	Type  string            `json:"type,omitempty"`
	Value map[string]string `json:"value,omitempty"`
}

// feishuResponse is the Feishu API response body. A Code of zero denotes
// success.
type feishuResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
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
		return fmt.Sprintf("feishu: status %d: %v", e.statusCode, e.err)
	}
	return fmt.Sprintf("feishu: %v", e.err)
}

// Unwrap returns the underlying error.
func (e *httpError) Unwrap() error { return e.err }

// StatusCode reports the HTTP status of the failed response.
// Zero denotes a network error. Consumed by the delivery tracking
// decorator via tracking.ResponseCodeOf.
func (e *httpError) StatusCode() int { return e.statusCode }

// appError wraps an app-mode failure (delivery or token fetch) with the
// HTTP status and feishu API code so the retry predicate can classify
// retryability: network errors, 5xx responses, and token-related API
// codes are retryable (the token cache is invalidated on token codes so
// the retry fetches a fresh token).
type appError struct {
	statusCode int
	apiCode    int
	err        error
}

// Error implements the error interface.
func (e *appError) Error() string {
	return fmt.Sprintf("feishu: %v", e.err)
}

// Unwrap returns the underlying error.
func (e *appError) Unwrap() error { return e.err }

// StatusCode reports the HTTP status of the failed response. Zero
// denotes a network error or an API-level failure. Consumed by the
// delivery tracking decorator via tracking.ResponseCodeOf.
func (e *appError) StatusCode() int { return e.statusCode }

// isRetryableSendErr reports whether err represents a retryable Feishu
// failure. Network errors (statusCode 0) and 5xx responses are retryable;
// 4xx responses and any non-httpError (including Feishu API errors and
// response parse errors) are not. App-mode token failures (network, 5xx,
// or token-related API codes) are retryable.
func isRetryableSendErr(err error) bool {
	var he *httpError
	if errors.As(err, &he) {
		return he.statusCode == 0 || he.statusCode >= 500
	}
	var ae *appError
	if errors.As(err, &ae) {
		if _, ok := tokenErrCodes[ae.apiCode]; ok {
			return true
		}
		return ae.statusCode == 0 || ae.statusCode >= 500
	}
	return false
}
