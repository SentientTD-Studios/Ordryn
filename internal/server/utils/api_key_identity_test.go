package utils

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"GoTodo/internal/storage"
)

func identityStatus(t *testing.T, p *storage.APIKeyPrincipal, method, target string) int {
	t.Helper()
	err := authorizeIdentityKeyRequest(httptest.NewRequest(method, target, nil), p)
	if err == nil {
		return http.StatusOK
	}
	var se *scopedKeyError
	if !errors.As(err, &se) {
		t.Fatalf("%s %s: unexpected error type %T: %v", method, target, err, err)
	}
	return se.status
}

func identityKey() *storage.APIKeyPrincipal {
	return &storage.APIKeyPrincipal{KeyID: 42, UserID: 7, IsIdentity: true, Scopes: []string{storage.APIScopeProfileRead}}
}

func TestIdentityKeyAllowsProfileAndSelfRevoke(t *testing.T) {
	p := identityKey()
	for _, c := range []struct{ method, target string }{
		{http.MethodGet, "/api/v2/me"},
		{http.MethodGet, "/api/v1/me"},
		{http.MethodGet, "/api/v2/organizations"},
		{http.MethodDelete, "/api/v2/api-keys/42"},
	} {
		if got := identityStatus(t, p, c.method, c.target); got != http.StatusOK {
			t.Errorf("%s %s: status %d, want allowed", c.method, c.target, got)
		}
	}
}

func TestIdentityKeyDeniesEverythingElse(t *testing.T) {
	p := identityKey()
	for _, c := range []struct{ method, target string }{
		{http.MethodPatch, "/api/v2/me"},
		{http.MethodPost, "/api/v2/me/username"},
		{http.MethodGet, "/api/v2/me/passkeys"},
		{http.MethodPost, "/api/v2/organizations"},
		{http.MethodGet, "/api/v2/organizations/3"},
		{http.MethodGet, "/api/v2/organizations/3/members"},
		{http.MethodGet, "/api/v2/api-keys"},
		{http.MethodPost, "/api/v2/api-keys"},
		{http.MethodDelete, "/api/v2/api-keys/43"}, // someone else's key
		{http.MethodDelete, "/api/v2/api-keys/abc"},
		{http.MethodGet, "/api/v2/tasks"},
		{http.MethodGet, "/api/v2/projects"},
		{http.MethodPost, "/api/v2/mcp"},
		{http.MethodGet, "/api/v2/admin/users"},
		{http.MethodPost, "/api/v2/auth/device/approve"},
	} {
		if got := identityStatus(t, p, c.method, c.target); got != http.StatusForbidden {
			t.Errorf("%s %s: status %d, want 403", c.method, c.target, got)
		}
	}
}

func TestIdentityKeyRequiresProfileScope(t *testing.T) {
	p := identityKey()
	p.Scopes = nil
	if got := identityStatus(t, p, http.MethodGet, "/api/v2/me"); got != http.StatusForbidden {
		t.Fatalf("status %d, want 403 without profile:read", got)
	}
}
