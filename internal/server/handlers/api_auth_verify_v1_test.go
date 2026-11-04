package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"GoTodo/internal/mailer"
	"GoTodo/internal/storage"
)

func postJSON(t *testing.T, h http.HandlerFunc, path, body string) (int, map[string]interface{}) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h(rec, req)
	var payload map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &payload)
	return rec.Code, payload
}

func TestVerifyEmailValidation(t *testing.T) {
	code, payload := postJSON(t, APIV1VerifyEmail, "/api/v2/auth/verify-email", `{"id":"","token":""}`)
	if code != http.StatusBadRequest || payload["error"] != "invalid_request" {
		t.Fatalf("missing fields: %d %v", code, payload)
	}
	// A malformed id is rejected before any database lookup.
	code, payload = postJSON(t, APIV1VerifyEmail, "/api/v2/auth/verify-email", `{"id":"nope","token":"x"}`)
	if code != http.StatusBadRequest || payload["error"] != "invalid_link" {
		t.Fatalf("malformed id: %d %v", code, payload)
	}
}

func TestResendVerificationValidation(t *testing.T) {
	code, payload := postJSON(t, APIV1ResendVerification, "/api/v2/auth/resend-verification", `{"email":""}`)
	if code != http.StatusBadRequest || payload["error"] != "invalid_request" {
		t.Fatalf("missing email: %d %v", code, payload)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v2/auth/resend-verification", nil)
	rec := httptest.NewRecorder()
	APIV1ResendVerification(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET: %d", rec.Code)
	}
}

func TestVerificationRequiredOnlyWithEmail(t *testing.T) {
	if verificationRequired(nil) {
		t.Fatal("no settings: verification can't be required")
	}
	if verificationRequired(&storage.SiteSettings{}) {
		t.Fatal("no email provider: verification can't be required")
	}
	withEmail := &storage.SiteSettings{Email: mailer.Config{
		Provider:        mailer.ProviderSMTP,
		FromAddress:     "noreply@example.com",
		SMTPHost:        "smtp.example.com",
		SMTPPort:        587,
		SMTPUsername:    "user",
		SMTPPasswordEnc: "enc",
	}}
	if !verificationRequired(withEmail) {
		t.Fatal("configured email should require verification")
	}
}

func TestVerificationPendingResponse(t *testing.T) {
	rec := httptest.NewRecorder()
	writeVerificationPending(rec)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", rec.Code)
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["verification_required"] != true || payload["message"] == "" {
		t.Fatalf("payload = %v", payload)
	}
}
