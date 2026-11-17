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

func TestNormalizeChatServerURL(t *testing.T) {
	ok := map[string]string{
		"https://chat.ordryn.com":       "https://chat.ordryn.com",
		" https://Chat.Ordryn.com/ ":    "https://chat.ordryn.com",
		"https://chat.example.com:8443": "https://chat.example.com:8443",
		"http://localhost:8099":         "http://localhost:8099",
		"http://127.0.0.1:8080":         "http://127.0.0.1:8080",
	}
	for in, want := range ok {
		got, err := storage.NormalizeChatServerURL(in)
		if err != nil || got != want {
			t.Errorf("NormalizeChatServerURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{
		"", "chat.ordryn.com", "http://chat.ordryn.com", "ftp://chat.ordryn.com",
		"https://chat.ordryn.com/path", "https://chat.ordryn.com?x=1", "https://user:pw@chat.ordryn.com",
		"https://chat.ordryn.com#frag",
	} {
		if got, err := storage.NormalizeChatServerURL(in); err == nil {
			t.Errorf("NormalizeChatServerURL(%q) = %q, want an error", in, got)
		}
	}
}

func postDeviceCode(t *testing.T, body map[string]interface{}) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v2/auth/device/code", bytes.NewReader(raw))
	rec := httptest.NewRecorder()
	APIDeviceCode(rec, req)
	return rec
}

func TestDeviceCodeSignInOnlyNeedsChatCredentials(t *testing.T) {
	// No client credentials: refused before anything is stored.
	rec := postDeviceCode(t, map[string]interface{}{"client_name": "Ordryn Chat", "scopes": []string{"profile:read"}})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("identity request without credentials: status %d, body %s", rec.Code, rec.Body)
	}
	var e map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &e)
	if e["error"] != "invalid_client" {
		t.Fatalf("error = %v", e)
	}
}

func TestDeviceCodeChatCredentialsNeverGetFullAccess(t *testing.T) {
	rec := postDeviceCode(t, map[string]interface{}{"client_name": "Ordryn Chat", "client_id": "chat_abc", "client_secret": "ordc_x"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("full-access request with chat credentials: status %d, body %s", rec.Code, rec.Body)
	}
}

func TestChatServerStillAllowedNeedsChatServerForIdentity(t *testing.T) {
	identity := &utils.DeviceAuthRecord{Scopes: []string{storage.APIScopeProfileRead}}
	if chatServerStillAllowed(identity) {
		t.Fatal("identity request without a chat server must not be approved")
	}
	if !chatServerStillAllowed(&utils.DeviceAuthRecord{}) {
		t.Fatal("full-access requests (Android app) are unaffected")
	}
}

func TestAdminChatRouterRejectsUnknownPaths(t *testing.T) {
	for _, tc := range []struct {
		method, path string
		want         int
	}{
		{http.MethodGet, "/api/v2/admin/chat/nope", http.StatusNotFound},
		{http.MethodGet, "/api/v2/admin/chat/servers", http.StatusMethodNotAllowed},
		{http.MethodPatch, "/api/v2/admin/chat/servers/abc", http.StatusBadRequest},
		{http.MethodGet, "/api/v2/admin/chat/servers/1/rotate-secret", http.StatusNotFound},
	} {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		req = utils.SetAPIUserID(req, 1)
		rec := httptest.NewRecorder()
		APIV1AdminChatRouter(rec, req)
		if rec.Code != tc.want {
			t.Errorf("%s %s: status %d, want %d", tc.method, tc.path, rec.Code, tc.want)
		}
	}
}

func TestChatDirectoryNeedsChatCredentials(t *testing.T) {
	rec := httptest.NewRecorder()
	APIV2ChatDirectory(rec, httptest.NewRequest(http.MethodPost, "/api/v2/chat/directory", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST: status %d", rec.Code)
	}
	// No credentials, then empty ones: refused before any lookup.
	for _, set := range []func(*http.Request){
		func(*http.Request) {},
		func(r *http.Request) { r.SetBasicAuth("", "") },
		func(r *http.Request) { r.Header.Set("Authorization", "Bearer gotodo_personal_key") },
	} {
		req := httptest.NewRequest(http.MethodGet, "/api/v2/chat/directory", nil)
		set(req)
		rec := httptest.NewRecorder()
		APIV2ChatDirectory(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status %d, body %s", rec.Code, rec.Body)
		}
	}
}
