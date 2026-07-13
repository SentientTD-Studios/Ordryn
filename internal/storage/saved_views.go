package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

const MaxSavedViewsPerUser = 20

// SavedViewFilter mirrors FilterContext fields (excluding page).
type SavedViewFilter struct {
	Project   string `json:"project"`
	Status    string `json:"status"`
	Due       string `json:"due"`
	Completed string `json:"completed"`
	Priority  string `json:"priority"`
	Tag       string `json:"tag"`
	Sort      string `json:"sort"`
	Search    string `json:"search"`
}

// SavedView is a per-user named filter preset.
type SavedView struct {
	ID         int
	UserID     int
	Name       string
	Filter     SavedViewFilter
	SortOrder  int
	CreatedAt  time.Time
}

// CreateSavedViewsTable ensures the saved_views table exists.
func CreateSavedViewsTable() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	_, err = pool.Exec(context.Background(), `
		CREATE TABLE IF NOT EXISTS saved_views (
			id SERIAL PRIMARY KEY,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			filter_json JSONB NOT NULL DEFAULT '{}',
			sort_order INTEGER NOT NULL DEFAULT 0,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE (user_id, name)
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create saved_views table: %v", err)
	}
	_, err = pool.Exec(context.Background(),
		`CREATE INDEX IF NOT EXISTS idx_saved_views_user_id ON saved_views(user_id)`)
	if err != nil {
		return fmt.Errorf("failed to create saved_views index: %v", err)
	}
	return nil
}

// ListSavedViewsForUser returns saved views ordered by sort_order then name.
func ListSavedViewsForUser(userID int) ([]SavedView, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(),
		`SELECT id, user_id, name, filter_json, sort_order, created_at
		 FROM saved_views WHERE user_id = $1
		 ORDER BY sort_order ASC, name ASC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]SavedView, 0)
	for rows.Next() {
		var sv SavedView
		var raw []byte
		if err := rows.Scan(&sv.ID, &sv.UserID, &sv.Name, &raw, &sv.SortOrder, &sv.CreatedAt); err != nil {
			return nil, err
		}
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &sv.Filter)
		}
		out = append(out, sv)
	}
	return out, nil
}

// CountSavedViewsForUser returns how many saved views a user has.
func CountSavedViewsForUser(userID int) (int, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return 0, err
	}
	defer CloseDatabase(pool)

	var count int
	err = pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM saved_views WHERE user_id = $1`, userID).Scan(&count)
	return count, err
}

// GetSavedViewByID returns a saved view owned by userID.
func GetSavedViewByID(id, userID int) (*SavedView, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	var sv SavedView
	var raw []byte
	err = pool.QueryRow(context.Background(),
		`SELECT id, user_id, name, filter_json, sort_order, created_at
		 FROM saved_views WHERE id = $1 AND user_id = $2`, id, userID).
		Scan(&sv.ID, &sv.UserID, &sv.Name, &raw, &sv.SortOrder, &sv.CreatedAt)
	if err != nil {
		return nil, err
	}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &sv.Filter)
	}
	return &sv, nil
}

// CreateSavedView inserts a new saved view for the user.
func CreateSavedView(userID int, name string, filter SavedViewFilter, sortOrder int) (*SavedView, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	raw, err := json.Marshal(filter)
	if err != nil {
		return nil, err
	}

	var id int
	var createdAt time.Time
	err = pool.QueryRow(context.Background(),
		`INSERT INTO saved_views (user_id, name, filter_json, sort_order)
		 VALUES ($1, $2, $3, $4) RETURNING id, created_at`,
		userID, name, raw, sortOrder).Scan(&id, &createdAt)
	if err != nil {
		return nil, err
	}
	return &SavedView{
		ID:        id,
		UserID:    userID,
		Name:      name,
		Filter:    filter,
		SortOrder: sortOrder,
		CreatedAt: createdAt,
	}, nil
}

// UpdateSavedView updates name and/or filter for an existing saved view.
func UpdateSavedView(id, userID int, name string, filter *SavedViewFilter) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	if filter != nil {
		raw, err := json.Marshal(filter)
		if err != nil {
			return err
		}
		_, err = pool.Exec(context.Background(),
			`UPDATE saved_views SET name = $1, filter_json = $2 WHERE id = $3 AND user_id = $4`,
			name, raw, id, userID)
		return err
	}
	_, err = pool.Exec(context.Background(),
		`UPDATE saved_views SET name = $1 WHERE id = $2 AND user_id = $3`,
		name, id, userID)
	return err
}

// DeleteSavedView removes a saved view owned by the user.
func DeleteSavedView(id, userID int) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	tag, err := pool.Exec(context.Background(),
		`DELETE FROM saved_views WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("saved view not found")
	}
	return nil
}
