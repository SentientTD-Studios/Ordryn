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
	CommentDeletedByUser  = "user"
	CommentDeletedByOwner = "owner"

	CommentRevisionKindEdit    = "edit"
	CommentRevisionKindDelete  = "delete"
	CommentRevisionKindRestore = "restore"

	commentAuditDefaultLimit = 50
	commentAuditMaxLimit     = 100
)

// TaskComment is a discussion post on a project task.
type TaskComment struct {
	ID               int
	TaskID           int
	UserID           int
	UserName         string
	AvatarURL        string
	Body             string
	CreatedAt        time.Time
	DeletedAt        *time.Time
	DeletedByUserID  int
	DeletedByKind    string
	EditedAt         *time.Time
	EditedByUserID   int
	EditedByUserName string
	AuthorRole       string
	AuthorRoleName   string
	// AuthorIsAgent is true when an AI agent wrote the comment.
	AuthorIsAgent bool
	Links         []TaskCommentLink
}

// TaskCommentLink is a task mentioned in a comment that the viewer can open.
type TaskCommentLink struct {
	ID    int
	Title string
}

// TaskCommentRevision is a snapshot of comment content before an edit, delete, or restore.
type TaskCommentRevision struct {
	ID               int
	CommentID        int
	TaskID           int
	Body             string
	Kind             string
	CreatedAt        time.Time
	EditedByUserID   int
	EditedByUserName string
	AuthorUserID     int
	AuthorUserName   string
	TaskTitle        string
	ProjectID        int
	ProjectName      string
	CommentDeleted   bool
	CurrentBody      string
}

// CommentAuditFilter selects rows for the admin comment history API.
type CommentAuditFilter struct {
	Query  string
	Kind   string
	Limit  int
	Offset int
}

// CreateTaskCommentsTable creates the task discussion store and revision audit log.
func CreateTaskCommentsTable() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	stmts := []string{
		`CREATE TABLE IF NOT EXISTS task_comments (
			id SERIAL PRIMARY KEY,
			task_id INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
			user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
			body TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			deleted_at TIMESTAMPTZ,
			deleted_by_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
			deleted_by_kind TEXT CHECK (deleted_by_kind IS NULL OR deleted_by_kind IN ('user', 'owner')),
			edited_at TIMESTAMPTZ,
			edited_by_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL
		)`,
		`ALTER TABLE task_comments ADD COLUMN IF NOT EXISTS edited_at TIMESTAMPTZ`,
		`ALTER TABLE task_comments ADD COLUMN IF NOT EXISTS edited_by_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL`,
		`CREATE INDEX IF NOT EXISTS idx_task_comments_task_created
			ON task_comments (task_id, created_at ASC, id ASC)`,
		`CREATE TABLE IF NOT EXISTS task_comment_revisions (
			id SERIAL PRIMARY KEY,
			comment_id INTEGER NOT NULL REFERENCES task_comments(id) ON DELETE CASCADE,
			task_id INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
			body TEXT NOT NULL DEFAULT '',
			kind TEXT NOT NULL CHECK (kind IN ('edit', 'delete', 'restore')),
			edited_by_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_task_comment_revisions_comment_created
			ON task_comment_revisions (comment_id, created_at DESC, id DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_task_comment_revisions_created
			ON task_comment_revisions (created_at DESC, id DESC)`,
	}
	for _, s := range stmts {
		if _, err := pool.Exec(context.Background(), s); err != nil {
			return fmt.Errorf("failed to create task_comments: %v", err)
		}
	}
	return nil
}

func scanTaskComment(row interface {
	Scan(dest ...any) error
}, c *TaskComment) error {
	var deletedAt sql.NullTime
	var deletedBy sql.NullInt64
	var deletedKind sql.NullString
	var editedAt sql.NullTime
	err := row.Scan(
		&c.ID, &c.TaskID, &c.UserID, &c.Body, &c.CreatedAt,
		&deletedAt, &deletedBy, &deletedKind,
		&editedAt, &c.EditedByUserID, &c.UserName, &c.EditedByUserName,
		&c.AvatarURL, &c.AuthorIsAgent,
	)
	if err != nil {
		return err
	}
	if deletedAt.Valid {
		t := deletedAt.Time
		c.DeletedAt = &t
	}
	if deletedBy.Valid {
		c.DeletedByUserID = int(deletedBy.Int64)
	}
	if deletedKind.Valid {
		c.DeletedByKind = deletedKind.String
	}
	if editedAt.Valid {
		t := editedAt.Time
		c.EditedAt = &t
	}
	return nil
}

const taskCommentSelect = `SELECT c.id, c.task_id, COALESCE(c.user_id, 0), COALESCE(c.body, ''), c.created_at,
		c.deleted_at, c.deleted_by_user_id, c.deleted_by_kind,
		c.edited_at, COALESCE(c.edited_by_user_id, 0),
		COALESCE(u.user_name, u.email, ''),
		COALESCE(eu.user_name, eu.email, ''),
		COALESCE(u.avatar_url, ''),
		COALESCE(u.is_agent, FALSE)
	 FROM task_comments c
	 LEFT JOIN users u ON u.id = c.user_id
	 LEFT JOIN users eu ON eu.id = c.edited_by_user_id`

const taskCommentRevisionSelect = `SELECT r.id, r.comment_id, r.task_id, COALESCE(r.body, ''), r.kind, r.created_at,
		COALESCE(r.edited_by_user_id, 0), COALESCE(eu.user_name, eu.email, ''),
		COALESCE(c.user_id, 0), COALESCE(au.user_name, au.email, ''),
		COALESCE(t.title, ''), COALESCE(t.project_id, 0), COALESCE(p.name, ''),
		(c.deleted_at IS NOT NULL), COALESCE(c.body, '')
	 FROM task_comment_revisions r
	 JOIN task_comments c ON c.id = r.comment_id
	 JOIN tasks t ON t.id = r.task_id
	 LEFT JOIN projects p ON p.id = t.project_id
	 LEFT JOIN users eu ON eu.id = r.edited_by_user_id
	 LEFT JOIN users au ON au.id = c.user_id`

func scanTaskCommentRevision(row interface {
	Scan(dest ...any) error
}, rev *TaskCommentRevision) error {
	return row.Scan(
		&rev.ID, &rev.CommentID, &rev.TaskID, &rev.Body, &rev.Kind, &rev.CreatedAt,
		&rev.EditedByUserID, &rev.EditedByUserName,
		&rev.AuthorUserID, &rev.AuthorUserName,
		&rev.TaskTitle, &rev.ProjectID, &rev.ProjectName,
		&rev.CommentDeleted, &rev.CurrentBody,
	)
}

func validCommentRevisionKind(kind string) bool {
	return kind == CommentRevisionKindEdit || kind == CommentRevisionKindDelete || kind == CommentRevisionKindRestore
}

// ListTaskComments returns discussion posts for a task, oldest first.
func ListTaskComments(taskID int) ([]TaskComment, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(),
		taskCommentSelect+` WHERE c.task_id = $1 ORDER BY c.created_at ASC, c.id ASC`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []TaskComment
	for rows.Next() {
		var c TaskComment
		if err := scanTaskComment(rows, &c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetTaskComment loads one comment by id.
func GetTaskComment(commentID int) (*TaskComment, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	var c TaskComment
	err = scanTaskComment(pool.QueryRow(context.Background(),
		taskCommentSelect+` WHERE c.id = $1`, commentID), &c)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("comment not found")
		}
		return nil, err
	}
	return &c, nil
}

// CreateTaskComment inserts a discussion post and returns it with author name.
func CreateTaskComment(taskID, userID int, body string) (*TaskComment, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	body = strings.TrimSpace(body)
	var id int
	err = pool.QueryRow(context.Background(),
		`INSERT INTO task_comments (task_id, user_id, body)
		 VALUES ($1, $2, $3) RETURNING id`,
		taskID, userID, body).Scan(&id)
	if err != nil {
		return nil, err
	}
	return GetTaskComment(id)
}

// SoftDeleteTaskComment snapshots the current body, then clears it and records who deleted it.
func SoftDeleteTaskComment(commentID, deletedByUserID int, kind string) error {
	if kind != CommentDeletedByUser && kind != CommentDeletedByOwner {
		return fmt.Errorf("invalid delete kind")
	}
	_, err := snapshotAndUpdateComment(commentID, deletedByUserID, "", CommentRevisionKindDelete, kind)
	return err
}

// UpdateTaskComment snapshots the current body and writes a new one.
func UpdateTaskComment(commentID, editorUserID int, body string) (*TaskComment, error) {
	return snapshotAndUpdateComment(commentID, editorUserID, body, CommentRevisionKindEdit, "")
}

// RestoreTaskCommentFromRevision snapshots the current body and restores a previous version.
func RestoreTaskCommentFromRevision(revisionID, editorUserID int) (*TaskComment, error) {
	rev, err := GetCommentRevision(revisionID)
	if err != nil {
		return nil, err
	}
	return snapshotAndUpdateComment(rev.CommentID, editorUserID, rev.Body, CommentRevisionKindRestore, "")
}

func snapshotAndUpdateComment(commentID, actorUserID int, newBody, revisionKind, deleteKind string) (*TaskComment, error) {
	if !validCommentRevisionKind(revisionKind) {
		return nil, fmt.Errorf("invalid revision kind")
	}
	newBody = strings.TrimSpace(newBody)
	softDelete := revisionKind == CommentRevisionKindDelete

	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var taskID int
	var currentBody string
	var deletedAt sql.NullTime
	err = tx.QueryRow(ctx,
		`SELECT task_id, COALESCE(body, ''), deleted_at FROM task_comments WHERE id = $1 FOR UPDATE`,
		commentID).Scan(&taskID, &currentBody, &deletedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("comment not found")
		}
		return nil, err
	}
	if softDelete && deletedAt.Valid {
		return nil, fmt.Errorf("comment not found")
	}

	undelete := deletedAt.Valid && !softDelete
	if !undelete && currentBody == newBody {
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return GetTaskComment(commentID)
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO task_comment_revisions (comment_id, task_id, body, kind, edited_by_user_id)
		 VALUES ($1, $2, $3, $4, $5)`,
		commentID, taskID, currentBody, revisionKind, actorUserID); err != nil {
		return nil, err
	}

	if softDelete {
		tag, err := tx.Exec(ctx,
			`UPDATE task_comments
			 SET body = '', deleted_at = NOW(), deleted_by_user_id = $1, deleted_by_kind = $2,
			     edited_at = NOW(), edited_by_user_id = $1
			 WHERE id = $3 AND deleted_at IS NULL`,
			actorUserID, deleteKind, commentID)
		if err != nil {
			return nil, err
		}
		if tag.RowsAffected() == 0 {
			return nil, fmt.Errorf("comment not found")
		}
	} else {
		_, err = tx.Exec(ctx,
			`UPDATE task_comments
			 SET body = $1, edited_at = NOW(), edited_by_user_id = $2,
			     deleted_at = NULL, deleted_by_user_id = NULL, deleted_by_kind = NULL
			 WHERE id = $3`,
			newBody, actorUserID, commentID)
		if err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return GetTaskComment(commentID)
}

// GetCommentRevision loads one revision with comment and task context.
func GetCommentRevision(revisionID int) (*TaskCommentRevision, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	var rev TaskCommentRevision
	err = scanTaskCommentRevision(pool.QueryRow(context.Background(),
		taskCommentRevisionSelect+` WHERE r.id = $1`, revisionID), &rev)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("revision not found")
		}
		return nil, err
	}
	return &rev, nil
}

// ListCommentRevisions returns prior versions of a comment, newest first.
func ListCommentRevisions(commentID int) ([]TaskCommentRevision, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(),
		taskCommentRevisionSelect+` WHERE r.comment_id = $1 ORDER BY r.created_at DESC, r.id DESC`, commentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []TaskCommentRevision
	for rows.Next() {
		var rev TaskCommentRevision
		if err := scanTaskCommentRevision(rows, &rev); err != nil {
			return nil, err
		}
		out = append(out, rev)
	}
	if out == nil {
		out = []TaskCommentRevision{}
	}
	return out, rows.Err()
}

// ListCommentAudit returns newest comment revisions for site admins.
func ListCommentAudit(f CommentAuditFilter) ([]TaskCommentRevision, int, error) {
	if f.Limit <= 0 {
		f.Limit = commentAuditDefaultLimit
	}
	if f.Limit > commentAuditMaxLimit {
		f.Limit = commentAuditMaxLimit
	}
	if f.Offset < 0 {
		f.Offset = 0
	}

	where := []string{"TRUE"}
	args := make([]any, 0, 4)
	n := 1
	if k := strings.TrimSpace(f.Kind); k != "" {
		if !validCommentRevisionKind(k) {
			return nil, 0, fmt.Errorf("invalid revision kind")
		}
		where = append(where, fmt.Sprintf("r.kind = $%d", n))
		args = append(args, k)
		n++
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		where = append(where, fmt.Sprintf(`(
			r.body ILIKE $%d OR COALESCE(t.title, '') ILIKE $%d OR COALESCE(p.name, '') ILIKE $%d
			OR COALESCE(au.user_name, au.email, '') ILIKE $%d
			OR COALESCE(eu.user_name, eu.email, '') ILIKE $%d
		)`, n, n, n, n, n))
		args = append(args, "%"+q+"%")
		n++
	}
	whereSQL := strings.Join(where, " AND ")

	pool, err := OpenDatabase()
	if err != nil {
		return nil, 0, err
	}
	defer CloseDatabase(pool)

	var total int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM task_comment_revisions r
		 JOIN task_comments c ON c.id = r.comment_id
		 JOIN tasks t ON t.id = r.task_id
		 LEFT JOIN projects p ON p.id = t.project_id
		 LEFT JOIN users eu ON eu.id = r.edited_by_user_id
		 LEFT JOIN users au ON au.id = c.user_id
		 WHERE `+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count comment audit: %w", err)
	}

	args = append(args, f.Limit, f.Offset)
	rows, err := pool.Query(context.Background(),
		taskCommentRevisionSelect+` WHERE `+whereSQL+`
		 ORDER BY r.created_at DESC, r.id DESC
		 LIMIT $`+fmt.Sprintf("%d", n)+` OFFSET $`+fmt.Sprintf("%d", n+1), args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list comment audit: %w", err)
	}
	defer rows.Close()

	out := make([]TaskCommentRevision, 0)
	for rows.Next() {
		var rev TaskCommentRevision
		if err := scanTaskCommentRevision(rows, &rev); err != nil {
			return nil, 0, err
		}
		out = append(out, rev)
	}
	return out, total, rows.Err()
}
