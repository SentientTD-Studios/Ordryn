package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	// TimeBlockStepMinutes is the grid time blocks snap to.
	TimeBlockStepMinutes = 15
	// MaxTimeBlockMinutes caps one block (a block may span many hours, up to a day).
	MaxTimeBlockMinutes = 24 * 60
	// MaxTimeBlocksListed caps the blocks returned for one calendar range.
	MaxTimeBlocksListed = 1000
)

// ErrTimeBlockNotFound means no block has that id.
var ErrTimeBlockNotFound = errors.New("time block not found")

// ErrTimeBlockLogged means the block's time was already logged.
var ErrTimeBlockLogged = errors.New("time block already logged")

// TimeBlock is a span of a user's calendar set aside for one task. Blocks are
// the plan; logging a block records a time entry for its duration.
type TimeBlock struct {
	ID          int
	UserID      int
	TaskID      int
	StartAt     time.Time
	EndAt       time.Time
	TimeEntryID int // 0 until the block is logged
	CreatedAt   time.Time

	TaskTitle     string
	TaskCompleted bool
	ProjectID     int
	ProjectName   string
}

// Minutes is the block's length.
func (b TimeBlock) Minutes() int {
	return int(b.EndAt.Sub(b.StartAt) / time.Minute)
}

// CreateTimeBlockTables adds when time was spent to time entries and creates
// the time block table. Run after CreateProjectWorkflowTables.
func CreateTimeBlockTables() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	stmts := []string{
		// spent_at is when the work happened; created_at stays when it was logged.
		`ALTER TABLE task_time_entries ADD COLUMN IF NOT EXISTS spent_at TIMESTAMPTZ`,
		`UPDATE task_time_entries SET spent_at = created_at WHERE spent_at IS NULL`,
		`ALTER TABLE task_time_entries ALTER COLUMN spent_at SET DEFAULT NOW()`,
		`ALTER TABLE task_time_entries ALTER COLUMN spent_at SET NOT NULL`,
		`CREATE INDEX IF NOT EXISTS idx_task_time_entries_spent ON task_time_entries (spent_at)`,
		`CREATE TABLE IF NOT EXISTS task_time_blocks (
			id SERIAL PRIMARY KEY,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			task_id INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
			start_at TIMESTAMPTZ NOT NULL,
			end_at TIMESTAMPTZ NOT NULL,
			time_entry_id INTEGER REFERENCES task_time_entries(id) ON DELETE SET NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			CHECK (end_at > start_at AND end_at - start_at <= INTERVAL '24 hours')
		)`,
		`CREATE INDEX IF NOT EXISTS idx_task_time_blocks_user_start ON task_time_blocks (user_id, start_at)`,
		`CREATE INDEX IF NOT EXISTS idx_task_time_blocks_task ON task_time_blocks (task_id)`,
	}
	for _, s := range stmts {
		if _, err := pool.Exec(context.Background(), s); err != nil {
			return fmt.Errorf("time block migration %q: %w", s, err)
		}
	}
	return nil
}

const timeBlockSelect = `
	SELECT b.id, b.user_id, b.task_id, b.start_at, b.end_at, COALESCE(b.time_entry_id, 0), b.created_at,
	       COALESCE(t.title, ''), COALESCE(t.completed, FALSE), COALESCE(t.project_id, 0), COALESCE(p.name, '')
	FROM task_time_blocks b
	JOIN tasks t ON t.id = b.task_id
	LEFT JOIN projects p ON p.id = t.project_id`

func scanTimeBlock(row pgx.Row, b *TimeBlock) error {
	return row.Scan(&b.ID, &b.UserID, &b.TaskID, &b.StartAt, &b.EndAt, &b.TimeEntryID, &b.CreatedAt,
		&b.TaskTitle, &b.TaskCompleted, &b.ProjectID, &b.ProjectName)
}

func collectTimeBlocks(rows pgx.Rows) ([]TimeBlock, error) {
	defer rows.Close()
	out := []TimeBlock{}
	for rows.Next() {
		var b TimeBlock
		if err := scanTimeBlock(rows, &b); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// ListUserTimeBlocks returns a user's blocks overlapping [from, to) on tasks
// that are not in the trash, earliest first. Callers still check task access.
func ListUserTimeBlocks(userID int, from, to time.Time) ([]TimeBlock, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	rows, err := pool.Query(context.Background(), timeBlockSelect+`
		WHERE b.user_id = $1 AND b.start_at < $3 AND b.end_at > $2 AND `+TaskNotTrashedSQL("t")+`
		ORDER BY b.start_at, b.id
		LIMIT $4`, userID, from, to, MaxTimeBlocksListed)
	if err != nil {
		return nil, err
	}
	return collectTimeBlocks(rows)
}

// ListUserTaskTimeBlocks returns a user's blocks on one task, earliest first.
func ListUserTaskTimeBlocks(userID, taskID int) ([]TimeBlock, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	rows, err := pool.Query(context.Background(), timeBlockSelect+`
		WHERE b.user_id = $1 AND b.task_id = $2
		ORDER BY b.start_at, b.id
		LIMIT $3`, userID, taskID, MaxTimeBlocksListed)
	if err != nil {
		return nil, err
	}
	return collectTimeBlocks(rows)
}

// GetTimeBlock loads one block.
func GetTimeBlock(blockID int) (*TimeBlock, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	var b TimeBlock
	if err := scanTimeBlock(pool.QueryRow(context.Background(), timeBlockSelect+` WHERE b.id = $1`, blockID), &b); err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			return nil, ErrTimeBlockNotFound
		}
		return nil, err
	}
	return &b, nil
}

// CreateTimeBlock inserts a block.
func CreateTimeBlock(userID, taskID int, start, end time.Time) (*TimeBlock, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	var id int
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO task_time_blocks (user_id, task_id, start_at, end_at) VALUES ($1, $2, $3, $4) RETURNING id`,
		userID, taskID, start, end).Scan(&id); err != nil {
		return nil, err
	}
	return GetTimeBlock(id)
}

// MoveTimeBlock changes an unlogged block's span, or returns ErrTimeBlockLogged.
func MoveTimeBlock(blockID, userID int, start, end time.Time) (*TimeBlock, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	ctx := context.Background()
	var logged bool
	err = pool.QueryRow(ctx,
		`UPDATE task_time_blocks SET start_at = CASE WHEN time_entry_id IS NULL THEN $3 ELSE start_at END,
		                             end_at = CASE WHEN time_entry_id IS NULL THEN $4 ELSE end_at END
		 WHERE id = $1 AND user_id = $2
		 RETURNING time_entry_id IS NOT NULL`, blockID, userID, start, end).Scan(&logged)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			return nil, ErrTimeBlockNotFound
		}
		return nil, err
	}
	if logged {
		return nil, ErrTimeBlockLogged
	}
	return GetTimeBlock(blockID)
}

// SetTimeBlockEntry links an unlogged block to its time entry. It returns
// ErrTimeBlockLogged when the block was logged in the meantime.
func SetTimeBlockEntry(blockID, entryID int) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	tag, err := pool.Exec(context.Background(),
		`UPDATE task_time_blocks SET time_entry_id = $2 WHERE id = $1 AND time_entry_id IS NULL`, blockID, entryID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrTimeBlockLogged
	}
	return nil
}

// DeleteTimeBlock removes a user's block. Logged time stays.
func DeleteTimeBlock(blockID, userID int) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	tag, err := pool.Exec(context.Background(),
		`DELETE FROM task_time_blocks WHERE id = $1 AND user_id = $2`, blockID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrTimeBlockNotFound
	}
	return nil
}
