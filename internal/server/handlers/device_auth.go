package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"GoTodo/internal/server/utils"
	"GoTodo/internal/statestore"
	"GoTodo/internal/storage"
)

const deviceGrantType = "urn:ietf:params:oauth:grant-type:device_code"

type deviceCodeRequest struct {
	ClientName  string `json:"client_name"`
	RedirectURI string `json:"redirect_uri"`
	// Scopes is optional. ["profile:read"] asks for a short-lived sign-in key
	// instead of a full-access personal key.
	Scopes []string `json:"scopes"`
	// ClientID and ClientSecret identify a chat server registered under
	// Admin → Chat servers. Sign-in-only requests must include them.
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

type deviceTokenRequest struct {
	DeviceCode string `json:"device_code"`
	GrantType  string `json:"grant_type"`
}

func writeDeviceOAuthError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
}

// DeviceAuthPublicChain wraps public device authorization API endpoints.
func DeviceAuthPublicChain(handler http.HandlerFunc) http.HandlerFunc {
	return utils.RequireAPIEnabled(
		utils.RequireStateStore(
			utils.RateLimitMiddleware(10, 1.0, 60, utils.KeyByIP)(handler),
		),
	)
}

// APIDeviceCode starts a browser-handoff device authorization request.
func APIDeviceCode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}

	var req deviceCodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
		return
	}

	redirectURI, err := utils.SafeDeviceRedirectURI(req.RedirectURI)
	if err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request",
			"Invalid redirect_uri. Allowed example: ordryn://auth-complete")
		return
	}

	scopes, err := utils.NormalizeDeviceScopes(req.Scopes)
	if err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_scope",
			"Unsupported scope. Omit scopes for a full-access key, or request [\"profile:read\"].")
		return
	}

	// Sign-in-only keys are for chat servers a site admin has registered: the
	// request must carry that server's client ID and secret. Chat credentials
	// never get a full-access key.
	identity := len(scopes) > 0
	hasClient := strings.TrimSpace(req.ClientID) != "" || strings.TrimSpace(req.ClientSecret) != ""
	var chat *utils.DeviceChatServer
	switch {
	case identity:
		cs, err := storage.AuthenticateChatServer(req.ClientID, req.ClientSecret)
		if errors.Is(err, storage.ErrChatClientRejected) {
			utils.APIJSONError(w, http.StatusUnauthorized, "invalid_client",
				"This chat server isn't allowed to use Sign in with Ordryn. An Ordryn admin can add it under Admin → Chat servers.")
			return
		}
		if err != nil {
			utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to check the chat server.")
			return
		}
		chat = &utils.DeviceChatServer{ID: cs.ID, Name: cs.Name, URL: cs.URL}
	case hasClient:
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_scope",
			"Chat server credentials can only request [\"profile:read\"].")
		return
	}

	record, err := utils.CreateDeviceAuthRequest(r.Context(), req.ClientName, redirectURI, scopes, chat)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to create device authorization request.")
		return
	}

	verificationURI := utils.AbsoluteURLForRequest(r, "/auth/device")
	verificationURIComplete := utils.AbsoluteURLForRequest(r, "/auth/device?user_code="+record.UserCode)

	resp := map[string]interface{}{
		"device_code":               record.DeviceCode,
		"user_code":                 record.UserCode,
		"verification_uri":          verificationURI,
		"verification_uri_complete": verificationURIComplete,
		"expires_in":                utils.DeviceAuthTTLSeconds,
		"interval":                  utils.DeviceAuthInterval,
	}
	if record.RedirectURI != "" {
		resp["redirect_uri"] = record.RedirectURI
	}
	if len(record.Scopes) > 0 {
		resp["scopes"] = record.Scopes
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(resp)
}

// APIDeviceToken polls for an approved device authorization and returns the API key once.
func APIDeviceToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}

	var req deviceTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
		return
	}
	req.DeviceCode = strings.TrimSpace(req.DeviceCode)
	if req.DeviceCode == "" {
		writeDeviceOAuthError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	if req.GrantType != deviceGrantType {
		writeDeviceOAuthError(w, http.StatusBadRequest, "invalid_grant")
		return
	}

	record, plaintext, err := utils.ConsumeApprovedDeviceToken(r.Context(), req.DeviceCode)
	if err != nil {
		if errors.Is(err, statestore.ErrNotFound) {
			writeDeviceOAuthError(w, http.StatusBadRequest, "expired_token")
			return
		}
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load device authorization request.")
		return
	}

	switch record.Status {
	case utils.DeviceAuthPending:
		writeDeviceOAuthError(w, http.StatusBadRequest, "authorization_pending")
		return
	case utils.DeviceAuthDenied:
		writeDeviceOAuthError(w, http.StatusBadRequest, "access_denied")
		return
	case utils.DeviceAuthApproved:
		if plaintext == "" {
			writeDeviceOAuthError(w, http.StatusBadRequest, "access_denied")
			return
		}
		resp := map[string]interface{}{
			"api_key":    plaintext,
			"name":       record.KeyName,
			"key_prefix": record.KeyPrefix,
			"key_id":     record.KeyID,
		}
		if len(record.Scopes) > 0 {
			resp["scopes"] = record.Scopes
		}
		if record.ExpiresAt != nil {
			resp["expires_at"] = record.ExpiresAt
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(resp)
		return
	default:
		writeDeviceOAuthError(w, http.StatusBadRequest, "expired_token")
	}
}
