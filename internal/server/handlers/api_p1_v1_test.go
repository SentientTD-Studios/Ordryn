package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"GoTodo/internal/server/utils"
)

func TestAPIV1MeMethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v2/me", nil)
	req = utils.SetAPIUserID(req, 1)
	rec := httptest.NewRecorder()
	APIV1Me(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestAPIV1ChangePasswordValidation(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v2/me/password", bytes.NewBufferString(`{"current_password":"x","new_password":"short","confirm_password":"short"}`))
	req.Header.Set("Content-Type", "application/json")
	req = utils.SetAPIUserID(req, 1)
	rec := httptest.NewRecorder()
	APIV1ChangePassword(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestAPIV1APIKeysCreateValidation(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v2/api-keys", bytes.NewBufferString(`{"name":""}`))
	req.Header.Set("Content-Type", "application/json")
	req = utils.SetAPIUserID(req, 1)
	rec := httptest.NewRecorder()
	APIV1APIKeysRouter(rec, req)
	if rec.Code != http.StatusBadRequest && rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 400 or 503; body=%s", rec.Code, rec.Body.String())
	}
}

func TestNormalizeAPIKeyName(t *testing.T) {
	name, msg := normalizeAPIKeyName("  Phone  ")
	if msg != "" || name != "Phone" {
		t.Fatalf("trim: name=%q msg=%q", name, msg)
	}
	if _, msg := normalizeAPIKeyName("   "); msg == "" {
		t.Fatal("whitespace-only name should be rejected")
	}
	if _, msg := normalizeAPIKeyName("\u00a0\u00a0"); msg == "" {
		t.Fatal("NBSP-only name should be rejected")
	}
	if _, msg := normalizeAPIKeyName(strings.Repeat("a", 81)); msg == "" {
		t.Fatal("name longer than 80 should be rejected")
	}
	name, msg = normalizeAPIKeyName(strings.Repeat("a", 80))
	if msg != "" || len(name) != 80 {
		t.Fatalf("80-char name should pass: name len=%d msg=%q", len(name), msg)
	}
}

func TestAPIV1APIKeysRenameValidation(t *testing.T) {
	tests := []struct {
		name string
		path string
		body string
	}{
		{name: "empty name", path: "/api/v2/api-keys/1", body: `{"name":""}`},
		{name: "whitespace name", path: "/api/v2/api-keys/1", body: `{"name":"   "}`},
		{name: "too long", path: "/api/v2/api-keys/1", body: `{"name":"` + strings.Repeat("a", 81) + `"}`},
		{name: "invalid id", path: "/api/v2/api-keys/abc", body: `{"name":"Phone"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPatch, tt.path, bytes.NewBufferString(tt.body))
			req.Header.Set("Content-Type", "application/json")
			req = utils.SetAPIUserID(req, 1)
			rec := httptest.NewRecorder()
			APIV1APIKeysRouter(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
			}
		})
	}
}

func TestAPIV1BulkValidation(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "missing action", body: `{"task_ids":[1]}`},
		{name: "missing ids", body: `{"action":"complete"}`},
		{name: "empty ids", body: `{"action":"complete","task_ids":[]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v2/tasks/bulk", bytes.NewBufferString(tt.body))
			req.Header.Set("Content-Type", "application/json")
			req = utils.SetAPIUserID(req, 1)
			rec := httptest.NewRecorder()
			APIV1TasksRouter(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
			}
		})
	}
}

func TestAPIV1TaskEventsInvalidID(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v2/tasks/abc/events", nil)
	req = utils.SetAPIUserID(req, 1)
	rec := httptest.NewRecorder()
	APIV1TasksRouter(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestAPIV1TaskCommentsInvalidID(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v2/tasks/abc/comments", nil)
	req = utils.SetAPIUserID(req, 1)
	rec := httptest.NewRecorder()
	APIV1TasksRouter(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}

	req = httptest.NewRequest(http.MethodPatch, "/api/v2/tasks/1/comments/abc", nil)
	req = utils.SetAPIUserID(req, 1)
	rec = httptest.NewRecorder()
	APIV1TasksRouter(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("patch comment id status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestAPIV1UndoMissingToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v2/tasks/undo", bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req = utils.SetAPIUserID(req, 1)
	rec := httptest.NewRecorder()
	APIV1TasksRouter(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	var payload map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload["error"] != "invalid_request" {
		t.Fatalf("error = %q", payload["error"])
	}
}
