package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"GoTodo/internal/domain"
	"GoTodo/internal/server/utils"
	"GoTodo/internal/sessionstore"
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
			ParentID:    t.ParentID,
			TagIDs:      t.TagIDs,
			Recurrence:  t.Recurrence,
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
			ParentID:    t.ParentID,
			TagIDs:      t.TagIDs,
			Recurrence:  t.Recurrence,
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
	ParentID    *int
	TagIDs      []int
	Recurrence  *utils.UndoRecurrence
}

type pendingUndo struct {
	ExpiresAt int64
	Tasks     []DeletedTaskSnapshot
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

// restoreDeletedTasks undoes a delete by restoring the tasks from the trash,
// with every comment, link, field, and history entry intact. A subtask listed
// alongside its parent comes back with the parent; one tried before its parent
// is retried once the parent is restored.
func restoreDeletedTasks(ctx context.Context, userID int, tasks []DeletedTaskSnapshot) error {
	pending := make([]int, 0, len(tasks))
	for _, t := range tasks {
		pending = append(pending, t.ID)
	}
	for pass := 0; pass < 2 && len(pending) > 0; pass++ {
		var retry []int
		for _, id := range pending {
			_, err := domain.RestoreTrashedTaskForUser(ctx, userID, id)
			switch {
			case err == nil:
			case errors.Is(err, domain.ErrConflict):
				retry = append(retry, id)
			case errors.Is(err, domain.ErrNotFound):
				// Already restored with its parent, or permanently deleted.
			default:
				return err
			}
		}
		pending = retry
	}
	return nil
}
