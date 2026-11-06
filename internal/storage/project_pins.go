package storage

import (
	"context"
	"errors"
	"fmt"
)

// MaxProjectPins caps how many projects one user can pin to the sidebar.
const MaxProjectPins = 50

// CreateProjectPinsTable creates the per-user pinned-project table. Pins are per user (not per
// project) so shared and company projects can be pinned without touching membership rows.
func CreateProjectPinsTable() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	stmts := []string{
		`CREATE TABLE IF NOT EXISTS user_project_pins (
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
			position INTEGER NOT NULL DEFAULT 0,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (user_id, project_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_user_project_pins_project_id ON user_project_pins(project_id)`,
	}
	for _, q := range stmts {
		if _, err := pool.Exec(context.Background(), q); err != nil {
			return fmt.Errorf("failed to create project pins table: %w", err)
		}
	}
	return nil
}

// GetProjectPinPositions returns project id -> pin position for the user's pinned projects.
// Pins on projects the user can no longer access are ignored.
func GetProjectPinPositions(userID int) (map[int]int, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(), `
		SELECT pin.project_id, pin.position
		FROM user_project_pins pin
		JOIN projects p ON p.id = pin.project_id
		LEFT JOIN project_members pm ON pm.project_id = p.id AND pm.user_id = $1
		WHERE pin.user_id = $1 AND (p.user_id = $1 OR pm.user_id = $1)`, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to query project pins: %w", err)
	}
	defer rows.Close()

	out := make(map[int]int)
	for rows.Next() {
		var id, pos int
		if err := rows.Scan(&id, &pos); err != nil {
			return nil, err
		}
		out[id] = pos
	}
	return out, rows.Err()
}

// ErrProjectPinNotAccessible is returned when a pin targets a project the user cannot open.
var ErrProjectPinNotAccessible = errors.New("project not found")

// SetProjectPins replaces the user's pinned projects with orderedIDs, in order.
// Every id must be a project the user owns or is a member of. An empty list clears all pins.
func SetProjectPins(userID int, orderedIDs []int) error {
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

	if _, err := tx.Exec(ctx, `DELETE FROM user_project_pins WHERE user_id = $1`, userID); err != nil {
		return fmt.Errorf("failed to clear project pins: %w", err)
	}
	if len(orderedIDs) > 0 {
		ids := make([]int32, len(orderedIDs))
		for i, id := range orderedIDs {
			ids[i] = int32(id)
		}
		res, err := tx.Exec(ctx, `
			INSERT INTO user_project_pins (user_id, project_id, position)
			SELECT $1, p.id, (ids.ord - 1)::int
			FROM unnest($2::int[]) WITH ORDINALITY AS ids(id, ord)
			JOIN projects p ON p.id = ids.id
			WHERE p.user_id = $1
			   OR EXISTS (SELECT 1 FROM project_members pm WHERE pm.project_id = p.id AND pm.user_id = $1)`,
			userID, ids)
		if err != nil {
			return fmt.Errorf("failed to save project pins: %w", err)
		}
		if int(res.RowsAffected()) != len(orderedIDs) {
			return ErrProjectPinNotAccessible
		}
	}
	return tx.Commit(ctx)
}
