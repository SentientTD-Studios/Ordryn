package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"GoTodo/internal/config"
	"GoTodo/internal/domain"
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
)

type apiSetupRequest struct {
	SetupToken      string `json:"setup_token"`
	Email           string `json:"email"`
	UserName        string `json:"user_name"`
	Password        string `json:"password"`
	ConfirmPassword string `json:"confirm_password"`
	Timezone        string `json:"timezone"`
	SiteName        string `json:"site_name"`
	SiteMode        string `json:"site_mode"`
	Registration    string `json:"registration"`
	EnableAPI       bool   `json:"enable_api"`
}

// APIV1Setup handles GET and POST /api/v2/setup (first-time install).
// GET reports whether setup is still required. POST creates the first admin and
// the initial site settings, then signs the admin in.
func APIV1Setup(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		required, err := storage.SetupRequired()
		if err != nil {
			utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Internal server error.")
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]bool{"setup_required": required})
	case http.MethodPost:
		apiV1SetupComplete(w, r)
	default:
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
	}
}

func apiV1SetupComplete(w http.ResponseWriter, r *http.Request) {
	var req apiSetupRequest
	if err := decodeJSONBody(r, &req); err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
		return
	}
	timezone := strings.TrimSpace(req.Timezone)
	adminID, err := domain.CompleteSetup(r.Context(), domain.SetupInput{
		Token:           req.SetupToken,
		Email:           req.Email,
		UserName:        req.UserName,
		Password:        req.Password,
		ConfirmPassword: req.ConfirmPassword,
		Timezone:        timezone,
		SiteName:        req.SiteName,
		SiteMode:        req.SiteMode,
		Registration:    req.Registration,
		EnableAPI:       req.EnableAPI,
	}, utils.IsValidTimezone(timezone))
	switch {
	case errors.Is(err, domain.ErrConflict):
		utils.APIJSONError(w, http.StatusConflict, "setup_complete", "This site has already been set up. Sign in instead.")
		return
	case errors.Is(err, domain.ErrForbidden):
		utils.APIJSONError(w, http.StatusForbidden, "invalid_setup_token", sharingClientMessage(err, "Invalid setup token."))
		return
	case errors.Is(err, domain.ErrValidation):
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", sharingClientMessage(err, "Invalid request."))
		return
	case err != nil && adminID == 0:
		fmt.Printf("APIV1Setup: %v\n", err)
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Internal server error.")
		return
	case err != nil:
		// The admin exists; the rest can be finished in Admin, so sign them in anyway.
		fmt.Printf("APIV1Setup: admin %d created, but settings were not fully applied: %v\n", adminID, err)
	}

	if settings, err := storage.GetSiteSettings(); err == nil {
		config.Cfg.SiteName = settings.SiteName
		config.Cfg.DefaultTimezone = settings.DefaultTimezone
	}
	if logErr := storage.LogAdminEvent(adminID, "site_setup_completed", "site", 0, "", map[string]interface{}{
		"site_mode":    storage.NormalizeSiteMode(req.SiteMode),
		"registration": req.Registration,
		"enable_api":   req.EnableAPI,
	}); logErr != nil {
		fmt.Printf("audit: failed to log admin site_setup_completed: %v\n", logErr)
	}

	profile, err := storage.GetUserProfileByID(adminID)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Internal server error.")
		return
	}
	if err := establishSession(w, r, profile); err != nil {
		fmt.Printf("APIV1Setup session: %v\n", err)
		utils.APIJSONError(w, http.StatusInternalServerError, "session_error", "Failed to create session.")
		return
	}
	writeAPIUserJSON(w, http.StatusCreated, profile)
}
