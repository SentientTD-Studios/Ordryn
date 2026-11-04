package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"GoTodo/internal/domain"
	"GoTodo/internal/mailer"
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"

	"golang.org/x/crypto/bcrypt"
)

type forgotPasswordRequest struct {
	Email        string `json:"email"`
	ConfirmEmail string `json:"confirm_email"`
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

	// Everything that depends on whether the account exists happens after the
	// response, so the reply takes the same time for every address.
	resetPage := utils.AbsoluteURLForRequest(r, "/reset-password")
	go sendPasswordResetEmail(email, resetPage)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

// sendPasswordResetEmail mails a reset link when email belongs to a real account.
// It runs in the background and never reports back to the requester.
func sendPasswordResetEmail(email, resetPage string) {
	defer func() {
		if rec := recover(); rec != nil {
			fmt.Printf("forgot-password: recovered from panic: %v\n", rec)
		}
	}()
	user, err := storage.GetUserByEmail(email)
	if err != nil || user == nil || storage.IsAgentEmail(user.Email) {
		return
	}
	// Send to the address on the account, whatever case the request used.
	email = user.Email
	resetToken, err := storage.GenerateResetToken(user.ID, email)
	if err != nil {
		fmt.Printf("forgot-password: could not create reset token: %v\n", err)
		return
	}
	resetLink := fmt.Sprintf("%s?token=%s&id=%s", resetPage, resetToken.Token, resetToken.ID)

	siteName := "GoTodo"
	settings, err := storage.GetSiteSettings()
	if err == nil && settings != nil && settings.SiteName != "" {
		siteName = settings.SiteName
	}

	subject := fmt.Sprintf("%s - Password Reset Request", siteName)
	body := fmt.Sprintf(`Hello,

You requested to reset your password. Open this link to choose a new password:

%s

This link will expire in 15 minutes.

If you did not request this password reset, please ignore this email.
`, resetLink)

	_ = mailer.SendEmail(storage.SiteEmailConfig(settings), mailer.TriggerPasswordReset, subject, body, email)
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
	if err := domain.ValidatePassword(newPassword); err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", passwordPolicyMessage(err))
		return
	}

	// Check the token before spending bcrypt time on a request that can't succeed.
	if _, err := storage.ValidateResetToken(id, token); err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid or expired reset token.")
		return
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Internal server error.")
		return
	}

	// Uses up the token and sets the password together, so the token works once.
	email, err := storage.ResetPasswordWithToken(id, token, string(hashedPassword))
	if errors.Is(err, storage.ErrResetTokenInvalid) {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid or expired reset token.")
		return
	}
	if err != nil {
		fmt.Printf("reset-password: %v\n", err)
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Internal server error.")
		return
	}

	siteName := "GoTodo"
	settings, err := storage.GetSiteSettings()
	if err == nil && settings != nil && settings.SiteName != "" {
		siteName = settings.SiteName
	}
	subject := fmt.Sprintf("%s - Password Changed", siteName)
	body := fmt.Sprintf(`Hello,

Your password has been changed for %s.

If you did not request this, please reach out to support.
`, siteName)
	_ = mailer.SendEmail(storage.SiteEmailConfig(settings), mailer.TriggerPasswordChanged, subject, body, email)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

// passwordPolicyMessage turns a domain.ValidatePassword error into a sentence for the user.
func passwordPolicyMessage(err error) string {
	msg := sharingClientMessage(err, "Password does not meet the requirements.")
	if msg == "" {
		return msg
	}
	msg = strings.ToUpper(msg[:1]) + msg[1:]
	if !strings.HasSuffix(msg, ".") {
		msg += "."
	}
	return msg
}
