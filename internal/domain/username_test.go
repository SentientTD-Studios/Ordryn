package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestPrepareUsername(t *testing.T) {
	name, err := PrepareUsername("  Ada_Lovelace  ")
	if err != nil {
		t.Fatal(err)
	}
	if name != "Ada_Lovelace" {
		t.Fatalf("got %q", name)
	}

	_, err = PrepareUsername("no spaces")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("spaces: %v", err)
	}
	_, err = PrepareUsername("ab")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("short: %v", err)
	}
	_, err = PrepareUsername(strings.Repeat("x", 33))
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("long: %v", err)
	}
	_, err = PrepareUsername("\u200B\u200B")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("invisible only: %v", err)
	}
}
