package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"GoTodo/internal/domain"
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
)

type apiRequestFormJSON struct {
	ID          int                        `json:"id"`
	ProjectID   int                        `json:"project_id"`
	Token       string                     `json:"token"`
	Path        string                     `json:"path"`
	Title       string                     `json:"title"`
	Intro       string                     `json:"intro"`
	Fields      []storage.RequestFormField `json:"fields"`
	Priority    int                        `json:"priority"`
	TagIDs      []int                      `json:"tag_ids"`
	Enabled     bool                       `json:"enabled"`
	OwnerID     *int                       `json:"owner_id"`
	Submissions int                        `json:"submissions"`
	CreatedAt   string                     `json:"created_at"`
	UpdatedAt   string                     `json:"updated_at"`
}

type apiRequestFormInput struct {
	Title    string                     `json:"title"`
	Intro    string                     `json:"intro"`
	Fields   []storage.RequestFormField `json:"fields"`
	Priority int                        `json:"priority"`
	TagIDs   []int                      `json:"tag_ids"`
	Enabled  *bool                      `json:"enabled"`
}

func (in apiRequestFormInput) toDomain() domain.RequestFormInput {
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	return domain.RequestFormInput{
		Title: in.Title, Intro: in.Intro, Fields: in.Fields, Priority: in.Priority, TagIDs: in.TagIDs, Enabled: enabled,
	}
}

func requestFormToJSON(f *storage.RequestForm) apiRequestFormJSON {
	out := apiRequestFormJSON{
		ID: f.ID, ProjectID: f.ProjectID, Token: f.Token, Path: "/f/" + f.Token,
		Title: f.Title, Intro: f.Intro, Fields: f.Fields, Priority: f.Priority, TagIDs: f.TagIDs,
		Enabled: f.Enabled, Submissions: f.Submissions,
		CreatedAt: f.CreatedAt.UTC().Format(time.RFC3339), UpdatedAt: f.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if f.OwnerID > 0 {
		id := f.OwnerID
		out.OwnerID = &id
	}
	if out.Fields == nil {
		out.Fields = []storage.RequestFormField{}
	}
	if out.TagIDs == nil {
		out.TagIDs = []int{}
	}
	return out
}

// apiV1ProjectRequestForms handles /api/v2/projects/{id}/forms[/{formId}[/rotate]].
func apiV1ProjectRequestForms(w http.ResponseWriter, r *http.Request, projectID int, rest []string) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	ctx := r.Context()
	writeForm := func(status int, f *storage.RequestForm) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(requestFormToJSON(f))
	}

	if len(rest) == 0 {
		switch r.Method {
		case http.MethodGet:
			forms, err := domain.ListRequestFormsForManager(ctx, userID, projectID)
			if err != nil {
				writeWorkflowDomainError(w, err)
				return
			}
			out := make([]apiRequestFormJSON, 0, len(forms))
			for i := range forms {
				out = append(out, requestFormToJSON(&forms[i]))
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_ = json.NewEncoder(w).Encode(out)
		case http.MethodPost:
			var in apiRequestFormInput
			if err := decodeJSONBody(r, &in); err != nil {
				utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
				return
			}
			f, err := domain.CreateRequestForm(ctx, userID, projectID, in.toDomain())
			if err != nil {
				writeWorkflowDomainError(w, err)
				return
			}
			writeForm(http.StatusCreated, f)
		default:
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		}
		return
	}

	formID, err := strconv.Atoi(rest[0])
	if err != nil || formID <= 0 || len(rest) > 2 {
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Form not found.")
		return
	}
	if len(rest) == 2 {
		if rest[1] != "rotate" || r.Method != http.MethodPost {
			utils.APIJSONError(w, http.StatusNotFound, "not_found", "Not found.")
			return
		}
		f, err := domain.RotateRequestFormLink(ctx, userID, projectID, formID)
		if err != nil {
			writeWorkflowDomainError(w, err)
			return
		}
		writeForm(http.StatusOK, f)
		return
	}
	switch r.Method {
	case http.MethodPut:
		var in apiRequestFormInput
		if err := decodeJSONBody(r, &in); err != nil {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
			return
		}
		f, err := domain.UpdateRequestForm(ctx, userID, projectID, formID, in.toDomain())
		if err != nil {
			writeWorkflowDomainError(w, err)
			return
		}
		writeForm(http.StatusOK, f)
	case http.MethodDelete:
		if err := domain.DeleteRequestFormForUser(ctx, userID, projectID, formID); err != nil {
			writeWorkflowDomainError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
	}
}

type apiPublicRequestFormJSON struct {
	Token       string                     `json:"token"`
	Title       string                     `json:"title"`
	Intro       string                     `json:"intro"`
	ProjectName string                     `json:"project_name"`
	Fields      []storage.RequestFormField `json:"fields"`
}

type apiRequestSubmitInput struct {
	Summary string            `json:"summary"`
	Answers map[string]string `json:"answers"`
}

// APIV1RequestForms handles GET /api/v2/forms/{token} and POST /api/v2/forms/{token}/submit.
func APIV1RequestForms(w http.ResponseWriter, r *http.Request) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v2/forms"), "/")
	parts := strings.Split(path, "/")
	if path == "" || len(parts) > 2 {
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Form not found.")
		return
	}
	token := parts[0]
	if len(parts) == 1 {
		if r.Method != http.MethodGet {
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
			return
		}
		f, err := domain.GetRequestFormForSubmitter(r.Context(), userID, token)
		if err != nil {
			writeWorkflowDomainError(w, err)
			return
		}
		fields := f.Fields
		if fields == nil {
			fields = []storage.RequestFormField{}
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(apiPublicRequestFormJSON{
			Token: f.Token, Title: f.Title, Intro: f.Intro, ProjectName: f.ProjectName, Fields: fields,
		})
		return
	}
	if parts[1] != "submit" {
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Not found.")
		return
	}
	if r.Method != http.MethodPost {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	var in apiRequestSubmitInput
	if err := decodeJSONBody(r, &in); err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
		return
	}
	taskID, err := domain.SubmitRequestForm(r.Context(), userID, token, in.Summary, in.Answers)
	if err != nil {
		writeWorkflowDomainError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]int{"task_id": taskID})
}
