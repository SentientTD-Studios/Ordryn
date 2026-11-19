package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
)

// Admin → Chat servers: which Ordryn Chat servers may use "Sign in with Ordryn".
//
//	GET    /api/v2/admin/chat                                  settings + servers
//	PUT    /api/v2/admin/chat                                  {signin_enabled}
//	POST   /api/v2/admin/chat/servers                          {name, url, directory_sync?, task_events?, task_create?, account_create?} → server + client_secret (once)
//	PATCH  /api/v2/admin/chat/servers/{id}                     {name?, url?, enabled?, directory_sync?, task_events?, task_create?, account_create?}
//	DELETE /api/v2/admin/chat/servers/{id}
//	POST   /api/v2/admin/chat/servers/{id}/rotate-secret       → server + client_secret (once)

type apiAdminChatJSON struct {
	SignInEnabled bool                 `json:"signin_enabled"`
	Servers       []storage.ChatServer `json:"servers"`
}

// apiChatServerWithSecretJSON is returned when a secret is created or rotated:
// the only time it's ever shown.
type apiChatServerWithSecretJSON struct {
	storage.ChatServer
	ClientSecret string `json:"client_secret"`
}

type apiChatServerWriteRequest struct {
	Name          *string `json:"name"`
	URL           *string `json:"url"`
	Enabled       *bool   `json:"enabled"`
	DirectorySync *bool   `json:"directory_sync"`
	TaskEvents    *bool   `json:"task_events"`
	TaskCreate    *bool   `json:"task_create"`
	AccountCreate *bool   `json:"account_create"`
}

func writeJSONStatus(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func chatServerName(raw string) (string, bool) {
	name := strings.TrimSpace(raw)
	return name, name != "" && len(name) <= 60
}

// APIV1AdminChatRouter handles /api/v2/admin/chat and everything under it.
func APIV1AdminChatRouter(w http.ResponseWriter, r *http.Request) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	parts := strings.Split(strings.Trim(utils.ParseAPIV1Subpath(r, "admin/chat"), "/"), "/")
	switch {
	case len(parts) == 1 && parts[0] == "":
		apiV1AdminChatSettings(w, r)
	case len(parts) == 1 && parts[0] == "servers" && r.Method == http.MethodPost:
		apiV1AdminCreateChatServer(w, r, userID)
	case len(parts) >= 2 && parts[0] == "servers":
		id, err := strconv.Atoi(parts[1])
		if err != nil || id <= 0 {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid chat server id.")
			return
		}
		switch {
		case len(parts) == 2:
			apiV1AdminChatServer(w, r, id)
		case len(parts) == 3 && parts[2] == "rotate-secret" && r.Method == http.MethodPost:
			apiV1AdminRotateChatSecret(w, r, id)
		default:
			utils.APIJSONError(w, http.StatusNotFound, "not_found", "Not found.")
		}
	case len(parts) == 1 && parts[0] == "servers":
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
	default:
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Not found.")
	}
}

func apiV1AdminChatSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
	case http.MethodPut:
		var req struct {
			SignInEnabled *bool `json:"signin_enabled"`
		}
		if err := decodeJSONBody(r, &req); err != nil || req.SignInEnabled == nil {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "signin_enabled is required.")
			return
		}
		before, _ := storage.GetChatSignInEnabled()
		if err := storage.SetChatSignInEnabled(*req.SignInEnabled); err != nil {
			utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to save chat settings.")
			return
		}
		if before != *req.SignInEnabled {
			logAdminEvent(r, "chat_signin_updated", "chat", 0, "Chat sign-in",
				map[string]interface{}{"from": before, "to": *req.SignInEnabled})
		}
	default:
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	on, err := storage.GetChatSignInEnabled()
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load chat settings.")
		return
	}
	servers, err := storage.ListChatServers()
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load chat servers.")
		return
	}
	writeJSONStatus(w, http.StatusOK, apiAdminChatJSON{SignInEnabled: on, Servers: servers})
}

func apiV1AdminCreateChatServer(w http.ResponseWriter, r *http.Request, userID int) {
	var req apiChatServerWriteRequest
	if err := decodeJSONBody(r, &req); err != nil || req.Name == nil || req.URL == nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "name and url are required.")
		return
	}
	name, ok := chatServerName(*req.Name)
	if !ok {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Name must be 1-60 characters.")
		return
	}
	u, err := storage.NormalizeChatServerURL(*req.URL)
	if err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	cs, secret, err := storage.CreateChatServer(name, u, req.DirectorySync != nil && *req.DirectorySync, req.TaskEvents != nil && *req.TaskEvents,
		req.TaskCreate != nil && *req.TaskCreate, req.AccountCreate != nil && *req.AccountCreate, userID)
	if errors.Is(err, storage.ErrChatServerURLTaken) {
		utils.APIJSONError(w, http.StatusConflict, "conflict", err.Error())
		return
	}
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to add chat server.")
		return
	}
	logAdminEvent(r, "chat_server_added", "chat_server", int64(cs.ID), cs.Name,
		map[string]interface{}{"url": cs.URL, "client_id": cs.ClientID, "directory_sync": cs.DirectorySync, "task_events": cs.TaskEvents,
			"task_create": cs.TaskCreate, "account_create": cs.AccountCreate})
	writeJSONStatus(w, http.StatusCreated, apiChatServerWithSecretJSON{ChatServer: *cs, ClientSecret: secret})
}

func apiV1AdminChatServer(w http.ResponseWriter, r *http.Request, id int) {
	switch r.Method {
	case http.MethodPatch:
		var req apiChatServerWriteRequest
		if err := decodeJSONBody(r, &req); err != nil {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
			return
		}
		if req.Name == nil && req.URL == nil && req.Enabled == nil && req.DirectorySync == nil && req.TaskEvents == nil && req.TaskCreate == nil && req.AccountCreate == nil {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Nothing to update.")
			return
		}
		if req.Name != nil {
			name, ok := chatServerName(*req.Name)
			if !ok {
				utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Name must be 1-60 characters.")
				return
			}
			req.Name = &name
		}
		if req.URL != nil {
			u, err := storage.NormalizeChatServerURL(*req.URL)
			if err != nil {
				utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", err.Error())
				return
			}
			req.URL = &u
		}
		before, err := storage.GetChatServer(id)
		if errors.Is(err, storage.ErrChatServerNotFound) {
			utils.APIJSONError(w, http.StatusNotFound, "not_found", "Chat server not found.")
			return
		}
		if err != nil {
			utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load chat server.")
			return
		}
		cs, err := storage.UpdateChatServer(id, req.Name, req.URL, req.Enabled, req.DirectorySync, req.TaskEvents, req.TaskCreate, req.AccountCreate)
		switch {
		case errors.Is(err, storage.ErrChatServerURLTaken):
			utils.APIJSONError(w, http.StatusConflict, "conflict", err.Error())
			return
		case errors.Is(err, storage.ErrChatServerNotFound):
			utils.APIJSONError(w, http.StatusNotFound, "not_found", "Chat server not found.")
			return
		case err != nil:
			utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to update chat server.")
			return
		}
		meta := map[string]interface{}{}
		if before.Name != cs.Name {
			meta["name"] = map[string]interface{}{"from": before.Name, "to": cs.Name}
		}
		if before.URL != cs.URL {
			meta["url"] = map[string]interface{}{"from": before.URL, "to": cs.URL}
		}
		if before.Enabled != cs.Enabled {
			meta["enabled"] = map[string]interface{}{"from": before.Enabled, "to": cs.Enabled}
		}
		if before.DirectorySync != cs.DirectorySync {
			meta["directory_sync"] = map[string]interface{}{"from": before.DirectorySync, "to": cs.DirectorySync}
		}
		if before.TaskEvents != cs.TaskEvents {
			meta["task_events"] = map[string]interface{}{"from": before.TaskEvents, "to": cs.TaskEvents}
		}
		if before.TaskCreate != cs.TaskCreate {
			meta["task_create"] = map[string]interface{}{"from": before.TaskCreate, "to": cs.TaskCreate}
		}
		if before.AccountCreate != cs.AccountCreate {
			meta["account_create"] = map[string]interface{}{"from": before.AccountCreate, "to": cs.AccountCreate}
		}
		if len(meta) > 0 {
			logAdminEvent(r, "chat_server_updated", "chat_server", int64(cs.ID), cs.Name, meta)
		}
		writeJSONStatus(w, http.StatusOK, cs)
	case http.MethodDelete:
		cs, err := storage.DeleteChatServer(id)
		if errors.Is(err, storage.ErrChatServerNotFound) {
			utils.APIJSONError(w, http.StatusNotFound, "not_found", "Chat server not found.")
			return
		}
		if err != nil {
			utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to remove chat server.")
			return
		}
		logAdminEvent(r, "chat_server_removed", "chat_server", int64(cs.ID), cs.Name, map[string]interface{}{"url": cs.URL})
		writeJSONStatus(w, http.StatusOK, map[string]bool{"ok": true})
	default:
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
	}
}

func apiV1AdminRotateChatSecret(w http.ResponseWriter, r *http.Request, id int) {
	cs, secret, err := storage.RotateChatServerSecret(id)
	if errors.Is(err, storage.ErrChatServerNotFound) {
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Chat server not found.")
		return
	}
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to rotate secret.")
		return
	}
	logAdminEvent(r, "chat_server_secret_rotated", "chat_server", int64(cs.ID), cs.Name, nil)
	writeJSONStatus(w, http.StatusOK, apiChatServerWithSecretJSON{ChatServer: *cs, ClientSecret: secret})
}
