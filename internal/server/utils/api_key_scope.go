package utils

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"GoTodo/internal/storage"
)

type apiKeyPrincipalKey struct{}

// Project-key lookups, swappable in tests.
var (
	scopedKeyAgent         = storage.GetAgentByUserID
	scopedKeyStatus        = storage.GetProjectStatus
	scopedKeyTaskProjectID = storage.GetTaskProjectID
	scopedKeyCanManage     = func(projectID, userID int) bool {
		proj, err := storage.GetAccessibleProjectByID(projectID, userID)
		if err != nil || proj == nil {
			return false
		}
		return storage.HasProjectPerm(projectID, proj.Role, storage.PermProjectManage)
	}
)

// scopedKeyError is a rejection with the HTTP response it should produce.
type scopedKeyError struct {
	status  int
	code    string
	message string
}

func (e *scopedKeyError) Error() string { return e.message }

func errScopedEndpoint(projectID int) error {
	return &scopedKeyError{http.StatusForbidden, "forbidden",
		"This API key is limited to project " + strconv.Itoa(projectID) + " and cannot call this endpoint."}
}

func errScopedMissing(scope string) error {
	return &scopedKeyError{http.StatusForbidden, "insufficient_scope",
		"This API key is missing the " + scope + " scope."}
}

func errScopedNotFound() error {
	return &scopedKeyError{http.StatusNotFound, "not_found", "Not found."}
}

func errScopedBadRequest(msg string) error {
	return &scopedKeyError{http.StatusBadRequest, "invalid_request", msg}
}

func errAgentGuardrail(msg string) error {
	return &scopedKeyError{http.StatusForbidden, "agent_guardrail", msg}
}

func errAgentOnly() error {
	return &scopedKeyError{http.StatusForbidden, "forbidden", "This endpoint is only available to AI agent keys."}
}

// resolveKeyAgent loads and checks the agent behind an agent key.
func resolveKeyAgent(p *storage.APIKeyPrincipal) (*storage.ProjectAgent, error) {
	a, err := scopedKeyAgent(p.UserID)
	if err != nil || a == nil || a.ProjectID != p.ProjectID {
		return nil, &scopedKeyError{http.StatusUnauthorized, "unauthorized", "This agent has been removed."}
	}
	if a.Role == "" {
		return nil, &scopedKeyError{http.StatusUnauthorized, "unauthorized", "This agent is no longer a member of its project."}
	}
	if !a.Enabled {
		return nil, &scopedKeyError{http.StatusForbidden, "agent_paused", "This agent is paused by a project manager."}
	}
	return a, nil
}

// WithAPIKeyPrincipal returns r carrying p, as authenticateBearer does.
func WithAPIKeyPrincipal(r *http.Request, p *storage.APIKeyPrincipal) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), apiKeyPrincipalKey{}, p))
}

// GetAPIKeyPrincipal returns the Bearer key behind the request, if any.
func GetAPIKeyPrincipal(r *http.Request) (*storage.APIKeyPrincipal, bool) {
	p, ok := r.Context().Value(apiKeyPrincipalKey{}).(*storage.APIKeyPrincipal)
	return p, ok && p != nil
}

// authenticateBearer resolves token, enforces project-key limits, and attaches
// the user to r. It writes the error response and returns false on failure.
func authenticateBearer(w http.ResponseWriter, r *http.Request, token string) bool {
	p, err := storage.LookupAPIKey(token)
	if err != nil {
		APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Invalid, expired, or revoked API key.")
		return false
	}
	if p.IsAgent && !p.IsProjectScoped() {
		APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Invalid, expired, or revoked API key.")
		return false
	}
	if p.IsProjectScoped() {
		// Agent keys act as the agent, whose own checks run in
		// authorizeProjectKeyRequest; other project keys act as their creator.
		if !p.IsAgent && !scopedKeyCanManage(p.ProjectID, p.UserID) {
			APIJSONError(w, http.StatusUnauthorized, "unauthorized",
				"This API key's creator no longer manages its project.")
			return false
		}
		if err := authorizeProjectKeyRequest(r, p); err != nil {
			var se *scopedKeyError
			if errors.As(err, &se) {
				APIJSONError(w, se.status, se.code, se.message)
			} else {
				APIJSONError(w, http.StatusInternalServerError, "internal_error", "Request failed.")
			}
			return false
		}
	}
	ctx := context.WithValue(r.Context(), apiKeyPrincipalKey{}, p)
	*r = *SetAPIAuthKind(SetAPIUserID(r.WithContext(ctx), p.UserID), AuthKindAPIKey)
	return true
}

// authorizeProjectKeyRequest is the allowlist for project-scoped keys. Anything
// not matched here is denied. The handler still applies the creator's own
// project role, so this only narrows what the key can reach.
func authorizeProjectKeyRequest(r *http.Request, p *storage.APIKeyPrincipal) error {
	parts := strings.Split(apiRoutePath(r), "/")
	pid := p.ProjectID
	require := func(scope string) error {
		if !p.HasScope(scope) {
			return errScopedMissing(scope)
		}
		return nil
	}
	// Agent keys are also bound by the agent's guardrails.
	var agent *storage.ProjectAgent
	if p.IsAgent {
		a, err := resolveKeyAgent(p)
		if err != nil {
			return err
		}
		agent = a
	}

	switch {
	// /agent/... and /mcp: the agent's own queue and MCP endpoint.
	case parts[0] == "agent" || (len(parts) == 1 && parts[0] == "mcp"):
		if agent == nil {
			return errAgentOnly()
		}
		return require(storage.APIScopeTasksRead)

	// /tasks/{id}/claim (agents only)
	case len(parts) == 3 && parts[0] == "tasks" && parts[2] == "claim" && agent != nil:
		taskID, ok := positiveInt(parts[1])
		if !ok || (r.Method != http.MethodPost && r.Method != http.MethodDelete) {
			break
		}
		if err := require(storage.APIScopeTasksWrite); err != nil {
			return err
		}
		return requireTaskInProject(taskID, pid)

	// /tasks
	case len(parts) == 1 && parts[0] == "tasks":
		switch r.Method {
		case http.MethodGet:
			if err := require(storage.APIScopeTasksRead); err != nil {
				return err
			}
			// Pin the list to the key's project whatever the caller asked for.
			q := r.URL.Query()
			q.Set("project", strconv.Itoa(pid))
			r.URL.RawQuery = q.Encode()
			return nil
		case http.MethodPost:
			if err := require(storage.APIScopeTasksWrite); err != nil {
				return err
			}
			if agent != nil && !agent.CanCreateTasks {
				return errAgentGuardrail("This agent is not allowed to create tasks.")
			}
			return checkScopedTaskBody(r, pid, true, agent)
		}

	// /tasks/{id}
	case len(parts) == 2 && parts[0] == "tasks":
		taskID, ok := positiveInt(parts[1])
		if !ok {
			break
		}
		switch r.Method {
		case http.MethodGet:
			if err := require(storage.APIScopeTasksRead); err != nil {
				return err
			}
			return requireTaskInProject(taskID, pid)
		case http.MethodPatch:
			if err := require(storage.APIScopeTasksWrite); err != nil {
				return err
			}
			if err := requireTaskInProject(taskID, pid); err != nil {
				return err
			}
			return checkScopedTaskBody(r, pid, false, agent)
		}

	// /tasks/{id}/comments
	case len(parts) == 3 && parts[0] == "tasks" && parts[2] == "comments":
		taskID, ok := positiveInt(parts[1])
		if !ok {
			break
		}
		switch r.Method {
		case http.MethodGet:
			if err := require(storage.APIScopeTasksRead); err != nil {
				return err
			}
			return requireTaskInProject(taskID, pid)
		case http.MethodPost:
			if err := require(storage.APIScopeCommentsWrite); err != nil {
				return err
			}
			if agent != nil && !agent.CanComment {
				return errAgentGuardrail("This agent is not allowed to post comments.")
			}
			return requireTaskInProject(taskID, pid)
		}

	// /projects/{pid} and its read-only lookups (statuses, sprints, custom fields)
	case parts[0] == "projects" && len(parts) >= 2 && len(parts) <= 3 && r.Method == http.MethodGet:
		id, ok := positiveInt(parts[1])
		if !ok || id != pid {
			break
		}
		if len(parts) == 3 {
			switch parts[2] {
			case "statuses", "sprints", "custom-fields":
			default:
				return errScopedEndpoint(pid)
			}
		}
		return require(storage.APIScopeTasksRead)
	}
	return errScopedEndpoint(pid)
}

func requireTaskInProject(taskID, projectID int) error {
	got, err := scopedKeyTaskProjectID(taskID)
	if err != nil || got != projectID {
		return errScopedNotFound()
	}
	return nil
}

// checkScopedTaskBody keeps creates and updates inside the key's project:
// project_id may only name that project, and parent_id must be one of its tasks.
// For agent keys it also applies the agent's field and status guardrails.
func checkScopedTaskBody(r *http.Request, projectID int, create bool, agent *storage.ProjectAgent) error {
	if r.Body == nil {
		if create {
			return errScopedBadRequest("project_id is required for this API key.")
		}
		return nil
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	_ = r.Body.Close()
	r.Body = io.NopCloser(bytes.NewReader(raw))
	if err != nil {
		return errScopedBadRequest("Failed to read body.")
	}
	// Fail closed: a body this check cannot read is never passed on, so a
	// laxer decoder in a handler can't smuggle fields past it.
	var body map[string]json.RawMessage
	if err := json.Unmarshal(raw, &body); err != nil || body == nil {
		return errScopedBadRequest("Request body must be a JSON object.")
	}

	// encoding/json matches field names case-insensitively, so check every
	// spelling the handler would accept ("project_id", "Project_ID", ...).
	hasProject := false
	for key, val := range body {
		switch {
		case strings.EqualFold(key, "project_id"):
			hasProject = true
			var got *int
			if err := json.Unmarshal(val, &got); err != nil || got == nil || *got != projectID {
				return &scopedKeyError{http.StatusForbidden, "forbidden",
					"This API key can only use project_id " + strconv.Itoa(projectID) + "."}
			}
		case strings.EqualFold(key, "parent_id"):
			var parent *int
			if err := json.Unmarshal(val, &parent); err == nil && parent != nil && *parent > 0 {
				if err := requireTaskInProject(*parent, projectID); err != nil {
					return err
				}
			}
		}
	}
	if create && !hasProject {
		return errScopedBadRequest("project_id is required for this API key.")
	}
	if agent != nil {
		return checkAgentTaskFields(agent, body, create)
	}
	return nil
}

// agentBodyFields maps task body keys to the guardrail that governs them.
// Keys not listed here are refused for agents (fail closed).
var agentBodyFields = map[string]string{
	"title":           storage.AgentFieldTitle,
	"description":     storage.AgentFieldDescription,
	"priority":        storage.AgentFieldPriority,
	"due_date":        storage.AgentFieldDueDate,
	"clear_due_date":  storage.AgentFieldDueDate,
	"tag_ids":         storage.AgentFieldTags,
	"estimate_points": storage.AgentFieldEstimate,
	"sprint_id":       storage.AgentFieldSprint,
	"fields":          storage.AgentFieldCustomFields,
	"status_id":       storage.AgentFieldStatus,
}

// checkAgentTaskFields enforces an agent's guardrails on a task create or update.
func checkAgentTaskFields(a *storage.ProjectAgent, body map[string]json.RawMessage, create bool) error {
	for key, val := range body {
		k := strings.ToLower(key)
		switch k {
		case "project_id":
			continue // already pinned to the agent's project
		case "parent_id":
			// Re-parenting reshapes the board; only agents that may create tasks can do it.
			if !create && !a.CanCreateTasks {
				return errAgentGuardrail("This agent is not allowed to move tasks under a different parent.")
			}
			continue
		case "completed":
			if !a.CanComplete {
				return errAgentGuardrail("This agent is not allowed to complete or reopen tasks.")
			}
			continue
		case "title", "description":
			if create {
				continue // a new task needs a title and description
			}
		}
		field, ok := agentBodyFields[k]
		if !ok {
			return errAgentGuardrail("This agent is not allowed to change " + key + ".")
		}
		if !a.HasField(field) {
			return errAgentGuardrail("This agent is not allowed to change " + field + ".")
		}
		if field == storage.AgentFieldStatus {
			if err := checkAgentStatusMove(a, val); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkAgentStatusMove(a *storage.ProjectAgent, raw json.RawMessage) error {
	var statusID *int
	if err := json.Unmarshal(raw, &statusID); err != nil || statusID == nil || *statusID <= 0 {
		return errAgentGuardrail("This agent must move tasks to a specific status.")
	}
	if len(a.AllowedStatusIDs) > 0 {
		allowed := false
		for _, id := range a.AllowedStatusIDs {
			if id == *statusID {
				allowed = true
				break
			}
		}
		if !allowed {
			return errAgentGuardrail("This agent is not allowed to move tasks to that status.")
		}
	}
	st, err := scopedKeyStatus(a.ProjectID, *statusID)
	if err != nil || st == nil {
		return errScopedBadRequest("Invalid status_id.")
	}
	if st.IsDone && !a.CanComplete {
		return errAgentGuardrail("This agent is not allowed to move tasks to a done status.")
	}
	return nil
}

// apiRoutePath returns the path after /api/v2/ (or /api/v1/), without slashes
// at either end, e.g. "tasks/12/comments".
func apiRoutePath(r *http.Request) string {
	path := r.URL.Path
	if base := PublicPathPrefix(); base != "" {
		path = strings.TrimPrefix(path, base)
	}
	for _, ver := range []string{"/api/v2/", "/api/v1/"} {
		if strings.HasPrefix(path, ver) {
			return strings.Trim(strings.TrimPrefix(path, ver), "/")
		}
	}
	return strings.Trim(path, "/")
}

func positiveInt(s string) (int, bool) {
	n, err := strconv.Atoi(s)
	return n, err == nil && n > 0
}
