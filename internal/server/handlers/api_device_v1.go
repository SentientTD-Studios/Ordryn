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
	resp := map[string]interface{}{
		"user_code":   record.UserCode,
		"client_name": record.ClientName,
		"status":      string(record.Status),
	}
	if record.RedirectURI != "" {
		resp["redirect_uri"] = record.RedirectURI
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(resp)
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
	clientName := utils.NormalizeClientName(record.ClientName)
	plaintext, keyRecord, err := storage.CreateOrRotateAPIKey(userID, clientName)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to create API key.")
		return
	}
	if err := utils.ApproveDeviceAuth(r.Context(), userCode, userID, plaintext, keyRecord.KeyPrefix, keyRecord.Name); err != nil {
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
