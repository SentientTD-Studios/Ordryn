package storage

import (
	"context"
	"fmt"
	"strings"
)

const MaxTagsPerTask = 5

// Tag represents a user-owned label.
type Tag struct {
	ID     int
	UserID int
	Name   string
	Color  string
}

var tagPalette = []string{
	"#6c757d", "#0d6efd", "#198754", "#dc3545",
	"#fd7e14", "#6610f2", "#20c997", "#d63384",
}

func tagColorForID(id int) string {
	if len(tagPalette) == 0 {
		return "#6c757d"
	}
	return tagPalette[(id-1)%len(tagPalette)]
}

// CreateTagsTables creates tags and task_tags tables.
func CreateTagsTables() error {
	pool, err := OpenDatabase()
	if err != nil {
		return fmt.Errorf("failed to open database: %v", err)
	}
	defer CloseDatabase(pool)

	_, err = pool.Exec(context.Background(), `
		CREATE TABLE IF NOT EXISTS tags (
			id SERIAL PRIMARY KEY,
			user_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			color VARCHAR(7) DEFAULT '#6c757d',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(user_id, name)
		);
		CREATE TABLE IF NOT EXISTS task_tags (
			task_id INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
			tag_id INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
			PRIMARY KEY (task_id, tag_id)
		);
		CREATE INDEX IF NOT EXISTS idx_task_tags_tag_id ON task_tags(tag_id);
	`)
	if err != nil {
		return fmt.Errorf("failed to create tags tables: %v", err)
	}
	return nil
}

// GetTagsForUser returns all tags for a user ordered by name.
func GetTagsForUser(userID int) ([]Tag, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(),
		"SELECT id, user_id, name, COALESCE(color, '#6c757d') FROM tags WHERE user_id = $1 ORDER BY LOWER(name)", userID)
	if err != nil {
		return nil, fmt.Errorf("failed to query tags: %v", err)
	}
	defer rows.Close()

	var out []Tag
	for rows.Next() {
		var t Tag
		if err := rows.Scan(&t.ID, &t.UserID, &t.Name, &t.Color); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

// GetOrCreateTagByName returns an existing tag or creates one (case-insensitive name match).
func GetOrCreateTagByName(userID int, name string) (*Tag, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("tag name is required")
	}
	if len(name) > 50 {
		return nil, fmt.Errorf("tag name must be 50 characters or less")
	}

	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	var t Tag
	err = pool.QueryRow(context.Background(),
		"SELECT id, user_id, name, COALESCE(color, '#6c757d') FROM tags WHERE user_id = $1 AND LOWER(name) = LOWER($2)",
		userID, name).Scan(&t.ID, &t.UserID, &t.Name, &t.Color)
	if err == nil {
		return &t, nil
	}

	err = pool.QueryRow(context.Background(),
		"INSERT INTO tags (user_id, name) VALUES ($1, $2) RETURNING id, user_id, name, COALESCE(color, '#6c757d')",
		userID, name).Scan(&t.ID, &t.UserID, &t.Name, &t.Color)
	if err != nil {
		return nil, fmt.Errorf("failed to create tag: %v", err)
	}
	t.Color = tagColorForID(t.ID)
	_, _ = pool.Exec(context.Background(), "UPDATE tags SET color = $1 WHERE id = $2", t.Color, t.ID)
	return &t, nil
}

// DeleteTag removes a tag owned by the user.
func DeleteTag(id, userID int) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	_, err = pool.Exec(context.Background(), "DELETE FROM tags WHERE id = $1 AND user_id = $2", id, userID)
	if err != nil {
		return fmt.Errorf("failed to delete tag: %v", err)
	}
	return nil
}

// UpdateTag renames a tag owned by the user.
func UpdateTag(id, userID int, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("tag name is required")
	}
	if len(name) > 50 {
		return fmt.Errorf("tag name must be 50 characters or less")
	}

	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	var exists bool
	if err := pool.QueryRow(context.Background(),
		"SELECT EXISTS(SELECT 1 FROM tags WHERE user_id = $1 AND LOWER(name) = LOWER($2) AND id != $3)",
		userID, name, id).Scan(&exists); err != nil {
		return fmt.Errorf("failed to check tag name: %v", err)
	}
	if exists {
		return fmt.Errorf("a tag with that name already exists")
	}

	tag, err := pool.Exec(context.Background(),
		"UPDATE tags SET name = $1 WHERE id = $2 AND user_id = $3", name, id, userID)
	if err != nil {
		return fmt.Errorf("failed to update tag: %v", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("tag not found")
	}
	return nil
}

// SetTaskTags replaces all tags on a task (max MaxTagsPerTask).
func SetTaskTags(taskID, userID int, tagIDs []int) error {
	if len(tagIDs) > MaxTagsPerTask {
		return fmt.Errorf("maximum %d tags per task", MaxTagsPerTask)
	}

	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	var ownerID int
	if err := pool.QueryRow(context.Background(), "SELECT user_id FROM tasks WHERE id = $1", taskID).Scan(&ownerID); err != nil {
		return fmt.Errorf("task not found")
	}
	if ownerID != userID {
		return fmt.Errorf("not authorized")
	}

	for _, tagID := range tagIDs {
		var exists bool
		if err := pool.QueryRow(context.Background(),
			"SELECT EXISTS(SELECT 1 FROM tags WHERE id = $1 AND user_id = $2)", tagID, userID).Scan(&exists); err != nil || !exists {
			return fmt.Errorf("invalid tag selection")
		}
	}

	_, err = pool.Exec(context.Background(), "DELETE FROM task_tags WHERE task_id = $1", taskID)
	if err != nil {
		return fmt.Errorf("failed to clear task tags: %v", err)
	}

	for _, tagID := range tagIDs {
		_, err = pool.Exec(context.Background(), "INSERT INTO task_tags (task_id, tag_id) VALUES ($1, $2)", taskID, tagID)
		if err != nil {
			return fmt.Errorf("failed to assign tag: %v", err)
		}
	}
	return nil
}

// GetTagsForTask returns tags assigned to a single task.
func GetTagsForTask(taskID int) ([]Tag, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(), `
		SELECT tg.id, tg.user_id, tg.name, COALESCE(tg.color, '#6c757d')
		FROM tags tg
		JOIN task_tags tt ON tt.tag_id = tg.id
		WHERE tt.task_id = $1
		ORDER BY LOWER(tg.name)`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Tag
	for rows.Next() {
		var t Tag
		if err := rows.Scan(&t.ID, &t.UserID, &t.Name, &t.Color); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

// GetTagsForTasks batch-loads tags for multiple tasks.
func GetTagsForTasks(taskIDs []int) (map[int][]Tag, error) {
	result := make(map[int][]Tag)
	if len(taskIDs) == 0 {
		return result, nil
	}

	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(), `
		SELECT tt.task_id, tg.id, tg.user_id, tg.name, COALESCE(tg.color, '#6c757d')
		FROM task_tags tt
		JOIN tags tg ON tg.id = tt.tag_id
		WHERE tt.task_id = ANY($1)
		ORDER BY tt.task_id, LOWER(tg.name)`, taskIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var taskID int
		var t Tag
		if err := rows.Scan(&taskID, &t.ID, &t.UserID, &t.Name, &t.Color); err != nil {
			return nil, err
		}
		result[taskID] = append(result[taskID], t)
	}
	return result, nil
}

// ResolveTaskTagIDs parses tag_ids from form and creates new tags from comma-separated names.
func ResolveTaskTagIDs(userID int, tagIDStrs []string, newTagsCSV string) ([]int, error) {
	seen := make(map[int]bool)
	var ids []int

	for _, s := range tagIDStrs {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		id, err := parseInt(s)
		if err != nil {
			continue
		}
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}

	for _, part := range strings.Split(newTagsCSV, ",") {
		name := strings.TrimSpace(part)
		if name == "" {
			continue
		}
		t, err := GetOrCreateTagByName(userID, name)
		if err != nil {
			return nil, err
		}
		if !seen[t.ID] {
			seen[t.ID] = true
			ids = append(ids, t.ID)
		}
	}

	if len(ids) > MaxTagsPerTask {
		return nil, fmt.Errorf("maximum %d tags per task", MaxTagsPerTask)
	}
	return ids, nil
}

func parseInt(s string) (int, error) {
	var n int
	_, err := fmt.Sscanf(s, "%d", &n)
	return n, err
}
