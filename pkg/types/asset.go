// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package types

// AssetType categorizes assets that can be scheduled (by the scheduler)
// or observed (by the collector).
//
// This is the single source of truth for the asset type value object;
// domain packages (pkg/asset, pkg/task, pkg/telemetry, etc.) import
// pkg/types directly to use it.
type AssetType string

const (
	// AssetTypeTask identifies a scheduled task asset.
	AssetTypeTask AssetType = "task"
	// AssetTypeDevice identifies a device asset.
	AssetTypeDevice AssetType = "device"
	// AssetTypeHost identifies a host asset.
	AssetTypeHost AssetType = "host"
	// AssetTypePort identifies a port asset.
	AssetTypePort AssetType = "port"
	// AssetTypeWebsite identifies a website asset.
	AssetTypeWebsite AssetType = "website"
	// AssetTypeService identifies a service asset.
	AssetTypeService AssetType = "service"
)

// AssetStatus enumerates the unified asset status. The kernel exposes
// only the 4-state base enum below; callers may add further
// business states (such as warning, error, maintenance) via their own
// types and map them back to these base states for kernel-side display.
//
// This is the single source of truth for the asset status value object;
// domain packages import pkg/types directly to use it.
type AssetStatus string

const (
	// AssetStatusNormal indicates the asset is operating normally.
	AssetStatusNormal AssetStatus = "normal"
	// AssetStatusAbnormal indicates the asset is in an abnormal state.
	AssetStatusAbnormal AssetStatus = "abnormal"
	// AssetStatusOffline indicates the asset is offline.
	AssetStatusOffline AssetStatus = "offline"
	// AssetStatusUnknown indicates the asset status is unknown (initial
	// state).
	AssetStatusUnknown AssetStatus = "unknown"
)
