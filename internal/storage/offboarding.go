package storage

import "context"

// OffboardingSummary is what an account still holds when someone leaves.
type OffboardingSummary struct {
	// ClaimedTaskIDs are open tasks the account has claimed.
	ClaimedTaskIDs []int
	// OwnedProjectIDs are live projects the account owns (multi mode) or created
	// (company mode, where managing comes from site roles and shares).
	OwnedProjectIDs []int
	PersonalKeys    int
	ProjectKeys     int
	// AgentsCreated counts active project AI agents the account added.
	AgentsCreated int
}

// GetOffboardingSummary loads what userID holds.
func GetOffboardingSummary(userID int) (*OffboardingSummary, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	ctx := context.Background()
	s := &OffboardingSummary{ClaimedTaskIDs: []int{}, OwnedProjectIDs: []int{}}
	rows, err := pool.Query(ctx, `
		SELECT t.id FROM tasks t
		WHERE t.claimed_by = $1 AND NOT COALESCE(t.completed, FALSE) AND t.deleted_at IS NULL
		ORDER BY t.id`, userID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		s.ClaimedTaskIDs = append(s.ClaimedTaskIDs, id)
	}
	rows.Close()

	ownedSQL := `SELECT pm.project_id FROM project_members pm JOIN projects p ON p.id = pm.project_id
		WHERE pm.user_id = $1 AND pm.role = 'owner' AND NOT COALESCE(p.archived, FALSE) ORDER BY pm.project_id`
	if IsCompanyMode() {
		ownedSQL = `SELECT p.id FROM projects p WHERE p.user_id = $1 AND NOT COALESCE(p.archived, FALSE) ORDER BY p.id`
	}
	rows, err = pool.Query(ctx, ownedSQL, userID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		s.OwnedProjectIDs = append(s.OwnedProjectIDs, id)
	}
	rows.Close()

	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FILTER (WHERE project_id IS NULL), COUNT(*) FILTER (WHERE project_id IS NOT NULL)
		FROM api_keys WHERE user_id = $1 AND revoked_at IS NULL`, userID).Scan(&s.PersonalKeys, &s.ProjectKeys); err != nil {
		return nil, err
	}
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM project_agents WHERE created_by = $1 AND removed_at IS NULL`, userID).Scan(&s.AgentsCreated); err != nil {
		return nil, err
	}
	return s, nil
}

// RevokeAllAPIKeysForUser revokes every personal and project key userID created.
func RevokeAllAPIKeysForUser(userID int) (int, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return 0, err
	}
	defer CloseDatabase(pool)

	tag, err := pool.Exec(context.Background(),
		`UPDATE api_keys SET revoked_at = NOW() WHERE user_id = $1 AND revoked_at IS NULL`, userID)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}
