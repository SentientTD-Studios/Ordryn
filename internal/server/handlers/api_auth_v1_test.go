package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"GoTodo/internal/storage"
)

func TestAPIV1AuthLoginValidation(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantCode   string
	}{
		{
			name:       "invalid json",
			body:       `{`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_request",
		},
		{
			name:       "missing fields",
			body:       `{"email":"a@b.com"}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_request",
		},
		{
			name:       "method not allowed",
			body:       `{}`,
			wantStatus: http.StatusMethodNotAllowed,
			wantCode:   "method_not_allowed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			method := http.MethodPost
			if tt.name == "method not allowed" {
				method = http.MethodGet
			}
			req := httptest.NewRequest(method, "/api/v1/auth/login", bytes.NewBufferString(tt.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			APIV1AuthLogin(rec, req)
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

func TestAPIV1AuthRegisterValidation(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantCode   string
	}{
		{
			name:       "password mismatch",
			body:       `{"email":"a@b.com","password":"x","confirm_password":"y","timezone":"UTC"}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_request",
		},
		{
			name:       "invalid timezone",
			body:       `{"email":"a@b.com","password":"x","confirm_password":"x","timezone":"Not/AZone"}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_request",
		},
		{
			name:       "missing email",
			body:       `{"password":"x","confirm_password":"x","timezone":"UTC"}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_request",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewBufferString(tt.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			APIV1AuthRegister(rec, req)
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

func TestAPIV1AuthLogoutMethod(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/logout", nil)
	rec := httptest.NewRecorder()
	APIV1AuthLogout(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestAPIV1MeUnauthenticatedReturnsNull(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	rec := httptest.NewRecorder()
	APIV1Me(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if rec.Body.String() != "null" {
		t.Fatalf("body = %q, want null", rec.Body.String())
	}
}

func TestProfileToMeJSON(t *testing.T) {
	out := profileToMeJSON(&storage.UserProfile{
		ID:           7,
		Email:        "a@b.com",
		UserName:     "Ada",
		Timezone:     "UTC",
		ItemsPerPage: 25,
		Permissions:  []string{"add", "edit"},
	})
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"id", "email", "user_name", "timezone", "items_per_page", "permissions", "digest_enabled", "digest_hour"} {
		if _, ok := m[key]; !ok {
			t.Fatalf("missing key %q in %s", key, string(raw))
		}
	}
}
