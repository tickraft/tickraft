// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package asset

import (
	"time"

	"github.com/tickraft/tickraft/pkg/types"
)

// Asset is the unified model for both Scheduler tasks and Collector targets.
// Each Asset represents an entity that can be scheduled (by Scheduler)
// and/or observed (by Collector).
type Asset struct {
	// ID is the unique identifier.
	ID int64 `json:"id" gorm:"primaryKey;autoIncrement"`
	// TenantID is the tenant identifier for multi-tenancy isolation.
	// The runtime is single-tenant: this field is always 0.
	// The runtime injects the actual tenant ID via the store layer.
	//
	// Together with AssetKey it forms a composite unique index
	// (idx_assets_tenant_key) so that duplicate asset keys within the same
	// tenant are rejected at the database level with a unique-constraint
	// violation, which the store layer maps to errdefs.ErrConflict.
	TenantID int64 `json:"-" gorm:"column:tenant_id;not null;uniqueIndex:idx_assets_tenant_key,priority:1"`
	// AssetType categorizes the asset.
	AssetType types.AssetType `json:"asset_type" gorm:"column:asset_type;not null"`
	// AssetKey is the tenant-unique identifier for the asset. It is the
	// second column of the composite unique index idx_assets_tenant_key.
	AssetKey string `json:"asset_key" gorm:"column:asset_key;not null;uniqueIndex:idx_assets_tenant_key,priority:2"`
	// Name is the human-readable asset name.
	Name string `json:"name" gorm:"column:name"`
	// Status is the current asset status.
	Status types.AssetStatus `json:"status" gorm:"column:status"`
	// Metadata holds optional JSON-encoded extension data.
	Metadata string `json:"metadata,omitempty" gorm:"column:metadata"`
	// LastActiveAt is the last time the asset reported or was executed.
	LastActiveAt time.Time `json:"last_active_at" gorm:"column:last_active_at"`
	// CreatedAt is the asset creation timestamp.
	CreatedAt time.Time `json:"created_at" gorm:"column:created_at;autoCreateTime"`
	// UpdatedAt is the last update timestamp.
	UpdatedAt time.Time `json:"updated_at" gorm:"column:updated_at;autoUpdateTime"`
}

// BuiltInMetadataKeys defines the preset metadata keys that the system
// recognizes as advisory labels for the Asset.Metadata JSON field.
// These keys are not enforced as a schema; they provide conventional
// labels for categorizing assets.
var BuiltInMetadataKeys = []string{
	"business_line", // Line of business the asset belongs to
	"project",       // Project the asset is associated with
	"owner",         // Person or team responsible for the asset
	"priority",      // Priority level of the asset
	"environment",   // Deployment environment (e.g., production, staging)
}
