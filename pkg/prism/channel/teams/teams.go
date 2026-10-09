// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package teams

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

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

// teamsErrorBodyMarker is the substring Teams returns in the response body
// when a MessageCard fails server-side validation (e.g. missing summary).
const teamsErrorBodyMarker = "Summary"

// Channel sends alert notifications as Microsoft Teams MessageCard payloads
// to a configured Incoming Webhook URL. It satisfies the alert.Channel
// interface.
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
const typeName = "teams"

// Name implements alert.Channel.
func (c *Channel) Name() string { return typeName }

// Send implements alert.Channel. It formats the alert as a Microsoft
// Teams MessageCard and POSTs it to the configured webhook URL.
//
// The send is protected by a circuit breaker: when the breaker is open Send
// short-circuits with channel.ErrCircuitOpen. Transient failures (5xx and
// network errors) are retried with exponential backoff and full jitter; 4xx
// responses and Teams validation errors fail immediately without retrying.
// On success the breaker is reset; on failure the breaker records a failure
// and a *channel.SendError is returned indicating whether the failure is
// retryable.
func (c *Channel) Send(ctx context.Context, evt alert.Event) error {
	if c.cfg.WebhookURL == "" {
		return channel.NewSendError(c.Name(), false, errors.New("teams: webhook url not configured"))
	}
	if !c.circuit.Allow() {
		c.logger.Debug("teams send suppressed: circuit breaker open")
		return channel.ErrCircuitOpen
	}
	body, err := c.formatMessageCard(ctx, evt)
	if err != nil {
		c.circuit.RecordFailure()
		return channel.NewSendError(c.Name(), false, fmt.Errorf("teams: build message card: %w", err))
	}
	err = c.retry.Do(ctx, func() error {
		return c.doSend(ctx, body)
	})
	if err != nil {
		c.circuit.RecordFailure()
		c.logger.Warn("teams send failed",
			zap.Error(err),
			zap.Bool("retryable", isRetryableSendErr(err)),
		)
		return channel.NewSendError(c.Name(), isRetryableSendErr(err), err)
	}
	c.circuit.RecordSuccess()
	return nil
}

// doSend performs a single HTTP POST with the pre-built MessageCard JSON
// body. It returns an *httpError for non-2xx responses, Teams validation
// errors, and network errors so the retry predicate can classify
// retryability.
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
		return &httpError{statusCode: resp.StatusCode, err: fmt.Errorf("read response body: %w", readErr)}
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		// Teams responds 200 even for some validation errors; detect
		// them via the response body marker.
		if isTeamsErrorBody(respBody) {
			return &httpError{
				statusCode: resp.StatusCode,
				err:        fmt.Errorf("teams validation error: %s", strings.TrimSpace(string(respBody))),
			}
		}
		return nil
	}
	return &httpError{
		statusCode: resp.StatusCode,
		err: fmt.Errorf("status %d from %s: %s",
			resp.StatusCode, c.cfg.WebhookURL, strings.TrimSpace(string(respBody))),
	}
}

// isTeamsErrorBody reports whether the Teams response body indicates a
// server-side validation failure (e.g. missing summary field).
func isTeamsErrorBody(body []byte) bool {
	return bytes.Contains(bytes.TrimSpace(body), []byte(teamsErrorBodyMarker))
}

// httpError wraps an HTTP failure with the response status code so the
// retry predicate can distinguish retryable (5xx, network) from
// non-retryable (4xx, validation) failures. A statusCode of zero denotes a
// network error.
type httpError struct {
	statusCode int
	err        error
}

// Error implements the error interface.
func (e *httpError) Error() string {
	if e.statusCode > 0 {
		return fmt.Sprintf("teams: status %d: %v", e.statusCode, e.err)
	}
	return fmt.Sprintf("teams: %v", e.err)
}

// Unwrap returns the underlying error.
func (e *httpError) Unwrap() error { return e.err }

// StatusCode reports the HTTP status of the failed response.
// Zero denotes a network error. Consumed by the delivery tracking
// decorator via tracking.ResponseCodeOf.
func (e *httpError) StatusCode() int { return e.statusCode }

// isRetryableSendErr reports whether err represents a retryable Teams
// failure. Network errors (statusCode 0) and 5xx responses are retryable;
// 4xx responses, Teams validation errors (2xx with error body), and any
// non-httpError are not.
func isRetryableSendErr(err error) bool {
	var he *httpError
	if errors.As(err, &he) {
		if he.statusCode == 0 {
			return true
		}
		return he.statusCode >= 500
	}
	return false
}

// ---------------------------------------------------------------------------
// MessageCard formatting
// ---------------------------------------------------------------------------

// messageCard is the Microsoft Teams MessageCard payload schema.
type messageCard struct {
	Type       string    `json:"@type"`
	Context    string    `json:"@context"`
	ThemeColor string    `json:"theme_color,omitempty"`
	Summary    string    `json:"summary"`
	Title      string    `json:"title"`
	Text       string    `json:"text"`
	Sections   []section `json:"sections,omitempty"`
}

// section is a MessageCard section containing a list of facts.
type section struct {
	Title string `json:"title,omitempty"`
	Facts []fact `json:"facts,omitempty"`
}

// fact is a name/value pair displayed in a MessageCard section.
type fact struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// MarshalJSON outputs the MessageCard in the format expected by the
// Microsoft Teams incoming webhook, which requires the camelCase
// "themeColor" key rather than the snake_case tag used on the struct.
func (m messageCard) MarshalJSON() ([]byte, error) {
	type alias messageCard
	aux := struct {
		alias
		ThemeColor string `json:"themeColor,omitempty"`
	}{
		alias:      alias(m),
		ThemeColor: m.ThemeColor,
	}
	return sonic.Marshal(aux)
}

// UnmarshalJSON accepts the camelCase "themeColor" key produced by the
// Microsoft Teams contract so round-trip (marshal then unmarshal) works.
func (m *messageCard) UnmarshalJSON(data []byte) error {
	type alias messageCard
	aux := struct {
		alias
		ThemeColor string `json:"themeColor,omitempty"`
	}{}
	if err := sonic.Unmarshal(data, &aux); err != nil {
		return err
	}
	*m = messageCard(aux.alias)
	m.ThemeColor = aux.ThemeColor
	return nil
}

// Alert level to theme color (hex RGB without #) mappings used to tint the
// MessageCard.
const (
	colorCritical = "FF0000" // red
	colorError    = "FF8C00" // dark orange
	colorWarning  = "FFD700" // gold
	colorInfo     = "0078D7" // blue
	colorDebug    = "808080" // gray
	colorDefault  = "0078D7" // blue
)

// Severity level names used for color mapping and alert level
// derivation. Defined as constants because they repeat across the level
// mapping and the derived-level fallback.
const (
	levelCritical = "critical"
	levelWarning  = "warning"
	levelInfo     = "info"
)

// themeColorForLevel returns the MessageCard themeColor for the given alert
// level. Unknown levels fall back to the default color.
func themeColorForLevel(level string) string {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case levelCritical, "fatal", "emergency", "severe":
		return colorCritical
	case "error", "err":
		return colorError
	case levelWarning, "warn":
		return colorWarning
	case levelInfo, "information", "notice":
		return colorInfo
	case "debug":
		return colorDebug
	default:
		return colorDefault
	}
}

// alertLevel returns the alert level string used for color mapping. Log
// alerts use the Severity field directly; metric alerts fall back to a derived
// level when Severity is empty.
func alertLevel(evt alert.Event) string {
	if alert.Severity(evt) != "" {
		return alert.Severity(evt)
	}
	if evt.Type == alert.TypeMetric {
		return levelWarning
	}
	return levelInfo
}

// formatMessageCard builds a Microsoft Teams MessageCard JSON payload from
// the alert event. It uses the shared format Render helper to produce a
// canonical Message (via template Library or i18n Formatter), then adapts
// it into a MessageCard with a level-derived themeColor, a title, a text
// summary, and a details section with structured facts.
func (c *Channel) formatMessageCard(ctx context.Context, evt alert.Event) ([]byte, error) {
	msg := format.Render(ctx, evt, format.RenderOptions{
		Formatter:       c.formatter,
		Library:         c.library,
		Registry:        c.registry,
		Logger:          c.logger,
		FrontendBaseURL: c.cfg.FrontendBaseURL,
		Scope:           c.cfg.Scope,
	})

	level := msg.Level
	if level == "" {
		level = alertLevel(evt)
	}
	summary := msg.Title
	if summary == "" {
		summary = msg.Description
	}
	if summary == "" {
		summary = string(evt.Type)
	}

	card := messageCard{
		Type:       "MessageCard",
		Context:    "http://schema.org/extensions",
		ThemeColor: themeColorForLevel(level),
		Summary:    summary,
		Title:      msg.Title,
		Text:       msg.Description,
		Sections:   buildSectionsFromMessage(msg),
	}
	return sonic.Marshal(card)
}

// buildSectionsFromMessage converts the canonical Message fields into
// MessageCard facts. Field keys are sorted for deterministic output, empty
// values are omitted, and the timestamp plus an optional asset link are
// appended.
func buildSectionsFromMessage(msg format.Message) []section {
	keys := make([]string, 0, len(msg.Fields))
	for k := range msg.Fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	facts := make([]fact, 0, len(keys)+2)
	for _, k := range keys {
		v := msg.Fields[k]
		if v == "" {
			continue
		}
		facts = append(facts, fact{Name: k, Value: v})
	}
	facts = append(facts, fact{Name: "timestamp", Value: msg.Timestamp.Format("2006-01-02 15:04:05")})
	if msg.AssetLink != "" {
		facts = append(facts, fact{Name: "asset_link", Value: msg.AssetLink})
	}
	return []section{
		{Title: "Alert Details", Facts: facts},
	}
}
