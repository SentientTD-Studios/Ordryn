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
	// FullName is "First Last", or empty when no name is set.
	FullName  string
	FirstName string
	LastName  string
	IsBanned  bool
	RoleID    int
	RoleName  string
}

// ListUsers returns all registered users ordered by id. AI agent accounts are
// left out; they are managed per project.
func ListUsers() ([]UserSummary, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(),
		`SELECT u.id, u.email, COALESCE(u.user_name, ''), u.full_name, u.first_name, u.last_name, COALESCE(u.is_banned, FALSE),
		        u.role_id, COALESCE(r.name, ''), COALESCE(r.display_name, '')
		 FROM users u
		 LEFT JOIN roles r ON r.id = u.role_id
		 WHERE NOT COALESCE(u.is_agent, FALSE) ORDER BY u.id ASC`)
	if err != nil {
		return nil, fmt.Errorf("failed to list users: %v", err)
	}
	defer rows.Close()

	var out []UserSummary
	for rows.Next() {
		var roleDisplay string
		var u UserSummary
		if err := rows.Scan(&u.ID, &u.Email, &u.UserName, &u.FullName, &u.FirstName, &u.LastName, &u.IsBanned, &u.RoleID, &u.RoleName, &roleDisplay); err != nil {
			return nil, err
		}
		u.RoleName = SiteRoleLabel(u.RoleName, roleDisplay)
		out = append(out, u)
	}
	return out, nil
}
