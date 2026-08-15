// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package router

import (
	"context"
	"time"

	"github.com/cloudwego/hertz/pkg/app"

	"github.com/tickraft/tickraft/pkg/api/handler/alert"
	"github.com/tickraft/tickraft/pkg/api/handler/asset"
	"github.com/tickraft/tickraft/pkg/api/handler/channel"
	"github.com/tickraft/tickraft/pkg/api/handler/remediation"
	"github.com/tickraft/tickraft/pkg/api/handler/task"
	"github.com/tickraft/tickraft/pkg/api/handler/telemetry"
	assetstore "github.com/tickraft/tickraft/pkg/asset"
	"github.com/tickraft/tickraft/pkg/pagination"
	"github.com/tickraft/tickraft/pkg/types"
	"go.uber.org/zap"
)

// --- Stub service implementations for route-registration tests ---
//
// These stubs satisfy the Service interfaces required by the handler package
// so that router tests can exercise RegisterRoutes in standalone mode
// without a real database or engine.

// stubTaskService implements task.Service.
type stubTaskService struct{}

func (stubTaskService) ListTasks(_ context.Context, _, _ int, _ task.Filter) ([]task.Task, int64, error) {
	return nil, 0, nil
}
func (stubTaskService) GetTask(_ context.Context, _ int64) (*task.Task, error) {
	return nil, nil
}
func (stubTaskService) CreateTask(_ context.Context, _ *task.Task) (*task.Task, error) {
	return &task.Task{}, nil
}
func (stubTaskService) UpdateTask(_ context.Context, _ int64, _ *task.Task) (*task.Task, error) {
	return &task.Task{}, nil
}
func (stubTaskService) DeleteTask(_ context.Context, _ int64) error  { return nil }
func (stubTaskService) TriggerTask(_ context.Context, _ int64) error { return nil }
func (stubTaskService) PauseTask(_ context.Context, _ int64) error   { return nil }
func (stubTaskService) ResumeTask(_ context.Context, _ int64) error  { return nil }
func (stubTaskService) ListExecutions(_ context.Context, _ int64, _, _ int, _ task.ExecutionFilter) ([]task.Execution, int64, error) {
	return nil, 0, nil
}
func (stubTaskService) GetExecution(_ context.Context, _, _ int64) (*task.Execution, error) {
	return nil, nil
}
func (stubTaskService) CopyTask(_ context.Context, _ int64, _ string) (*task.Task, error) {
	return &task.Task{}, nil
}
func (stubTaskService) GetExecutionStats(_ context.Context, _, _ time.Time) (task.ExecutionStats, error) {
	return task.ExecutionStats{}, nil
}

var _ task.Service = (*stubTaskService)(nil)

// stubAlertService implements alert.Service.
type stubAlertService struct{}

func (stubAlertService) ListRules(_ context.Context, _, _ int) ([]alert.Rule, int64, error) {
	return nil, 0, nil
}
func (stubAlertService) GetRule(_ context.Context, _ int64) (*alert.Rule, error) { return nil, nil }
func (stubAlertService) CreateRule(_ context.Context, _ *alert.Rule) (*alert.Rule, error) {
	return &alert.Rule{}, nil
}
func (stubAlertService) UpdateRule(_ context.Context, _ int64, _ *alert.Rule) (*alert.Rule, error) {
	return &alert.Rule{}, nil
}
func (stubAlertService) DeleteRule(_ context.Context, _ int64) error { return nil }
func (stubAlertService) ListRecords(_ context.Context, _, _ int, _ alert.RecordFilter) ([]alert.Record, int64, error) {
	return nil, 0, nil
}
func (stubAlertService) GetRecord(_ context.Context, _ int64) (*alert.Record, error) {
	return nil, nil
}
func (stubAlertService) AcknowledgeRecord(_ context.Context, _ int64) (*alert.Record, error) {
	return nil, nil
}
func (stubAlertService) ResolveRecord(_ context.Context, _ int64) (*alert.Record, error) {
	return nil, nil
}

var _ alert.Service = (*stubAlertService)(nil)

// stubChannelService implements channel.Service.
type stubChannelService struct{}

func (stubChannelService) ListChannels(_ context.Context, _, _ int) ([]channel.Channel, int64, error) {
	return nil, 0, nil
}
func (stubChannelService) GetChannel(_ context.Context, _ int64) (*channel.Channel, error) {
	return nil, nil
}
func (stubChannelService) CreateChannel(_ context.Context, _ *channel.Channel) (*channel.Channel, error) {
	return &channel.Channel{}, nil
}
func (stubChannelService) UpdateChannel(_ context.Context, _ int64, _ *channel.Channel) (*channel.Channel, error) {
	return &channel.Channel{}, nil
}
func (stubChannelService) DeleteChannel(_ context.Context, _ int64) error { return nil }
func (stubChannelService) TestChannel(_ context.Context, _ int64) error   { return nil }

var _ channel.Service = (*stubChannelService)(nil)

// stubRemediationService implements remediation.Service.
type stubRemediationService struct{}

func (stubRemediationService) ListRules(_ context.Context, _, _ int) ([]remediation.Rule, int64, error) {
	return nil, 0, nil
}
func (stubRemediationService) GetRule(_ context.Context, _ int64) (*remediation.Rule, error) {
	return nil, nil
}
func (stubRemediationService) CreateRule(_ context.Context, _ *remediation.Rule) (*remediation.Rule, error) {
	return &remediation.Rule{}, nil
}
func (stubRemediationService) UpdateRule(_ context.Context, _ int64, _ *remediation.Rule) (*remediation.Rule, error) {
	return &remediation.Rule{}, nil
}
func (stubRemediationService) DeleteRule(_ context.Context, _ int64) error { return nil }
func (stubRemediationService) ListRecords(_ context.Context, _, _ int, _ string) ([]remediation.Record, int64, error) {
	return nil, 0, nil
}

var _ remediation.Service = (*stubRemediationService)(nil)

// stubReportHandler implements telemetry.ReportHandler.
type stubReportHandler struct{}

func (stubReportHandler) Report(_ context.Context, _ *app.RequestContext) {}

var _ telemetry.ReportHandler = (*stubReportHandler)(nil)

// stubTelemetryService implements telemetry.Service.
type stubTelemetryService struct{}

func (stubTelemetryService) ListTasks(_ context.Context, _, _ int, _ telemetry.Filter) ([]telemetry.Task, int64, error) {
	return nil, 0, nil
}
func (stubTelemetryService) GetTask(_ context.Context, _ int64) (*telemetry.Task, error) {
	return nil, nil
}
func (stubTelemetryService) CreateTask(_ context.Context, _ *telemetry.Task) (*telemetry.Task, error) {
	return &telemetry.Task{}, nil
}
func (stubTelemetryService) UpdateTask(_ context.Context, _ int64, _ *telemetry.Task) (*telemetry.Task, error) {
	return &telemetry.Task{}, nil
}
func (stubTelemetryService) DeleteTask(_ context.Context, _ int64) error { return nil }

var _ telemetry.Service = (*stubTelemetryService)(nil)

// stubAssetStore implements assetstore.Store for route-registration tests.
type stubAssetStore struct{}

func (stubAssetStore) CountByStatus(_ context.Context) (map[string]int64, error) {
	return map[string]int64{}, nil
}

func (stubAssetStore) Migrate(_ context.Context) error { return nil }
func (stubAssetStore) Create(_ context.Context, _ *assetstore.Asset) error {
	return nil
}
func (stubAssetStore) Update(_ context.Context, _ *assetstore.Asset) error {
	return nil
}
func (stubAssetStore) GetByID(_ context.Context, _ int64) (*assetstore.Asset, error) {
	return nil, nil
}
func (stubAssetStore) GetByKey(_ context.Context, _ int64, _ string) (*assetstore.Asset, error) {
	return nil, nil
}
func (stubAssetStore) UpdateStatus(_ context.Context, _ int64, _ types.AssetStatus, _ time.Time) error {
	return nil
}
func (stubAssetStore) List(_ context.Context, _, _ int, _ assetstore.ListFilter) ([]*assetstore.Asset, int64, error) {
	return nil, 0, nil
}
func (stubAssetStore) ListKeyset(_ context.Context, _ pagination.PageRequest) (pagination.PageResult[*assetstore.Asset], error) {
	return pagination.PageResult[*assetstore.Asset]{}, nil
}
func (stubAssetStore) Delete(_ context.Context, _ int64) error { return nil }
func (stubAssetStore) CountByType(_ context.Context, _ int64, _ types.AssetType) (int64, error) {
	return 0, nil
}
func (stubAssetStore) ExistsByKey(_ context.Context, _ string) (bool, error) {
	return false, nil
}

var _ assetstore.Store = (*stubAssetStore)(nil)

// allRequiredRegisterOptions returns a complete set of RegisterOption values
// that satisfy all required services. Tests can
// append additional options (e.g. WithSystemService) to override individual
// defaults.
func allRequiredRegisterOptions() []RegisterOption {
	return []RegisterOption{
		WithTaskService(stubTaskService{}),
		WithAlertService(stubAlertService{}),
		WithChannelService(stubChannelService{}),
		WithRemediationRuleService(stubRemediationService{}),
		WithSystemService(newFakeSystemService()),
		WithTelemetryService(stubTelemetryService{}),
		WithTelemetryReportHandler(stubReportHandler{}),
		WithAssetHandler(asset.NewHandler(stubAssetStore{}, zap.NewNop())),
	}
}
