package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"GoTodo/internal/domain"
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
)

type apiCeremonySettingsJSON struct {
	Kanban                 bool                   `json:"kanban"`
	Site                   map[string]bool        `json:"site"`
	Project                map[string]bool        `json:"project"`
	Active                 map[string]bool        `json:"active"`
	HoursPerPoint          float64                `json:"hours_per_point"`
	NewTasksNeedRefinement bool                   `json:"new_tasks_need_refinement"`
	CanManage              bool                   `json:"can_manage"`
	CanFacilitate          map[string]bool        `json:"can_facilitate"`
	Retro                  domain.RetroConfig     `json:"retro"`
	Standup                domain.StandupConfig   `json:"standup"`
	RetroTemplates         []apiRetroTemplateJSON `json:"retro_templates"`
}

type apiRetroTemplateJSON struct {
	ID      string               `json:"id"`
	Columns []domain.RetroColumn `json:"columns"`
}

type apiCeremonySettingsPatch struct {
	Ceremonies             map[string]bool       `json:"ceremonies"`
	HoursPerPoint          *float64              `json:"hours_per_point"`
	NewTasksNeedRefinement *bool                 `json:"new_tasks_need_refinement"`
	Retro                  *domain.RetroConfig   `json:"retro"`
	Standup                *domain.StandupConfig `json:"standup"`
}

type apiRefinementTaskJSON struct {
	ID            int      `json:"id"`
	ParentID      *int     `json:"parent_id"`
	ParentTitle   string   `json:"parent_title,omitempty"`
	Title         string   `json:"title"`
	Priority      int      `json:"priority"`
	Completed     bool     `json:"completed"`
	StatusName    string   `json:"status_name,omitempty"`
	SprintID      *int     `json:"sprint_id"`
	SprintName    string   `json:"sprint_name,omitempty"`
	ClaimedByName string   `json:"claimed_by_name,omitempty"`
	EstimateHours *float64 `json:"estimate_hours"`
	Refinement    string   `json:"refinement"`
	Deleted       bool     `json:"deleted"`
}

type apiCeremonySessionMetaJSON struct {
	ID              int     `json:"id"`
	ProjectID       int     `json:"project_id"`
	Kind            string  `json:"kind"`
	Title           string  `json:"title"`
	Status          string  `json:"status"`
	FacilitatorID   *int    `json:"facilitator_id"`
	FacilitatorName string  `json:"facilitator_name,omitempty"`
	SprintID        *int    `json:"sprint_id"`
	SprintName      string  `json:"sprint_name,omitempty"`
	Revision        int     `json:"revision"`
	CreatedAt       string  `json:"created_at"`
	UpdatedAt       string  `json:"updated_at"`
	EndedAt         *string `json:"ended_at"`
}

type apiRefinementSessionJSON struct {
	apiCeremonySessionMetaJSON
	Queue         []int                   `json:"queue"`
	Current       int                     `json:"current"`
	CurrentTaskID *int                    `json:"current_task_id"`
	Outcomes      map[string]string       `json:"outcomes"`
	Tasks         []apiRefinementTaskJSON `json:"tasks"`
	CanFacilitate bool                    `json:"can_facilitate"`
	IsFacilitator bool                    `json:"is_facilitator"`
}

type apiRefinementSessionSummaryJSON struct {
	apiCeremonySessionMetaJSON
	Queued  int `json:"queued"`
	Refined int `json:"refined"`
	Skipped int `json:"skipped"`
}

type apiRefinementStartRequest struct {
	Title            string `json:"title"`
	SprintID         *int   `json:"sprint_id"`
	Backlog          bool   `json:"backlog"`
	IncludeUnflagged bool   `json:"include_unflagged"`
	TaskIDs          []int  `json:"task_ids"`
}

type apiRefinementActionRequest struct {
	Action   string `json:"action"`
	Revision int    `json:"revision"`
	TaskID   int    `json:"task_id"`
	TaskIDs  []int  `json:"task_ids"`
	Index    int    `json:"index"`
}

func positiveIntPtr(v int) *int {
	if v <= 0 {
		return nil
	}
	return &v
}

func ceremonySettingsJSON(s *domain.CeremonySettings) apiCeremonySettingsJSON {
	active := make(map[string]bool, len(storage.CeremonyKinds))
	for _, k := range storage.CeremonyKinds {
		active[k] = s.Active(k)
	}
	return apiCeremonySettingsJSON{
		Kanban: s.Kanban, Site: s.Site, Project: s.Project, Active: active,
		HoursPerPoint: s.HoursPerPoint, NewTasksNeedRefinement: s.NewTasksNeedRefinement,
		CanManage: s.CanManage, CanFacilitate: s.CanFacilitate,
		Retro: s.Retro, Standup: s.Standup, RetroTemplates: retroTemplatesJSON(),
	}
}

func retroTemplatesJSON() []apiRetroTemplateJSON {
	out := make([]apiRetroTemplateJSON, 0, len(domain.RetroTemplateIDs))
	for _, id := range domain.RetroTemplateIDs {
		out = append(out, apiRetroTemplateJSON{ID: id, Columns: domain.RetroTemplateColumns(id)})
	}
	return out
}

func ceremonySessionMetaJSON(s storage.CeremonySession) apiCeremonySessionMetaJSON {
	out := apiCeremonySessionMetaJSON{
		ID: s.ID, ProjectID: s.ProjectID, Kind: s.Kind, Title: s.Title, Status: s.Status,
		FacilitatorID: positiveIntPtr(s.FacilitatorID), FacilitatorName: s.FacilitatorName,
		SprintID: positiveIntPtr(s.SprintID), SprintName: s.SprintName, Revision: s.Revision,
		CreatedAt: s.CreatedAt.UTC().Format(time.RFC3339), UpdatedAt: s.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if s.EndedAt != nil {
		v := s.EndedAt.UTC().Format(time.RFC3339)
		out.EndedAt = &v
	}
	return out
}

func refinementSessionJSON(s *domain.RefinementSession) apiRefinementSessionJSON {
	outcomes := make(map[string]string, len(s.State.Outcomes))
	for id, o := range s.State.Outcomes {
		outcomes[strconv.Itoa(id)] = o
	}
	tasks := make([]apiRefinementTaskJSON, 0, len(s.Tasks))
	for _, t := range s.Tasks {
		tasks = append(tasks, apiRefinementTaskJSON{
			ID: t.ID, ParentID: positiveIntPtr(t.ParentID), ParentTitle: t.ParentTitle, Title: t.Title,
			Priority: t.Priority, Completed: t.Completed, StatusName: t.StatusName,
			SprintID: positiveIntPtr(t.SprintID), SprintName: t.SprintName, ClaimedByName: t.ClaimedByName,
			EstimateHours: t.EstimateHours, Refinement: t.Refinement, Deleted: t.Trashed,
		})
	}
	return apiRefinementSessionJSON{
		apiCeremonySessionMetaJSON: ceremonySessionMetaJSON(s.CeremonySession),
		Queue:                      s.State.Queue,
		Current:                    s.State.Current,
		CurrentTaskID:              positiveIntPtr(s.State.CurrentTaskID()),
		Outcomes:                   outcomes,
		Tasks:                      tasks,
		CanFacilitate:              s.CanFacilitate,
		IsFacilitator:              s.IsFacilitator,
	}
}

func writeCeremonyError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Not found.")
	case errors.Is(err, domain.ErrForbidden):
		utils.APIJSONError(w, http.StatusForbidden, "forbidden", sharingClientMessage(err, "You cannot do that in this project."))
	case errors.Is(err, domain.ErrConflict):
		utils.APIJSONError(w, http.StatusConflict, "conflict", sharingClientMessage(err, "Conflict."))
	case errors.Is(err, domain.ErrValidation):
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", sharingClientMessage(err, "Invalid request."))
	default:
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Request failed.")
	}
}

// apiV1ProjectCeremonies handles /api/v2/projects/{id}/ceremonies...
func apiV1ProjectCeremonies(w http.ResponseWriter, r *http.Request, projectID int, rest []string) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	if len(rest) == 0 {
		switch r.Method {
		case http.MethodGet:
			s, err := domain.GetCeremonySettings(r.Context(), userID, projectID)
			if err != nil {
				writeCeremonyError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, ceremonySettingsJSON(s))
		case http.MethodPatch:
			var req apiCeremonySettingsPatch
			if err := decodeJSONBody(r, &req); err != nil {
				utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
				return
			}
			s, err := domain.UpdateCeremonySettings(r.Context(), userID, projectID, domain.UpdateCeremonySettingsInput{
				Ceremonies: req.Ceremonies, HoursPerPoint: req.HoursPerPoint, NewTasksNeedRefinement: req.NewTasksNeedRefinement,
				Retro: req.Retro, Standup: req.Standup,
			})
			if err != nil {
				writeCeremonyError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, ceremonySettingsJSON(s))
		default:
			methodNotAllowed(w)
		}
		return
	}
	if len(rest) < 2 || rest[1] != "sessions" {
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Not found.")
		return
	}
	switch rest[0] {
	case storage.CeremonyRefinement:
		apiV1RefinementSessions(w, r, userID, projectID, rest[2:])
	case storage.CeremonyRetro:
		apiV1RetroSessions(w, r, userID, projectID, rest[2:])
	case storage.CeremonyStandup:
		apiV1StandupSessions(w, r, userID, projectID, rest[2:])
	default:
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Not found.")
	}
}

// apiV1RefinementSessions handles /api/v2/projects/{id}/ceremonies/refinement/sessions...
func apiV1RefinementSessions(w http.ResponseWriter, r *http.Request, userID, projectID int, rest []string) {
	ctx := r.Context()
	if len(rest) == 0 {
		switch r.Method {
		case http.MethodGet:
			limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
			rows, err := domain.ListRefinementSessions(ctx, userID, projectID, limit)
			if err != nil {
				writeCeremonyError(w, err)
				return
			}
			out := make([]apiRefinementSessionSummaryJSON, 0, len(rows))
			for _, s := range rows {
				out = append(out, apiRefinementSessionSummaryJSON{
					apiCeremonySessionMetaJSON: ceremonySessionMetaJSON(s.CeremonySession),
					Queued:                     s.Queued, Refined: s.Refined, Skipped: s.Skipped,
				})
			}
			writeJSON(w, http.StatusOK, out)
		case http.MethodPost:
			var req apiRefinementStartRequest
			if err := decodeJSONBody(r, &req); err != nil {
				utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
				return
			}
			sprint := req.SprintID
			if req.Backlog {
				zero := 0
				sprint = &zero
			}
			s, err := domain.StartRefinementSession(ctx, userID, projectID, domain.CreateRefinementSessionInput{
				Title: req.Title, SprintID: sprint, IncludeUnflagged: req.IncludeUnflagged, TaskIDs: req.TaskIDs,
			})
			if err != nil {
				writeCeremonyError(w, err)
				return
			}
			writeJSON(w, http.StatusCreated, refinementSessionJSON(s))
		default:
			methodNotAllowed(w)
		}
		return
	}

	if rest[0] == "active" && len(rest) == 1 {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		s, err := domain.GetActiveRefinementSession(ctx, userID, projectID)
		if err != nil {
			writeCeremonyError(w, err)
			return
		}
		if s == nil {
			writeJSON(w, http.StatusOK, map[string]any{"session": nil})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"session": refinementSessionJSON(s)})
		return
	}

	sessionID, err := strconv.Atoi(rest[0])
	if err != nil || sessionID <= 0 {
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Not found.")
		return
	}
	switch {
	case len(rest) == 1 && r.Method == http.MethodGet:
		s, err := domain.GetRefinementSession(ctx, userID, projectID, sessionID)
		if err != nil {
			writeCeremonyError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, refinementSessionJSON(s))
	case len(rest) == 2 && rest[1] == "actions" && r.Method == http.MethodPost:
		var req apiRefinementActionRequest
		if err := decodeJSONBody(r, &req); err != nil {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
			return
		}
		s, err := domain.ApplyRefinementAction(ctx, userID, projectID, sessionID, domain.RefinementActionInput{
			Action: req.Action, Revision: req.Revision, TaskID: req.TaskID, TaskIDs: req.TaskIDs, Index: req.Index,
		})
		if err != nil {
			writeCeremonyError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, refinementSessionJSON(s))
	case len(rest) <= 2:
		methodNotAllowed(w)
	default:
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Not found.")
	}
}
