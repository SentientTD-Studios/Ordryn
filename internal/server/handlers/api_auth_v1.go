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

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
)

type apiAuthRegisterRequest struct {
	Email           string `json:"email"`
	Password        string `json:"password"`
	ConfirmPassword string `json:"confirm_password"`
	Timezone        string `json:"timezone"`
	InviteToken     string `json:"invite_token"`
	UserName        string `json:"user_name"`
}

type apiAuthLoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type apiUserMeJSON struct {
	ID                      int      `json:"id"`
	Email                   string   `json:"email"`
	UserName                string   `json:"user_name"`
	Timezone                string   `json:"timezone"`
	ItemsPerPage            int      `json:"items_per_page"`
	Permissions             []string `json:"permissions"`
	DigestEnabled           bool     `json:"digest_enabled"`
	DigestHour              int      `json:"digest_hour"`
	AllowProjectInvites     bool     `json:"allow_project_invites"`
	UsernameChangeAvailable bool     `json:"username_change_available"`
}

func profileToMeJSON(p *storage.UserProfile) apiUserMeJSON {
	perms := p.Permissions
	if perms == nil {
		perms = []string{}
	}
	return apiUserMeJSON{
		ID:                      p.ID,
		Email:                   p.Email,
		UserName:                p.UserName,
		Timezone:                p.Timezone,
		ItemsPerPage:            p.ItemsPerPage,
		Permissions:             perms,
		DigestEnabled:           p.DigestEnabled,
		DigestHour:              p.DigestHour,
		AllowProjectInvites:     p.AllowProjectInvites,
		UsernameChangeAvailable: p.UsernameChangeAvailable,
	}
}

func writeAPIUserJSON(w http.ResponseWriter, status int, p *storage.UserProfile) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(profileToMeJSON(p))
}

func establishSession(w http.ResponseWriter, r *http.Request, p *storage.UserProfile) error {
	session, err := sessionstore.GetSession(r)
	if err != nil {
		return err
	}
	session.Values["user_id"] = p.ID
	session.Values["email"] = p.Email
	session.Values["user_name"] = p.UserName
	session.Values["role_id"] = p.RoleID
	session.Values["permissions"] = p.Permissions
	session.Values["timezone"] = p.Timezone
	session.Values["items_per_page"] = p.ItemsPerPage
	sessionstore.ApplySecureCookieOptions(session)
	return session.Save(r, w)
}

// APIV1AuthRegister handles POST /api/v1/auth/register.
func APIV1AuthRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}

	var req apiAuthRegisterRequest
	if err := decodeJSONBody(r, &req); err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
		return
	}
	email := strings.TrimSpace(req.Email)
	timezone := strings.TrimSpace(req.Timezone)
	token := strings.TrimSpace(req.InviteToken)

	if email == "" || req.Password == "" || req.ConfirmPassword == "" || timezone == "" {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "All fields are required.")
		return
	}
	userName, err := domain.PrepareUsername(req.UserName)
	if err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if req.Password != req.ConfirmPassword {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Passwords do not match.")
		return
	}
	if !utils.IsValidTimezone(timezone) {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid timezone.")
		return
	}

	enableRegistration := true
	inviteOnly := true
	if settings, err := storage.GetSiteSettings(); err == nil && settings != nil {
		enableRegistration = settings.EnableRegistration
		inviteOnly = settings.InviteOnly
	}
	if !enableRegistration {
		utils.APIJSONError(w, http.StatusForbidden, "registration_disabled",
			"This site is currently not accepting new signups.")
		return
	}
	if inviteOnly && token == "" {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invite token is required.")
		return
	}

	exists, err := storage.UserExistsByEmail(email)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Internal server error.")
		return
	}
	if exists {
		if inviteOnly {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_invite",
				"Invalid email or invite token; please double check and try again.")
		} else {
			utils.APIJSONError(w, http.StatusConflict, "email_taken", "An account with this email already exists.")
		}
		return
	}

	taken, err := storage.UsernameTaken(userName, 0)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Internal server error.")
		return
	}
	if taken {
		utils.APIJSONError(w, http.StatusConflict, "username_taken", "That username is already taken.")
		return
	}

	inviteID := 0
	if inviteOnly {
		id, used, invErr := storage.LookupInvite(email, token)
		if invErr != nil {
			if errors.Is(invErr, pgx.ErrNoRows) {
				utils.APIJSONError(w, http.StatusBadRequest, "invalid_invite",
					"Invalid email or invite token; please double check and try again.")
				return
			}
			utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Internal server error.")
			return
		}
		if used {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_invite",
				"Invalid email or invite token; please double check and try again.")
			return
		}
		inviteID = id
	}

	roleID, err := storage.GetDefaultRoleID()
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Internal server error.")
		return
	}
	hashed, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Internal server error.")
		return
	}

	userID, err := storage.RegisterUser(email, string(hashed), timezone, userName, roleID, inviteID)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Internal server error.")
		return
	}

	profile, err := storage.GetUserProfileByID(userID)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Internal server error.")
		return
	}
	if err := establishSession(w, r, profile); err != nil {
		fmt.Printf("APIV1AuthRegister session: %v\n", err)
		utils.APIJSONError(w, http.StatusInternalServerError, "session_error", "Failed to create session.")
		return
	}
	writeAPIUserJSON(w, http.StatusCreated, profile)
}

// APIV1AuthLogin handles POST /api/v1/auth/login.
func APIV1AuthLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}

	var req apiAuthLoginRequest
	if err := decodeJSONBody(r, &req); err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
		return
	}
	email := strings.TrimSpace(req.Email)
	if email == "" || req.Password == "" {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Email and password are required.")
		return
	}

	blocked, err := utils.IsLoginBlocked(r.Context(), email, 5)
	if err != nil {
		fmt.Printf("APIV1AuthLogin block check: %v\n", err)
	}
	if blocked {
		utils.APIJSONError(w, http.StatusTooManyRequests, "rate_limit_exceeded",
			"Too many login attempts; please try again later.")
		return
	}

	hashedPassword, _, _, err := storage.GetAuthCredentials(email)
	if err != nil {
		if _, incErr := utils.IncrementFailedLogin(r.Context(), email, 900); incErr != nil {
			fmt.Printf("APIV1AuthLogin increment failed login: %v\n", incErr)
		}
		utils.APIJSONError(w, http.StatusUnauthorized, "invalid_credentials", "Invalid email or password.")
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(req.Password)) != nil {
		if _, incErr := utils.IncrementFailedLogin(r.Context(), email, 900); incErr != nil {
			fmt.Printf("APIV1AuthLogin increment failed login: %v\n", incErr)
		}
		utils.APIJSONError(w, http.StatusUnauthorized, "invalid_credentials", "Invalid email or password.")
		return
	}

	if banned, banErr := storage.IsUserBanned(email); banErr == nil && banned {
		utils.APIJSONError(w, http.StatusForbidden, "banned", "This account has been banned.")
		return
	}

	user, err := storage.GetUserByEmail(email)
	if err != nil || user == nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Internal server error.")
		return
	}
	profile, err := storage.GetUserProfileByID(user.ID)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Internal server error.")
		return
	}
	if err := establishSession(w, r, profile); err != nil {
		fmt.Printf("APIV1AuthLogin session: %v\n", err)
		utils.APIJSONError(w, http.StatusInternalServerError, "session_error", "Failed to create session.")
		return
	}
	if clearErr := utils.ClearFailedLogin(r.Context(), email); clearErr != nil {
		fmt.Printf("APIV1AuthLogin clear failed login: %v\n", clearErr)
	}
	writeAPIUserJSON(w, http.StatusOK, profile)
}

// APIV1AuthLogout handles POST /api/v1/auth/logout.
func APIV1AuthLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	sessionstore.ClearSessionCookie(w, r)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

// APIV1UsernameAvailable handles GET /api/v1/auth/username-available?username=
func APIV1UsernameAvailable(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	raw := r.URL.Query().Get("username")
	excludeID := 0
	if uid := utils.GetSessionUserID(r); uid != nil {
		excludeID = *uid
	}
	name, available, err := domain.UsernameAvailable(raw, excludeID)
	if err != nil {
		if errors.Is(err, domain.ErrValidation) {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"username":  domain.NormalizeUsername(raw),
				"available": false,
				"valid":     false,
				"message":   err.Error(),
			})
			return
		}
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Internal server error.")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"username":  name,
		"available": available,
		"valid":     true,
	})
}
