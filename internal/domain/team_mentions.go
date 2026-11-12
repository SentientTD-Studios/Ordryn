package domain

import (
	"regexp"
	"strings"

	"GoTodo/internal/storage"
)

var teamHandleUnsafe = regexp.MustCompile(`[^a-z0-9_]+`)

// TeamMentionHandle is how a team is @mentioned: its name lowercased with
// anything but letters and digits turned into underscores ("Design Team" ->
// "design_team").
func TeamMentionHandle(name string) string {
	return strings.Trim(teamHandleUnsafe.ReplaceAllString(strings.ToLower(strings.TrimSpace(name)), "_"), "_")
}

// teamMentionMembers expands @team handles in names to the team members on the
// project. Names that match a project member's username are left to the caller:
// people win over teams with the same handle.
func teamMentionMembers(projectID int, names map[string]struct{}, members []storage.ProjectMember) []storage.ProjectMember {
	if len(names) == 0 {
		return nil
	}
	teams, err := storage.ListMentionTeams(projectID)
	if err != nil || len(teams) == 0 {
		return nil
	}
	byID := make(map[int]storage.ProjectMember, len(members))
	usernames := make(map[string]struct{}, len(members))
	for _, m := range members {
		byID[m.UserID] = m
		usernames[strings.ToLower(strings.TrimSpace(m.UserName))] = struct{}{}
	}
	var out []storage.ProjectMember
	for _, t := range teams {
		handle := TeamMentionHandle(t.Name)
		if handle == "" {
			continue
		}
		if _, ok := names[handle]; !ok {
			continue
		}
		if _, isUser := usernames[handle]; isUser {
			continue
		}
		for _, id := range t.MemberIDs {
			if m, ok := byID[id]; ok {
				out = append(out, m)
			}
		}
	}
	return out
}

// teamHandlesMatching returns team handles on the project starting with prefix.
func teamHandlesMatching(projectID int, prefix string, limit int) []string {
	prefix = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(prefix), "@"))
	if projectID <= 0 || prefix == "" || limit <= 0 {
		return nil
	}
	teams, err := storage.ListMentionTeams(projectID)
	if err != nil {
		return nil
	}
	var out []string
	for _, t := range teams {
		h := TeamMentionHandle(t.Name)
		if h != "" && strings.HasPrefix(h, prefix) {
			out = append(out, h)
			if len(out) == limit {
				break
			}
		}
	}
	return out
}

// TeamHandlesForMention returns up to limit team handles on the project starting
// with q, for the mention autocomplete. The caller already checked project access.
func TeamHandlesForMention(projectID int, q string, limit int) []string {
	return teamHandlesMatching(projectID, q, limit)
}
