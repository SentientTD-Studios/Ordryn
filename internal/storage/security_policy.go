package storage

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

// MFA requirement modes.
const (
	MFARequireOff   = "off"
	MFARequireAll   = "all"
	MFARequireRoles = "roles"
)

// Password length bounds admins can choose between. The floor matches the
// built-in minimum; the ceiling leaves room under bcrypt's 72-byte limit.
const (
	PolicyMinPasswordFloor   = 8
	PolicyMinPasswordCeiling = 64
)

// SecurityPolicy is the site's sign-in policy (Admin → Security).
type SecurityPolicy struct {
	// RequireMFA is off, all (everyone), or roles (MFARoleIDs only). People
	// without MFA can sign in but only reach their profile until they set it up.
	RequireMFA string
	MFARoleIDs []int
	// MinPasswordLength applies when a password is set or changed.
	MinPasswordLength int
	// SessionIdleMinutes signs a browser out after this long without a request; 0 is off.
	SessionIdleMinutes int
	// SessionMaxHours signs a browser out this long after sign-in; 0 keeps the default.
	SessionMaxHours int
	// AllowedEmailDomains limits new accounts (signup, invites, people managers)
	// to these domains. Empty allows any.
	AllowedEmailDomains []string
	UpdatedAt           time.Time
}

// DefaultSecurityPolicy is what a site that never set a policy gets.
func DefaultSecurityPolicy() SecurityPolicy {
	return SecurityPolicy{RequireMFA: MFARequireOff, MFARoleIDs: []int{}, MinPasswordLength: PolicyMinPasswordFloor,
		AllowedEmailDomains: []string{}}
}

// MigrateSecurityPolicy creates the single-row policy table.
func MigrateSecurityPolicy() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	_, err = pool.Exec(context.Background(), `
		CREATE TABLE IF NOT EXISTS security_policy (
			id SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
			require_mfa TEXT NOT NULL DEFAULT 'off',
			mfa_role_ids INTEGER[] NOT NULL DEFAULT '{}',
			min_password_length INTEGER NOT NULL DEFAULT 8,
			session_idle_minutes INTEGER NOT NULL DEFAULT 0,
			session_max_hours INTEGER NOT NULL DEFAULT 0,
			allowed_email_domains TEXT[] NOT NULL DEFAULT '{}',
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`)
	if err != nil {
		return fmt.Errorf("security_policy: %w", err)
	}
	return nil
}

// The policy is read on every authenticated request, so keep it briefly in memory.
var (
	policyMu      sync.Mutex
	policyCached  *SecurityPolicy
	policyFetched time.Time
)

const policyCacheTTL = 30 * time.Second

// InvalidateSecurityPolicyCache makes the next read hit the database.
func InvalidateSecurityPolicyCache() {
	policyMu.Lock()
	policyCached = nil
	policyMu.Unlock()
}

// GetSecurityPolicy returns the current policy (cached for 30 seconds). On a
// database error it returns the default policy, which never locks anyone out.
func GetSecurityPolicy() SecurityPolicy {
	policyMu.Lock()
	if policyCached != nil && time.Since(policyFetched) < policyCacheTTL {
		p := *policyCached
		policyMu.Unlock()
		return p
	}
	policyMu.Unlock()

	p, err := LoadSecurityPolicy()
	if err != nil {
		fmt.Printf("security policy: %v\n", err)
		return DefaultSecurityPolicy()
	}
	policyMu.Lock()
	policyCached = &p
	policyFetched = time.Now()
	policyMu.Unlock()
	return p
}

// LoadSecurityPolicy reads the policy from the database.
func LoadSecurityPolicy() (SecurityPolicy, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return SecurityPolicy{}, err
	}
	defer CloseDatabase(pool)

	p := DefaultSecurityPolicy()
	err = pool.QueryRow(context.Background(), `
		SELECT require_mfa, mfa_role_ids, min_password_length, session_idle_minutes, session_max_hours,
		       allowed_email_domains, updated_at
		FROM security_policy WHERE id = 1`).Scan(
		&p.RequireMFA, &p.MFARoleIDs, &p.MinPasswordLength, &p.SessionIdleMinutes, &p.SessionMaxHours,
		&p.AllowedEmailDomains, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return DefaultSecurityPolicy(), nil
	}
	if err != nil {
		return SecurityPolicy{}, err
	}
	if p.MFARoleIDs == nil {
		p.MFARoleIDs = []int{}
	}
	if p.AllowedEmailDomains == nil {
		p.AllowedEmailDomains = []string{}
	}
	return p, nil
}

// SaveSecurityPolicy replaces the policy.
func SaveSecurityPolicy(p SecurityPolicy) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	if p.MFARoleIDs == nil {
		p.MFARoleIDs = []int{}
	}
	if p.AllowedEmailDomains == nil {
		p.AllowedEmailDomains = []string{}
	}
	_, err = pool.Exec(context.Background(), `
		INSERT INTO security_policy (id, require_mfa, mfa_role_ids, min_password_length, session_idle_minutes,
		                             session_max_hours, allowed_email_domains, updated_at)
		VALUES (1, $1, $2, $3, $4, $5, $6, NOW())
		ON CONFLICT (id) DO UPDATE SET
			require_mfa = EXCLUDED.require_mfa, mfa_role_ids = EXCLUDED.mfa_role_ids,
			min_password_length = EXCLUDED.min_password_length,
			session_idle_minutes = EXCLUDED.session_idle_minutes,
			session_max_hours = EXCLUDED.session_max_hours,
			allowed_email_domains = EXCLUDED.allowed_email_domains, updated_at = NOW()`,
		p.RequireMFA, p.MFARoleIDs, p.MinPasswordLength, p.SessionIdleMinutes, p.SessionMaxHours, p.AllowedEmailDomains)
	if err == nil {
		InvalidateSecurityPolicyCache()
	}
	return err
}

// MFARequiredForRole reports whether the policy makes accounts with roleID set up MFA.
func (p SecurityPolicy) MFARequiredForRole(roleID int) bool {
	switch p.RequireMFA {
	case MFARequireAll:
		return true
	case MFARequireRoles:
		return slices.Contains(p.MFARoleIDs, roleID)
	}
	return false
}

// EmailDomainAllowed reports whether a new account may use email.
func (p SecurityPolicy) EmailDomainAllowed(email string) bool {
	if len(p.AllowedEmailDomains) == 0 {
		return true
	}
	at := strings.LastIndex(email, "@")
	if at < 0 {
		return false
	}
	return slices.Contains(p.AllowedEmailDomains, strings.ToLower(strings.TrimSpace(email[at+1:])))
}

// MFAEnrollmentRequired reports whether userID must set up MFA before using the site.
func MFAEnrollmentRequired(userID int) bool {
	p := GetSecurityPolicy()
	if p.RequireMFA == MFARequireOff {
		return false
	}
	pool, err := OpenDatabase()
	if err != nil {
		return false
	}
	defer CloseDatabase(pool)

	var roleID int
	if err := pool.QueryRow(context.Background(),
		`SELECT COALESCE(role_id, 0) FROM users WHERE id = $1`, userID).Scan(&roleID); err != nil {
		return false
	}
	if !p.MFARequiredForRole(roleID) {
		return false
	}
	has, err := UserHasMFA(userID)
	return err == nil && !has
}
