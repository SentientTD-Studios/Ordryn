package utils

import (
	"testing"
)

func TestResolveMode(t *testing.T) {
	t.Setenv("ORDRYN_MODE", "")
	t.Setenv("GOTODO_MODE", "")

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

	t.Setenv("ORDRYN_MODE", "api")
	if got := ResolveMode(nil); got != ModeAPI {
		t.Fatalf("ORDRYN_MODE=api = %q, want %q", got, ModeAPI)
	}
	// CLI wins over env
	if got := ResolveMode([]string{"--mode=full"}); got != ModeFull {
		t.Fatalf("CLI should override env: got %q", got)
	}
}

func TestResolveModeLegacyEnv(t *testing.T) {
	t.Setenv("ORDRYN_MODE", "")
	t.Setenv("GOTODO_MODE", "api")
	if got := ResolveMode(nil); got != ModeAPI {
		t.Fatalf("GOTODO_MODE=api = %q, want %q", got, ModeAPI)
	}

	// The new name wins when both are set.
	t.Setenv("ORDRYN_MODE", "full")
	if got := ResolveMode(nil); got != ModeFull {
		t.Fatalf("ORDRYN_MODE should override GOTODO_MODE: got %q", got)
	}
}
