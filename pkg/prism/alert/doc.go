// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package alert provides the alert domain types used by the prism engine.
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
