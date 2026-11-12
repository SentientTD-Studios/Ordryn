package storage

import (
	"context"
	"fmt"
	"time"
)

// MigrateUsersAddOnboarding adds users.onboarding_dismissed_at: when the user
// hid the getting-started checklist (NULL = still shown).
func MigrateUsersAddOnboarding() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	ctx := context.Background()
	var exists bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM information_schema.columns
		               WHERE table_name = 'users' AND column_name = 'onboarding_dismissed_at')`).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}
	if _, err := pool.Exec(ctx,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS onboarding_dismissed_at TIMESTAMPTZ`); err != nil {
		return fmt.Errorf("add users.onboarding_dismissed_at: %w", err)
	}
	// People already using the site on upgrade don't need a getting-started list.
	if _, err := pool.Exec(ctx, `
		UPDATE users u SET onboarding_dismissed_at = NOW()
		WHERE EXISTS (SELECT 1 FROM tasks t WHERE t.user_id = u.id)`); err != nil {
		return fmt.Errorf("backfill onboarding_dismissed_at: %w", err)
	}
	return nil
}

// OnboardingFacts are what the getting-started checklist is computed from.
type OnboardingFacts struct {
	DismissedAt   *time.Time
	HasProject    bool // member of at least one project
	OwnsProject   bool
	HasTask       bool // created a task
	CompletedTask bool // completed a task they created or claimed
	HasTeammate   bool // shares a project with another person
	SiteUsers     int  // people (not agents or the system account) on the site
}

// GetOnboardingFacts loads a user's onboarding facts in one round trip.
func GetOnboardingFacts(userID int) (OnboardingFacts, error) {
	var f OnboardingFacts
	pool, err := OpenDatabase()
	if err != nil {
		return f, err
	}
	defer CloseDatabase(pool)
	err = pool.QueryRow(context.Background(), `
		SELECT
			(SELECT onboarding_dismissed_at FROM users WHERE id = $1),
			EXISTS (SELECT 1 FROM project_members pm JOIN projects p ON p.id = pm.project_id
			        WHERE pm.user_id = $1)
			  OR EXISTS (SELECT 1 FROM projects p WHERE p.user_id = $1),
			EXISTS (SELECT 1 FROM projects p WHERE p.user_id = $1),
			EXISTS (SELECT 1 FROM tasks t WHERE t.user_id = $1),
			EXISTS (SELECT 1 FROM tasks t WHERE COALESCE(t.completed, false)
			        AND (t.user_id = $1 OR t.claimed_by = $1)),
			EXISTS (SELECT 1 FROM project_members me
			        JOIN project_members other ON other.project_id = me.project_id AND other.user_id <> me.user_id
			        JOIN users u ON u.id = other.user_id
			        WHERE me.user_id = $1 AND NOT COALESCE(u.is_agent, false)),
			(SELECT COUNT(*) FROM users u WHERE NOT COALESCE(u.is_agent, false) AND NOT COALESCE(u.is_system, false))`,
		userID).Scan(&f.DismissedAt, &f.HasProject, &f.OwnsProject, &f.HasTask, &f.CompletedTask, &f.HasTeammate, &f.SiteUsers)
	return f, err
}

// SetOnboardingDismissed hides (true) or brings back (false) the checklist.
func SetOnboardingDismissed(userID int, dismissed bool) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	q := `UPDATE users SET onboarding_dismissed_at = NULL WHERE id = $1`
	if dismissed {
		q = `UPDATE users SET onboarding_dismissed_at = NOW() WHERE id = $1`
	}
	_, err = pool.Exec(context.Background(), q, userID)
	return err
}
