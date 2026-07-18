package handlers

import (
	"GoTodo/internal/server/utils"
	"GoTodo/internal/sessionstore"
	"GoTodo/internal/storage"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func toRedisUndoSnapshots(tasks []DeletedTaskSnapshot) []utils.UndoTaskSnapshot {
	out := make([]utils.UndoTaskSnapshot, 0, len(tasks))
	for _, t := range tasks {
		out = append(out, utils.UndoTaskSnapshot{
			ID:          t.ID,
			Title:       t.Title,
			Description: t.Description,
			DueDate:     t.DueDate,
			Completed:   t.Completed,
			IsFavorite:  t.IsFavorite,
			Priority:    t.Priority,
			Position:    t.Position,
			ProjectID:   t.ProjectID,
			TagIDs:      t.TagIDs,
		})
	}
	return out
}

func fromRedisUndoSnapshots(tasks []utils.UndoTaskSnapshot) []DeletedTaskSnapshot {
	out := make([]DeletedTaskSnapshot, 0, len(tasks))
	for _, t := range tasks {
		out = append(out, DeletedTaskSnapshot{
			ID:          t.ID,
			Title:       t.Title,
			Description: t.Description,
			DueDate:     t.DueDate,
			Completed:   t.Completed,
			IsFavorite:  t.IsFavorite,
			Priority:    t.Priority,
			Position:    t.Position,
			ProjectID:   t.ProjectID,
			TagIDs:      t.TagIDs,
		})
	}
	return out
}

const undoTTL = 120 * time.Second

// DeletedTaskSnapshot captures task state for undo restore.
type DeletedTaskSnapshot struct {
	ID          int
	Title       string
	Description string
	DueDate     string
	Completed   bool
	IsFavorite  bool
	Priority    int
	Position    int
	ProjectID   *int
	TagIDs      []int
}

type pendingUndo struct {
	ExpiresAt int64
	Tasks     []DeletedTaskSnapshot
}

func snapshotTasksForUndo(ctx context.Context, db *pgxpool.Pool, ids []int, userID int) ([]DeletedTaskSnapshot, error) {
	out := make([]DeletedTaskSnapshot, 0, len(ids))
	for _, id := range ids {
		var snap DeletedTaskSnapshot
		var projectID sql.NullInt64
		var dueDate sql.NullString
		err := db.QueryRow(ctx, `
			SELECT id, title, COALESCE(description, ''), COALESCE(completed, false),
			       COALESCE(is_favorite, false), COALESCE(priority, 0), COALESCE(position, 0),
			       project_id, COALESCE(CAST(due_date AS TEXT), '')
			FROM tasks WHERE id = $1 AND user_id = $2`,
			id, userID).Scan(
			&snap.ID, &snap.Title, &snap.Description, &snap.Completed, &snap.IsFavorite,
			&snap.Priority, &snap.Position, &projectID, &dueDate,
		)
		if err != nil {
			return nil, fmt.Errorf("task not found")
		}
		if projectID.Valid {
			pid := int(projectID.Int64)
			snap.ProjectID = &pid
		}
		if dueDate.Valid {
			snap.DueDate = dueDate.String
		}
		tags, err := storage.GetTagsForTask(id)
		if err != nil {
			return nil, err
		}
		snap.TagIDs = make([]int, 0, len(tags))
		for _, t := range tags {
			snap.TagIDs = append(snap.TagIDs, t.ID)
		}
		out = append(out, snap)
	}
	return out, nil
}

func savePendingUndo(r *http.Request, w http.ResponseWriter, tasks []DeletedTaskSnapshot) error {
	if len(tasks) == 0 {
		return nil
	}
	sess, err := sessionstore.Store.Get(r, "session")
	if err != nil {
		return err
	}
	payload, err := json.Marshal(pendingUndo{
		ExpiresAt: time.Now().Add(undoTTL).Unix(),
		Tasks:     tasks,
	})
	if err != nil {
		return err
	}
	sess.Values["pending_undo"] = string(payload)
	return sess.Save(r, w)
}

func loadPendingUndo(r *http.Request) (*pendingUndo, error) {
	sess, err := sessionstore.Store.Get(r, "session")
	if err != nil {
		return nil, err
	}
	raw, ok := sess.Values["pending_undo"]
	if !ok || raw == nil {
		return nil, fmt.Errorf("nothing to undo")
	}
	var pu pendingUndo
	switch v := raw.(type) {
	case string:
		if err := json.Unmarshal([]byte(v), &pu); err != nil {
			return nil, fmt.Errorf("invalid undo state")
		}
	case pendingUndo:
		pu = v
	default:
		return nil, fmt.Errorf("invalid undo state")
	}
	if time.Now().Unix() > pu.ExpiresAt {
		return nil, fmt.Errorf("undo expired")
	}
	if len(pu.Tasks) == 0 {
		return nil, fmt.Errorf("nothing to undo")
	}
	return &pu, nil
}

func clearPendingUndo(r *http.Request, w http.ResponseWriter) error {
	sess, err := sessionstore.Store.Get(r, "session")
	if err != nil {
		return err
	}
	delete(sess.Values, "pending_undo")
	return sess.Save(r, w)
}

func restoreDeletedTasks(ctx context.Context, db *pgxpool.Pool, userID int, tasks []DeletedTaskSnapshot) error {
	var nextPos int
	if err := db.QueryRow(ctx,
		"SELECT COALESCE(MAX(position),0) + 1 FROM tasks WHERE user_id = $1 AND (is_favorite IS NULL OR is_favorite = false)",
		userID).Scan(&nextPos); err != nil {
		return err
	}

	for _, snap := range tasks {
		position := nextPos
		nextPos++
		if snap.IsFavorite {
			position = snap.Position
		}

		newID, err := insertRestoredTask(ctx, db, userID, snap, position)
		if err != nil {
			return err
		}
		if len(snap.TagIDs) > 0 {
			if err := storage.SetTaskTags(newID, userID, snap.TagIDs); err != nil {
				return err
			}
		}
		logTaskEvent(newID, userID, "created", map[string]interface{}{"restored": true, "original_id": snap.ID})
	}

	_, _ = db.Exec(ctx, `SELECT setval(pg_get_serial_sequence('tasks', 'id'), GREATEST((SELECT COALESCE(MAX(id), 1) FROM tasks), 1))`)
	return nil
}

func insertRestoredTask(ctx context.Context, db *pgxpool.Pool, userID int, snap DeletedTaskSnapshot, position int) (int, error) {
	if snap.ID > 0 {
		var exists bool
		_ = db.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM tasks WHERE id = $1)", snap.ID).Scan(&exists)
		if !exists {
			var newID int
			err := insertTaskWithID(ctx, db, snap.ID, userID, snap, position, &newID)
			if err == nil {
				return newID, nil
			}
		}
	}
	var newID int
	err := insertTaskWithID(ctx, db, 0, userID, snap, position, &newID)
	return newID, err
}

func insertTaskWithID(ctx context.Context, db *pgxpool.Pool, explicitID, userID int, snap DeletedTaskSnapshot, position int, outID *int) error {
	if explicitID > 0 {
		if snap.ProjectID != nil && snap.DueDate != "" {
			return db.QueryRow(ctx,
				`INSERT INTO tasks (id, title, description, completed, user_id, time_stamp, position, priority, project_id, due_date, is_favorite)
				 VALUES ($1,$2,$3,$4,$5,NOW() AT TIME ZONE 'UTC',$6,$7,$8,$9,$10) RETURNING id`,
				explicitID, snap.Title, snap.Description, snap.Completed, userID, position, snap.Priority, *snap.ProjectID, snap.DueDate, snap.IsFavorite).Scan(outID)
		}
		if snap.ProjectID != nil {
			return db.QueryRow(ctx,
				`INSERT INTO tasks (id, title, description, completed, user_id, time_stamp, position, priority, project_id, is_favorite)
				 VALUES ($1,$2,$3,$4,$5,NOW() AT TIME ZONE 'UTC',$6,$7,$8,$9) RETURNING id`,
				explicitID, snap.Title, snap.Description, snap.Completed, userID, position, snap.Priority, *snap.ProjectID, snap.IsFavorite).Scan(outID)
		}
		if snap.DueDate != "" {
			return db.QueryRow(ctx,
				`INSERT INTO tasks (id, title, description, completed, user_id, time_stamp, position, priority, due_date, is_favorite)
				 VALUES ($1,$2,$3,$4,$5,NOW() AT TIME ZONE 'UTC',$6,$7,$8,$9) RETURNING id`,
				explicitID, snap.Title, snap.Description, snap.Completed, userID, position, snap.Priority, snap.DueDate, snap.IsFavorite).Scan(outID)
		}
		return db.QueryRow(ctx,
			`INSERT INTO tasks (id, title, description, completed, user_id, time_stamp, position, priority, is_favorite)
			 VALUES ($1,$2,$3,$4,$5,NOW() AT TIME ZONE 'UTC',$6,$7,$8) RETURNING id`,
			explicitID, snap.Title, snap.Description, snap.Completed, userID, position, snap.Priority, snap.IsFavorite).Scan(outID)
	}
	if snap.ProjectID != nil && snap.DueDate != "" {
		return db.QueryRow(ctx,
			`INSERT INTO tasks (title, description, completed, user_id, time_stamp, position, priority, project_id, due_date, is_favorite)
			 VALUES ($1,$2,$3,$4,NOW() AT TIME ZONE 'UTC',$5,$6,$7,$8,$9) RETURNING id`,
			snap.Title, snap.Description, snap.Completed, userID, position, snap.Priority, *snap.ProjectID, snap.DueDate, snap.IsFavorite).Scan(outID)
	}
	if snap.ProjectID != nil {
		return db.QueryRow(ctx,
			`INSERT INTO tasks (title, description, completed, user_id, time_stamp, position, priority, project_id, is_favorite)
			 VALUES ($1,$2,$3,$4,NOW() AT TIME ZONE 'UTC',$5,$6,$7,$8) RETURNING id`,
			snap.Title, snap.Description, snap.Completed, userID, position, snap.Priority, *snap.ProjectID, snap.IsFavorite).Scan(outID)
	}
	if snap.DueDate != "" {
		return db.QueryRow(ctx,
			`INSERT INTO tasks (title, description, completed, user_id, time_stamp, position, priority, due_date, is_favorite)
			 VALUES ($1,$2,$3,$4,NOW() AT TIME ZONE 'UTC',$5,$6,$7,$8) RETURNING id`,
			snap.Title, snap.Description, snap.Completed, userID, position, snap.Priority, snap.DueDate, snap.IsFavorite).Scan(outID)
	}
	return db.QueryRow(ctx,
		`INSERT INTO tasks (title, description, completed, user_id, time_stamp, position, priority, is_favorite)
		 VALUES ($1,$2,$3,$4,NOW() AT TIME ZONE 'UTC',$5,$6,$7) RETURNING id`,
		snap.Title, snap.Description, snap.Completed, userID, position, snap.Priority, snap.IsFavorite).Scan(outID)
}
