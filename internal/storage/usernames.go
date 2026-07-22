package storage

import (
	"context"
	"fmt"
	"strings"

	"GoTodo/internal/username"

	"github.com/jackc/pgx/v5"
)

// GetUserByUsername loads a user by username (case-insensitive).
func GetUserByUsername(name string) (*User, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	var user User
	err = pool.QueryRow(context.Background(), `
		SELECT id, email, password, COALESCE(user_name, ''), COALESCE(items_per_page, 15)
		FROM users
		WHERE LOWER(user_name) = LOWER($1)`,
		strings.TrimSpace(name)).Scan(
		&user.ID, &user.Email, &user.Password, &user.UserName, &user.ItemsPerPage)
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// UsernameTaken reports whether another user already has this username (case-insensitive).
// excludeUserID of 0 means no exclusion.
func UsernameTaken(name string, excludeUserID int) (bool, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return false, err
	}
	defer CloseDatabase(pool)

	var id int
	err = pool.QueryRow(context.Background(), `
		SELECT id FROM users
		WHERE LOWER(user_name) = LOWER($1) AND ($2 = 0 OR id <> $2)
		LIMIT 1`,
		strings.TrimSpace(name), excludeUserID).Scan(&id)
	if err != nil {
		if err == pgx.ErrNoRows {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// UserHasUsernameChangeAvailable reports whether the user may claim a free username change.
func UserHasUsernameChangeAvailable(userID int) (bool, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return false, err
	}
	defer CloseDatabase(pool)

	var available bool
	err = pool.QueryRow(context.Background(),
		`SELECT COALESCE(username_change_available, FALSE) FROM users WHERE id = $1`, userID).Scan(&available)
	return available, err
}

// SetUsername sets user_name and username_change_available for a user.
func SetUsername(userID int, name string, changeAvailable bool) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	tag, err := pool.Exec(context.Background(), `
		UPDATE users SET user_name = $1, username_change_available = $2, updated_at = CURRENT_TIMESTAMP
		WHERE id = $3`,
		name, changeAvailable, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("user not found")
	}
	return nil
}

// UpdateUserProfilePrefsByID updates mutable prefs without changing user_name.
func UpdateUserProfilePrefsByID(userID int, timezone string, itemsPerPage int, digestEnabled bool, digestHour int, allowProjectInvites bool) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	_, err = pool.Exec(context.Background(), `
		UPDATE users SET timezone = $1, items_per_page = $2,
		       digest_enabled = $3, digest_hour = $4, allow_project_invites = $5,
		       updated_at = CURRENT_TIMESTAMP
		WHERE id = $6`,
		timezone, itemsPerPage, digestEnabled, digestHour, allowProjectInvites, userID)
	return err
}

// MigrateUsersUsernames adds username_change_available, backfills unique usernames,
// grants a free change to existing users (once), and creates a case-insensitive unique index.
func MigrateUsersUsernames() error {
	pool, err := OpenDatabase()
	if err != nil {
		return fmt.Errorf("failed to open database: %v", err)
	}
	defer CloseDatabase(pool)

	ctx := context.Background()

	_, err = pool.Exec(ctx,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS username_change_available BOOLEAN NOT NULL DEFAULT FALSE`)
	if err != nil {
		return fmt.Errorf("failed to add username_change_available: %v", err)
	}

	var indexExists bool
	err = pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM pg_indexes
			WHERE indexname = 'users_user_name_lower_uidx'
		)`).Scan(&indexExists)
	if err != nil {
		return fmt.Errorf("failed to check username index: %v", err)
	}

	if !indexExists {
		if err := backfillAllUsernamesWithFreeChange(ctx); err != nil {
			return err
		}
		_, err = pool.Exec(ctx, `
			CREATE UNIQUE INDEX IF NOT EXISTS users_user_name_lower_uidx
			ON users (LOWER(user_name))`)
		if err != nil {
			return fmt.Errorf("failed to create username unique index: %v", err)
		}
		return nil
	}

	// Index already exists: only repair empty names without re-granting free changes.
	return repairEmptyUsernames(ctx)
}

func backfillAllUsernamesWithFreeChange(ctx context.Context) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(ctx, `SELECT id, email, COALESCE(user_name, '') FROM users ORDER BY id ASC`)
	if err != nil {
		return fmt.Errorf("failed to list users for username backfill: %v", err)
	}
	defer rows.Close()

	type row struct {
		id    int
		email string
		name  string
	}
	var users []row
	for rows.Next() {
		var u row
		if err := rows.Scan(&u.id, &u.email, &u.name); err != nil {
			return err
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	claimed := make(map[string]int) // lower(name) -> user id
	for _, u := range users {
		norm := username.Normalize(u.name)
		if username.ValidFormat(norm) {
			key := strings.ToLower(norm)
			if _, exists := claimed[key]; !exists {
				claimed[key] = u.id
			}
		}
	}

	for _, u := range users {
		norm := username.Normalize(u.name)
		key := strings.ToLower(norm)
		keep := username.ValidFormat(norm) && claimed[key] == u.id

		finalName := norm
		if !keep {
			finalName, err = allocateGeneratedUsername(u.email, claimed)
			if err != nil {
				return fmt.Errorf("generate username for user %d: %w", u.id, err)
			}
			claimed[strings.ToLower(finalName)] = u.id
		}

		_, err = pool.Exec(ctx, `
			UPDATE users SET user_name = $1, username_change_available = TRUE
			WHERE id = $2`, finalName, u.id)
		if err != nil {
			return fmt.Errorf("backfill username for user %d: %w", u.id, err)
		}
	}
	return nil
}

func repairEmptyUsernames(ctx context.Context) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(ctx, `
		SELECT id, email FROM users
		WHERE user_name IS NULL OR TRIM(user_name) = ''
		ORDER BY id ASC`)
	if err != nil {
		return fmt.Errorf("failed to list users needing username repair: %v", err)
	}
	defer rows.Close()

	claimed := make(map[string]int)
	for rows.Next() {
		var id int
		var email string
		if err := rows.Scan(&id, &email); err != nil {
			return err
		}
		name, err := allocateGeneratedUsername(email, claimed)
		if err != nil {
			return fmt.Errorf("repair username for user %d: %w", id, err)
		}
		claimed[strings.ToLower(name)] = id
		_, err = pool.Exec(ctx, `
			UPDATE users SET user_name = $1
			WHERE id = $2 AND (user_name IS NULL OR TRIM(user_name) = '')`,
			name, id)
		if err != nil {
			return err
		}
	}
	return rows.Err()
}

func allocateGeneratedUsername(email string, claimed map[string]int) (string, error) {
	widths := []int{5, 5, 5, 5, 5, 5, 5, 5, 6, 6, 6, 7, 8}
	for _, w := range widths {
		cand, err := username.GenerateCandidate(email, w)
		if err != nil {
			return "", err
		}
		key := strings.ToLower(cand)
		if _, exists := claimed[key]; exists {
			continue
		}
		taken, err := UsernameTaken(cand, 0)
		if err != nil {
			return "", err
		}
		if taken {
			continue
		}
		return cand, nil
	}
	return "", fmt.Errorf("could not allocate unique username for %s", email)
}
