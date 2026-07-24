package domain

import (
	"context"
	"database/sql"
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
		canRead, writeRole, _, accessErr := storage.CanUserAccessTask(id, userID)
		if accessErr != nil {
			return accessErr
		}
		if !canRead || !storage.RoleCanWrite(writeRole) {
			return fmt.Errorf("%w: task %d does not belong to user or mismatched favorite group/project", ErrValidation, id)
		}
		var isFavorite bool
		var proj sql.NullInt64
		err := pool.QueryRow(ctx,
			`SELECT COALESCE(is_favorite,false), project_id FROM tasks WHERE id = $1`, id).Scan(&isFavorite, &proj)
		if err != nil {
			return err
		}
		if isFavorite != isFav {
			return fmt.Errorf("%w: task %d does not belong to user or mismatched favorite group/project", ErrValidation, id)
		}
		if projectFilter != nil {
			if *projectFilter == 0 {
				if proj.Valid {
					return fmt.Errorf("%w: task %d does not belong to user or mismatched favorite group/project", ErrValidation, id)
				}
			} else if !proj.Valid || int(proj.Int64) != *projectFilter {
				return fmt.Errorf("%w: task %d does not belong to user or mismatched favorite group/project", ErrValidation, id)
			}
		}
	}

	vis := storage.TaskListVisibleCondition("t", "$1", projectFilter)
	argsAll := []interface{}{userID, isFav}
	q := "SELECT t.id FROM tasks t WHERE " + vis + " AND COALESCE(t.is_favorite,false) = $2"
	if projectFilter != nil {
		if *projectFilter == 0 {
			q += " AND t.project_id IS NULL"
		} else {
			q += " AND t.project_id = $3"
			argsAll = append(argsAll, *projectFilter)
		}
	}
	q += " ORDER BY t.position ASC, t.id ASC"

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
		if _, err := tx.Exec(ctx, "UPDATE tasks SET position = $1 WHERE id = $2", pos, id); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}

	_ = storage.LogTaskEvent(ids[0], userID, "reordered", map[string]interface{}{"count": len(ids)})
	return nil
}
