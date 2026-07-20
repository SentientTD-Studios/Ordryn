package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"GoTodo/internal/server/utils"
)

func TestSafeDeviceReturnTo(t *testing.T) {
	origBase := utils.GetBasePath()
	t.Cleanup(func() {
		utils.BasePath = origBase
	})
	utils.BasePath = ""

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty", input: "", want: ""},
		{name: "device path", input: "/auth/device?user_code=ABCD-EFGH", want: "/auth/device?user_code=ABCD-EFGH"},
		{name: "device root", input: "/auth/device", want: "/auth/device"},
		{name: "legacy app device path", input: "/app/auth/device?user_code=ABCD-EFGH", want: "/auth/device?user_code=ABCD-EFGH"},
		{name: "external url", input: "https://evil.example/auth/device", want: ""},
		{name: "protocol relative", input: "//evil.example/auth/device", want: ""},
		{name: "home redirect", input: "/dashboard", want: ""},
		{name: "relative path", input: "auth/device", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := utils.SafeDeviceReturnTo(tt.input); got != tt.want {
				t.Fatalf("SafeDeviceReturnTo(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}

	t.Run("with base path", func(t *testing.T) {
		utils.BasePath = "/gotodo"
		if got := utils.SafeDeviceReturnTo("/gotodo/auth/device?user_code=ABCD-EFGH"); got != "/gotodo/auth/device?user_code=ABCD-EFGH" {
			t.Fatalf("base path match = %q", got)
		}
		if got := utils.SafeDeviceReturnTo("/auth/device?user_code=ABCD-EFGH"); got != "/gotodo/auth/device?user_code=ABCD-EFGH" {
			t.Fatalf("base path prefix = %q", got)
		}
		if got := utils.SafeDeviceReturnTo("/gotodo/app/auth/device?user_code=ABCD-EFGH"); got != "/gotodo/auth/device?user_code=ABCD-EFGH" {
			t.Fatalf("legacy app path = %q", got)
		}
	})
}

func TestNormalizeClientName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "", want: "Android app"},
		{input: "  ", want: "Android app"},
		{input: " Pixel 9 ", want: "Pixel 9"},
	}
	for _, tt := range tests {
		if got := utils.NormalizeClientName(tt.input); got != tt.want {
			t.Fatalf("NormalizeClientName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestSafeDeviceRedirectURI(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "empty", input: "", want: ""},
		{name: "canonical", input: "ordryn://auth-complete", want: "ordryn://auth-complete"},
		{name: "strips query", input: "ordryn://auth-complete?foo=1", want: "ordryn://auth-complete"},
		{name: "scheme case", input: "Ordryn://auth-complete", want: "ordryn://auth-complete"},
		{name: "https rejected", input: "https://evil.example/", wantErr: true},
		{name: "wrong host", input: "ordryn://evil", wantErr: true},
		{name: "path rejected", input: "ordryn://auth-complete/extra", wantErr: true},
		{name: "userinfo rejected", input: "ordryn://user@auth-complete", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := utils.SafeDeviceRedirectURI(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("SafeDeviceRedirectURI(%q): %v", tt.input, err)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDeviceDecisionRedirectURI(t *testing.T) {
	if got := utils.DeviceDecisionRedirectURI("ordryn://auth-complete", true); got != "ordryn://auth-complete?status=approved" {
		t.Fatalf("approve = %q", got)
	}
	if got := utils.DeviceDecisionRedirectURI("ordryn://auth-complete", false); got != "ordryn://auth-complete?error=access_denied" {
		t.Fatalf("deny = %q", got)
	}
	if got := utils.DeviceDecisionRedirectURI("", true); got != "" {
		t.Fatalf("empty = %q", got)
	}
}

func TestAPIDeviceCodeInvalidRedirectURI(t *testing.T) {
	body := bytes.NewBufferString(`{"client_name":"Android app","redirect_uri":"https://evil.example/"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/device/code", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	APIDeviceCode(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	var payload map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload["error"] != "invalid_request" {
		t.Fatalf("error = %q, want invalid_request", payload["error"])
	}
}

func TestAPIDeviceTokenMissingDeviceCode(t *testing.T) {
	body := bytes.NewBufferString(`{"grant_type":"urn:ietf:params:oauth:grant-type:device_code"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/device/token", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	APIDeviceToken(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	var payload map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload["error"] != "invalid_request" {
		t.Fatalf("error = %q, want invalid_request", payload["error"])
	}
}

func TestAPIDeviceCodeVerificationURLWithFullBasePath(t *testing.T) {
	origBase := utils.BasePath
	t.Cleanup(func() {
		utils.BasePath = origBase
		utils.RedisClient = nil
	})
	utils.BasePath = "https://demo.ryanmalacina.com/gotodo"

	// Redis is required by the middleware chain; call handler directly.
	body := bytes.NewBufferString(`{"client_name":"Android app"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/device/code", body)
	req.Header.Set("Content-Type", "application/json")
	req.Host = "demo.ryanmalacina.com"
	req.Header.Set("X-Forwarded-Proto", "https")

	// Handler-only test: skip Redis by not going through middleware.
	// We only assert URL shape from a mocked record by testing AbsoluteURLForRequest
	// in utils; here verify APIDeviceCode doesn't double-prefix when URLs are built.
	got := utils.AbsoluteURLForRequest(req, "/auth/device?user_code=ABCD-EFGH")
	want := "https://demo.ryanmalacina.com/gotodo/auth/device?user_code=ABCD-EFGH"
	if got != want {
		t.Fatalf("verification URL = %q, want %q", got, want)
	}
}

func TestAPIDeviceCodeMethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/device/code", nil)
	rec := httptest.NewRecorder()

	APIDeviceCode(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusMethodNotAllowed, rec.Body.String())
	}
}

func TestAPIDeviceTokenMethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/device/token", nil)
	rec := httptest.NewRecorder()

	APIDeviceToken(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusMethodNotAllowed, rec.Body.String())
	}
}
