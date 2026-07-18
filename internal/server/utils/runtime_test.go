package utils

import (
	"os"
	"testing"
)

func TestResolveMode(t *testing.T) {
	orig := os.Getenv("GOTODO_MODE")
	defer os.Setenv("GOTODO_MODE", orig)
	_ = os.Unsetenv("GOTODO_MODE")

	if got := ResolveMode(nil); got != ModeFull {
		t.Fatalf("default mode = %q, want %q", got, ModeFull)
	}
	if got := ResolveMode([]string{"--mode=api"}); got != ModeAPI {
		t.Fatalf("--mode=api = %q, want %q", got, ModeAPI)
	}
	if got := ResolveMode([]string{"--mode", "api"}); got != ModeAPI {
		t.Fatalf("--mode api = %q, want %q", got, ModeAPI)
	}
	if got := ResolveMode([]string{"--mode=full"}); got != ModeFull {
		t.Fatalf("--mode=full = %q, want %q", got, ModeFull)
	}

	_ = os.Setenv("GOTODO_MODE", "api")
	if got := ResolveMode(nil); got != ModeAPI {
		t.Fatalf("GOTODO_MODE=api = %q, want %q", got, ModeAPI)
	}
	// CLI wins over env
	if got := ResolveMode([]string{"--mode=full"}); got != ModeFull {
		t.Fatalf("CLI should override env: got %q", got)
	}
}
