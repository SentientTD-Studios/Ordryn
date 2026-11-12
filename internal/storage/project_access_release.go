package storage

import (
	"context"
	"fmt"
)

// Losing access to a project releases everything that tied the person to its
// tasks: their claims, review requests, user-type custom field values, and task
// and project watches. Every way of losing access deletes a project_members row
// (removing a member, leaving, a company share or team change, attaching an
// organization, removing a person from an organization, deleting an account),
// so a trigger on that table catches all of them, including future ones.
//
// The trigger is deferred to commit and checks access again then, so code that
// deletes and re-inserts members in one transaction (attaching an organization
// re-imports its people) doesn't drop the people who stay. The project's owner
// never loses access this way.

const releaseLostProjectAccessFunc = `
CREATE OR REPLACE FUNCTION gotodo_release_lost_project_access() RETURNS trigger AS $$
BEGIN
	IF EXISTS (SELECT 1 FROM project_members WHERE project_id = OLD.project_id AND user_id = OLD.user_id)
	   OR EXISTS (SELECT 1 FROM projects WHERE id = OLD.project_id AND user_id = OLD.user_id) THEN
		RETURN NULL;
	END IF;
	PERFORM gotodo_release_project_assignments(OLD.project_id, OLD.user_id);
	RETURN NULL;
END;
$$ LANGUAGE plpgsql`

const releaseProjectAssignmentsFunc = `
CREATE OR REPLACE FUNCTION gotodo_release_project_assignments(p_project INTEGER, p_user INTEGER) RETURNS void AS $$
BEGIN
	UPDATE tasks SET claimed_by = NULL
	 WHERE project_id = p_project AND claimed_by = p_user;
	UPDATE tasks SET reviewer_id = NULL
	 WHERE project_id = p_project AND reviewer_id = p_user;
	DELETE FROM custom_field_values v
	 USING tasks t, custom_field_defs d
	 WHERE v.task_id = t.id AND t.project_id = p_project
	   AND d.field_key = v.field_key AND d.type = 'user'
	   AND (v.value #>> '{}') = p_user::text;
	DELETE FROM task_watchers w
	 USING tasks t
	 WHERE w.task_id = t.id AND t.project_id = p_project AND w.user_id = p_user;
	DELETE FROM project_watchers WHERE project_id = p_project AND user_id = p_user;
END;
$$ LANGUAGE plpgsql`

// staleAssignmentsSQL lists (project, person) pairs where someone is still tied
// to a project's tasks without being able to open it: left behind before the
// trigger existed.
const staleAssignmentsSQL = `
	SELECT DISTINCT x.project_id, x.user_id FROM (
		SELECT project_id, claimed_by AS user_id FROM tasks WHERE project_id IS NOT NULL AND claimed_by IS NOT NULL
		UNION SELECT project_id, reviewer_id FROM tasks WHERE project_id IS NOT NULL AND reviewer_id IS NOT NULL
		UNION SELECT t.project_id, w.user_id FROM task_watchers w JOIN tasks t ON t.id = w.task_id WHERE t.project_id IS NOT NULL
		UNION SELECT project_id, user_id FROM project_watchers
		UNION SELECT t.project_id, (v.value #>> '{}')::int
		  FROM custom_field_values v
		  JOIN custom_field_defs d ON d.field_key = v.field_key AND d.type = 'user'
		  JOIN tasks t ON t.id = v.task_id
		 WHERE t.project_id IS NOT NULL AND (v.value #>> '{}') ~ '^[0-9]{1,9}$'
	) x
	JOIN projects p ON p.id = x.project_id
	WHERE p.user_id IS DISTINCT FROM x.user_id
	  AND NOT EXISTS (SELECT 1 FROM project_members pm WHERE pm.project_id = x.project_id AND pm.user_id = x.user_id)`

// MigrateReleaseLostProjectAccess installs the trigger and releases anything
// already left behind. Safe to run on every start.
func MigrateReleaseLostProjectAccess() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	ctx := context.Background()

	for _, stmt := range []string{releaseProjectAssignmentsFunc, releaseLostProjectAccessFunc} {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("release function: %w", err)
		}
	}
	var exists bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM pg_trigger
		 WHERE tgname = 'gotodo_release_lost_project_access' AND tgrelid = 'project_members'::regclass)`).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		if _, err := pool.Exec(ctx, `
			CREATE CONSTRAINT TRIGGER gotodo_release_lost_project_access
			AFTER DELETE ON project_members
			DEFERRABLE INITIALLY DEFERRED
			FOR EACH ROW EXECUTE FUNCTION gotodo_release_lost_project_access()`); err != nil {
			return fmt.Errorf("release trigger: %w", err)
		}
	}
	return ReleaseStaleProjectAssignments()
}

// ReleaseStaleProjectAssignments releases claims, reviews, user field values and
// watches held by people who can no longer open the project.
func ReleaseStaleProjectAssignments() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	ctx := context.Background()

	rows, err := pool.Query(ctx, staleAssignmentsSQL)
	if err != nil {
		return err
	}
	type pair struct{ project, user int }
	var stale []pair
	for rows.Next() {
		var p pair
		if err := rows.Scan(&p.project, &p.user); err != nil {
			rows.Close()
			return err
		}
		stale = append(stale, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, p := range stale {
		if _, err := pool.Exec(ctx, `SELECT gotodo_release_project_assignments($1, $2)`, p.project, p.user); err != nil {
			return err
		}
	}
	return nil
}
