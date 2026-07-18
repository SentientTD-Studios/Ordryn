package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"GoTodo/internal/server/utils"

	"github.com/redis/go-redis/v9"
)

const deviceGrantType = "urn:ietf:params:oauth:grant-type:device_code"

type deviceCodeRequest struct {
	ClientName string `json:"client_name"`
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
		utils.RequireAPIRedis(
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

	record, err := utils.CreateDeviceAuthRequest(r.Context(), req.ClientName)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to create device authorization request.")
		return
	}

	verificationURI := utils.AbsoluteURLForRequest(r, "/auth/device")
	verificationURIComplete := utils.AbsoluteURLForRequest(r, "/auth/device?user_code="+record.UserCode)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"device_code":               record.DeviceCode,
		"user_code":                 record.UserCode,
		"verification_uri":          verificationURI,
		"verification_uri_complete": verificationURIComplete,
		"expires_in":                utils.DeviceAuthTTLSeconds,
		"interval":                  utils.DeviceAuthInterval,
	})
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
		if err == redis.Nil {
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
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"api_key":    plaintext,
			"name":       record.KeyName,
			"key_prefix": record.KeyPrefix,
		})
		return
	default:
		writeDeviceOAuthError(w, http.StatusBadRequest, "expired_token")
	}
}
