package utils

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRateLimitSpecForSeparatesSPAAndAPIBuckets(t *testing.T) {
	spaRead := RateLimitSpecFor(7, AuthKindSession, http.MethodGet)
	spaWrite := RateLimitSpecFor(7, AuthKindSession, http.MethodPost)
	apiRead := RateLimitSpecFor(7, AuthKindAPIKey, http.MethodGet)
	apiWrite := RateLimitSpecFor(7, AuthKindAPIKey, http.MethodPatch)

	keys := map[string]struct{}{
		spaRead.Key:  {},
		spaWrite.Key: {},
		apiRead.Key:  {},
		apiWrite.Key: {},
	}
	if len(keys) != 4 {
		t.Fatalf("expected 4 distinct Redis keys, got %#v", keys)
	}
	if spaRead.Capacity <= apiRead.Capacity {
		t.Fatalf("SPA read capacity %d should exceed API %d", spaRead.Capacity, apiRead.Capacity)
	}
	if spaWrite.Capacity <= apiWrite.Capacity {
		t.Fatalf("SPA write capacity %d should exceed API %d", spaWrite.Capacity, apiWrite.Capacity)
	}
	if spaRead.Refill <= apiRead.Refill {
		t.Fatalf("SPA read refill %v should exceed API %v", spaRead.Refill, apiRead.Refill)
	}
	if spaWrite.Key == spaRead.Key {
		t.Fatal("SPA read and write must not share a bucket")
	}
	if apiWrite.Key == apiRead.Key {
		t.Fatal("API read and write must not share a bucket")
	}
	if spaRead.Capacity != SPAReadCapacity || apiRead.Capacity != APIReadCapacity {
		t.Fatalf("unexpected capacities spa=%d api=%d", spaRead.Capacity, apiRead.Capacity)
	}
}

func TestRateLimitSpecForWriteMethods(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		spec := RateLimitSpecFor(1, AuthKindAPIKey, method)
		if spec.Capacity != APIWriteCapacity {
			t.Fatalf("%s: capacity=%d want write %d", method, spec.Capacity, APIWriteCapacity)
		}
	}
	spec := RateLimitSpecFor(1, AuthKindAPIKey, http.MethodHead)
	if spec.Capacity != APIReadCapacity {
		t.Fatalf("HEAD: capacity=%d want read %d", spec.Capacity, APIReadCapacity)
	}
}

func TestAPIRateLimitMiddlewareUnauthorized(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v2/tasks", nil)
	rec := httptest.NewRecorder()
	called := false
	APIRateLimitMiddleware(func(http.ResponseWriter, *http.Request) {
		called = true
	})(rec, req)
	if called {
		t.Fatal("handler should not run")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d want %d body=%s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
}

func TestAPIRateLimitMiddlewareRequiresRedis(t *testing.T) {
	orig := RedisClient
	RedisClient = nil
	t.Cleanup(func() { RedisClient = orig })

	req := SetAPIUserID(httptest.NewRequest(http.MethodGet, "/api/v2/tasks", nil), 1)
	rec := httptest.NewRecorder()
	APIRateLimitMiddleware(func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler should not run")
	})(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d want %d body=%s", rec.Code, http.StatusServiceUnavailable, rec.Body.String())
	}
	var payload map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload["error"] != "api_unavailable" {
		t.Fatalf("error=%q want api_unavailable", payload["error"])
	}
}

func TestGetAPIAuthKindDefaultsToAPIKey(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if got := GetAPIAuthKind(req); got != AuthKindAPIKey {
		t.Fatalf("kind=%q want %q", got, AuthKindAPIKey)
	}
	req = SetAPIAuthKind(req, AuthKindSession)
	if got := GetAPIAuthKind(req); got != AuthKindSession {
		t.Fatalf("kind=%q want %q", got, AuthKindSession)
	}
}

func TestParseAPIV1SubpathAcceptsV1AndV2(t *testing.T) {
	for _, path := range []string{"/api/v2/tasks/12/comments", "/api/v1/tasks/12/comments"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		got := ParseAPIV1Subpath(req, "tasks")
		if got != "12/comments" {
			t.Fatalf("%s: subpath=%q want 12/comments", path, got)
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v3/tasks/12", nil)
	if got := ParseAPIV1Subpath(req, "tasks"); got != "" {
		t.Fatalf("unexpected subpath %q for /api/v3", got)
	}
}
