package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"GoTodo/internal/server/utils"
)

func TestAPIV1MeMethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/me", nil)
	req = utils.SetAPIUserID(req, 1)
	rec := httptest.NewRecorder()
	APIV1Me(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestAPIV1ChangePasswordValidation(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/me/password", bytes.NewBufferString(`{"current_password":"x","new_password":"short","confirm_password":"short"}`))
	req.Header.Set("Content-Type", "application/json")
	req = utils.SetAPIUserID(req, 1)
	rec := httptest.NewRecorder()
	APIV1ChangePassword(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestAPIV1APIKeysCreateValidation(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/api-keys", bytes.NewBufferString(`{"name":""}`))
	req.Header.Set("Content-Type", "application/json")
	req = utils.SetAPIUserID(req, 1)
	rec := httptest.NewRecorder()
	APIV1APIKeysRouter(rec, req)
	if rec.Code != http.StatusBadRequest && rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 400 or 503; body=%s", rec.Code, rec.Body.String())
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
			req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/bulk", bytes.NewBufferString(tt.body))
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
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/abc/events", nil)
	req = utils.SetAPIUserID(req, 1)
	rec := httptest.NewRecorder()
	APIV1TasksRouter(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestAPIV1UndoMissingToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/undo", bytes.NewBufferString(`{}`))
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
