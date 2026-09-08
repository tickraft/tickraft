// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package channel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/bytedance/sonic"

	"github.com/tickraft/tickraft/pkg/errdefs"
	"github.com/tickraft/tickraft/pkg/pagination"
	"github.com/tickraft/tickraft/pkg/prism/alert"
	"github.com/tickraft/tickraft/pkg/prism/channel/tracking"
)

// ValidationError carries a channel business error code and a
// user-facing message. The HTTP layer maps it to a 400-class response
// with the code attached.
type ValidationError struct {
	Code int
	Msg  string
}

// Error implements the error interface.
func (e *ValidationError) Error() string { return e.Msg }

// ChannelService implements Service using the prism channel store and the
// delivery store. Mutating operations (Create/Update/Delete) trigger a
// hot-reload of the engine's in-memory channel list via the Runtime seam.
// The <Domain>Service name mirrors the convention of the other domain
// service implementations (see pkg/system).
//
//nolint:revive // intentional stutter: mirrors the <Domain>Service convention
type ChannelService struct {
	channels *Store
	delivery *DeliveryStore
	runtime  Runtime
}

var _ Service = (*ChannelService)(nil)

// NewChannelService creates a ChannelService backed by the given stores
// and prism runtime. The runtime is used to hot-reload channels after
// mutations and to build runtime channels for test dispatches and
// delivery retries; a nil runtime disables both (useful for tests).
func NewChannelService(channels *Store, delivery *DeliveryStore, runtime Runtime) *ChannelService {
	return &ChannelService{channels: channels, delivery: delivery, runtime: runtime}
}

// ListChannels returns all channel configurations with sensitive config
// fields masked.
func (s *ChannelService) ListChannels(ctx context.Context) ([]*Channel, error) {
	channels, err := s.channels.List(ctx)
	if err != nil {
		return nil, mapStoreError(err)
	}
	masked := make([]*Channel, 0, len(channels))
	for i := range channels {
		m, err := s.channels.MaskConfig(&channels[i])
		if err != nil {
			return nil, mapStoreError(err)
		}
		masked = append(masked, m)
	}
	return masked, nil
}

// GetChannel returns a single channel configuration with sensitive config
// fields masked.
func (s *ChannelService) GetChannel(ctx context.Context, id int64) (*Channel, error) {
	ch, err := s.channels.Get(ctx, id)
	if err != nil {
		return nil, mapStoreError(err)
	}
	return s.channels.MaskConfig(ch)
}

// CreateChannel validates and creates a new channel configuration, then
// hot-reloads the engine's channel list. Sensitive fields are encrypted at
// rest by the store; the returned echo carries masked secrets.
func (s *ChannelService) CreateChannel(ctx context.Context, req *CreateRequest) (*Channel, error) {
	if req == nil {
		return nil, errdefs.ErrInvalidRequest
	}
	if err := AssertTypeAllowed(ctx, req.Type); err != nil {
		return nil, toValidationError(err)
	}
	if err := validateInput(req.Name, req.Type, req.Config); err != nil {
		return nil, err
	}
	if err := ValidateTypeConfig(req.Type, string(req.Config)); err != nil {
		return nil, &ValidationError{Code: CodeConfigInvalid, Msg: err.Error()}
	}

	ch := Channel{
		Name:    req.Name,
		Type:    req.Type,
		Config:  string(req.Config),
		Enabled: req.Enabled,
	}
	if err := s.channels.Create(ctx, &ch); err != nil {
		return nil, mapStoreError(err)
	}
	s.reloadChannels(ctx)
	return s.channels.MaskConfig(&ch)
}

// UpdateChannel applies a partial update to the channel identified by id.
// Omitted fields keep their stored values; sensitive fields submitted
// empty or still masked ("****…") keep the stored plaintext, so an edit
// that does not re-enter a secret does not clobber it. The engine's
// channel list is hot-reloaded after a successful write.
func (s *ChannelService) UpdateChannel(ctx context.Context, id int64, req *UpdateRequest) (*Channel, error) {
	if req == nil {
		return nil, errdefs.ErrInvalidRequest
	}
	existing, err := s.channels.Get(ctx, id)
	if err != nil {
		return nil, mapStoreError(err)
	}

	resolved := resolveUpdateFields(existing, req)
	if err := AssertTypeAllowed(ctx, resolved.chType); err != nil {
		return nil, toValidationError(err)
	}
	if err := validateInput(resolved.name, resolved.chType, json.RawMessage(resolved.config)); err != nil {
		return nil, err
	}
	if err := ValidateTypeConfig(resolved.chType, resolved.config); err != nil {
		return nil, &ValidationError{Code: CodeConfigInvalid, Msg: err.Error()}
	}

	ch := Channel{
		ID:      id,
		Name:    resolved.name,
		Type:    resolved.chType,
		Config:  resolved.config,
		Enabled: resolved.enabled,
	}
	if err := s.channels.Update(ctx, &ch); err != nil {
		return nil, mapStoreError(err)
	}
	s.reloadChannels(ctx)
	return s.channels.MaskConfig(&ch)
}

// DeleteChannel removes the channel identified by id and hot-reloads the
// engine's channel list.
func (s *ChannelService) DeleteChannel(ctx context.Context, id int64) error {
	if err := s.channels.Delete(ctx, id); err != nil {
		return mapStoreError(err)
	}
	s.reloadChannels(ctx)
	return nil
}

// TestChannel sends a synthetic alert notification, either through a
// saved channel (req.ID) or an inline type+config pair.
func (s *ChannelService) TestChannel(ctx context.Context, req *TestRequest) error {
	if req == nil {
		return errdefs.ErrInvalidRequest
	}
	if s.runtime == nil {
		return errdefs.NewServiceError(http.StatusServiceUnavailable, errdefs.CodeInternal,
			"channel runtime not available")
	}

	chType, configJSON, err := s.resolveTestConfig(ctx, req)
	if err != nil {
		return err
	}

	def := &Channel{Type: chType, Config: configJSON}
	ch, err := s.runtime.BuildChannel(def)
	if err != nil {
		return &ValidationError{Code: CodeConfigInvalid, Msg: err.Error()}
	}
	return ch.Send(ctx, buildTestAlert())
}

// TestAllChannels probes every enabled saved channel concurrently with a
// synthetic alert and reports one result per channel. A channel that
// fails to build from its stored config reports OK=false with the build
// error, so configuration problems show up alongside connectivity ones.
// The returned error is non-nil only when the channel list cannot be
// loaded or no runtime is wired; individual probe failures are carried in
// the results.
func (s *ChannelService) TestAllChannels(ctx context.Context) ([]TestResult, error) {
	if s.runtime == nil {
		return nil, errdefs.NewServiceError(http.StatusServiceUnavailable, errdefs.CodeInternal,
			"channel runtime not available")
	}
	defs, err := s.channels.ListEnabled(ctx)
	if err != nil {
		return nil, mapStoreError(err)
	}

	results := make([]TestResult, len(defs))
	var wg sync.WaitGroup
	for i := range defs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = s.probeChannel(ctx, defs[i])
		}(i)
	}
	wg.Wait()
	return results, nil
}

// probeChannel builds a channel from its persisted definition and sends
// the synthetic test alert, timing the send.
func (s *ChannelService) probeChannel(ctx context.Context, def *Channel) TestResult {
	res := TestResult{ChannelID: def.ID, Name: def.Name, Type: def.Type}
	ch, err := s.runtime.BuildChannel(def)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	start := time.Now()
	if sendErr := ch.Send(ctx, buildTestAlert()); sendErr != nil {
		res.DurationMs = time.Since(start).Milliseconds()
		res.Error = sendErr.Error()
		return res
	}
	res.DurationMs = time.Since(start).Milliseconds()
	res.OK = true
	return res
}

// ListChannelOptions returns the compact projection of every channel,
// used by the deliveries page filter dropdown.
func (s *ChannelService) ListChannelOptions(ctx context.Context) ([]Option, error) {
	channels, err := s.channels.List(ctx)
	if err != nil {
		return nil, mapStoreError(err)
	}
	options := make([]Option, 0, len(channels))
	for i := range channels {
		options = append(options, Option{
			ID:      channels[i].ID,
			Name:    channels[i].Name,
			Type:    channels[i].Type,
			Enabled: channels[i].Enabled != nil && *channels[i].Enabled,
		})
	}
	return options, nil
}

// ListDeliveries returns delivery records matching params together with
// the total count (offset pagination).
func (s *ChannelService) ListDeliveries(
	ctx context.Context, params DeliveryListParams,
) ([]DeliveryRecord, int64, error) {
	return s.delivery.List(ctx, params)
}

// ListDeliveriesKeyset returns a page of delivery records using keyset
// (cursor-based) pagination.
func (s *ChannelService) ListDeliveriesKeyset(
	ctx context.Context, params DeliveryListParams,
) (pagination.PageResult[DeliveryRecord], error) {
	return s.delivery.ListKeyset(ctx, params)
}

// RetryDelivery replays a failed delivery: the persisted alert event is
// re-sent synchronously through the channel's current configuration and
// the outcome is appended to the record's attempt history. The returned
// record's Status reflects the retry outcome.
func (s *ChannelService) RetryDelivery(ctx context.Context, id int64) (*DeliveryRecord, error) {
	if s.runtime == nil {
		return nil, errdefs.NewServiceError(http.StatusServiceUnavailable, errdefs.CodeInternal,
			"channel runtime not available")
	}

	rec, err := s.delivery.Get(ctx, id)
	if err != nil {
		return nil, mapDeliveryError(err)
	}

	if msg := retryRefusal(rec); msg != "" {
		return nil, &ValidationError{Code: CodeNotRetryable, Msg: msg}
	}

	cfg, err := s.channels.Get(ctx, rec.ChannelID)
	if err != nil {
		return nil, mapStoreError(err)
	}
	if cfg.Enabled == nil || !*cfg.Enabled {
		return nil, &ValidationError{Code: CodeNotRetryable, Msg: "channel is disabled"}
	}

	ch, err := s.runtime.BuildChannel(cfg)
	if err != nil {
		return nil, &ValidationError{Code: CodeConfigInvalid, Msg: err.Error()}
	}

	var evt alert.Event
	if err := sonic.UnmarshalString(rec.RequestPayload, &evt); err != nil {
		return nil, &ValidationError{Code: CodeNotRetryable, Msg: "request payload is not a replayable alert event"}
	}

	start := time.Now()
	sendErr := ch.Send(ctx, evt)
	attempt := Attempt{
		Time:       start,
		N:          len(rec.Attempts),
		DurationMs: time.Since(start).Milliseconds(),
	}
	if sendErr == nil {
		attempt.Result = string(tracking.StatusSuccess)
		rec.Status = string(tracking.StatusSuccess)
		rec.Error = ""
		rec.ResponseCode = 0
	} else {
		attempt.Result = string(tracking.StatusFailed)
		attempt.Error = sendErr.Error()
		rec.Status = string(tracking.StatusFailed)
		rec.Error = sendErr.Error()
		rec.ResponseCode = tracking.ResponseCodeOf(sendErr)
	}
	rec.DurationMs = attempt.DurationMs
	rec.Attempts = append(rec.Attempts, attempt)

	if err := s.delivery.UpdateAttempt(ctx, rec); err != nil {
		return nil, err
	}
	return rec, nil
}

// resolvedUpdate is the effective field set produced by
// resolveUpdateFields.
type resolvedUpdate struct {
	name    string
	chType  string
	config  string
	enabled *bool
}

// resolveUpdateFields overlays an update request onto the stored channel:
// omitted or empty fields keep their stored value, and the config is
// merged so sensitive fields submitted empty or still masked keep the
// stored plaintext.
func resolveUpdateFields(existing *Channel, req *UpdateRequest) resolvedUpdate {
	res := resolvedUpdate{
		name:    req.Name,
		chType:  req.Type,
		config:  existing.Config,
		enabled: req.Enabled,
	}
	if res.name == "" {
		res.name = existing.Name
	}
	if res.chType == "" {
		res.chType = existing.Type
	}
	if res.enabled == nil {
		res.enabled = existing.Enabled
	}
	if len(req.Config) > 0 && !isEmptyJSON(req.Config) {
		merged, err := mergeMaskedSecrets(existing.Config, string(req.Config))
		if err == nil {
			res.config = merged
		}
	}
	return res
}

// mergeMaskedSecrets merges an incoming channel config over the stored
// one for sensitive fields: when the incoming value is empty or still
// carries the masked placeholder ("****…") produced by masked reads, the
// stored plaintext is kept. Non-sensitive keys pass through untouched, and
// keys the incoming config omits keep their stored values.
func mergeMaskedSecrets(stored, incoming string) (string, error) {
	var base, next map[string]any
	if err := sonic.UnmarshalString(stored, &base); err != nil {
		return "", fmt.Errorf("parse stored config: %w", err)
	}
	if err := sonic.UnmarshalString(incoming, &next); err != nil {
		return "", fmt.Errorf("parse incoming config: %w", err)
	}
	sensitive := SensitiveKeys()
	isSensitive := func(key string) bool {
		_, ok := sensitive[normalizeKey(key)]
		return ok
	}
	baseNorm := make(map[string]string, len(base))
	for key, val := range base {
		if s, ok := val.(string); ok {
			baseNorm[normalizeKey(key)] = s
		}
	}
	merged := make(map[string]any, len(base)+len(next))
	for key, val := range base {
		merged[key] = val
	}
	for key, val := range next {
		s, ok := val.(string)
		if !ok || !isSensitive(key) {
			merged[key] = val
			continue
		}
		if s == "" || isMaskedValue(s) {
			if old, exists := baseNorm[normalizeKey(key)]; exists && old != "" {
				merged[key] = old
				continue
			}
		}
		merged[key] = val
	}
	out, err := sonic.MarshalString(merged)
	if err != nil {
		return "", fmt.Errorf("marshal merged config: %w", err)
	}
	return out, nil
}

// validateInput validates the name, type, and config JSON of a
// create/update request. The config may be empty (for partial updates
// that preserve the existing value) but when present must be valid JSON.
func validateInput(name, chType string, config json.RawMessage) *ValidationError {
	if name == "" {
		return &ValidationError{Code: CodeNameEmpty, Msg: "name is required"}
	}
	if !ValidType(chType) {
		return &ValidationError{Code: CodeTypeInvalid, Msg: "invalid channel type"}
	}
	if len(config) > 0 && !isEmptyJSON(config) {
		var probe map[string]json.RawMessage
		if err := sonic.Unmarshal(config, &probe); err != nil {
			return &ValidationError{Code: CodeConfigInvalid, Msg: "config must be a valid JSON object"}
		}
	}
	return nil
}

// isEmptyJSON reports whether raw is an empty or null JSON value.
func isEmptyJSON(raw json.RawMessage) bool {
	s := string(raw)
	return s == "" || s == "null" || s == "{}"
}

// resolveTestConfig resolves the channel type and config JSON for a test
// request. When an ID is supplied the saved (decrypted) config is loaded;
// otherwise the inline type and config are validated and used.
func (s *ChannelService) resolveTestConfig(
	ctx context.Context,
	req *TestRequest,
) (chType, chConfig string, verr error) {
	if req.ID != nil {
		cfg, err := s.channels.Get(ctx, *req.ID)
		if err != nil {
			return "", "", mapStoreError(err)
		}
		return cfg.Type, cfg.Config, nil
	}

	if req.Type == "" {
		return "", "", &ValidationError{Code: CodeTypeInvalid, Msg: "type is required for inline test"}
	}
	if err := AssertTypeAllowed(ctx, req.Type); err != nil {
		return "", "", toValidationError(err)
	}
	if len(req.Config) == 0 || isEmptyJSON(req.Config) {
		return "", "", &ValidationError{Code: CodeConfigInvalid, Msg: "config is required for inline test"}
	}
	return req.Type, string(req.Config), nil
}

// retryRefusal returns a human-readable reason the delivery cannot be
// retried, or an empty string when it can.
func retryRefusal(rec *DeliveryRecord) string {
	switch {
	case rec.Status == string(tracking.StatusSuccess):
		return "delivery already succeeded"
	case rec.ChannelID == 0:
		return "delivery was made by an environment-built channel without a configuration"
	case rec.RequestPayload == "":
		return "delivery record has no replayable request payload"
	}
	return ""
}

// buildTestAlert returns a sample log alert used by the channel test
// endpoint to exercise each channel adapter.
func buildTestAlert() alert.Event {
	return alert.Event{
		Type:      alert.TypeLog,
		Timestamp: time.Now(),
		Violations: []alert.Violation{{
			Kind:     alert.ViolationKindLog,
			Severity: "info",
			Log: &alert.LogContext{
				Keyword: "[Tickraft Test]",
				Content: "this is a test alert generated by the tickraft channel test endpoint",
			},
		}},
	}
}

// reloadChannels triggers a hot-reload of the engine's channel list.
// Errors are swallowed since a reload failure does not invalidate the
// CRUD operation that triggered it.
func (s *ChannelService) reloadChannels(ctx context.Context) {
	if s.runtime == nil {
		return
	}
	_ = s.runtime.ReloadChannels(ctx)
}

// toValidationError normalizes a type-registry error into a
// ValidationError for the HTTP layer.
func toValidationError(err error) *ValidationError {
	var ve *ValidationError
	if errors.As(err, &ve) {
		return ve
	}
	return &ValidationError{Code: CodeTypeInvalid, Msg: err.Error()}
}

// mapStoreError translates a channel store error into a handler-level
// ServiceError suitable for the API response layer.
func mapStoreError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrChannelNotFound) {
		return errdefs.ErrChannelNotFound
	}
	if errors.Is(err, errdefs.ErrNotFound) {
		return errdefs.ErrChannelNotFound
	}
	return errdefs.NewServiceError(http.StatusInternalServerError, errdefs.CodeInternal, err.Error())
}

// mapDeliveryError translates a delivery store error into a handler-level
// error. A missing record is reported as a 404 with the channel business
// code attached via errdefs.
func mapDeliveryError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, errdefs.ErrNotFound) {
		return errdefs.ErrChannelNotFound
	}
	return errdefs.NewServiceError(http.StatusInternalServerError, errdefs.CodeInternal, err.Error())
}
