// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package prism implements the AIOps engine that unifies alerting,
// notification channels, rule evaluation, and self-healing remediation
// for the tickraft observability pipeline.
package prism

import (
	"fmt"
	"strings"
	"time"

	"github.com/bytedance/sonic"
	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/i18n"
	"github.com/tickraft/tickraft/pkg/prism/alert"
	"github.com/tickraft/tickraft/pkg/prism/alert/template"
	"github.com/tickraft/tickraft/pkg/prism/channel"
	"github.com/tickraft/tickraft/pkg/prism/channel/dingtalk"
	"github.com/tickraft/tickraft/pkg/prism/channel/discord"
	"github.com/tickraft/tickraft/pkg/prism/channel/email"
	"github.com/tickraft/tickraft/pkg/prism/channel/feishu"
	"github.com/tickraft/tickraft/pkg/prism/channel/format"
	"github.com/tickraft/tickraft/pkg/prism/channel/slack"
	"github.com/tickraft/tickraft/pkg/prism/channel/teams"
	"github.com/tickraft/tickraft/pkg/prism/channel/telegram"
	"github.com/tickraft/tickraft/pkg/prism/channel/tracking"
	"github.com/tickraft/tickraft/pkg/prism/channel/webhook"
	"github.com/tickraft/tickraft/pkg/prism/channel/wecom"
)

// Compile-time assertions that each built-in channel implementation
// satisfies the alert.Channel interface. A failure here surfaces at build
// time rather than at registration time.
var (
	_ alert.Channel = (*webhook.Channel)(nil)
	_ alert.Channel = (*email.Channel)(nil)
	_ alert.Channel = (*feishu.Channel)(nil)
	_ alert.Channel = (*slack.Channel)(nil)
	_ alert.Channel = (*teams.Channel)(nil)
	_ alert.Channel = (*dingtalk.Channel)(nil)
	_ alert.Channel = (*discord.Channel)(nil)
	_ alert.Channel = (*telegram.Channel)(nil)
	_ alert.Channel = (*wecom.Channel)(nil)
)

// RegisterBuiltinChannelTypes registers the nine built-in channel types
// (webhook, email, and the seven instant-messaging types) in the channel
// type registry. The composition root calls it once at startup; extension
// editions register their additional types (for example SMS) through
// channel.RegisterType with the same shape.
func RegisterBuiltinChannelTypes() {
	channel.RegisterType("webhook", channel.TypeInfo{
		Build:    buildWebhookType,
		Validate: validateFlatConfig(validateWebhookFlat),
	})
	emailInfo := channel.TypeInfo{
		Build:    buildEmailType,
		Validate: validateFlatConfig(validateEmailFlat),
	}
	// The SMTP password is a credential: without this declaration it would
	// neither be encrypted at rest nor masked in API echoes.
	emailInfo.SensitiveKeys = []string{"password"}
	channel.RegisterType("email", emailInfo)
	dingtalkInfo := imTypeInfo(func(cfg dingtalk.Config, deps renderDeps) (alert.Channel, error) {
		cfg.ProxyURL = deps.resolveProxy(cfg.ProxyURL)
		cfg.ProxyBypass = deps.proxyBypass
		cfg.Scope = deps.scope
		return dingtalk.New(cfg, dingtalk.WithLogger(deps.logger),
			dingtalk.WithFormatter(deps.formatter), dingtalk.WithLibrary(deps.library),
			dingtalk.WithRegistry(deps.registry))
	})
	dingtalkInfo.SensitiveKeys = []string{"app_secret"}
	channel.RegisterType("dingtalk", dingtalkInfo)
	channel.RegisterType("discord", imTypeInfo(func(cfg discord.Config, deps renderDeps) (alert.Channel, error) {
		cfg.ProxyURL = deps.resolveProxy(cfg.ProxyURL)
		cfg.ProxyBypass = deps.proxyBypass
		cfg.Scope = deps.scope
		return discord.New(cfg, discord.WithLogger(deps.logger),
			discord.WithFormatter(deps.formatter), discord.WithLibrary(deps.library),
			discord.WithRegistry(deps.registry))
	}))
	feishuInfo := imTypeInfo(func(cfg feishu.Config, deps renderDeps) (alert.Channel, error) {
		cfg.ProxyURL = deps.resolveProxy(cfg.ProxyURL)
		cfg.ProxyBypass = deps.proxyBypass
		cfg.Scope = deps.scope
		cfg.Interaction = deps.interaction
		return feishu.New(cfg, feishu.WithLogger(deps.logger),
			feishu.WithFormatter(deps.formatter), feishu.WithLibrary(deps.library),
			feishu.WithRegistry(deps.registry), feishu.WithPlainNotificationOnly(deps.plainOnly))
	})
	feishuInfo.SensitiveKeys = []string{"app_secret"}
	channel.RegisterType("feishu", feishuInfo)
	channel.RegisterType("slack", imTypeInfo(func(cfg slack.Config, deps renderDeps) (alert.Channel, error) {
		cfg.ProxyURL = deps.resolveProxy(cfg.ProxyURL)
		cfg.ProxyBypass = deps.proxyBypass
		cfg.Scope = deps.scope
		return slack.New(cfg, slack.WithLogger(deps.logger),
			slack.WithFormatter(deps.formatter), slack.WithLibrary(deps.library),
			slack.WithRegistry(deps.registry))
	}))
	channel.RegisterType("teams", imTypeInfo(func(cfg teams.Config, deps renderDeps) (alert.Channel, error) {
		cfg.ProxyURL = deps.resolveProxy(cfg.ProxyURL)
		cfg.ProxyBypass = deps.proxyBypass
		cfg.Scope = deps.scope
		return teams.New(cfg, teams.WithLogger(deps.logger),
			teams.WithFormatter(deps.formatter), teams.WithLibrary(deps.library),
			teams.WithRegistry(deps.registry))
	}))
	channel.RegisterType("telegram", imTypeInfo(func(cfg telegram.Config, deps renderDeps) (alert.Channel, error) {
		cfg.ProxyURL = deps.resolveProxy(cfg.ProxyURL)
		cfg.ProxyBypass = deps.proxyBypass
		cfg.Scope = deps.scope
		return telegram.New(cfg, telegram.WithLogger(deps.logger),
			telegram.WithFormatter(deps.formatter), telegram.WithLibrary(deps.library),
			telegram.WithRegistry(deps.registry))
	}))
	wecomInfo := imTypeInfo(func(cfg wecom.Config, deps renderDeps) (alert.Channel, error) {
		cfg.ProxyURL = deps.resolveProxy(cfg.ProxyURL)
		cfg.ProxyBypass = deps.proxyBypass
		cfg.Scope = deps.scope
		cfg.Interaction = deps.interaction
		return wecom.New(cfg, wecom.WithLogger(deps.logger),
			wecom.WithFormatter(deps.formatter), wecom.WithLibrary(deps.library),
			wecom.WithRegistry(deps.registry))
	})
	wecomInfo.SensitiveKeys = []string{"callback_token", "encoding_aes_key"}
	channel.RegisterType("wecom", wecomInfo)
}

// renderDeps is the flattened view of channel.BuildOptions passed to the
// per-type constructors.
type renderDeps struct {
	logger    *zap.Logger
	formatter i18n.Formatter
	library   template.Library
	registry  i18n.Registry
	// proxyURL/proxyBypass mirror BuildOptions' deployment-wide egress
	// policy; plainOnly mirrors the L0 plain-notification policy; scope
	// mirrors the L1 network-scope rendering policy; interaction mirrors
	// the L1 interactive-card policy.
	proxyURL    string
	proxyBypass []string
	plainOnly   bool
	scope       format.ScopeOptions
	interaction format.InteractionOptions
}

// renderDepsOf normalizes a BuildOptions into renderDeps, defaulting the
// logger to a no-op.
func renderDepsOf(opts channel.BuildOptions) renderDeps {
	logger := opts.Logger
	if logger == nil {
		logger = zap.NewNop()
	}
	return renderDeps{
		logger:      logger,
		formatter:   opts.Formatter,
		library:     opts.Library,
		registry:    opts.Registry,
		proxyURL:    opts.ProxyURL,
		proxyBypass: opts.ProxyBypass,
		plainOnly:   opts.PlainNotificationOnly,
		scope:       opts.Scope,
		interaction: opts.Interaction,
	}
}

// resolveProxy returns the effective proxy URL for one channel: a
// per-channel proxy_url wins over the deployment-wide proxy. The bypass
// list is deployment-level only (per-channel granularity is "set your own
// proxy or leave proxy_url empty"), so closures assign
// deps.proxyBypass directly.
func (d renderDeps) resolveProxy(perChannel string) string {
	if perChannel == "" {
		return d.proxyURL
	}
	return perChannel
}

// validatableConfig is the contract every channel type's Config satisfies:
// self-validation on the value receiver.
type validatableConfig interface{ Validate() error }

// parseTypedConfig decodes a stored config JSON into the type's Config.
func parseTypedConfig[T any](chType, configJSON string) (T, error) {
	var cfg T
	if err := sonic.UnmarshalString(configJSON, &cfg); err != nil {
		return cfg, fmt.Errorf("parse %s config: %w", chType, err)
	}
	return cfg, nil
}

// imTypeInfo builds the registry entry for an instant-messaging channel
// type: parsing and validation come from the type's own Config, and
// construct adapts the package constructor to the shared render
// collaborators.
func imTypeInfo[C validatableConfig](construct func(C, renderDeps) (alert.Channel, error)) channel.TypeInfo {
	return channel.TypeInfo{
		Build: func(configJSON string, opts channel.BuildOptions) (alert.Channel, error) {
			cfg, err := parseTypedConfig[C]("channel", configJSON)
			if err != nil {
				return nil, err
			}
			return construct(cfg, renderDepsOf(opts))
		},
		Validate: func(configJSON string) error {
			cfg, err := parseTypedConfig[C]("channel", configJSON)
			if err != nil {
				return err
			}
			return cfg.Validate()
		},
	}
}

// buildWebhookType constructs a webhook channel from the flat wire Config.
func buildWebhookType(configJSON string, opts channel.BuildOptions) (alert.Channel, error) {
	cfg, err := parseTypedConfig[channel.Config]("webhook", configJSON)
	if err != nil {
		return nil, err
	}
	return buildWebhookChannel(cfg, opts)
}

// buildEmailType constructs an email channel from the flat wire Config.
func buildEmailType(configJSON string, opts channel.BuildOptions) (alert.Channel, error) {
	cfg, err := parseTypedConfig[channel.Config]("email", configJSON)
	if err != nil {
		return nil, err
	}
	return buildEmailChannel(cfg, opts)
}

// validateFlatConfig validates a webhook/email request against the flat
// wire Config and the type-specific semantic checks.
func validateFlatConfig(semantic func(channel.Config) error) channel.ValidateFunc {
	return func(configJSON string) error {
		cfg, err := parseTypedConfig[channel.Config]("channel", configJSON)
		if err != nil {
			return err
		}
		return semantic(cfg)
	}
}

// validateWebhookFlat applies the webhook semantics to a parsed flat
// config.
func validateWebhookFlat(cfg channel.Config) error {
	whCfg, err := webhookConfigOf(cfg)
	if err != nil {
		return err
	}
	return whCfg.Validate()
}

// validateEmailFlat applies the email semantics to a parsed flat config.
func validateEmailFlat(cfg channel.Config) error {
	emailCfg, err := emailConfigOf(cfg)
	if err != nil {
		return err
	}
	return emailCfg.Validate()
}

// BuildChannel constructs a runtime alert.Channel from a persisted channel
// definition through the type registry. When deliveries is non-nil the
// built channel is wrapped with the delivery-tracking decorator tagged
// with the definition's identity, so every Send outcome is recorded in
// sys_prism_delivery. A nil deliveries disables tracking.
func BuildChannel(
	def *channel.Channel,
	opts channel.BuildOptions,
	deliveries tracking.DeliveryRecordStore,
) (alert.Channel, error) {
	if def == nil {
		return nil, fmt.Errorf("channel: build from nil channel")
	}
	ch, err := buildChannelOfType(def.Type, def.Config, opts)
	if err != nil {
		return nil, err
	}
	if deliveries == nil {
		return ch, nil
	}
	deps := renderDepsOf(opts)
	return tracking.New(ch, deliveries, deps.logger, channelIdentity(def), format.RenderOptions{
		Formatter: deps.formatter,
		Library:   deps.library,
		Registry:  deps.registry,
		Logger:    deps.logger,
	}), nil
}

// buildChannelOfType builds an unwrapped channel of the given type through
// the registry. It backs the test-dispatch and retry paths, which record
// outcomes themselves and therefore must not double-record through the
// tracking decorator.
func buildChannelOfType(chType, configJSON string, opts channel.BuildOptions) (alert.Channel, error) {
	info, ok := channel.LookupType(chType)
	if !ok {
		return nil, fmt.Errorf("channel: unsupported channel type %q", chType)
	}
	return info.Build(configJSON, opts)
}

// channelIdentity normalizes a persisted definition into the delivery
// record identity.
func channelIdentity(def *channel.Channel) tracking.Identity {
	name := def.Name
	if name == "" {
		name = def.Type
	}
	return tracking.Identity{ChannelID: def.ID, ChannelName: name, ChannelType: def.Type}
}

// BuildChannels converts a slice of persisted channel definitions into
// runtime alert.Channel instances (see BuildChannel). Channels that fail
// to build are skipped; the returned error is non-nil only when at least
// one channel could not be built, so callers can warn without aborting.
func BuildChannels(
	defs []*channel.Channel,
	opts channel.BuildOptions,
	deliveries tracking.DeliveryRecordStore,
) ([]alert.Channel, error) {
	channels := make([]alert.Channel, 0, len(defs))
	var errs []string
	for _, def := range defs {
		ch, err := BuildChannel(def, opts, deliveries)
		if err != nil {
			errs = append(errs, fmt.Sprintf("channel #%d (%s): %v", def.ID, def.Name, err))
			continue
		}
		channels = append(channels, ch)
	}
	if len(errs) > 0 {
		return channels, fmt.Errorf("build channels: %s", strings.Join(errs, "; "))
	}
	return channels, nil
}

// buildWebhookChannel maps the flat wire Config onto the webhook package's
// Config.
func buildWebhookChannel(cfg channel.Config, opts channel.BuildOptions) (alert.Channel, error) {
	whCfg, err := webhookConfigOf(cfg)
	if err != nil {
		return nil, err
	}
	deps := renderDepsOf(opts)
	whCfg.ProxyURL = deps.resolveProxy(whCfg.ProxyURL)
	whCfg.ProxyBypass = deps.proxyBypass
	return webhook.New(whCfg, webhook.WithLogger(deps.logger))
}

// webhookConfigOf converts the flat wire config into the webhook Config.
func webhookConfigOf(cfg channel.Config) (webhook.Config, error) {
	whCfg := webhook.Config{
		URL:      cfg.URL,
		Headers:  cfg.Headers,
		ProxyURL: cfg.ProxyURL,
	}
	if cfg.Timeout != "" {
		d, err := parseChannelTimeout(cfg.Timeout)
		if err != nil {
			return webhook.Config{}, fmt.Errorf("parse webhook timeout: %w", err)
		}
		whCfg.Timeout = d
	}
	return whCfg, nil
}

// buildEmailChannel maps the flat wire Config onto the email package's
// Config.
func buildEmailChannel(cfg channel.Config, opts channel.BuildOptions) (alert.Channel, error) {
	emailCfg, err := emailConfigOf(cfg)
	if err != nil {
		return nil, err
	}
	deps := renderDepsOf(opts)
	emailCfg.ProxyURL = deps.resolveProxy(emailCfg.ProxyURL)
	emailCfg.Scope = deps.scope
	return email.New(emailCfg, email.WithLogger(deps.logger),
		email.WithFormatter(deps.formatter), email.WithLibrary(deps.library))
}

// emailConfigOf converts the flat wire config into the email Config.
func emailConfigOf(cfg channel.Config) (email.Config, error) {
	tlsMode, err := parseEmailTLSMode(cfg.TLSMode)
	if err != nil {
		return email.Config{}, err
	}
	authType, err := parseEmailAuthType(cfg.AuthType)
	if err != nil {
		return email.Config{}, err
	}
	return email.Config{
		Host:     cfg.Host,
		Port:     cfg.Port,
		Username: cfg.Username,
		Password: cfg.Password,
		From:     cfg.From,
		To:       cfg.To,
		TLSMode:  tlsMode,
		AuthType: authType,
		HTMLMode: cfg.HTMLMode,
		ProxyURL: cfg.ProxyURL,
	}, nil
}

// parseChannelTimeout parses a duration string from a channel config.
func parseChannelTimeout(s string) (time.Duration, error) {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q: %w", s, err)
	}
	return d, nil
}

// parseEmailTLSMode maps the flat wire tls_mode string onto the email
// package's TLSMode.
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

// parseEmailAuthType maps the flat wire auth_type string onto the email
// package's AuthType.
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
