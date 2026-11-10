package domain

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"GoTodo/internal/crypto/secret"
	githubclient "GoTodo/internal/githubclient"
	"GoTodo/internal/live"
	"GoTodo/internal/storage"
)

// GitHubOAuthScope is the OAuth scope requested for repo admin + issues.
const GitHubOAuthScope = "repo"

// GitHubConnectionPublic is the safe view of a user's GitHub connection.
type GitHubConnectionPublic struct {
	Connected   bool   `json:"connected"`
	GitHubLogin string `json:"github_login,omitempty"`
	AuthMethod  string `json:"auth_method,omitempty"`
	ConnectedAt string `json:"connected_at,omitempty"`
	// Warning is set on connect when the token is valid but cannot reach
	// private repositories (e.g. a classic token without the repo scope).
	Warning string `json:"warning,omitempty"`
}

// ProjectGitHubRepoPublic is the safe view of a project↔repo link.
type ProjectGitHubRepoPublic struct {
	Linked         bool   `json:"linked"`
	Owner          string `json:"owner,omitempty"`
	Repo           string `json:"repo,omitempty"`
	FullName       string `json:"full_name,omitempty"`
	HTMLURL        string `json:"html_url,omitempty"`
	RepoID         int64  `json:"repo_id,omitempty"`
	LinkedByUserID int    `json:"linked_by_user_id,omitempty"`
	WebhookSecret  string `json:"webhook_secret,omitempty"`
	LinkedAt       string `json:"linked_at,omitempty"`
}

// TaskGitHubIssuePublic is exposed on task payloads.
type TaskGitHubIssuePublic struct {
	IssueNumber   int    `json:"issue_number"`
	IssueID       int64  `json:"issue_id"`
	IssueURL      string `json:"issue_url"`
	IssueState    string `json:"issue_state"`
	IssueTitle    string `json:"issue_title,omitempty"`
	LastSyncError string `json:"last_sync_error,omitempty"`
}

func formatTimeRFC3339(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// GetGitHubConnectionPublic returns connection status for the user.
func GetGitHubConnectionPublic(ctx context.Context, userID int) (*GitHubConnectionPublic, error) {
	_ = ctx
	conn, err := storage.GetGitHubConnection(userID)
	if err != nil {
		return nil, err
	}
	if conn == nil {
		return &GitHubConnectionPublic{Connected: false}, nil
	}
	return &GitHubConnectionPublic{
		Connected:   true,
		GitHubLogin: conn.GitHubLogin,
		AuthMethod:  conn.AuthMethod,
		ConnectedAt: formatTimeRFC3339(conn.ConnectedAt),
	}, nil
}

// ConnectGitHubWithPAT validates a personal access token and stores it encrypted.
func ConnectGitHubWithPAT(ctx context.Context, userID int, token string) (*GitHubConnectionPublic, error) {
	return connectGitHubToken(ctx, userID, token, storage.GitHubAuthPAT)
}

// ConnectGitHubWithOAuthToken stores a token obtained via GitHub OAuth.
func ConnectGitHubWithOAuthToken(ctx context.Context, userID int, token string) (*GitHubConnectionPublic, error) {
	return connectGitHubToken(ctx, userID, token, storage.GitHubAuthOAuth)
}

func connectGitHubToken(ctx context.Context, userID int, token, method string) (*GitHubConnectionPublic, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, fmt.Errorf("%w: token is required", ErrValidation)
	}
	if len(token) > 255 {
		return nil, fmt.Errorf("%w: token is too long", ErrValidation)
	}
	client := githubclient.New(token)
	user, err := client.GetAuthenticatedUser(ctx)
	if err != nil {
		return nil, mapGitHubErr(err, "Invalid or expired GitHub token.")
	}
	enc, err := secret.Encrypt(token)
	if err != nil {
		return nil, fmt.Errorf("encrypt token: %w", err)
	}
	conn, err := storage.UpsertGitHubConnection(userID, enc, user.Login, method, user.ID)
	if err != nil {
		return nil, err
	}
	return &GitHubConnectionPublic{
		Connected:   true,
		GitHubLogin: conn.GitHubLogin,
		AuthMethod:  conn.AuthMethod,
		ConnectedAt: formatTimeRFC3339(conn.ConnectedAt),
		Warning:     githubTokenScopeWarning(user),
	}, nil
}

// githubTokenScopeWarning explains when a classic/OAuth token lacks the repo
// scope GitHub requires for private repositories. Fine-grained tokens report
// no scopes, so nothing can be checked up front for them.
func githubTokenScopeWarning(user *githubclient.User) string {
	if user == nil || !user.ScopesKnown || githubclient.ScopesAllowPrivateRepos(user.Scopes) {
		return ""
	}
	return "This token does not have the \"repo\" scope, so only public repositories can be linked. " +
		"To use private repositories, create a classic token with the \"repo\" scope or a fine-grained token with access to those repositories."
}

// DisconnectGitHub removes the user's stored GitHub credentials.
func DisconnectGitHub(ctx context.Context, userID int) error {
	_ = ctx
	return storage.DeleteGitHubConnection(userID)
}

func githubClientForUser(ctx context.Context, userID int) (*githubclient.Client, error) {
	_ = ctx
	conn, err := storage.GetGitHubConnection(userID)
	if err != nil {
		return nil, err
	}
	if conn == nil {
		return nil, fmt.Errorf("%w: connect GitHub in Settings first", ErrValidation)
	}
	token, err := secret.Decrypt(conn.AccessTokenEnc)
	if err != nil {
		return nil, fmt.Errorf("%w: stored GitHub token is unavailable; reconnect GitHub", ErrValidation)
	}
	return githubclient.New(token), nil
}

func projectGitHubPublic(r *storage.ProjectGitHubRepo, includeWebhook bool) *ProjectGitHubRepoPublic {
	if r == nil {
		return &ProjectGitHubRepoPublic{Linked: false}
	}
	out := &ProjectGitHubRepoPublic{
		Linked:         true,
		Owner:          r.GitHubOwner,
		Repo:           r.GitHubRepo,
		FullName:       r.GitHubOwner + "/" + r.GitHubRepo,
		HTMLURL:        "https://github.com/" + r.GitHubOwner + "/" + r.GitHubRepo,
		RepoID:         r.GitHubRepoID,
		LinkedByUserID: r.LinkedByUserID,
		LinkedAt:       formatTimeRFC3339(r.LinkedAt),
	}
	if includeWebhook {
		out.WebhookSecret = r.WebhookSecret
	}
	return out
}

// GetProjectGitHubRepoForUser returns the linked repo if the user can access the project.
func GetProjectGitHubRepoForUser(ctx context.Context, userID, projectID int) (*ProjectGitHubRepoPublic, error) {
	_ = ctx
	proj, err := storage.GetAccessibleProjectByID(projectID, userID)
	if err != nil {
		return nil, ErrNotFound
	}
	repo, err := storage.GetProjectGitHubRepo(projectID)
	if err != nil {
		return nil, err
	}
	return projectGitHubPublic(repo, storage.RoleCanManageProject(proj.ID, proj.Role)), nil
}

// LinkProjectGitHubRepo verifies admin access and links the repository (owner only).
func LinkProjectGitHubRepo(ctx context.Context, userID, projectID int, repoRef string) (*ProjectGitHubRepoPublic, error) {
	proj, err := storage.GetAccessibleProjectByID(projectID, userID)
	if err != nil {
		return nil, ErrNotFound
	}
	if !storage.RoleCanManageProject(proj.ID, proj.Role) {
		return nil, ErrForbidden
	}
	owner, name, err := githubclient.ParseRepoFullName(repoRef)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	client, err := githubClientForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	repo, err := client.GetRepo(ctx, owner, name)
	if err != nil {
		return nil, mapGitHubRepoErr(err, client.Token, owner, name, "Repository not found or inaccessible.")
	}
	if !githubclient.HasAdminAccess(repo) {
		return nil, fmt.Errorf("%w: you must have admin access on %s to link it", ErrForbidden, repo.FullName)
	}

	secretBytes := make([]byte, 24)
	if _, err := rand.Read(secretBytes); err != nil {
		return nil, err
	}
	webhookSecret := hex.EncodeToString(secretBytes)

	existing, _ := storage.GetProjectGitHubRepo(projectID)
	if existing != nil && existing.WebhookSecret != "" &&
		strings.EqualFold(existing.GitHubOwner, owner) && strings.EqualFold(existing.GitHubRepo, name) {
		webhookSecret = existing.WebhookSecret
	}

	saved, err := storage.UpsertProjectGitHubRepo(projectID, userID, repo.Owner.Login, repo.Name, repo.ID, webhookSecret)
	if err != nil {
		return nil, err
	}
	_ = storage.LogProjectEvent(projectID, userID, "github_repo_linked", map[string]interface{}{
		"full_name": saved.GitHubOwner + "/" + saved.GitHubRepo,
		"repo_id":   saved.GitHubRepoID,
	})
	live.AfterProjectChange(userID, projectID, live.TypeProjectUpdated)
	return projectGitHubPublic(saved, true), nil
}

// UnlinkProjectGitHubRepo removes the repo link and task issue links (owner only).
func UnlinkProjectGitHubRepo(ctx context.Context, userID, projectID int) error {
	_ = ctx
	proj, err := storage.GetAccessibleProjectByID(projectID, userID)
	if err != nil {
		return ErrNotFound
	}
	if !storage.RoleCanManageProject(proj.ID, proj.Role) {
		return ErrForbidden
	}
	if err := storage.DeleteProjectGitHubRepo(projectID); err != nil {
		return err
	}
	_ = storage.LogProjectEvent(projectID, userID, "github_repo_unlinked", nil)
	live.AfterProjectChange(userID, projectID, live.TypeProjectUpdated)
	return nil
}

func taskGitHubPublic(issue *storage.TaskGitHubIssue) *TaskGitHubIssuePublic {
	if issue == nil {
		return nil
	}
	return &TaskGitHubIssuePublic{
		IssueNumber:   issue.IssueNumber,
		IssueID:       issue.IssueID,
		IssueURL:      issue.IssueURL,
		IssueState:    issue.IssueState,
		IssueTitle:    issue.IssueTitle,
		LastSyncError: issue.LastSyncError,
	}
}

// GetTaskGitHubIssuePublic returns the link for a readable task.
func GetTaskGitHubIssuePublic(ctx context.Context, userID, taskID int) (*TaskGitHubIssuePublic, error) {
	_ = ctx
	canRead, _, _, err := storage.CanUserAccessTask(taskID, userID)
	if err != nil || !canRead {
		return nil, ErrNotFound
	}
	issue, err := storage.GetTaskGitHubIssue(taskID)
	if err != nil {
		return nil, err
	}
	return taskGitHubPublic(issue), nil
}

func requireWritableTaskInLinkedProject(userID, taskID int) (projectID int, repo *storage.ProjectGitHubRepo, err error) {
	canRead, role, projectID, err := storage.CanUserAccessTask(taskID, userID)
	if err != nil || !canRead {
		return 0, nil, ErrNotFound
	}
	if err := denyMissingTaskPerm(projectID, role, storage.PermTasksEdit); err != nil {
		return 0, nil, ErrNotFound
	}
	if projectID <= 0 {
		return 0, nil, fmt.Errorf("%w: task must belong to a project with a linked GitHub repository", ErrValidation)
	}
	repo, err = storage.GetProjectGitHubRepo(projectID)
	if err != nil {
		return 0, nil, err
	}
	if repo == nil {
		return 0, nil, fmt.Errorf("%w: link a GitHub repository to this project first", ErrValidation)
	}
	return projectID, repo, nil
}

// CreateGitHubIssueForTask creates a GitHub issue from the task and links it.
func CreateGitHubIssueForTask(ctx context.Context, userID, taskID int, title, body string, taskURL string) (*TaskGitHubIssuePublic, error) {
	_, repo, err := requireWritableTaskInLinkedProject(userID, taskID)
	if err != nil {
		return nil, err
	}
	existing, err := storage.GetTaskGitHubIssue(taskID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, fmt.Errorf("%w: task is already linked to a GitHub issue", ErrConflict)
	}

	task, err := storage.GetTaskTitleDescription(taskID)
	if err != nil {
		return nil, ErrNotFound
	}
	title = strings.TrimSpace(title)
	if title == "" {
		title = task.Title
	}
	if title == "" {
		return nil, fmt.Errorf("%w: title is required", ErrValidation)
	}
	body = strings.TrimSpace(body)
	if body == "" {
		body = task.Description
	}
	if taskURL != "" {
		if body != "" {
			body += "\n\n"
		}
		body += "---\nTracked in Ordryn: " + taskURL
	}

	client, err := githubClientForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	issue, err := client.CreateIssue(ctx, repo.GitHubOwner, repo.GitHubRepo, title, body)
	if err != nil {
		return nil, mapGitHubRepoErr(err, client.Token, repo.GitHubOwner, repo.GitHubRepo, "Failed to create GitHub issue.")
	}
	saved, err := storage.UpsertTaskGitHubIssue(taskID, issue.ID, issue.Number, issue.HTMLURL, issue.State, issue.Title)
	if err != nil {
		return nil, err
	}
	_ = storage.LogTaskEvent(taskID, userID, "github_issue_created", map[string]interface{}{
		"issue_number": issue.Number,
		"issue_url":    issue.HTMLURL,
	})
	live.AfterTaskChange(userID, taskID, live.TypeTaskUpdated)
	return taskGitHubPublic(saved), nil
}

// LinkExistingGitHubIssue attaches an existing issue to the task (same linked repo).
func LinkExistingGitHubIssue(ctx context.Context, userID, taskID int, issueRef string) (*TaskGitHubIssuePublic, error) {
	_, repo, err := requireWritableTaskInLinkedProject(userID, taskID)
	if err != nil {
		return nil, err
	}
	existing, err := storage.GetTaskGitHubIssue(taskID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, fmt.Errorf("%w: task is already linked to a GitHub issue", ErrConflict)
	}

	owner, name, number, err := githubclient.ParseIssueRef(issueRef)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	if owner != "" && name != "" {
		if !strings.EqualFold(owner, repo.GitHubOwner) || !strings.EqualFold(name, repo.GitHubRepo) {
			return nil, fmt.Errorf("%w: issue must belong to linked repository %s/%s", ErrValidation, repo.GitHubOwner, repo.GitHubRepo)
		}
	}

	client, err := githubClientForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	issue, err := client.GetIssue(ctx, repo.GitHubOwner, repo.GitHubRepo, number)
	if err != nil {
		return nil, mapGitHubErr(err, "GitHub issue not found.")
	}
	// Pull requests share the issues API; reject PRs.
	if strings.Contains(strings.ToLower(issue.HTMLURL), "/pull/") {
		return nil, fmt.Errorf("%w: link GitHub issues only (not pull requests)", ErrValidation)
	}

	other, err := storage.GetTaskGitHubIssueByIssueID(issue.ID)
	if err != nil {
		return nil, err
	}
	if other != nil && other.TaskID != taskID {
		return nil, fmt.Errorf("%w: that GitHub issue is already linked to another task", ErrConflict)
	}

	saved, err := storage.UpsertTaskGitHubIssue(taskID, issue.ID, issue.Number, issue.HTMLURL, issue.State, issue.Title)
	if err != nil {
		return nil, err
	}
	_ = storage.LogTaskEvent(taskID, userID, "github_issue_linked", map[string]interface{}{
		"issue_number": issue.Number,
		"issue_url":    issue.HTMLURL,
	})
	live.AfterTaskChange(userID, taskID, live.TypeTaskUpdated)
	return taskGitHubPublic(saved), nil
}

// UnlinkGitHubIssue removes the Ordryn↔issue link without deleting the GitHub issue.
func UnlinkGitHubIssue(ctx context.Context, userID, taskID int) error {
	_ = ctx
	canRead, role, projectID, err := storage.CanUserAccessTask(taskID, userID)
	if err != nil || !canRead {
		return ErrNotFound
	}
	if err := denyMissingTaskPerm(projectID, role, storage.PermTasksEdit); err != nil {
		return ErrNotFound
	}
	existing, err := storage.GetTaskGitHubIssue(taskID)
	if err != nil {
		return err
	}
	if existing == nil {
		return nil
	}
	if err := storage.DeleteTaskGitHubIssue(taskID); err != nil {
		return err
	}
	_ = storage.LogTaskEvent(taskID, userID, "github_issue_unlinked", map[string]interface{}{
		"issue_number": existing.IssueNumber,
	})
	live.AfterTaskChange(userID, taskID, live.TypeTaskUpdated)
	return nil
}

// SyncGitHubIssueFromOrdrynState closes/reopens the linked issue based on Ordryn completed/done.
// Best-effort: errors are stored on the link and logged; they do not fail the task update.
func SyncGitHubIssueFromOrdrynState(ctx context.Context, actorUserID, taskID int, completed bool) {
	issue, err := storage.GetTaskGitHubIssue(taskID)
	if err != nil || issue == nil {
		return
	}
	desired := "open"
	if completed {
		desired = "closed"
	}
	if strings.EqualFold(issue.IssueState, desired) {
		return
	}
	projectID, err := storage.GetTaskProjectID(taskID)
	if err != nil || projectID <= 0 {
		return
	}
	repo, err := storage.GetProjectGitHubRepo(projectID)
	if err != nil || repo == nil {
		return
	}
	tokenUserID := actorUserID
	if conn, _ := storage.GetGitHubConnection(actorUserID); conn == nil {
		tokenUserID = repo.LinkedByUserID
	}
	client, err := githubClientForUser(ctx, tokenUserID)
	if err != nil {
		recordGitHubSyncError(taskID, err)
		return
	}
	updated, err := client.SetIssueState(ctx, repo.GitHubOwner, repo.GitHubRepo, issue.IssueNumber, desired)
	if err != nil {
		log.Printf("github sync task=%d: %v", taskID, err)
		recordGitHubSyncError(taskID, err)
		return
	}
	_ = storage.UpdateTaskGitHubIssueState(taskID, updated.State, "")
}

func recordGitHubSyncError(taskID int, syncErr error) {
	if syncErr == nil {
		return
	}
	issue, err := storage.GetTaskGitHubIssue(taskID)
	if err != nil || issue == nil {
		return
	}
	_ = storage.UpdateTaskGitHubIssueState(taskID, issue.IssueState, syncErr.Error())
}

// ApplyGitHubIssueWebhookState updates Ordryn from a GitHub issues webhook (never creates tasks).
func ApplyGitHubIssueWebhookState(ctx context.Context, owner, repoName string, issueID int64, number int, state, title, htmlURL, deliverySecret string) error {
	_ = ctx
	link, err := storage.FindProjectGitHubRepoByNames(owner, repoName)
	if err != nil {
		return err
	}
	if link == nil {
		return ErrNotFound
	}
	if link.WebhookSecret == "" || deliverySecret == "" || !secureEqual(link.WebhookSecret, deliverySecret) {
		return ErrForbidden
	}
	issue, err := storage.GetTaskGitHubIssueByIssueID(issueID)
	if err != nil {
		return err
	}
	if issue == nil {
		// Intentionally ignore unlinked issues — never create Ordryn tasks from GitHub.
		return nil
	}
	state = strings.ToLower(strings.TrimSpace(state))
	if state != "open" && state != "closed" {
		return nil
	}
	_, _ = storage.UpsertTaskGitHubIssue(issue.TaskID, issueID, number, htmlURL, state, title)

	completed := state == "closed"
	projectID, err := storage.GetTaskProjectID(issue.TaskID)
	if err != nil || projectID <= 0 {
		return nil
	}
	oldCompleted, lookupErr := lookupTaskCompleted(issue.TaskID)
	if lookupErr != nil {
		log.Printf("github webhook completed lookup task=%d: %v", issue.TaskID, lookupErr)
	}
	hookedCompleted := false
	allow, err := kanbanAllowsGitHubStatusSync(issue.TaskID, projectID)
	if err != nil {
		log.Printf("github webhook status sync task=%d: %v", issue.TaskID, err)
	} else if allow {
		if err := ApplyCompletedStatusSync(issue.TaskID, projectID, completed); err != nil {
			log.Printf("github webhook status sync task=%d: %v", issue.TaskID, err)
		} else if lookupErr == nil && oldCompleted != completed {
			afterCompletionChanged(0, issue.TaskID, completed)
			hookedCompleted = true
		}
	}
	_ = storage.LogTaskEvent(issue.TaskID, 0, "github_issue_synced", map[string]interface{}{
		"issue_number": number,
		"issue_state":  state,
		"source":       "github",
	})
	if hookedCompleted {
		live.AfterTaskChangeLive(0, issue.TaskID, live.TypeTaskUpdated)
	} else {
		live.AfterTaskChange(0, issue.TaskID, live.TypeTaskUpdated)
	}
	return nil
}

// kanbanAllowsGitHubStatusSync reports whether a GitHub open/close event should
// move the task to the default or done column. Intermediate columns are left alone.
func kanbanAllowsGitHubStatusSync(taskID, projectID int) (bool, error) {
	mode, err := storage.GetProjectWorkflowMode(projectID)
	if err != nil {
		return false, err
	}
	if mode != storage.WorkflowKanban {
		return true, nil
	}
	st, err := storage.GetTaskProjectStatus(taskID)
	if err != nil {
		return false, err
	}
	if st == nil {
		return true, nil
	}
	return st.IsDefault || st.IsDone, nil
}

func secureEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := 0; i < len(a); i++ {
		v |= a[i] ^ b[i]
	}
	return v == 0
}

// mapGitHubRepoErr maps a repo-scoped GitHub error, replacing GitHub's
// deliberately vague 404 for private repositories with the likely cause.
func mapGitHubRepoErr(err error, token, owner, repo, fallback string) error {
	if apiErr, ok := err.(*githubclient.APIError); ok {
		if hint := githubRepoAccessHint(apiErr, token, owner, repo); hint != "" {
			if apiErr.Status == http.StatusNotFound {
				return fmt.Errorf("%w: %s", ErrNotFound, hint)
			}
			return fmt.Errorf("%w: %s", ErrForbidden, hint)
		}
	}
	return mapGitHubErr(err, fallback)
}

// githubRepoAccessHint returns an actionable message for access failures on
// owner/repo, or "" when the error is not an access problem.
func githubRepoAccessHint(apiErr *githubclient.APIError, token, owner, repo string) string {
	full := owner + "/" + repo
	// SAML SSO: org requires the token to be authorized for SSO.
	if apiErr.SSO != "" {
		msg := fmt.Sprintf("The %s organization uses SAML single sign-on and your GitHub token is not authorized for it.", owner)
		if u := githubclient.SSOAuthorizeURL(apiErr.SSO); u != "" {
			return msg + " Authorize it here, then try again: " + u
		}
		return msg + " Authorize the token for SSO in your GitHub token settings, then try again."
	}
	if apiErr.Status == http.StatusForbidden &&
		strings.Contains(strings.ToLower(apiErr.Message), "oauth app access restrictions") {
		return fmt.Sprintf("The %s organization restricts OAuth app access. An organization owner must approve this app "+
			"(GitHub → Settings → Applications), or connect with a personal access token instead.", owner)
	}
	if apiErr.Status != http.StatusNotFound {
		return ""
	}
	// GitHub returns 404 (not 403) for private repositories the token cannot see.
	if apiErr.HasOAuthScopes && !githubclient.ScopesAllowPrivateRepos(githubclient.ParseScopes(apiErr.OAuthScopes)) {
		return fmt.Sprintf("Could not access %s. Your GitHub token lacks the \"repo\" scope required for private repositories; "+
			"reconnect GitHub with a token that has the \"repo\" scope.", full)
	}
	if githubclient.TokenKind(token) == "fine_grained" {
		return fmt.Sprintf("Could not access %s. If it is private, make sure your fine-grained token's resource owner is %q "+
			"and the repository is selected under Repository access, with Issues read/write permission. "+
			"Organizations may also need to approve fine-grained tokens.", full, owner)
	}
	return fmt.Sprintf("Could not access %s. Check the name; if it is private, the connected GitHub account must be a "+
		"collaborator or member with access to it.", full)
}

func mapGitHubErr(err error, fallback string) error {
	if err == nil {
		return nil
	}
	if apiErr, ok := err.(*githubclient.APIError); ok {
		detail := strings.TrimSpace(apiErr.Message)
		if detail == "" {
			detail = fallback
		} else if fallback != "" && !strings.EqualFold(detail, fallback) {
			detail = fallback + " " + detail
		}
		switch apiErr.Status {
		case http.StatusUnauthorized, http.StatusForbidden:
			return fmt.Errorf("%w: %s", ErrForbidden, detail)
		case http.StatusNotFound:
			return fmt.Errorf("%w: %s", ErrNotFound, detail)
		case http.StatusUnprocessableEntity:
			return fmt.Errorf("%w: %s", ErrValidation, detail)
		}
		return fmt.Errorf("%w: %s", ErrValidation, detail)
	}
	return fmt.Errorf("%w: %s", ErrValidation, fallback)
}

// GitHubOAuthConfigured reports whether the site admin configured OAuth credentials.
func GitHubOAuthConfigured() bool {
	s, err := storage.GetSiteSettings()
	if err != nil || s == nil {
		return false
	}
	return strings.TrimSpace(s.GitHubOAuthClientID) != "" && strings.TrimSpace(s.GitHubOAuthClientSecretEnc) != ""
}
