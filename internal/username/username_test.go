package username_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"GoTodo/internal/username"
)

func TestNormalize(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"  alice  ", "alice"},
		{"\u200Balice\u200B", "alice"},
		{"\ufeffbob", "bob"},
		{"ada\u00adlovelace", "adalovelace"},
		{"\n\tcarol\t\n", "carol"},
	}
	for _, tt := range tests {
		if got := username.Normalize(tt.in); got != tt.want {
			t.Errorf("Normalize(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestValidFormat(t *testing.T) {
	valid := []string{"abc", "Alice_1", "a_b_c", strings.Repeat("x", 32)}
	for _, s := range valid {
		if !username.ValidFormat(s) {
			t.Errorf("ValidFormat(%q) = false, want true", s)
		}
	}
	invalid := []string{"", "ab", "a b", "ada-1", "😀abc", strings.Repeat("x", 33), "has.dot"}
	for _, s := range invalid {
		if username.ValidFormat(s) {
			t.Errorf("ValidFormat(%q) = true, want false", s)
		}
	}
}

func TestEmailPrefix(t *testing.T) {
	if got := username.EmailPrefix("John.Doe@example.com"); got != "jo" {
		t.Fatalf("EmailPrefix john = %q, want jo", got)
	}
	if got := username.EmailPrefix("123@example.com"); got != "xx" {
		t.Fatalf("EmailPrefix digits = %q, want xx", got)
	}
	if got := username.EmailPrefix("a@example.com"); got != "ax" {
		t.Fatalf("EmailPrefix single letter = %q, want ax", got)
	}
}

func TestGenerateCandidate(t *testing.T) {
	cand, err := username.GenerateCandidate("alice@example.com", 5)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(cand, "al") {
		t.Fatalf("candidate %q missing prefix al", cand)
	}
	if utf8.RuneCountInString(cand) != 7 {
		t.Fatalf("candidate %q length = %d, want 7", cand, utf8.RuneCountInString(cand))
	}
	if !username.ValidFormat(cand) {
		t.Fatalf("candidate %q failed ValidFormat", cand)
	}
}
