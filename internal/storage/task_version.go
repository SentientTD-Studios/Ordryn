package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// tasks.version goes up by one whenever a task's content changes, so a client
// that edits from an out-of-date copy can be refused instead of silently
// overwriting someone else's change (see UpdateTask's ExpectedVersion).
//
// Database triggers keep it current, so every write path (API, automation,
// chat, GitHub and calendar sync, bulk actions) is covered without each one
// remembering to bump it.
//
// Columns that a task edit never overwrites are left out, so they don't cause
// false conflicts: manual ordering, timestamps, who holds or reviews the task,
// personal favourites, trash bookkeeping and recurrence links. A column added
// later is protected by default.
const taskVersionSQL = `
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS version INTEGER NOT NULL DEFAULT 1;

CREATE OR REPLACE FUNCTION gotodo_task_bump_version() RETURNS trigger AS $$
DECLARE
	ignored TEXT[] := ARRAY['version', 'position', 'date_modified', 'time_stamp',
		'claimed_by', 'reviewer_id', 'is_favorite',
		'deleted_at', 'deleted_by', 'delete_batch',
		'recurrence_series_id', 'recurrence_prev_id'];
BEGIN
	IF (to_jsonb(NEW) - ignored) IS DISTINCT FROM (to_jsonb(OLD) - ignored) THEN
		NEW.version := OLD.version + 1;
	END IF;
	RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS gotodo_task_bump_version ON tasks;
CREATE TRIGGER gotodo_task_bump_version BEFORE UPDATE ON tasks
	FOR EACH ROW EXECUTE FUNCTION gotodo_task_bump_version();

-- Tags, custom field values and the repeat rule live in their own tables but
-- are part of what a task edit writes, so changing them bumps the task too.
CREATE OR REPLACE FUNCTION gotodo_task_child_bump_version() RETURNS trigger AS $$
BEGIN
	IF TG_OP = 'DELETE' THEN
		UPDATE tasks SET version = version + 1 WHERE id = OLD.task_id;
		RETURN OLD;
	END IF;
	IF TG_OP = 'UPDATE' AND OLD.task_id IS DISTINCT FROM NEW.task_id THEN
		UPDATE tasks SET version = version + 1 WHERE id = OLD.task_id;
	END IF;
	UPDATE tasks SET version = version + 1 WHERE id = NEW.task_id;
	RETURN NEW;
END;
$$ LANGUAGE plpgsql;
`

var taskVersionChildTables = []string{"task_tags", "custom_field_values", "task_recurrence"}

// MigrateTasksAddVersion adds tasks.version and the triggers that maintain it.
// Needs tasks, task_tags, custom_field_values and task_recurrence.
func MigrateTasksAddVersion() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, taskVersionSQL); err != nil {
		return fmt.Errorf("task version: %v", err)
	}
	for _, table := range taskVersionChildTables {
		trigger := "gotodo_" + table + "_bump_task_version"
		stmt := fmt.Sprintf(`DROP TRIGGER IF EXISTS %[1]s ON %[2]s;
CREATE TRIGGER %[1]s AFTER INSERT OR UPDATE OR DELETE ON %[2]s
	FOR EACH ROW EXECUTE FUNCTION gotodo_task_child_bump_version();`, trigger, table)
		if _, err := pool.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("task version trigger on %s: %v", table, err)
		}
	}
	return nil
}

// ErrTaskNotFound is returned by GetTaskVersion for a missing task.
var ErrTaskNotFound = errors.New("task not found")

// GetTaskVersion returns a task's current version.
func GetTaskVersion(taskID int) (int, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return 0, err
	}
	defer CloseDatabase(pool)
	var v int
	err = pool.QueryRow(context.Background(), `SELECT version FROM tasks WHERE id = $1`, taskID).Scan(&v)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrTaskNotFound
	}
	return v, err
}
