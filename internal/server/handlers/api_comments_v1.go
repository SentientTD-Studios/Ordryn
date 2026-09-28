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

type apiTaskCommentLinkJSON struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
}

type apiTaskCommentJSON struct {
	ID               int                      `json:"id"`
	TaskID           int                      `json:"task_id"`
	UserID           int                      `json:"user_id"`
	UserName         string                   `json:"user_name,omitempty"`
	AvatarURL        string                   `json:"avatar_url,omitempty"`
	Body             string                   `json:"body"`
	CreatedAt        string                   `json:"created_at"`
	Deleted          bool                     `json:"deleted"`
	DeletedAt        *string                  `json:"deleted_at,omitempty"`
	DeletedByUserID  int                      `json:"deleted_by_user_id,omitempty"`
	DeletedByKind    string                   `json:"deleted_by_kind,omitempty"`
	EditedAt         *string                  `json:"edited_at,omitempty"`
	EditedByUserID   int                      `json:"edited_by_user_id,omitempty"`
	EditedByUserName string                   `json:"edited_by_user_name,omitempty"`
	AuthorRole       string                   `json:"author_role,omitempty"`
	AuthorRoleName   string                   `json:"author_role_name,omitempty"`
	Links            []apiTaskCommentLinkJSON `json:"links,omitempty"`
}

type apiTaskCommentCreateRequest struct {
	Body string `json:"body"`
}

type apiTaskCommentRestoreRequest struct {
	RevisionID int `json:"revision_id"`
}

type apiTaskCommentRevisionJSON struct {
	ID               int    `json:"id"`
	CommentID        int    `json:"comment_id"`
	TaskID           int    `json:"task_id"`
	Body             string `json:"body"`
	Kind             string `json:"kind"`
	CreatedAt        string `json:"created_at"`
	EditedByUserID   int    `json:"edited_by_user_id,omitempty"`
	EditedByUserName string `json:"edited_by_user_name,omitempty"`
	AuthorUserID     int    `json:"author_user_id,omitempty"`
	AuthorUserName   string `json:"author_user_name,omitempty"`
	TaskTitle        string `json:"task_title,omitempty"`
	ProjectID        int    `json:"project_id,omitempty"`
	ProjectName      string `json:"project_name,omitempty"`
	CommentDeleted   bool   `json:"comment_deleted"`
	CurrentBody      string `json:"current_body,omitempty"`
}

func commentToAPIJSON(c storage.TaskComment) apiTaskCommentJSON {
	out := apiTaskCommentJSON{
		ID:               c.ID,
		TaskID:           c.TaskID,
		UserID:           c.UserID,
		UserName:         c.UserName,
		AvatarURL:        c.AvatarURL,
		Body:             c.Body,
		CreatedAt:        c.CreatedAt.UTC().Format(time.RFC3339),
		EditedByUserID:   c.EditedByUserID,
		EditedByUserName: c.EditedByUserName,
		AuthorRole:       c.AuthorRole,
		AuthorRoleName:   c.AuthorRoleName,
	}
	if c.EditedAt != nil {
		s := c.EditedAt.UTC().Format(time.RFC3339)
		out.EditedAt = &s
	}
	if c.DeletedAt != nil {
		out.Deleted = true
		out.Body = ""
		s := c.DeletedAt.UTC().Format(time.RFC3339)
		out.DeletedAt = &s
		out.DeletedByUserID = c.DeletedByUserID
		out.DeletedByKind = c.DeletedByKind
	}
	if len(c.Links) > 0 {
		out.Links = make([]apiTaskCommentLinkJSON, 0, len(c.Links))
		for _, l := range c.Links {
			out.Links = append(out.Links, apiTaskCommentLinkJSON{ID: l.ID, Title: l.Title})
		}
	}
	return out
}

func commentRevisionToAPIJSON(rev storage.TaskCommentRevision) apiTaskCommentRevisionJSON {
	return apiTaskCommentRevisionJSON{
		ID:               rev.ID,
		CommentID:        rev.CommentID,
		TaskID:           rev.TaskID,
		Body:             rev.Body,
		Kind:             rev.Kind,
		CreatedAt:        rev.CreatedAt.UTC().Format(time.RFC3339),
		EditedByUserID:   rev.EditedByUserID,
		EditedByUserName: rev.EditedByUserName,
		AuthorUserID:     rev.AuthorUserID,
		AuthorUserName:   rev.AuthorUserName,
		TaskTitle:        rev.TaskTitle,
		ProjectID:        rev.ProjectID,
		ProjectName:      rev.ProjectName,
		CommentDeleted:   rev.CommentDeleted,
		CurrentBody:      rev.CurrentBody,
	}
}

func handleTaskComments(w http.ResponseWriter, r *http.Request, taskID int, rest []string) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}

	if len(rest) == 0 {
		switch r.Method {
		case http.MethodGet:
			comments, err := domain.ListCommentsForUser(r.Context(), userID, taskID)
			if err != nil {
				writeWorkflowDomainError(w, err)
				return
			}
			out := make([]apiTaskCommentJSON, 0, len(comments))
			for _, c := range comments {
				out = append(out, commentToAPIJSON(c))
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_ = json.NewEncoder(w).Encode(out)
		case http.MethodPost:
			var req apiTaskCommentCreateRequest
			if err := decodeJSONBody(r, &req); err != nil {
				utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
				return
			}
			c, err := domain.AddCommentForUser(r.Context(), userID, taskID, req.Body)
			if err != nil {
				writeWorkflowDomainError(w, err)
				return
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(commentToAPIJSON(*c))
		default:
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		}
		return
	}

	commentID, err := strconv.Atoi(rest[0])
	if err != nil || commentID <= 0 {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid comment id.")
		return
	}

	if len(rest) == 1 {
		switch r.Method {
		case http.MethodPatch:
			var req apiTaskCommentCreateRequest
			if err := decodeJSONBody(r, &req); err != nil {
				utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
				return
			}
			c, err := domain.EditCommentForUser(r.Context(), userID, taskID, commentID, req.Body)
			if err != nil {
				writeWorkflowDomainError(w, err)
				return
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_ = json.NewEncoder(w).Encode(commentToAPIJSON(*c))
		case http.MethodDelete:
			if err := domain.DeleteCommentForUser(r.Context(), userID, taskID, commentID); err != nil {
				writeWorkflowDomainError(w, err)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		}
		return
	}

	action := strings.TrimSpace(rest[1])
	if len(rest) == 2 && action == "revisions" {
		if r.Method != http.MethodGet {
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
			return
		}
		revs, err := domain.ListCommentRevisionsForUser(r.Context(), userID, taskID, commentID)
		if err != nil {
			writeWorkflowDomainError(w, err)
			return
		}
		out := make([]apiTaskCommentRevisionJSON, 0, len(revs))
		for _, rev := range revs {
			out = append(out, commentRevisionToAPIJSON(rev))
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(out)
		return
	}
	if len(rest) == 2 && action == "restore" {
		if r.Method != http.MethodPost {
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
			return
		}
		var req apiTaskCommentRestoreRequest
		if err := decodeJSONBody(r, &req); err != nil {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
			return
		}
		if req.RevisionID <= 0 {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid revision id.")
			return
		}
		c, err := domain.RestoreCommentRevisionForUser(r.Context(), userID, taskID, commentID, req.RevisionID)
		if err != nil {
			writeWorkflowDomainError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(commentToAPIJSON(*c))
		return
	}

	utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid comment path.")
}
