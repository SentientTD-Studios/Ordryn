package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"GoTodo/internal/domain"
	"GoTodo/internal/server/utils"
	"GoTodo/internal/sessionstore"
	"GoTodo/internal/storage"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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
	FirstName               string   `json:"first_name"`
	LastName                string   `json:"last_name"`
	Timezone                string   `json:"timezone"`
	ItemsPerPage            int      `json:"items_per_page"`
	Permissions             []string `json:"permissions"`
	AllowProjectInvites     bool     `json:"allow_project_invites"`
	HomeSplitView           bool     `json:"home_split_view"`
	UsernameChangeAvailable bool     `json:"username_change_available"`
	MFAEnabled              bool     `json:"mfa_enabled"`
	AvatarURL               string   `json:"avatar_url"`
	// ManagesPeople is true when the user gets the People page: they manage accounts or names.
	ManagesPeople bool `json:"manages_people"`
	// IsSiteOwner marks the one account above admins.
	IsSiteOwner bool `json:"is_site_owner"`
	// CanEditName is true in company mode for those allowed to set their own name.
	CanEditName bool `json:"can_edit_name"`
	// IsGuest marks a company-mode guest account.
	IsGuest bool `json:"is_guest"`
}

func profileToMeJSON(p *storage.UserProfile) apiUserMeJSON {
	perms := p.Permissions
	if perms == nil {
		perms = []string{}
	}
	access, _ := domain.PeopleAccessFor(p.ID)
	return apiUserMeJSON{
		ID:                      p.ID,
		Email:                   p.Email,
		UserName:                p.UserName,
		FirstName:               p.FirstName,
		LastName:                p.LastName,
		Timezone:                p.Timezone,
		ItemsPerPage:            p.ItemsPerPage,
		Permissions:             perms,
		AllowProjectInvites:     p.AllowProjectInvites,
		HomeSplitView:           p.HomeSplitView,
		UsernameChangeAvailable: p.UsernameChangeAvailable,
		MFAEnabled:              p.MFAEnabled,
		AvatarURL:               p.AvatarURL,
		ManagesPeople:           access.SeesPeople(),
		IsSiteOwner:             access.Owner,
		CanEditName:             domain.CanEditNames(p.ID, access, p.ID),
		IsGuest:                 storage.IsGuestUser(p.ID),
	}
}

func writeAPIUserJSON(w http.ResponseWriter, status int, p *storage.UserProfile) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(profileToMeJSON(p))
}

// establishSession signs p in on this browser. Each sign-in gets a fresh session
// id and records the account's session version, so it can be revoked later.
func establishSession(w http.ResponseWriter, r *http.Request, p *storage.UserProfile) error {
	session, err := sessionstore.GetSession(r)
	if err != nil {
		return err
	}
	sid, err := utils.NewSessionID()
	if err != nil {
		return err
	}
	session.Values = map[interface{}]interface{}{
		utils.SessionIDKey:      sid,
		utils.SessionVersionKey: p.SessionVersion,
		"user_id":               p.ID,
		"email":                 p.Email,
		"user_name":             p.UserName,
		"role_id":               p.RoleID,
		"permissions":           p.Permissions,
		"timezone":              p.Timezone,
		"items_per_page":        p.ItemsPerPage,
	}
	if session.Options == nil && sessionstore.Store != nil && sessionstore.Store.Options != nil {
		opts := *sessionstore.Store.Options
		session.Options = &opts
	}
	if session.Options != nil {
		session.Options.MaxAge = utils.SessionMaxAge()
		session.Options.Path = "/"
		session.Options.HttpOnly = true
		session.Options.SameSite = http.SameSiteLaxMode
	}
	sessionstore.ApplySecureCookieOptions(session)
	return session.Save(r, w)
}

// APIV1AuthRegister handles POST /api/v2/auth/register.
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
	if sso, err := storage.GetSSOSettings(); err == nil && sso.Configured() && sso.SSOOnly {
		utils.APIJSONError(w, http.StatusForbidden, "sso_required", "This site uses single sign-on. Sign in with it to get an account.")
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
	if err := domain.ValidatePassword(req.Password); err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", passwordPolicyMessage(err))
		return
	}
	if !utils.IsValidTimezone(timezone) {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid timezone.")
		return
	}

	if required, err := storage.SetupRequired(); err != nil || required {
		utils.APIJSONError(w, http.StatusConflict, "setup_required",
			"This site hasn't been set up yet. Finish first-time setup to create the admin account.")
		return
	}
	if mode, err := storage.GetSiteMode(); err == nil && mode == storage.SiteModeCompany {
		utils.APIJSONError(w, http.StatusForbidden, "registration_disabled",
			"Accounts on this site are created by its admins and team managers.")
		return
	}

	enableRegistration := true
	inviteOnly := true
	settings, settingsErr := storage.GetSiteSettings()
	if settingsErr != nil {
		settings = nil
	}
	if settings != nil {
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
	// Open sign-up on a site that can send email: confirm the address first, and
	// never say whether it's already registered.
	if !inviteOnly && token == "" && verificationRequired(settings) {
		registerWithVerification(w, r, email, userName, req.Password, timezone)
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
	if inviteOnly || token != "" {
		id, used, expiresAt, invErr := storage.LookupInvite(email, token)
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
		if expiresAt != nil && time.Now().After(*expiresAt) {
			utils.APIJSONError(w, http.StatusBadRequest, "invite_expired",
				"This invite has expired. Please ask for a new invite.")
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

// signupMailPerHour is how many sign-ups that send email one client may make per hour.
const signupMailPerHour = 5

// registerWithVerification creates an unconfirmed account and emails a link, or,
// when the email already has an account, emails its owner instead. Both give the
// same response and do the same expensive work, so neither the reply nor its
// timing reveals which happened. Usernames are public, so a taken one is reported.
func registerWithVerification(w http.ResponseWriter, r *http.Request, email, userName, password, timezone string) {
	// Every request here sends an email, so limit how often one client can do it.
	allowed, err := utils.AllowRequest(context.WithoutCancel(r.Context()), utils.RedisClient,
		"rl:tb:signup-mail:"+utils.ClientIP(r), signupMailPerHour, signupMailPerHour/3600.0, 1, 3600)
	if err != nil {
		fmt.Printf("sign-up limit check: %v\n", err)
	} else if !allowed {
		utils.APIJSONError(w, http.StatusTooManyRequests, "rate_limit_exceeded",
			"Too many sign-ups from your network. Try again later.")
		return
	}

	existing, err := storage.GetUserByEmail(email)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Internal server error.")
		return
	}
	unconfirmed := false
	if existing != nil {
		verified, verr := storage.IsEmailVerified(existing.ID)
		if verr != nil {
			utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Internal server error.")
			return
		}
		unconfirmed = !verified
	}
	// A repeat sign-up replaces an unconfirmed account's username, so that account
	// doesn't count as holding it.
	excludeID := 0
	if unconfirmed {
		excludeID = existing.ID
	}
	taken, err := storage.UsernameTaken(userName, excludeID)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Internal server error.")
		return
	}
	if taken {
		utils.APIJSONError(w, http.StatusConflict, "username_taken", "That username is already taken.")
		return
	}
	roleID, err := storage.GetDefaultRoleID()
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Internal server error.")
		return
	}
	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Internal server error.")
		return
	}
	if existing != nil {
		replaced := false
		if unconfirmed {
			// Signing up again before confirming: take this sign-up's details and
			// send a fresh link. The first sign-up may not have been the owner's,
			// and confirming also needs the password, so only whoever has both the
			// inbox and the latest password can activate the account.
			replaced, err = storage.ReplaceUnverifiedRegistration(existing.ID, string(hashed), timezone, userName)
			if err != nil {
				var pgErr *pgconn.PgError
				if errors.As(err, &pgErr) && pgErr.Code == "23505" {
					utils.APIJSONError(w, http.StatusConflict, "username_taken", "That username is already taken.")
					return
				}
				fmt.Printf("APIV1AuthRegister replace unconfirmed: %v\n", err)
				utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Internal server error.")
				return
			}
		}
		if replaced {
			go sendVerificationEmail(existing.ID, existing.Email, utils.AbsoluteURLForRequest(r, "/verify-email"))
		} else {
			go sendSignupExistingEmail(existing.Email,
				utils.AbsoluteURLForRequest(r, "/login"),
				utils.AbsoluteURLForRequest(r, "/forgot-password"))
		}
		writeVerificationPending(w)
		return
	}

	userID, err := storage.RegisterUnverifiedUser(email, string(hashed), timezone, userName, roleID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			if strings.Contains(pgErr.ConstraintName, "user_name") {
				utils.APIJSONError(w, http.StatusConflict, "username_taken", "That username is already taken.")
				return
			}
			// A concurrent sign-up took the email: answer as if it had existed.
			writeVerificationPending(w)
			return
		}
		fmt.Printf("APIV1AuthRegister: %v\n", err)
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Internal server error.")
		return
	}
	go sendVerificationEmail(userID, storage.NormalizeEmail(email), utils.AbsoluteURLForRequest(r, "/verify-email"))
	writeVerificationPending(w)
}

// APIV1AuthLogin handles POST /api/v2/auth/login.
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

	if !loginGateAllows(w, r, email) {
		return
	}

	hashedPassword, _, _, err := storage.GetAuthCredentials(email)
	// AI agent accounts have no password and only ever use their API keys.
	if err == nil && storage.IsAgentEmail(email) {
		err = errors.New("agent accounts cannot sign in")
	}
	if err != nil {
		// No real compare ran; spend the same time so a fast reply doesn't
		// reveal that the account doesn't exist.
		domain.SpendPasswordCheckTime(req.Password)
		recordFailedLogin(r, email)
		utils.APIJSONError(w, http.StatusUnauthorized, "invalid_credentials", "Invalid email or password.")
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(req.Password)) != nil {
		recordFailedLogin(r, email)
		utils.APIJSONError(w, http.StatusUnauthorized, "invalid_credentials", "Invalid email or password.")
		return
	}

	if banned, banErr := storage.IsUserBanned(email); banErr == nil && banned {
		utils.APIJSONError(w, http.StatusForbidden, "banned", "This account has been deactivated. Contact your site administrator.")
		return
	}

	user, err := storage.GetUserByEmail(email)
	if err != nil || user == nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Internal server error.")
		return
	}
	// Only reached with the right password. Admins keep password sign-in so a
	// broken identity provider can't lock everyone out.
	if storage.SSOOnlyFor(user.ID) {
		utils.APIJSONError(w, http.StatusForbidden, "sso_required", "This site uses single sign-on. Use the sign-in button instead.")
		return
	}
	// Only reached with the right password, so this reveals nothing to a guesser.
	// Enforced only while the site can send email: otherwise there'd be no way to
	// get a confirmation link, and the account would be locked out for good.
	if verified, err := storage.IsEmailVerified(user.ID); err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Internal server error.")
		return
	} else if !verified && siteCanSendEmail() {
		utils.APIJSONError(w, http.StatusForbidden, "email_unverified",
			"Confirm your email address before signing in. Check your inbox for the link, or send a new one.")
		return
	}
	profile, err := storage.GetUserProfileByID(user.ID)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Internal server error.")
		return
	}
	mfaEnabled, mfaErr := storage.UserHasMFA(user.ID)
	if mfaErr != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Internal server error.")
		return
	}
	if mfaEnabled {
		if err := utils.EstablishPendingMFASession(w, r, user.ID, email); err != nil {
			fmt.Printf("APIV1AuthLogin pending MFA session: %v\n", err)
			utils.APIJSONError(w, http.StatusInternalServerError, "session_error", "Failed to create session.")
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]bool{"mfa_required": true})
		return
	}
	if err := establishSession(w, r, profile); err != nil {
		fmt.Printf("APIV1AuthLogin session: %v\n", err)
		utils.APIJSONError(w, http.StatusInternalServerError, "session_error", "Failed to create session.")
		return
	}
	if clearErr := utils.ClearFailedLogin(r.Context(), email, utils.ClientIP(r)); clearErr != nil {
		fmt.Printf("APIV1AuthLogin clear failed login: %v\n", clearErr)
	}
	writeAPIUserJSON(w, http.StatusOK, profile)
}

// loginGateAllows checks the sign-in limits for email from this client and, when
// the attempt can't proceed, writes the 429 and returns false. Redis errors fail
// open: a Redis outage shouldn't stop everyone signing in.
func loginGateAllows(w http.ResponseWriter, r *http.Request, email string) bool {
	gate, err := utils.CheckLogin(r.Context(), email, utils.ClientIP(r))
	if err != nil {
		fmt.Printf("sign-in limit check: %v\n", err)
	}
	switch gate {
	case utils.LoginLocked:
		utils.APIJSONError(w, http.StatusTooManyRequests, "rate_limit_exceeded",
			"Too many login attempts; please try again later.")
		return false
	case utils.LoginSlowedDown:
		w.Header().Set("Retry-After", strconv.Itoa(utils.LoginSlowdownSeconds))
		utils.APIJSONError(w, http.StatusTooManyRequests, "rate_limit_exceeded",
			"Too many failed sign-ins from your network. Wait a few seconds and try again.")
		return false
	}
	return true
}

// recordFailedLogin counts a failed sign-in against the email and the client IP.
func recordFailedLogin(r *http.Request, email string) {
	if err := utils.RecordFailedLogin(r.Context(), email, utils.ClientIP(r)); err != nil {
		fmt.Printf("record failed login: %v\n", err)
	}
}

// APIV1AuthLogout handles POST /api/v2/auth/logout.
func APIV1AuthLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	// Revoke this sign-in on the server too, so a copy of the cookie stops working.
	if sess, err := sessionstore.GetSession(r); err == nil {
		if err := utils.RevokeSessionID(r.Context(), utils.SessionStringValue(sess, utils.SessionIDKey)); err != nil {
			fmt.Printf("APIV1AuthLogout revoke session: %v\n", err)
		}
	}
	sessionstore.ClearSessionCookie(w, r)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

// APIV1UsernameAvailable handles GET /api/v2/auth/username-available?username=
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
