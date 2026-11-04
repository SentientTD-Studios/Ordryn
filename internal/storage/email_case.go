package storage

import (
	"context"
	"fmt"
	"strings"
)

// NormalizeEmail is the stored form of an account email: trimmed and lowercased.
// Lookups compare LOWER(email), so accounts created before normalization with
// mixed case still match.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// EmailCaseDuplicate is a set of accounts whose emails differ only in case.
type EmailCaseDuplicate struct {
	Email   string
	UserIDs []int
}

// FindEmailCaseDuplicates lists accounts that share an email apart from case.
func FindEmailCaseDuplicates() ([]EmailCaseDuplicate, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(), `
		SELECT LOWER(email), array_agg(id ORDER BY id)
		FROM users
		GROUP BY LOWER(email)
		HAVING COUNT(*) > 1
		ORDER BY LOWER(email)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EmailCaseDuplicate
	for rows.Next() {
		var d EmailCaseDuplicate
		var ids []int32
		if err := rows.Scan(&d.Email, &ids); err != nil {
			return nil, err
		}
		for _, id := range ids {
			d.UserIDs = append(d.UserIDs, int(id))
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// MigrateUsersEmailCaseInsensitive adds a unique index on LOWER(email) so two
// accounts can't differ only by the case of their email. If such accounts already
// exist, it logs their ids and leaves the index out: merging or renaming them is
// an admin decision. It runs again on every start, so the index is added once the
// duplicates are resolved.
func MigrateUsersEmailCaseInsensitive() error {
	dups, err := FindEmailCaseDuplicates()
	if err != nil {
		return fmt.Errorf("users email case: %w", err)
	}
	if len(dups) > 0 {
		fmt.Printf("Warning: %d email address(es) are used by more than one account, differing only in case. "+
			"Sign-in picks the account whose email matches exactly, otherwise the oldest. Rename or deactivate the extras, "+
			"then restart to enable case-insensitive uniqueness.\n", len(dups))
		for _, d := range dups {
			fmt.Printf("  accounts %v share one email\n", d.UserIDs)
		}
		return nil
	}

	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	if _, err := pool.Exec(context.Background(),
		`CREATE UNIQUE INDEX IF NOT EXISTS users_email_lower_key ON users (LOWER(email))`); err != nil {
		return fmt.Errorf("users email case: %w", err)
	}
	return nil
}
