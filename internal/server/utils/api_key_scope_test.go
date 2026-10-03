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

// stubTaskProjects maps task id -> project id for the duration of a test.
func stubTaskProjects(t *testing.T, m map[int]int) {
	t.Helper()
	prev := scopedKeyTaskProjectID
	scopedKeyTaskProjectID = func(taskID int) (int, error) {
		if pid, ok := m[taskID]; ok {
			return pid, nil
		}
		return 0, errors.New("task not found")
	}
	t.Cleanup(func() { scopedKeyTaskProjectID = prev })
}

func projectKey(scopes ...string) *storage.APIKeyPrincipal {
	return &storage.APIKeyPrincipal{KeyID: 1, UserID: 7, ProjectID: 10, Scopes: scopes}
}

var allScopes = []string{storage.APIScopeTasksRead, storage.APIScopeTasksWrite, storage.APIScopeCommentsWrite}

func scopedStatus(t *testing.T, p *storage.APIKeyPrincipal, method, target, body string) int {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	r := httptest.NewRequest(method, target, rdr)
	err := authorizeProjectKeyRequest(r, p)
	if err == nil {
		return http.StatusOK
	}
	var se *scopedKeyError
	if !errors.As(err, &se) {
		t.Fatalf("%s %s: unexpected error type %T: %v", method, target, err, err)
	}
	return se.status
}

func TestProjectKeyAllowsTaskRoutesInItsProject(t *testing.T) {
	stubTaskProjects(t, map[int]int{100: 10, 101: 10})
	p := projectKey(allScopes...)
	cases := []struct{ method, target, body string }{
		{http.MethodGet, "/api/v2/tasks", ""},
		{http.MethodPost, "/api/v2/tasks", `{"title":"x","project_id":10}`},
		{http.MethodPost, "/api/v2/tasks", `{"title":"x","project_id":10,"parent_id":101}`},
		{http.MethodGet, "/api/v2/tasks/100", ""},
		{http.MethodPatch, "/api/v2/tasks/100", `{"completed":true}`},
		{http.MethodPatch, "/api/v1/tasks/100", `{"project_id":10}`},
		{http.MethodGet, "/api/v2/tasks/100/comments", ""},
		{http.MethodPost, "/api/v2/tasks/100/comments", `{"body":"hi"}`},
		{http.MethodGet, "/api/v2/projects/10", ""},
		{http.MethodGet, "/api/v2/projects/10/statuses", ""},
		{http.MethodGet, "/api/v2/projects/10/sprints", ""},
		{http.MethodGet, "/api/v2/projects/10/custom-fields", ""},
	}
	for _, c := range cases {
		if got := scopedStatus(t, p, c.method, c.target, c.body); got != http.StatusOK {
			t.Errorf("%s %s %s: status %d, want allowed", c.method, c.target, c.body, got)
		}
	}
}

func TestProjectKeyDeniesEverythingElse(t *testing.T) {
	stubTaskProjects(t, map[int]int{100: 10})
	p := projectKey(allScopes...)
	cases := []struct{ method, target string }{
		{http.MethodGet, "/api/v2/me"},
		{http.MethodPost, "/api/v2/me/password"},
		{http.MethodGet, "/api/v2/api-keys"},
		{http.MethodPost, "/api/v2/api-keys"},
		{http.MethodGet, "/api/v2/projects"},
		{http.MethodPatch, "/api/v2/projects/10"},
		{http.MethodDelete, "/api/v2/projects/10"},
		{http.MethodGet, "/api/v2/projects/10/members"},
		{http.MethodGet, "/api/v2/projects/10/api-keys"},
		{http.MethodPost, "/api/v2/projects/10/api-keys"},
		{http.MethodPost, "/api/v2/projects/10/statuses"},
		{http.MethodGet, "/api/v2/projects/11"},
		{http.MethodGet, "/api/v2/projects/11/statuses"},
		{http.MethodDelete, "/api/v2/tasks/100"},
		{http.MethodPost, "/api/v2/tasks/bulk"},
		{http.MethodPost, "/api/v2/tasks/reorder"},
		{http.MethodPost, "/api/v2/tasks/undo"},
		{http.MethodPost, "/api/v2/tasks/100/archive"},
		{http.MethodPost, "/api/v2/tasks/100/claim"},
		{http.MethodGet, "/api/v2/tasks/100/events"},
		{http.MethodPatch, "/api/v2/tasks/100/comments/5"},
		{http.MethodDelete, "/api/v2/tasks/100/comments/5"},
		{http.MethodGet, "/api/v2/export"},
		{http.MethodPost, "/api/v2/import"},
		{http.MethodGet, "/api/v2/tags"},
		{http.MethodGet, "/api/v2/admin/users"},
		{http.MethodGet, "/api/v2/notifications"},
	}
	for _, c := range cases {
		if got := scopedStatus(t, p, c.method, c.target, ""); got != http.StatusForbidden {
			t.Errorf("%s %s: status %d, want 403", c.method, c.target, got)
		}
	}
}

func TestProjectKeyHidesTasksInOtherProjects(t *testing.T) {
	stubTaskProjects(t, map[int]int{100: 10, 200: 20})
	p := projectKey(allScopes...)
	for _, c := range []struct{ method, target, body string }{
		{http.MethodGet, "/api/v2/tasks/200", ""},
		{http.MethodPatch, "/api/v2/tasks/200", `{"title":"x"}`},
		{http.MethodGet, "/api/v2/tasks/200/comments", ""},
		{http.MethodPost, "/api/v2/tasks/200/comments", `{"body":"x"}`},
		{http.MethodGet, "/api/v2/tasks/999", ""},
		{http.MethodPost, "/api/v2/tasks", `{"title":"x","project_id":10,"parent_id":200}`},
		{http.MethodPatch, "/api/v2/tasks/100", `{"parent_id":200}`},
		{http.MethodPatch, "/api/v2/tasks/100", `{"Parent_ID":200}`},
	} {
		if got := scopedStatus(t, p, c.method, c.target, c.body); got != http.StatusNotFound {
			t.Errorf("%s %s %s: status %d, want 404", c.method, c.target, c.body, got)
		}
	}
}

func TestProjectKeyCannotMoveOrCreateOutsideProject(t *testing.T) {
	stubTaskProjects(t, map[int]int{100: 10})
	p := projectKey(allScopes...)
	for _, c := range []struct {
		method, target, body string
		want                 int
	}{
		{http.MethodPost, "/api/v2/tasks", `{"title":"x"}`, http.StatusBadRequest},
		{http.MethodPost, "/api/v2/tasks", `{"title":"x","project_id":20}`, http.StatusForbidden},
		{http.MethodPost, "/api/v2/tasks", `{"title":"x","project_id":null}`, http.StatusForbidden},
		{http.MethodPatch, "/api/v2/tasks/100", `{"project_id":20}`, http.StatusForbidden},
		{http.MethodPatch, "/api/v2/tasks/100", `{"project_id":null}`, http.StatusForbidden},
		{http.MethodPatch, "/api/v2/tasks/100", `{"PROJECT_ID":20}`, http.StatusForbidden},
		{http.MethodPatch, "/api/v2/tasks/100", `{"project_id":10,"Project_Id":20}`, http.StatusForbidden},
		{http.MethodPost, "/api/v2/tasks", `{"title":"x","Project_ID":20}`, http.StatusForbidden},
		{http.MethodPatch, "/api/v2/tasks/100", `{"project_id":20} trailing`, http.StatusBadRequest},
		{http.MethodPatch, "/api/v2/tasks/100", `null`, http.StatusBadRequest},
		{http.MethodPost, "/api/v2/tasks", `[{"project_id":20}]`, http.StatusBadRequest},
	} {
		if got := scopedStatus(t, p, c.method, c.target, c.body); got != c.want {
			t.Errorf("%s %s %s: status %d, want %d", c.method, c.target, c.body, got, c.want)
		}
	}
}

func TestProjectKeyEnforcesScopes(t *testing.T) {
	stubTaskProjects(t, map[int]int{100: 10})
	readOnly := projectKey(storage.APIScopeTasksRead)
	for _, c := range []struct{ method, target, body string }{
		{http.MethodPost, "/api/v2/tasks", `{"title":"x","project_id":10}`},
		{http.MethodPatch, "/api/v2/tasks/100", `{"completed":true}`},
		{http.MethodPost, "/api/v2/tasks/100/comments", `{"body":"x"}`},
	} {
		if got := scopedStatus(t, readOnly, c.method, c.target, c.body); got != http.StatusForbidden {
			t.Errorf("read-only %s %s: status %d, want 403", c.method, c.target, got)
		}
	}

	commentOnly := projectKey(storage.APIScopeCommentsWrite)
	if got := scopedStatus(t, commentOnly, http.MethodPost, "/api/v2/tasks/100/comments", `{"body":"x"}`); got != http.StatusOK {
		t.Errorf("comments:write should post comments, got %d", got)
	}
	for _, target := range []string{"/api/v2/tasks", "/api/v2/tasks/100", "/api/v2/tasks/100/comments", "/api/v2/projects/10"} {
		if got := scopedStatus(t, commentOnly, http.MethodGet, target, ""); got != http.StatusForbidden {
			t.Errorf("comments-only GET %s: status %d, want 403", target, got)
		}
	}
}

func TestProjectKeyPinsTaskListToProject(t *testing.T) {
	p := projectKey(storage.APIScopeTasksRead)
	r := httptest.NewRequest(http.MethodGet, "/api/v2/tasks?project=99&q=bug", nil)
	if err := authorizeProjectKeyRequest(r, p); err != nil {
		t.Fatal(err)
	}
	if got := r.URL.Query().Get("project"); got != "10" {
		t.Fatalf("project filter=%q want 10", got)
	}
	if got := r.URL.Query().Get("q"); got != "bug" {
		t.Fatalf("other params should survive, q=%q", got)
	}
}

func TestProjectKeyBodyIsRestoredForHandler(t *testing.T) {
	p := projectKey(allScopes...)
	const body = `{"title":"keep me","project_id":10}`
	r := httptest.NewRequest(http.MethodPost, "/api/v2/tasks", strings.NewReader(body))
	if err := authorizeProjectKeyRequest(r, p); err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(r.Body)
	if string(got) != body {
		t.Fatalf("body=%q want %q", got, body)
	}
}

func TestProjectKeyRespectsBasePath(t *testing.T) {
	prev := BasePath
	BasePath = "https://example.com/todo"
	t.Cleanup(func() { BasePath = prev })
	stubTaskProjects(t, map[int]int{100: 10})
	p := projectKey(allScopes...)
	if got := scopedStatus(t, p, http.MethodGet, "/todo/api/v2/tasks/100", ""); got != http.StatusOK {
		t.Fatalf("base-path task GET: status %d", got)
	}
	if got := scopedStatus(t, p, http.MethodGet, "/todo/api/v2/me", ""); got != http.StatusForbidden {
		t.Fatalf("base-path /me: status %d", got)
	}
}
