// Package username provides pure normalize/validate helpers for unique usernames.
package username

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	MinLength = 3
	MaxLength = 32
)

var validPattern = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

// invisible runes stripped from anywhere in the string (after edge trim).
var invisibleRunes = map[rune]bool{
	'\u200B': true, // zero-width space
	'\u200C': true, // zero-width non-joiner
	'\u200D': true, // zero-width joiner
	'\u2060': true, // word joiner
	'\uFEFF': true, // BOM / zero-width no-break space
	'\u00AD': true, // soft hyphen
	'\u180E': true, // mongolian vowel separator
}

// Normalize trims leading/trailing Unicode whitespace and strips invisible characters.
func Normalize(s string) string {
	s = strings.TrimFunc(s, unicode.IsSpace)
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if invisibleRunes[r] {
			continue
		}
		b.WriteRune(r)
	}
	return strings.TrimFunc(b.String(), unicode.IsSpace)
}

// ValidFormat reports whether s (already normalized) matches username rules.
func ValidFormat(s string) bool {
	n := utf8.RuneCountInString(s)
	if n < MinLength || n > MaxLength {
		return false
	}
	return validPattern.MatchString(s)
}

// FormatError returns a human-readable validation message, or "" if valid.
func FormatError(s string) string {
	n := utf8.RuneCountInString(s)
	if s == "" {
		return "username is required"
	}
	if n < MinLength {
		return fmt.Sprintf("username must be at least %d characters", MinLength)
	}
	if n > MaxLength {
		return fmt.Sprintf("username must be at most %d characters", MaxLength)
	}
	if !validPattern.MatchString(s) {
		return "username may only contain letters, numbers, and underscores"
	}
	return ""
}

// EmailPrefix returns the first two ASCII letters from the email local-part (lowercased),
// padding with 'x' when fewer than two letters exist.
func EmailPrefix(email string) string {
	local := email
	if i := strings.IndexByte(email, '@'); i >= 0 {
		local = email[:i]
	}
	var letters []byte
	for _, r := range strings.ToLower(local) {
		if r >= 'a' && r <= 'z' {
			letters = append(letters, byte(r))
			if len(letters) == 2 {
				break
			}
		}
	}
	for len(letters) < 2 {
		letters = append(letters, 'x')
	}
	return string(letters)
}

// GenerateCandidate builds prefix + digitSuffix of the given width.
func GenerateCandidate(email string, digitWidth int) (string, error) {
	if digitWidth < 1 {
		digitWidth = 5
	}
	prefix := EmailPrefix(email)
	digits, err := randomDigits(digitWidth)
	if err != nil {
		return "", err
	}
	return prefix + digits, nil
}

func randomDigits(n int) (string, error) {
	var b strings.Builder
	b.Grow(n)
	for i := 0; i < n; i++ {
		v, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			return "", err
		}
		b.WriteByte(byte('0' + v.Int64()))
	}
	return b.String(), nil
}
