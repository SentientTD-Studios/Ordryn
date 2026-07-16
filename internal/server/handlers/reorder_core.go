package handlers

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrReorderValidation is returned when task IDs fail ownership/favorite/project checks.
var ErrReorderValidation = errors.New("reorder validation failed")

// applyRelativeReorder places orderedIDs into the slots those IDs currently occupy
// in allIDs. This supports full-page reorder and filtered subsets (e.g. incomplete-only)
// without overwriting unrelated tasks such as completed ones outside the client list.
func applyRelativeReorder(allIDs []int, orderedIDs []int) ([]int, error) {
	if len(orderedIDs) == 0 {
		return allIDs, nil
	}

	indexOf := make(map[int]int, len(allIDs))
	for i, id := range allIDs {
		indexOf[id] = i
	}

	slots := make([]int, 0, len(orderedIDs))
	seen := make(map[int]struct{}, len(orderedIDs))
	for _, id := range orderedIDs {
		if _, dup := seen[id]; dup {
			return nil, fmt.Errorf("%w: duplicate task id %d", ErrReorderValidation, id)
		}
		seen[id] = struct{}{}
		idx, ok := indexOf[id]
		if !ok {
			return nil, fmt.Errorf("%w: task %d not in reorder group", ErrReorderValidation, id)
		}
		slots = append(slots, idx)
	}
	sort.Ints(slots)

	out := make([]int, len(allIDs))
	copy(out, allIDs)
	for i, slot := range slots {
		out[slot] = orderedIDs[i]
	}
	return out, nil
}

// reorderTaskPositions validates IDs and renumbers position within a favorite/project group.
// page and pageSize are retained for call-site compatibility but relative slot placement is used.
func reorderTaskPositions(
	ctx context.Context,
	db *pgxpool.Pool,
	userID int,
	ids []int,
	isFav bool,
	page, pageSize int,
	projectFilter *int,
) error {
	_ = page
	_ = pageSize
	if len(ids) == 0 {
		return fmt.Errorf("%w: empty task_ids", ErrReorderValidation)
	}

	for _, id := range ids {
		var exists bool
		projectCond := ""
		args := []interface{}{id, userID, isFav}
		if projectFilter != nil {
			if *projectFilter == 0 {
				projectCond = " AND project_id IS NULL"
			} else {
				projectCond = " AND project_id = $4"
				args = append(args, *projectFilter)
			}
		}
		query := "SELECT EXISTS(SELECT 1 FROM tasks WHERE id = $1 AND user_id = $2 AND COALESCE(is_favorite,false) = $3" + projectCond + ")"
		if err := db.QueryRow(ctx, query, args...).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("%w: task %d does not belong to user or mismatched favorite group/project", ErrReorderValidation, id)
		}
	}

	argsAll := []interface{}{userID, isFav}
	q := "SELECT id FROM tasks WHERE user_id = $1 AND COALESCE(is_favorite,false) = $2"
	if projectFilter != nil {
		if *projectFilter == 0 {
			q += " AND project_id IS NULL"
		} else {
			q += " AND project_id = $3"
			argsAll = append(argsAll, *projectFilter)
		}
	}
	q += " ORDER BY position ASC, id ASC"

	rowsAll, err := db.Query(ctx, q, argsAll...)
	if err != nil {
		return err
	}
	defer rowsAll.Close()

	allIDs := make([]int, 0)
	for rowsAll.Next() {
		var tid int
		if err := rowsAll.Scan(&tid); err != nil {
			return err
		}
		allIDs = append(allIDs, tid)
	}
	if err := rowsAll.Err(); err != nil {
		return err
	}

	if len(allIDs) == 0 {
		return nil
	}

	allIDs, err = applyRelativeReorder(allIDs, ids)
	if err != nil {
		return err
	}

	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	for idx, id := range allIDs {
		pos := idx + 1
		if _, err := tx.Exec(ctx, "UPDATE tasks SET position = $1 WHERE id = $2 AND user_id = $3", pos, id, userID); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}

	logTaskEvent(ids[0], userID, "reordered", map[string]interface{}{"count": len(ids)})
	return nil
}
