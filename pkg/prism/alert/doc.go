// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package alert provides the alert domain: events, the rule engine,
// rule persistence, and dispatch contracts.
//
// The rule engine (Engine) compiles Rule expressions against the
// AlertEnv contract through the pkg/expr kernel and evaluates them in a
// single pass per event, yielding both the matched rule IDs and the
// structured Violations of every matched comparison sub-condition.
// AlertMatcher adapts the engine to the Matcher interface consumed by
// the prism engine; Register is the startup wiring entry point.
//
// The prism engine (in package [github.com/tickraft/tickraft/pkg/prism])
// subscribes to collector alert events (metric and log alerts) published on
// the event bus, evaluates registered alert rules against each event, and
// dispatches matching alerts to registered notification channels through a
// bounded worker pool.
//
// Notification channel implementations live in the
// [github.com/tickraft/tickraft/pkg/prism/channel] package and its
// sub-packages. Channels are loaded from the database at engine startup via
// [github.com/tickraft/tickraft/pkg/prism/channel.Store.ListEnabled] and
// hot-reloaded on CRUD operations.
package alert
