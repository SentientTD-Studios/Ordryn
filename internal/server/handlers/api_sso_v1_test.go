package handlers

import (
	"reflect"
	"testing"
)

func TestSafeReturnPath(t *testing.T) {
	cases := map[string]string{
		"":                     "/",
		"/p/web/tasks/4":       "/p/web/tasks/4",
		"/tasks?x=1":           "/tasks?x=1",
		"//evil.example.com":   "/",
		"https://evil.example": "/",
		"/\\evil.example.com":  "/",
		"tasks":                "/",
		"javascript:alert(1)":  "/",
		"  /dashboard  ":       "/dashboard",
	}
	for in, want := range cases {
		if got := safeReturnPath(in); got != want {
			t.Errorf("safeReturnPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSSOClaimHelpers(t *testing.T) {
	if !claimBool(true) || !claimBool("TRUE") || claimBool("no") || claimBool(nil) {
		t.Fatal("claimBool")
	}
	if got := claimStrings([]any{"a", 1, "b"}); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("array: %v", got)
	}
	if got := claimStrings("a, b c"); !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Fatalf("string: %v", got)
	}
}
