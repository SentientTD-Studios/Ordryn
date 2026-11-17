package utils

import (
	"net/http/httptest"
	"slices"
	"testing"
)

func TestAPIPathSuffix(t *testing.T) {
	cases := map[string]string{
		"/api/v2/me":           "/me",
		"/api/v1/me/mfa/setup": "/me/mfa/setup",
		"/api/v2/tasks/12/":    "/tasks/12",
		"/elsewhere":           "/elsewhere",
	}
	for path, want := range cases {
		r := httptest.NewRequest("GET", path, nil)
		if got := apiPathSuffix(r); got != want {
			t.Errorf("%s: got %q want %q", path, got, want)
		}
	}
	if !slices.Contains(mfaEnrollmentPaths, "/me/mfa/enable") || slices.Contains(mfaEnrollmentPaths, "/tasks") {
		t.Fatal("enrollment allowlist")
	}
}
