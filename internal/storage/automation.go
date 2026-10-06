package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

var ErrAutomationRuleNotFound = errors.New("automation rule not found")

// RoleAutomation is the built-in role the site automation account holds on
// every project. It is never stored in project_members, and the leading
// underscore keeps it out of the custom role slug space.
const RoleAutomation = "_automation"

// SystemUserName is the preferred handle of the site automation account.
const SystemUserName = "automation"

// SystemUserDisplayName is how the automation account is shown in history.
const SystemUserDisplayName = "Automation"

const systemUserEmail = "automation@agents.invalid"

// automationPerms are the project permissions the automation role holds.
// Rules can change tasks like an editor, but never delete them or manage the project.
var automationPerms = map[string]bool{
	PermTasksEdit:     true,
	PermTasksArchive:  true,
	PermTasksRestore:  true,
	PermTasksComplete: true,
	PermTasksClaim:    true,
	PermTasksStatus:   true,
	PermTasksSprint:   true,
}

// Rule outcomes recorded in automation_rule_runs.
const (
	AutomationRunApplied = "applied"
	AutomationRunError   = "error"
)

// AutomationRule is one project rule: when (trigger) / if (conditions) / then (actions).
// Trigger config, conditions, and actions are JSON validated by the domain layer.
type AutomationRule struct {
	ID                int
	ProjectID         int
	Name              string
	Enabled           bool
	Position          int
	TriggerType       string
	TriggerConfig     json.RawMessage
	Conditions        json.RawMessage
	Actions           json.RawMessage
	RecipeID          string
	CreatedBy         int
	UpdatedBy         int
	CreatedAt         time.Time
	UpdatedAt         time.Time
	RunCount          int
	ErrorCount        int
	ConsecutiveErrors int
	LastRunAt         *time.Time
	LastError         string
	PausedReason      string
}

// AutomationRuleRun is one time a rule acted on a task.
type AutomationRuleRun struct {
	ID        int64
	RuleID    int
	ProjectID int
	TaskID    int
	Trigger   string
	Outcome   string
	Changes   json.RawMessage
	Error     string
	CreatedAt time.Time
	// Filled by list queries for display.
	RuleName  string
	TaskTitle string
}

// --- system account ---

var systemUser struct {
	sync.Mutex
	id       int
	checked  time.Time
	resolved bool
}

// SystemUserID returns the automation account's user id, or 0 before it exists.
func SystemUserID() int {
	systemUser.Lock()
	defer systemUser.Unlock()
	if systemUser.resolved {
		return systemUser.id
	}
	if time.Since(systemUser.checked) < 30*time.Second {
		return 0
	}
	systemUser.checked = time.Now()
	pool, err := OpenDatabase()
	if err != nil {
		return 0
	}
	defer CloseDatabase(pool)
	var id int
	if err := pool.QueryRow(context.Background(),
		`SELECT id FROM users WHERE is_system ORDER BY id LIMIT 1`).Scan(&id); err != nil {
		return 0
	}
	systemUser.id = id
	systemUser.resolved = true
	return id
}

// IsSystemUser reports whether userID is the site automation account.
func IsSystemUser(userID int) bool {
	if userID <= 0 {
		return false
	}
	return userID == SystemUserID()
}

// EnsureSystemUser creates the protected automation account when missing and
// returns its id. The account is flagged is_agent (no login, no inbox, hidden
// from user lists) and is_system (site-wide, owned by no project).
func EnsureSystemUser() (int, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return 0, err
	}
	defer CloseDatabase(pool)
	ctx := context.Background()

	var id int
	err = pool.QueryRow(ctx, `SELECT id FROM users WHERE is_system ORDER BY id LIMIT 1`).Scan(&id)
	if err == nil {
		setSystemUserID(id)
		return id, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, err
	}

	roleID, err := GetRoleIDByName("user")
	if err != nil {
		return 0, err
	}
	// A real member may already use the preferred handle; fall back to a suffixed one.
	handle := SystemUserName
	for i := 0; i < 20; i++ {
		taken, err := UsernameTaken(handle, 0)
		if err != nil {
			return 0, err
		}
		if !taken {
			break
		}
		handle = fmt.Sprintf("%s_%d", SystemUserName, i+2)
	}
	err = pool.QueryRow(ctx,
		`INSERT INTO users (email, password, role_id, user_name, is_agent, is_system, allow_project_invites)
		 VALUES ($1, $2, $3, $4, TRUE, TRUE, FALSE)
		 ON CONFLICT DO NOTHING
		 RETURNING id`,
		systemUserEmail, agentPasswordSentinel, roleID, handle).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		// A row with the reserved email already exists; claim it.
		err = pool.QueryRow(ctx,
			`UPDATE users SET is_agent = TRUE, is_system = TRUE, password = $2, allow_project_invites = FALSE
			 WHERE email = $1 RETURNING id`, systemUserEmail, agentPasswordSentinel).Scan(&id)
	}
	if err != nil {
		return 0, fmt.Errorf("create automation user: %w", err)
	}
	setSystemUserID(id)
	return id, nil
}

func setSystemUserID(id int) {
	systemUser.Lock()
	systemUser.id = id
	systemUser.resolved = id > 0
	systemUser.Unlock()
}

// automationRoleFor returns RoleAutomation when userID is the automation account
// and the project exists.
func automationRoleFor(projectID, userID int) (string, bool) {
	if !IsSystemUser(userID) {
		return "", false
	}
	pool, err := OpenDatabase()
	if err != nil {
		return "", true
	}
	defer CloseDatabase(pool)
	var ok bool
	_ = pool.QueryRow(context.Background(),
		`SELECT EXISTS (SELECT 1 FROM projects WHERE id = $1)`, projectID).Scan(&ok)
	if !ok {
		return "", true
	}
	return RoleAutomation, true
}

// --- schema ---

// CreateAutomationTables adds users.is_system and the rule tables.
func CreateAutomationTables() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	stmts := []string{
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS is_system BOOLEAN NOT NULL DEFAULT FALSE`,
		`CREATE TABLE IF NOT EXISTS automation_rules (
			id SERIAL PRIMARY KEY,
			project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
			name VARCHAR(80) NOT NULL,
			enabled BOOLEAN NOT NULL DEFAULT TRUE,
			position INTEGER NOT NULL DEFAULT 0,
			trigger_type VARCHAR(40) NOT NULL,
			trigger_config JSONB NOT NULL DEFAULT '{}',
			conditions JSONB NOT NULL DEFAULT '{}',
			actions JSONB NOT NULL DEFAULT '[]',
			recipe_id VARCHAR(40) NOT NULL DEFAULT '',
			created_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
			updated_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			run_count INTEGER NOT NULL DEFAULT 0,
			error_count INTEGER NOT NULL DEFAULT 0,
			consecutive_errors INTEGER NOT NULL DEFAULT 0,
			last_run_at TIMESTAMPTZ,
			last_error TEXT NOT NULL DEFAULT '',
			paused_reason TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_automation_rules_project ON automation_rules (project_id, position, id)`,
		`CREATE TABLE IF NOT EXISTS automation_rule_runs (
			id BIGSERIAL PRIMARY KEY,
			rule_id INTEGER NOT NULL REFERENCES automation_rules(id) ON DELETE CASCADE,
			project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
			task_id INTEGER REFERENCES tasks(id) ON DELETE SET NULL,
			trigger VARCHAR(40) NOT NULL DEFAULT '',
			outcome VARCHAR(16) NOT NULL,
			changes JSONB NOT NULL DEFAULT '[]',
			error TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_automation_rule_runs_project ON automation_rule_runs (project_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_automation_rule_runs_rule ON automation_rule_runs (rule_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_automation_rule_runs_task ON automation_rule_runs (task_id, created_at DESC)`,
		// Timed rules act once per task per episode (e.g. per due date).
		`CREATE TABLE IF NOT EXISTS automation_rule_marks (
			rule_id INTEGER NOT NULL REFERENCES automation_rules(id) ON DELETE CASCADE,
			task_id INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
			episode TEXT NOT NULL,
			marked_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (rule_id, task_id)
		)`,
	}
	for _, s := range stmts {
		if _, err := pool.Exec(context.Background(), s); err != nil {
			return fmt.Errorf("failed to create automation tables: %v", err)
		}
	}
	return nil
}

// --- rules ---

const automationRuleCols = `id, project_id, name, enabled, position, trigger_type, trigger_config, conditions,
	actions, recipe_id, COALESCE(created_by, 0), COALESCE(updated_by, 0), created_at, updated_at,
	run_count, error_count, consecutive_errors, last_run_at, last_error, paused_reason`

func scanAutomationRule(row interface{ Scan(dest ...any) error }, r *AutomationRule) error {
	var lastRun sql.NullTime
	var trig, cond, acts []byte
	if err := row.Scan(&r.ID, &r.ProjectID, &r.Name, &r.Enabled, &r.Position, &r.TriggerType, &trig, &cond,
		&acts, &r.RecipeID, &r.CreatedBy, &r.UpdatedBy, &r.CreatedAt, &r.UpdatedAt,
		&r.RunCount, &r.ErrorCount, &r.ConsecutiveErrors, &lastRun, &r.LastError, &r.PausedReason); err != nil {
		return err
	}
	r.TriggerConfig = json.RawMessage(trig)
	r.Conditions = json.RawMessage(cond)
	r.Actions = json.RawMessage(acts)
	if lastRun.Valid {
		t := lastRun.Time
		r.LastRunAt = &t
	}
	return nil
}

func queryAutomationRules(where string, args ...any) ([]AutomationRule, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	rows, err := pool.Query(context.Background(),
		`SELECT `+automationRuleCols+` FROM automation_rules WHERE `+where+` ORDER BY position, id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AutomationRule{}
	for rows.Next() {
		var r AutomationRule
		if err := scanAutomationRule(rows, &r); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListAutomationRules returns a project's rules in display order.
func ListAutomationRules(projectID int) ([]AutomationRule, error) {
	return queryAutomationRules(`project_id = $1`, projectID)
}

// ListEnabledAutomationRules returns a project's enabled rules for triggerTypes.
func ListEnabledAutomationRules(projectID int, triggerTypes []string) ([]AutomationRule, error) {
	return queryAutomationRules(`project_id = $1 AND enabled AND trigger_type = ANY($2)`, projectID, triggerTypes)
}

// ListEnabledTimedAutomationRules returns enabled rules with a timed trigger on
// projects that are not archived.
func ListEnabledTimedAutomationRules(triggerTypes []string) ([]AutomationRule, error) {
	return queryAutomationRules(`enabled AND trigger_type = ANY($1)
		AND project_id IN (SELECT id FROM projects WHERE NOT COALESCE(archived, FALSE))`, triggerTypes)
}

// GetAutomationRule returns one rule in a project.
func GetAutomationRule(projectID, ruleID int) (*AutomationRule, error) {
	list, err := queryAutomationRules(`project_id = $1 AND id = $2`, projectID, ruleID)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, ErrAutomationRuleNotFound
	}
	return &list[0], nil
}

// CountAutomationRules returns how many rules a project has.
func CountAutomationRules(projectID int) (int, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return 0, err
	}
	defer CloseDatabase(pool)
	var n int
	err = pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM automation_rules WHERE project_id = $1`, projectID).Scan(&n)
	return n, err
}

// CountEnabledAutomationRules returns how many enabled rules exist site-wide.
func CountEnabledAutomationRules() (int, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return 0, err
	}
	defer CloseDatabase(pool)
	var n int
	err = pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM automation_rules WHERE enabled`).Scan(&n)
	return n, err
}

// CreateAutomationRule inserts r at the end of the project's list.
func CreateAutomationRule(r AutomationRule) (*AutomationRule, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	var id int
	err = pool.QueryRow(context.Background(), `
		INSERT INTO automation_rules (project_id, name, enabled, position, trigger_type, trigger_config,
			conditions, actions, recipe_id, created_by, updated_by)
		VALUES ($1, $2, $3,
			(SELECT COALESCE(MAX(position), 0) + 1 FROM automation_rules WHERE project_id = $1),
			$4, $5, $6, $7, $8, NULLIF($9, 0), NULLIF($9, 0))
		RETURNING id`,
		r.ProjectID, r.Name, r.Enabled, r.TriggerType, []byte(r.TriggerConfig),
		[]byte(r.Conditions), []byte(r.Actions), r.RecipeID, r.CreatedBy).Scan(&id)
	if err != nil {
		return nil, err
	}
	return GetAutomationRule(r.ProjectID, id)
}

// UpdateAutomationRule saves a rule's editable fields. Re-enabling a rule
// clears its error streak and pause reason.
func UpdateAutomationRule(r AutomationRule, actorID int) (*AutomationRule, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	tag, err := pool.Exec(context.Background(), `
		UPDATE automation_rules SET
			name = $3, enabled = $4, trigger_type = $5, trigger_config = $6, conditions = $7, actions = $8,
			updated_by = NULLIF($9, 0), updated_at = NOW(),
			consecutive_errors = CASE WHEN $4 AND NOT enabled THEN 0 ELSE consecutive_errors END,
			paused_reason = CASE WHEN $4 THEN '' ELSE paused_reason END
		WHERE project_id = $1 AND id = $2`,
		r.ProjectID, r.ID, r.Name, r.Enabled, r.TriggerType, []byte(r.TriggerConfig),
		[]byte(r.Conditions), []byte(r.Actions), actorID)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrAutomationRuleNotFound
	}
	// Changing what a timed rule matches starts its episodes over.
	_, _ = pool.Exec(context.Background(), `DELETE FROM automation_rule_marks WHERE rule_id = $1`, r.ID)
	return GetAutomationRule(r.ProjectID, r.ID)
}

// DeleteAutomationRule removes a rule and its history.
func DeleteAutomationRule(projectID, ruleID int) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	tag, err := pool.Exec(context.Background(),
		`DELETE FROM automation_rules WHERE project_id = $1 AND id = $2`, projectID, ruleID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrAutomationRuleNotFound
	}
	return nil
}

// ReorderAutomationRules sets positions from ids order; unknown ids are ignored.
func ReorderAutomationRules(projectID int, ids []int) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	for i, id := range ids {
		if _, err := pool.Exec(context.Background(),
			`UPDATE automation_rules SET position = $3 WHERE project_id = $1 AND id = $2`,
			projectID, id, i+1); err != nil {
			return err
		}
	}
	return nil
}

// PauseAutomationRule disables a rule and records why.
func PauseAutomationRule(ruleID int, reason string) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	_, err = pool.Exec(context.Background(),
		`UPDATE automation_rules SET enabled = FALSE, paused_reason = $2, updated_at = NOW() WHERE id = $1`,
		ruleID, reason)
	return err
}

// --- runs ---

// RecordAutomationRun logs one rule run and updates the rule's counters.
// It returns the rule's consecutive error count after this run.
func RecordAutomationRun(run AutomationRuleRun) (int, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return 0, err
	}
	defer CloseDatabase(pool)
	ctx := context.Background()
	changes := []byte(run.Changes)
	if len(changes) == 0 {
		changes = []byte("[]")
	}
	var taskArg any
	if run.TaskID > 0 {
		taskArg = run.TaskID
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO automation_rule_runs (rule_id, project_id, task_id, trigger, outcome, changes, error)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		run.RuleID, run.ProjectID, taskArg, run.Trigger, run.Outcome, changes, run.Error); err != nil {
		return 0, err
	}
	var streak int
	if run.Outcome == AutomationRunError {
		err = pool.QueryRow(ctx, `
			UPDATE automation_rules SET run_count = run_count + 1, error_count = error_count + 1,
				consecutive_errors = consecutive_errors + 1, last_run_at = NOW(), last_error = $2
			WHERE id = $1 RETURNING consecutive_errors`, run.RuleID, run.Error).Scan(&streak)
	} else {
		_, err = pool.Exec(ctx, `
			UPDATE automation_rules SET run_count = run_count + 1, consecutive_errors = 0, last_run_at = NOW()
			WHERE id = $1`, run.RuleID)
	}
	return streak, err
}

// CountRecentAutomationRuns returns how many runs a rule logged since t.
func CountRecentAutomationRuns(ruleID int, since time.Time) (int, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return 0, err
	}
	defer CloseDatabase(pool)
	var n int
	err = pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM automation_rule_runs WHERE rule_id = $1 AND created_at >= $2`,
		ruleID, since).Scan(&n)
	return n, err
}

// ListAutomationRuns returns recent runs for a project, optionally one rule or task.
func ListAutomationRuns(projectID, ruleID, taskID, limit int) ([]AutomationRuleRun, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	rows, err := pool.Query(context.Background(), `
		SELECT r.id, r.rule_id, r.project_id, COALESCE(r.task_id, 0), r.trigger, r.outcome, r.changes,
		       r.error, r.created_at, COALESCE(ar.name, ''), COALESCE(t.title, '')
		FROM automation_rule_runs r
		JOIN automation_rules ar ON ar.id = r.rule_id
		LEFT JOIN tasks t ON t.id = r.task_id
		WHERE r.project_id = $1 AND ($2 = 0 OR r.rule_id = $2) AND ($3 = 0 OR r.task_id = $3)
		ORDER BY r.created_at DESC, r.id DESC
		LIMIT $4`, projectID, ruleID, taskID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AutomationRuleRun{}
	for rows.Next() {
		var run AutomationRuleRun
		var changes []byte
		if err := rows.Scan(&run.ID, &run.RuleID, &run.ProjectID, &run.TaskID, &run.Trigger, &run.Outcome,
			&changes, &run.Error, &run.CreatedAt, &run.RuleName, &run.TaskTitle); err != nil {
			return nil, err
		}
		run.Changes = json.RawMessage(changes)
		out = append(out, run)
	}
	return out, rows.Err()
}

// PurgeAutomationRuns deletes run history older than days (0 keeps everything).
func PurgeAutomationRuns(days int) error {
	if days <= 0 {
		return nil
	}
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	_, err = pool.Exec(context.Background(),
		`DELETE FROM automation_rule_runs WHERE created_at < NOW() - ($1 * INTERVAL '1 day')`, days)
	return err
}

// --- timed triggers ---

// AutomationCandidate is a task a timed rule may act on in its current episode.
type AutomationCandidate struct {
	TaskID  int
	Episode string
}

// AutomationTimedQuery selects tasks for a timed trigger. Days is the rule's
// threshold. Results skip archived tasks and tasks already marked for the
// same episode.
type AutomationTimedQuery struct {
	ProjectID   int
	RuleID      int
	TriggerType string
	Days        int
	Limit       int
}

// Timed trigger types handled by ListAutomationCandidates.
const (
	AutomationTimedOverdue     = "time.overdue"
	AutomationTimedDueSoon     = "time.due_soon"
	AutomationTimedCompleted   = "time.completed_ago"
	AutomationTimedInStatus    = "time.in_status"
	AutomationTimedInactive    = "time.inactive"
	AutomationTimedSprintEnded = "time.sprint_ended"
)

// automationLastActivitySQL is when a task last had an event or comment.
const automationLastActivitySQL = `GREATEST(
	COALESCE((SELECT MAX(e.created_at) FROM task_events e WHERE e.task_id = t.id), t.time_stamp),
	COALESCE((SELECT MAX(c.created_at)::timestamp FROM task_comments c WHERE c.task_id = t.id), t.time_stamp),
	t.time_stamp)`

// ListAutomationCandidates returns tasks matching a timed trigger.
func ListAutomationCandidates(q AutomationTimedQuery) ([]AutomationCandidate, error) {
	if q.Limit <= 0 {
		q.Limit = 500
	}
	if q.Days < 0 {
		q.Days = 0
	}
	var episode, where string
	switch q.TriggerType {
	case AutomationTimedOverdue:
		episode = `CAST(t.due_date AS TEXT)`
		where = `NOT COALESCE(t.completed, FALSE) AND t.due_date IS NOT NULL
			AND t.due_date <= CURRENT_DATE - ($3 * INTERVAL '1 day') AND t.due_date < CURRENT_DATE`
	case AutomationTimedDueSoon:
		episode = `CAST(t.due_date AS TEXT)`
		where = `NOT COALESCE(t.completed, FALSE) AND t.due_date IS NOT NULL
			AND t.due_date >= CURRENT_DATE AND t.due_date <= CURRENT_DATE + ($3 * INTERVAL '1 day')`
	case AutomationTimedCompleted:
		episode = `CAST(done.at AS TEXT)`
		where = `COALESCE(t.completed, FALSE) AND done.at IS NOT NULL
			AND done.at <= NOW() - ($3 * INTERVAL '1 day')`
	case AutomationTimedInStatus:
		episode = `COALESCE(t.status_id, 0) || '@' || CAST(entered.at AS TEXT)`
		where = `t.status_id IS NOT NULL AND entered.at <= NOW() - ($3 * INTERVAL '1 day')`
	case AutomationTimedInactive:
		episode = `CAST(act.at AS TEXT)`
		where = `NOT COALESCE(t.completed, FALSE) AND act.at <= NOW() - ($3 * INTERVAL '1 day')`
	case AutomationTimedSprintEnded:
		episode = `CAST(t.sprint_id AS TEXT)`
		where = `NOT COALESCE(t.completed, FALSE) AND sp.end_date IS NOT NULL
			AND sp.end_date < CURRENT_DATE - ($3 * INTERVAL '1 day')`
	default:
		return nil, fmt.Errorf("unknown timed trigger %q", q.TriggerType)
	}

	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	query := `
		SELECT t.id, ` + episode + ` AS episode
		FROM tasks t
		LEFT JOIN project_sprints sp ON sp.id = t.sprint_id
		LEFT JOIN LATERAL (
			SELECT MAX(e.created_at) AS at FROM task_events e
			WHERE e.task_id = t.id AND e.event_type = 'completed') done ON TRUE
		LEFT JOIN LATERAL (
			SELECT COALESCE(MAX(e.created_at), t.time_stamp) AS at FROM task_events e
			WHERE e.task_id = t.id AND e.event_type = 'status_changed') entered ON TRUE
		LEFT JOIN LATERAL (SELECT ` + automationLastActivitySQL + ` AS at) act ON TRUE
		WHERE t.project_id = $1
		  AND ` + where + `
		  AND NOT EXISTS (
			SELECT 1 FROM task_tags tt JOIN tags tg ON tg.id = tt.tag_id
			WHERE tt.task_id = t.id AND tg.protected AND LOWER(tg.name) = '` + ArchivedTagName + `')
		  AND NOT EXISTS (
			SELECT 1 FROM automation_rule_marks m
			WHERE m.rule_id = $2 AND m.task_id = t.id AND m.episode = ` + episode + `)
		ORDER BY t.id
		LIMIT $4`
	rows, err := pool.Query(context.Background(), query, q.ProjectID, q.RuleID, q.Days, q.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AutomationCandidate{}
	for rows.Next() {
		var c AutomationCandidate
		var ep sql.NullString
		if err := rows.Scan(&c.TaskID, &ep); err != nil {
			return nil, err
		}
		c.Episode = ep.String
		out = append(out, c)
	}
	return out, rows.Err()
}

// MarkAutomationEpisode records that a timed rule handled a task's episode.
func MarkAutomationEpisode(ruleID, taskID int, episode string) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	_, err = pool.Exec(context.Background(), `
		INSERT INTO automation_rule_marks (rule_id, task_id, episode) VALUES ($1, $2, $3)
		ON CONFLICT (rule_id, task_id) DO UPDATE SET episode = EXCLUDED.episode, marked_at = NOW()`,
		ruleID, taskID, episode)
	return err
}

// TaskIsArchived reports whether a task carries the protected archived tag.
func TaskIsArchived(taskID int) bool {
	pool, err := OpenDatabase()
	if err != nil {
		return false
	}
	defer CloseDatabase(pool)
	var archived bool
	_ = pool.QueryRow(context.Background(), `
		SELECT EXISTS (SELECT 1 FROM task_tags tt JOIN tags tg ON tg.id = tt.tag_id
			WHERE tt.task_id = $1 AND tg.protected AND LOWER(tg.name) = $2)`,
		taskID, strings.ToLower(ArchivedTagName)).Scan(&archived)
	return archived
}

// ProjectIsArchived reports whether a project is archived (missing projects count as archived).
func ProjectIsArchived(projectID int) bool {
	pool, err := OpenDatabase()
	if err != nil {
		return true
	}
	defer CloseDatabase(pool)
	archived := true
	_ = pool.QueryRow(context.Background(),
		`SELECT COALESCE(archived, FALSE) FROM projects WHERE id = $1`, projectID).Scan(&archived)
	return archived
}

// ListProjectTaskIDs returns up to limit task ids in a project, newest first.
func ListProjectTaskIDs(projectID, limit int) ([]int, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	rows, err := pool.Query(context.Background(),
		`SELECT id FROM tasks WHERE project_id = $1 ORDER BY id DESC LIMIT $2`, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []int{}
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
