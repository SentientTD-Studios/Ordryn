package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"

	"github.com/redis/go-redis/v9"
)

type deviceDecisionRequest struct {
	UserCode string `json:"user_code"`
}

// APIV1DeviceStatus returns the current device-auth state for a user_code.
func APIV1DeviceStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	userCode := strings.TrimSpace(r.URL.Query().Get("user_code"))
	if userCode == "" {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "user_code is required.")
		return
	}
	record, err := utils.GetDeviceAuthByUserCode(r.Context(), userCode)
	if err != nil {
		if err == redis.Nil {
			utils.APIJSONError(w, http.StatusNotFound, "not_found", "Authorization request expired or not found.")
			return
		}
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load authorization request.")
		return
	}
	access := "full"
	if record.IsIdentity() {
		access = "identity"
	}
	resp := map[string]interface{}{
		"user_code":   record.UserCode,
		"client_name": record.ClientName,
		"status":      string(record.Status),
		"access":      access,
	}
	if record.RedirectURI != "" {
		resp["redirect_uri"] = record.RedirectURI
	}
	if record.ChatServer != nil {
		resp["chat_server"] = map[string]string{"name": record.ChatServer.Name, "url": record.ChatServer.URL}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(resp)
}

// chatServerStillAllowed re-checks a request's chat server at approval time,
// in case an admin removed or disabled it after the request started.
func chatServerStillAllowed(record *utils.DeviceAuthRecord) bool {
	if record.ChatServer == nil {
		return !record.IsIdentity() // identity requests always come from a chat server
	}
	on, err := storage.GetChatSignInEnabled()
	if err != nil || !on {
		return false
	}
	cs, err := storage.GetChatServer(record.ChatServer.ID)
	return err == nil && cs.Enabled
}

// APIV1DeviceApprove approves a pending device authorization (JSON).
func APIV1DeviceApprove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	userID, ok := utils.GetAPIUserID(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	var req deviceDecisionRequest
	if err := decodeJSONBody(r, &req); err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
		return
	}
	userCode := strings.TrimSpace(req.UserCode)
	if userCode == "" {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "user_code is required.")
		return
	}
	record, err := utils.GetDeviceAuthByUserCode(r.Context(), userCode)
	if err != nil {
		if err == redis.Nil {
			utils.APIJSONError(w, http.StatusNotFound, "not_found", "Authorization request expired or not found.")
			return
		}
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load authorization request.")
		return
	}
	if record.Status != utils.DeviceAuthPending {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Authorization request is no longer pending.")
		return
	}
	if !chatServerStillAllowed(record) {
		_ = utils.DenyDeviceAuth(r.Context(), userCode)
		utils.APIJSONError(w, http.StatusForbidden, "forbidden",
			"This chat server is no longer allowed to use Sign in with Ordryn.")
		return
	}
	clientName := utils.NormalizeClientName(record.ClientName)
	if record.ChatServer != nil {
		clientName = utils.NormalizeClientName("Ordryn Chat: " + record.ChatServer.Name)
	}
	var plaintext string
	var keyRecord *storage.APIKey
	if record.IsIdentity() {
		plaintext, keyRecord, err = storage.CreateOrRotateIdentityAPIKey(userID, clientName)
	} else {
		plaintext, keyRecord, err = storage.CreateOrRotateAPIKey(userID, clientName)
	}
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to create API key.")
		return
	}
	if err := utils.ApproveDeviceAuth(r.Context(), userCode, userID, plaintext, keyRecord); err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to approve authorization request.")
		return
	}
	resp := map[string]interface{}{
		"ok":     true,
		"status": "approved",
	}
	if redirect := utils.DeviceDecisionRedirectURI(record.RedirectURI, true); redirect != "" {
		resp["redirect_uri"] = redirect
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(resp)
}

// APIV1DeviceDeny denies a pending device authorization (JSON).
func APIV1DeviceDeny(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	if _, ok := utils.GetAPIUserID(r); !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	var req deviceDecisionRequest
	if err := decodeJSONBody(r, &req); err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
		return
	}
	userCode := strings.TrimSpace(req.UserCode)
	if userCode == "" {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "user_code is required.")
		return
	}
	record, err := utils.GetDeviceAuthByUserCode(r.Context(), userCode)
	if err != nil {
		if err == redis.Nil {
			utils.APIJSONError(w, http.StatusNotFound, "not_found", "Authorization request expired or not found.")
			return
		}
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load authorization request.")
		return
	}
	if err := utils.DenyDeviceAuth(r.Context(), userCode); err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to deny authorization request.")
		return
	}
	resp := map[string]interface{}{
		"ok":     true,
		"status": "denied",
	}
	if redirect := utils.DeviceDecisionRedirectURI(record.RedirectURI, false); redirect != "" {
		resp["redirect_uri"] = redirect
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(resp)
}
