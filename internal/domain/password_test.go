package domain

import (
	"errors"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func TestValidatePassword(t *testing.T) {
	tests := []struct {
		name     string
		password string
		wantErr  bool
	}{
		{"too short", "short12", true},
		{"minimum length", "12345678", false},
		{"multibyte counts characters", "ééééééé", true},
		{"eight multibyte characters", "éééééééé", false},
		{"exactly 72 bytes", strings.Repeat("a", 72), false},
		{"73 bytes", strings.Repeat("a", 73), true},
		{"37 two-byte characters is 74 bytes", strings.Repeat("é", 37), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePassword(tt.password)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidatePassword() err = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && !errors.Is(err, ErrValidation) {
				t.Fatalf("error should wrap ErrValidation: %v", err)
			}
		})
	}
}

// Every password ValidatePassword accepts must be hashable by bcrypt.
func TestValidatePasswordAcceptsOnlyHashable(t *testing.T) {
	pw := strings.Repeat("a", MaxPasswordBytes)
	if err := ValidatePassword(pw); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.MinCost); err != nil {
		t.Fatalf("bcrypt rejected an accepted password: %v", err)
	}
}

func TestSpendPasswordCheckTimeTakesBcryptTime(t *testing.T) {
	SpendPasswordCheckTime("warm up the dummy hash")
	start := time.Now()
	SpendPasswordCheckTime("any password")
	if elapsed := time.Since(start); elapsed < 5*time.Millisecond {
		t.Fatalf("dummy compare took %v; expected a real bcrypt compare", elapsed)
	}
}
