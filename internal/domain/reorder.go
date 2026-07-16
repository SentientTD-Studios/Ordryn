package domain

import (
	"context"
	"fmt"
	"sort"

	"GoTodo/internal/storage"
)

// ApplyRelativeReorder places orderedIDs into the slots those IDs currently occupy in allIDs.
func ApplyRelativeReorder(allIDs []int, orderedIDs []int) ([]int, error) {
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
			return nil, fmt.Errorf("%w: duplicate task id %d", ErrValidation, id)
		}
		seen[id] = struct{}{}
		idx, ok := indexOf[id]
		if !ok {
			return nil, fmt.Errorf("%w: task %d not in reorder group", ErrValidation, id)
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

// ReorderTasks validates IDs and renumbers position within a favorite/project group.
func ReorderTasks(ctx context.Context, userID int, ids []int, isFav bool, projectFilter *int) error {
	if len(ids) == 0 {
		return fmt.Errorf("%w: empty task_ids", ErrValidation)
	}

	pool, err := storage.OpenDatabase()
	if err != nil {
		return err
	}
	defer storage.CloseDatabase(pool)

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
		if err := pool.QueryRow(ctx, query, args...).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("%w: task %d does not belong to user or mismatched favorite group/project", ErrValidation, id)
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

	rowsAll, err := pool.Query(ctx, q, argsAll...)
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

	allIDs, err = ApplyRelativeReorder(allIDs, ids)
	if err != nil {
		return err
	}

	tx, err := pool.Begin(ctx)
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

	_ = storage.LogTaskEvent(ids[0], userID, "reordered", map[string]interface{}{"count": len(ids)})
	return nil
}
