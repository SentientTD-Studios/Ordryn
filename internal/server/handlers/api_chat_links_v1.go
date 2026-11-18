package handlers

import (
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
)

// Project links for Ordryn Chat. A registered chat server with "Task events"
// allowed under Admin → Chat servers authenticates with HTTP Basic (client
// ID : client secret), like the directory.
//
//	GET    /api/v2/chat/projects?user_id=N   projects user N can see, and which they can manage
//	PUT    /api/v2/chat/links/{linkId}       {project_id, user_id}: link on behalf of user N (must manage the project)
//	DELETE /api/v2/chat/links/{linkId}
//	GET    /api/v2/chat/events?after=N       acknowledges events ≤ N, returns newer ones and every link's state
//
// The chat server says which of its (Ordryn-linked) users is acting; Ordryn
// decides whether that user may link the project, and keeps checking on every
// event, pausing the link once the user can no longer manage the project.

var chatLinkIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

const chatEventsPageSize = 200

// ChatEventsChain rate-limits the chat link and event endpoints per IP, in a
// bucket of their own so event polling can't starve directory sync.
func ChatEventsChain(handler http.HandlerFunc) http.HandlerFunc {
	key := func(r *http.Request) string {
		if ip := utils.KeyByIP(r); ip != "" {
			return "chat-events:" + ip
		}
		return ""
	}
	return utils.RequireAPIEnabled(
		utils.RequireAPIRedis(
			utils.RateLimitMiddleware(60, 2, 120, key)(handler),
		),
	)
}

// chatServerForEvents authenticates the chat server and requires task events.
func chatServerForEvents(w http.ResponseWriter, r *http.Request) (*storage.ChatServer, bool) {
	clientID, secret, ok := r.BasicAuth()
	if !ok {
		w.Header().Set("WWW-Authenticate", `Basic realm="Ordryn Chat"`)
		utils.APIJSONError(w, http.StatusUnauthorized, "invalid_client", "Send the chat server's client ID and secret (HTTP Basic).")
		return nil, false
	}
	cs, err := storage.AuthenticateChatServer(clientID, secret)
	if errors.Is(err, storage.ErrChatClientRejected) {
		utils.APIJSONError(w, http.StatusUnauthorized, "invalid_client",
			"This chat server isn't allowed to use Ordryn. An Ordryn admin can add it under Admin → Chat servers.")
		return nil, false
	}
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to check the chat server.")
		return nil, false
	}
	if !cs.TaskEvents {
		utils.APIJSONError(w, http.StatusForbidden, "task_events_disabled",
			"Project updates aren't allowed for this chat server. An Ordryn admin can turn on \"Task events\" for it under Admin → Chat servers.")
		return nil, false
	}
	return cs, true
}

// APIV2ChatProjects serves GET /api/v2/chat/projects.
func APIV2ChatProjects(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	if _, ok := chatServerForEvents(w, r); !ok {
		return
	}
	userID, err := strconv.Atoi(r.URL.Query().Get("user_id"))
	if err != nil || userID <= 0 {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "user_id is required.")
		return
	}
	if storage.IsSystemUser(userID) {
		writeJSONStatus(w, http.StatusOK, map[string]any{"projects": []storage.ChatProject{}})
		return
	}
	projects, err := storage.ChatProjectsForUser(userID)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to list projects.")
		return
	}
	writeJSONStatus(w, http.StatusOK, map[string]any{"projects": projects})
}

// APIV2ChatLinks serves PUT and DELETE /api/v2/chat/links/{linkId}.
func APIV2ChatLinks(w http.ResponseWriter, r *http.Request) {
	cs, ok := chatServerForEvents(w, r)
	if !ok {
		return
	}
	linkID := strings.Trim(utils.ParseAPIV1Subpath(r, "chat/links"), "/")
	if !chatLinkIDRe.MatchString(linkID) {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid link id.")
		return
	}
	switch r.Method {
	case http.MethodPut:
		var req struct {
			ProjectID int `json:"project_id"`
			UserID    int `json:"user_id"`
		}
		if err := decodeJSONBody(r, &req); err != nil || req.ProjectID <= 0 || req.UserID <= 0 {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "project_id and user_id are required.")
			return
		}
		name, reason, err := storage.ChatLinkCheck(req.UserID, req.ProjectID)
		if err != nil {
			utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to check project access.")
			return
		}
		if reason != "" {
			utils.APIJSONError(w, http.StatusForbidden, "forbidden",
				"Only people who can manage this project in Ordryn can link it to a channel.")
			return
		}
		if err := storage.UpsertChatLink(cs.ID, linkID, req.ProjectID, req.UserID); err != nil {
			utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to save the link.")
			return
		}
		writeJSONStatus(w, http.StatusOK, storage.ChatLink{
			LinkID: linkID, ProjectID: req.ProjectID, ProjectName: name, UserID: req.UserID, Active: true,
		})
	case http.MethodDelete:
		if err := storage.DeleteChatLink(cs.ID, linkID); err != nil {
			utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to remove the link.")
			return
		}
		writeJSONStatus(w, http.StatusOK, map[string]bool{"ok": true})
	default:
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
	}
}

// APIV2ChatEvents serves GET /api/v2/chat/events.
func APIV2ChatEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	cs, ok := chatServerForEvents(w, r)
	if !ok {
		return
	}
	var after int64
	if v := r.URL.Query().Get("after"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "after must be a non-negative integer.")
			return
		}
		after = n
	}
	events, err := storage.TakeChatEvents(cs.ID, after, chatEventsPageSize)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to read events.")
		return
	}
	links, err := storage.ListChatLinks(cs.ID)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to list links.")
		return
	}
	writeJSONStatus(w, http.StatusOK, map[string]any{
		"events": events, "links": links, "more": len(events) == chatEventsPageSize,
	})
}
