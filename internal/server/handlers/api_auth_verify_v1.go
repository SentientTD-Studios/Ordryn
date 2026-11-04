package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"GoTodo/internal/mailer"
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
)

// verificationRequired reports whether open self-registration must confirm the
// email first: only when the site can send email. Without email the old flow
// stays, and CheckRegistrationConfig warns at startup.
func verificationRequired(settings *storage.SiteSettings) bool {
	return settings != nil && mailer.Configured(storage.SiteEmailConfig(settings))
}

func siteDisplayName(settings *storage.SiteSettings) string {
	if settings != nil && strings.TrimSpace(settings.SiteName) != "" {
		return settings.SiteName
	}
	return "GoTodo"
}

// writeVerificationPending is the one response open registration gives, whether
// or not the email already has an account.
func writeVerificationPending(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"verification_required": true,
		"message":               "Check your email for a link to confirm your address and finish signing up.",
	})
}

// sendVerificationEmail issues a confirmation link and mails it. It runs in the
// background; failures are logged and recorded in the email audit.
func sendVerificationEmail(userID int, email, confirmPage string) {
	defer recoverBackground("verification email")
	v, err := storage.IssueEmailVerification(userID)
	if err != nil {
		fmt.Printf("verification email: could not issue link for user %d: %v\n", userID, err)
		return
	}
	settings, _ := storage.GetSiteSettings()
	site := siteDisplayName(settings)
	link := fmt.Sprintf("%s?id=%s&token=%s", confirmPage, v.ID, v.Token)
	subject := fmt.Sprintf("%s - Confirm your email", site)
	body := fmt.Sprintf(`Hello,

Confirm your email address to finish creating your %s account:

%s

This link expires in 48 hours. If you didn't sign up, ignore this email and no account will be activated.
`, site, link)
	_ = mailer.SendEmail(storage.SiteEmailConfig(settings), mailer.TriggerEmailVerification, subject, body, email)
}

// sendSignupExistingEmail tells the owner of an existing account that someone
// tried to register with their address, instead of telling the requester.
func sendSignupExistingEmail(email, signInPage, forgotPage string) {
	defer recoverBackground("existing-account email")
	settings, _ := storage.GetSiteSettings()
	site := siteDisplayName(settings)
	subject := fmt.Sprintf("%s - Sign-up attempt with your email", site)
	body := fmt.Sprintf(`Hello,

Someone tried to create a new %s account with this email address, but you already have one.

If it was you, sign in here:
%s

Forgot your password? Reset it here:
%s

If it wasn't you, you can ignore this email. Your account hasn't changed.
`, site, signInPage, forgotPage)
	_ = mailer.SendEmail(storage.SiteEmailConfig(settings), mailer.TriggerSignupExisting, subject, body, email)
}

func recoverBackground(what string) {
	if rec := recover(); rec != nil {
		fmt.Printf("%s: recovered from panic: %v\n", what, rec)
	}
}

type verifyEmailRequest struct {
	ID    string `json:"id"`
	Token string `json:"token"`
}

// APIV1VerifyEmail handles POST /api/v2/auth/verify-email. It confirms the email
// but doesn't sign in: the person signs in with their password next.
func APIV1VerifyEmail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	var req verifyEmailRequest
	if err := decodeJSONBody(r, &req); err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
		return
	}
	id := strings.TrimSpace(req.ID)
	token := strings.TrimSpace(req.Token)
	if id == "" || token == "" {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "id and token are required.")
		return
	}
	if _, err := storage.VerifyEmailWithToken(id, token); err != nil {
		if errors.Is(err, storage.ErrVerificationInvalid) {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_link",
				"This confirmation link is invalid or has expired. Request a new one below.")
			return
		}
		fmt.Printf("APIV1VerifyEmail: %v\n", err)
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Internal server error.")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

type resendVerificationRequest struct {
	Email string `json:"email"`
}

// APIV1ResendVerification handles POST /api/v2/auth/resend-verification. It always
// answers the same way and does the work in the background, like forgot-password.
func APIV1ResendVerification(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	var req resendVerificationRequest
	if err := decodeJSONBody(r, &req); err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
		return
	}
	email := strings.TrimSpace(req.Email)
	if email == "" {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Email is required.")
		return
	}
	confirmPage := utils.AbsoluteURLForRequest(r, "/verify-email")
	go resendVerification(email, confirmPage)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func resendVerification(email, confirmPage string) {
	defer recoverBackground("resend verification")
	user, err := storage.GetUserByEmail(email)
	if err != nil || user == nil {
		return
	}
	verified, err := storage.IsEmailVerified(user.ID)
	if err != nil || verified {
		return
	}
	sendVerificationEmail(user.ID, user.Email, confirmPage)
}

// CheckRegistrationConfig warns at startup when open registration runs without
// email: sign-up then can't confirm addresses and reveals which are taken.
func CheckRegistrationConfig() {
	settings, err := storage.GetSiteSettings()
	if err != nil || settings == nil {
		return
	}
	if mode, err := storage.GetSiteMode(); err == nil && mode == storage.SiteModeCompany {
		return
	}
	if settings.EnableRegistration && !settings.InviteOnly && !verificationRequired(settings) {
		fmt.Println("Warning: open registration is on but outbound email isn't configured, so new " +
			"accounts skip email confirmation and sign-up reveals whether an email is already registered. " +
			"Configure email in Admin → Email, or switch registration to invite-only.")
	}
}
