// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package wecom

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
const typeName = "wecom"

// WeCom API path constants used by app mode.
const (
	pathGetToken    = "/cgi-bin/gettoken" //nolint:gosec // API path constant, not a credential
	pathMessageSend = "/cgi-bin/message/send"
)

// errcode values returned by the WeCom API that indicate an invalid or
// expired access_token. When such an error is returned the cached token
// is invalidated so the next attempt fetches a fresh one, and the send is
// retried.
const (
	errcodeInvalidToken = 40014
	errcodeTokenExpired = 42001
)

// tokenResponse is the JSON body returned by the gettoken API.
type tokenResponse struct {
	ErrCode     int    `json:"errcode"`
	ErrMsg      string `json:"errmsg"`
	AccessToken string `json:"access_token"`
	ExpiresIn   int64  `json:"expires_in"`
}

// apiResponse is the common JSON envelope returned by WeCom APIs. Non-zero
// ErrCode indicates an error.
type apiResponse struct {
	ErrCode int    `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
}

// robotTextPayload is the robot webhook text message body.
type robotTextPayload struct {
	MsgType string        `json:"msgtype"`
	Text    robotTextBody `json:"text"`
}

type robotTextBody struct {
	Content string `json:"content"`
}

// robotMarkdownPayload is the robot webhook markdown message body.
type robotMarkdownPayload struct {
	MsgType  string            `json:"msgtype"`
	Markdown robotMarkdownBody `json:"markdown"`
}

type robotMarkdownBody struct {
	Content string `json:"content"`
}

// appTextPayload is the application text message body.
type appTextPayload struct {
	ToUser  string      `json:"touser"`
	MsgType string      `json:"msgtype"`
	AgentID int         `json:"agentid"`
	Text    appTextBody `json:"text"`
}

type appTextBody struct {
	Content string `json:"content"`
}

// appMarkdownPayload is the application markdown message body.
type appMarkdownPayload struct {
	ToUser   string          `json:"touser"`
	MsgType  string          `json:"msgtype"`
	AgentID  int             `json:"agentid"`
	Markdown appMarkdownBody `json:"markdown"`
}

type appMarkdownBody struct {
	Content string `json:"content"`
}

// Channel sends alert notifications to WeCom (企业微信). It supports two
// delivery modes configured via Config.Mode: robot (webhook) and app
// (application message). It satisfies the alert.Channel interface.
//
// All sends are protected by a circuit breaker and retried with
// exponential backoff. App mode caches the access_token and refreshes it
// proactively before expiry; token-related API errors invalidate the
// cache and trigger a refresh on the next retry.
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

	// access_token cache (app mode). Protected by tokenMu.
	tokenMu       sync.Mutex
	accessToken   string
	tokenExpireAt time.Time
}

// Compile-time assertion that Channel implements alert.Channel.
var _ alert.Channel = (*Channel)(nil)

// Name implements alert.Channel.
func (c *Channel) Name() string { return typeName }

// Send implements alert.Channel. It dispatches the alert to the
// configured delivery mode (robot or app), wrapping the send in a retry
// loop and guarding it with a circuit breaker.
//
// When the breaker is open Send short-circuits with channel.ErrCircuitOpen.
// Transient failures (5xx, network errors, and token-related API errors)
// are retried with exponential backoff and full jitter; 4xx responses and
// non-token API errors fail immediately without retrying. On success the
// breaker is reset; on failure the breaker records a failure and a
// *channel.SendError is returned indicating whether the failure is
// retryable.
func (c *Channel) Send(ctx context.Context, evt alert.Event) error {
	var sendFn func(context.Context, alert.Event) error
	switch c.cfg.Mode {
	case ModeRobot:
		sendFn = c.sendRobot
	case ModeApp:
		sendFn = c.sendApp
	default:
		// Defensive guard for direct construction without a validated
		// Config. New rejects this, but a zero-value Channel may still
		// be constructed directly.
		return channel.NewSendError(c.Name(), false, errors.New("wecom: mode not configured"))
	}

	if !c.circuit.Allow() {
		c.logger.Debug("wecom send suppressed: circuit breaker open")
		return channel.ErrCircuitOpen
	}

	err := c.retry.Do(ctx, func() error {
		return sendFn(ctx, evt)
	})
	if err != nil {
		c.circuit.RecordFailure()
		c.logger.Warn("wecom send failed",
			zap.Error(err),
			zap.Bool("retryable", isRetryable(err)),
		)
		return channel.NewSendError(c.Name(), isRetryable(err), err)
	}
	c.circuit.RecordSuccess()
	return nil
}

// sendRobot delivers the alert to the configured robot webhook URL. The
// alert is formatted as text or markdown according to cfg.MessageType and
// POSTed as JSON. A non-zero errcode in the response is an error.
func (c *Channel) sendRobot(ctx context.Context, evt alert.Event) error {
	body, err := c.buildRobotPayload(ctx, evt)
	if err != nil {
		return &apiError{statusCode: 0, err: fmt.Errorf("build robot payload: %w", err)}
	}
	statusCode, respBody, err := c.doPost(ctx, c.cfg.RobotWebhookURL, body)
	if err != nil {
		return err
	}
	if statusCode < 200 || statusCode >= 300 {
		return &apiError{statusCode: statusCode}
	}
	var ar apiResponse
	if jerr := sonic.Unmarshal(respBody, &ar); jerr != nil {
		return &apiError{statusCode: statusCode, err: fmt.Errorf("decode robot response: %w", jerr)}
	}
	if ar.ErrCode != 0 {
		return &apiError{statusCode: statusCode, errcode: ar.ErrCode, errmsg: ar.ErrMsg}
	}
	return nil
}

// sendApp delivers the alert via the WeCom application API. It first
// obtains a valid access_token (from cache or by refreshing), then POSTs
// the formatted message to the message/send endpoint. A token-related
// errcode invalidates the cache so the next retry fetches a fresh token.
func (c *Channel) sendApp(ctx context.Context, evt alert.Event) error {
	token, err := c.getAccessToken(ctx)
	if err != nil {
		return err
	}
	body, err := c.buildAppPayload(ctx, evt)
	if err != nil {
		return &apiError{statusCode: 0, err: fmt.Errorf("build app payload: %w", err)}
	}

	sendURL, err := url.JoinPath(c.baseURL, pathMessageSend)
	if err != nil {
		return &apiError{statusCode: 0, err: fmt.Errorf("build send url: %w", err)}
	}
	q := url.Values{}
	q.Set("access_token", token)
	sendURL = sendURL + "?" + q.Encode()

	statusCode, respBody, err := c.doPost(ctx, sendURL, body)
	if err != nil {
		return err
	}
	if statusCode < 200 || statusCode >= 300 {
		return &apiError{statusCode: statusCode}
	}
	var ar apiResponse
	if jerr := sonic.Unmarshal(respBody, &ar); jerr != nil {
		return &apiError{statusCode: statusCode, err: fmt.Errorf("decode app response: %w", jerr)}
	}
	if ar.ErrCode != 0 {
		// Token-related errors invalidate the cache so the next attempt
		// (via retry) fetches a fresh token.
		if isTokenErrCode(ar.ErrCode) {
			c.invalidateToken()
		}
		return &apiError{statusCode: statusCode, errcode: ar.ErrCode, errmsg: ar.ErrMsg}
	}
	return nil
}

// getAccessToken returns a valid access_token, refreshing it from the
// WeCom API when the cache is empty or about to expire. The cache is
// protected by tokenMu so concurrent sends share a single token and only
// one refresh is in flight at a time.
func (c *Channel) getAccessToken(ctx context.Context) (string, error) {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()
	if c.accessToken != "" && time.Now().Before(c.tokenExpireAt) {
		return c.accessToken, nil
	}
	token, expiresIn, err := c.fetchToken(ctx)
	if err != nil {
		return "", err
	}
	c.accessToken = token
	expiry := time.Duration(expiresIn) * time.Second
	if expiry > tokenRefreshLeadTime {
		expiry -= tokenRefreshLeadTime
	}
	c.tokenExpireAt = time.Now().Add(expiry)
	return c.accessToken, nil
}

// invalidateToken clears the cached access_token so the next getAccessToken
// call fetches a fresh one.
func (c *Channel) invalidateToken() {
	c.tokenMu.Lock()
	c.accessToken = ""
	c.tokenExpireAt = time.Time{}
	c.tokenMu.Unlock()
}

// fetchToken calls the WeCom gettoken API and returns the access_token and
// its expiry in seconds.
func (c *Channel) fetchToken(ctx context.Context) (token string, expiresIn int64, err error) {
	tokenURL, err := url.JoinPath(c.baseURL, pathGetToken)
	if err != nil {
		return "", 0, &apiError{statusCode: 0, err: fmt.Errorf("build token url: %w", err)}
	}
	q := url.Values{}
	q.Set("corpid", c.cfg.CorpID)
	q.Set("corpsecret", c.cfg.Secret)
	tokenURL = tokenURL + "?" + q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, tokenURL, http.NoBody)
	if err != nil {
		return "", 0, &apiError{statusCode: 0, err: fmt.Errorf("build token request: %w", err)}
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", 0, &apiError{statusCode: 0, err: err}
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()
	data, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return "", 0, &apiError{statusCode: resp.StatusCode, err: fmt.Errorf("read token response: %w", readErr)}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", 0, &apiError{statusCode: resp.StatusCode}
	}
	var tr tokenResponse
	if jerr := sonic.Unmarshal(data, &tr); jerr != nil {
		return "", 0, &apiError{statusCode: resp.StatusCode, err: fmt.Errorf("decode token response: %w", jerr)}
	}
	if tr.ErrCode != 0 {
		return "", 0, &apiError{statusCode: resp.StatusCode, errcode: tr.ErrCode, errmsg: tr.ErrMsg}
	}
	if tr.AccessToken == "" {
		return "", 0, &apiError{statusCode: resp.StatusCode, err: errors.New("wecom: empty access_token in response")}
	}
	return tr.AccessToken, tr.ExpiresIn, nil
}

// doPost performs an HTTP POST with a JSON body and returns the response
// status code and body. Network errors are returned as an *apiError with a
// zero statusCode; the caller is responsible for interpreting the status
// code and response body.
func (c *Channel) doPost(ctx context.Context, target string, body []byte) (status int, respBody []byte, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return 0, nil, &apiError{statusCode: 0, err: fmt.Errorf("build request: %w", err)}
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, nil, &apiError{statusCode: 0, err: err}
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()
	data, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return resp.StatusCode, nil, &apiError{
			statusCode: resp.StatusCode,
			err:        fmt.Errorf("read response: %w", readErr),
		}
	}
	return resp.StatusCode, data, nil
}

// renderAlert converts an alert event into a canonical format.Message using
// the two-tier dispatch in format.Render: template-based (Library) or
// formatter-based (Formatter). The rendered Message is then adapted by
// the formatTextMessage and formatMarkdownMessage helpers into
// WeCom-specific payloads.
func (c *Channel) renderAlert(ctx context.Context, evt alert.Event) format.Message {
	return format.Render(ctx, evt, format.RenderOptions{
		Formatter: c.formatter,
		Library:   c.library,
		Registry:  c.registry,
		Logger:    c.logger,
		Scope:     c.cfg.Scope,
	})
}

// buildRobotPayload marshals the alert into a robot webhook message body
// according to cfg.MessageType.
func (c *Channel) buildRobotPayload(ctx context.Context, evt alert.Event) ([]byte, error) {
	msg := c.renderAlert(ctx, evt)
	switch c.cfg.MessageType {
	case MessageTypeMarkdown:
		payload := robotMarkdownPayload{
			MsgType:  string(MessageTypeMarkdown),
			Markdown: robotMarkdownBody{Content: format.RenderMarkdown(msg, wecomMarkdownStyle)},
		}
		return sonic.Marshal(payload)
	default:
		payload := robotTextPayload{
			MsgType: string(MessageTypeText),
			Text:    robotTextBody{Content: format.RenderText(msg)},
		}
		return sonic.Marshal(payload)
	}
}

// buildAppPayload marshals the alert into an application message body.
// With the interactive-card policy enabled it renders a button_interaction
// template card (whose button keys carry the action verb and EventID, and
// whose task_id is the EventID); otherwise it falls back to
// cfg.MessageType (text or markdown).
func (c *Channel) buildAppPayload(ctx context.Context, evt alert.Event) ([]byte, error) {
	msg := c.renderAlert(ctx, evt)
	if buttons := c.cfg.Interaction.ButtonsFor(evt); len(buttons) > 0 {
		return buildTemplateCardPayload(c.cfg.ToUser, c.cfg.AgentID, msg, evt, buttons)
	}
	switch c.cfg.MessageType {
	case MessageTypeMarkdown:
		payload := appMarkdownPayload{
			ToUser:   c.cfg.ToUser,
			MsgType:  string(MessageTypeMarkdown),
			AgentID:  c.cfg.AgentID,
			Markdown: appMarkdownBody{Content: format.RenderMarkdown(msg, wecomMarkdownStyle)},
		}
		return sonic.Marshal(payload)
	default:
		payload := appTextPayload{
			ToUser:  c.cfg.ToUser,
			MsgType: string(MessageTypeText),
			AgentID: c.cfg.AgentID,
			Text:    appTextBody{Content: format.RenderText(msg)},
		}
		return sonic.Marshal(payload)
	}
}

// buildTemplateCardPayload renders the canonical Message as a WeCom
// button_interaction template card. Each button's key is
// "<action>:<event_id>" — the format the inbound card event echoes back
// as EventKey. The task_id (required by WeCom and unique per card) reuses
// the alert's EventID.
func buildTemplateCardPayload(
	toUser string, agentID int, msg format.Message, evt alert.Event, buttons []format.ActionButton,
) ([]byte, error) {
	card := templateCard{
		CardType:  "button_interaction",
		Source:    cardSource{Desc: "Tickraft Alert"},
		MainTitle: cardTitle{Title: msg.Title},
		SubTitleText: strings.Join([]string{
			msg.Description,
			"Level: " + msg.Level,
			"Time: " + msg.Timestamp.Format(time.RFC3339),
		}, "\n"),
		TaskID: evt.EventID,
	}
	card.ButtonList = make([]cardButton, 0, len(buttons))
	for _, b := range buttons {
		card.ButtonList = append(card.ButtonList, cardButton{
			Text:  b.Label,
			Style: 1,
			Key:   b.Action + ":" + evt.EventID,
		})
	}
	payload := appTemplateCardPayload{
		ToUser:       toUser,
		MsgType:      "template_card",
		AgentID:      agentID,
		TemplateCard: card,
	}
	return sonic.Marshal(payload)
}

// appTemplateCardPayload is the application template_card message body.
type appTemplateCardPayload struct {
	ToUser       string       `json:"touser"`
	MsgType      string       `json:"msgtype"`
	AgentID      int          `json:"agentid"`
	TemplateCard templateCard `json:"template_card"`
}

// templateCard is the button_interaction card body.
type templateCard struct {
	CardType     string       `json:"card_type"`
	Source       cardSource   `json:"source"`
	MainTitle    cardTitle    `json:"main_title"`
	SubTitleText string       `json:"sub_title_text"`
	ButtonList   []cardButton `json:"button_list"`
	TaskID       string       `json:"task_id"`
}

type cardSource struct {
	Desc string `json:"desc"`
}

type cardTitle struct {
	Title string `json:"title"`
}

type cardButton struct {
	Text  string `json:"text"`
	Style int    `json:"style"`
	Key   string `json:"key"`
}

// wecomMarkdownStyle is the MarkdownStyle used to render WeCom markdown
// messages: a level-2 title followed by blockquote body lines.
var wecomMarkdownStyle = format.MarkdownStyle{
	HeaderFormat: "## Tickraft Alert: %s\n",
	LinePrefix:   "> ",
	LinkFormat:   "> [View Asset](%s)\n",
}

// isTokenErrCode reports whether errcode indicates an invalid or expired
// access_token.
func isTokenErrCode(errcode int) bool {
	return errcode == errcodeInvalidToken || errcode == errcodeTokenExpired
}

// apiError wraps a WeCom API or HTTP failure with the response status code
// and, when available, the WeCom errcode. A statusCode of zero denotes a
// network error. errcode is non-zero only when the API returned a JSON
// body with an errcode field.
type apiError struct {
	// statusCode is the HTTP status code; 0 denotes a network error.
	statusCode int
	// errcode is the WeCom API errcode; 0 when not applicable.
	errcode int
	// errmsg is the WeCom API errmsg.
	errmsg string
	// err is the underlying error (e.g. a network error).
	err error
}

// Error implements the error interface.
func (e *apiError) Error() string {
	switch {
	case e.statusCode > 0 && e.errcode != 0:
		return fmt.Sprintf("wecom: status %d, errcode %d: %s", e.statusCode, e.errcode, e.errmsg)
	case e.errcode != 0:
		return fmt.Sprintf("wecom: errcode %d: %s", e.errcode, e.errmsg)
	case e.statusCode > 0 && e.err != nil:
		return fmt.Sprintf("wecom: status %d: %v", e.statusCode, e.err)
	case e.statusCode > 0:
		return fmt.Sprintf("wecom: status %d", e.statusCode)
	case e.err != nil:
		return fmt.Sprintf("wecom: %v", e.err)
	default:
		return "wecom: unknown error"
	}
}

// Unwrap returns the underlying error, allowing errors.Is and errors.As
// to traverse into the wrapped error.
func (e *apiError) Unwrap() error { return e.err }

// StatusCode reports the HTTP status of the failed response.
// Zero denotes a network error. Consumed by the delivery tracking
// decorator via tracking.ResponseCodeOf.
func (e *apiError) StatusCode() int { return e.statusCode }

// isRetryable reports whether err represents a retryable wecom failure.
// Network errors (statusCode 0) and 5xx responses are retryable; 4xx
// responses are not. On a 2xx response with a non-zero errcode, only
// token-related errcodes are retryable (they trigger a token refresh);
// all other API errors fail fast.
func isRetryable(err error) bool {
	var ae *apiError
	if !errors.As(err, &ae) {
		return false
	}
	if ae.statusCode == 0 || ae.statusCode >= 500 {
		return true
	}
	if ae.statusCode >= 400 {
		return false
	}
	// 2xx with a non-zero errcode: only token errors are retryable.
	return isTokenErrCode(ae.errcode)
}
