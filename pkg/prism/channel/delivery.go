// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package channel

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"gorm.io/gorm"

	"github.com/tickraft/tickraft/pkg/db"
	"github.com/tickraft/tickraft/pkg/pagination"
	"github.com/tickraft/tickraft/pkg/prism/channel/tracking"
)

// Compile-time assertion that DeliveryStore satisfies the
// tracking.DeliveryRecordStore interface, so it can be injected directly
// into the tracking decorator without a wrapper.
var _ tracking.DeliveryRecordStore = (*DeliveryStore)(nil)

// DeliveryStore persists and queries alert delivery records backed by
// GORM. It implements tracking.DeliveryRecordStore so it can be injected
// directly into the tracking decorator.
//
// When a TenantResolver is installed (NewTenantDeliveryStore) queries are
// scoped to the resolved tenant and Record stamps it; the default
// NewDeliveryStore construction runs unscoped.
type DeliveryStore struct {
	dbc    *gorm.DB
	tenant TenantResolver
}

// NewDeliveryStore creates a new DeliveryStore backed by GORM.
func NewDeliveryStore(dbc *gorm.DB) *DeliveryStore {
	return &DeliveryStore{dbc: dbc}
}

// NewTenantDeliveryStore creates a DeliveryStore scoped to the tenant
// resolved from each call's context. Record stamps the resolved tenant
// (0 when the context carries none, matching the system-level default);
// Get/UpdateAttempt/List/ListKeyset require a tenant and treat a
// cross-tenant row as not found.
func NewTenantDeliveryStore(dbc *gorm.DB, resolve TenantResolver) *DeliveryStore {
	return &DeliveryStore{dbc: dbc, tenant: resolve}
}

// Record persists a single delivery record. This method satisfies
// tracking.DeliveryRecordStore.
func (s *DeliveryStore) Record(ctx context.Context, rec tracking.DeliveryRecord) error {
	entry := DeliveryRecord{
		TenantID:     optionalTenant(ctx, s.tenant),
		ChannelID:    rec.ChannelID,
		ChannelName:  rec.ChannelName,
		ChannelType:  rec.ChannelType,
		AlertType:    rec.AlertType,
		AlertTitle:   rec.AlertTitle,
		EventID:      rec.EventID,
		Status:       string(rec.Status),
		Error:        rec.Error,
		ResponseCode: rec.ResponseCode,
		DurationMs:   rec.DurationMs,
		Attempts: Attempts{{
			Time:       rec.SentAt,
			N:          0,
			Result:     string(rec.Status),
			Error:      rec.Error,
			DurationMs: rec.DurationMs,
		}},
		SentAt: rec.SentAt,
	}
	if raw, err := tracking.MarshalEvent(rec.Event); err == nil {
		entry.RequestPayload = string(raw)
	}

	if err := s.dbc.WithContext(ctx).Create(&entry).Error; err != nil {
		return db.MapError(err)
	}
	return nil
}

// Get returns the delivery record with the given id. It backs the retry
// endpoint's load step.
func (s *DeliveryStore) Get(ctx context.Context, id int64) (*DeliveryRecord, error) {
	q, err := s.queryScope(ctx)
	if err != nil {
		return nil, err
	}
	var rec DeliveryRecord
	if err := q.First(&rec, id).Error; err != nil {
		return nil, db.MapError(err)
	}
	return &rec, nil
}

// queryScope returns the base query for the call, tenant-filtered when a
// resolver is installed. It fails with ErrTenantRequired when scoping is
// active but the context carries no tenant.
func (s *DeliveryStore) queryScope(ctx context.Context) (*gorm.DB, error) {
	q := s.dbc.WithContext(ctx)
	if s.tenant == nil {
		return q, nil
	}
	tenantID, err := requiredTenant(ctx, s.tenant)
	if err != nil {
		return nil, err
	}
	return q.Where("tenant_id = ?", tenantID), nil
}

// UpdateAttempt persists the outcome of a manual retry attempt. The caller
// has already appended the attempt to rec.Attempts and set the top-level
// status/error/duration to the latest attempt's values.
func (s *DeliveryStore) UpdateAttempt(ctx context.Context, rec *DeliveryRecord) error {
	q, err := s.queryScope(ctx)
	if err != nil {
		return err
	}
	err = q.
		Model(&DeliveryRecord{}).
		Where("id = ?", rec.ID).
		Updates(map[string]any{
			"status":        rec.Status,
			"error":         rec.Error,
			"response_code": rec.ResponseCode,
			"duration_ms":   rec.DurationMs,
			"attempts":      rec.Attempts,
		}).Error
	if err != nil {
		return db.MapError(err)
	}
	return nil
}

// listScope builds the filtered query for delivery records. It is shared
// by List and ListKeyset so the two pagination modes stay consistent. When
// a tenant resolver is installed the query is tenant-filtered.
func (s *DeliveryStore) listScope(ctx context.Context, params DeliveryListParams) (*gorm.DB, error) {
	q, err := s.queryScope(ctx)
	if err != nil {
		return nil, err
	}
	q = q.Model(&DeliveryRecord{})
	if params.ChannelID > 0 {
		q = q.Where("channel_id = ?", params.ChannelID)
	}
	if params.Status != "" {
		q = q.Where("status = ?", params.Status)
	}
	if params.AlertTitle != "" {
		q = q.Where("alert_title LIKE ?", "%"+params.AlertTitle+"%")
	}
	if params.StartTime != nil {
		q = q.Where("sent_at >= ?", *params.StartTime)
	}
	if params.EndTime != nil {
		q = q.Where("sent_at <= ?", *params.EndTime)
	}
	return q, nil
}

// List returns delivery records matching params together with the total
// count. Results are ordered by SentAt descending so the most recent
// delivery attempts appear first.
func (s *DeliveryStore) List(ctx context.Context, params DeliveryListParams) ([]DeliveryRecord, int64, error) {
	page, size := pagination.Clamp(params.Page, params.Size)
	params.Page, params.Size = page, size

	scope, err := s.listScope(ctx, params)
	if err != nil {
		return nil, 0, err
	}
	var total int64
	if err := scope.Count(&total).Error; err != nil {
		return nil, 0, db.MapError(err)
	}

	scope, err = s.listScope(ctx, params)
	if err != nil {
		return nil, 0, err
	}
	var records []DeliveryRecord
	offset := (params.Page - 1) * params.Size
	if err := scope.
		Order("sent_at DESC").
		Offset(offset).
		Limit(params.Size).
		Find(&records).Error; err != nil {
		return nil, 0, db.MapError(err)
	}

	return records, total, nil
}

// deliveryKeysetCursor is the keyset descriptor for delivery record list
// queries. The composite ordering (sent_at DESC, id DESC) matches List; id
// is the primary key tie-breaker for rows that share a sent_at value.
var deliveryKeysetCursor = pagination.Cursor{
	Column:    "sent_at",
	Column2:   "id",
	Direction: pagination.Desc,
}

// keysetTimeLayout is the wire format of the sent_at component inside the
// keyset cursor token.
const keysetTimeLayout = "2006-01-02 15:04:05.999999999"

// ListKeyset returns a page of delivery records using keyset (cursor-based)
// pagination, avoiding the O(N) cost of OFFSET on deep pages. The opaque
// next-page cursor is returned in the result; pass it as params.Cursor for
// the subsequent page. An empty cursor means the first page.
//
// The cursor values are bound with their concrete types (time.Time, int64)
// rather than the raw token strings: the drivers then compare them against
// the column types, which keeps the sent_at-equality branch of the tuple
// predicate exact on every backend (string-bound comparisons miss
// same-second rows on SQLite, whose datetime values are stored as text).
func (s *DeliveryStore) ListKeyset(
	ctx context.Context,
	params DeliveryListParams,
) (pagination.PageResult[DeliveryRecord], error) {
	size := pagination.ClampSize(params.Size)

	scope, err := s.listScope(ctx, params)
	if err != nil {
		return pagination.PageResult[DeliveryRecord]{}, err
	}
	var total int64
	if err := scope.Count(&total).Error; err != nil {
		return pagination.PageResult[DeliveryRecord]{}, db.MapError(err)
	}

	q, err := s.listScope(ctx, params)
	if err != nil {
		return pagination.PageResult[DeliveryRecord]{}, err
	}
	q = q.
		Order("sent_at DESC, id DESC").
		Limit(size)
	if params.Cursor != "" {
		decoded, err := pagination.DecodeCursor(params.Cursor)
		if err != nil {
			return pagination.PageResult[DeliveryRecord]{}, fmt.Errorf("channel: list deliveries keyset: %w", err)
		}
		if decoded.Column != deliveryKeysetCursor.Column ||
			decoded.Column2 != deliveryKeysetCursor.Column2 ||
			decoded.Direction != deliveryKeysetCursor.Direction {
			return pagination.PageResult[DeliveryRecord]{}, fmt.Errorf(
				"channel: list deliveries keyset: %w: column/direction mismatch", pagination.ErrInvalidCursor)
		}
		sentAt, err := time.Parse(keysetTimeLayout, decoded.Value)
		if err != nil {
			return pagination.PageResult[DeliveryRecord]{}, fmt.Errorf(
				"channel: list deliveries keyset: %w: invalid sent_at value", pagination.ErrInvalidCursor)
		}
		id, err := strconv.ParseInt(decoded.Value2, 10, 64)
		if err != nil {
			return pagination.PageResult[DeliveryRecord]{}, fmt.Errorf(
				"channel: list deliveries keyset: %w: invalid id value", pagination.ErrInvalidCursor)
		}
		q = q.Where("(sent_at < ?) OR (sent_at = ? AND id < ?)", sentAt, sentAt, id)
	}

	var records []DeliveryRecord
	if err := q.Find(&records).Error; err != nil {
		return pagination.PageResult[DeliveryRecord]{}, fmt.Errorf(
			"channel: list deliveries keyset: %w", db.MapError(err))
	}

	next, err := pagination.NextCursor2ForSize(
		deliveryKeysetCursor, records, size, func(r DeliveryRecord) (string, string) {
			return r.SentAt.Format(keysetTimeLayout), strconv.FormatInt(r.ID, 10)
		})
	if err != nil {
		return pagination.PageResult[DeliveryRecord]{}, fmt.Errorf("channel: list deliveries keyset: %w", err)
	}

	return pagination.PageResult[DeliveryRecord]{
		Items:      records,
		Total:      total,
		NextCursor: next,
	}, nil
}

// DeleteOlderThan removes delivery records recorded before the given time,
// across every tenant (it is a maintenance sweep, not a tenant-facing
// query, so the tenant scope is deliberately bypassed). It returns the
// number of rows removed.
func (s *DeliveryStore) DeleteOlderThan(ctx context.Context, before time.Time) (int64, error) {
	if s == nil || s.dbc == nil {
		return 0, nil
	}
	res := s.dbc.WithContext(ctx).
		Where("sent_at < ?", before).
		Delete(&DeliveryRecord{})
	if res.Error != nil {
		return 0, fmt.Errorf("channel: delete old delivery records: %w", db.MapError(res.Error))
	}
	return res.RowsAffected, nil
}
