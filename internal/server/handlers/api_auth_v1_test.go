package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"GoTodo/internal/server/utils"
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
			req := httptest.NewRequest(method, "/api/v2/auth/login", bytes.NewBufferString(tt.body))
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
			body:       `{"email":"a@b.com","password":"x","confirm_password":"y","timezone":"UTC","user_name":"alice"}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_request",
		},
		{
			name:       "invalid timezone",
			body:       `{"email":"a@b.com","password":"x","confirm_password":"x","timezone":"Not/AZone","user_name":"alice"}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_request",
		},
		{
			name:       "missing email",
			body:       `{"password":"x","confirm_password":"x","timezone":"UTC","user_name":"alice"}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_request",
		},
		{
			name:       "invalid username",
			body:       `{"email":"a@b.com","password":"x","confirm_password":"x","timezone":"UTC","user_name":"bad name"}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_request",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v2/auth/register", bytes.NewBufferString(tt.body))
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
	req := httptest.NewRequest(http.MethodGet, "/api/v2/auth/logout", nil)
	rec := httptest.NewRecorder()
	APIV1AuthLogout(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestAPIV1AuthMFAVerifyValidation(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v2/auth/mfa/verify", nil)
	rec := httptest.NewRecorder()
	APIV1AuthMFAVerify(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v2/auth/mfa/verify", bytes.NewBufferString(`{"code":"123456"}`))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	APIV1AuthMFAVerify(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
	var payload map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["error"] != "mfa_pending_required" {
		t.Fatalf("error = %q", payload["error"])
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v2/auth/mfa/verify", bytes.NewBufferString(`{`))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	// Still unauthorized before JSON parse when no pending session.
	APIV1AuthMFAVerify(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad json without pending: status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAPIV1AuthMFAVerifyEmptyCodeWithPending(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v2/auth/mfa/verify", bytes.NewBufferString(`{"code":""}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	if err := utils.EstablishPendingMFASession(rec, req, 7, "a@b.com"); err != nil {
		t.Fatal(err)
	}
	var cookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "session" {
			cookie = c
			break
		}
	}
	if cookie == nil {
		t.Fatal("expected pending session cookie")
	}

	req2 := httptest.NewRequest(http.MethodPost, "/api/v2/auth/mfa/verify", bytes.NewBufferString(`{"code":""}`))
	req2.Header.Set("Content-Type", "application/json")
	req2.AddCookie(cookie)
	if utils.GetSessionUserID(req2) != nil {
		t.Fatal("pending MFA session must not authenticate")
	}
	userID, email, ok := utils.GetPendingMFA(req2)
	if !ok || userID != 7 || email != "a@b.com" {
		t.Fatalf("pending=%d %q ok=%v", userID, email, ok)
	}
	rec2 := httptest.NewRecorder()
	APIV1AuthMFAVerify(rec2, req2)
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec2.Code, rec2.Body.String())
	}
}

func TestAPIV1MeMFAMethods(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v2/me/mfa", nil)
	rec := httptest.NewRecorder()
	APIV1MeMFA(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v2/me/mfa/setup", nil)
	rec = httptest.NewRecorder()
	APIV1MeMFASetup(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("setup status = %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v2/me/mfa/enable", nil)
	rec = httptest.NewRecorder()
	APIV1MeMFAEnable(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("enable status = %d", rec.Code)
	}
}

func TestAPIV1MeUnauthenticatedReturnsNull(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v2/me", nil)
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
	for _, key := range []string{"id", "email", "user_name", "timezone", "items_per_page", "permissions", "allow_project_invites", "username_change_available", "mfa_enabled"} {
		if _, ok := m[key]; !ok {
			t.Fatalf("missing key %q in %s", key, string(raw))
		}
	}
}
