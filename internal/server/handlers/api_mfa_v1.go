package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"GoTodo/internal/domain"
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
)

type apiMFACodeRequest struct {
	Code string `json:"code"`
}

func writeMFADomainError(w http.ResponseWriter, err error, invalidStatus int, invalidCode, invalidMessage string) {
	if errors.Is(err, domain.ErrValidation) {
		utils.APIJSONError(w, invalidStatus, invalidCode, invalidMessage)
		return
	}
	if errors.Is(err, domain.ErrConflict) {
		utils.APIJSONError(w, http.StatusConflict, "conflict", err.Error())
		return
	}
	utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Internal server error.")
}

func mfaIssuer() string {
	if settings, err := storage.GetSiteSettings(); err == nil && settings != nil && settings.SiteName != "" {
		return settings.SiteName
	}
	return "GoTodo"
}

func incrementFailedMFA(r *http.Request, email string) {
	if email == "" {
		return
	}
	if _, incErr := utils.IncrementFailedLogin(r.Context(), email, 900); incErr != nil {
		fmt.Printf("MFA increment failed login: %v\n", incErr)
	}
}

// APIV1AuthMFAVerify handles POST /api/v2/auth/mfa/verify.
func APIV1AuthMFAVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}

	userID, email, ok := utils.GetPendingMFA(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "mfa_pending_required",
			"Password login with MFA is required before submitting a code.")
		return
	}

	blocked, err := utils.IsLoginBlocked(r.Context(), email, 5)
	if err != nil {
		fmt.Printf("APIV1AuthMFAVerify block check: %v\n", err)
	}
	if blocked {
		utils.APIJSONError(w, http.StatusTooManyRequests, "rate_limit_exceeded",
			"Too many login attempts; please try again later.")
		return
	}

	var req apiMFACodeRequest
	if err := decodeJSONBody(r, &req); err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
		return
	}
	if strings.TrimSpace(req.Code) == "" {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Authentication code is required.")
		return
	}

	if err := domain.VerifyLoginMFA(userID, req.Code); err != nil {
		if errors.Is(err, domain.ErrValidation) || errors.Is(err, domain.ErrConflict) {
			incrementFailedMFA(r, email)
			utils.APIJSONError(w, http.StatusUnauthorized, "invalid_credentials", "Invalid authentication code.")
			return
		}
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Internal server error.")
		return
	}

	profile, err := storage.GetUserProfileByID(userID)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Internal server error.")
		return
	}
	if err := establishSession(w, r, profile); err != nil {
		fmt.Printf("APIV1AuthMFAVerify session: %v\n", err)
		utils.APIJSONError(w, http.StatusInternalServerError, "session_error", "Failed to create session.")
		return
	}
	if email != "" {
		if clearErr := utils.ClearFailedLogin(r.Context(), email); clearErr != nil {
			fmt.Printf("APIV1AuthMFAVerify clear failed login: %v\n", clearErr)
		}
	}
	writeAPIUserJSON(w, http.StatusOK, profile)
}

// APIV1MeMFA handles GET /api/v2/me/mfa.
func APIV1MeMFA(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	userID, ok := utils.GetAPIUserID(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	status, err := domain.GetMFAStatus(userID)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Internal server error.")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"enabled":                  status.Enabled,
		"recovery_codes_remaining": status.RecoveryCodesRemaining,
	})
}

// APIV1MeMFASetup handles POST /api/v2/me/mfa/setup.
func APIV1MeMFASetup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	userID, ok := utils.GetAPIUserID(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	status, err := domain.GetMFAStatus(userID)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Internal server error.")
		return
	}
	if status.Enabled {
		utils.APIJSONError(w, http.StatusConflict, "conflict", "MFA is already enabled.")
		return
	}
	profile, err := storage.GetUserProfileByID(userID)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Internal server error.")
		return
	}
	account := profile.Email
	if account == "" {
		account = profile.UserName
	}
	setup, err := domain.GenerateTOTPSetup(mfaIssuer(), account)
	if err != nil {
		writeMFADomainError(w, err, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if err := utils.SetMFASetupSecret(w, r, setup.Secret); err != nil {
		fmt.Printf("APIV1MeMFASetup session: %v\n", err)
		utils.APIJSONError(w, http.StatusInternalServerError, "session_error", "Failed to save MFA setup.")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"secret":      setup.Secret,
		"otpauth_url": setup.OtpauthURL,
	})
}

// APIV1MeMFAEnable handles POST /api/v2/me/mfa/enable.
func APIV1MeMFAEnable(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	userID, ok := utils.GetAPIUserID(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	var req apiMFACodeRequest
	if err := decodeJSONBody(r, &req); err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
		return
	}
	codes, err := domain.EnableMFA(userID, utils.GetMFASetupSecret(r), req.Code)
	if err != nil {
		writeMFADomainError(w, err, http.StatusBadRequest, "invalid_request", "Invalid authentication code.")
		return
	}
	_ = utils.ClearMFASetupSecret(w, r)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"recovery_codes": codes,
	})
}

// APIV1MeMFADisable handles POST /api/v2/me/mfa/disable.
func APIV1MeMFADisable(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	userID, ok := utils.GetAPIUserID(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	var req apiMFACodeRequest
	if err := decodeJSONBody(r, &req); err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
		return
	}
	if err := domain.DisableMFA(userID, req.Code); err != nil {
		writeMFADomainError(w, err, http.StatusBadRequest, "invalid_request", "Invalid authentication code.")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

// APIV1MeMFARecoveryCodes handles POST /api/v2/me/mfa/recovery-codes.
func APIV1MeMFARecoveryCodes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	userID, ok := utils.GetAPIUserID(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	var req apiMFACodeRequest
	if err := decodeJSONBody(r, &req); err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
		return
	}
	codes, err := domain.RegenerateRecoveryCodes(userID, req.Code)
	if err != nil {
		writeMFADomainError(w, err, http.StatusBadRequest, "invalid_request", "Invalid authentication code.")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"recovery_codes": codes,
	})
}
