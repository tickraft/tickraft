// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package asset

import (
	"time"

	"github.com/bytedance/sonic"

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
	// (idx_sys_asset_tenant_key) so that duplicate asset keys within the
	// same tenant are rejected at the database level with a unique-constraint
	// violation, which the store layer maps to errdefs.ErrConflict.
	TenantID int64 `json:"-" gorm:"column:tenant_id;not null;uniqueIndex:idx_sys_asset_tenant_key,priority:1"`
	// AssetType categorizes the asset.
	AssetType types.AssetType `json:"asset_type" gorm:"column:asset_type;not null"`
	// AssetKey is the tenant-unique identifier for the asset. It is the
	// second column of the composite unique index idx_sys_asset_tenant_key.
	AssetKey string `json:"asset_key" gorm:"column:asset_key;not null;uniqueIndex:idx_sys_asset_tenant_key,priority:2"`
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

// TableName returns the database table name for Asset.
func (Asset) TableName() string { return "sys_asset" }

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

// TagsFromMetadata decodes an asset's JSON metadata blob into a string
// map. Malformed or empty metadata yields an empty map so rule
// expressions reading tags from such assets evaluate to "" rather than
// failing.
func TagsFromMetadata(raw string) map[string]string {
	tags := map[string]string{}
	if raw == "" {
		return tags
	}
	// Best-effort decode: a non-object or malformed blob leaves the
	// map empty, which rules read as "no tags".
	_ = sonic.Unmarshal([]byte(raw), &tags)
	if tags == nil {
		return map[string]string{}
	}
	return tags
}

// CustomFieldCount counts the metadata keys that are not preset labels
// (see BuiltInMetadataKeys). It is the metered unit of the custom-field
// quota (quota.TypeCustomField): preset labels are free, every key
// beyond them is one custom field. Values of any JSON type count (the
// metering decodes into a generic object, unlike the string-typed
// TagsFromMetadata); malformed or empty metadata counts as zero,
// mirroring the best-effort contract of TagsFromMetadata.
func CustomFieldCount(raw string) int {
	if raw == "" {
		return 0
	}
	var obj map[string]any
	if err := sonic.Unmarshal([]byte(raw), &obj); err != nil || obj == nil {
		return 0
	}
	preset := make(map[string]struct{}, len(BuiltInMetadataKeys))
	for _, k := range BuiltInMetadataKeys {
		preset[k] = struct{}{}
	}
	n := 0
	for k := range obj {
		if _, ok := preset[k]; !ok {
			n++
		}
	}
	return n
}
