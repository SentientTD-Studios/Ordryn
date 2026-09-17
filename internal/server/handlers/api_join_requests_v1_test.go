package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAPIV1JoinRequestsCreateValidation(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		body       string
		wantStatus int
		wantCode   string
	}{
		{
			name:       "method not allowed",
			method:     http.MethodGet,
			body:       `{}`,
			wantStatus: http.StatusMethodNotAllowed,
			wantCode:   "method_not_allowed",
		},
		{
			name:       "invalid json",
			method:     http.MethodPost,
			body:       `{`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_request",
		},
		{
			name:       "missing email",
			method:     http.MethodPost,
			body:       `{"message":"hi"}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_request",
		},
		{
			name:       "invalid email",
			method:     http.MethodPost,
			body:       `{"email":"not-an-email"}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_request",
		},
		{
			name:       "message too long",
			method:     http.MethodPost,
			body:       `{"email":"a@b.com","message":"` + strings.Repeat("x", 501) + `"}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_request",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/api/v2/join-requests", bytes.NewBufferString(tt.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			APIV1JoinRequestsCreate(rec, req)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tt.wantStatus, rec.Body.String())
			}
			var payload map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
				t.Fatalf("decode: %v body=%s", err, rec.Body.String())
			}
			if payload["error"] != tt.wantCode {
				t.Fatalf("error = %q, want %q", payload["error"], tt.wantCode)
			}
		})
	}
}

func TestAPIV1AdminJoinRequestsValidation(t *testing.T) {
	t.Run("collection method not allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v2/admin/join-requests", nil)
		rec := httptest.NewRecorder()
		APIV1AdminJoinRequestsRouter(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusMethodNotAllowed, rec.Body.String())
		}
	})
	t.Run("invalid id", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v2/admin/join-requests/abc/approve", nil)
		rec := httptest.NewRecorder()
		APIV1AdminJoinRequestsRouter(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
		}
	})
	t.Run("unknown action", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v2/admin/join-requests/1/nope", nil)
		rec := httptest.NewRecorder()
		APIV1AdminJoinRequestsRouter(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusNotFound, rec.Body.String())
		}
	})
}
