package domain

import (
	"errors"
	"strings"
	"testing"

	"GoTodo/internal/storage"
)

func TestTaskTextLimitsMatch(t *testing.T) {
	if MaxDescriptionLength != storage.MaxTaskCommentBody {
		t.Fatalf("description limit %d != comment limit %d", MaxDescriptionLength, storage.MaxTaskCommentBody)
	}
	if MaxDescriptionLength != 5000 {
		t.Fatalf("expected 5000-character limit, got %d", MaxDescriptionLength)
	}
}

func TestValidateDescriptionLength(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{"empty", "", false},
		{"at limit ascii", strings.Repeat("a", MaxDescriptionLength), false},
		{"over limit ascii", strings.Repeat("a", MaxDescriptionLength+1), true},
		// Multi-byte runes count as one character each, not by byte length.
		{"at limit multibyte", strings.Repeat("é", MaxDescriptionLength), false},
		{"over limit multibyte", strings.Repeat("😀", MaxDescriptionLength+1), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateDescriptionLength(tc.in)
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
	cases := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{"empty", "   ", true},
		{"at limit ascii", strings.Repeat("a", storage.MaxTaskCommentBody), false},
		{"over limit ascii", strings.Repeat("a", storage.MaxTaskCommentBody+1), true},
		{"at limit multibyte", strings.Repeat("ü", storage.MaxTaskCommentBody), false},
		{"over limit multibyte", strings.Repeat("ü", storage.MaxTaskCommentBody+1), true},
		{"whitespace trimmed before counting", "  " + strings.Repeat("a", storage.MaxTaskCommentBody) + "\n\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := validateCommentBody(tc.in)
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
