package domain

import (
	"crypto/rand"
	"fmt"
	"sync"
	"unicode/utf8"

	"GoTodo/internal/storage"

	"golang.org/x/crypto/bcrypt"
)

const (
	// MinPasswordLength is counted in characters.
	MinPasswordLength = 8
	// MaxPasswordBytes is bcrypt's input limit: longer passwords fail to hash.
	MaxPasswordBytes = 72
)

// ValidatePassword enforces the password policy shared by registration, setup,
// password reset and password change. The error wraps ErrValidation and its
// message is safe to show to the user.
func ValidatePassword(password string) error {
	if min := PasswordMinLength(); utf8.RuneCountInString(password) < min {
		return fmt.Errorf("%w: password must be at least %d characters long", ErrValidation, min)
	}
	if len(password) > MaxPasswordBytes {
		return fmt.Errorf("%w: password is too long; use at most %d bytes (72 plain letters and numbers, fewer with accented characters or emoji)",
			ErrValidation, MaxPasswordBytes)
	}
	return nil
}

// PasswordMinLength is the site's minimum (Admin → Security), never below MinPasswordLength.
func PasswordMinLength() int {
	if n := storage.GetSecurityPolicy().MinPasswordLength; n > MinPasswordLength {
		return n
	}
	return MinPasswordLength
}

// CheckNewAccountEmail refuses an email whose domain the security policy doesn't allow.
func CheckNewAccountEmail(email string) error {
	if !storage.GetSecurityPolicy().EmailDomainAllowed(email) {
		return fmt.Errorf("%w: new accounts on this site must use an approved email domain", ErrValidation)
	}
	return nil
}

var dummyPasswordHash struct {
	once sync.Once
	hash []byte
}

// SpendPasswordCheckTime runs a bcrypt compare that always fails, at the same
// cost as a real one. Call it when sign-in fails before any real compare ran
// (unknown email, agent account), so response time doesn't reveal whether the
// account exists.
func SpendPasswordCheckTime(password string) {
	dummyPasswordHash.once.Do(func() {
		secret := make([]byte, 32)
		_, _ = rand.Read(secret)
		hash, err := bcrypt.GenerateFromPassword(secret, bcrypt.DefaultCost)
		if err == nil {
			dummyPasswordHash.hash = hash
		}
	})
	if dummyPasswordHash.hash == nil {
		return
	}
	_ = bcrypt.CompareHashAndPassword(dummyPasswordHash.hash, []byte(password))
}
