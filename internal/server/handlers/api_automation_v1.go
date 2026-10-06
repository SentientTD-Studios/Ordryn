package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"GoTodo/internal/domain"
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
)

type apiAutomationRuleJSON struct {
	ID                int             `json:"id"`
	ProjectID         int             `json:"project_id"`
	Name              string          `json:"name"`
	Enabled           bool            `json:"enabled"`
	Position          int             `json:"position"`
	TriggerType       string          `json:"trigger_type"`
	TriggerConfig     json.RawMessage `json:"trigger_config"`
	Conditions        json.RawMessage `json:"conditions"`
	Actions           json.RawMessage `json:"actions"`
	RecipeID          string          `json:"recipe_id"`
	CreatedByID       int             `json:"created_by_id"`
	UpdatedByID       int             `json:"updated_by_id"`
	CreatedAt         string          `json:"created_at"`
	UpdatedAt         string          `json:"updated_at"`
	RunCount          int             `json:"run_count"`
	ErrorCount        int             `json:"error_count"`
	ConsecutiveErrors int             `json:"consecutive_errors"`
	LastRunAt         *string         `json:"last_run_at"`
	LastError         string          `json:"last_error"`
	PausedReason      string          `json:"paused_reason"`
}

// apiAutomationRuleInput is the create/PATCH body. Omitted fields keep their value.
type apiAutomationRuleInput struct {
	Name          *string                         `json:"name"`
	Enabled       *bool                           `json:"enabled"`
	TriggerType   *string                         `json:"trigger_type"`
	TriggerConfig *domain.AutomationTriggerConfig `json:"trigger_config"`
	Conditions    *domain.AutomationConditions    `json:"conditions"`
	Actions       *[]domain.AutomationAction      `json:"actions"`
	RecipeID      string                          `json:"recipe_id"`
}

func (in apiAutomationRuleInput) toDomain() domain.AutomationRuleInput {
	return domain.AutomationRuleInput{
		Name:          in.Name,
		Enabled:       in.Enabled,
		TriggerType:   in.TriggerType,
		TriggerConfig: in.TriggerConfig,
		Conditions:    in.Conditions,
		Actions:       in.Actions,
		RecipeID:      in.RecipeID,
	}
}

type apiAutomationRunJSON struct {
	ID        int64           `json:"id"`
	RuleID    int             `json:"rule_id"`
	RuleName  string          `json:"rule_name"`
	ProjectID int             `json:"project_id"`
	TaskID    int             `json:"task_id"`
	TaskTitle string          `json:"task_title"`
	Trigger   string          `json:"trigger"`
	Outcome   string          `json:"outcome"`
	Changes   json.RawMessage `json:"changes"`
	Error     string          `json:"error"`
	CreatedAt string          `json:"created_at"`
}

func automationRuleToJSON(r storage.AutomationRule) apiAutomationRuleJSON {
	return apiAutomationRuleJSON{
		ID:                r.ID,
		ProjectID:         r.ProjectID,
		Name:              r.Name,
		Enabled:           r.Enabled,
		Position:          r.Position,
		TriggerType:       r.TriggerType,
		TriggerConfig:     rawOr(r.TriggerConfig, `{}`),
		Conditions:        rawOr(r.Conditions, `{}`),
		Actions:           rawOr(r.Actions, `[]`),
		RecipeID:          r.RecipeID,
		CreatedByID:       r.CreatedBy,
		UpdatedByID:       r.UpdatedBy,
		CreatedAt:         r.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:         r.UpdatedAt.UTC().Format(time.RFC3339),
		RunCount:          r.RunCount,
		ErrorCount:        r.ErrorCount,
		ConsecutiveErrors: r.ConsecutiveErrors,
		LastRunAt:         optionalRFC3339(r.LastRunAt),
		LastError:         r.LastError,
		PausedReason:      r.PausedReason,
	}
}

func automationRulesToJSON(rules []storage.AutomationRule) []apiAutomationRuleJSON {
	out := make([]apiAutomationRuleJSON, 0, len(rules))
	for _, r := range rules {
		out = append(out, automationRuleToJSON(r))
	}
	return out
}

func automationRunsToJSON(runs []storage.AutomationRuleRun) []apiAutomationRunJSON {
	out := make([]apiAutomationRunJSON, 0, len(runs))
	for _, r := range runs {
		out = append(out, apiAutomationRunJSON{
			ID:        r.ID,
			RuleID:    r.RuleID,
			RuleName:  r.RuleName,
			ProjectID: r.ProjectID,
			TaskID:    r.TaskID,
			TaskTitle: r.TaskTitle,
			Trigger:   r.Trigger,
			Outcome:   r.Outcome,
			Changes:   rawOr(r.Changes, `[]`),
			Error:     r.Error,
			CreatedAt: r.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	return out
}

func rawOr(raw json.RawMessage, fallback string) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(fallback)
	}
	return raw
}

func writeAutomationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Not found.")
	case errors.Is(err, domain.ErrForbidden):
		utils.APIJSONError(w, http.StatusForbidden, "forbidden", sharingClientMessage(err, "Only project managers can manage automation rules."))
	case errors.Is(err, domain.ErrConflict):
		utils.APIJSONError(w, http.StatusConflict, "conflict", sharingClientMessage(err, "Conflict."))
	case errors.Is(err, domain.ErrValidation):
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", sharingClientMessage(err, "Invalid request."))
	default:
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Request failed.")
	}
}

// apiV1ProjectAutomations handles /api/v2/projects/{id}/automations/...
func apiV1ProjectAutomations(w http.ResponseWriter, r *http.Request, projectID int, rest []string) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	if len(rest) == 0 {
		switch r.Method {
		case http.MethodGet:
			rules, err := domain.ListAutomationRules(userID, projectID)
			if err != nil {
				writeAutomationError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, automationRulesToJSON(rules))
		case http.MethodPost:
			var req apiAutomationRuleInput
			if err := decodeJSONBody(r, &req); err != nil {
				utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
				return
			}
			rule, err := domain.CreateAutomationRule(userID, projectID, req.toDomain())
			if err != nil {
				writeAutomationError(w, err)
				return
			}
			writeJSON(w, http.StatusCreated, automationRuleToJSON(*rule))
		default:
			methodNotAllowed(w)
		}
		return
	}

	switch rest[0] {
	case "runs":
		if len(rest) != 1 || r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		q := r.URL.Query()
		ruleID, _ := strconv.Atoi(q.Get("rule_id"))
		taskID, _ := strconv.Atoi(q.Get("task_id"))
		limit, _ := strconv.Atoi(q.Get("limit"))
		runs, err := domain.ListAutomationRuns(userID, projectID, ruleID, taskID, limit)
		if err != nil {
			writeAutomationError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, automationRunsToJSON(runs))
		return
	case "preview":
		if len(rest) != 1 || r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		var req apiAutomationRuleInput
		if err := decodeJSONBody(r, &req); err != nil {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
			return
		}
		tasks, err := domain.PreviewAutomationRule(userID, projectID, req.toDomain())
		if err != nil {
			writeAutomationError(w, err)
			return
		}
		out := make([]map[string]interface{}, 0, len(tasks))
		for _, t := range tasks {
			out = append(out, map[string]interface{}{"id": t.ID, "title": t.Title})
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"tasks": out})
		return
	case "reorder":
		if len(rest) != 1 || r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		var req struct {
			IDs []int `json:"ids"`
		}
		if err := decodeJSONBody(r, &req); err != nil {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
			return
		}
		rules, err := domain.ReorderAutomationRules(userID, projectID, req.IDs)
		if err != nil {
			writeAutomationError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, automationRulesToJSON(rules))
		return
	}

	ruleID, err := strconv.Atoi(rest[0])
	if err != nil || ruleID <= 0 {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid rule id.")
		return
	}
	if len(rest) == 2 && rest[1] == "runs" {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		runs, err := domain.ListAutomationRuns(userID, projectID, ruleID, 0, limit)
		if err != nil {
			writeAutomationError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, automationRunsToJSON(runs))
		return
	}
	if len(rest) != 1 {
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Not found.")
		return
	}
	switch r.Method {
	case http.MethodGet:
		rule, err := domain.GetAutomationRule(userID, projectID, ruleID)
		if err != nil {
			writeAutomationError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, automationRuleToJSON(*rule))
	case http.MethodPatch:
		var req apiAutomationRuleInput
		if err := decodeJSONBody(r, &req); err != nil {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
			return
		}
		rule, err := domain.UpdateAutomationRule(userID, projectID, ruleID, req.toDomain())
		if err != nil {
			writeAutomationError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, automationRuleToJSON(*rule))
	case http.MethodDelete:
		if err := domain.DeleteAutomationRule(userID, projectID, ruleID); err != nil {
			writeAutomationError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		methodNotAllowed(w)
	}
}
