package utils

import (
	"errors"
	"net/http"
	"testing"

	"GoTodo/internal/storage"
)

// Statuses on project 10: 1 To do, 2 Review, 3 Done (is_done).
func stubAgent(t *testing.T, a *storage.ProjectAgent) {
	t.Helper()
	prevAgent, prevStatus := scopedKeyAgent, scopedKeyStatus
	scopedKeyAgent = func(userID int) (*storage.ProjectAgent, error) {
		if a == nil || userID != a.UserID {
			return nil, storage.ErrAgentNotFound
		}
		return a, nil
	}
	scopedKeyStatus = func(projectID, statusID int) (*storage.ProjectStatus, error) {
		if projectID != 10 || statusID < 1 || statusID > 3 {
			return nil, errors.New("no such status")
		}
		return &storage.ProjectStatus{ID: statusID, ProjectID: 10, IsDone: statusID == 3}, nil
	}
	t.Cleanup(func() { scopedKeyAgent, scopedKeyStatus = prevAgent, prevStatus })
}

func agentKey() *storage.APIKeyPrincipal {
	return &storage.APIKeyPrincipal{KeyID: 2, UserID: 50, ProjectID: 10, Scopes: allScopes, IsAgent: true}
}

func baseAgent() *storage.ProjectAgent {
	return &storage.ProjectAgent{
		ID: 1, ProjectID: 10, UserID: 50, Enabled: true, Role: "editor",
		EditableFields: []string{storage.AgentFieldStatus, storage.AgentFieldDescription},
		CanComment:     true,
	}
}

func TestAgentKeyFollowsGuardrails(t *testing.T) {
	stubTaskProjects(t, map[int]int{100: 10, 101: 10})
	a := baseAgent()
	a.AllowedStatusIDs = []int{2}
	stubAgent(t, a)
	p := agentKey()

	cases := []struct {
		name, method, target, body string
		want                       int
	}{
		{"read task", http.MethodGet, "/api/v2/tasks/100", "", http.StatusOK},
		{"comment", http.MethodPost, "/api/v2/tasks/100/comments", `{"body":"done"}`, http.StatusOK},
		{"allowed field", http.MethodPatch, "/api/v2/tasks/100", `{"description":"notes"}`, http.StatusOK},
		{"allowed status", http.MethodPatch, "/api/v2/tasks/100", `{"status_id":2}`, http.StatusOK},
		{"status not in list", http.MethodPatch, "/api/v2/tasks/100", `{"status_id":1}`, http.StatusForbidden},
		{"field not granted", http.MethodPatch, "/api/v2/tasks/100", `{"title":"new"}`, http.StatusForbidden},
		{"field not granted, odd case", http.MethodPatch, "/api/v2/tasks/100", `{"Title":"new"}`, http.StatusForbidden},
		{"complete", http.MethodPatch, "/api/v2/tasks/100", `{"completed":true}`, http.StatusForbidden},
		{"reopen", http.MethodPatch, "/api/v2/tasks/100", `{"completed":false}`, http.StatusForbidden},
		{"unknown key", http.MethodPatch, "/api/v2/tasks/100", `{"recurrence":null}`, http.StatusForbidden},
		{"reparent", http.MethodPatch, "/api/v2/tasks/100", `{"parent_id":101}`, http.StatusForbidden},
		{"create", http.MethodPost, "/api/v2/tasks", `{"title":"x","project_id":10}`, http.StatusForbidden},
		{"claim", http.MethodPost, "/api/v2/tasks/100/claim", "", http.StatusOK},
		{"unclaim", http.MethodDelete, "/api/v2/tasks/100/claim", "", http.StatusOK},
		{"own queue", http.MethodGet, "/api/v2/agent/runs", "", http.StatusOK},
		{"mcp", http.MethodPost, "/api/v2/mcp", "", http.StatusOK},
		{"still no delete", http.MethodDelete, "/api/v2/tasks/100", "", http.StatusForbidden},
		{"still no members", http.MethodGet, "/api/v2/projects/10/members", "", http.StatusForbidden},
	}
	for _, c := range cases {
		if got := scopedStatus(t, p, c.method, c.target, c.body); got != c.want {
			t.Errorf("%s: %s %s %s: status %d, want %d", c.name, c.method, c.target, c.body, got, c.want)
		}
	}
}

func TestAgentKeyDoneStatusNeedsCanComplete(t *testing.T) {
	stubTaskProjects(t, map[int]int{100: 10})
	a := baseAgent() // no allowed-status list: any column except done ones
	stubAgent(t, a)
	p := agentKey()
	if got := scopedStatus(t, p, http.MethodPatch, "/api/v2/tasks/100", `{"status_id":1}`); got != http.StatusOK {
		t.Fatalf("move to open column: %d, want 200", got)
	}
	if got := scopedStatus(t, p, http.MethodPatch, "/api/v2/tasks/100", `{"status_id":3}`); got != http.StatusForbidden {
		t.Fatalf("move to done without can_complete: %d, want 403", got)
	}
	a.CanComplete = true
	if got := scopedStatus(t, p, http.MethodPatch, "/api/v2/tasks/100", `{"status_id":3}`); got != http.StatusOK {
		t.Fatalf("move to done with can_complete: %d, want 200", got)
	}
	if got := scopedStatus(t, p, http.MethodPatch, "/api/v2/tasks/100", `{"completed":true}`); got != http.StatusOK {
		t.Fatalf("complete with can_complete: %d, want 200", got)
	}
}

func TestAgentKeyCreateAndCommentToggles(t *testing.T) {
	stubTaskProjects(t, map[int]int{100: 10})
	a := baseAgent()
	a.CanCreateTasks = true
	a.CanComment = false
	stubAgent(t, a)
	p := agentKey()
	if got := scopedStatus(t, p, http.MethodPost, "/api/v2/tasks", `{"title":"x","description":"y","project_id":10,"parent_id":100}`); got != http.StatusOK {
		t.Fatalf("create subtask: %d, want 200", got)
	}
	if got := scopedStatus(t, p, http.MethodPost, "/api/v2/tasks", `{"title":"x","project_id":10,"priority":3}`); got != http.StatusForbidden {
		t.Fatalf("create with ungranted priority: %d, want 403", got)
	}
	if got := scopedStatus(t, p, http.MethodPost, "/api/v2/tasks/100/comments", `{"body":"x"}`); got != http.StatusForbidden {
		t.Fatalf("comment with can_comment off: %d, want 403", got)
	}
}

func TestAgentKeyPausedOrRemoved(t *testing.T) {
	stubTaskProjects(t, map[int]int{100: 10})
	a := baseAgent()
	a.Enabled = false
	stubAgent(t, a)
	p := agentKey()
	if got := scopedStatus(t, p, http.MethodGet, "/api/v2/tasks/100", ""); got != http.StatusForbidden {
		t.Fatalf("paused agent: %d, want 403", got)
	}
	a.Enabled = true
	a.Role = "" // no longer a member
	if got := scopedStatus(t, p, http.MethodGet, "/api/v2/tasks/100", ""); got != http.StatusUnauthorized {
		t.Fatalf("non-member agent: %d, want 401", got)
	}
	stubAgent(t, nil)
	if got := scopedStatus(t, p, http.MethodGet, "/api/v2/tasks/100", ""); got != http.StatusUnauthorized {
		t.Fatalf("removed agent: %d, want 401", got)
	}
}

func TestAgentOnlyRoutesRejectOtherProjectKeys(t *testing.T) {
	p := projectKey(allScopes...)
	for _, target := range []string{"/api/v2/agent", "/api/v2/agent/runs", "/api/v2/mcp"} {
		if got := scopedStatus(t, p, http.MethodGet, target, ""); got != http.StatusForbidden {
			t.Errorf("GET %s with a manager project key: %d, want 403", target, got)
		}
	}
	stubTaskProjects(t, map[int]int{100: 10})
	if got := scopedStatus(t, p, http.MethodPost, "/api/v2/tasks/100/claim", ""); got != http.StatusForbidden {
		t.Errorf("manager project key claiming: %d, want 403 (claim is agent-only)", got)
	}
}
