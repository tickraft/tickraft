// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package jwt

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	jwtgo "github.com/golang-jwt/jwt/v5"
)

// Errors for JWT operations.
var (
	// ErrTokenExpired is returned when a token has passed its expiration time.
	ErrTokenExpired = errors.New("auth: token expired")
	// ErrTokenInvalid is returned when a token cannot be parsed or verified.
	ErrTokenInvalid = errors.New("auth: token invalid")
	// ErrTokenInBlacklist is returned when a token has been revoked.
	ErrTokenInBlacklist = errors.New("jwt: token is in blacklist")
	// ErrSecretTooShort is returned by New when the signing secret is
	// shorter than the minimum required length.
	ErrSecretTooShort = errors.New("auth: secret must be at least 32 bytes")
)

// Claims extends the standard JWT claims with tickraft-specific fields.
type Claims struct {
	UserID int64 `json:"user_id"`
	// TenantID is the tenant identifier for multi-tenancy isolation.
	// The runtime is single-tenant: this field is always 0.
	// The runtime sets the actual tenant ID during token generation.
	TenantID int64  `json:"tenant_id"`
	Role     int    `json:"role"` // 0=visitor 1=developer 2=admin
	Username string `json:"username,omitempty"`
	Region   string `json:"region,omitempty"` // cn, global, private
	jwtgo.RegisteredClaims
}

// JTI returns the token's unique identifier from the standard registered claims.
func (c Claims) JTI() string {
	return c.ID
}

// newJTI generates a random 32-character hex string using crypto/rand.
func newJTI() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// Fallback to timestamp-based ID if crypto/rand fails.
		return time.Now().Format("20060102150405.000000000")
	}
	return hex.EncodeToString(buf[:])
}

// Sign creates a signed JWT token string from the given claims and secret.
func Sign(claims Claims, secret string) (string, error) {
	token := jwtgo.NewWithClaims(jwtgo.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

// SignToken creates a signed JWT token with full user info including username
// and a unique JTI for revocation tracking.
func SignToken(userID, tenantID int64, role int, username, secret string, expiry time.Duration) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID:   userID,
		TenantID: tenantID,
		Role:     role,
		Username: username,
		RegisteredClaims: jwtgo.RegisteredClaims{
			ID:        newJTI(),
			ExpiresAt: jwtgo.NewNumericDate(now.Add(expiry)),
			IssuedAt:  jwtgo.NewNumericDate(now),
			NotBefore: jwtgo.NewNumericDate(now),
		},
	}
	return Sign(claims, secret)
}

// Parse parses and validates a JWT token string using the given secret.
// Returns the claims if the token is valid.
func Parse(tokenString string, secret string) (*Claims, error) {
	token, err := jwtgo.ParseWithClaims(tokenString, &Claims{}, func(token *jwtgo.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwtgo.SigningMethodHMAC); !ok {
			return nil, ErrTokenInvalid
		}
		return []byte(secret), nil
	})
	if err != nil {
		if errors.Is(err, jwtgo.ErrTokenExpired) {
			return nil, ErrTokenExpired
		}
		return nil, ErrTokenInvalid
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, ErrTokenInvalid
	}
	return claims, nil
}

const (
	defaultAccessExpire  = 2 * time.Hour
	defaultRefreshExpire = 7 * 24 * time.Hour
	minSecretLen         = 32

	// TokenTypeAccess identifies an access token.
	TokenTypeAccess = "access"
	// TokenTypeRefresh identifies a refresh token.
	TokenTypeRefresh = "refresh"
)

// TokenPair holds the access and refresh tokens returned after authentication.
type TokenPair struct {
	// AccessToken is the short-lived token used for API authorization.
	AccessToken string
	// RefreshToken is the long-lived token used to obtain a new access token.
	RefreshToken string
}

// UserClaims represents the decoded claims embedded in a JWT token.
type UserClaims struct {
	// UID is the unique identifier of the authenticated user.
	UID int64
	// Username is the login name of the user.
	Username string
	// Role is the authorization role of the user (RoleVisitor, RoleDeveloper, RoleAdmin).
	Role int
	// TenantID is the tenant to which the user belongs.
	// The runtime is single-tenant: this field is always 0.
	// The runtime sets the actual tenant ID during login.
	TenantID int64
	// JTI is the unique token identifier used for revocation tracking.
	JTI string
	// Region is the deployment region of the token (cn, global, private).
	Region string
	// ExpiresAt is the token expiration time, used for blacklist TTL.
	ExpiresAt time.Time
}

// BlacklistChecker reports whether a JTI is in the token blacklist.
type BlacklistChecker func(jti string) (bool, error)

// Config holds the configuration for JWT.
type Config struct {
	// Secret is the HMAC signing key; must be at least 32 bytes.
	Secret string
	// AccessExpire is the access token lifetime. Defaults to 2 hours if zero.
	AccessExpire time.Duration
	// RefreshExpire is the refresh token lifetime. Defaults to 7 days if zero.
	RefreshExpire time.Duration
	// Issuer is the value written to the JWT iss claim.
	Issuer string
}

// JWT generates and validates dual access/refresh token pairs.
type JWT struct {
	config           Config
	blacklistChecker BlacklistChecker
}

// New creates a new JWT instance with the given config and blacklist checker.
// It returns ErrSecretTooShort if Secret is shorter than 32 bytes.
func New(config Config, blacklistChecker BlacklistChecker) (*JWT, error) {
	if len(config.Secret) < minSecretLen {
		return nil, fmt.Errorf("%w: got %d bytes", ErrSecretTooShort, len(config.Secret))
	}
	if config.AccessExpire <= 0 {
		config.AccessExpire = defaultAccessExpire
	}
	if config.RefreshExpire <= 0 {
		config.RefreshExpire = defaultRefreshExpire
	}
	return &JWT{
		config:           config,
		blacklistChecker: blacklistChecker,
	}, nil
}

// GenerateTokenPair creates a new access/refresh token pair for the given user claims.
func (jwt *JWT) GenerateTokenPair(claims UserClaims) (*TokenPair, error) {
	accessToken, err := jwt.signToken(claims, TokenTypeAccess, jwt.config.AccessExpire)
	if err != nil {
		return nil, fmt.Errorf("generate access token: %w", err)
	}
	refreshToken, err := jwt.signToken(claims, TokenTypeRefresh, jwt.config.RefreshExpire)
	if err != nil {
		return nil, fmt.Errorf("generate refresh token: %w", err)
	}
	return &TokenPair{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
	}, nil
}

// ValidateToken parses and validates a token string, ensuring it matches the
// expected tokenType and is not in the blacklist.
func (jwt *JWT) ValidateToken(tokenStr string, tokenType string) (*UserClaims, error) {
	claims, err := Parse(tokenStr, jwt.config.Secret)
	if err != nil {
		return nil, err
	}

	// Verify token type matches the Subject field.
	if claims.Subject != tokenType {
		return nil, ErrTokenInvalid
	}

	// Check blacklist.
	if jwt.blacklistChecker != nil {
		jti := claims.JTI()
		if jti == "" {
			return nil, ErrTokenInvalid
		}
		blacklisted, err := jwt.blacklistChecker(jti)
		if err != nil {
			return nil, fmt.Errorf("check blacklist: %w", err)
		}
		if blacklisted {
			return nil, ErrTokenInBlacklist
		}
	}

	userClaims := &UserClaims{
		UID:      claims.UserID,
		Username: claims.Username,
		Role:     claims.Role,
		TenantID: claims.TenantID,
		JTI:      claims.JTI(),
		Region:   claims.Region,
	}
	if claims.ExpiresAt != nil {
		userClaims.ExpiresAt = claims.ExpiresAt.Time
	}
	return userClaims, nil
}

// ParseForRevocation parses a token and returns its JTI and expiry without
// enforcing token type or blacklist checks. This is used by logout flows
// where the token needs to be added to the blacklist.
func (jwt *JWT) ParseForRevocation(tokenStr string) (jti string, expireAt time.Time, err error) {
	claims, err := Parse(tokenStr, jwt.config.Secret)
	if err != nil {
		return "", time.Time{}, err
	}
	jti = claims.JTI()
	if claims.ExpiresAt != nil {
		expireAt = claims.ExpiresAt.Time
	}
	return jti, expireAt, nil
}

// RefreshToken validates a refresh token and generates a new token pair.
// The caller is responsible for adding the old refresh token's JTI to the blacklist.
func (jwt *JWT) RefreshToken(refreshToken string) (*TokenPair, error) {
	userClaims, err := jwt.ValidateToken(refreshToken, TokenTypeRefresh)
	if err != nil {
		return nil, fmt.Errorf("validate refresh token: %w", err)
	}
	return jwt.GenerateTokenPair(*userClaims)
}

// signToken creates a signed JWT with the given claims, token type, and expiry.
func (jwt *JWT) signToken(claims UserClaims, tokenType string, expiry time.Duration) (string, error) {
	now := time.Now()
	jwtClaims := Claims{
		UserID:   claims.UID,
		TenantID: claims.TenantID,
		Role:     claims.Role,
		Username: claims.Username,
		Region:   claims.Region,
		RegisteredClaims: jwtgo.RegisteredClaims{
			ID:        newJTI(),
			Issuer:    jwt.config.Issuer,
			Subject:   tokenType,
			ExpiresAt: jwtgo.NewNumericDate(now.Add(expiry)),
			IssuedAt:  jwtgo.NewNumericDate(now),
			NotBefore: jwtgo.NewNumericDate(now),
		},
	}
	return Sign(jwtClaims, jwt.config.Secret)
}
