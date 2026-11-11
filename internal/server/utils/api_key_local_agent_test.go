package utils

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"GoTodo/internal/storage"
)

func localKey(limit int, scopes ...string) *storage.APIKeyPrincipal {
	return &storage.APIKeyPrincipal{KeyID: 2, UserID: 7, IsLocalAgent: true, LimitProjectID: limit, Scopes: scopes}
}

func localStatus(t *testing.T, p *storage.APIKeyPrincipal, method, target, body string) int {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	r := httptest.NewRequest(method, target, rdr)
	err := authorizeLocalAgentKeyRequest(r, p)
	if err == nil {
		return http.StatusOK
	}
	var se *scopedKeyError
	if !errors.As(err, &se) {
		t.Fatalf("%s %s: unexpected error type %T: %v", method, target, err, err)
	}
	return se.status
}

func TestLocalAgentKeyAllowsTaskWork(t *testing.T) {
	stubTaskProjects(t, map[int]int{100: 10, 200: 20})
	p := localKey(0, allScopes...)
	for _, c := range []struct{ method, target, body string }{
		{http.MethodPost, "/api/v2/mcp", `{}`},
		{http.MethodGet, "/api/v2/me", ""},
		{http.MethodGet, "/api/v2/tasks?claimed=me", ""},
		{http.MethodPost, "/api/v2/tasks", `{"title":"x","project_id":20}`},
		{http.MethodGet, "/api/v2/tasks/100", ""},
		{http.MethodGet, "/api/v2/tasks/200", ""}, // any project the owner can see
		{http.MethodPatch, "/api/v2/tasks/100", `{"status_id":3}`},
		{http.MethodGet, "/api/v2/tasks/100/comments", ""},
		{http.MethodPost, "/api/v2/tasks/100/comments", `{"body":"done"}`},
		{http.MethodPost, "/api/v2/tasks/100/claim", ""},
		{http.MethodDelete, "/api/v2/tasks/100/claim", ""},
		{http.MethodGet, "/api/v2/projects", ""},
		{http.MethodGet, "/api/v2/projects/20", ""},
		{http.MethodGet, "/api/v2/projects/20/statuses", ""},
		{http.MethodGet, "/api/v2/tags?project_id=20", ""},
	} {
		if got := localStatus(t, p, c.method, c.target, c.body); got != http.StatusOK {
			t.Errorf("%s %s %s: status %d, want allowed", c.method, c.target, c.body, got)
		}
	}
}

func TestLocalAgentKeyDeniesAccountAndDestructiveRoutes(t *testing.T) {
	stubTaskProjects(t, map[int]int{100: 10})
	p := localKey(0, allScopes...)
	for _, c := range []struct{ method, target string }{
		// A local key must never mint or manage keys, or change the account.
		{http.MethodGet, "/api/v2/api-keys"},
		{http.MethodPost, "/api/v2/api-keys"},
		{http.MethodDelete, "/api/v2/api-keys/3"},
		{http.MethodPost, "/api/v2/me/password"},
		{http.MethodPatch, "/api/v2/me"},
		{http.MethodPost, "/api/v2/me/mfa/disable"},
		{http.MethodGet, "/api/v2/me/github"},
		{http.MethodPost, "/api/v2/projects/10/api-keys"},
		{http.MethodPost, "/api/v2/auth/device/approve"},
		// Destructive or bulk task changes.
		{http.MethodDelete, "/api/v2/tasks/100"},
		{http.MethodPost, "/api/v2/tasks/bulk"},
		{http.MethodPost, "/api/v2/tasks/100/archive"},
		{http.MethodDelete, "/api/v2/tasks/100/comments/5"},
		{http.MethodPatch, "/api/v2/tasks/100/comments/5"},
		// Project management and everything else.
		{http.MethodPost, "/api/v2/projects"},
		{http.MethodPatch, "/api/v2/projects/10"},
		{http.MethodDelete, "/api/v2/projects/10"},
		{http.MethodGet, "/api/v2/projects/10/members"},
		{http.MethodPost, "/api/v2/projects/10/statuses"},
		{http.MethodPost, "/api/v2/tags"},
		{http.MethodGet, "/api/v2/agent"},
		{http.MethodGet, "/api/v2/agent/runs"},
		{http.MethodGet, "/api/v2/export"},
		{http.MethodPost, "/api/v2/import"},
		{http.MethodGet, "/api/v2/admin/users"},
		{http.MethodGet, "/api/v2/notifications"},
		{http.MethodGet, "/api/v2/organizations"},
	} {
		if got := localStatus(t, p, c.method, c.target, ""); got != http.StatusForbidden {
			t.Errorf("%s %s: status %d, want 403", c.method, c.target, got)
		}
	}
}

func TestLocalAgentKeyEnforcesScopes(t *testing.T) {
	stubTaskProjects(t, map[int]int{100: 10})
	readOnly := localKey(0, storage.APIScopeTasksRead)
	for _, c := range []struct{ method, target, body string }{
		{http.MethodPost, "/api/v2/tasks", `{"title":"x","project_id":10}`},
		{http.MethodPatch, "/api/v2/tasks/100", `{"completed":true}`},
		{http.MethodPost, "/api/v2/tasks/100/comments", `{"body":"x"}`},
		{http.MethodPost, "/api/v2/tasks/100/claim", ""},
	} {
		if got := localStatus(t, readOnly, c.method, c.target, c.body); got != http.StatusForbidden {
			t.Errorf("read-only %s %s: status %d, want 403", c.method, c.target, got)
		}
	}
	// Without tasks:read even MCP is closed.
	noRead := localKey(0, storage.APIScopeCommentsWrite)
	if got := localStatus(t, noRead, http.MethodPost, "/api/v2/mcp", `{}`); got != http.StatusForbidden {
		t.Errorf("mcp without tasks:read: %d", got)
	}
}

func TestLocalAgentKeyProjectLimit(t *testing.T) {
	stubTaskProjects(t, map[int]int{100: 10, 200: 20})
	p := localKey(10, allScopes...)
	for _, c := range []struct {
		method, target, body string
		want                 int
	}{
		{http.MethodGet, "/api/v2/tasks/100", "", http.StatusOK},
		{http.MethodGet, "/api/v2/projects/10/statuses", "", http.StatusOK},
		{http.MethodGet, "/api/v2/tasks/200", "", http.StatusNotFound},
		{http.MethodGet, "/api/v2/tasks/200/comments", "", http.StatusNotFound},
		{http.MethodPost, "/api/v2/tasks/200/comments", `{"body":"x"}`, http.StatusNotFound},
		{http.MethodPost, "/api/v2/tasks/200/claim", "", http.StatusNotFound},
		{http.MethodGet, "/api/v2/projects/20", "", http.StatusNotFound},
		{http.MethodGet, "/api/v2/tags?project_id=20", "", http.StatusNotFound},
		{http.MethodGet, "/api/v2/projects", "", http.StatusForbidden},
		{http.MethodPost, "/api/v2/tasks", `{"title":"x","project_id":20}`, http.StatusForbidden},
		{http.MethodPost, "/api/v2/tasks", `{"title":"x"}`, http.StatusBadRequest},
		{http.MethodPatch, "/api/v2/tasks/100", `{"Project_ID":20}`, http.StatusForbidden},
		{http.MethodPatch, "/api/v2/tasks/100", `{"parent_id":200}`, http.StatusNotFound},
	} {
		if got := localStatus(t, p, c.method, c.target, c.body); got != c.want {
			t.Errorf("%s %s %s: status %d, want %d", c.method, c.target, c.body, got, c.want)
		}
	}

	r := httptest.NewRequest(http.MethodGet, "/api/v2/tasks?project=20&search=bug", nil)
	if err := authorizeLocalAgentKeyRequest(r, p); err != nil {
		t.Fatal(err)
	}
	if got := r.URL.Query().Get("project"); got != "10" {
		t.Fatalf("project filter=%q want 10", got)
	}
	r = httptest.NewRequest(http.MethodGet, "/api/v2/tags", nil)
	if err := authorizeLocalAgentKeyRequest(r, p); err != nil {
		t.Fatal(err)
	}
	if got := r.URL.Query().Get("project_id"); got != "10" {
		t.Fatalf("tags project_id=%q want 10", got)
	}
}
