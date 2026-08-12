// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/tickraft/tickraft/pkg/auth"
	"github.com/tickraft/tickraft/pkg/auth/jwt"
	"github.com/tickraft/tickraft/pkg/auth/password"
	"github.com/tickraft/tickraft/pkg/user"
)

// Authenticator provides authentication (identity verification).
type Authenticator interface {
	// Login authenticates a user and returns a login result containing the
	// token pair plus policy flags (e.g. MustChangePassword).
	Login(ctx context.Context, username, password string) (*auth.LoginResult, error)
	// Verify validates a JWT token and returns the user.
	Verify(ctx context.Context, token string) (*user.User, error)
}

// Authorizer provides authorization (permission checking).
type Authorizer interface {
	// Can checks if a user has permission to perform an action on a asset.
	Can(ctx context.Context, user *user.User, action string, assetType string, assetID int64) (bool, error)
}

// Registrar provides user registration.
type Registrar interface {
	// Register creates a new user.
	Register(ctx context.Context, username, password, email string) (*user.User, error)
}

// Policy defines the permission checking strategy.
// The default implementation provides an RBAC policy.
type Policy interface {
	// Check returns whether the user with the given role is allowed to
	// perform the specified action on the asset type.
	Check(role int, action string, assetType string) bool
}

// jwtAuthenticator implements Authenticator using JWT for token operations.
type jwtAuthenticator struct {
	users      user.Store
	blacklist  auth.BlacklistStore
	jwt        *jwt.JWT
	bcryptCost int
}

// NewAuthenticator creates a new Authenticator backed by the given stores
// and JWT manager. bcryptCost of 0 means bcrypt.DefaultCost will be used.
func NewAuthenticator(users user.Store, blacklist auth.BlacklistStore, jwtMgr *jwt.JWT, bcryptCost int) Authenticator {
	return &jwtAuthenticator{
		users:      users,
		blacklist:  blacklist,
		jwt:        jwtMgr,
		bcryptCost: bcryptCost,
	}
}

// Login authenticates a user by username and password, returning a login
// result containing the token pair and policy flags on success.
func (a *jwtAuthenticator) Login(ctx context.Context, username, pwd string) (*auth.LoginResult, error) {
	user, err := a.users.GetByUsername(ctx, username)
	if err != nil {
		return nil, auth.ErrUnauthorized
	}

	if err = password.Verify(user.PasswordHash, pwd); err != nil {
		return nil, auth.ErrUnauthorized
	}

	// Build jwt.UserClaims from user data.
	//
	// The runtime is single-tenant; TenantID is left as the zero
	// value. The extended User model embeds user.User and
	// populates TenantID from its own augmented user type before issuing tokens.
	claims := jwt.UserClaims{
		UID:      user.ID,
		Username: user.Username,
		Role:     user.Role,
	}

	tokenPair, err := a.jwt.GenerateTokenPair(claims)
	if err != nil {
		return nil, fmt.Errorf("auth: generate token pair: %w", err)
	}

	return &auth.LoginResult{
		TokenPair:          &jwt.TokenPair{AccessToken: tokenPair.AccessToken, RefreshToken: tokenPair.RefreshToken},
		MustChangePassword: user.MustChangePassword,
	}, nil
}

// Verify validates a JWT token and returns the associated user.
func (a *jwtAuthenticator) Verify(ctx context.Context, token string) (*user.User, error) {
	claims, err := a.jwt.ValidateToken(token, auth.TokenTypeAccess)
	if err != nil {
		return nil, auth.ErrUnauthorized
	}

	user, err := a.users.GetByID(ctx, claims.UID)
	if err != nil {
		return nil, auth.ErrUnauthorized
	}

	return user, nil
}

// userRegistrar implements Registrar for user registration.
type userRegistrar struct {
	users      user.Store
	bcryptCost int
}

// NewRegistrar creates a new Registrar backed by the given user store.
// bcryptCost of 0 means bcrypt.DefaultCost will be used.
func NewRegistrar(users user.Store, bcryptCost int) Registrar {
	return &userRegistrar{
		users:      users,
		bcryptCost: bcryptCost,
	}
}

// Register creates a new user.
func (r *userRegistrar) Register(ctx context.Context, username, pwd, email string) (*user.User, error) {
	// Check if username already exists
	if _, err := r.users.GetByUsername(ctx, username); err == nil {
		return nil, auth.ErrUserExists
	}

	// Hash the password
	hashedPwd, err := password.HashWithCost(pwd, r.bcryptCost)
	if err != nil {
		return nil, fmt.Errorf("auth: hash password: %w", err)
	}

	// Create the user record
	now := time.Now()
	id, err := r.users.Create(ctx, username, hashedPwd, email, auth.RoleDeveloper)
	if err != nil {
		return nil, fmt.Errorf("auth: create user: %w", err)
	}

	return &user.User{
		ID:           id,
		Username:     username,
		PasswordHash: hashedPwd,
		Role:         auth.RoleDeveloper,
		Email:        email,
		CreatedAt:    now,
	}, nil
}

// rbacPolicy implements Policy with a role-based access control strategy.
// admin: full access to all resources.
// developer: read/write on tasks, devices, alerts; read on others.
// visitor: read-only on all resources.
type rbacPolicy struct {
	// rules maps role -> assetType -> set of allowed actions.
	rules map[int]map[string]map[string]bool
}

// newRBACPolicy creates the default RBAC policy.
func newRBACPolicy() *rbacPolicy {
	rbac := &rbacPolicy{
		rules: make(map[int]map[string]map[string]bool),
	}

	// Admin: full access
	rbac.rules[auth.RoleAdmin] = map[string]map[string]bool{
		"*": {auth.ActionRead: true, auth.ActionWrite: true, auth.ActionDelete: true},
	}

	// Developer: manage tasks, devices, alerts; read others
	devResources := map[string]map[string]bool{
		"task":   {auth.ActionRead: true, auth.ActionWrite: true, auth.ActionDelete: true},
		"device": {auth.ActionRead: true, auth.ActionWrite: true, auth.ActionDelete: false},
		"alert":  {auth.ActionRead: true, auth.ActionWrite: true, auth.ActionDelete: false},
		"*":      {auth.ActionRead: true, auth.ActionWrite: false, auth.ActionDelete: false},
	}
	rbac.rules[auth.RoleDeveloper] = devResources

	// Visitor: read-only
	rbac.rules[auth.RoleVisitor] = map[string]map[string]bool{
		"*": {auth.ActionRead: true, auth.ActionWrite: false, auth.ActionDelete: false},
	}

	return rbac
}

// Check returns whether the given role is allowed to perform the action on the asset type.
func (rbac *rbacPolicy) Check(role int, action string, assetType string) bool {
	resourceRules, ok := rbac.rules[role]
	if !ok {
		return false
	}

	// Check asset-specific rules first
	if actions, found := resourceRules[assetType]; found {
		return actions[action]
	}

	// Fall back to wildcard rules
	if actions, found := resourceRules["*"]; found {
		return actions[action]
	}

	return false
}

// rbacAuthorizer implements Authorizer using the built-in RBAC policy.
type rbacAuthorizer struct {
	policy Policy
}

// NewAuthorizer creates a new Authorizer backed by the built-in RBAC policy.
// The runtime is single-tenant: all users belong to tenant 0 and
// permission checks are resolved by the default RBAC rules.
func NewAuthorizer() Authorizer {
	return &rbacAuthorizer{
		policy: newRBACPolicy(),
	}
}

// Can checks if a user has permission to perform an action on a asset.
func (a *rbacAuthorizer) Can(ctx context.Context, user *user.User, action string, assetType string, assetID int64) (bool, error) {
	if user == nil {
		return false, nil
	}

	return a.policy.Check(user.Role, action, assetType), nil
}

// DefaultPolicy returns the default RBAC policy.
func DefaultPolicy() Policy {
	return newRBACPolicy()
}
