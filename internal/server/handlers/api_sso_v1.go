package handlers

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"GoTodo/internal/crypto/secret"
	"GoTodo/internal/domain"
	"GoTodo/internal/server/utils"
	"GoTodo/internal/sessionstore"
	"GoTodo/internal/storage"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

const (
	ssoStateTTL    = 10 * time.Minute
	ssoStatePrefix = "sso:state:"
	ssoCallback    = "/api/v2/auth/sso/callback"
	ssoStateCookie = "ordryn_sso_state"
)

// ssoLabel is the text after "Sign in with" on the login page.
func ssoLabel(s *storage.SSOSettings) string {
	if s == nil || !s.Configured() {
		return ""
	}
	if s.Label != "" {
		return s.Label
	}
	return "SSO"
}

// Discovery documents rarely change; keep providers for an hour so a busy
// login page doesn't fetch /.well-known/openid-configuration every time.
var (
	ssoProviderMu    sync.Mutex
	ssoProviderCache = map[string]ssoCachedProvider{}
)

type ssoCachedProvider struct {
	p       *oidc.Provider
	fetched time.Time
}

func ssoProvider(ctx context.Context, issuer string) (*oidc.Provider, error) {
	ssoProviderMu.Lock()
	defer ssoProviderMu.Unlock()
	if c, ok := ssoProviderCache[issuer]; ok && time.Since(c.fetched) < time.Hour {
		return c.p, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	p, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, err
	}
	ssoProviderCache[issuer] = ssoCachedProvider{p: p, fetched: time.Now()}
	return p, nil
}

func ssoOAuthConfig(r *http.Request, s *storage.SSOSettings, p *oidc.Provider) (*oauth2.Config, error) {
	clientSecret, err := secret.Decrypt(s.ClientSecretEnc)
	if err != nil {
		return nil, err
	}
	scopes := strings.Fields(s.Scopes)
	if len(scopes) == 0 {
		scopes = strings.Fields(storage.DefaultSSOScopes)
	}
	return &oauth2.Config{
		ClientID:     s.ClientID,
		ClientSecret: clientSecret,
		Endpoint:     p.Endpoint(),
		RedirectURL:  utils.AbsoluteURLForRequest(r, ssoCallback),
		Scopes:       append([]string{oidc.ScopeOpenID}, scopes...),
	}, nil
}

type ssoPendingLogin struct {
	Nonce    string `json:"nonce"`
	Verifier string `json:"verifier"`
	Return   string `json:"return"`
}

// safeReturnPath keeps redirects on this site: a path, never another origin.
func safeReturnPath(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") || strings.Contains(raw, `\`) {
		return "/"
	}
	if u, err := url.Parse(raw); err != nil || u.Host != "" || u.Scheme != "" {
		return "/"
	}
	return raw
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// APIV1SSOStart handles GET /api/v2/auth/sso/start?return=/path: it redirects
// the browser to the identity provider.
func APIV1SSOStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	fail := func(msg string) { ssoRedirectError(w, r, msg) }
	s, err := storage.GetSSOSettings()
	if err != nil || !s.Configured() {
		fail("Single sign-on isn't set up on this site.")
		return
	}
	if utils.RedisClient == nil {
		fail("Single sign-on needs Redis.")
		return
	}
	p, err := ssoProvider(r.Context(), s.IssuerURL)
	if err != nil {
		fmt.Printf("sso discovery: %v\n", err)
		fail("Couldn't reach the identity provider. Try again in a moment.")
		return
	}
	cfg, err := ssoOAuthConfig(r, s, p)
	if err != nil {
		fail("Single sign-on is misconfigured. Ask an admin to check it.")
		return
	}
	state, err1 := randomHex(24)
	nonce, err2 := randomHex(24)
	if err1 != nil || err2 != nil {
		fail("Couldn't start sign-in.")
		return
	}
	pending := ssoPendingLogin{Nonce: nonce, Verifier: oauth2.GenerateVerifier(), Return: safeReturnPath(r.URL.Query().Get("return"))}
	raw, _ := json.Marshal(pending)
	if err := utils.RedisClient.Set(r.Context(), ssoStatePrefix+state, raw, ssoStateTTL).Err(); err != nil {
		fail("Couldn't start sign-in.")
		return
	}
	// Ties the callback to this browser, so a callback link started elsewhere
	// can't sign someone into another account (login CSRF).
	http.SetCookie(w, &http.Cookie{
		Name: ssoStateCookie, Value: state, Path: "/", MaxAge: int(ssoStateTTL.Seconds()),
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: sessionstore.SecureCookies() || r.TLS != nil,
	})
	http.Redirect(w, r, cfg.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(pending.Verifier)), http.StatusFound)
}

// APIV1SSOCallback handles GET /api/v2/auth/sso/callback from the provider.
func APIV1SSOCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	fail := func(msg string) { ssoRedirectError(w, r, msg) }
	q := r.URL.Query()
	if e := q.Get("error"); e != "" {
		fail("The identity provider didn't sign you in (" + truncate(e, 60) + ").")
		return
	}
	code, state := strings.TrimSpace(q.Get("code")), strings.TrimSpace(q.Get("state"))
	if code == "" || state == "" || utils.RedisClient == nil {
		fail("Sign-in was interrupted. Try again.")
		return
	}
	cookie, cerr := r.Cookie(ssoStateCookie)
	http.SetCookie(w, &http.Cookie{Name: ssoStateCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	if cerr != nil || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(state)) != 1 {
		fail("This sign-in was started in a different browser. Try again.")
		return
	}
	ctx := r.Context()
	raw, err := utils.RedisClient.GetDel(ctx, ssoStatePrefix+state).Result()
	if err != nil || raw == "" {
		fail("This sign-in link expired. Try again.")
		return
	}
	var pending ssoPendingLogin
	if json.Unmarshal([]byte(raw), &pending) != nil {
		fail("Sign-in was interrupted. Try again.")
		return
	}

	s, err := storage.GetSSOSettings()
	if err != nil || !s.Configured() {
		fail("Single sign-on isn't set up on this site.")
		return
	}
	p, err := ssoProvider(ctx, s.IssuerURL)
	if err != nil {
		fail("Couldn't reach the identity provider. Try again in a moment.")
		return
	}
	cfg, err := ssoOAuthConfig(r, s, p)
	if err != nil {
		fail("Single sign-on is misconfigured. Ask an admin to check it.")
		return
	}
	exCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tok, err := cfg.Exchange(exCtx, code, oauth2.VerifierOption(pending.Verifier))
	if err != nil {
		fmt.Printf("sso exchange: %v\n", err)
		fail("The identity provider refused the sign-in. Ask an admin to check the client secret.")
		return
	}
	rawID, _ := tok.Extra("id_token").(string)
	if rawID == "" {
		fail("The identity provider didn't return an ID token.")
		return
	}
	idTok, err := p.Verifier(&oidc.Config{ClientID: s.ClientID}).Verify(exCtx, rawID)
	if err != nil {
		fmt.Printf("sso verify: %v\n", err)
		fail("The sign-in token couldn't be verified.")
		return
	}
	if idTok.Nonce != pending.Nonce {
		fail("The sign-in token didn't match this browser. Try again.")
		return
	}
	claims, err := ssoClaimsFromToken(idTok, s.GroupsClaim)
	if err != nil {
		fail("The sign-in token couldn't be read.")
		return
	}

	res, err := domain.ResolveSSOLogin(s, claims)
	if err != nil {
		if errors.Is(err, domain.ErrSSODenied) {
			fail(strings.TrimPrefix(err.Error(), domain.ErrSSODenied.Error()+": "))
			return
		}
		fmt.Printf("sso resolve: %v\n", err)
		fail("Sign-in failed. Try again.")
		return
	}
	profile, err := storage.GetUserProfileByID(res.UserID)
	if err != nil {
		fail("Sign-in failed. Try again.")
		return
	}
	// The provider handled the second factor, so local TOTP isn't asked for.
	if err := establishSession(w, r, profile); err != nil {
		fail("Couldn't create your session.")
		return
	}
	http.Redirect(w, r, utils.PublicPath(pending.Return), http.StatusSeeOther)
}

func ssoClaimsFromToken(idTok *oidc.IDToken, groupsClaim string) (domain.SSOClaims, error) {
	var std struct {
		Email             string `json:"email"`
		EmailVerified     any    `json:"email_verified"`
		GivenName         string `json:"given_name"`
		FamilyName        string `json:"family_name"`
		Name              string `json:"name"`
		PreferredUsername string `json:"preferred_username"`
	}
	if err := idTok.Claims(&std); err != nil {
		return domain.SSOClaims{}, err
	}
	c := domain.SSOClaims{
		Issuer: idTok.Issuer, Subject: idTok.Subject, Email: std.Email,
		EmailVerified: claimBool(std.EmailVerified), GivenName: std.GivenName, FamilyName: std.FamilyName,
		Name: std.Name, PreferredUsername: std.PreferredUsername,
	}
	if groupsClaim != "" {
		var all map[string]any
		if err := idTok.Claims(&all); err == nil {
			c.Groups = claimStrings(all[groupsClaim])
		}
	}
	return c, nil
}

// claimBool reads email_verified, which some providers send as "true".
func claimBool(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return strings.EqualFold(t, "true")
	}
	return false
}

func claimStrings(v any) []string {
	switch t := v.(type) {
	case []any:
		out := make([]string, 0, len(t))
		for _, x := range t {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case string:
		return strings.Fields(strings.ReplaceAll(t, ",", " "))
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// ssoRedirectError sends the browser back to the login page with a message.
func ssoRedirectError(w http.ResponseWriter, r *http.Request, msg string) {
	http.Redirect(w, r, utils.PublicPath("/login?sso_error="+url.QueryEscape(msg)), http.StatusSeeOther)
}

type adminSSOJSON struct {
	Enabled         bool           `json:"enabled"`
	Label           string         `json:"label"`
	IssuerURL       string         `json:"issuer_url"`
	ClientID        string         `json:"client_id"`
	ClientSecretSet bool           `json:"client_secret_set"`
	Scopes          string         `json:"scopes"`
	AutoCreate      bool           `json:"auto_create"`
	AllowedDomains  []string       `json:"allowed_domains"`
	SSOOnly         bool           `json:"sso_only"`
	GroupsClaim     string         `json:"groups_claim"`
	TeamMap         map[string]int `json:"team_map"`
	// RedirectURI is what to register with the provider.
	RedirectURI string `json:"redirect_uri"`
}

type adminSSOPatch struct {
	Enabled        *bool           `json:"enabled"`
	Label          *string         `json:"label"`
	IssuerURL      *string         `json:"issuer_url"`
	ClientID       *string         `json:"client_id"`
	ClientSecret   *string         `json:"client_secret"`
	Scopes         *string         `json:"scopes"`
	AutoCreate     *bool           `json:"auto_create"`
	AllowedDomains *[]string       `json:"allowed_domains"`
	SSOOnly        *bool           `json:"sso_only"`
	GroupsClaim    *string         `json:"groups_claim"`
	TeamMap        *map[string]int `json:"team_map"`
}

func adminSSOView(r *http.Request, s *storage.SSOSettings) adminSSOJSON {
	scopes := s.Scopes
	if scopes == "" {
		scopes = storage.DefaultSSOScopes
	}
	return adminSSOJSON{
		Enabled: s.Enabled, Label: s.Label, IssuerURL: s.IssuerURL, ClientID: s.ClientID,
		ClientSecretSet: s.ClientSecretEnc != "", Scopes: scopes, AutoCreate: s.AutoCreate,
		AllowedDomains: s.AllowedDomains, SSOOnly: s.SSOOnly, GroupsClaim: s.GroupsClaim,
		TeamMap: s.TeamMap, RedirectURI: utils.AbsoluteURLForRequest(r, ssoCallback),
	}
}

// APIV1AdminSSO handles GET/PATCH /api/v2/admin/sso (admins only).
func APIV1AdminSSO(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s, err := storage.GetSSOSettings()
		if err != nil {
			utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load SSO settings.")
			return
		}
		writeJSON(w, http.StatusOK, adminSSOView(r, s))
	case http.MethodPatch:
		var req adminSSOPatch
		if err := decodeJSONBody(r, &req); err != nil {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
			return
		}
		s, err := domain.UpdateSSOSettings(domain.SSOSettingsInput{
			Enabled: req.Enabled, Label: req.Label, IssuerURL: req.IssuerURL, ClientID: req.ClientID,
			ClientSecret: req.ClientSecret, Scopes: req.Scopes, AutoCreate: req.AutoCreate,
			AllowedDomains: req.AllowedDomains, SSOOnly: req.SSOOnly, GroupsClaim: req.GroupsClaim, TeamMap: req.TeamMap,
		})
		if err != nil {
			writePeopleDomainError(w, err)
			return
		}
		ssoProviderMu.Lock()
		delete(ssoProviderCache, s.IssuerURL)
		ssoProviderMu.Unlock()
		logAdminEvent(r, "admin.sso_updated", "site", 1, "Single sign-on", map[string]interface{}{
			"enabled": s.Enabled, "sso_only": s.SSOOnly, "auto_create": s.AutoCreate,
		})
		writeJSON(w, http.StatusOK, adminSSOView(r, s))
	default:
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
	}
}

// APIV1AdminSSOTest handles POST /api/v2/admin/sso/test: it fetches the
// provider's discovery document so admins can check the issuer URL.
func APIV1AdminSSOTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	s, err := storage.GetSSOSettings()
	if err != nil || s.IssuerURL == "" {
		utils.APIJSONError(w, http.StatusBadRequest, "validation", "Save an issuer URL first.")
		return
	}
	ssoProviderMu.Lock()
	delete(ssoProviderCache, s.IssuerURL)
	ssoProviderMu.Unlock()
	p, err := ssoProvider(r.Context(), s.IssuerURL)
	if err != nil {
		utils.APIJSONError(w, http.StatusBadGateway, "sso_discovery_failed", "Couldn't load the provider's configuration: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "ok", "authorization_endpoint": p.Endpoint().AuthURL, "token_endpoint": p.Endpoint().TokenURL,
	})
}
