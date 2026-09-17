package handlers

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"GoTodo/internal/crypto/secret"
	"GoTodo/internal/domain"
	githubclient "GoTodo/internal/githubclient"
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
)

const (
	githubOAuthStateTTL    = 10 * time.Minute
	githubOAuthStatePref   = "github:oauth:state:"
	githubWebhookSecretHdr = "X-Ordryn-Webhook-Secret"
	githubHubSignatureHdr  = "X-Hub-Signature-256"
)

type apiGitHubPATRequest struct {
	Token string `json:"token"`
}

type apiProjectGitHubLinkRequest struct {
	Repository string `json:"repository"`
}

type apiTaskGitHubCreateRequest struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

type apiTaskGitHubLinkRequest struct {
	Issue string `json:"issue"`
}

type apiTaskGitHubJSON struct {
	IssueNumber   int    `json:"issue_number"`
	IssueID       int64  `json:"issue_id"`
	IssueURL      string `json:"issue_url"`
	IssueState    string `json:"issue_state"`
	IssueTitle    string `json:"issue_title,omitempty"`
	LastSyncError string `json:"last_sync_error,omitempty"`
}

func writeGitHubDomainError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		utils.APIJSONError(w, http.StatusNotFound, "not_found", githubClientMessage(err, "Not found."))
	case errors.Is(err, domain.ErrForbidden):
		utils.APIJSONError(w, http.StatusForbidden, "forbidden", githubClientMessage(err, "Forbidden."))
	case errors.Is(err, domain.ErrConflict):
		utils.APIJSONError(w, http.StatusConflict, "conflict", githubClientMessage(err, "Conflict."))
	case errors.Is(err, domain.ErrValidation):
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", githubClientMessage(err, "Invalid request."))
	default:
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Request failed.")
	}
}

func githubClientMessage(err error, fallback string) string {
	msg := err.Error()
	for _, prefix := range []string{"validation: ", "not found: ", "forbidden: ", "conflict: "} {
		if strings.HasPrefix(msg, prefix) {
			return strings.TrimPrefix(msg, prefix)
		}
	}
	if msg != "" &&
		msg != domain.ErrValidation.Error() &&
		msg != domain.ErrNotFound.Error() &&
		msg != domain.ErrForbidden.Error() &&
		msg != domain.ErrConflict.Error() {
		return msg
	}
	return fallback
}

func taskGitHubToAPIJSON(p *domain.TaskGitHubIssuePublic) *apiTaskGitHubJSON {
	if p == nil {
		return nil
	}
	return &apiTaskGitHubJSON{
		IssueNumber:   p.IssueNumber,
		IssueID:       p.IssueID,
		IssueURL:      p.IssueURL,
		IssueState:    p.IssueState,
		IssueTitle:    p.IssueTitle,
		LastSyncError: p.LastSyncError,
	}
}

// APIV1MeGitHub handles GET/DELETE /api/v2/me/github.
func APIV1MeGitHub(w http.ResponseWriter, r *http.Request) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	switch r.Method {
	case http.MethodGet:
		conn, err := domain.GetGitHubConnectionPublic(r.Context(), userID)
		if err != nil {
			writeGitHubDomainError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(conn)
	case http.MethodDelete:
		if err := domain.DisconnectGitHub(r.Context(), userID); err != nil {
			writeGitHubDomainError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	default:
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
	}
}

// APIV1MeGitHubPAT handles POST /api/v2/me/github/pat.
func APIV1MeGitHubPAT(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	var req apiGitHubPATRequest
	if err := decodeJSONBody(r, &req); err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
		return
	}
	conn, err := domain.ConnectGitHubWithPAT(r.Context(), userID, req.Token)
	if err != nil {
		writeGitHubDomainError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(conn)
}

// APIV1MeGitHubOAuthStart handles GET /api/v2/me/github/oauth/start.
func APIV1MeGitHubOAuthStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	if !domain.GitHubOAuthConfigured() {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "GitHub OAuth is not configured.")
		return
	}
	if utils.RedisClient == nil {
		utils.APIJSONError(w, http.StatusServiceUnavailable, "unavailable", "OAuth requires Redis.")
		return
	}
	settings, err := storage.GetSiteSettings()
	if err != nil || settings == nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load settings.")
		return
	}

	stateBytes := make([]byte, 24)
	if _, err := rand.Read(stateBytes); err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to start OAuth.")
		return
	}
	state := hex.EncodeToString(stateBytes)
	redirectURI := utils.AbsoluteURLForRequest(r, "/api/v2/auth/github/callback")

	ctx := r.Context()
	if err := utils.RedisClient.Set(ctx, githubOAuthStatePref+state, strconv.Itoa(userID), githubOAuthStateTTL).Err(); err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to store OAuth state.")
		return
	}

	authorizeURL := githubclient.AuthorizeURL(settings.GitHubOAuthClientID, redirectURI, state, domain.GitHubOAuthScope)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"authorize_url": authorizeURL,
		"redirect_uri":  redirectURI,
	})
}

// APIV1GitHubOAuthCallback handles GET /api/v2/auth/github/callback (public).
func APIV1GitHubOAuthCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	redirectFail := func(reason string) {
		http.Redirect(w, r, utils.PublicPath("/settings?github=error&reason="+url.QueryEscape(reason)), http.StatusSeeOther)
	}
	if utils.RedisClient == nil {
		redirectFail("redis")
		return
	}
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	if code == "" || state == "" {
		redirectFail("missing")
		return
	}
	if errParam := strings.TrimSpace(r.URL.Query().Get("error")); errParam != "" {
		redirectFail("denied")
		return
	}

	ctx := r.Context()
	key := githubOAuthStatePref + state
	userIDStr, err := utils.RedisClient.Get(ctx, key).Result()
	_ = utils.RedisClient.Del(ctx, key).Err()
	if err != nil || userIDStr == "" {
		redirectFail("state")
		return
	}
	userID, err := strconv.Atoi(userIDStr)
	if err != nil || userID <= 0 {
		redirectFail("state")
		return
	}

	settings, err := storage.GetSiteSettings()
	if err != nil || settings == nil || !domain.GitHubOAuthConfigured() {
		redirectFail("config")
		return
	}
	clientSecret, err := secret.Decrypt(settings.GitHubOAuthClientSecretEnc)
	if err != nil || clientSecret == "" {
		redirectFail("config")
		return
	}

	token, err := githubclient.ExchangeOAuthCode(ctx, settings.GitHubOAuthClientID, clientSecret, code)
	if err != nil {
		redirectFail("exchange")
		return
	}
	if _, err := domain.ConnectGitHubWithOAuthToken(ctx, userID, token); err != nil {
		redirectFail("connect")
		return
	}
	http.Redirect(w, r, utils.PublicPath("/settings?github=connected"), http.StatusSeeOther)
}

// apiV1ProjectGitHub handles GET/PUT/DELETE /api/v2/projects/{id}/github.
func apiV1ProjectGitHub(w http.ResponseWriter, r *http.Request, projectID int) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	switch r.Method {
	case http.MethodGet:
		repo, err := domain.GetProjectGitHubRepoForUser(r.Context(), userID, projectID)
		if err != nil {
			writeGitHubDomainError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(repo)
	case http.MethodPut:
		var req apiProjectGitHubLinkRequest
		if err := decodeJSONBody(r, &req); err != nil {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
			return
		}
		repo, err := domain.LinkProjectGitHubRepo(r.Context(), userID, projectID, req.Repository)
		if err != nil {
			writeGitHubDomainError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(repo)
	case http.MethodDelete:
		if err := domain.UnlinkProjectGitHubRepo(r.Context(), userID, projectID); err != nil {
			writeGitHubDomainError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	default:
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
	}
}

// apiV1TaskGitHubIssue handles POST/PUT/DELETE /api/v2/tasks/{id}/github-issue.
func apiV1TaskGitHubIssue(w http.ResponseWriter, r *http.Request, taskID int) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	switch r.Method {
	case http.MethodPost:
		var req apiTaskGitHubCreateRequest
		// Empty body is allowed (title/body default from the task).
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		defer r.Body.Close()
		if err != nil {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid request body.")
			return
		}
		if len(body) > 0 {
			if err := json.Unmarshal(body, &req); err != nil {
				utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
				return
			}
		}
		taskURL := ""
		if path := domain.PublicSharePathForTask(taskID); path != "" {
			taskURL = utils.AbsoluteURLForRequest(r, path)
		}
		issue, err := domain.CreateGitHubIssueForTask(r.Context(), userID, taskID, req.Title, req.Body, taskURL)
		if err != nil {
			writeGitHubDomainError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(taskGitHubToAPIJSON(issue))
	case http.MethodPut:
		var req apiTaskGitHubLinkRequest
		if err := decodeJSONBody(r, &req); err != nil {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
			return
		}
		issue, err := domain.LinkExistingGitHubIssue(r.Context(), userID, taskID, req.Issue)
		if err != nil {
			writeGitHubDomainError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(taskGitHubToAPIJSON(issue))
	case http.MethodDelete:
		if err := domain.UnlinkGitHubIssue(r.Context(), userID, taskID); err != nil {
			writeGitHubDomainError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	default:
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
	}
}

// APIV1GitHubWebhook handles POST /api/v2/webhooks/github (public).
// Auth: X-Ordryn-Webhook-Secret matching the project link secret, or X-Hub-Signature-256 HMAC.
func APIV1GitHubWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Failed to read body.")
		return
	}
	defer r.Body.Close()

	event := strings.TrimSpace(r.Header.Get("X-GitHub-Event"))
	if event != "" && event != "issues" && event != "ping" {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ignored"})
		return
	}
	if event == "ping" {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		return
	}

	var payload struct {
		Action string `json:"action"`
		Issue  *struct {
			ID      int64  `json:"id"`
			Number  int    `json:"number"`
			Title   string `json:"title"`
			State   string `json:"state"`
			HTMLURL string `json:"html_url"`
		} `json:"issue"`
		Repository *struct {
			Name  string `json:"name"`
			Owner struct {
				Login string `json:"login"`
			} `json:"owner"`
		} `json:"repository"`
	}
	if err := json.Unmarshal(body, &payload); err != nil || payload.Issue == nil || payload.Repository == nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid webhook payload.")
		return
	}
	owner := payload.Repository.Owner.Login
	repoName := payload.Repository.Name
	if owner == "" || repoName == "" {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid repository in payload.")
		return
	}

	customSecret := strings.TrimSpace(r.Header.Get(githubWebhookSecretHdr))
	hubSig := strings.TrimSpace(r.Header.Get(githubHubSignatureHdr))

	deliverySecret := ""
	if customSecret != "" {
		deliverySecret = customSecret
	} else if hubSig != "" {
		link, err := storage.FindProjectGitHubRepoByNames(owner, repoName)
		if err != nil || link == nil || link.WebhookSecret == "" {
			utils.APIJSONError(w, http.StatusForbidden, "forbidden", "Forbidden.")
			return
		}
		if !verifyGitHubWebhookHMAC(link.WebhookSecret, body, hubSig) {
			utils.APIJSONError(w, http.StatusForbidden, "forbidden", "Invalid signature.")
			return
		}
		deliverySecret = link.WebhookSecret
	} else {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized",
			"Provide X-Ordryn-Webhook-Secret or X-Hub-Signature-256.")
		return
	}

	err = domain.ApplyGitHubIssueWebhookState(
		context.Background(),
		owner,
		repoName,
		payload.Issue.ID,
		payload.Issue.Number,
		payload.Issue.State,
		payload.Issue.Title,
		payload.Issue.HTMLURL,
		deliverySecret,
	)
	if err != nil {
		writeGitHubDomainError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func verifyGitHubWebhookHMAC(secret string, body []byte, signatureHeader string) bool {
	const prefix = "sha256="
	if !strings.HasPrefix(signatureHeader, prefix) {
		return false
	}
	wantHex := strings.TrimPrefix(signatureHeader, prefix)
	want, err := hex.DecodeString(wantHex)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	got := mac.Sum(nil)
	return hmac.Equal(got, want)
}
