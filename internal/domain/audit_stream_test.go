package domain

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"GoTodo/internal/storage"
)

func TestAuditStreamDeliversNewEventsSigned(t *testing.T) {
	ctx := context.Background()
	setupCompanySite(t, "gotodo_audit_stream_test")

	var mu sync.Mutex
	var got []storage.AuditStreamEvent
	failing := false
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mac := hmac.New(sha256.New, []byte("s3cret"))
		mac.Write([]byte(r.Header.Get("X-Ordryn-Timestamp") + "."))
		mac.Write(body)
		if r.Header.Get("X-Ordryn-Signature") != "sha256="+hex.EncodeToString(mac.Sum(nil)) {
			http.Error(w, "bad signature", http.StatusUnauthorized)
			return
		}
		if r.Header.Get("Authorization") != "Splunk tok" {
			http.Error(w, "no auth", http.StatusUnauthorized)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if failing {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		var payload struct {
			Events []storage.AuditStreamEvent `json:"events"`
		}
		_ = json.Unmarshal(body, &payload)
		got = append(got, payload.Events...)
	}))
	defer srv.Close()
	prev := auditStreamClient
	auditStreamClient = srv.Client()
	t.Cleanup(func() { auditStreamClient = prev })

	// Before streaming is on: never replayed.
	_ = storage.LogAdminEvent(1, "old_event", "site", 1, "old", nil)

	bad := "http://insecure.example.com"
	if _, err := UpdateAuditStream(AuditStreamInput{URL: &bad}); !errors.Is(err, ErrValidation) {
		t.Fatalf("http endpoint: %v", err)
	}
	on, url, sec, auth := true, srv.URL, "s3cret", "Splunk tok"
	if _, err := UpdateAuditStream(AuditStreamInput{Enabled: &on, URL: &url, Secret: &sec, AuthHeader: &auth}); err != nil {
		t.Fatal(err)
	}
	_ = storage.LogAdminEvent(1, "user_deactivated", "user", 5, "bob", map[string]interface{}{"from": "active"})
	_ = storage.LogAdminEvent(1, "user_reactivated", "user", 5, "bob", nil)

	n, err := auditStreamTick(ctx)
	if err != nil || n != 2 {
		t.Fatalf("tick: %d %v", n, err)
	}
	if got[0].EventType != "user_deactivated" || got[1].EventType != "user_reactivated" {
		t.Fatalf("events: %+v", got)
	}
	if n, _ := auditStreamTick(ctx); n != 0 {
		t.Fatalf("nothing new should be sent, got %d", n)
	}

	mu.Lock()
	failing = true
	mu.Unlock()
	_ = storage.LogAdminEvent(1, "role_changed", "role", 2, "x", nil)
	if _, err := auditStreamTick(ctx); err == nil {
		t.Fatal("a failed delivery should report an error")
	}
	mu.Lock()
	failing = false
	mu.Unlock()
	if n, err := auditStreamTick(ctx); err != nil || n != 1 {
		t.Fatalf("retry after failure: %d %v", n, err)
	}
}
