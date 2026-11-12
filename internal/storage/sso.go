package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// SSOSettings configures sign-in through an OpenID Connect provider (Google
// Workspace, Microsoft Entra ID, Okta, Authentik, Keycloak, …). One provider per site.
type SSOSettings struct {
	Enabled bool
	// Label is the button text after "Sign in with", e.g. "Okta".
	Label           string
	IssuerURL       string
	ClientID        string
	ClientSecretEnc string
	// Scopes beyond "openid", space separated. Defaults to "email profile".
	Scopes string
	// AutoCreate makes an account the first time someone signs in, when their
	// email domain is allowed. Off means only existing accounts can use SSO.
	AutoCreate bool
	// AllowedDomains limits SSO sign-in and account creation to these email
	// domains (lowercase, no "@"). Empty allows any domain the provider vouches for.
	AllowedDomains []string
	// SSOOnly turns off password sign-in for everyone except admins, who keep it
	// so a broken provider can't lock the site out.
	SSOOnly bool
	// GroupsClaim names the ID token claim holding the person's groups ("groups").
	GroupsClaim string
	// TeamMap maps a provider group to a company-mode team id. Each sign-in adds
	// the person to the mapped teams for their groups and removes them from mapped
	// teams for groups they've left. Teams not in the map are never touched.
	TeamMap   map[string]int
	UpdatedAt time.Time
}

// DefaultSSOScopes are requested alongside "openid".
const DefaultSSOScopes = "email profile"

// MigrateSSO creates the single-row SSO settings table and the identity links.
func MigrateSSO() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	stmts := []string{
		`CREATE TABLE IF NOT EXISTS sso_settings (
			id SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
			enabled BOOLEAN NOT NULL DEFAULT FALSE,
			label TEXT NOT NULL DEFAULT '',
			issuer_url TEXT NOT NULL DEFAULT '',
			client_id TEXT NOT NULL DEFAULT '',
			client_secret_enc TEXT NOT NULL DEFAULT '',
			scopes TEXT NOT NULL DEFAULT '',
			auto_create BOOLEAN NOT NULL DEFAULT FALSE,
			allowed_domains TEXT[] NOT NULL DEFAULT '{}',
			sso_only BOOLEAN NOT NULL DEFAULT FALSE,
			groups_claim TEXT NOT NULL DEFAULT '',
			team_map JSONB NOT NULL DEFAULT '{}',
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS user_identities (
			id SERIAL PRIMARY KEY,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			issuer TEXT NOT NULL,
			subject TEXT NOT NULL,
			email TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			last_login_at TIMESTAMPTZ,
			UNIQUE (issuer, subject)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_user_identities_user ON user_identities (user_id)`,
	}
	for _, s := range stmts {
		if _, err := pool.Exec(context.Background(), s); err != nil {
			return fmt.Errorf("sso tables: %w", err)
		}
	}
	return nil
}

// GetSSOSettings returns the SSO configuration; a site that never set it gets the zero value.
func GetSSOSettings() (*SSOSettings, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	s := &SSOSettings{TeamMap: map[string]int{}, AllowedDomains: []string{}}
	var teamMap []byte
	err = pool.QueryRow(context.Background(), `
		SELECT enabled, label, issuer_url, client_id, client_secret_enc, scopes, auto_create,
		       allowed_domains, sso_only, groups_claim, team_map, updated_at
		FROM sso_settings WHERE id = 1`).Scan(
		&s.Enabled, &s.Label, &s.IssuerURL, &s.ClientID, &s.ClientSecretEnc, &s.Scopes, &s.AutoCreate,
		&s.AllowedDomains, &s.SSOOnly, &s.GroupsClaim, &teamMap, &s.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if len(teamMap) > 0 {
		if err := json.Unmarshal(teamMap, &s.TeamMap); err != nil {
			return nil, fmt.Errorf("sso team map: %w", err)
		}
	}
	if s.AllowedDomains == nil {
		s.AllowedDomains = []string{}
	}
	return s, nil
}

// SaveSSOSettings replaces the SSO configuration.
func SaveSSOSettings(s SSOSettings) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	if s.TeamMap == nil {
		s.TeamMap = map[string]int{}
	}
	if s.AllowedDomains == nil {
		s.AllowedDomains = []string{}
	}
	teamMap, err := json.Marshal(s.TeamMap)
	if err != nil {
		return err
	}
	_, err = pool.Exec(context.Background(), `
		INSERT INTO sso_settings (id, enabled, label, issuer_url, client_id, client_secret_enc, scopes,
		                          auto_create, allowed_domains, sso_only, groups_claim, team_map, updated_at)
		VALUES (1, $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, NOW())
		ON CONFLICT (id) DO UPDATE SET
			enabled = EXCLUDED.enabled, label = EXCLUDED.label, issuer_url = EXCLUDED.issuer_url,
			client_id = EXCLUDED.client_id, client_secret_enc = EXCLUDED.client_secret_enc,
			scopes = EXCLUDED.scopes, auto_create = EXCLUDED.auto_create,
			allowed_domains = EXCLUDED.allowed_domains, sso_only = EXCLUDED.sso_only,
			groups_claim = EXCLUDED.groups_claim, team_map = EXCLUDED.team_map, updated_at = NOW()`,
		s.Enabled, s.Label, s.IssuerURL, s.ClientID, s.ClientSecretEnc, s.Scopes,
		s.AutoCreate, s.AllowedDomains, s.SSOOnly, s.GroupsClaim, teamMap)
	return err
}

// Configured reports whether SSO is on and has what it needs to run.
func (s *SSOSettings) Configured() bool {
	return s != nil && s.Enabled && s.IssuerURL != "" && s.ClientID != "" && s.ClientSecretEnc != ""
}

// DomainAllowed reports whether email's domain may use SSO.
func (s *SSOSettings) DomainAllowed(email string) bool {
	if len(s.AllowedDomains) == 0 {
		return true
	}
	at := strings.LastIndex(email, "@")
	if at < 0 {
		return false
	}
	domain := strings.ToLower(email[at+1:])
	for _, d := range s.AllowedDomains {
		if domain == d {
			return true
		}
	}
	return false
}

// UserIdentity links an account to a provider's subject.
type UserIdentity struct {
	ID          int
	UserID      int
	Issuer      string
	Subject     string
	Email       string
	CreatedAt   time.Time
	LastLoginAt *time.Time
}

// FindUserByIdentity returns the account linked to (issuer, subject), or 0.
func FindUserByIdentity(issuer, subject string) (int, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return 0, err
	}
	defer CloseDatabase(pool)

	var id int
	err = pool.QueryRow(context.Background(),
		`SELECT user_id FROM user_identities WHERE issuer = $1 AND subject = $2`, issuer, subject).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

// LinkUserIdentity records that (issuer, subject) signs in as userID, and stamps the sign-in.
func LinkUserIdentity(userID int, issuer, subject, email string) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	_, err = pool.Exec(context.Background(), `
		INSERT INTO user_identities (user_id, issuer, subject, email, last_login_at)
		VALUES ($1, $2, $3, $4, NOW())
		ON CONFLICT (issuer, subject) DO UPDATE SET email = EXCLUDED.email, last_login_at = NOW()
		WHERE user_identities.user_id = EXCLUDED.user_id`,
		userID, issuer, subject, email)
	return err
}

// ListUserIdentities returns the SSO identities linked to an account.
func ListUserIdentities(userID int) ([]UserIdentity, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(), `
		SELECT id, user_id, issuer, subject, email, created_at, last_login_at
		FROM user_identities WHERE user_id = $1 ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []UserIdentity{}
	for rows.Next() {
		var u UserIdentity
		if err := rows.Scan(&u.ID, &u.UserID, &u.Issuer, &u.Subject, &u.Email, &u.CreatedAt, &u.LastLoginAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// SSOOnlyFor reports whether password sign-in is off for userID: SSO-only is on
// and the account isn't an admin.
func SSOOnlyFor(userID int) bool {
	s, err := GetSSOSettings()
	if err != nil || !s.Configured() || !s.SSOOnly {
		return false
	}
	return !UserHasSitePermission(userID, SitePermAdmin)
}
