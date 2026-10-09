// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package telemetry

import (
	"context"

	"github.com/tickraft/tickraft/pkg/types"
)

// Processor handles the business logic for a specific asset type.
// It is protocol-agnostic — all collection channels share the same processor.
type Processor interface {
	// Type returns the asset type this processor handles.
	Type() types.AssetType
	// Process handles a telemetry and returns the processing result.
	Process(ctx context.Context, t *Telemetry) (*ProcessResult, error)
	// OnTimeout handles the asset timeout scenario.
	OnTimeout(ctx context.Context, assetID int64) error
}

// ProcessResult indicates the outcome of processing a telemetry.
type ProcessResult struct {
	// PrevStatus is the status before the transition.
	PrevStatus types.AssetStatus
	// CurrStatus is the status after the transition.
	CurrStatus types.AssetStatus
	// Reason describes why the status changed.
	Reason string
	// Alerts holds any alerts triggered during processing.
	Alerts []AlertContext
}
