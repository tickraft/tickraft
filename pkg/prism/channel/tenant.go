// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package channel

import (
	"context"
	"errors"
)

// ErrTenantRequired is returned by tenant-scoped stores when the installed
// TenantResolver cannot produce a tenant ID from the request context. It
// surfaces as a 400-class error so a missing tenant context never degrades
// into an unscoped query that could leak data across tenants.
var ErrTenantRequired = errors.New("channel: tenant id required in context")

// TenantResolver extracts the owning tenant ID from a context. The second
// return value reports whether the context carries a tenant at all.
//
// The open-source build never installs a resolver: every store operation
// runs unscoped with tenant_id 0. Multi-tenant editions install one via
// NewTenantStore/NewTenantDeliveryStore so the same stores become
// tenant-scoped without any query duplication.
type TenantResolver func(ctx context.Context) (int64, bool)

// requiredTenant resolves the tenant for an operation that must be scoped.
// It fails with ErrTenantRequired when the resolver is installed but the
// context carries no usable tenant ID.
func requiredTenant(ctx context.Context, resolve TenantResolver) (int64, error) {
	tenantID, ok := resolve(ctx)
	if !ok || tenantID <= 0 {
		return 0, ErrTenantRequired
	}
	return tenantID, nil
}

// optionalTenant resolves the tenant for a lenient operation (delivery
// recording, last-used stamping): a missing tenant yields 0, which the
// column default already uses for system-level rows.
func optionalTenant(ctx context.Context, resolve TenantResolver) int64 {
	if resolve == nil {
		return 0
	}
	tenantID, _ := resolve(ctx)
	return tenantID
}
