package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	GitHubAuthOAuth = "oauth"
	GitHubAuthPAT   = "pat"
)

// GitHubConnection is a user's linked GitHub credentials (token encrypted at rest).
//
// A user may hold several connections, one per OwnerScope. An empty OwnerScope
// is the catch-all connection (OAuth or classic token, which cover every owner
// the GitHub account can reach). A non-empty OwnerScope (stored lowercase) is a
// token limited to that user or organization, e.g. a fine-grained PAT whose
// resource owner is that account.
type GitHubConnection struct {
	UserID         int
	OwnerScope     string
	AccessTokenEnc string
	GitHubUserID   int64
	GitHubLogin    string
	AuthMethod     string
	ConnectedAt    time.Time
	UpdatedAt      time.Time
}

// ProjectGitHubRepo links a kanban/classic project to one GitHub repository.
type ProjectGitHubRepo struct {
	ProjectID      int
	GitHubOwner    string
	GitHubRepo     string
	GitHubRepoID   int64
	LinkedByUserID int
	WebhookSecret  string
	LinkedAt       time.Time
	UpdatedAt      time.Time
}

// TaskGitHubIssue is the persisted link from an Ordryn task to a GitHub issue.
type TaskGitHubIssue struct {
	TaskID        int
	IssueID       int64
	IssueNumber   int
	IssueURL      string
	IssueState    string
	IssueTitle    string
	LastSyncedAt  *time.Time
	LastSyncError string
}

// CreateGitHubTables creates GitHub integration tables.
func CreateGitHubTables() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	stmts := []string{
		`CREATE TABLE IF NOT EXISTS github_connections (
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			owner_scope VARCHAR(255) NOT NULL DEFAULT '',
			access_token_enc TEXT NOT NULL,
			github_user_id BIGINT NOT NULL DEFAULT 0,
			github_login VARCHAR(255) NOT NULL DEFAULT '',
			auth_method VARCHAR(16) NOT NULL,
			connected_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (user_id, owner_scope),
			CHECK (auth_method IN ('oauth', 'pat'))
		)`,
		// Migrate single-connection tables (PK user_id) to per-owner connections.
		// Existing rows become the catch-all connection (owner_scope '').
		`ALTER TABLE github_connections ADD COLUMN IF NOT EXISTS owner_scope VARCHAR(255) NOT NULL DEFAULT ''`,
		`DO $$
		BEGIN
			IF EXISTS (
				SELECT 1 FROM pg_index i
				JOIN pg_class c ON c.oid = i.indrelid
				WHERE c.relname = 'github_connections' AND i.indisprimary AND i.indnatts = 1
			) THEN
				ALTER TABLE github_connections DROP CONSTRAINT github_connections_pkey;
				ALTER TABLE github_connections ADD PRIMARY KEY (user_id, owner_scope);
			END IF;
		END $$`,
		`CREATE TABLE IF NOT EXISTS project_github_repos (
			project_id INTEGER PRIMARY KEY REFERENCES projects(id) ON DELETE CASCADE,
			github_owner VARCHAR(255) NOT NULL,
			github_repo VARCHAR(255) NOT NULL,
			github_repo_id BIGINT NOT NULL DEFAULT 0,
			linked_by_user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			webhook_secret VARCHAR(64) NOT NULL DEFAULT '',
			linked_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_project_github_repos_repo
			ON project_github_repos (lower(github_owner), lower(github_repo))`,
		`CREATE TABLE IF NOT EXISTS task_github_issues (
			task_id INTEGER PRIMARY KEY REFERENCES tasks(id) ON DELETE CASCADE,
			issue_id BIGINT NOT NULL DEFAULT 0,
			issue_number INTEGER NOT NULL,
			issue_url TEXT NOT NULL DEFAULT '',
			issue_state VARCHAR(16) NOT NULL DEFAULT 'open',
			issue_title TEXT NOT NULL DEFAULT '',
			last_synced_at TIMESTAMPTZ,
			last_sync_error TEXT NOT NULL DEFAULT '',
			UNIQUE (issue_id),
			CHECK (issue_number > 0)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_task_github_issues_issue_id ON task_github_issues (issue_id)`,
	}
	for _, q := range stmts {
		if _, err := pool.Exec(context.Background(), q); err != nil {
			return fmt.Errorf("create github tables: %w", err)
		}
	}
	return nil
}

const githubConnectionCols = `user_id, owner_scope, access_token_enc, github_user_id, github_login, auth_method, connected_at, updated_at`

func scanGitHubConnection(row pgx.Row) (*GitHubConnection, error) {
	var c GitHubConnection
	if err := row.Scan(&c.UserID, &c.OwnerScope, &c.AccessTokenEnc, &c.GitHubUserID, &c.GitHubLogin,
		&c.AuthMethod, &c.ConnectedAt, &c.UpdatedAt); err != nil {
		return nil, err
	}
	return &c, nil
}

// NormalizeGitHubOwnerScope lowercases and trims an owner scope ("" = catch-all).
func NormalizeGitHubOwnerScope(owner string) string {
	return strings.ToLower(strings.TrimSpace(owner))
}

// UpsertGitHubConnection stores or replaces the user's connection for ownerScope.
func UpsertGitHubConnection(userID int, ownerScope, tokenEnc, login, method string, githubUserID int64) (*GitHubConnection, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	method = strings.TrimSpace(strings.ToLower(method))
	if method != GitHubAuthOAuth && method != GitHubAuthPAT {
		return nil, fmt.Errorf("invalid auth method")
	}

	c, err := scanGitHubConnection(pool.QueryRow(context.Background(), `
		INSERT INTO github_connections (
			user_id, owner_scope, access_token_enc, github_user_id, github_login, auth_method, connected_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, NOW(), NOW())
		ON CONFLICT (user_id, owner_scope) DO UPDATE SET
			access_token_enc = EXCLUDED.access_token_enc,
			github_user_id = EXCLUDED.github_user_id,
			github_login = EXCLUDED.github_login,
			auth_method = EXCLUDED.auth_method,
			updated_at = NOW()
		RETURNING `+githubConnectionCols,
		userID, NormalizeGitHubOwnerScope(ownerScope), tokenEnc, githubUserID, strings.TrimSpace(login), method,
	))
	if err != nil {
		return nil, fmt.Errorf("upsert github connection: %w", err)
	}
	return c, nil
}

// ListGitHubConnections returns all of a user's connections, catch-all first.
func ListGitHubConnections(userID int) ([]GitHubConnection, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(), `
		SELECT `+githubConnectionCols+`
		FROM github_connections WHERE user_id = $1
		ORDER BY owner_scope`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GitHubConnection
	for rows.Next() {
		c, err := scanGitHubConnection(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

// GetGitHubConnectionForOwner returns the user's connection that should be used
// for repositories owned by owner: an exact owner-scoped connection first, else
// the catch-all connection. Returns nil if neither exists.
func GetGitHubConnectionForOwner(userID int, owner string) (*GitHubConnection, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	c, err := scanGitHubConnection(pool.QueryRow(context.Background(), `
		SELECT `+githubConnectionCols+`
		FROM github_connections
		WHERE user_id = $1 AND owner_scope IN ($2, '')
		ORDER BY owner_scope DESC
		LIMIT 1`, userID, NormalizeGitHubOwnerScope(owner),
	))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return c, nil
}

// DeleteGitHubConnection removes the user's connection for ownerScope ("" = catch-all).
func DeleteGitHubConnection(userID int, ownerScope string) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	_, err = pool.Exec(context.Background(),
		`DELETE FROM github_connections WHERE user_id = $1 AND owner_scope = $2`,
		userID, NormalizeGitHubOwnerScope(ownerScope))
	return err
}

// DeleteAllGitHubConnections removes every GitHub connection for the user.
func DeleteAllGitHubConnections(userID int) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	_, err = pool.Exec(context.Background(), `DELETE FROM github_connections WHERE user_id = $1`, userID)
	return err
}

// UpsertProjectGitHubRepo links a project to a GitHub repository.
func UpsertProjectGitHubRepo(projectID, linkedBy int, owner, repo string, repoID int64, webhookSecret string) (*ProjectGitHubRepo, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	var r ProjectGitHubRepo
	err = pool.QueryRow(context.Background(), `
		INSERT INTO project_github_repos (
			project_id, github_owner, github_repo, github_repo_id, linked_by_user_id, webhook_secret, linked_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, NOW(), NOW())
		ON CONFLICT (project_id) DO UPDATE SET
			github_owner = EXCLUDED.github_owner,
			github_repo = EXCLUDED.github_repo,
			github_repo_id = EXCLUDED.github_repo_id,
			linked_by_user_id = EXCLUDED.linked_by_user_id,
			webhook_secret = CASE
				WHEN EXCLUDED.webhook_secret <> '' THEN EXCLUDED.webhook_secret
				ELSE project_github_repos.webhook_secret
			END,
			updated_at = NOW()
		RETURNING project_id, github_owner, github_repo, github_repo_id, linked_by_user_id,
		          webhook_secret, linked_at, updated_at`,
		projectID, owner, repo, repoID, linkedBy, webhookSecret,
	).Scan(&r.ProjectID, &r.GitHubOwner, &r.GitHubRepo, &r.GitHubRepoID, &r.LinkedByUserID,
		&r.WebhookSecret, &r.LinkedAt, &r.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("upsert project github repo: %w", err)
	}
	return &r, nil
}

// GetProjectGitHubRepo returns the linked repo for a project, or nil.
func GetProjectGitHubRepo(projectID int) (*ProjectGitHubRepo, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	var r ProjectGitHubRepo
	err = pool.QueryRow(context.Background(), `
		SELECT project_id, github_owner, github_repo, github_repo_id, linked_by_user_id,
		       webhook_secret, linked_at, updated_at
		FROM project_github_repos WHERE project_id = $1`, projectID,
	).Scan(&r.ProjectID, &r.GitHubOwner, &r.GitHubRepo, &r.GitHubRepoID, &r.LinkedByUserID,
		&r.WebhookSecret, &r.LinkedAt, &r.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &r, nil
}

// FindProjectGitHubRepoByNames finds a project link by owner/repo (case-insensitive).
func FindProjectGitHubRepoByNames(owner, repo string) (*ProjectGitHubRepo, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	var r ProjectGitHubRepo
	err = pool.QueryRow(context.Background(), `
		SELECT project_id, github_owner, github_repo, github_repo_id, linked_by_user_id,
		       webhook_secret, linked_at, updated_at
		FROM project_github_repos
		WHERE lower(github_owner) = lower($1) AND lower(github_repo) = lower($2)
		LIMIT 1`, owner, repo,
	).Scan(&r.ProjectID, &r.GitHubOwner, &r.GitHubRepo, &r.GitHubRepoID, &r.LinkedByUserID,
		&r.WebhookSecret, &r.LinkedAt, &r.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &r, nil
}

// DeleteProjectGitHubRepo unlinks the repo and clears task issue links for that project.
func DeleteProjectGitHubRepo(projectID int) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	tx, err := pool.Begin(context.Background())
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())

	_, err = tx.Exec(context.Background(), `
		DELETE FROM task_github_issues
		WHERE task_id IN (SELECT id FROM tasks WHERE project_id = $1)`, projectID)
	if err != nil {
		return err
	}
	_, err = tx.Exec(context.Background(), `DELETE FROM project_github_repos WHERE project_id = $1`, projectID)
	if err != nil {
		return err
	}
	return tx.Commit(context.Background())
}

// UpsertTaskGitHubIssue stores or replaces a task↔issue link.
func UpsertTaskGitHubIssue(taskID int, issueID int64, number int, url, state, title string) (*TaskGitHubIssue, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	var issue TaskGitHubIssue
	var lastSynced sql.NullTime
	err = pool.QueryRow(context.Background(), `
		INSERT INTO task_github_issues (
			task_id, issue_id, issue_number, issue_url, issue_state, issue_title, last_synced_at, last_sync_error
		) VALUES ($1, $2, $3, $4, $5, $6, NOW(), '')
		ON CONFLICT (task_id) DO UPDATE SET
			issue_id = EXCLUDED.issue_id,
			issue_number = EXCLUDED.issue_number,
			issue_url = EXCLUDED.issue_url,
			issue_state = EXCLUDED.issue_state,
			issue_title = EXCLUDED.issue_title,
			last_synced_at = NOW(),
			last_sync_error = ''
		RETURNING task_id, issue_id, issue_number, issue_url, issue_state, issue_title, last_synced_at, last_sync_error`,
		taskID, issueID, number, url, state, title,
	).Scan(&issue.TaskID, &issue.IssueID, &issue.IssueNumber, &issue.IssueURL, &issue.IssueState,
		&issue.IssueTitle, &lastSynced, &issue.LastSyncError)
	if err != nil {
		return nil, fmt.Errorf("upsert task github issue: %w", err)
	}
	if lastSynced.Valid {
		t := lastSynced.Time
		issue.LastSyncedAt = &t
	}
	return &issue, nil
}

// GetTaskGitHubIssue returns the issue link for a task, or nil.
func GetTaskGitHubIssue(taskID int) (*TaskGitHubIssue, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	var issue TaskGitHubIssue
	var lastSynced sql.NullTime
	err = pool.QueryRow(context.Background(), `
		SELECT task_id, issue_id, issue_number, issue_url, issue_state, issue_title, last_synced_at, last_sync_error
		FROM task_github_issues WHERE task_id = $1`, taskID,
	).Scan(&issue.TaskID, &issue.IssueID, &issue.IssueNumber, &issue.IssueURL, &issue.IssueState,
		&issue.IssueTitle, &lastSynced, &issue.LastSyncError)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if lastSynced.Valid {
		t := lastSynced.Time
		issue.LastSyncedAt = &t
	}
	return &issue, nil
}

// GetTaskGitHubIssueByIssueID finds a task link by GitHub issue id.
func GetTaskGitHubIssueByIssueID(issueID int64) (*TaskGitHubIssue, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	var issue TaskGitHubIssue
	var lastSynced sql.NullTime
	err = pool.QueryRow(context.Background(), `
		SELECT task_id, issue_id, issue_number, issue_url, issue_state, issue_title, last_synced_at, last_sync_error
		FROM task_github_issues WHERE issue_id = $1`, issueID,
	).Scan(&issue.TaskID, &issue.IssueID, &issue.IssueNumber, &issue.IssueURL, &issue.IssueState,
		&issue.IssueTitle, &lastSynced, &issue.LastSyncError)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if lastSynced.Valid {
		t := lastSynced.Time
		issue.LastSyncedAt = &t
	}
	return &issue, nil
}

// DeleteTaskGitHubIssue removes the link (does not touch GitHub).
func DeleteTaskGitHubIssue(taskID int) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	_, err = pool.Exec(context.Background(), `DELETE FROM task_github_issues WHERE task_id = $1`, taskID)
	return err
}

// UpdateTaskGitHubIssueState updates local issue state after sync.
func UpdateTaskGitHubIssueState(taskID int, state string, syncErr string) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	_, err = pool.Exec(context.Background(), `
		UPDATE task_github_issues
		SET issue_state = $2, last_synced_at = NOW(), last_sync_error = $3
		WHERE task_id = $1`, taskID, state, syncErr)
	return err
}

// GetGitHubIssuesForTasks returns issue links keyed by task id.
func GetGitHubIssuesForTasks(taskIDs []int) (map[int]TaskGitHubIssue, error) {
	out := make(map[int]TaskGitHubIssue)
	if len(taskIDs) == 0 {
		return out, nil
	}
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(), `
		SELECT task_id, issue_id, issue_number, issue_url, issue_state, issue_title, last_synced_at, last_sync_error
		FROM task_github_issues WHERE task_id = ANY($1)`, taskIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var issue TaskGitHubIssue
		var lastSynced sql.NullTime
		if err := rows.Scan(&issue.TaskID, &issue.IssueID, &issue.IssueNumber, &issue.IssueURL,
			&issue.IssueState, &issue.IssueTitle, &lastSynced, &issue.LastSyncError); err != nil {
			return nil, err
		}
		if lastSynced.Valid {
			t := lastSynced.Time
			issue.LastSyncedAt = &t
		}
		out[issue.TaskID] = issue
	}
	return out, rows.Err()
}

// TaskTitleDescription is a minimal task payload for GitHub issue creation.
type TaskTitleDescription struct {
	Title       string
	Description string
}

// GetTaskTitleDescription returns title and description for a task.
func GetTaskTitleDescription(taskID int) (*TaskTitleDescription, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	var t TaskTitleDescription
	err = pool.QueryRow(context.Background(),
		`SELECT title, COALESCE(description, '') FROM tasks WHERE id = $1`, taskID,
	).Scan(&t.Title, &t.Description)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("task not found")
		}
		return nil, err
	}
	return &t, nil
}

// GetTaskProjectID returns the project_id for a task (0 if none).
func GetTaskProjectID(taskID int) (int, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return 0, err
	}
	defer CloseDatabase(pool)

	var proj sql.NullInt64
	err = pool.QueryRow(context.Background(),
		`SELECT project_id FROM tasks WHERE id = $1`, taskID).Scan(&proj)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			return 0, fmt.Errorf("task not found")
		}
		return 0, err
	}
	if !proj.Valid {
		return 0, nil
	}
	return int(proj.Int64), nil
}
