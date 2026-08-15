// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package auth

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/tickraft/tickraft/pkg/auth/apikey"
	"github.com/tickraft/tickraft/pkg/auth/jwt"
	"github.com/tickraft/tickraft/pkg/auth/password"
	"github.com/tickraft/tickraft/pkg/errdefs"
	"github.com/tickraft/tickraft/pkg/user"
	"go.uber.org/zap"
)

const (
	maxLoginFails   = 5
	lockoutDuration = 15 * time.Minute
	failWindow      = 5 * time.Minute
	// cleanupInterval is how often the background goroutine scans
	// loginFails for expired entries.
	cleanupInterval = time.Minute
	// failEntryTTL is the time after the last failure beyond which a
	// non-locked entry is considered stale and eligible for cleanup.
	// Set equal to lockoutDuration so that locked entries are retained
	// for the full lockout window before becoming eligible.
	failEntryTTL = lockoutDuration
)

var (
	// ErrTooManyRequests is returned when login is rate-limited.
	ErrTooManyRequests = fmt.Errorf("auth: %w", errdefs.ErrTooManyRequests)
	// ErrWeakPassword is returned when a password does not meet strength requirements.
	ErrWeakPassword = fmt.Errorf("auth: %w", errdefs.ErrInvalidArgument)
	// ErrInvalidUsername is returned when a username does not meet format requirements.
	ErrInvalidUsername = fmt.Errorf("auth: %w", errdefs.ErrInvalidArgument)
)

// Service implements the single-user auth business logic.
type Service struct {
	jwt       *jwt.JWT
	users     user.Store
	apiKeys   user.APIKeyStore
	blacklist BlacklistStore

	// Login rate limiter: username -> fail record
	mu              sync.Mutex
	loginFails      map[string]*loginFailRecord
	cleanupInterval time.Duration
	cancel          context.CancelFunc
}

type loginFailRecord struct {
	count        int
	lockedUntil  time.Time
	lastFailedAt time.Time
}

// NewService creates a new auth service.
func NewService(
	jwtMgr *jwt.JWT,
	users user.Store,
	apiKeys user.APIKeyStore,
	blacklist BlacklistStore,
) *Service {
	s := &Service{
		jwt:             jwtMgr,
		users:           users,
		apiKeys:         apiKeys,
		blacklist:       blacklist,
		loginFails:      make(map[string]*loginFailRecord),
		cleanupInterval: cleanupInterval,
	}
	s.startCleanupLoop()
	return s
}

// Login authenticates a user and returns a login result containing the token
// pair and policy flags (e.g. MustChangePassword).
func (s *Service) Login(ctx context.Context, username, pwd string) (*LoginResult, error) {
	if err := validateUsername(username); err != nil {
		zap.L().Warn("auth login: validate username", zap.String("username", username), zap.Error(err))
		return nil, err
	}

	if err := s.checkRateLimit(username); err != nil {
		zap.L().Warn("auth login: rate limit", zap.String("username", username), zap.Error(err))
		return nil, err
	}

	user, err := s.users.GetByUsername(ctx, username)
	if err != nil {
		zap.L().Warn("auth login: get user by username", zap.String("username", username), zap.Error(err))
		s.recordLoginFailure(username)
		return nil, ErrUnauthorized
	}

	// Reject disabled users (Status == 0). This is a generic security check:
	// the user.User.Status field is defined in the user package and
	// conventionally 0=disabled, 1=active.
	if user.Status == 0 {
		zap.L().Warn("auth login: user disabled", zap.String("username", username), zap.Int64("user_id", user.ID))
		s.recordLoginFailure(username)
		return nil, ErrUnauthorized
	}

	if err := password.Verify(user.PasswordHash, pwd); err != nil {
		zap.L().Warn("auth login: password verify", zap.String("username", username), zap.Int64("user_id", user.ID), zap.Error(err))
		s.recordLoginFailure(username)
		return nil, ErrUnauthorized
	}

	s.recordLoginSuccess(username)

	// The runtime is single-tenant; TenantID is left as the zero
	// value. The runtime populates TenantID from the augmented user
	// type before issuing tokens.
	claims := jwt.UserClaims{
		UID:      user.ID,
		Username: user.Username,
		Role:     user.Role,
	}

	tokenPair, err := s.jwt.GenerateTokenPair(claims)
	if err != nil {
		zap.L().Error("auth login: generate token pair", zap.String("username", username), zap.Int64("user_id", user.ID), zap.Error(err))
		return nil, fmt.Errorf("generate token pair: %w", err)
	}

	return &LoginResult{
		TokenPair:          &jwt.TokenPair{AccessToken: tokenPair.AccessToken, RefreshToken: tokenPair.RefreshToken},
		MustChangePassword: user.MustChangePassword,
	}, nil
}

// IssueTokens issues a token pair for an already-authenticated user. It is
// intended for flows where authentication is completed outside of Login, such
// as external identity provider callback handlers: after the caller validates
// the external identity, it calls IssueTokens to obtain JWTs without
// re-verifying the password. The caller must ensure the user has been fully
// authenticated before calling this method.
func (s *Service) IssueTokens(ctx context.Context, user *user.User) (*LoginResult, error) {
	if user == nil {
		return nil, fmt.Errorf("auth: issue tokens: nil user")
	}

	// The runtime is single-tenant; TenantID is left as the zero
	// value. The runtime populates TenantID from the augmented user
	// type before issuing tokens.
	claims := jwt.UserClaims{
		UID:      user.ID,
		Username: user.Username,
		Role:     user.Role,
	}

	tokenPair, err := s.jwt.GenerateTokenPair(claims)
	if err != nil {
		return nil, fmt.Errorf("generate token pair: %w", err)
	}

	return &LoginResult{
		TokenPair:          &jwt.TokenPair{AccessToken: tokenPair.AccessToken, RefreshToken: tokenPair.RefreshToken},
		MustChangePassword: user.MustChangePassword,
	}, nil
}

// Logout blacklists the access token (identified by JTI and expiry) and,
// if a refresh token string is provided, parses and blacklists it too.
func (s *Service) Logout(ctx context.Context, accessJTI string, accessExpireAt time.Time, refreshToken string) error {
	if accessJTI != "" && !accessExpireAt.IsZero() {
		if err := s.blacklist.Add(ctx, accessJTI, accessExpireAt); err != nil {
			return fmt.Errorf("blacklist access token: %w", err)
		}
	}
	if refreshToken != "" {
		jti, expireAt, err := s.jwt.ParseForRevocation(refreshToken)
		if err != nil {
			if errors.Is(err, jwt.ErrTokenExpired) {
				// Token already expired; nothing to blacklist.
				return nil
			}
			return fmt.Errorf("parse refresh token for revocation: %w", err)
		}
		if jti != "" && !expireAt.IsZero() {
			if err := s.blacklist.Add(ctx, jti, expireAt); err != nil {
				return fmt.Errorf("blacklist refresh token: %w", err)
			}
		}
	}
	return nil
}

// RefreshToken validates a refresh token, rejects it if its JTI has been
// blacklisted (revoked or already redeemed), issues a new token pair, and
// blacklists the old refresh token's JTI so it cannot be replayed. This
// implements refresh token rotation: each refresh token is redeemable
// exactly once.
func (s *Service) RefreshToken(ctx context.Context, refreshToken string) (*jwt.TokenPair, error) {
	jti, expireAt, err := s.jwt.ParseForRevocation(refreshToken)
	if err == nil && jti != "" {
		revoked, rerr := s.blacklist.Exists(ctx, jti)
		if rerr != nil {
			return nil, fmt.Errorf("check refresh token blacklist: %w", rerr)
		}
		if revoked {
			return nil, fmt.Errorf("refresh token: %w", ErrUnauthorized)
		}
	}

	tokenPair, err := s.jwt.RefreshToken(refreshToken)
	if err != nil {
		return nil, fmt.Errorf("refresh token: %w", err)
	}

	// Redeem the old refresh token: blacklist its JTI until its original
	// expiry so the same token cannot be exchanged twice.
	if jti != "" && !expireAt.IsZero() {
		if err := s.blacklist.Add(ctx, jti, expireAt); err != nil {
			zap.L().Warn("auth: blacklist redeemed refresh token failed",
				zap.String("jti", jti),
				zap.Error(err),
			)
		}
	}

	return &jwt.TokenPair{
		AccessToken:  tokenPair.AccessToken,
		RefreshToken: tokenPair.RefreshToken,
	}, nil
}

// ChangePassword changes the user's password. On success it clears the
// MustChangePassword flag. The currentJTI parameter identifies the JWT
// of the requesting session; token revocation after credential changes
// is handled by the caller via Logout, so this method does not perform
// JTI-based revocation.
func (s *Service) ChangePassword(ctx context.Context, userID int64, oldPwd, newPwd, currentJTI string) error {
	if err := validatePassword(newPwd); err != nil {
		return err
	}

	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return ErrUnauthorized
	}

	if err := password.Verify(user.PasswordHash, oldPwd); err != nil {
		return ErrUnauthorized
	}

	hash, err := password.Hash(newPwd)
	if err != nil {
		return fmt.Errorf("hash new password: %w", err)
	}

	if err := s.users.UpdatePassword(ctx, userID, hash); err != nil {
		return fmt.Errorf("update password: %w", err)
	}

	// Clear the MustChangePassword flag so the user is no longer forced to
	// change password on the next login. This is best-effort: the password
	// has already been updated, so a flag-clear failure is logged but does
	// not fail the operation.
	if user.MustChangePassword {
		if err := s.users.Update(ctx, userID, map[string]any{"must_change_password": false}); err != nil {
			zap.L().Warn("auth: clear must_change_password failed",
				zap.Int64("user_id", userID),
				zap.Error(err),
			)
		}
	}

	return nil
}

// CreateAPIKey generates a new API key.
func (s *Service) CreateAPIKey(ctx context.Context, name string, expiredAt *time.Time) (rawKey string, info *user.APIKey, err error) {
	raw, hash, prefix, err := apikey.GenerateAPIKey()
	if err != nil {
		return "", nil, fmt.Errorf("generate api key: %w", err)
	}

	id, err := s.apiKeys.Create(ctx, name, prefix, hash, expiredAt)
	if err != nil {
		return "", nil, fmt.Errorf("store api key: %w", err)
	}

	return raw, &user.APIKey{
		ID:        id,
		Name:      name,
		KeyPrefix: prefix,
		KeyHash:   hash,
		Status:    apikey.StatusActive,
		CreatedAt: time.Now(),
		ExpiredAt: expiredAt,
	}, nil
}

// ListAPIKeys returns a page of API keys together with the total count.
// page is 1-based and size is the maximum number of keys returned.
func (s *Service) ListAPIKeys(ctx context.Context, page, size int) ([]user.APIKey, int64, error) {
	return s.apiKeys.List(ctx, page, size)
}

// RevokeAPIKey revokes an API key by ID.
func (s *Service) RevokeAPIKey(ctx context.Context, id int64) error {
	return s.apiKeys.Revoke(ctx, id)
}

// GetAPIKeyByHash looks up an API key by its SHA-256 hash. It is used
// by the API key authentication middleware to resolve a key from its
// hash without exposing the raw key store.
func (s *Service) GetAPIKeyByHash(ctx context.Context, hash string) (*user.APIKey, error) {
	return s.apiKeys.GetByHash(ctx, hash)
}

// ValidateAPIKey validates a raw API key against the store.
func (s *Service) ValidateAPIKey(ctx context.Context, rawKey string) (*user.APIKey, error) {
	hash := apikey.HashAPIKey(rawKey)
	stored, err := s.apiKeys.GetByHash(ctx, hash)
	if err != nil {
		return nil, apikey.ErrAPIKeyInvalid
	}

	if err := apikey.ValidateAPIKey(rawKey, stored.KeyHash, stored.Status, stored.ExpiredAt); err != nil {
		return nil, err
	}

	return stored, nil
}

// GetProfile retrieves the user identified by userID. It returns the full
// user.User so the caller (the serviceAdapter in internal/api/router) can
// project it into a handler-layer UserProfile without the auth package
// depending on the handler package.
func (s *Service) GetProfile(ctx context.Context, userID int64) (*user.User, error) {
	return s.users.GetByID(ctx, userID)
}

// UpdateProfile updates the profile fields of the user identified by userID.
// Only non-nil pointer arguments are applied; nil pointers leave the
// corresponding field unchanged. It returns the updated user after the
// change is persisted.
func (s *Service) UpdateProfile(ctx context.Context, userID int64, nickname, email, language, alertFormatStyle *string) (*user.User, error) {
	data := make(map[string]any)
	if nickname != nil {
		data["nickname"] = *nickname
	}
	if email != nil {
		data["email"] = *email
	}
	if language != nil {
		data["language"] = *language
	}
	if alertFormatStyle != nil {
		data["alert_format_style"] = *alertFormatStyle
	}
	if len(data) == 0 {
		// Nothing to update; return the current user without a write.
		return s.users.GetByID(ctx, userID)
	}
	if err := s.users.Update(ctx, userID, data); err != nil {
		return nil, err
	}
	return s.users.GetByID(ctx, userID)
}

// Close stops the background cleanup goroutine, releasing resources.
// It is safe to call multiple times and safe to call on a Service that
// was constructed directly (without NewService) where no goroutine was started.
func (s *Service) Close() {
	if s.cancel != nil {
		s.cancel()
	}
}

// checkRateLimit returns an error if the username is currently locked out.
func (s *Service) checkRateLimit(username string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	rec, exists := s.loginFails[username]
	if !exists {
		return nil
	}

	if !rec.lockedUntil.IsZero() && time.Now().Before(rec.lockedUntil) {
		return ErrTooManyRequests
	}

	// Lockout expired, reset.
	if !rec.lockedUntil.IsZero() && !time.Now().Before(rec.lockedUntil) {
		delete(s.loginFails, username)
	}

	return nil
}

// recordLoginFailure increments the failure counter and sets lockout if
// threshold reached. Failures older than failWindow reset the counter so
// only failures within the window accumulate toward the lockout threshold.
func (s *Service) recordLoginFailure(username string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()

	rec, exists := s.loginFails[username]
	if !exists {
		rec = &loginFailRecord{}
		s.loginFails[username] = rec
	} else if rec.count > 0 && now.Sub(rec.lastFailedAt) > failWindow {
		// Previous failures fell outside the window; start counting fresh.
		rec.count = 0
	}

	rec.count++
	rec.lastFailedAt = now
	if rec.count >= maxLoginFails {
		rec.lockedUntil = now.Add(lockoutDuration)
	}
}

// recordLoginSuccess clears the failure counter for the username.
func (s *Service) recordLoginSuccess(username string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.loginFails, username)
}

// startCleanupLoop launches the background goroutine that periodically
// removes expired login-fail entries. The goroutine is stopped by
// canceling the context stored on s.cancel (via Close).
func (s *Service) startCleanupLoop() {
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	go s.runCleanupLoop(ctx)
}

// runCleanupLoop is the background goroutine that periodically calls
// cleanupExpiredFails. It exits when ctx is canceled.
func (s *Service) runCleanupLoop(ctx context.Context) {
	interval := s.cleanupInterval
	if interval <= 0 {
		interval = cleanupInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.cleanupExpiredFails()
		}
	}
}

// cleanupExpiredFails removes login-fail entries that are no longer
// active. An entry is removed when it is not currently locked and its
// last failure is older than failEntryTTL. Locked entries are retained
// until the lockout expires.
func (s *Service) cleanupExpiredFails() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for username, rec := range s.loginFails {
		// Keep entries that are still locked.
		if !rec.lockedUntil.IsZero() && now.Before(rec.lockedUntil) {
			continue
		}
		// Remove entries whose last failure is older than the TTL.
		if now.Sub(rec.lastFailedAt) > failEntryTTL {
			delete(s.loginFails, username)
		}
	}
}

// validateUsername checks that the username matches the canonical
// user.UsernameRegex (3-64 chars, only letters, digits and underscores).
func validateUsername(username string) error {
	if !user.UsernameRegex.MatchString(username) {
		return ErrInvalidUsername
	}
	return nil
}

// validatePassword checks that the password is 8-128 chars with at least one letter and one digit.
func validatePassword(pwd string) error {
	n := len(pwd)
	if n < 8 || n > 128 {
		return ErrWeakPassword
	}

	var hasLetter, hasDigit bool
	for _, r := range pwd {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			hasLetter = true
		}
		if r >= '0' && r <= '9' {
			hasDigit = true
		}
		if hasLetter && hasDigit {
			break
		}
	}

	if !hasLetter || !hasDigit {
		return ErrWeakPassword
	}

	return nil
}
