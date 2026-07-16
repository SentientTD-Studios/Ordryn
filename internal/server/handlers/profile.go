package handlers

import (
	"GoTodo/internal/server/utils"
	"GoTodo/internal/sessionstore"
	"GoTodo/internal/storage"
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// APIUpdateProfile updates the user's name, timezone, and pagination preference.
func APIUpdateProfile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	email, _, _, _, loggedIn, _ := utils.GetSessionUserWithTimezone(r)
	if !loggedIn {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if isBanned, err := storage.IsUserBanned(email); err == nil && isBanned {
		w.WriteHeader(http.StatusForbidden)
		return
	}

	userName := strings.TrimSpace(r.FormValue("user_name"))
	timezone := strings.TrimSpace(r.FormValue("timezone"))
	itemsPerPageStr := r.FormValue("items_per_page")
	digestEnabled := r.FormValue("digest_enabled") == "on" || r.FormValue("digest_enabled") == "true"
	digestHourStr := r.FormValue("digest_hour")

	if userName == "" {
		http.Error(w, "Name is required", http.StatusBadRequest)
		return
	}
	if !utils.IsValidTimezone(timezone) {
		http.Error(w, "Invalid timezone", http.StatusBadRequest)
		return
	}

	itemsPerPage := 0
	if itemsPerPageStr != "" {
		v, err := strconv.Atoi(itemsPerPageStr)
		if err != nil || !utils.ValidItemsPerPage(v) {
			http.Error(w, "Invalid items per page", http.StatusBadRequest)
			return
		}
		itemsPerPage = v
	}

	digestHour := 8
	if digestHourStr != "" {
		if v, err := strconv.Atoi(digestHourStr); err == nil && v >= 0 && v <= 23 {
			digestHour = v
		}
	}

	pool, err := storage.OpenDatabase()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer storage.CloseDatabase(pool)

	ctx := context.Background()
	if itemsPerPage > 0 {
		_, err = pool.Exec(ctx,
			`UPDATE users SET user_name = $1, timezone = $2, items_per_page = $3,
			 digest_enabled = $4, digest_hour = $5 WHERE email = $6`,
			userName, timezone, itemsPerPage, digestEnabled, digestHour, email)
	} else {
		_, err = pool.Exec(ctx,
			`UPDATE users SET user_name = $1, timezone = $2, digest_enabled = $3, digest_hour = $4 WHERE email = $5`,
			userName, timezone, digestEnabled, digestHour, email)
	}
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	session, err := sessionstore.Store.Get(r, "session")
	if err != nil {
		if strings.Contains(err.Error(), "securecookie: expired timestamp") {
			sessionstore.ClearSessionCookie(w, r)
			http.Redirect(w, r, utils.GetBasePath()+"/login", http.StatusSeeOther)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	session.Values["user_name"] = userName
	session.Values["timezone"] = timezone
	if itemsPerPage > 0 {
		session.Values["items_per_page"] = itemsPerPage
	}
	if err := session.Save(r, w); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// ProfilePage renders the user profile page.
func ProfilePage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	email, _, permissions, timezone, loggedIn, user_name := utils.GetSessionUserWithTimezone(r)
	if !loggedIn {
		http.Redirect(w, r, utils.GetBasePath()+"/", http.StatusSeeOther)
		return
	}

	status := r.URL.Query().Get("status")
	var statusMsg string
	if status == "success" {
		statusMsg = "Profile updated successfully!"
	}

	user, userErr := storage.GetUserByEmail(email)
	itemsPerPage := 15
	digestEnabled := false
	digestHour := 8
	userID := utils.GetSessionUserID(r)
	calendarFeedURL := ""
	calendarTokenErr := ""
	if user != nil && user.ItemsPerPage > 0 {
		itemsPerPage = user.ItemsPerPage
	}
	if userID != nil {
		if token, err := storage.GetOrCreateCalendarToken(*userID); err == nil {
			calendarFeedURL = calendarFeedURLForRequest(r, token)
		} else {
			calendarTokenErr = "Could not create calendar link. Try saving your profile or use Regenerate link below."
		}
	}
	if userErr != nil {
		calendarTokenErr = "Could not load profile data. Try again later."
	}

	pool, _ := storage.OpenDatabase()
	if pool != nil && userID != nil {
		_ = pool.QueryRow(context.Background(),
			`SELECT COALESCE(digest_enabled, false), COALESCE(digest_hour, 8) FROM users WHERE id = $1`,
			*userID).Scan(&digestEnabled, &digestHour)
	}

	csrfToken, _ := utils.EnsureCSRFToken(r, w)

	enableAPI := utils.IsAPIEnabled()
	apiKeys := []map[string]interface{}{}
	if enableAPI && userID != nil {
		apiKeys = ListAPIKeysForProfilePage(*userID)
	}

	context := map[string]interface{}{
		"UserEmail":         email,
		"Email":             email,
		"Timezone":          timezone,
		"Status":            statusMsg,
		"Name":              user_name,
		"ItemsPerPage":      itemsPerPage,
		"DigestEnabled":     digestEnabled,
		"DigestHour":        digestHour,
		"LoggedIn":          loggedIn,
		"Permissions":       permissions,
		"CalendarFeedURL":   calendarFeedURL,
		"CalendarTokenErr":  calendarTokenErr,
		"CSRFToken":         csrfToken,
		"AllowedTimezones":  utils.AllowedTimezones,
		"EnableAPI":         enableAPI,
		"APIKeys":           apiKeys,
		"RedisAvailable":    utils.RedisAvailable(),
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := utils.RenderTemplate(w, r, "profile.html", context); err != nil {
		http.Error(w, "Error rendering template: "+err.Error(), http.StatusInternalServerError)
	}
}

// APIChangePassword allows a user to change their password.
func APIChangePassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	email, _, _, _, loggedIn, _ := utils.GetSessionUserWithTimezone(r)
	if !loggedIn {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if isBanned, err := storage.IsUserBanned(email); err == nil && isBanned {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	currentPassword := r.FormValue("current_password")
	newPassword := r.FormValue("new_password")
	confirmPassword := r.FormValue("confirm_password")

	if currentPassword == "" || newPassword == "" || confirmPassword == "" {
		http.Error(w, "All password fields are required", http.StatusBadRequest)
		return
	}
	if newPassword != confirmPassword {
		http.Error(w, "New passwords do not match", http.StatusBadRequest)
		return
	}
	if len(newPassword) < 8 {
		http.Error(w, "New password must be at least 8 characters long", http.StatusBadRequest)
		return
	}

	pool, err := storage.OpenDatabase()
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer storage.CloseDatabase(pool)

	var currentHashedPassword string
	err = pool.QueryRow(context.Background(), "SELECT password FROM users WHERE email = $1", email).Scan(&currentHashedPassword)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if err = bcrypt.CompareHashAndPassword([]byte(currentHashedPassword), []byte(currentPassword)); err != nil {
		http.Error(w, "Current password is incorrect", http.StatusBadRequest)
		return
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	_, err = pool.Exec(context.Background(), "UPDATE users SET password = $1 WHERE email = $2", string(hashedPassword), email)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	siteName := "GoTodo"
	if settings, err := storage.GetSiteSettings(); err == nil && settings != nil && settings.SiteName != "" {
		siteName = settings.SiteName
	}
	subject := fmt.Sprintf("%s - Password Changed", siteName)
	body := fmt.Sprintf(`Hello,

Your password has been changed for %s

If you did not request this, please reach out to support.

This email cannot receive replies. Please do not reply to this email.
`, siteName)

	if err = utils.SendEmail(subject, body, email); err != nil {
		fmt.Printf("Warning: Failed to send password changed email to %s: %v\n", email, err)
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "success")
}
