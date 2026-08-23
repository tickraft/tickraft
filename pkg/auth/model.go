// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package auth

import "time"

// TokenBlacklist represents a revoked JWT token entry.
//
// The table sys_token_blacklist is populated whenever a token is explicitly
// revoked (e.g. on logout or administrative revocation) and consulted by the
// authenticator on every request to short-circuit validation of tokens that
// have been invalidated before their natural expiry.
//
// Rows are periodically purged by the maintenance loop once their expired_at
// timestamp is in the past; see internal/service.runMaintenanceSweep.
type TokenBlacklist struct {
	ID        int64     `json:"id"`
	TokenJTI  string    `json:"token_jti"`
	ExpiredAt time.Time `json:"expired_at"`
	CreatedAt time.Time `json:"created_at"`
}

// TableName returns the database table name for TokenBlacklist.
func (TokenBlacklist) TableName() string {
	return "sys_token_blacklist"
}
