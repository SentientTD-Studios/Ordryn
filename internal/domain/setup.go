package domain

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base32"
	"errors"
	"fmt"
	"strings"
	"sync"

	"GoTodo/internal/storage"

	"golang.org/x/crypto/bcrypt"
)

// Registration policies offered during first-time setup.
const (
	SetupRegistrationInvite = "invite"
	SetupRegistrationOpen   = "open"
	SetupRegistrationClosed = "closed"
)

// SetupInput is everything first-time setup configures.
type SetupInput struct {
	Token           string
	Email           string
	UserName        string
	Password        string
	ConfirmPassword string
	// Timezone is the admin's timezone and becomes the site default.
	Timezone     string
	SiteName     string
	SiteMode     string
	Registration string
	EnableAPI    bool
}

// setupToken guards first-time setup. It lives only in this process: a restart
// issues a new one, and each replica behind a load balancer has its own.
var setupToken struct {
	sync.Mutex
	value string
}

var setupTokenEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// IssueSetupToken returns the token that first-time setup requires, creating it on
// first use. It returns "" when the site is already set up.
func IssueSetupToken() (string, error) {
	required, err := storage.SetupRequired()
	if err != nil || !required {
		return "", err
	}
	setupToken.Lock()
	defer setupToken.Unlock()
	if setupToken.value == "" {
		b := make([]byte, 10)
		if _, err := rand.Read(b); err != nil {
			return "", err
		}
		raw := strings.ToLower(setupTokenEncoding.EncodeToString(b))
		setupToken.value = raw[0:4] + "-" + raw[4:8] + "-" + raw[8:12] + "-" + raw[12:16]
	}
	return setupToken.value, nil
}

func normalizeSetupToken(s string) string {
	s = strings.ToLower(s)
	return strings.Map(func(r rune) rune {
		if r == '-' || r == ' ' {
			return -1
		}
		return r
	}, s)
}

func setupTokenMatches(given string) bool {
	setupToken.Lock()
	want := setupToken.value
	setupToken.Unlock()
	if want == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(normalizeSetupToken(given)), []byte(normalizeSetupToken(want))) == 1
}

func clearSetupToken() {
	setupToken.Lock()
	setupToken.value = ""
	setupToken.Unlock()
}

// CompleteSetup creates the first admin account and applies the initial site
// settings, then returns the admin's id. timezoneOK should come from utils.IsValidTimezone.
func CompleteSetup(ctx context.Context, in SetupInput, timezoneOK bool) (int, error) {
	_ = ctx
	required, err := storage.SetupRequired()
	if err != nil {
		return 0, err
	}
	if !required {
		return 0, fmt.Errorf("%w: %s", ErrConflict, storage.ErrSetupComplete)
	}
	if !setupTokenMatches(in.Token) {
		return 0, fmt.Errorf("%w: the setup token is incorrect; copy it from the server log", ErrForbidden)
	}

	email := strings.TrimSpace(in.Email)
	if email == "" || !strings.Contains(email, "@") {
		return 0, fmt.Errorf("%w: a valid email address is required", ErrValidation)
	}
	userName, available, err := UsernameAvailable(in.UserName, 0)
	if err != nil {
		return 0, err
	}
	if !available {
		return 0, fmt.Errorf("%w: that username is reserved; choose another", ErrValidation)
	}
	if len(in.Password) < 8 {
		return 0, fmt.Errorf("%w: password must be at least 8 characters long", ErrValidation)
	}
	if in.Password != in.ConfirmPassword {
		return 0, fmt.Errorf("%w: passwords do not match", ErrValidation)
	}
	timezone := strings.TrimSpace(in.Timezone)
	if timezone == "" || !timezoneOK {
		return 0, fmt.Errorf("%w: invalid timezone", ErrValidation)
	}
	mode := storage.NormalizeSiteMode(in.SiteMode)
	if mode == "" {
		return 0, fmt.Errorf("%w: site mode must be multi or company", ErrValidation)
	}
	registration := strings.ToLower(strings.TrimSpace(in.Registration))
	if registration == "" {
		registration = SetupRegistrationInvite
	}
	if registration != SetupRegistrationInvite && registration != SetupRegistrationOpen && registration != SetupRegistrationClosed {
		return 0, fmt.Errorf("%w: registration must be invite, open, or closed", ErrValidation)
	}
	// A company's accounts come from admins and people managers, never self-signup.
	if mode == storage.SiteModeCompany {
		registration = SetupRegistrationClosed
	}
	siteName := strings.TrimSpace(in.SiteName)

	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		return 0, err
	}
	adminID, err := storage.CreateSetupAdmin(storage.SetupAdmin{
		Email:        email,
		PasswordHash: string(hash),
		UserName:     userName,
		Timezone:     timezone,
	})
	if errors.Is(err, storage.ErrSetupComplete) {
		return 0, fmt.Errorf("%w: %s", ErrConflict, err)
	}
	if err != nil {
		return 0, err
	}
	clearSetupToken()

	// The admin exists from here on, so a failure below leaves settings for the
	// admin to finish in Admin rather than reopening setup.
	if err := storage.EnsureSiteSettingsRow(); err != nil {
		return adminID, fmt.Errorf("setup site settings: %w", err)
	}
	// Whoever sets the site up owns it: the one account above the other admins.
	if err := storage.SetSiteOwner(adminID); err != nil {
		return adminID, fmt.Errorf("setup site owner: %w", err)
	}
	settings, err := storage.GetSiteSettings()
	if err != nil {
		return adminID, fmt.Errorf("setup site settings: %w", err)
	}
	if siteName != "" {
		settings.SiteName = siteName
	}
	settings.DefaultTimezone = timezone
	settings.EnableRegistration = registration != SetupRegistrationClosed
	settings.InviteOnly = registration == SetupRegistrationInvite
	settings.EnableAPI = in.EnableAPI
	if err := storage.UpsertSiteSettings(*settings); err != nil {
		return adminID, fmt.Errorf("setup site settings: %w", err)
	}
	// Runs after the site name is saved, since company mode names the company after the site.
	if err := storage.ChangeSiteMode(mode); err != nil {
		return adminID, fmt.Errorf("setup site mode: %w", err)
	}
	return adminID, nil
}
