// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

// Package handler hosts the HTTP handlers for the tickraft API
// server.
//
// The package is organized around a single RegisterRoutes entry point that
// wires every endpoint onto an [*api.Server] via a set of
// [RouteOption] values. Each option injects either a middleware
// (app.HandlerFunc) or a service implementation, keeping the handler package
// free of any direct dependency on pkg/auth, pkg/auth/jwt, or pkg/cache.
//
// # Service SPI
//
// Each domain module exposes a service interface and an in-memory default:
//
//   - auth.Service: authentication and API-key management.
//   - task.Service: scheduled task CRUD and execution history. The
//     in-memory default (NewMemoryTaskService) is suitable for the
//     runtime; NewSchedulerTaskService wires the real scheduler
//     engine and persistent stores.
//   - AlertService: alert rule and record management. The in-memory default
//     (NewMemoryAlertService) is the default;
//     NewPrismAlertService wires the real alert engine and persistent
//     stores.
//   - SystemService: system configuration and info.
//   - telemetry.Service: telemetry collection task CRUD.
//
// Extended builds inject real services through the corresponding
// With*Service option before [RegisterRoutes] is called.
//
// # Handler types
//
// Most domain modules have no handler-local entity types: the wire shape
// and the storage shape are the same model (e.g. prism/alert.Rule,
// prism/channel.Channel, telemetry.MonitorPoint). Those models carry both
// json and gorm tags, and internal columns (TenantID, DeletedAt,
// runtime-managed status fields) are tagged json:"-" so binding and
// serialization can never read or write them.
//
// A handler-local type remains only where the API shape genuinely differs
// from storage (request-only payloads such as loginRequest, and view
// structs such as system.Info), or where the handler type is the single
// model itself (system.Config, TokenPair). See docs/model-layering-design.md
// for the full layering contract.
//
// # Purity
//
// The handler package depends only on the Go standard library, hertz,
// pkg/api, and the tickraft packages it receives as injected
// dependencies. It does not import any third-party observability, RPC, or
// caching client libraries.
package handler
