package storage

import (
	"context"
	"fmt"
)

// UserSummary is a lightweight user record for admin views.
type UserSummary struct {
	ID       int
	Email    string
	UserName string
	IsBanned bool
}

// ListUsers returns all registered users ordered by id.
func ListUsers() ([]UserSummary, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(),
		`SELECT id, email, COALESCE(user_name, ''), COALESCE(is_banned, FALSE)
		 FROM users ORDER BY id ASC`)
	if err != nil {
		return nil, fmt.Errorf("failed to list users: %v", err)
	}
	defer rows.Close()

	var out []UserSummary
	for rows.Next() {
		var u UserSummary
		if err := rows.Scan(&u.ID, &u.Email, &u.UserName, &u.IsBanned); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, nil
}
