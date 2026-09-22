package utils

import (
	"net/http/httptest"
	"testing"
)

func TestAbsoluteURLForRequestWithFullBasePath(t *testing.T) {
	origBase := BasePath
	t.Cleanup(func() {
		BasePath = origBase
	})
	BasePath = "https://demo.ryanmalacina.com/gotodo"

	req := httptest.NewRequest("POST", "/api/v2/auth/device/code", nil)
	req.Host = "demo.ryanmalacina.com"
	req.Header.Set("X-Forwarded-Proto", "https")

	got := AbsoluteURLForRequest(req, "/auth/device?user_code=9GGJ-PHGG")
	want := "https://demo.ryanmalacina.com/gotodo/auth/device?user_code=9GGJ-PHGG"
	if got != want {
		t.Fatalf("AbsoluteURLForRequest() = %q, want %q", got, want)
	}
}

func TestAbsoluteURLForRequestWithPathBase(t *testing.T) {
	origBase := BasePath
	t.Cleanup(func() {
		BasePath = origBase
	})
	BasePath = "/gotodo"

	req := httptest.NewRequest("POST", "/api/v2/auth/device/code", nil)
	req.Host = "demo.ryanmalacina.com"
	req.Header.Set("X-Forwarded-Proto", "https")

	got := AbsoluteURLForRequest(req, "/auth/device?user_code=9GGJ-PHGG")
	want := "https://demo.ryanmalacina.com/gotodo/auth/device?user_code=9GGJ-PHGG"
	if got != want {
		t.Fatalf("AbsoluteURLForRequest() = %q, want %q", got, want)
	}
}

func TestTrimPublicPrefix(t *testing.T) {
	origBase := BasePath
	t.Cleanup(func() { BasePath = origBase })
	BasePath = "/gotodo"
	if got := TrimPublicPrefix("/gotodo/uploads/a.png"); got != "/uploads/a.png" {
		t.Fatalf("got %q", got)
	}
	BasePath = "/"
	if got := TrimPublicPrefix("/uploads/a.png"); got != "/uploads/a.png" {
		t.Fatalf("got %q", got)
	}
}
