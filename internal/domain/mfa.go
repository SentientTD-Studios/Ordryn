package domain

import (
	"crypto/rand"
	"crypto/subtle"
	"fmt"
	"strings"
	"time"
	"unicode"

	"GoTodo/internal/crypto/secret"
	"GoTodo/internal/storage"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/bcrypt"
)

const (
	RecoveryCodeCount  = 5
	recoveryCodeLength = 10
	totpPeriodSeconds  = 30
)

// Crockford base32 without I, L, O, U.
const recoveryAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

type TOTPSetup struct {
	Secret     string
	OtpauthURL string
}

type MFAStatus struct {
	Enabled                bool
	RecoveryCodesRemaining int
}

// GenerateTOTPSetup creates a new authenticator secret and otpauth URL.
func GenerateTOTPSetup(issuer, accountName string) (*TOTPSetup, error) {
	issuer = strings.TrimSpace(issuer)
	if issuer == "" {
		issuer = "GoTodo"
	}
	accountName = strings.TrimSpace(accountName)
	if accountName == "" {
		return nil, fmt.Errorf("%w: account name is required", ErrValidation)
	}
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      issuer,
		AccountName: accountName,
		Period:      totpPeriodSeconds,
		Digits:      otp.DigitsSix,
		Algorithm:   otp.AlgorithmSHA1,
		SecretSize:  20,
	})
	if err != nil {
		return nil, err
	}
	return &TOTPSetup{Secret: key.Secret(), OtpauthURL: key.URL()}, nil
}

// VerifyTOTP reports whether code is valid for secret at now, rejecting reused timesteps.
func VerifyTOTP(secret, code string, lastStep int64, now time.Time) (step int64, ok bool) {
	code = strings.TrimSpace(code)
	if len(code) != 6 {
		return 0, false
	}
	for _, r := range code {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	opts := totp.ValidateOpts{
		Period:    totpPeriodSeconds,
		Skew:      0,
		Digits:    otp.DigitsSix,
		Algorithm: otp.AlgorithmSHA1,
	}
	current := now.Unix() / totpPeriodSeconds
	for _, delta := range []int64{-1, 0, 1} {
		candidate := current + delta
		if candidate <= lastStep {
			continue
		}
		expected, err := totp.GenerateCodeCustom(secret, time.Unix(candidate*totpPeriodSeconds, 0), opts)
		if err != nil {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(expected), []byte(code)) == 1 {
			return candidate, true
		}
	}
	return 0, false
}

// NormalizeRecoveryCode strips separators and uppercases a recovery code.
func NormalizeRecoveryCode(code string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(strings.TrimSpace(code)) {
		if unicode.IsSpace(r) || r == '-' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// FormatRecoveryCode displays a 10-character code as XXXXX-XXXXX.
func FormatRecoveryCode(raw string) string {
	raw = NormalizeRecoveryCode(raw)
	if len(raw) != recoveryCodeLength {
		return raw
	}
	return raw[:5] + "-" + raw[5:]
}

// GenerateRecoveryCodes returns n unique plaintext codes in display form.
func GenerateRecoveryCodes(n int) ([]string, error) {
	if n <= 0 {
		return nil, fmt.Errorf("%w: recovery code count must be positive", ErrValidation)
	}
	out := make([]string, 0, n)
	seen := make(map[string]struct{}, n)
	buf := make([]byte, recoveryCodeLength)
	for len(out) < n {
		if _, err := rand.Read(buf); err != nil {
			return nil, err
		}
		var raw strings.Builder
		raw.Grow(recoveryCodeLength)
		for i := 0; i < recoveryCodeLength; i++ {
			raw.WriteByte(recoveryAlphabet[int(buf[i])%len(recoveryAlphabet)])
		}
		norm := raw.String()
		if _, ok := seen[norm]; ok {
			continue
		}
		seen[norm] = struct{}{}
		out = append(out, FormatRecoveryCode(norm))
	}
	return out, nil
}

// HashRecoveryCode bcrypt-hashes the normalized recovery code.
func HashRecoveryCode(code string) (string, error) {
	norm := NormalizeRecoveryCode(code)
	if len(norm) != recoveryCodeLength {
		return "", fmt.Errorf("%w: invalid recovery code", ErrValidation)
	}
	hashed, err := bcrypt.GenerateFromPassword([]byte(norm), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hashed), nil
}

// MatchRecoveryCode finds the first unused hash that matches code.
func MatchRecoveryCode(code string, hashes []storage.RecoveryCodeHash) (id int, ok bool) {
	norm := NormalizeRecoveryCode(code)
	if len(norm) != recoveryCodeLength {
		return 0, false
	}
	plain := []byte(norm)
	for _, h := range hashes {
		if bcrypt.CompareHashAndPassword([]byte(h.Hash), plain) == nil {
			return h.ID, true
		}
	}
	return 0, false
}

func hashRecoveryCodes(codes []string) ([]string, error) {
	hashes := make([]string, 0, len(codes))
	for _, c := range codes {
		h, err := HashRecoveryCode(c)
		if err != nil {
			return nil, err
		}
		hashes = append(hashes, h)
	}
	return hashes, nil
}

func decryptTOTPSecret(encrypted string) (string, error) {
	if encrypted == "" {
		return "", fmt.Errorf("%w: MFA is not configured", ErrValidation)
	}
	plain, err := secret.Decrypt(encrypted)
	if err != nil {
		return "", fmt.Errorf("decrypt mfa secret: %w", err)
	}
	return plain, nil
}

func looksLikeTOTP(code string) bool {
	code = strings.TrimSpace(code)
	if len(code) != 6 {
		return false
	}
	for _, r := range code {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// ConsumeMFACode validates a TOTP or unused recovery code and records the use.
// consumeRecovery controls whether a matching recovery code is marked used.
func ConsumeMFACode(userID int, code string, consumeRecovery bool) error {
	code = strings.TrimSpace(code)
	if code == "" {
		return fmt.Errorf("%w: authentication code is required", ErrValidation)
	}
	state, err := storage.GetMFAState(userID)
	if err != nil {
		return err
	}
	if !state.Enabled {
		return fmt.Errorf("%w: MFA is not enabled", ErrConflict)
	}

	if looksLikeTOTP(code) {
		plain, err := decryptTOTPSecret(state.EncryptedSecret)
		if err != nil {
			return err
		}
		step, ok := VerifyTOTP(plain, code, state.LastStep, time.Now())
		if !ok {
			return fmt.Errorf("%w: invalid authentication code", ErrValidation)
		}
		return storage.UpdateMFALastStep(userID, step)
	}

	hashes, err := storage.ListUnusedRecoveryHashes(userID)
	if err != nil {
		return err
	}
	id, ok := MatchRecoveryCode(code, hashes)
	if !ok {
		return fmt.Errorf("%w: invalid authentication code", ErrValidation)
	}
	if consumeRecovery {
		return storage.MarkRecoveryCodeUsed(id)
	}
	return nil
}

// GetMFAStatus returns whether MFA is on and how many unused recovery codes remain.
func GetMFAStatus(userID int) (*MFAStatus, error) {
	state, err := storage.GetMFAState(userID)
	if err != nil {
		return nil, err
	}
	remaining := 0
	if state.Enabled {
		remaining, err = storage.CountUnusedRecoveryCodes(userID)
		if err != nil {
			return nil, err
		}
	}
	return &MFAStatus{Enabled: state.Enabled, RecoveryCodesRemaining: remaining}, nil
}

// EnableMFA verifies the pending TOTP secret and persists MFA plus 5 recovery codes.
func EnableMFA(userID int, pendingSecret, code string) ([]string, error) {
	pendingSecret = strings.TrimSpace(pendingSecret)
	if pendingSecret == "" {
		return nil, fmt.Errorf("%w: MFA setup has not been started", ErrValidation)
	}
	state, err := storage.GetMFAState(userID)
	if err != nil {
		return nil, err
	}
	if state.Enabled {
		return nil, fmt.Errorf("%w: MFA is already enabled", ErrConflict)
	}
	if _, ok := VerifyTOTP(pendingSecret, strings.TrimSpace(code), 0, time.Now()); !ok {
		return nil, fmt.Errorf("%w: invalid authentication code", ErrValidation)
	}
	encrypted, err := secret.Encrypt(pendingSecret)
	if err != nil {
		return nil, err
	}
	codes, err := GenerateRecoveryCodes(RecoveryCodeCount)
	if err != nil {
		return nil, err
	}
	hashes, err := hashRecoveryCodes(codes)
	if err != nil {
		return nil, err
	}
	if err := storage.EnableUserMFA(userID, encrypted, hashes); err != nil {
		return nil, err
	}
	return codes, nil
}

// DisableMFA turns MFA off after a valid TOTP or recovery code.
func DisableMFA(userID int, code string) error {
	if profile, err := storage.GetUserProfileByID(userID); err == nil &&
		storage.GetSecurityPolicy().MFARequiredForRole(profile.RoleID) {
		return fmt.Errorf("%w: this site requires two-factor authentication for your role", ErrForbidden)
	}
	if err := ConsumeMFACode(userID, code, true); err != nil {
		return err
	}
	return storage.DisableUserMFA(userID)
}

// RegenerateRecoveryCodes replaces all recovery codes after a valid TOTP or recovery code.
func RegenerateRecoveryCodes(userID int, code string) ([]string, error) {
	if err := ConsumeMFACode(userID, code, true); err != nil {
		return nil, err
	}
	codes, err := GenerateRecoveryCodes(RecoveryCodeCount)
	if err != nil {
		return nil, err
	}
	hashes, err := hashRecoveryCodes(codes)
	if err != nil {
		return nil, err
	}
	if err := storage.ReplaceRecoveryCodeHashes(userID, hashes); err != nil {
		return nil, err
	}
	return codes, nil
}

// VerifyLoginMFA validates a TOTP or recovery code during the login challenge.
func VerifyLoginMFA(userID int, code string) error {
	return ConsumeMFACode(userID, code, true)
}
