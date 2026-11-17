package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"GoTodo/internal/domain"
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
)

// passkeyRP describes this site to authenticators. PUBLIC_URL wins when set;
// otherwise the request's own origin is used (the browser checks that the page
// origin matches, so a forged Host header can't be used to sign in elsewhere).
func passkeyRP(r *http.Request) domain.PasskeyRP {
	origin := utils.PublicURL()
	if origin == "" {
		origin = utils.RequestScheme(r) + "://" + r.Host
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return domain.PasskeyRP{}
	}
	name := ""
	if s, err := storage.GetSiteSettings(); err == nil && s != nil {
		name = s.SiteName
	}
	return domain.PasskeyRP{ID: u.Hostname(), Origin: u.Scheme + "://" + u.Host, DisplayName: name}
}

type apiPasskeyJSON struct {
	ID         int     `json:"id"`
	Name       string  `json:"name"`
	CreatedAt  string  `json:"created_at"`
	LastUsedAt *string `json:"last_used_at"`
}

func passkeyToJSON(p storage.Passkey) apiPasskeyJSON {
	out := apiPasskeyJSON{ID: p.ID, Name: p.Name, CreatedAt: p.CreatedAt.UTC().Format(time.RFC3339)}
	if p.LastUsedAt != nil {
		s := p.LastUsedAt.UTC().Format(time.RFC3339)
		out.LastUsedAt = &s
	}
	return out
}

type apiPasskeyCeremonyJSON struct {
	Token   string          `json:"token"`
	Options json.RawMessage `json:"options"`
}

type apiPasskeyFinishRequest struct {
	Token      string          `json:"token"`
	Name       string          `json:"name"`
	Credential json.RawMessage `json:"credential"`
}

// APIV1AuthPasskeyBegin handles POST /api/v2/auth/passkey/begin.
func APIV1AuthPasskeyBegin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	opts, token, err := domain.BeginPasskeyLogin(r.Context(), passkeyRP(r))
	if err != nil {
		writeWorkflowDomainError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(apiPasskeyCeremonyJSON{Token: token, Options: opts})
}

// APIV1AuthPasskeyFinish handles POST /api/v2/auth/passkey/finish: verifies the
// passkey and signs in. A passkey proves possession and the user's PIN or
// biometric, so it counts as two-factor: no separate MFA step follows.
func APIV1AuthPasskeyFinish(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	var req apiPasskeyFinishRequest
	if err := decodeJSONBody(r, &req); err != nil || len(req.Credential) == 0 {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
		return
	}
	userID, err := domain.FinishPasskeyLogin(r.Context(), req.Token, req.Credential, passkeyRP(r))
	if err != nil {
		if errors.Is(err, domain.ErrPasskeyFailed) {
			utils.APIJSONError(w, http.StatusUnauthorized, "invalid_credentials", "That passkey couldn't be used to sign in.")
			return
		}
		writeWorkflowDomainError(w, err)
		return
	}
	if storage.IsUserBannedByID(userID) {
		utils.APIJSONError(w, http.StatusForbidden, "banned", "This account has been deactivated. Contact your site administrator.")
		return
	}
	if storage.SSOOnlyFor(userID) {
		utils.APIJSONError(w, http.StatusForbidden, "sso_required", "This site uses single sign-on. Use the sign-in button instead.")
		return
	}
	if verified, err := storage.IsEmailVerified(userID); err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Internal server error.")
		return
	} else if !verified && siteCanSendEmail() {
		utils.APIJSONError(w, http.StatusForbidden, "email_unverified",
			"Confirm your email address before signing in. Check your inbox for the link, or send a new one.")
		return
	}
	profile, err := storage.GetUserProfileByID(userID)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Internal server error.")
		return
	}
	if err := establishSessionVia(w, r, profile, utils.AuthMethodPasskey); err != nil {
		fmt.Printf("APIV1AuthPasskeyFinish session: %v\n", err)
		utils.APIJSONError(w, http.StatusInternalServerError, "session_error", "Failed to create session.")
		return
	}
	writeAPIUserJSON(w, http.StatusOK, profile)
}

// APIV1MePasskeys handles /api/v2/me/passkeys[/register/begin|/register/finish|/{id}].
func APIV1MePasskeys(w http.ResponseWriter, r *http.Request) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	// Passkeys belong to people signed in through the browser, not API keys.
	if utils.ExtractBearerTokenForHandlers(r) != "" {
		utils.APIJSONError(w, http.StatusForbidden, "forbidden", "Manage passkeys from a signed-in browser session.")
		return
	}
	rest := strings.Trim(strings.TrimPrefix(utils.TrimPublicPrefix(r.URL.Path), "/api/v2/me/passkeys"), "/")
	rest = strings.TrimPrefix(rest, "/")
	ctx := r.Context()
	switch {
	case rest == "" && r.Method == http.MethodGet:
		keys, err := domain.ListPasskeysForUser(ctx, userID)
		if err != nil {
			utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load passkeys.")
			return
		}
		out := make([]apiPasskeyJSON, 0, len(keys))
		for _, k := range keys {
			out = append(out, passkeyToJSON(k))
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(out)
	case rest == "register/begin" && r.Method == http.MethodPost:
		var req struct {
			Password string `json:"password"`
		}
		_ = decodeJSONBody(r, &req)
		opts, token, err := domain.BeginPasskeyRegistration(ctx, userID, req.Password, utils.SessionSignedInWithSSO(r), passkeyRP(r))
		if err != nil {
			writeWorkflowDomainError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(apiPasskeyCeremonyJSON{Token: token, Options: opts})
	case rest == "register/finish" && r.Method == http.MethodPost:
		var req apiPasskeyFinishRequest
		if err := decodeJSONBody(r, &req); err != nil || len(req.Credential) == 0 {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
			return
		}
		pk, err := domain.FinishPasskeyRegistration(ctx, userID, req.Token, req.Name, req.Credential, passkeyRP(r))
		if err != nil {
			writeWorkflowDomainError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(passkeyToJSON(*pk))
	default:
		id, err := strconv.Atoi(rest)
		if err != nil || id <= 0 || strings.Contains(rest, "/") {
			utils.APIJSONError(w, http.StatusNotFound, "not_found", "Not found.")
			return
		}
		switch r.Method {
		case http.MethodPatch:
			var req struct {
				Name string `json:"name"`
			}
			if err := decodeJSONBody(r, &req); err != nil {
				utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
				return
			}
			if err := domain.RenamePasskeyForUser(ctx, userID, id, req.Name); err != nil {
				writeWorkflowDomainError(w, err)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		case http.MethodDelete:
			if err := domain.DeletePasskeyForUser(ctx, userID, id); err != nil {
				writeWorkflowDomainError(w, err)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		}
	}
}
