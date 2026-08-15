// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package auth

import (
	"context"
	"time"

	"github.com/tickraft/tickraft/pkg/user"
)

// TokenPair holds the access and refresh tokens returned after authentication.
// This is a handler-local type to avoid importing pkg/auth or pkg/auth/jwt.
// When MFA is required, AccessToken and RefreshToken are empty and the caller
// must exchange MFATicket for tokens via the MFALogin endpoint.
type TokenPair struct {
	AccessToken        string `json:"access_token,omitempty"`
	RefreshToken       string `json:"refresh_token,omitempty"`
	MustChangePassword bool   `json:"must_change_password,omitempty"`
	// MFARequired is true when the user has MFA enabled and must submit a
	// TOTP code to complete login. AccessToken/RefreshToken are empty in
	// this case.
	MFARequired bool `json:"mfa_required,omitempty"`
	// MFATicket is the short-lived ticket to exchange via MFALogin.
	MFATicket string `json:"mfa_ticket,omitempty"`
}

// Service provides authentication and authorization operations.
// The concrete implementation is injected via RouteOption, keeping the
// handler package free of any dependency on pkg/auth or pkg/auth/jwt.
// The adapter wrapping *auth.Service is created in internal/api/router/router.go.
type Service interface {
	// Login authenticates a user and returns a token pair.
	Login(ctx context.Context, username, password string) (*TokenPair, error)
	// Logout blacklists the access token (by JTI and expiry) and optionally
	// parses and blacklists the given refresh token string.
	Logout(ctx context.Context, accessJTI string, accessExpireAt time.Time, refreshToken string) error
	// RefreshToken validates a refresh token and returns a new token pair.
	RefreshToken(ctx context.Context, refreshToken string) (*TokenPair, error)
	// ChangePassword changes the user's password. currentJTI identifies the
	// caller's in-flight token so it can be exempted from revocation.
	ChangePassword(ctx context.Context, userID int64, oldPassword, newPassword, currentJTI string) error
	// CreateAPIKey generates a new API key and returns the raw key plus metadata.
	CreateAPIKey(ctx context.Context, name string, expiredAt *time.Time) (rawKey string, info *user.APIKey, err error)
	// ListAPIKeys returns a page of API keys together with the total count.
	// page is 1-based and size is the maximum number of keys returned.
	ListAPIKeys(ctx context.Context, page, size int) ([]user.APIKey, int64, error)
	// RevokeAPIKey revokes an API key by ID.
	RevokeAPIKey(ctx context.Context, id int64) error
	// GetProfile returns the profile of the current user identified by userID.
	GetProfile(ctx context.Context, userID int64) (*UserProfile, error)
	// UpdateProfile updates the profile of the current user identified by
	// userID. Only nickname, email, language, and alert format style may be
	// changed through this endpoint.
	UpdateProfile(ctx context.Context, userID int64, req *UpdateProfileRequest) (*UserProfile, error)
}

// UserProfile represents the current user's profile. It is returned by
// GET /api/v1/system/profile and updated by PUT /api/v1/system/profile.
type UserProfile struct {
	ID               int64  `json:"id"`
	Username         string `json:"username"`
	Nickname         string `json:"nickname,omitempty"`
	Email            string `json:"email,omitempty"`
	Role             int    `json:"role"`
	Language         string `json:"language"`
	AlertFormatStyle string `json:"alert_format_style"`
}

// UpdateProfileRequest is the request body for PUT /api/v1/system/profile.
// Only the fields present in the request are updated; nil pointer fields
// are ignored so callers can perform partial updates.
type UpdateProfileRequest struct {
	Nickname         *string `json:"nickname,omitempty"`
	Email            *string `json:"email,omitempty"`
	Language         *string `json:"language,omitempty"`
	AlertFormatStyle *string `json:"alert_format_style,omitempty"`
}
