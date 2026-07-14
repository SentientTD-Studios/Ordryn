package handlers

import (
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
	"context"
	"fmt"
	"net/http"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// ForgotPasswordPage renders the forgot password page
func ForgotPasswordPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	context := map[string]interface{}{}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := utils.RenderTemplate(w, r, "forgot_password.html", context); err != nil {
		http.Error(w, "Error rendering template: "+err.Error(), http.StatusInternalServerError)
	}
}

// APIForgotPassword processes the forgot password request
func APIForgotPassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	email := strings.TrimSpace(r.FormValue("email"))
	confirmEmail := strings.TrimSpace(r.FormValue("confirm_email"))

	// Validate emails are provided
	if email == "" || confirmEmail == "" {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "Email fields are required")
		return
	}

	// Validate emails match
	if email != confirmEmail {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "Emails do not match")
		return
	}

	// Check if user exists (but don't reveal this to the client)
	user, err := storage.GetUserByEmail(email)
	if err == nil && user != nil {
		// User exists - generate reset token
		resetToken, err := storage.GenerateResetToken(email)
		if err != nil {
			fmt.Printf("Error generating reset token: %v\n", err)
			// Still return success to not reveal user existence
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, "success")
			return
		}

		// Send reset email
		basePath := utils.GetBasePath()
		resetLink := fmt.Sprintf("%s/password-reset?token=%s&id=%s", basePath, resetToken.Token, resetToken.ID)

		siteName := "GoTodo"
		if settings, err := storage.GetSiteSettings(); err == nil && settings != nil && settings.SiteName != "" {
			siteName = settings.SiteName
		}

		subject := fmt.Sprintf("%s - Password Reset Request", siteName)
		body := fmt.Sprintf(`Hello,

You requested to reset your password. Click the link below to reset your password:

<a href="%s">%s</a>

This link will expire in 15 minutes.

If you did not request this password reset, please ignore this email.
`, resetLink, resetLink)

		err = utils.SendEmail(subject, body, email)
		if err != nil {
			fmt.Printf("Error sending reset email: %v\n", err)
			// Still return success to not reveal user existence
		}
	}

	// Always return success message (security best practice)
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "success")
}

// PasswordResetPage renders the password reset page
func PasswordResetPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	token := r.URL.Query().Get("token")
	id := r.URL.Query().Get("id")

	if token == "" || id == "" {
		context := map[string]interface{}{
			"InvalidToken": true,
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := utils.RenderTemplate(w, r, "password_reset.html", context); err != nil {
			http.Error(w, "Error rendering template: "+err.Error(), http.StatusInternalServerError)
		}
		return
	}

	// Validate token
	reset, err := storage.ValidateResetToken(id, token)
	if err != nil {
		context := map[string]interface{}{
			"InvalidToken": true,
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := utils.RenderTemplate(w, r, "password_reset.html", context); err != nil {
			http.Error(w, "Error rendering template: "+err.Error(), http.StatusInternalServerError)
		}
		return
	}

	// Token is valid - show reset form
	context := map[string]interface{}{
		"InvalidToken": false,
		"Email":        reset.Email,
		"Token":        token,
		"ID":           id,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := utils.RenderTemplate(w, r, "password_reset.html", context); err != nil {
		http.Error(w, "Error rendering template: "+err.Error(), http.StatusInternalServerError)
	}
}

// APIResetPassword processes the password reset
func APIResetPassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	id := r.FormValue("id")
	token := r.FormValue("token")
	newPassword := r.FormValue("new_password")
	confirmPassword := r.FormValue("confirm_password")

	// Validate all fields are provided
	if id == "" || token == "" || newPassword == "" || confirmPassword == "" {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "All fields are required")
		return
	}

	// Validate passwords match
	if newPassword != confirmPassword {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "Passwords do not match")
		return
	}

	// Validate password length
	if len(newPassword) < 8 {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "Password must be at least 8 characters long")
		return
	}

	// Validate reset token
	reset, err := storage.ValidateResetToken(id, token)
	if err != nil {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "Invalid or expired reset token")
		return
	}

	// Hash the new password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "Internal server error")
		return
	}

	// Update password in database
	db, err := storage.OpenDatabase()
	if err != nil {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "Internal server error")
		return
	}
	defer storage.CloseDatabase(db)

	_, err = db.Exec(context.Background(), "UPDATE users SET password = $1 WHERE email = $2", string(hashedPassword), reset.Email)
	if err != nil {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "Internal server error")
		return
	}

	// Send password changed email
	siteName := "GoTodo"
	if settings, err := storage.GetSiteSettings(); err == nil && settings != nil && settings.SiteName != "" {
		siteName = settings.SiteName
	}
	subject := fmt.Sprintf("%s - Password Changed", siteName)
	body := fmt.Sprintf(`Hello,

Your password has been changed for %s.

If you did not request this, please reach out to support.

This email cannot receive replies. Please do not reply to this email.
`, siteName)

	err = utils.SendEmail(subject, body, reset.Email)
	if err != nil {
		fmt.Printf("Warning: Failed to send password changed email to %s: %v\n", reset.Email, err)
		// Continue anyway - password was updated successfully
	}

	// Delete the used reset token
	err = storage.DeleteResetToken(id, token)
	if err != nil {
		fmt.Printf("Error deleting reset token: %v\n", err)
		// Continue anyway - password was updated successfully
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "success")
}
