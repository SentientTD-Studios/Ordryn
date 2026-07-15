package handlers

import (
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

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

	basePath := utils.GetBasePath()
	verificationPath := basePath + "/auth/device?user_code=" + record.UserCode
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	host := r.Host
	if host == "" {
		host = "localhost"
	}
	verificationComplete := scheme + "://" + host + verificationPath

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"device_code":              record.DeviceCode,
		"user_code":                record.UserCode,
		"verification_uri":           basePath + "/auth/device",
		"verification_uri_complete": verificationComplete,
		"expires_in":               utils.DeviceAuthTTLSeconds,
		"interval":                 utils.DeviceAuthInterval,
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

// DeviceAuthPageHandler renders the browser approval page for a device authorization request.
func DeviceAuthPageHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userCode := strings.TrimSpace(r.URL.Query().Get("user_code"))
	email, _, permissions, loggedIn := utils.GetSessionUser(r)

	context := map[string]interface{}{
		"LoggedIn":    loggedIn,
		"UserEmail":   email,
		"Permissions": permissions,
		"Title":       "Authorize device",
		"UserCode":    userCode,
		"ReturnTo":    utils.SafeDeviceReturnTo(r.URL.RequestURI()),
	}

	if userCode == "" {
		context["State"] = "missing"
		utils.RenderTemplate(w, r, "device_auth.html", context)
		return
	}

	record, err := utils.GetDeviceAuthByUserCode(r.Context(), userCode)
	if err != nil {
		if err == redis.Nil {
			context["State"] = "expired"
		} else {
			context["State"] = "error"
		}
		utils.RenderTemplate(w, r, "device_auth.html", context)
		return
	}

	context["ClientName"] = record.ClientName
	context["UserCode"] = record.UserCode

	switch record.Status {
	case utils.DeviceAuthPending:
		if loggedIn {
			context["State"] = "pending"
			csrfToken, _ := utils.EnsureCSRFToken(r, w)
			context["CSRFToken"] = csrfToken
		} else {
			context["State"] = "login"
			if context["ReturnTo"] == "" {
				basePath := utils.GetBasePath()
				context["ReturnTo"] = basePath + "/auth/device?user_code=" + record.UserCode
			}
		}
	case utils.DeviceAuthApproved:
		context["State"] = "approved"
	case utils.DeviceAuthDenied:
		context["State"] = "denied"
	default:
		context["State"] = "error"
	}

	utils.RenderTemplate(w, r, "device_auth.html", context)
}

// APIDeviceApprove approves a pending device authorization and stages the API key for polling.
func APIDeviceApprove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	uid := utils.GetSessionUserID(r)
	if uid == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	userCode := strings.TrimSpace(r.FormValue("user_code"))
	if userCode == "" {
		http.Error(w, "Missing user code", http.StatusBadRequest)
		return
	}

	record, err := utils.GetDeviceAuthByUserCode(r.Context(), userCode)
	if err != nil {
		if err == redis.Nil {
			http.Error(w, "Authorization request expired", http.StatusBadRequest)
			return
		}
		http.Error(w, "Failed to load authorization request", http.StatusInternalServerError)
		return
	}
	if record.Status != utils.DeviceAuthPending {
		http.Error(w, "Authorization request is no longer pending", http.StatusBadRequest)
		return
	}

	clientName := utils.NormalizeClientName(record.ClientName)
	plaintext, keyRecord, err := storage.CreateOrRotateAPIKey(*uid, clientName)
	if err != nil {
		http.Error(w, "Failed to create API key", http.StatusInternalServerError)
		return
	}

	if err := utils.ApproveDeviceAuth(r.Context(), userCode, *uid, plaintext, keyRecord.KeyPrefix, keyRecord.Name); err != nil {
		http.Error(w, "Failed to approve authorization request", http.StatusInternalServerError)
		return
	}

	basePath := utils.GetBasePath()
	w.Header().Set("HX-Redirect", basePath+"/auth/device?user_code="+record.UserCode)
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, " ")
}

// APIDeviceDeny denies a pending device authorization request.
func APIDeviceDeny(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if utils.GetSessionUserID(r) == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	userCode := strings.TrimSpace(r.FormValue("user_code"))
	if userCode == "" {
		http.Error(w, "Missing user code", http.StatusBadRequest)
		return
	}

	record, err := utils.GetDeviceAuthByUserCode(r.Context(), userCode)
	if err != nil {
		if err == redis.Nil {
			http.Error(w, "Authorization request expired", http.StatusBadRequest)
			return
		}
		http.Error(w, "Failed to load authorization request", http.StatusInternalServerError)
		return
	}

	if err := utils.DenyDeviceAuth(r.Context(), userCode); err != nil {
		http.Error(w, "Failed to deny authorization request", http.StatusInternalServerError)
		return
	}

	basePath := utils.GetBasePath()
	w.Header().Set("HX-Redirect", basePath+"/auth/device?user_code="+record.UserCode)
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, " ")
}
