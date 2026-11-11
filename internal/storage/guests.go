package storage

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

// Guest accounts (company mode) are people from outside the company, such as
// clients or contractors. They aren't members of the company, so projects shared
// with the whole company never reach them. What they can reach otherwise is set
// by a site admin in the guest policy and applied when project access is
// recomputed.

// GuestPolicy is the site admin's rule for what guests can reach.
type GuestPolicy struct {
	// Enabled allows creating guests and turning accounts into guests.
	Enabled bool
	// AccessLevel caps project access: AccessView (read only) or AccessFull.
	AccessLevel string
	// RoleID is the site role guests get on projects with full access. Zero
	// means read only even when AccessLevel is full.
	RoleID int
	// ViaTeams lets shares with a team a guest is on reach them. Without it,
	// only projects shared with the guest personally do.
	ViaTeams bool
	// Directory lets guests look up anyone's username (for example to mention
	// them). Without it they only find people on their own projects.
	Directory bool
	UpdatedAt time.Time
}

// MigrateGuests adds users.is_guest and the guest_policy row.
func MigrateGuests() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	for _, stmt := range []string{
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS is_guest BOOLEAN NOT NULL DEFAULT FALSE`,
		`CREATE TABLE IF NOT EXISTS guest_policy (
			id INTEGER PRIMARY KEY DEFAULT 1 CHECK (id = 1),
			enabled BOOLEAN NOT NULL DEFAULT FALSE,
			access_level VARCHAR(8) NOT NULL DEFAULT 'view' CHECK (access_level IN ('view', 'full')),
			role_id INTEGER REFERENCES roles(id) ON DELETE SET NULL,
			via_teams BOOLEAN NOT NULL DEFAULT FALSE,
			directory BOOLEAN NOT NULL DEFAULT FALSE,
			updated_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`INSERT INTO guest_policy (id) VALUES (1) ON CONFLICT DO NOTHING`,
	} {
		if _, err := pool.Exec(context.Background(), stmt); err != nil {
			return fmt.Errorf("guests: %w", err)
		}
	}
	return nil
}

var guestPolicyCache struct {
	sync.Mutex
	policy GuestPolicy
	loaded time.Time
}

// guestPolicyTTL bounds how stale a cached policy can be on another server process.
const guestPolicyTTL = 30 * time.Second

func defaultGuestPolicy() GuestPolicy { return GuestPolicy{AccessLevel: AccessView} }

// GetGuestPolicy returns the guest policy (cached briefly). On a read error it
// returns the strictest policy.
func GetGuestPolicy() GuestPolicy {
	guestPolicyCache.Lock()
	defer guestPolicyCache.Unlock()
	if !guestPolicyCache.loaded.IsZero() && time.Since(guestPolicyCache.loaded) < guestPolicyTTL {
		return guestPolicyCache.policy
	}
	p, err := loadGuestPolicy()
	if err != nil {
		return defaultGuestPolicy()
	}
	guestPolicyCache.policy = p
	guestPolicyCache.loaded = time.Now()
	return p
}

func loadGuestPolicy() (GuestPolicy, error) {
	p := defaultGuestPolicy()
	pool, err := OpenDatabase()
	if err != nil {
		return p, err
	}
	defer CloseDatabase(pool)
	var roleID *int
	err = pool.QueryRow(context.Background(), `
		SELECT enabled, access_level, role_id, via_teams, directory, updated_at FROM guest_policy WHERE id = 1`).
		Scan(&p.Enabled, &p.AccessLevel, &roleID, &p.ViaTeams, &p.Directory, &p.UpdatedAt)
	if err != nil {
		return defaultGuestPolicy(), err
	}
	if roleID != nil {
		p.RoleID = *roleID
	}
	return p, nil
}

// SaveGuestPolicy stores the policy and recomputes every project's access.
func SaveGuestPolicy(p GuestPolicy, actorID int) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	var roleID any
	if p.RoleID > 0 {
		roleID = p.RoleID
	}
	_, err = pool.Exec(context.Background(), `
		UPDATE guest_policy SET enabled = $1, access_level = $2, role_id = $3, via_teams = $4, directory = $5,
		       updated_by = $6, updated_at = NOW()
		WHERE id = 1`, p.Enabled, p.AccessLevel, roleID, p.ViaTeams, p.Directory, actorID)
	CloseDatabase(pool)
	if err != nil {
		return err
	}
	InvalidateGuestPolicyCache()
	return RecomputeAllCompanyProjects()
}

// InvalidateGuestPolicyCache drops the cached policy.
func InvalidateGuestPolicyCache() {
	guestPolicyCache.Lock()
	guestPolicyCache.loaded = time.Time{}
	guestPolicyCache.Unlock()
}

// IsGuestUser reports whether userID is a guest account.
func IsGuestUser(userID int) bool {
	if userID <= 0 {
		return false
	}
	pool, err := OpenDatabase()
	if err != nil {
		return false
	}
	defer CloseDatabase(pool)
	var guest bool
	_ = pool.QueryRow(context.Background(), `SELECT COALESCE(is_guest, FALSE) FROM users WHERE id = $1`, userID).Scan(&guest)
	return guest
}

// SetUserGuest turns an account into a guest (taking it out of the company) or
// back into a regular member (rejoining the company), then recomputes its access.
func SetUserGuest(userID int, guest bool) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	ctx := context.Background()
	_, err = pool.Exec(ctx, `UPDATE users SET is_guest = $2 WHERE id = $1 AND NOT COALESCE(is_agent, FALSE)`, userID, guest)
	if err == nil && guest {
		_, err = pool.Exec(ctx, `
			DELETE FROM organization_members
			WHERE user_id = $1 AND organization_id IN (SELECT id FROM organizations WHERE is_company)`, userID)
	}
	CloseDatabase(pool)
	if err != nil {
		return err
	}
	if !guest {
		if err := JoinCompanyIfEnabled(userID); err != nil {
			return err
		}
	}
	return RecomputeCompanyProjectsForUser(userID)
}

// CountGuests returns how many guest accounts exist.
func CountGuests() (int, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return 0, err
	}
	defer CloseDatabase(pool)
	var n int
	err = pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM users WHERE is_guest`).Scan(&n)
	return n, err
}

// SearchCoMembersByUsernamePrefix finds usernames of people who share a project
// with userID (used for guests who can't search the whole directory).
func SearchCoMembersByUsernamePrefix(userID int, q string, limit int) ([]string, error) {
	q = strings.TrimSpace(q)
	if q == "" || userID <= 0 {
		return []string{}, nil
	}
	if limit <= 0 || limit > userSearchDefaultLimit {
		limit = userSearchDefaultLimit
	}
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	rows, err := pool.Query(context.Background(), `
		SELECT DISTINCT COALESCE(u.user_name, '')
		FROM project_members mine
		JOIN project_members pm ON pm.project_id = mine.project_id
		JOIN users u ON u.id = pm.user_id
		WHERE mine.user_id = $1 AND u.id <> $1
		  AND TRIM(COALESCE(u.user_name, '')) <> ''
		  AND COALESCE(u.is_banned, FALSE) = FALSE
		  AND LOWER(u.user_name) LIKE LOWER($2) ESCAPE E'\\'
		ORDER BY 1
		LIMIT $3`, userID, EscapeLikePattern(q)+"%", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

// guestAllowedSitePerms are the only site permissions a guest's role may hold.
// Project permissions are separate and always allowed.
var guestAllowedSitePerms = map[string]bool{SitePermProjectsCreate: true}

// GuestBlockedPerms returns the permissions in perms a guest's role may not
// hold: admin and every site permission except creating projects.
func GuestBlockedPerms(perms []string) []string {
	site := map[string]bool{SitePermAdmin: true}
	for _, p := range sitePermCatalog {
		site[p.ID] = true
	}
	for _, p := range companySitePermCatalog {
		site[p.ID] = true
	}
	var out []string
	for _, p := range perms {
		if site[p] && !guestAllowedSitePerms[p] {
			out = append(out, p)
		}
	}
	return out
}

// RoleHeldByGuests reports whether any guest has the site role.
func RoleHeldByGuests(roleID int) bool {
	pool, err := OpenDatabase()
	if err != nil {
		return true // fail closed: callers refuse the change
	}
	defer CloseDatabase(pool)
	var held bool
	if err := pool.QueryRow(context.Background(),
		`SELECT EXISTS (SELECT 1 FROM users WHERE role_id = $1 AND is_guest)`, roleID).Scan(&held); err != nil {
		return true
	}
	return held
}

// roleHasExact reports whether the user's role lists perm itself (not via admin).
func roleHasExact(pool interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}, userID int, perm string) bool {
	var has bool
	_ = pool.QueryRow(context.Background(), `
		SELECT COALESCE($2::text = ANY(r.permissions), FALSE) FROM users u JOIN roles r ON r.id = u.role_id WHERE u.id = $1`,
		userID, perm).Scan(&has)
	return has
}
