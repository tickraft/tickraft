// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package channel

import (
	"context"
	"errors"
	"strings"
	"sync"

	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/i18n"
	"github.com/tickraft/tickraft/pkg/prism/alert"
	"github.com/tickraft/tickraft/pkg/prism/alert/template"
	"github.com/tickraft/tickraft/pkg/prism/channel/format"
)

// ErrTypeNotAllowed is returned by AssertTypeAllowed when a registered
// TypeGuard rejects the use of the channel type in the current context.
var ErrTypeNotAllowed = errors.New("channel: channel type not allowed")

// BuildOptions carries the collaborators used to construct a channel from
// its persisted config JSON. Formatter, Library, and Registry drive the
// locale-aware alert rendering inside each channel adapter; all of them may
// be zero values, in which case the built channel falls back to
// type-specific defaults.
//
// The ProxyURL/ProxyBypass/PlainNotificationOnly/Scope fields carry the
// deployment-wide egress and notification policies (M5 private-deployment
// adaptation): they are plain capability knobs without any licensing
// notion, and editions gate the settings UI that drives them.
type BuildOptions struct {
	// Logger is the structured logger shared by the built channels. When
	// nil, a no-op logger is used.
	Logger *zap.Logger
	// Formatter renders alert events into localized messages. Injected at
	// startup by the composition root.
	Formatter i18n.Formatter
	// Library is the alert template library used for template-based
	// rendering. When non-nil and alert.TemplateID is non-empty, channels
	// render through the library instead of the formatter.
	Library template.Library
	// Registry resolves level labels, field labels, and time formats for
	// template-based rendering. When nil the template renderer falls back
	// to English defaults.
	Registry i18n.Registry
	// ProxyURL is the deployment-wide egress proxy applied to every built
	// channel whose own config does not set a per-channel proxy_url. A
	// per-channel proxy_url always wins. Supported schemes are http,
	// https, and socks5 (email supports socks5 only). When empty all
	// channels connect directly.
	ProxyURL string
	// ProxyBypass is a NO_PROXY-style list of hostnames and domain
	// suffixes whose connections must bypass ProxyURL and go direct (for
	// example intranet SMTP relays or IM endpoints reachable without
	// egress). Effective only when ProxyURL is set.
	ProxyBypass []string
	// PlainNotificationOnly is the L0 degraded-notification policy for
	// network-isolated deployments: interactive-card channels render
	// plain text instead, and resource links are annotated as intranet
	// addresses. Defaults to false (full rendering).
	PlainNotificationOnly bool
	// Scope is the deployment-wide network-scope rendering policy (M5 L1:
	// dual templates, dual domains, content masking). When its
	// NetworkScope is extranet, rendered messages are masked, resource
	// links are adapted onto the public domain, and the scoped template
	// variant is preferred. The zero value keeps full rendering.
	Scope format.ScopeOptions
	// Interaction is the deployment-wide interactive-card policy (M5 L1
	// inbound events): when enabled, interactive-capable channels render
	// callback buttons (action verb plus the alert's EventID) on their
	// cards. The zero value keeps link-only cards. Plain capability knob
	// without any licensing notion; editions gate the inbound transports
	// that complete the loop.
	Interaction format.InteractionOptions
}

// BuildFunc constructs a runtime alert.Channel from the channel type's
// config JSON (the format stored in the sys_prism_channel table).
type BuildFunc func(configJSON string, opts BuildOptions) (alert.Channel, error)

// ValidateFunc validates a channel type's config JSON at the API boundary
// (create/update/inline-test). It returns a descriptive error when the
// config is unusable for the type.
type ValidateFunc func(configJSON string) error

// TypeGuardFunc authorizes the use of a channel type in a request context.
// It is the type-level licensing/feature hook: implementations return a
// non-nil error (typically a 403-class error) when the type must not be
// created, updated, or tested in this deployment. A nil guard allows the
// type unconditionally.
type TypeGuardFunc func(ctx context.Context) error

// TypeInfo describes a channel type registered via RegisterType. It bundles
// the construction entry point with the per-type metadata the channel
// service needs: config validation, the sensitive config keys beyond the
// built-in set, and the optional authorization guard.
type TypeInfo struct {
	// Build constructs the channel from its config JSON.
	Build BuildFunc
	// Validate, when non-nil, is invoked by the service layer on
	// create/update and inline-test requests carrying the type.
	Validate ValidateFunc
	// SensitiveKeys lists normalized (lowercase, underscore-stripped)
	// config keys, beyond the built-in sensitive set, whose values must be
	// encrypted at rest and masked in API responses.
	SensitiveKeys []string
	// TypeGuard, when non-nil, authorizes the type per request context.
	TypeGuard TypeGuardFunc
}

var (
	// registryMu guards typeRegistry. A write lock is held during
	// RegisterType; a read lock is held during lookups. The lock is never
	// held while invoking a Build/Validate/TypeGuard function.
	registryMu sync.RWMutex
	// typeRegistry maps lowercased channel type names to their TypeInfo.
	typeRegistry = make(map[string]TypeInfo)
)

// RegisterType registers the TypeInfo for a channel type name. The name is
// normalized to lowercase and matched case-insensitively against the Type
// field of channel rows and requests. Registering an already-registered
// name overwrites the previous entry; the last registration wins.
//
// The built-in types (webhook, email, and the seven instant-messaging
// types) are registered by the composition root at startup; extension
// editions register their additional types (for example SMS) through this
// same entry point. RegisterType is safe for concurrent calls.
func RegisterType(name string, info TypeInfo) {
	name = strings.ToLower(strings.TrimSpace(name))
	registryMu.Lock()
	defer registryMu.Unlock()
	typeRegistry[name] = info
}

// LookupType returns the TypeInfo registered for name and whether one is
// registered. The lookup is case-insensitive.
func LookupType(name string) (TypeInfo, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	info, ok := typeRegistry[strings.ToLower(strings.TrimSpace(name))]
	return info, ok
}

// builtinTypes is the set of channel types the open-source build registers
// at startup: the two generic channels plus the seven instant-messaging
// channels. It backs ValidType before (and independent of) the composition
// root's RegisterType calls.
var builtinTypes = map[string]struct{}{
	"webhook":  {},
	"email":    {},
	"dingtalk": {},
	"discord":  {},
	"feishu":   {},
	"slack":    {},
	"teams":    {},
	"telegram": {},
	"wecom":    {},
}

// ValidType reports whether name is an accepted channel type: one of the
// built-in types or a type registered via RegisterType. The check is
// case-insensitive.
func ValidType(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	if _, ok := builtinTypes[name]; ok {
		return true
	}
	registryMu.RLock()
	defer registryMu.RUnlock()
	_, ok := typeRegistry[name]
	return ok
}

// AssertTypeAllowed reports whether the channel type may be used in ctx.
// An unregistered or non-built-in type is rejected. For registered types
// the TypeInfo's TypeGuard (when set) decides; built-in types without a
// registered override are allowed unconditionally.
func AssertTypeAllowed(ctx context.Context, name string) error {
	if !ValidType(name) {
		return &ValidationError{Code: CodeTypeInvalid, Msg: "invalid channel type"}
	}
	registryMu.RLock()
	info, ok := typeRegistry[strings.ToLower(strings.TrimSpace(name))]
	registryMu.RUnlock()
	if ok && info.TypeGuard != nil {
		return info.TypeGuard(ctx)
	}
	return nil
}

// ValidateTypeConfig validates a config JSON against the registered
// TypeInfo's Validate function. It returns nil when the type has no
// validator registered.
func ValidateTypeConfig(name, configJSON string) error {
	registryMu.RLock()
	info, ok := typeRegistry[strings.ToLower(strings.TrimSpace(name))]
	registryMu.RUnlock()
	if !ok || info.Validate == nil {
		return nil
	}
	return info.Validate(configJSON)
}

// SensitiveKeys returns the union of the built-in sensitive config keys and
// the extra keys contributed by every registered TypeInfo. Keys are
// normalized (lowercase, underscores stripped).
func SensitiveKeys() map[string]struct{} {
	keys := make(map[string]struct{}, len(builtinSensitiveKeys)+4)
	for k := range builtinSensitiveKeys {
		keys[k] = struct{}{}
	}
	registryMu.RLock()
	defer registryMu.RUnlock()
	for _, info := range typeRegistry {
		for _, k := range info.SensitiveKeys {
			if k == "" {
				continue
			}
			keys[normalizeKey(k)] = struct{}{}
		}
	}
	return keys
}

// ResetRegistryForTest clears all registered channel types. It is intended
// for test isolation only and must never be called from production code.
func ResetRegistryForTest() {
	registryMu.Lock()
	defer registryMu.Unlock()
	typeRegistry = make(map[string]TypeInfo)
}
