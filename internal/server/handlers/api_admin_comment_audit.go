package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"GoTodo/internal/domain"
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
)

type apiCommentAuditListResponse struct {
	Items  []apiTaskCommentRevisionJSON `json:"items"`
	Total  int                          `json:"total"`
	Limit  int                          `json:"limit"`
	Offset int                          `json:"offset"`
}

// APIV1AdminCommentAuditRouter handles GET /api/v2/admin/comment-audit and restore.
func APIV1AdminCommentAuditRouter(w http.ResponseWriter, r *http.Request) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}

	sub := utils.ParseAPIV1Subpath(r, "admin/comment-audit")
	if sub == "" {
		if r.Method != http.MethodGet {
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
			return
		}
		filter, errMsg := parseCommentAuditListQuery(r)
		if errMsg != "" {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", errMsg)
			return
		}
		items, total, err := domain.ListCommentAuditForAdmin(r.Context(), userID, filter)
		if err != nil {
			writeWorkflowDomainError(w, err)
			return
		}
		out := make([]apiTaskCommentRevisionJSON, 0, len(items))
		for _, row := range items {
			out = append(out, commentRevisionToAPIJSON(row))
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(apiCommentAuditListResponse{
			Items:  out,
			Total:  total,
			Limit:  filter.Limit,
			Offset: filter.Offset,
		})
		return
	}

	parts := strings.Split(sub, "/")
	if len(parts) != 2 || parts[1] != "restore" {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid comment audit path.")
		return
	}
	revisionID, err := strconv.Atoi(parts[0])
	if err != nil || revisionID <= 0 {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid revision id.")
		return
	}
	if r.Method != http.MethodPost {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	c, err := domain.RestoreCommentRevisionForAdmin(r.Context(), userID, revisionID)
	if err != nil {
		writeWorkflowDomainError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(commentToAPIJSON(*c))
}

func parseCommentAuditListQuery(r *http.Request) (storage.CommentAuditFilter, string) {
	q := r.URL.Query()
	f := storage.CommentAuditFilter{
		Query: strings.TrimSpace(q.Get("q")),
		Kind:  strings.TrimSpace(q.Get("kind")),
	}
	if f.Kind != "" && f.Kind != storage.CommentRevisionKindEdit &&
		f.Kind != storage.CommentRevisionKindDelete && f.Kind != storage.CommentRevisionKindRestore {
		return f, "kind must be edit, delete, or restore."
	}
	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		n, convErr := strconv.Atoi(raw)
		if convErr != nil || n <= 0 {
			return f, "limit must be a positive integer."
		}
		f.Limit = n
	}
	if f.Limit <= 0 {
		f.Limit = 50
	}
	if f.Limit > 100 {
		f.Limit = 100
	}
	if raw := strings.TrimSpace(q.Get("offset")); raw != "" {
		n, convErr := strconv.Atoi(raw)
		if convErr != nil || n < 0 {
			return f, "offset must be a non-negative integer."
		}
		f.Offset = n
	}
	return f, ""
}
