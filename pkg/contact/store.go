// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package contact

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/tickraft/tickraft/pkg/db"
)

// ErrNotFound is returned when a contact cannot be located by its ID.
var ErrNotFound = errors.New("contact: not found")

// ErrTenantRequired is returned by tenant-scoped stores when the
// installed TenantResolver cannot produce a tenant ID from the request
// context.
var ErrTenantRequired = errors.New("contact: tenant id required in context")

// TenantResolver extracts the owning tenant ID from a context. The second
// return value reports whether the context carries a tenant at all.
//
// The open-source build never installs a resolver: every store operation
// runs unscoped with tenant_id 0. Multi-tenant editions install one via
// NewTenantStore so the same store becomes tenant-scoped without any
// query duplication (see pkg/prism/channel for the original pattern).
type TenantResolver func(ctx context.Context) (int64, bool)

// requiredTenant resolves the tenant for an operation that must be scoped.
// It fails with ErrTenantRequired when a resolver is installed but the
// context carries no usable tenant ID, so a missing tenant context never
// degrades into an unscoped query that could leak data across tenants.
func requiredTenant(ctx context.Context, resolve TenantResolver) (int64, error) {
	tenantID, ok := resolve(ctx)
	if !ok || tenantID <= 0 {
		return 0, ErrTenantRequired
	}
	return tenantID, nil
}

// Store persists and queries notification contacts (sys_contact).
//
// When a TenantResolver is installed (NewTenantStore) every query and
// write is scoped to the tenant resolved from the call context; the
// default NewStore construction runs unscoped.
type Store struct {
	dbc    *gorm.DB
	tenant TenantResolver
}

// NewStore creates a Store backed by GORM, running unscoped (the
// open-source single-tenant construction).
func NewStore(dbc *gorm.DB) *Store {
	return &Store{dbc: dbc}
}

// NewTenantStore creates a Store scoped to the tenant resolved from each
// call's context: List/Count filter by tenant, Get/Update/Delete
// additionally match the tenant (a cross-tenant row behaves as not
// found), and Create stamps the owning tenant. resolve must be non-nil.
func NewTenantStore(dbc *gorm.DB, resolve TenantResolver) *Store {
	return &Store{dbc: dbc, tenant: resolve}
}

// Migrate creates or updates the sys_contact table. It is idempotent.
func (s *Store) Migrate(ctx context.Context) error {
	return s.dbc.WithContext(ctx).AutoMigrate(&Contact{})
}

// List returns a page of contacts matching the filter (keyword substring
// on name/email/phone), ordered by ID ascending, together with the total
// count of matching rows.
func (s *Store) List(ctx context.Context, filter ListFilter) ([]Contact, int64, error) {
	q, err := s.queryScope(ctx)
	if err != nil {
		return nil, 0, err
	}
	if filter.Keyword != "" {
		like := "%" + filter.Keyword + "%"
		q = q.Where("name LIKE ? OR email LIKE ? OR phone LIKE ?", like, like, like)
	}
	var total int64
	if err := q.Model(&Contact{}).Count(&total).Error; err != nil {
		return nil, 0, db.MapError(err)
	}
	var items []Contact
	page, size := filter.Page, filter.Size
	page = max(page, 1)
	if size < 1 {
		size = 20
	}
	if err := q.Order("id ASC").Limit(size).Offset((page - 1) * size).Find(&items).Error; err != nil {
		return nil, 0, db.MapError(err)
	}
	return items, total, nil
}

// Count returns the number of contacts in the directory.
func (s *Store) Count(ctx context.Context) (int64, error) {
	q, err := s.queryScope(ctx)
	if err != nil {
		return 0, err
	}
	var total int64
	if err := q.Model(&Contact{}).Count(&total).Error; err != nil {
		return 0, db.MapError(err)
	}
	return total, nil
}

// Get returns the contact identified by id.
func (s *Store) Get(ctx context.Context, id int64) (*Contact, error) {
	q, err := s.queryScope(ctx)
	if err != nil {
		return nil, err
	}
	var c Contact
	if err := q.First(&c, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, db.MapError(err)
	}
	return &c, nil
}

// Create persists a new contact, stamping the owning tenant when a
// resolver is installed.
func (s *Store) Create(ctx context.Context, c *Contact) error {
	q := s.dbc.WithContext(ctx)
	if s.tenant != nil {
		tenantID, err := requiredTenant(ctx, s.tenant)
		if err != nil {
			return err
		}
		c.TenantID = tenantID
	}
	if err := q.Create(c).Error; err != nil {
		return db.MapError(err)
	}
	return nil
}

// Update persists the modified contact, matching the owning tenant when a
// resolver is installed. It returns ErrNotFound when no row matched.
func (s *Store) Update(ctx context.Context, c *Contact) error {
	q := s.dbc.WithContext(ctx)
	if s.tenant != nil {
		tenantID, err := requiredTenant(ctx, s.tenant)
		if err != nil {
			return err
		}
		q = q.Where("tenant_id = ?", tenantID)
		c.TenantID = tenantID
	}
	res := q.Model(&Contact{}).Where("id = ?", c.ID).
		Select("name", "email", "phone", "remark").Updates(c)
	if res.Error != nil {
		return db.MapError(res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete removes the contact identified by id, matching the owning tenant
// when a resolver is installed. It returns ErrNotFound when no row was
// removed.
func (s *Store) Delete(ctx context.Context, id int64) error {
	q, err := s.queryScope(ctx)
	if err != nil {
		return err
	}
	res := q.Delete(&Contact{}, id)
	if res.Error != nil {
		return db.MapError(res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// queryScope returns the base query for the call, tenant-filtered when a
// resolver is installed.
func (s *Store) queryScope(ctx context.Context) (*gorm.DB, error) {
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
