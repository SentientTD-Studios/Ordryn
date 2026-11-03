package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// PersonTeam is one team a person belongs to.
type PersonTeam struct {
	ID       int
	Name     string
	Role     string
	RoleName string
}

// Person is an account as shown to people managers.
type Person struct {
	UserSummary
	Teams []PersonTeam
}

// IsUserBannedByID reports whether the account is banned (deactivated). A missing
// account counts as banned so a stale session can't outlive it.
func IsUserBannedByID(userID int) bool {
	pool, err := OpenDatabase()
	if err != nil {
		return false
	}
	defer CloseDatabase(pool)

	var banned bool
	err = pool.QueryRow(context.Background(),
		`SELECT COALESCE(is_banned, FALSE) FROM users WHERE id = $1`, userID).Scan(&banned)
	if err != nil {
		return errors.Is(err, pgx.ErrNoRows)
	}
	return banned
}

// ListUserTeams returns the teams (organizations other than the company) the user is on.
func ListUserTeams(userID int) ([]int, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(), `
		SELECT o.id FROM organization_members m
		JOIN organizations o ON o.id = m.organization_id
		WHERE m.user_id = $1 AND NOT o.is_company
		ORDER BY o.id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// ListPeople returns accounts with their teams. With everyone false, only members of
// teamIDs are listed. Agent and automation accounts are left out.
func ListPeople(everyone bool, teamIDs []int) ([]Person, error) {
	if !everyone && len(teamIDs) == 0 {
		return []Person{}, nil
	}
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	ctx := context.Background()
	rows, err := pool.Query(ctx, `
		SELECT u.id, u.email, COALESCE(u.user_name, ''), u.full_name, u.first_name, u.last_name, COALESCE(u.is_banned, FALSE),
		       u.role_id, COALESCE(r.name, ''), COALESCE(r.display_name, '')
		FROM users u
		LEFT JOIN roles r ON r.id = u.role_id
		WHERE NOT COALESCE(u.is_agent, FALSE)
		  AND ($1 OR EXISTS (
		        SELECT 1 FROM organization_members m
		        WHERE m.user_id = u.id AND m.organization_id = ANY($2)))
		ORDER BY LOWER(COALESCE(NULLIF(u.full_name, ''), NULLIF(u.user_name, ''), u.email)), u.id`, everyone, teamIDs)
	if err != nil {
		return nil, fmt.Errorf("list people: %w", err)
	}
	defer rows.Close()
	out := []Person{}
	index := map[int]int{}
	for rows.Next() {
		var roleDisplay string
		var p Person
		if err := rows.Scan(&p.ID, &p.Email, &p.UserName, &p.FullName, &p.FirstName, &p.LastName, &p.IsBanned, &p.RoleID, &p.RoleName, &roleDisplay); err != nil {
			return nil, err
		}
		p.RoleName = SiteRoleLabel(p.RoleName, roleDisplay)
		p.Teams = []PersonTeam{}
		index[p.ID] = len(out)
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()

	teamRows, err := pool.Query(ctx, `
		SELECT m.user_id, o.id, o.name, m.role FROM organization_members m
		JOIN organizations o ON o.id = m.organization_id
		WHERE NOT o.is_company
		ORDER BY LOWER(o.name), o.id`)
	if err != nil {
		return nil, err
	}
	defer teamRows.Close()
	type teamRow struct {
		userID int
		team   PersonTeam
	}
	var memberships []teamRow
	for teamRows.Next() {
		var t teamRow
		if err := teamRows.Scan(&t.userID, &t.team.ID, &t.team.Name, &t.team.Role); err != nil {
			return nil, err
		}
		if _, ok := index[t.userID]; ok {
			memberships = append(memberships, t)
		}
	}
	if err := teamRows.Err(); err != nil {
		return nil, err
	}
	teamRows.Close()
	for _, t := range memberships {
		t.team.RoleName = OrgRoleDisplayName(t.team.ID, t.team.Role)
		p := &out[index[t.userID]]
		p.Teams = append(p.Teams, t.team)
	}
	return out, nil
}

// TeamRef names a team.
type TeamRef struct {
	ID   int
	Name string
}

// ListTeams returns every organization other than the company, by name.
func ListTeams() ([]TeamRef, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(),
		`SELECT id, name FROM organizations WHERE NOT is_company ORDER BY LOWER(name), id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TeamRef{}
	for rows.Next() {
		var t TeamRef
		if err := rows.Scan(&t.ID, &t.Name); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
