package storage

import "context"

// MentionTeam is a team whose members can be @mentioned together on a project.
type MentionTeam struct {
	OrgID int
	Name  string
	// MemberIDs are the team members who are also on the project.
	MemberIDs []int
}

// ListMentionTeams returns teams (organizations other than the company) with at
// least one member on projectID, and which of their members are on it.
func ListMentionTeams(projectID int) ([]MentionTeam, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(), `
		SELECT o.id, o.name, ARRAY_AGG(DISTINCT om.user_id ORDER BY om.user_id)
		FROM organizations o
		JOIN organization_members om ON om.organization_id = o.id
		JOIN project_members pm ON pm.user_id = om.user_id AND pm.project_id = $1
		JOIN users u ON u.id = om.user_id
		WHERE NOT COALESCE(o.is_company, FALSE)
		  AND NOT COALESCE(u.is_agent, FALSE) AND NOT COALESCE(u.is_banned, FALSE)
		GROUP BY o.id, o.name
		ORDER BY LOWER(o.name)`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MentionTeam{}
	for rows.Next() {
		var t MentionTeam
		if err := rows.Scan(&t.OrgID, &t.Name, &t.MemberIDs); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
