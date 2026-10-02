package storage

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrAgentNotFound    = errors.New("agent not found")
	ErrAgentRunNotFound = errors.New("agent run not found")
	// ErrAgentRunOpen means the agent already has a queued or running run for the task.
	ErrAgentRunOpen = errors.New("agent already has an open run for this task")
)

// Agent run lifecycle.
const (
	AgentRunQueued    = "queued"
	AgentRunRunning   = "running"
	AgentRunSucceeded = "succeeded"
	AgentRunFailed    = "failed"
	AgentRunCancelled = "cancelled"
)

// How an agent run was started.
const (
	AgentTriggerManual  = "manual"
	AgentTriggerMention = "mention"
	AgentTriggerStatus  = "status"
)

// Who may start an agent run.
const (
	AgentTriggerByManagers = "managers"
	AgentTriggerByWriters  = "writers"
	// AgentTriggerBySelected limits callers to TriggerRoleSlugs and TriggerUserIDs.
	AgentTriggerBySelected = "selected"
)

// Task fields an agent may be allowed to edit. Status and completion are
// governed separately by AllowedStatusIDs and CanComplete.
const (
	AgentFieldTitle        = "title"
	AgentFieldDescription  = "description"
	AgentFieldPriority     = "priority"
	AgentFieldDueDate      = "due_date"
	AgentFieldTags         = "tags"
	AgentFieldEstimate     = "estimate"
	AgentFieldSprint       = "sprint"
	AgentFieldCustomFields = "custom_fields"
	AgentFieldStatus       = "status"
)

// AgentEditableFields lists every field an agent can be granted, in display order.
var AgentEditableFields = []string{
	AgentFieldStatus, AgentFieldTitle, AgentFieldDescription, AgentFieldPriority, AgentFieldDueDate,
	AgentFieldTags, AgentFieldEstimate, AgentFieldSprint, AgentFieldCustomFields,
}

// ProjectAgent is an AI agent member of one project. Each agent is backed by
// a users row (is_agent) so comments, claims, mentions, and activity work
// like any other member; the agent row holds its triggers and guardrails.
type ProjectAgent struct {
	ID          int
	ProjectID   int
	UserID      int
	Handle      string // the backing user's username, used for @mentions
	Name        string
	Description string
	// Instructions are standing directions sent with every run.
	Instructions string
	Enabled      bool
	Role         string // project membership role slug
	CreatedBy    int
	CreatedAt    time.Time
	UpdatedAt    time.Time

	TriggerOnMention bool
	TriggerStatusIDs []int
	TriggerBy        string
	TriggerRoleSlugs []string // roles that may call the agent (TriggerBy "selected")
	TriggerUserIDs   []int    // members who may call the agent (TriggerBy "selected")
	ClaimOnDispatch  bool

	AllowedStatusIDs []int
	EditableFields   []string
	CanComplete      bool
	CanCreateTasks   bool
	CanComment       bool
	MaxRunsPerHour   int

	WebhookURL        string
	WebhookSecretSet  bool
	LastDeliveryAt    *time.Time
	LastDeliveryError string
}

// HasField reports whether the agent may edit field.
func (a *ProjectAgent) HasField(field string) bool {
	if a == nil {
		return false
	}
	for _, f := range a.EditableFields {
		if f == field {
			return true
		}
	}
	return false
}

// AgentRun is one request for an agent to work on a task.
type AgentRun struct {
	ID            int
	AgentID       int
	ProjectID     int
	TaskID        int
	Trigger       string
	TriggeredBy   int
	Note          string
	Status        string
	DeliveryError string
	Delivered     bool
	Summary       string
	CreatedAt     time.Time
	StartedAt     *time.Time
	FinishedAt    *time.Time
	// Filled by list queries for display.
	AgentName       string
	TaskTitle       string
	TriggeredByName string
}

// IsOpen reports whether the run is still waiting on the agent.
func (r *AgentRun) IsOpen() bool {
	return r != nil && (r.Status == AgentRunQueued || r.Status == AgentRunRunning)
}

// MigrateUsersAddIsAgent marks users rows that back AI agents.
func MigrateUsersAddIsAgent() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	_, err = pool.Exec(context.Background(),
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS is_agent BOOLEAN NOT NULL DEFAULT FALSE`)
	if err != nil {
		return fmt.Errorf("failed to add users.is_agent: %v", err)
	}
	return nil
}

// CreateProjectAgentTables creates project_agents and agent_runs.
func CreateProjectAgentTables() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	stmts := []string{
		`CREATE TABLE IF NOT EXISTS project_agents (
			id SERIAL PRIMARY KEY,
			project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
			user_id INTEGER NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
			name VARCHAR(80) NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			instructions TEXT NOT NULL DEFAULT '',
			enabled BOOLEAN NOT NULL DEFAULT TRUE,
			created_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			removed_at TIMESTAMPTZ,
			trigger_on_mention BOOLEAN NOT NULL DEFAULT TRUE,
			trigger_status_ids INTEGER[] NOT NULL DEFAULT '{}',
			trigger_by TEXT NOT NULL DEFAULT 'managers',
			claim_on_dispatch BOOLEAN NOT NULL DEFAULT TRUE,
			allowed_status_ids INTEGER[] NOT NULL DEFAULT '{}',
			editable_fields TEXT[] NOT NULL DEFAULT '{}',
			can_complete BOOLEAN NOT NULL DEFAULT FALSE,
			can_create_tasks BOOLEAN NOT NULL DEFAULT FALSE,
			can_comment BOOLEAN NOT NULL DEFAULT TRUE,
			max_runs_per_hour INTEGER NOT NULL DEFAULT 20,
			webhook_url TEXT NOT NULL DEFAULT '',
			webhook_secret_enc TEXT NOT NULL DEFAULT '',
			last_delivery_at TIMESTAMPTZ,
			last_delivery_error TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_project_agents_project ON project_agents (project_id)`,
		`ALTER TABLE project_agents ADD COLUMN IF NOT EXISTS trigger_role_slugs TEXT[] NOT NULL DEFAULT '{}'`,
		`ALTER TABLE project_agents ADD COLUMN IF NOT EXISTS trigger_user_ids INTEGER[] NOT NULL DEFAULT '{}'`,
		`CREATE TABLE IF NOT EXISTS agent_runs (
			id SERIAL PRIMARY KEY,
			agent_id INTEGER NOT NULL REFERENCES project_agents(id) ON DELETE CASCADE,
			project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
			task_id INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
			trigger TEXT NOT NULL,
			triggered_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
			note TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'queued',
			delivered BOOLEAN NOT NULL DEFAULT FALSE,
			delivery_error TEXT NOT NULL DEFAULT '',
			summary TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			started_at TIMESTAMPTZ,
			finished_at TIMESTAMPTZ
		)`,
		`CREATE INDEX IF NOT EXISTS idx_agent_runs_agent ON agent_runs (agent_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_agent_runs_task ON agent_runs (task_id, created_at DESC)`,
		// At most one open run per agent and task, so repeat triggers don't pile up.
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_runs_open
			ON agent_runs (agent_id, task_id) WHERE status IN ('queued', 'running')`,
	}
	for _, s := range stmts {
		if _, err := pool.Exec(context.Background(), s); err != nil {
			return fmt.Errorf("failed to create agent tables: %v", err)
		}
	}
	return nil
}

const projectAgentSelectCols = `a.id, a.project_id, a.user_id, COALESCE(u.user_name, ''), a.name, a.description,
	a.instructions, a.enabled, COALESCE(pm.role, ''), COALESCE(a.created_by, 0), a.created_at, a.updated_at,
	a.trigger_on_mention, a.trigger_status_ids, a.trigger_by, a.trigger_role_slugs, a.trigger_user_ids, a.claim_on_dispatch,
	a.allowed_status_ids, a.editable_fields, a.can_complete, a.can_create_tasks, a.can_comment,
	a.max_runs_per_hour, a.webhook_url, a.webhook_secret_enc <> '', a.last_delivery_at, a.last_delivery_error`

const projectAgentFrom = `FROM project_agents a
	JOIN users u ON u.id = a.user_id
	LEFT JOIN project_members pm ON pm.project_id = a.project_id AND pm.user_id = a.user_id`

func scanProjectAgent(row interface{ Scan(dest ...any) error }, a *ProjectAgent) error {
	var triggerIDs, allowedIDs, triggerUsers []int32
	if err := row.Scan(
		&a.ID, &a.ProjectID, &a.UserID, &a.Handle, &a.Name, &a.Description,
		&a.Instructions, &a.Enabled, &a.Role, &a.CreatedBy, &a.CreatedAt, &a.UpdatedAt,
		&a.TriggerOnMention, &triggerIDs, &a.TriggerBy, &a.TriggerRoleSlugs, &triggerUsers, &a.ClaimOnDispatch,
		&allowedIDs, &a.EditableFields, &a.CanComplete, &a.CanCreateTasks, &a.CanComment,
		&a.MaxRunsPerHour, &a.WebhookURL, &a.WebhookSecretSet, &a.LastDeliveryAt, &a.LastDeliveryError,
	); err != nil {
		return err
	}
	a.TriggerStatusIDs = int32sToInts(triggerIDs)
	a.AllowedStatusIDs = int32sToInts(allowedIDs)
	a.TriggerUserIDs = int32sToInts(triggerUsers)
	if a.TriggerRoleSlugs == nil {
		a.TriggerRoleSlugs = []string{}
	}
	if a.EditableFields == nil {
		a.EditableFields = []string{}
	}
	return nil
}

func int32sToInts(in []int32) []int {
	out := make([]int, 0, len(in))
	for _, v := range in {
		out = append(out, int(v))
	}
	return out
}

func newAgentEmail() (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	// .invalid is reserved (RFC 2606), so mail can never be delivered here.
	return "agent-" + hex.EncodeToString(b) + "@agents.invalid", nil
}

// agentPasswordSentinel is not a bcrypt hash, so no password ever matches it.
const agentPasswordSentinel = "!agent-no-login"

// CreateProjectAgent creates the agent's backing user, adds it to the project
// with role, and stores the agent row, all in one transaction.
func CreateProjectAgent(a ProjectAgent) (*ProjectAgent, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	roleID, err := GetRoleIDByName("user")
	if err != nil {
		return nil, err
	}
	email, err := newAgentEmail()
	if err != nil {
		return nil, err
	}

	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var userID int
	err = tx.QueryRow(ctx,
		`INSERT INTO users (email, password, role_id, user_name, is_agent, allow_project_invites)
		 VALUES ($1, $2, $3, $4, TRUE, FALSE) RETURNING id`,
		email, agentPasswordSentinel, roleID, a.Handle).Scan(&userID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, ErrUsernameTaken
		}
		return nil, fmt.Errorf("create agent user: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO project_members (project_id, user_id, role) VALUES ($1, $2, $3)`,
		a.ProjectID, userID, a.Role); err != nil {
		return nil, fmt.Errorf("add agent member: %w", err)
	}
	var id int
	err = tx.QueryRow(ctx, `
		INSERT INTO project_agents (
			project_id, user_id, name, description, instructions, enabled, created_by,
			trigger_on_mention, trigger_status_ids, trigger_by, claim_on_dispatch,
			allowed_status_ids, editable_fields, can_complete, can_create_tasks, can_comment,
			max_runs_per_hour, webhook_url, trigger_role_slugs, trigger_user_ids)
		VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, 0), $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20)
		RETURNING id`,
		a.ProjectID, userID, a.Name, a.Description, a.Instructions, a.Enabled, a.CreatedBy,
		a.TriggerOnMention, a.TriggerStatusIDs, a.TriggerBy, a.ClaimOnDispatch,
		a.AllowedStatusIDs, a.EditableFields, a.CanComplete, a.CanCreateTasks, a.CanComment,
		a.MaxRunsPerHour, a.WebhookURL, nonNilStrings(a.TriggerRoleSlugs), nonNilInts(a.TriggerUserIDs)).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("create agent: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return GetProjectAgent(a.ProjectID, id)
}

// ErrUsernameTaken is returned when a new account's username is already in use.
var ErrUsernameTaken = errors.New("username is already taken")

// GetProjectAgent loads one active (not removed) agent of a project.
func GetProjectAgent(projectID, agentID int) (*ProjectAgent, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	var a ProjectAgent
	err = scanProjectAgent(pool.QueryRow(context.Background(),
		`SELECT `+projectAgentSelectCols+` `+projectAgentFrom+`
		 WHERE a.id = $1 AND a.project_id = $2 AND a.removed_at IS NULL`, agentID, projectID), &a)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			return nil, ErrAgentNotFound
		}
		return nil, err
	}
	return &a, nil
}

// GetAgentByUserID loads the active agent backed by userID.
func GetAgentByUserID(userID int) (*ProjectAgent, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	var a ProjectAgent
	err = scanProjectAgent(pool.QueryRow(context.Background(),
		`SELECT `+projectAgentSelectCols+` `+projectAgentFrom+`
		 WHERE a.user_id = $1 AND a.removed_at IS NULL`, userID), &a)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			return nil, ErrAgentNotFound
		}
		return nil, err
	}
	return &a, nil
}

// ListProjectAgents returns a project's active agents, oldest first.
func ListProjectAgents(projectID int) ([]ProjectAgent, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(),
		`SELECT `+projectAgentSelectCols+` `+projectAgentFrom+`
		 WHERE a.project_id = $1 AND a.removed_at IS NULL
		 ORDER BY a.created_at ASC, a.id ASC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ProjectAgent, 0)
	for rows.Next() {
		var a ProjectAgent
		if err := scanProjectAgent(rows, &a); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ListEnabledAgentsForProject returns agents that may receive runs.
func ListEnabledAgentsForProject(projectID int) ([]ProjectAgent, error) {
	all, err := ListProjectAgents(projectID)
	if err != nil {
		return nil, err
	}
	out := all[:0]
	for _, a := range all {
		if a.Enabled && a.Role != "" {
			out = append(out, a)
		}
	}
	return out, nil
}

// UpdateProjectAgent saves an agent's editable settings (not its handle,
// role, or webhook secret, which have their own setters).
func UpdateProjectAgent(a ProjectAgent) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	tag, err := pool.Exec(context.Background(), `
		UPDATE project_agents SET
			name = $3, description = $4, instructions = $5, enabled = $6,
			trigger_on_mention = $7, trigger_status_ids = $8, trigger_by = $9, claim_on_dispatch = $10,
			allowed_status_ids = $11, editable_fields = $12, can_complete = $13, can_create_tasks = $14,
			can_comment = $15, max_runs_per_hour = $16, webhook_url = $17,
			trigger_role_slugs = $18, trigger_user_ids = $19, updated_at = NOW()
		WHERE id = $1 AND project_id = $2 AND removed_at IS NULL`,
		a.ID, a.ProjectID, a.Name, a.Description, a.Instructions, a.Enabled,
		a.TriggerOnMention, a.TriggerStatusIDs, a.TriggerBy, a.ClaimOnDispatch,
		a.AllowedStatusIDs, a.EditableFields, a.CanComplete, a.CanCreateTasks,
		a.CanComment, a.MaxRunsPerHour, a.WebhookURL,
		nonNilStrings(a.TriggerRoleSlugs), nonNilInts(a.TriggerUserIDs))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrAgentNotFound
	}
	return nil
}

// SetAgentWebhookSecret stores an encrypted signing secret ("" clears it).
func SetAgentWebhookSecret(agentID int, encrypted string) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	_, err = pool.Exec(context.Background(),
		`UPDATE project_agents SET webhook_secret_enc = $2, updated_at = NOW() WHERE id = $1`, agentID, encrypted)
	return err
}

// GetAgentWebhookSecretEnc returns the encrypted signing secret, or "".
func GetAgentWebhookSecretEnc(agentID int) (string, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return "", err
	}
	defer CloseDatabase(pool)
	var enc string
	err = pool.QueryRow(context.Background(),
		`SELECT webhook_secret_enc FROM project_agents WHERE id = $1`, agentID).Scan(&enc)
	return enc, err
}

// RecordAgentDelivery notes the outcome of the latest webhook delivery.
func RecordAgentDelivery(agentID int, errMsg string) {
	pool, err := OpenDatabase()
	if err != nil {
		return
	}
	defer CloseDatabase(pool)
	_, _ = pool.Exec(context.Background(),
		`UPDATE project_agents SET last_delivery_at = NOW(), last_delivery_error = $2 WHERE id = $1`,
		agentID, truncateRunes(errMsg, 500))
}

// RemoveProjectAgent retires an agent: it leaves the project, its keys are
// revoked, open runs are cancelled, and its account is banned. The user row
// stays so its comments and activity keep their author.
func RemoveProjectAgent(projectID, agentID int) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var userID int
	err = tx.QueryRow(ctx,
		`UPDATE project_agents SET removed_at = NOW(), enabled = FALSE, updated_at = NOW()
		 WHERE id = $1 AND project_id = $2 AND removed_at IS NULL RETURNING user_id`,
		agentID, projectID).Scan(&userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrAgentNotFound
		}
		return err
	}
	for _, stmt := range []struct {
		sql  string
		args []any
	}{
		{`DELETE FROM project_members WHERE project_id = $1 AND user_id = $2`, []any{projectID, userID}},
		{`UPDATE api_keys SET revoked_at = NOW() WHERE user_id = $1 AND revoked_at IS NULL`, []any{userID}},
		{`UPDATE users SET is_banned = TRUE WHERE id = $1 AND is_agent`, []any{userID}},
		{`UPDATE tasks SET claimed_by = NULL WHERE claimed_by = $1 AND project_id = $2`, []any{userID, projectID}},
	} {
		if _, err := tx.Exec(ctx, stmt.sql, stmt.args...); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx,
		`UPDATE agent_runs SET status = 'cancelled', finished_at = NOW(), summary = 'Agent removed'
		 WHERE agent_id = $1 AND status IN ('queued', 'running')`, agentID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// CountActiveAgents returns how many enabled agents exist site-wide.
func CountActiveAgents() (int, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return 0, err
	}
	defer CloseDatabase(pool)
	var n int
	err = pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM project_agents WHERE enabled AND removed_at IS NULL`).Scan(&n)
	return n, err
}

// IsAgentUser reports whether userID backs an AI agent.
func IsAgentUser(userID int) bool {
	if userID <= 0 {
		return false
	}
	pool, err := OpenDatabase()
	if err != nil {
		return false
	}
	defer CloseDatabase(pool)
	var isAgent bool
	_ = pool.QueryRow(context.Background(),
		`SELECT COALESCE(is_agent, FALSE) FROM users WHERE id = $1`, userID).Scan(&isAgent)
	return isAgent
}

// IsAgentEmail reports whether email belongs to an agent account.
func IsAgentEmail(email string) bool {
	return strings.HasSuffix(strings.ToLower(strings.TrimSpace(email)), "@agents.invalid")
}

// ListAgentAPIKeys returns an agent's active keys.
func ListAgentAPIKeys(agentUserID int) ([]APIKey, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(),
		`SELECT id, user_id, name, key_prefix, created_at, last_used_at, project_id, scopes, expires_at
		 FROM api_keys WHERE user_id = $1 AND revoked_at IS NULL
		 ORDER BY created_at DESC`, agentUserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]APIKey, 0)
	for rows.Next() {
		var k APIKey
		if err := rows.Scan(&k.ID, &k.UserID, &k.Name, &k.KeyPrefix, &k.CreatedAt, &k.LastUsedAt,
			&k.ProjectID, &k.Scopes, &k.ExpiresAt); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// RevokeAgentAPIKey revokes one of an agent's keys.
func RevokeAgentAPIKey(agentUserID, keyID int) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	tag, err := pool.Exec(context.Background(),
		`UPDATE api_keys SET revoked_at = NOW() WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL`,
		keyID, agentUserID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrAPIKeyNotFound
	}
	return nil
}

const agentRunSelectCols = `r.id, r.agent_id, r.project_id, r.task_id, r.trigger, COALESCE(r.triggered_by, 0),
	r.note, r.status, r.delivered, r.delivery_error, r.summary, r.created_at, r.started_at, r.finished_at,
	COALESCE(a.name, ''), COALESCE(t.title, ''), COALESCE(NULLIF(tu.user_name, ''), tu.email, '')`

const agentRunFrom = `FROM agent_runs r
	JOIN project_agents a ON a.id = r.agent_id
	LEFT JOIN tasks t ON t.id = r.task_id
	LEFT JOIN users tu ON tu.id = r.triggered_by`

func scanAgentRun(row interface{ Scan(dest ...any) error }, r *AgentRun) error {
	return row.Scan(&r.ID, &r.AgentID, &r.ProjectID, &r.TaskID, &r.Trigger, &r.TriggeredBy,
		&r.Note, &r.Status, &r.Delivered, &r.DeliveryError, &r.Summary, &r.CreatedAt, &r.StartedAt, &r.FinishedAt,
		&r.AgentName, &r.TaskTitle, &r.TriggeredByName)
}

// CreateAgentRun queues a run. Returns ErrAgentRunOpen when one is already open.
func CreateAgentRun(r AgentRun) (*AgentRun, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	var id int
	err = pool.QueryRow(context.Background(), `
		INSERT INTO agent_runs (agent_id, project_id, task_id, trigger, triggered_by, note)
		VALUES ($1, $2, $3, $4, NULLIF($5, 0), $6) RETURNING id`,
		r.AgentID, r.ProjectID, r.TaskID, r.Trigger, r.TriggeredBy, r.Note).Scan(&id)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, ErrAgentRunOpen
		}
		return nil, err
	}
	return GetAgentRun(id)
}

// GetAgentRun loads one run.
func GetAgentRun(runID int) (*AgentRun, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	var r AgentRun
	err = scanAgentRun(pool.QueryRow(context.Background(),
		`SELECT `+agentRunSelectCols+` `+agentRunFrom+` WHERE r.id = $1`, runID), &r)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			return nil, ErrAgentRunNotFound
		}
		return nil, err
	}
	return &r, nil
}

// AgentRunFilter narrows ListAgentRuns. Zero values mean "any".
type AgentRunFilter struct {
	ProjectID int
	AgentID   int
	TaskID    int
	Statuses  []string
	Limit     int
}

// ListAgentRuns returns runs newest first.
func ListAgentRuns(f AgentRunFilter) ([]AgentRun, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	statuses := f.Statuses
	if statuses == nil {
		statuses = []string{}
	}
	rows, err := pool.Query(context.Background(), `
		SELECT `+agentRunSelectCols+` `+agentRunFrom+`
		WHERE ($1 = 0 OR r.project_id = $1)
		  AND ($2 = 0 OR r.agent_id = $2)
		  AND ($3 = 0 OR r.task_id = $3)
		  AND (cardinality($4::text[]) = 0 OR r.status = ANY($4))
		ORDER BY r.created_at DESC, r.id DESC
		LIMIT $5`,
		f.ProjectID, f.AgentID, f.TaskID, statuses, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]AgentRun, 0)
	for rows.Next() {
		var r AgentRun
		if err := scanAgentRun(rows, &r); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// CountAgentRunsSince counts runs created for an agent after since.
func CountAgentRunsSince(agentID int, since time.Time) (int, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return 0, err
	}
	defer CloseDatabase(pool)
	var n int
	err = pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM agent_runs WHERE agent_id = $1 AND created_at > $2`, agentID, since).Scan(&n)
	return n, err
}

// TransitionAgentRun moves a run from one of from to to. It returns
// ErrAgentRunNotFound when the run is not in an allowed state.
func TransitionAgentRun(runID, agentID int, from []string, to, summary string) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	tag, err := pool.Exec(context.Background(), `
		UPDATE agent_runs SET
			status = $3,
			summary = CASE WHEN $4 = '' THEN summary ELSE $4 END,
			started_at = CASE WHEN $3 = 'running' THEN COALESCE(started_at, NOW()) ELSE started_at END,
			finished_at = CASE WHEN $3 IN ('succeeded', 'failed', 'cancelled') THEN NOW() ELSE finished_at END
		WHERE id = $1 AND ($2 = 0 OR agent_id = $2) AND status = ANY($5)`,
		runID, agentID, to, truncateRunes(summary, 4000), from)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrAgentRunNotFound
	}
	return nil
}

// MarkAgentRunDelivery records the webhook outcome for a run.
func MarkAgentRunDelivery(runID int, errMsg string) {
	pool, err := OpenDatabase()
	if err != nil {
		return
	}
	defer CloseDatabase(pool)
	_, _ = pool.Exec(context.Background(),
		`UPDATE agent_runs SET delivered = ($2 = ''), delivery_error = $2 WHERE id = $1`,
		runID, truncateRunes(errMsg, 500))
}

func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}

// nonNilStrings and nonNilInts keep NOT NULL array columns from receiving NULL.
func nonNilStrings(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

func nonNilInts(v []int) []int {
	if v == nil {
		return []int{}
	}
	return v
}
