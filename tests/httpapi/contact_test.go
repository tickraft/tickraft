// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package httpapi

import (
	"fmt"
	"net/http"
	"testing"
)

// contactPayload builds a contact request body, overriding the fields that
// matter to the test.
func contactPayload(name, email, phone string) map[string]string {
	body := map[string]string{"name": name}
	if email != "" {
		body["email"] = email
	}
	if phone != "" {
		body["phone"] = phone
	}
	return body
}

// TestContactDirectoryCRUD walks the full contact lifecycle over the API:
// create, read, list (with keyword + pagination), update, delete. The CE
// quota provider registered by the harness keeps the ceiling at 2 (N4), so
// the lifecycle also demonstrates the successful path stays inside it by
// reusing the same row.
func TestContactDirectoryCRUD(t *testing.T) {
	hs := newHarness(t)
	token := hs.login(adminUsername, adminPassword)

	// Create.
	status, env := hs.do("POST", "/api/v1/contacts",
		contactPayload("Alice", "alice@example.com", ""), token)
	var created struct {
		ID    int64  `json:"id"`
		Name  string `json:"name"`
		Email string `json:"email"`
		Phone string `json:"phone"`
	}
	hs.mustOK(status, env, "create contact", &created)
	if created.ID == 0 || created.Name != "Alice" || created.Email != "alice@example.com" {
		t.Fatalf("create contact: unexpected body %+v", created)
	}

	// Get.
	status, env = hs.do("GET", fmt.Sprintf("/api/v1/contacts/%d", created.ID), nil, token)
	var got struct {
		ID    int64  `json:"id"`
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	hs.mustOK(status, env, "get contact", &got)
	if got.ID != created.ID || got.Email != "alice@example.com" {
		t.Fatalf("get contact: unexpected body %+v", got)
	}

	// Update (full replace; drop the email, keep a phone).
	status, env = hs.do("PUT", fmt.Sprintf("/api/v1/contacts/%d", created.ID),
		contactPayload("Alice Chen", "", "+8613800000001"), token)
	var updated struct {
		ID    int64  `json:"id"`
		Name  string `json:"name"`
		Email string `json:"email"`
		Phone string `json:"phone"`
	}
	hs.mustOK(status, env, "update contact", &updated)
	if updated.Name != "Alice Chen" || updated.Email != "" || updated.Phone != "+8613800000001" {
		t.Fatalf("update contact: unexpected body %+v", updated)
	}

	// List with keyword hit + pagination envelope.
	pd := hs.listPage(token, "/api/v1/contacts?page=1&size=20&keyword=alice")
	if pd.Total != 1 || len(pd.Items) != 1 {
		t.Fatalf("list contacts: expected the seeded row, got total=%d items=%d", pd.Total, len(pd.Items))
	}

	// Delete, then confirm the row is gone.
	status, env = hs.do("DELETE", fmt.Sprintf("/api/v1/contacts/%d", created.ID), nil, token)
	hs.mustOK(status, env, "delete contact", nil)
	status, env = hs.do("GET", fmt.Sprintf("/api/v1/contacts/%d", created.ID), nil, token)
	if status != http.StatusNotFound {
		t.Fatalf("get deleted contact: expected 404, got %d (code=%d)", status, env.Code)
	}
}

// TestContactQuotaCeiling verifies the N4 decision end to end: the third
// contact creation is rejected with 409 Conflict once the CE ceiling of 2
// is reached, and the rejection frees up after a delete.
func TestContactQuotaCeiling(t *testing.T) {
	hs := newHarness(t)
	token := hs.login(adminUsername, adminPassword)

	// Clean slate: remove anything earlier tests in this package may have
	// left behind so the ceiling math is deterministic.
	pd := hs.listPage(token, "/api/v1/contacts?page=1&size=100")
	for _, item := range pd.Items {
		id, _ := item["id"].(float64)
		status, env := hs.do("DELETE", fmt.Sprintf("/api/v1/contacts/%d", int64(id)), nil, token)
		hs.mustOK(status, env, "cleanup contact", nil)
	}

	for i, email := range []string{"one@example.com", "two@example.com"} {
		status, env := hs.do("POST", "/api/v1/contacts",
			contactPayload(fmt.Sprintf("Seat %d", i+1), email, ""), token)
		hs.mustOK(status, env, "create contact under ceiling", nil)
	}

	// The third create hits the ceiling (N4: CE = 2 notification-only seats).
	status, env := hs.do("POST", "/api/v1/contacts",
		contactPayload("Over limit", "over@example.com", ""), token)
	if status != http.StatusConflict {
		t.Fatalf("create over ceiling: expected 409, got %d (code=%d, message=%s)",
			status, env.Code, env.Message)
	}

	// Freeing a seat re-opens creation.
	pd = hs.listPage(token, "/api/v1/contacts?page=1&size=100")
	if pd.Total != 2 {
		t.Fatalf("list contacts: expected 2 rows at ceiling, got %d", pd.Total)
	}
	id, _ := pd.Items[0]["id"].(float64)
	status, env = hs.do("DELETE", fmt.Sprintf("/api/v1/contacts/%d", int64(id)), nil, token)
	hs.mustOK(status, env, "delete contact to free seat", nil)
	status, env = hs.do("POST", "/api/v1/contacts",
		contactPayload("Backfill", "backfill@example.com", ""), token)
	hs.mustOK(status, env, "create contact after freeing a seat", nil)
}

// TestContactValidation exercises the payload contract: name required, at
// least one of email/phone, and emails must be plain valid addresses.
func TestContactValidation(t *testing.T) {
	hs := newHarness(t)
	token := hs.login(adminUsername, adminPassword)

	cases := []struct {
		name string
		body map[string]string
	}{
		{"missing name", contactPayload("", "x@example.com", "")},
		{"no email and no phone", contactPayload("NoChannel", "", "")},
		{"invalid email", contactPayload("Bad", "not-an-email", "")},
		{"display-form email", contactPayload("Display", "Alice <alice@example.com>", "")},
	}
	for _, tc := range cases {
		status, env := hs.do("POST", "/api/v1/contacts", tc.body, token)
		if status != http.StatusBadRequest {
			t.Fatalf("%s: expected 400, got %d (code=%d, message=%s)",
				tc.name, status, env.Code, env.Message)
		}
	}
}
