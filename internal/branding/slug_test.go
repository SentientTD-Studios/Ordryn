package branding

import (
	"strings"
	"testing"
)

func TestSlug(t *testing.T) {
	cases := map[string]string{
		"Acme Tasks":                  "acme-tasks",
		"  Ordryn!  ":                 "ordryn",
		"":                            DefaultSlug,
		"***":                         DefaultSlug,
		"日本":                          DefaultSlug,
		"Web App (v2)":                "web-app-v2",
		strings.Repeat("a", 30):       strings.Repeat("a", 24),
		"abcdefghijklmnopqrstuvw xyz": "abcdefghijklmnopqrstuvw",
	}
	for in, want := range cases {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestKeyPrefix(t *testing.T) {
	if got := KeyPrefix("Acme Tasks"); got != "acmetasks" {
		t.Fatalf("got %q", got)
	}
	if got := KeyPrefix(""); got != DefaultSlug {
		t.Fatalf("got %q", got)
	}
}
