// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package prism

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/tickraft/tickraft/pkg/prism/alert"
	"github.com/tickraft/tickraft/pkg/prism/channel"
	"github.com/tickraft/tickraft/pkg/prism/channel/email"
	"github.com/tickraft/tickraft/pkg/prism/channel/webhook"
)

// BuildChannelFromRecord constructs a runtime alert.Channel from a database
// channel Record. It parses the record's Config JSON, looks up a registered
// channel factory by type, and falls back to the built-in webhook and email
// implementations.
func BuildChannelFromRecord(m *channel.Record) (alert.Channel, error) {
	if m == nil {
		return nil, fmt.Errorf("channel: build from nil record")
	}
	var cfg channel.Config
	if err := json.Unmarshal([]byte(m.Config), &cfg); err != nil {
		return nil, fmt.Errorf("parse channel config: %w", err)
	}
	normalizedType := strings.ToLower(m.Type)
	if factory := channel.LookupFactory(normalizedType); factory != nil {
		return factory(cfg)
	}
	switch normalizedType {
	case "webhook":
		return buildWebhookChannel(cfg)
	case "email":
		return buildEmailChannel(cfg)
	default:
		return nil, fmt.Errorf("unsupported channel type: %s", m.Type)
	}
}

// BuildChannelsFromRecords converts a slice of database Records into runtime
// alert.Channel instances. Records that fail to build are logged and
// skipped; the returned error is non-nil only when at least one record
// could not be built.
func BuildChannelsFromRecords(records []*channel.Record) ([]alert.Channel, error) {
	channels := make([]alert.Channel, 0, len(records))
	var errs []string
	for _, rec := range records {
		ch, err := BuildChannelFromRecord(rec)
		if err != nil {
			errs = append(errs, fmt.Sprintf("channel #%d (%s): %v", rec.ID, rec.Name, err))
			continue
		}
		channels = append(channels, ch)
	}
	if len(errs) > 0 {
		return channels, fmt.Errorf("build channels: %s", strings.Join(errs, "; "))
	}
	return channels, nil
}

func buildWebhookChannel(cfg channel.Config) (alert.Channel, error) {
	whCfg := webhook.Config{
		URL:     cfg.URL,
		Headers: cfg.Headers,
	}
	if cfg.Timeout != "" {
		d, err := time.ParseDuration(cfg.Timeout)
		if err != nil {
			return nil, fmt.Errorf("parse webhook timeout: %w", err)
		}
		whCfg.Timeout = d
	}
	return webhook.New(whCfg)
}

func buildEmailChannel(cfg channel.Config) (alert.Channel, error) {
	tlsMode, err := parseEmailTLSMode(cfg.TLSMode)
	if err != nil {
		return nil, err
	}
	authType, err := parseEmailAuthType(cfg.AuthType)
	if err != nil {
		return nil, err
	}
	return email.New(email.Config{
		Host:     cfg.Host,
		Port:     cfg.Port,
		Username: cfg.Username,
		Password: cfg.Password,
		From:     cfg.From,
		To:       cfg.To,
		TLSMode:  tlsMode,
		AuthType: authType,
		HTMLMode: cfg.HTMLMode,
	})
}

func parseEmailTLSMode(s string) (email.TLSMode, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "none":
		return email.TLSModeNone, nil
	case "implicit":
		return email.TLSModeImplicit, nil
	case "starttls":
		return email.TLSModeStartTLS, nil
	default:
		return 0, fmt.Errorf("unknown tls_mode %q (want none, implicit, or starttls)", s)
	}
}

func parseEmailAuthType(s string) (email.AuthType, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "plain":
		return email.AuthTypePlain, nil
	case "login":
		return email.AuthTypeLogin, nil
	case "cram-md5":
		return email.AuthTypeCramMD5, nil
	default:
		return 0, fmt.Errorf("unknown auth_type %q (want plain, login, or cram-md5)", s)
	}
}
