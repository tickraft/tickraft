// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package auth

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
	rbac.rules[RoleAdmin] = map[string]map[string]bool{
		"*": {ActionRead: true, ActionWrite: true, ActionDelete: true},
	}

	// Developer: manage tasks, devices, alerts; read others
	devResources := map[string]map[string]bool{
		"task":   {ActionRead: true, ActionWrite: true, ActionDelete: true},
		"device": {ActionRead: true, ActionWrite: true, ActionDelete: false},
		"alert":  {ActionRead: true, ActionWrite: true, ActionDelete: false},
		"*":      {ActionRead: true, ActionWrite: false, ActionDelete: false},
	}
	rbac.rules[RoleDeveloper] = devResources

	// Visitor: read-only
	rbac.rules[RoleVisitor] = map[string]map[string]bool{
		"*": {ActionRead: true, ActionWrite: false, ActionDelete: false},
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

// Compile-time assertion that rbacPolicy satisfies Policy.
var _ Policy = (*rbacPolicy)(nil)

// DefaultPolicy returns the default RBAC policy.
func DefaultPolicy() Policy {
	return newRBACPolicy()
}
