package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"

	"golang.org/x/crypto/bcrypt"
)

type forgotPasswordRequest struct {
	Email         string `json:"email"`
	ConfirmEmail  string `json:"confirm_email"`
}

type resetPasswordRequest struct {
	ID              string `json:"id"`
	Token           string `json:"token"`
	NewPassword     string `json:"new_password"`
	ConfirmPassword string `json:"confirm_password"`
}

// APIV1ForgotPassword sends a reset email when the account exists.
func APIV1ForgotPassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}

	var req forgotPasswordRequest
	if err := decodeJSONBody(r, &req); err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
		return
	}

	email := strings.TrimSpace(req.Email)
	confirmEmail := strings.TrimSpace(req.ConfirmEmail)
	if email == "" || confirmEmail == "" {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Email fields are required.")
		return
	}
	if email != confirmEmail {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Emails do not match.")
		return
	}

	user, err := storage.GetUserByEmail(email)
	if err == nil && user != nil {
		resetToken, err := storage.GenerateResetToken(email)
		if err == nil {
			resetLink := utils.AbsoluteURLForRequest(r,
				fmt.Sprintf("/reset-password?token=%s&id=%s", resetToken.Token, resetToken.ID))

			siteName := "GoTodo"
			if settings, err := storage.GetSiteSettings(); err == nil && settings != nil && settings.SiteName != "" {
				siteName = settings.SiteName
			}

			subject := fmt.Sprintf("%s - Password Reset Request", siteName)
			body := fmt.Sprintf(`Hello,

You requested to reset your password. Open this link to choose a new password:

%s

This link will expire in 15 minutes.

If you did not request this password reset, please ignore this email.
`, resetLink)

			_ = utils.SendEmail(subject, body, email)
		}
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

// APIV1ResetPasswordRouter handles GET validate + POST reset for password recovery.
func APIV1ResetPasswordRouter(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		apiV1ValidateResetToken(w, r)
	case http.MethodPost:
		apiV1ResetPassword(w, r)
	default:
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
	}
}

func apiV1ValidateResetToken(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if token == "" || id == "" {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "token and id are required.")
		return
	}
	reset, err := storage.ValidateResetToken(id, token)
	if err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid or expired reset token.")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"valid": true,
		"email": reset.Email,
	})
}

func apiV1ResetPassword(w http.ResponseWriter, r *http.Request) {
	var req resetPasswordRequest
	if err := decodeJSONBody(r, &req); err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
		return
	}

	id := strings.TrimSpace(req.ID)
	token := strings.TrimSpace(req.Token)
	newPassword := req.NewPassword
	confirmPassword := req.ConfirmPassword

	if id == "" || token == "" || newPassword == "" || confirmPassword == "" {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "All fields are required.")
		return
	}
	if newPassword != confirmPassword {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Passwords do not match.")
		return
	}
	if len(newPassword) < 8 {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Password must be at least 8 characters long.")
		return
	}

	reset, err := storage.ValidateResetToken(id, token)
	if err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid or expired reset token.")
		return
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Internal server error.")
		return
	}

	db, err := storage.OpenDatabase()
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Internal server error.")
		return
	}
	defer storage.CloseDatabase(db)

	if _, err := db.Exec(context.Background(), "UPDATE users SET password = $1 WHERE email = $2", string(hashedPassword), reset.Email); err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Internal server error.")
		return
	}

	siteName := "GoTodo"
	if settings, err := storage.GetSiteSettings(); err == nil && settings != nil && settings.SiteName != "" {
		siteName = settings.SiteName
	}
	subject := fmt.Sprintf("%s - Password Changed", siteName)
	body := fmt.Sprintf(`Hello,

Your password has been changed for %s.

If you did not request this, please reach out to support.
`, siteName)
	_ = utils.SendEmail(subject, body, reset.Email)
	_ = storage.DeleteResetToken(id, token)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}
