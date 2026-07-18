package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"GoTodo/internal/domain"
	"GoTodo/internal/server/utils"
	"GoTodo/internal/sessionstore"
	"GoTodo/internal/storage"
)

type apiMePatchRequest struct {
	UserName      *string `json:"user_name"`
	Timezone      *string `json:"timezone"`
	ItemsPerPage  *int    `json:"items_per_page"`
	DigestEnabled *bool   `json:"digest_enabled"`
	DigestHour    *int    `json:"digest_hour"`
}

type apiChangePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
	ConfirmPassword string `json:"confirm_password"`
}

// APIV1Me handles GET/PATCH /api/v1/me.
func APIV1Me(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		apiV1GetMe(w, r)
	case http.MethodPatch:
		apiV1PatchMe(w, r)
	default:
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
	}
}

func apiV1GetMe(w http.ResponseWriter, r *http.Request) {
	userID, ok := utils.GetAPIUserID(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	profile, err := storage.GetUserProfileByID(userID)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Internal server error.")
		return
	}
	writeAPIUserJSON(w, http.StatusOK, profile)
}

func apiV1PatchMe(w http.ResponseWriter, r *http.Request) {
	userID, ok := utils.GetAPIUserID(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	var req apiMePatchRequest
	if err := decodeJSONBody(r, &req); err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
		return
	}
	current, err := storage.GetUserProfileByID(userID)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Internal server error.")
		return
	}
	in := domain.UpdateProfileInput{
		UserName:      current.UserName,
		Timezone:      current.Timezone,
		ItemsPerPage:  current.ItemsPerPage,
		DigestEnabled: current.DigestEnabled,
		DigestHour:    current.DigestHour,
	}
	if req.UserName != nil {
		in.UserName = *req.UserName
	}
	if req.Timezone != nil {
		in.Timezone = *req.Timezone
	}
	if req.ItemsPerPage != nil {
		in.ItemsPerPage = *req.ItemsPerPage
	}
	if req.DigestEnabled != nil {
		in.DigestEnabled = *req.DigestEnabled
	}
	if req.DigestHour != nil {
		in.DigestHour = *req.DigestHour
	}

	profile, err := domain.UpdateProfile(r.Context(), userID, in, utils.IsValidTimezone(in.Timezone), utils.ValidItemsPerPage(in.ItemsPerPage))
	if err != nil {
		if errors.Is(err, domain.ErrValidation) {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to update profile.")
		return
	}
	refreshSessionProfile(w, r, profile)
	writeAPIUserJSON(w, http.StatusOK, profile)
}

// APIV1ChangePassword handles POST /api/v1/me/password.
func APIV1ChangePassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	userID, ok := utils.GetAPIUserID(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	var req apiChangePasswordRequest
	if err := decodeJSONBody(r, &req); err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
		return
	}
	if err := domain.ChangePassword(r.Context(), userID, req.CurrentPassword, req.NewPassword, req.ConfirmPassword); err != nil {
		if errors.Is(err, domain.ErrValidation) {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to change password.")
		return
	}

	profile, err := storage.GetUserProfileByID(userID)
	if err == nil && profile != nil {
		siteName := "GoTodo"
		if settings, serr := storage.GetSiteSettings(); serr == nil && settings != nil && settings.SiteName != "" {
			siteName = settings.SiteName
		}
		subject := fmt.Sprintf("%s - Password Changed", siteName)
		body := fmt.Sprintf(`Hello,

Your password has been changed for %s

If you did not request this, please reach out to support.

This email cannot receive replies. Please do not reply to this email.
`, siteName)
		if err := utils.SendEmail(subject, body, profile.Email); err != nil {
			fmt.Printf("Warning: Failed to send password changed email to %s: %v\n", profile.Email, err)
		}
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func refreshSessionProfile(w http.ResponseWriter, r *http.Request, p *storage.UserProfile) {
	if p == nil {
		return
	}
	if utils.GetSessionUserID(r) == nil {
		return
	}
	session, err := sessionstore.Store.Get(r, "session")
	if err != nil {
		if strings.Contains(err.Error(), "securecookie: expired timestamp") {
			sessionstore.ClearSessionCookie(w, r)
			session, err = sessionstore.Store.Get(r, "session")
		}
		if err != nil {
			return
		}
	}
	session.Values["user_name"] = p.UserName
	session.Values["timezone"] = p.Timezone
	session.Values["items_per_page"] = p.ItemsPerPage
	_ = session.Save(r, w)
}
