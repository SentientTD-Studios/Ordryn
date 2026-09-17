package handlers

import (
	"GoTodo/internal/domain"
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type apiNotificationJSON struct {
	ID          int     `json:"id"`
	Type        string  `json:"type"`
	Title       string  `json:"title"`
	Body        string  `json:"body"`
	ProjectID   *int    `json:"project_id,omitempty"`
	TaskID      *int    `json:"task_id,omitempty"`
	ProjectName string  `json:"project_name,omitempty"`
	ActorName   string  `json:"actor_name,omitempty"`
	ReadAt      *string `json:"read_at,omitempty"`
	CreatedAt   string  `json:"created_at"`
}

type apiNotificationListResponse struct {
	Notifications []apiNotificationJSON `json:"notifications"`
	Total         int                   `json:"total"`
	Page          int                   `json:"page"`
	PerPage       int                   `json:"per_page"`
	UnreadCount   int                   `json:"unread_count"`
}

func notificationToAPIJSON(n storage.UserNotification) apiNotificationJSON {
	out := apiNotificationJSON{
		ID:          n.ID,
		Type:        n.Type,
		Title:       n.Title,
		Body:        n.Body,
		ProjectName: n.ProjectName,
		ActorName:   n.ActorName,
		CreatedAt:   n.CreatedAt.UTC().Format(time.RFC3339),
	}
	if n.ProjectID > 0 {
		pid := n.ProjectID
		out.ProjectID = &pid
	}
	if n.TaskID > 0 {
		tid := n.TaskID
		out.TaskID = &tid
	}
	if n.ReadAt != nil {
		s := n.ReadAt.UTC().Format(time.RFC3339)
		out.ReadAt = &s
	}
	return out
}

// APIV1NotificationsRouter handles /api/v2/notifications and subpaths.
func APIV1NotificationsRouter(w http.ResponseWriter, r *http.Request) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}

	sub := utils.ParseAPIV1Subpath(r, "notifications")
	if sub == "" {
		switch r.Method {
		case http.MethodGet:
			apiV1ListNotifications(w, r, userID)
		default:
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		}
		return
	}

	switch sub {
	case "unread-count":
		if r.Method != http.MethodGet {
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
			return
		}
		count, err := domain.UnreadNotificationCountForUser(r.Context(), userID)
		if err != nil {
			utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load unread count.")
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]int{"unread_count": count})
		return
	case "read-all":
		if r.Method != http.MethodPost {
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
			return
		}
		if err := domain.MarkAllNotificationsReadForUser(r.Context(), userID); err != nil {
			utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to mark notifications read.")
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}

	parts := strings.Split(sub, "/")
	id, err := strconv.Atoi(parts[0])
	if err != nil || id <= 0 {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid notification id.")
		return
	}
	if len(parts) == 2 && parts[1] == "read" && r.Method == http.MethodPost {
		if err := domain.MarkNotificationReadForUser(r.Context(), userID, id); err != nil {
			writeWorkflowDomainError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid notification path.")
}

func apiV1ListNotifications(w http.ResponseWriter, r *http.Request, userID int) {
	page := 1
	perPage := 20
	if p := r.URL.Query().Get("page"); p != "" {
		if v, err := strconv.Atoi(p); err == nil && v > 0 {
			page = v
		}
	}
	if pp := r.URL.Query().Get("per_page"); pp != "" {
		if v, err := strconv.Atoi(pp); err == nil && v > 0 {
			perPage = v
		}
	}
	items, total, err := domain.ListNotificationsForUser(r.Context(), userID, page, perPage)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load notifications.")
		return
	}
	unread, _ := domain.UnreadNotificationCountForUser(r.Context(), userID)
	out := make([]apiNotificationJSON, 0, len(items))
	for _, n := range items {
		out = append(out, notificationToAPIJSON(n))
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(apiNotificationListResponse{
		Notifications: out,
		Total:         total,
		Page:          page,
		PerPage:       perPage,
		UnreadCount:   unread,
	})
}
