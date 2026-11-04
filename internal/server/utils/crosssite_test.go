package utils

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRejectCrossSite(t *testing.T) {
	t.Setenv("PUBLIC_URL", "https://todo.example.com")
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }
	h := RejectCrossSite(ok)

	tests := []struct {
		name    string
		method  string
		headers map[string]string
		host    string
		allowed bool
	}{
		{"same-origin fetch", "POST", map[string]string{"Sec-Fetch-Site": "same-origin"}, "", true},
		{"cross-site form post", "POST", map[string]string{"Sec-Fetch-Site": "cross-site"}, "", false},
		{"sibling subdomain", "POST", map[string]string{"Sec-Fetch-Site": "same-site"}, "", false},
		{"user-initiated", "POST", map[string]string{"Sec-Fetch-Site": "none"}, "", true},
		{"cross-site GET is fine", "GET", map[string]string{"Sec-Fetch-Site": "cross-site"}, "", true},
		{"cross-site DELETE", "DELETE", map[string]string{"Sec-Fetch-Site": "cross-site"}, "", false},
		{"no browser headers (curl, CLI, webhook)", "POST", nil, "", true},
		{"bearer client from anywhere", "POST", map[string]string{"Sec-Fetch-Site": "cross-site", "Authorization": "Bearer abc123"}, "", true},
		{"old browser, matching Origin", "POST", map[string]string{"Origin": "http://app.local:8080"}, "app.local:8080", true},
		{"old browser, PUBLIC_URL Origin", "POST", map[string]string{"Origin": "https://todo.example.com"}, "internal:8080", true},
		{"old browser, foreign Origin", "POST", map[string]string{"Origin": "https://evil.example"}, "app.local:8080", false},
		{"null Origin from sandboxed frame", "POST", map[string]string{"Origin": "null"}, "app.local:8080", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/api/v2/auth/login", nil)
			if tt.host != "" {
				req.Host = tt.host
			}
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}
			rec := httptest.NewRecorder()
			h(rec, req)
			allowed := rec.Code == http.StatusNoContent
			if allowed != tt.allowed {
				t.Fatalf("allowed = %v (status %d), want %v", allowed, rec.Code, tt.allowed)
			}
		})
	}
}
