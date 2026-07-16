package utils

import "testing"

func TestResolveUI(t *testing.T) {
	t.Setenv("GOTODO_UI", "")
	if got := ResolveUI(nil); got != UIHtmx {
		t.Fatalf("default = %q, want %q", got, UIHtmx)
	}
	if got := ResolveUI([]string{"--ui=spa"}); got != UISPA {
		t.Fatalf("--ui=spa = %q", got)
	}
	t.Setenv("GOTODO_UI", "spa")
	if got := ResolveUI(nil); got != UISPA {
		t.Fatalf("env spa = %q", got)
	}
}
