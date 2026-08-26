// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package router

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"

	authapi "github.com/tickraft/tickraft/pkg/api/handler/auth"
	"github.com/tickraft/tickraft/pkg/auth"
	"github.com/tickraft/tickraft/pkg/auth/apikey"
	"github.com/tickraft/tickraft/pkg/errdefs"
	"github.com/tickraft/tickraft/pkg/user"
)

// RevokeFunc invalidates all of a user's sessions except the one identified
// by exceptJTI. It is the injection seam for HA deployments that back the
// JWT blacklist with a shared store: after a password change, every replica
// must reject the user's other tokens. Editions without a shared blacklist
// leave it nil and only the current session is logged out.
type RevokeFunc func(userID int64, exceptJTI string) error

// apiKeyCacheEntry is a cached API key lookup result with its expiry.
type apiKeyCacheEntry struct {
	info      *apikey.Info
	expiresAt time.Time
}

// serviceAdapter wraps *auth.Service to satisfy the auth handler's Service
// interface. It converts jwt.TokenPair to the handler-local TokenPair so the
// handler package never needs to import pkg/auth or pkg/auth/jwt.
type serviceAdapter struct {
	svc *auth.Service
	// revoker, when set, invalidates all of the user's other sessions after
	// a successful password change (HA deployments).
	revoker RevokeFunc
}

// Login authenticates a user and returns a handler-local TokenPair.
func (adapter *serviceAdapter) Login(ctx context.Context, username, password string) (*authapi.TokenPair, error) {
	res, err := adapter.svc.Login(ctx, username, password)
	if err != nil {
		return nil, err
	}
	// When MFA is required, the token pair is nil; return an empty pair
	// carrying only the MFA signals so the frontend can redirect to the
	// MFA login flow.
	if res.MFARequired {
		return &authapi.TokenPair{
			MFARequired: true,
			MFATicket:   res.MFATicket,
		}, nil
	}
	return &authapi.TokenPair{
		AccessToken:        res.AccessToken,
		RefreshToken:       res.RefreshToken,
		MustChangePassword: res.MustChangePassword,
	}, nil
}

// Logout blacklists the access token and optionally the refresh token.
func (adapter *serviceAdapter) Logout(
	ctx context.Context,
	accessJTI string,
	accessExpireAt time.Time,
	refreshToken string,
) error {
	return adapter.svc.Logout(ctx, accessJTI, accessExpireAt, refreshToken)
}

// RefreshToken validates a refresh token and returns a handler-local TokenPair.
func (adapter *serviceAdapter) RefreshToken(ctx context.Context, refreshToken string) (*authapi.TokenPair, error) {
	tp, err := adapter.svc.RefreshToken(ctx, refreshToken)
	if err != nil {
		return nil, err
	}
	return &authapi.TokenPair{
		AccessToken:  tp.AccessToken,
		RefreshToken: tp.RefreshToken,
	}, nil
}

// ChangePassword changes the user's password. currentJTI identifies the
// caller's in-flight token so it can be exempted from revocation.
func (adapter *serviceAdapter) ChangePassword(
	ctx context.Context,
	userID int64,
	oldPassword, newPassword, currentJTI string,
) error {
	if err := adapter.svc.ChangePassword(ctx, userID, oldPassword, newPassword, currentJTI); err != nil {
		return err
	}
	if adapter.revoker != nil {
		if err := adapter.revoker(userID, currentJTI); err != nil {
			// Revocation failure must not fail the password change itself;
			// the old tokens expire naturally and the user can re-login.
			zap.L().Warn("auth adapter: revoke user tokens after password change",
				zap.Int64("user_id", userID), zap.Error(err))
		}
	}
	return nil
}

// CreateAPIKey generates a new API key and returns the raw key plus metadata.
func (adapter *serviceAdapter) CreateAPIKey(
	ctx context.Context,
	name string,
	expiredAt *time.Time,
) (string, *user.APIKey, error) {
	return adapter.svc.CreateAPIKey(ctx, name, expiredAt)
}

// ListAPIKeys returns a page of API keys together with the total count.
func (adapter *serviceAdapter) ListAPIKeys(ctx context.Context, page, size int) ([]user.APIKey, int64, error) {
	return adapter.svc.ListAPIKeys(ctx, page, size)
}

// RevokeAPIKey revokes an API key by ID.
func (adapter *serviceAdapter) RevokeAPIKey(ctx context.Context, id int64) error {
	return adapter.svc.RevokeAPIKey(ctx, id)
}

// GetProfile retrieves the profile of the current user identified by userID.
// It delegates to the auth service and projects the user.User into a
// handler-layer UserProfile.
func (adapter *serviceAdapter) GetProfile(ctx context.Context, userID int64) (*authapi.UserProfile, error) {
	u, err := adapter.svc.GetProfile(ctx, userID)
	if err != nil {
		return nil, err
	}
	return userToProfile(u), nil
}

// UpdateProfile updates the profile of the current user identified by userID.
// It delegates to the auth service and projects the updated user.User into a
// handler-layer UserProfile.
func (adapter *serviceAdapter) UpdateProfile(
	ctx context.Context,
	userID int64,
	req *authapi.UpdateProfileRequest,
) (*authapi.UserProfile, error) {
	if req == nil {
		return nil, errdefs.ErrInvalidRequest
	}
	u, err := adapter.svc.UpdateProfile(ctx, userID, auth.UpdateProfileParams{
		Nickname:         req.Nickname,
		Email:            req.Email,
		Language:         req.Language,
		AlertFormatStyle: req.AlertFormatStyle,
	})
	if err != nil {
		return nil, err
	}
	return userToProfile(u), nil
}

// userToProfile converts a user.User into a handler-layer UserProfile DTO.
func userToProfile(u *user.User) *authapi.UserProfile {
	return &authapi.UserProfile{
		ID:               u.ID,
		Username:         u.Username,
		Nickname:         u.Nickname,
		Email:            u.Email,
		Role:             u.Role,
		Language:         u.Language,
		AlertFormatStyle: u.AlertFormatStyle,
	}
}

// newAPIKeyGetter builds the API key keyGetter for the combined auth
// middleware. A short-TTL cache fronts the per-request DB lookup: API key
// metadata changes rarely, and the lookup sits on every authenticated request
// with an API key. Revocations take effect within the TTL window.
func newAPIKeyGetter(service *auth.Service) func(ctx context.Context, keyHash string) (*apikey.Info, error) {
	const apiKeyCacheTTL = 30 * time.Second
	var (
		apiKeyCacheMu sync.Mutex
		apiKeyCache   = make(map[string]apiKeyCacheEntry)
	)
	return func(ctx context.Context, keyHash string) (*apikey.Info, error) {
		now := time.Now()
		apiKeyCacheMu.Lock()
		if e, ok := apiKeyCache[keyHash]; ok && now.Before(e.expiresAt) {
			apiKeyCacheMu.Unlock()
			return e.info, nil
		}
		apiKeyCacheMu.Unlock()

		stored, err := service.GetAPIKeyByHash(ctx, keyHash)
		if err != nil {
			return nil, err
		}
		info := &apikey.Info{
			ID:        stored.ID,
			Name:      stored.Name,
			KeyPrefix: stored.KeyPrefix,
			KeyHash:   stored.KeyHash,
			Status:    stored.Status,
			CreatedAt: stored.CreatedAt,
			ExpiredAt: stored.ExpiredAt,
		}

		apiKeyCacheMu.Lock()
		// Bound the cache to avoid unbounded growth under key churn.
		if len(apiKeyCache) >= 1024 {
			apiKeyCache = make(map[string]apiKeyCacheEntry)
		}
		apiKeyCache[keyHash] = apiKeyCacheEntry{info: info, expiresAt: now.Add(apiKeyCacheTTL)}
		apiKeyCacheMu.Unlock()
		return info, nil
	}
}

// Compile-time assertion that serviceAdapter satisfies the handler SPI.
var _ authapi.Service = (*serviceAdapter)(nil)
