package handlers

import (
	"GoTodo/internal/server/utils"
	"GoTodo/internal/sessionstore"
	"GoTodo/internal/storage"
	"context"
	"fmt"
	"net/http"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

func APILogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	err := r.ParseForm()
	if err != nil {
		http.Error(w, "Error parsing form data", http.StatusBadRequest)
		return
	}

	email := strings.TrimSpace(r.FormValue("email"))
	password := r.FormValue("password")

	if email == "" || password == "" {
		w.Header().Set("HX-Retarget", "#login-error")
		w.Header().Set("HX-Reswap", "innerHTML")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "Email and password are required.")
		return
	}

	// Check per-account failed-login lockout (threshold 5 attempts)
	blocked, err := utils.IsLoginBlocked(r.Context(), email, 5)
	if err != nil {
		fmt.Printf("Error checking login block: %v\n", err)
	}

	if blocked {
		w.Header().Set("HX-Retarget", "#login-error")
		w.Header().Set("HX-Reswap", "innerHTML")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "Too many login attempts; please try again later")
		return
	}

	db, err := storage.OpenDatabase()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "Internal server error.")
		return
	}
	defer storage.CloseDatabase(db)

	var hashedPassword string
	var roleID int
	var timezone string
	row := db.QueryRow(context.Background(), "SELECT password, role_id, COALESCE(timezone, 'America/New_York') FROM users WHERE email = $1", email)
	err = row.Scan(&hashedPassword, &roleID, &timezone)
	if err != nil {
		// Increment failed-login counter for this email
		if _, incErr := utils.IncrementFailedLogin(r.Context(), email, 900); incErr != nil {
			fmt.Printf("Error incrementing failed-login counter: %v\n", incErr)
		}
		w.Header().Set("HX-Retarget", "#login-error")
		w.Header().Set("HX-Reswap", "innerHTML")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "Invalid username or password.")
		return
	}

	err = bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password))
	if err != nil {
		// Increment failed-login counter for this email
		if _, incErr := utils.IncrementFailedLogin(r.Context(), email, 900); incErr != nil {
			fmt.Printf("Error incrementing failed-login counter: %v\n", incErr)
		}
		w.Header().Set("HX-Retarget", "#login-error")
		w.Header().Set("HX-Reswap", "innerHTML")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "Invalid username or password.")
		return
	}

	// Check if user is banned before proceeding
	isBanned, errBan := storage.IsUserBanned(email)
	if errBan == nil && isBanned {
		w.Header().Set("HX-Retarget", "#login-error")
		w.Header().Set("HX-Reswap", "innerHTML")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "This account has been banned.")
		return
	}

	permissions, err := storage.GetPermissionsByRoleID(roleID)
	if err != nil {
		fmt.Printf("Error fetching permissions: %v\n", err)
		permissions = []string{}
	}

	session, err := sessionstore.Store.Get(r, "session")
	if err != nil {
		// If cookie has an expired securecookie timestamp, clear it and retry
		if strings.Contains(err.Error(), "securecookie: expired timestamp") {
			sessionstore.ClearSessionCookie(w, r)
			// Try to get a fresh session after clearing
			session, err = sessionstore.Store.Get(r, "session")
			if err != nil {
				fmt.Printf("Error getting session after clearing cookie: %v\n", err)
				w.WriteHeader(http.StatusInternalServerError)
				fmt.Fprint(w, "Session error.")
				return
			}
		} else {
			fmt.Printf("Error getting session: %v\n", err)
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, "Session error.")
			return
		}
	}

	// Fetch user_name for session
	user, err := storage.GetUserByEmail(email)
	if err == nil && user != nil {
		session.Values["user_name"] = user.UserName
		// Store user ID in session so handlers can use it directly without extra DB lookups
		session.Values["user_id"] = user.ID
		// Store user's default items per page in session
		session.Values["items_per_page"] = user.ItemsPerPage
	} else {
		session.Values["user_name"] = ""
	}
	session.Values["email"] = email
	session.Values["role_id"] = roleID
	session.Values["permissions"] = permissions
	session.Values["timezone"] = timezone

	// Successful login — clear failed-login counter
	if clearErr := utils.ClearFailedLogin(r.Context(), email); clearErr != nil {
		fmt.Printf("Error clearing failed-login counter: %v\n", clearErr)
	}

	err = session.Save(r, w)
	if err != nil {
		fmt.Printf("Error saving session: %v\n", err)
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "Failed to save session.")
		return
	}

	basePath := utils.GetBasePath()

	redirect := basePath
	if returnTo := utils.SafeDeviceReturnTo(r.FormValue("return_to")); returnTo != "" {
		redirect = returnTo
	}

	w.Header().Set("HX-Trigger", "login-success")
	w.Header().Set("HX-Redirect", redirect)
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, " ")
}
