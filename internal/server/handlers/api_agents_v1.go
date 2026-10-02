package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"GoTodo/internal/domain"
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
)

type apiAgentJSON struct {
	ID                int      `json:"id"`
	ProjectID         int      `json:"project_id"`
	UserID            int      `json:"user_id"`
	Handle            string   `json:"handle"`
	Name              string   `json:"name"`
	Description       string   `json:"description"`
	Instructions      string   `json:"instructions"`
	Enabled           bool     `json:"enabled"`
	Role              string   `json:"role"`
	RoleName          string   `json:"role_name"`
	TriggerOnMention  bool     `json:"trigger_on_mention"`
	TriggerStatusIDs  []int    `json:"trigger_status_ids"`
	TriggerBy         string   `json:"trigger_by"`
	TriggerRoleSlugs  []string `json:"trigger_role_slugs"`
	TriggerUserIDs    []int    `json:"trigger_user_ids"`
	ClaimOnDispatch   bool     `json:"claim_on_dispatch"`
	AllowedStatusIDs  []int    `json:"allowed_status_ids"`
	EditableFields    []string `json:"editable_fields"`
	CanComplete       bool     `json:"can_complete"`
	CanCreateTasks    bool     `json:"can_create_tasks"`
	CanComment        bool     `json:"can_comment"`
	MaxRunsPerHour    int      `json:"max_runs_per_hour"`
	WebhookURL        string   `json:"webhook_url"`
	WebhookSecretSet  bool     `json:"webhook_secret_set"`
	LastDeliveryAt    *string  `json:"last_delivery_at"`
	LastDeliveryError string   `json:"last_delivery_error"`
	CreatedByID       int      `json:"created_by_id"`
	CreatedAt         string   `json:"created_at"`
	UpdatedAt         string   `json:"updated_at"`
}

// apiAgentInput is the create/PATCH body. Omitted fields keep their value.
type apiAgentInput struct {
	Handle           string    `json:"handle"`
	Name             *string   `json:"name"`
	Description      *string   `json:"description"`
	Instructions     *string   `json:"instructions"`
	Enabled          *bool     `json:"enabled"`
	Role             *string   `json:"role"`
	WebhookURL       *string   `json:"webhook_url"`
	TriggerOnMention *bool     `json:"trigger_on_mention"`
	TriggerStatusIDs *[]int    `json:"trigger_status_ids"`
	TriggerBy        *string   `json:"trigger_by"`
	TriggerRoleSlugs *[]string `json:"trigger_role_slugs"`
	TriggerUserIDs   *[]int    `json:"trigger_user_ids"`
	ClaimOnDispatch  *bool     `json:"claim_on_dispatch"`
	AllowedStatusIDs *[]int    `json:"allowed_status_ids"`
	EditableFields   *[]string `json:"editable_fields"`
	CanComplete      *bool     `json:"can_complete"`
	CanCreateTasks   *bool     `json:"can_create_tasks"`
	CanComment       *bool     `json:"can_comment"`
	MaxRunsPerHour   *int      `json:"max_runs_per_hour"`
}

func (in apiAgentInput) toDomain() domain.AgentInput {
	return domain.AgentInput{
		Handle:           in.Handle,
		Name:             in.Name,
		Description:      in.Description,
		Instructions:     in.Instructions,
		Enabled:          in.Enabled,
		Role:             in.Role,
		WebhookURL:       in.WebhookURL,
		TriggerOnMention: in.TriggerOnMention,
		TriggerStatusIDs: in.TriggerStatusIDs,
		TriggerBy:        in.TriggerBy,
		TriggerRoleSlugs: in.TriggerRoleSlugs,
		TriggerUserIDs:   in.TriggerUserIDs,
		ClaimOnDispatch:  in.ClaimOnDispatch,
		AllowedStatusIDs: in.AllowedStatusIDs,
		EditableFields:   in.EditableFields,
		CanComplete:      in.CanComplete,
		CanCreateTasks:   in.CanCreateTasks,
		CanComment:       in.CanComment,
		MaxRunsPerHour:   in.MaxRunsPerHour,
	}
}

type apiAgentRunJSON struct {
	ID            int     `json:"id"`
	AgentID       int     `json:"agent_id"`
	AgentName     string  `json:"agent_name"`
	ProjectID     int     `json:"project_id"`
	TaskID        int     `json:"task_id"`
	TaskTitle     string  `json:"task_title"`
	Trigger       string  `json:"trigger"`
	TriggeredByID int     `json:"triggered_by_id"`
	TriggeredBy   string  `json:"triggered_by"`
	Note          string  `json:"note"`
	Status        string  `json:"status"`
	Delivered     bool    `json:"delivered"`
	DeliveryError string  `json:"delivery_error"`
	Summary       string  `json:"summary"`
	CreatedAt     string  `json:"created_at"`
	StartedAt     *string `json:"started_at"`
	FinishedAt    *string `json:"finished_at"`
}

func agentToJSON(a storage.ProjectAgent) apiAgentJSON {
	return apiAgentJSON{
		ID:                a.ID,
		ProjectID:         a.ProjectID,
		UserID:            a.UserID,
		Handle:            a.Handle,
		Name:              a.Name,
		Description:       a.Description,
		Instructions:      a.Instructions,
		Enabled:           a.Enabled,
		Role:              a.Role,
		RoleName:          storage.RoleDisplayName(a.ProjectID, a.Role),
		TriggerOnMention:  a.TriggerOnMention,
		TriggerStatusIDs:  nonNilInts(a.TriggerStatusIDs),
		TriggerBy:         a.TriggerBy,
		TriggerRoleSlugs:  nonNilStrings(a.TriggerRoleSlugs),
		TriggerUserIDs:    nonNilInts(a.TriggerUserIDs),
		ClaimOnDispatch:   a.ClaimOnDispatch,
		AllowedStatusIDs:  nonNilInts(a.AllowedStatusIDs),
		EditableFields:    nonNilStrings(a.EditableFields),
		CanComplete:       a.CanComplete,
		CanCreateTasks:    a.CanCreateTasks,
		CanComment:        a.CanComment,
		MaxRunsPerHour:    a.MaxRunsPerHour,
		WebhookURL:        a.WebhookURL,
		WebhookSecretSet:  a.WebhookSecretSet,
		LastDeliveryAt:    optionalRFC3339(a.LastDeliveryAt),
		LastDeliveryError: a.LastDeliveryError,
		CreatedByID:       a.CreatedBy,
		CreatedAt:         formatRFC3339(a.CreatedAt),
		UpdatedAt:         formatRFC3339(a.UpdatedAt),
	}
}

func agentRunToJSON(r storage.AgentRun) apiAgentRunJSON {
	return apiAgentRunJSON{
		ID:            r.ID,
		AgentID:       r.AgentID,
		AgentName:     r.AgentName,
		ProjectID:     r.ProjectID,
		TaskID:        r.TaskID,
		TaskTitle:     r.TaskTitle,
		Trigger:       r.Trigger,
		TriggeredByID: r.TriggeredBy,
		TriggeredBy:   r.TriggeredByName,
		Note:          r.Note,
		Status:        r.Status,
		Delivered:     r.Delivered,
		DeliveryError: r.DeliveryError,
		Summary:       r.Summary,
		CreatedAt:     formatRFC3339(r.CreatedAt),
		StartedAt:     optionalRFC3339(r.StartedAt),
		FinishedAt:    optionalRFC3339(r.FinishedAt),
	}
}

func agentRunsToJSON(runs []storage.AgentRun) []apiAgentRunJSON {
	out := make([]apiAgentRunJSON, 0, len(runs))
	for _, r := range runs {
		out = append(out, agentRunToJSON(r))
	}
	return out
}

func nonNilInts(v []int) []int {
	if v == nil {
		return []int{}
	}
	return v
}

func nonNilStrings(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

func writeAgentError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Not found.")
	case errors.Is(err, domain.ErrForbidden):
		utils.APIJSONError(w, http.StatusForbidden, "forbidden", sharingClientMessage(err, "Only project managers can manage AI agents."))
	case errors.Is(err, domain.ErrConflict):
		utils.APIJSONError(w, http.StatusConflict, "conflict", sharingClientMessage(err, "Conflict."))
	case errors.Is(err, domain.ErrValidation):
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", sharingClientMessage(err, "Invalid request."))
	default:
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Request failed.")
	}
}

func methodNotAllowed(w http.ResponseWriter) {
	utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
}

// apiV1ProjectAgents handles /api/v2/projects/{id}/agents/...
func apiV1ProjectAgents(w http.ResponseWriter, r *http.Request, projectID int, rest []string) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	if len(rest) == 0 {
		switch r.Method {
		case http.MethodGet:
			agents, err := domain.ListProjectAgents(userID, projectID)
			if err != nil {
				writeAgentError(w, err)
				return
			}
			out := make([]apiAgentJSON, 0, len(agents))
			for _, a := range agents {
				out = append(out, agentToJSON(a))
			}
			writeJSON(w, http.StatusOK, out)
		case http.MethodPost:
			var req apiAgentInput
			if err := decodeJSONBody(r, &req); err != nil {
				utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
				return
			}
			a, err := domain.CreateProjectAgent(userID, projectID, req.toDomain())
			if err != nil {
				writeAgentError(w, err)
				return
			}
			writeJSON(w, http.StatusCreated, agentToJSON(*a))
		default:
			methodNotAllowed(w)
		}
		return
	}

	// /agents/runs — run history across the project's agents.
	if rest[0] == "runs" && len(rest) == 1 {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		agentID, _ := strconv.Atoi(r.URL.Query().Get("agent_id"))
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		runs, err := domain.ListProjectAgentRuns(userID, projectID, agentID, limit)
		if err != nil {
			writeAgentError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, agentRunsToJSON(runs))
		return
	}

	agentID, err := strconv.Atoi(rest[0])
	if err != nil || agentID <= 0 {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid agent id.")
		return
	}
	if len(rest) == 1 {
		switch r.Method {
		case http.MethodGet:
			a, err := domain.GetProjectAgent(userID, projectID, agentID)
			if err != nil {
				writeAgentError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, agentToJSON(*a))
		case http.MethodPatch:
			var req apiAgentInput
			if err := decodeJSONBody(r, &req); err != nil {
				utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
				return
			}
			a, err := domain.UpdateProjectAgent(userID, projectID, agentID, req.toDomain())
			if err != nil {
				writeAgentError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, agentToJSON(*a))
		case http.MethodDelete:
			if err := domain.RemoveProjectAgent(userID, projectID, agentID); err != nil {
				writeAgentError(w, err)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			methodNotAllowed(w)
		}
		return
	}

	switch rest[1] {
	case "keys":
		apiV1AgentKeys(w, r, userID, projectID, agentID, rest[2:])
	case "webhook-secret":
		if len(rest) != 2 || r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		sec, err := domain.RotateAgentWebhookSecret(userID, projectID, agentID)
		if err != nil {
			writeAgentError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"secret": sec})
	case "test":
		if len(rest) != 2 || r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		if err := domain.TestAgentWebhook(userID, projectID, agentID); err != nil {
			writeAgentError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	case "runs":
		if len(rest) != 2 || r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		runs, err := domain.ListProjectAgentRuns(userID, projectID, agentID, limit)
		if err != nil {
			writeAgentError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, agentRunsToJSON(runs))
	default:
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Not found.")
	}
}

func apiV1AgentKeys(w http.ResponseWriter, r *http.Request, userID, projectID, agentID int, rest []string) {
	if len(rest) == 0 {
		switch r.Method {
		case http.MethodGet:
			keys, err := domain.ListAgentAPIKeys(userID, projectID, agentID)
			if err != nil {
				writeAgentError(w, err)
				return
			}
			out := make([]apiProjectKeyJSON, 0, len(keys))
			for _, k := range keys {
				out = append(out, projectKeyToJSON(k))
			}
			writeJSON(w, http.StatusOK, out)
		case http.MethodPost:
			if !utils.RedisAvailable() {
				utils.APIJSONError(w, http.StatusServiceUnavailable, "api_unavailable", "Redis is required for the REST API.")
				return
			}
			var req struct {
				Name      string  `json:"name"`
				ExpiresAt *string `json:"expires_at"`
			}
			if err := decodeJSONBody(r, &req); err != nil {
				utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
				return
			}
			var expires *time.Time
			if req.ExpiresAt != nil && strings.TrimSpace(*req.ExpiresAt) != "" {
				t, err := time.Parse(time.RFC3339, strings.TrimSpace(*req.ExpiresAt))
				if err != nil {
					utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "expires_at must be an RFC 3339 timestamp.")
					return
				}
				expires = &t
			}
			plaintext, rec, err := domain.CreateAgentAPIKey(userID, projectID, agentID, req.Name, expires)
			if err != nil {
				writeAgentError(w, err)
				return
			}
			writeJSON(w, http.StatusCreated, apiProjectKeyCreateResponse{
				apiProjectKeyJSON: projectKeyToJSON(*rec),
				Key:               plaintext,
			})
		default:
			methodNotAllowed(w)
		}
		return
	}
	keyID, err := strconv.Atoi(rest[0])
	if len(rest) != 1 || err != nil || keyID <= 0 {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid API key id.")
		return
	}
	if r.Method != http.MethodDelete {
		methodNotAllowed(w)
		return
	}
	if err := domain.RevokeAgentAPIKey(userID, projectID, agentID, keyID); err != nil {
		writeAgentError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type apiTaskAgentOption struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Handle string `json:"handle"`
}

// apiV1TaskAgentRuns handles /api/v2/tasks/{id}/agent-runs[/{runId}/cancel].
func apiV1TaskAgentRuns(w http.ResponseWriter, r *http.Request, taskID int, rest []string) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	if len(rest) == 0 {
		switch r.Method {
		case http.MethodGet:
			res, err := domain.ListTaskAgentRuns(userID, taskID)
			if err != nil {
				writeAgentError(w, err)
				return
			}
			agents := make([]apiTaskAgentOption, 0, len(res.Agents))
			for _, a := range res.Agents {
				agents = append(agents, apiTaskAgentOption{ID: a.ID, Name: a.Name, Handle: a.Handle})
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"runs":   agentRunsToJSON(res.Runs),
				"agents": agents,
			})
		case http.MethodPost:
			var req struct {
				AgentID int    `json:"agent_id"`
				Note    string `json:"note"`
			}
			if err := decodeJSONBody(r, &req); err != nil || req.AgentID <= 0 {
				utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "agent_id is required.")
				return
			}
			run, err := domain.DispatchAgentRun(userID, taskID, req.AgentID, req.Note)
			if err != nil {
				writeAgentError(w, err)
				return
			}
			writeJSON(w, http.StatusCreated, agentRunToJSON(*run))
		default:
			methodNotAllowed(w)
		}
		return
	}
	runID, err := strconv.Atoi(rest[0])
	if err != nil || runID <= 0 || len(rest) != 2 || rest[1] != "cancel" {
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Not found.")
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if run, err := storage.GetAgentRun(runID); err != nil || run.TaskID != taskID {
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Not found.")
		return
	}
	run, err := domain.CancelAgentRun(userID, runID)
	if err != nil {
		writeAgentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, agentRunToJSON(*run))
}

// agentPrincipal returns the calling agent account, or writes 403.
func agentPrincipal(w http.ResponseWriter, r *http.Request) (int, bool) {
	p, ok := utils.GetAPIKeyPrincipal(r)
	if !ok || !p.IsAgent {
		utils.APIJSONError(w, http.StatusForbidden, "forbidden", "This endpoint is only available to AI agent keys.")
		return 0, false
	}
	return p.UserID, true
}

// APIV1AgentRouter handles the agent's own endpoints:
//
//	GET  /api/v2/agent                      who am I, guardrails, statuses
//	GET  /api/v2/agent/runs                 my queue (?status=queued,running)
//	GET  /api/v2/agent/runs/{id}            one run
//	POST /api/v2/agent/runs/{id}/start      queued -> running
//	POST /api/v2/agent/runs/{id}/finish     -> succeeded | failed, with summary
func APIV1AgentRouter(w http.ResponseWriter, r *http.Request) {
	agentUserID, ok := agentPrincipal(w, r)
	if !ok {
		return
	}
	sub := utils.ParseAPIV1Subpath(r, "agent")
	parts := []string{}
	if sub != "" {
		parts = strings.Split(sub, "/")
	}
	switch {
	case len(parts) == 0:
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		ctx, err := domain.GetAgentSelf(agentUserID)
		if err != nil {
			writeAgentError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, ctx)
	case parts[0] == "runs" && len(parts) == 1:
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		var statuses []string
		if s := strings.TrimSpace(r.URL.Query().Get("status")); s != "" {
			for _, v := range strings.Split(s, ",") {
				if v = strings.TrimSpace(v); v != "" {
					statuses = append(statuses, v)
				}
			}
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		runs, err := domain.ListAgentQueue(agentUserID, statuses, limit)
		if err != nil {
			writeAgentError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, agentRunsToJSON(runs))
	case parts[0] == "runs" && len(parts) >= 2:
		runID, err := strconv.Atoi(parts[1])
		if err != nil || runID <= 0 {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid run id.")
			return
		}
		var run *storage.AgentRun
		switch {
		case len(parts) == 2 && r.Method == http.MethodGet:
			run, err = domain.GetAgentRunForAgent(agentUserID, runID)
		case len(parts) == 3 && parts[2] == "start" && r.Method == http.MethodPost:
			run, err = domain.StartAgentRun(agentUserID, runID)
		case len(parts) == 3 && parts[2] == "finish" && r.Method == http.MethodPost:
			var req struct {
				Status  string `json:"status"`
				Summary string `json:"summary"`
			}
			if derr := decodeJSONBody(r, &req); derr != nil {
				utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
				return
			}
			run, err = domain.FinishAgentRun(agentUserID, runID, req.Status, req.Summary)
		default:
			utils.APIJSONError(w, http.StatusNotFound, "not_found", "Not found.")
			return
		}
		if err != nil {
			writeAgentError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, agentRunToJSON(*run))
	default:
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Not found.")
	}
}
