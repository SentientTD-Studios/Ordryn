package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	// DefaultAttachmentMaxBytes is the per-file limit until an admin changes it.
	DefaultAttachmentMaxBytes int64 = 5 << 20
	// MaxAttachmentMaxBytes is the most an admin may allow per file. Files live
	// in the database, so this stays well below what a blob store could take.
	MaxAttachmentMaxBytes int64 = 50 << 20
	minAttachmentMaxBytes int64 = 16 << 10
	// MaxAttachmentsPerTask caps how many files one task can hold.
	MaxAttachmentsPerTask = 20
)

// ErrAttachmentNotFound is returned when an attachment id does not exist on the task.
var ErrAttachmentNotFound = errors.New("attachment not found")

// TaskAttachment is a file uploaded to a task. Data is only loaded by GetTaskAttachmentData.
type TaskAttachment struct {
	ID          int
	TaskID      int
	UserID      int
	UserName    string
	FileName    string
	ContentType string
	SizeBytes   int64
	CreatedAt   time.Time
}

// ClampAttachmentMaxBytes keeps the per-file limit between 16 KiB and 50 MiB;
// zero or negative means the default.
func ClampAttachmentMaxBytes(n int64) int64 {
	switch {
	case n <= 0:
		return DefaultAttachmentMaxBytes
	case n < minAttachmentMaxBytes:
		return minAttachmentMaxBytes
	case n > MaxAttachmentMaxBytes:
		return MaxAttachmentMaxBytes
	}
	return n
}

// CreateTaskAttachmentsTable creates task_attachments and the site settings
// that control uploads (off by default).
func CreateTaskAttachmentsTable() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS task_attachments (
			id SERIAL PRIMARY KEY,
			task_id INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
			user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
			file_name TEXT NOT NULL,
			content_type TEXT NOT NULL,
			size_bytes BIGINT NOT NULL,
			data BYTEA NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_task_attachments_task ON task_attachments (task_id)`,
		`ALTER TABLE site_settings ADD COLUMN IF NOT EXISTS attachments_enabled BOOLEAN DEFAULT FALSE`,
		fmt.Sprintf(`ALTER TABLE site_settings ADD COLUMN IF NOT EXISTS attachment_max_bytes BIGINT DEFAULT %d`, DefaultAttachmentMaxBytes),
	}
	for _, s := range stmts {
		if _, err := pool.Exec(context.Background(), s); err != nil {
			return fmt.Errorf("create task attachments: %w", err)
		}
	}
	return nil
}

const attachmentSelect = `
	SELECT a.id, a.task_id, COALESCE(a.user_id, 0), COALESCE(NULLIF(u.user_name, ''), u.email, ''),
		a.file_name, a.content_type, a.size_bytes, a.created_at
	FROM task_attachments a
	LEFT JOIN users u ON u.id = a.user_id`

func scanAttachment(row pgx.Row) (TaskAttachment, error) {
	var a TaskAttachment
	err := row.Scan(&a.ID, &a.TaskID, &a.UserID, &a.UserName, &a.FileName, &a.ContentType, &a.SizeBytes, &a.CreatedAt)
	return a, err
}

// ListTaskAttachments returns a task's attachments, oldest first, without file data.
func ListTaskAttachments(taskID int) ([]TaskAttachment, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	rows, err := pool.Query(context.Background(), attachmentSelect+` WHERE a.task_id = $1 ORDER BY a.created_at, a.id`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TaskAttachment{}
	for rows.Next() {
		a, err := scanAttachment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// GetTaskAttachment loads one attachment's metadata, scoped to its task.
func GetTaskAttachment(taskID, id int) (*TaskAttachment, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	a, err := scanAttachment(pool.QueryRow(context.Background(), attachmentSelect+` WHERE a.task_id = $1 AND a.id = $2`, taskID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrAttachmentNotFound
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// GetTaskAttachmentData returns the stored bytes of one attachment.
func GetTaskAttachmentData(taskID, id int) ([]byte, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	var data []byte
	err = pool.QueryRow(context.Background(),
		`SELECT data FROM task_attachments WHERE task_id = $1 AND id = $2`, taskID, id).Scan(&data)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrAttachmentNotFound
	}
	return data, err
}

// AddTaskAttachment stores a file unless the task already has the maximum number.
func AddTaskAttachment(taskID, userID int, fileName, contentType string, data []byte) (*TaskAttachment, error) {
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
	defer tx.Rollback(ctx)
	// Serialize uploads to one task so the count check holds.
	if _, err := tx.Exec(ctx, `SELECT 1 FROM tasks WHERE id = $1 FOR UPDATE`, taskID); err != nil {
		return nil, err
	}
	var n int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM task_attachments WHERE task_id = $1`, taskID).Scan(&n); err != nil {
		return nil, err
	}
	if n >= MaxAttachmentsPerTask {
		return nil, ErrAttachmentLimit
	}
	var id int
	if err := tx.QueryRow(ctx, `
		INSERT INTO task_attachments (task_id, user_id, file_name, content_type, size_bytes, data)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		taskID, userID, fileName, contentType, int64(len(data)), data).Scan(&id); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return GetTaskAttachment(taskID, id)
}

// ErrAttachmentLimit is returned when a task already holds MaxAttachmentsPerTask files.
var ErrAttachmentLimit = fmt.Errorf("a task can have at most %d attachments", MaxAttachmentsPerTask)

// DeleteTaskAttachment removes one attachment from a task.
func DeleteTaskAttachment(taskID, id int) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	tag, err := pool.Exec(context.Background(), `DELETE FROM task_attachments WHERE task_id = $1 AND id = $2`, taskID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrAttachmentNotFound
	}
	return nil
}

// GetAttachmentSettings reads just the upload switch and per-file limit.
func GetAttachmentSettings() (enabled bool, maxBytes int64, err error) {
	pool, err := OpenDatabase()
	if err != nil {
		return false, DefaultAttachmentMaxBytes, err
	}
	defer CloseDatabase(pool)
	err = pool.QueryRow(context.Background(), `
		SELECT COALESCE(attachments_enabled, FALSE), COALESCE(attachment_max_bytes, 0)
		FROM site_settings WHERE id = 1`).Scan(&enabled, &maxBytes)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, DefaultAttachmentMaxBytes, nil
	}
	return enabled, ClampAttachmentMaxBytes(maxBytes), err
}
