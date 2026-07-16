package utils

import "testing"

func TestResolveUI(t *testing.T) {
	t.Setenv("GOTODO_UI", "")
	if got := ResolveUI(nil); got != UISPA {
		t.Fatalf("default = %q, want %q", got, UISPA)
	}
	if got := ResolveUI([]string{"--ui=htmx"}); got != UIHtmx {
		t.Fatalf("--ui=htmx = %q", got)
	}
	t.Setenv("GOTODO_UI", "htmx")
	if got := ResolveUI(nil); got != UIHtmx {
		t.Fatalf("env htmx = %q", got)
	}
}
