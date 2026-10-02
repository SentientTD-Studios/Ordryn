package domain

import (
	"errors"
	"strings"
	"testing"

	"GoTodo/internal/storage"
)

func TestDefaultTaskTextLimits(t *testing.T) {
	l := storage.DefaultTaskTextLimits()
	if l.Description != 20000 || l.Comment != 20000 {
		t.Fatalf("expected 20000-character defaults, got %+v", l)
	}
}

func TestValidateDescriptionLength(t *testing.T) {
	const max = 1000
	cases := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{"empty", "", false},
		{"at limit ascii", strings.Repeat("a", max), false},
		{"over limit ascii", strings.Repeat("a", max+1), true},
		// Multi-byte runes count as one character each, not by byte length.
		{"at limit multibyte", strings.Repeat("é", max), false},
		{"over limit multibyte", strings.Repeat("😀", max+1), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateDescriptionLength(tc.in, max)
			if tc.wantErr {
				if !errors.Is(err, ErrValidation) {
					t.Fatalf("expected ErrValidation, got %v", err)
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestValidateCommentBodyLength(t *testing.T) {
	const max = 1000
	cases := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{"empty", "   ", true},
		{"at limit ascii", strings.Repeat("a", max), false},
		{"over limit ascii", strings.Repeat("a", max+1), true},
		{"at limit multibyte", strings.Repeat("ü", max), false},
		{"over limit multibyte", strings.Repeat("ü", max+1), true},
		{"whitespace trimmed before counting", "  " + strings.Repeat("a", max) + "\n\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := validateCommentBodyMax(tc.in, max)
			if tc.wantErr {
				if !errors.Is(err, ErrValidation) {
					t.Fatalf("expected ErrValidation, got %v", err)
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
