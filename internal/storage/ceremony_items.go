package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Ceremony item types. Items are written by many people at once, so they live in their own rows
// rather than in the session's revisioned state.
const (
	CeremonyItemCard   = "card"   // retro card
	CeremonyItemAction = "action" // retro action item
	CeremonyItemEntry  = "entry"  // standup check-in, one per person
)

// ErrCeremonyItemNotFound is returned when an item id does not exist in the session.
var ErrCeremonyItemNotFound = errors.New("ceremony item not found")

// CeremonyItem is a retro card, retro action item, or standup check-in.
type CeremonyItem struct {
	ID         int
	SessionID  int
	Type       string
	Column     string
	AuthorID   int
	AuthorName string
	Body       string
	Data       json.RawMessage
	ParentID   int // retro grouping: the card this one is stacked under
	Position   int
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// CreateCeremonyItemTables adds item and vote tables plus per-project ceremony settings.
func CreateCeremonyItemTables() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	stmts := []string{
		`ALTER TABLE project_ceremonies ADD COLUMN IF NOT EXISTS settings JSONB NOT NULL DEFAULT '{}'::jsonb`,
		`CREATE TABLE IF NOT EXISTS ceremony_items (
			id SERIAL PRIMARY KEY,
			session_id INTEGER NOT NULL REFERENCES ceremony_sessions(id) ON DELETE CASCADE,
			item_type TEXT NOT NULL,
			column_key TEXT NOT NULL DEFAULT '',
			author_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
			body TEXT NOT NULL DEFAULT '',
			data JSONB NOT NULL DEFAULT '{}'::jsonb,
			parent_id INTEGER REFERENCES ceremony_items(id) ON DELETE SET NULL,
			position INTEGER NOT NULL DEFAULT 0,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_ceremony_items_session ON ceremony_items (session_id, item_type, position, id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_ceremony_items_one_entry
			ON ceremony_items (session_id, author_id) WHERE item_type = 'entry'`,
		`CREATE TABLE IF NOT EXISTS ceremony_votes (
			item_id INTEGER NOT NULL REFERENCES ceremony_items(id) ON DELETE CASCADE,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (item_id, user_id)
		)`,
	}
	for _, s := range stmts {
		if _, err := pool.Exec(context.Background(), s); err != nil {
			return fmt.Errorf("create ceremony item tables: %w", err)
		}
	}
	return nil
}

// GetProjectCeremonyConfig returns a project's stored settings JSON for kind ("{}" when unset).
func GetProjectCeremonyConfig(projectID int, kind string) (json.RawMessage, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	var raw []byte
	err = pool.QueryRow(context.Background(),
		`SELECT settings FROM project_ceremonies WHERE project_id = $1 AND kind = $2`, projectID, kind).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return json.RawMessage(`{}`), nil
	}
	if err != nil {
		return nil, err
	}
	return json.RawMessage(raw), nil
}

// SetProjectCeremonyConfig stores a project's settings JSON for kind without changing its opt-in.
func SetProjectCeremonyConfig(projectID int, kind string, cfg json.RawMessage) error {
	if !ValidCeremonyKind(kind) {
		return fmt.Errorf("unknown ceremony %q", kind)
	}
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	_, err = pool.Exec(context.Background(), `
		INSERT INTO project_ceremonies (project_id, kind, enabled, settings, updated_at) VALUES ($1, $2, FALSE, $3, NOW())
		ON CONFLICT (project_id, kind) DO UPDATE SET settings = EXCLUDED.settings, updated_at = NOW()`,
		projectID, kind, []byte(cfg))
	return err
}

const ceremonyItemSelect = `
	SELECT i.id, i.session_id, i.item_type, i.column_key, COALESCE(i.author_id, 0),
	       COALESCE(NULLIF(u.full_name, ''), NULLIF(u.user_name, ''), u.email, ''),
	       i.body, i.data, COALESCE(i.parent_id, 0), i.position, i.created_at, i.updated_at
	FROM ceremony_items i
	LEFT JOIN users u ON u.id = i.author_id`

func scanCeremonyItem(row pgx.Row) (*CeremonyItem, error) {
	var it CeremonyItem
	var data []byte
	err := row.Scan(&it.ID, &it.SessionID, &it.Type, &it.Column, &it.AuthorID, &it.AuthorName,
		&it.Body, &data, &it.ParentID, &it.Position, &it.CreatedAt, &it.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrCeremonyItemNotFound
		}
		return nil, err
	}
	it.Data = json.RawMessage(data)
	return &it, nil
}

// ListCeremonyItems returns a session's items, oldest first within position.
func ListCeremonyItems(sessionID int) ([]CeremonyItem, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	rows, err := pool.Query(context.Background(),
		ceremonyItemSelect+` WHERE i.session_id = $1 ORDER BY i.item_type, i.position, i.id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CeremonyItem{}
	for rows.Next() {
		it, err := scanCeremonyItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *it)
	}
	return out, rows.Err()
}

// GetCeremonyItem loads one item within a session.
func GetCeremonyItem(sessionID, itemID int) (*CeremonyItem, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	return scanCeremonyItem(pool.QueryRow(context.Background(),
		ceremonyItemSelect+` WHERE i.session_id = $1 AND i.id = $2`, sessionID, itemID))
}

// CountCeremonyItems counts a session's items of one type (all authors, or one author when authorID > 0).
func CountCeremonyItems(sessionID int, itemType string, authorID int) (int, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return 0, err
	}
	defer CloseDatabase(pool)
	var n int
	err = pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM ceremony_items
		WHERE session_id = $1 AND item_type = $2 AND ($3 = 0 OR author_id = $3)`,
		sessionID, itemType, authorID).Scan(&n)
	return n, err
}

// CreateCeremonyItem appends an item at the end of its column.
func CreateCeremonyItem(it CeremonyItem) (*CeremonyItem, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	data := []byte(it.Data)
	if len(data) == 0 {
		data = []byte(`{}`)
	}
	var author interface{}
	if it.AuthorID > 0 {
		author = it.AuthorID
	}
	var id int
	err = pool.QueryRow(context.Background(), `
		INSERT INTO ceremony_items (session_id, item_type, column_key, author_id, body, data, position)
		VALUES ($1, $2, $3, $4, $5, $6,
		        (SELECT COALESCE(MAX(position), 0) + 1 FROM ceremony_items WHERE session_id = $1 AND item_type = $2 AND column_key = $3))
		RETURNING id`,
		it.SessionID, it.Type, it.Column, author, it.Body, data).Scan(&id)
	if err != nil {
		return nil, err
	}
	return GetCeremonyItem(it.SessionID, id)
}

// UpsertCeremonyEntry creates or replaces a person's standup check-in.
func UpsertCeremonyEntry(sessionID, userID int, data json.RawMessage) (*CeremonyItem, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	var id int
	err = pool.QueryRow(context.Background(), `
		INSERT INTO ceremony_items (session_id, item_type, author_id, data)
		VALUES ($1, 'entry', $2, $3)
		ON CONFLICT (session_id, author_id) WHERE item_type = 'entry'
		DO UPDATE SET data = EXCLUDED.data, updated_at = NOW()
		RETURNING id`, sessionID, userID, []byte(data)).Scan(&id)
	if err != nil {
		return nil, err
	}
	return GetCeremonyItem(sessionID, id)
}

// UpdateCeremonyItem rewrites an item's column, body, data and parent.
func UpdateCeremonyItem(it CeremonyItem) (*CeremonyItem, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	var parent interface{}
	if it.ParentID > 0 {
		parent = it.ParentID
	}
	data := []byte(it.Data)
	if len(data) == 0 {
		data = []byte(`{}`)
	}
	tag, err := pool.Exec(context.Background(), `
		UPDATE ceremony_items SET column_key = $3, body = $4, data = $5, parent_id = $6, updated_at = NOW()
		WHERE session_id = $1 AND id = $2`,
		it.SessionID, it.ID, it.Column, it.Body, data, parent)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrCeremonyItemNotFound
	}
	return GetCeremonyItem(it.SessionID, it.ID)
}

// UngroupCeremonyChildren moves every card stacked under parentID back to the top level (and into
// column, when it is non-empty).
func UngroupCeremonyChildren(sessionID, parentID int, column string) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	_, err = pool.Exec(context.Background(), `
		UPDATE ceremony_items SET parent_id = NULL,
		       column_key = CASE WHEN $3 = '' THEN column_key ELSE $3 END, updated_at = NOW()
		WHERE session_id = $1 AND parent_id = $2`, sessionID, parentID, column)
	return err
}

// MoveCeremonyChildren keeps a group's stacked cards in the same column as the group's top card.
func MoveCeremonyChildren(sessionID, parentID int, column string) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	_, err = pool.Exec(context.Background(), `
		UPDATE ceremony_items SET column_key = $3, updated_at = NOW()
		WHERE session_id = $1 AND parent_id = $2`, sessionID, parentID, column)
	return err
}

// DeleteCeremonyItem removes an item; cards stacked under it return to the top level.
func DeleteCeremonyItem(sessionID, itemID int) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	tag, err := pool.Exec(context.Background(),
		`DELETE FROM ceremony_items WHERE session_id = $1 AND id = $2`, sessionID, itemID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrCeremonyItemNotFound
	}
	return nil
}

// CeremonyVoteCounts returns votes per item and the item ids userID voted for.
func CeremonyVoteCounts(sessionID, userID int) (map[int]int, map[int]bool, error) {
	counts := map[int]int{}
	mine := map[int]bool{}
	pool, err := OpenDatabase()
	if err != nil {
		return counts, mine, err
	}
	defer CloseDatabase(pool)
	rows, err := pool.Query(context.Background(), `
		SELECT v.item_id, v.user_id FROM ceremony_votes v
		JOIN ceremony_items i ON i.id = v.item_id
		WHERE i.session_id = $1`, sessionID)
	if err != nil {
		return counts, mine, err
	}
	defer rows.Close()
	for rows.Next() {
		var item, user int
		if err := rows.Scan(&item, &user); err != nil {
			return counts, mine, err
		}
		counts[item]++
		if user == userID {
			mine[item] = true
		}
	}
	return counts, mine, rows.Err()
}

// SetCeremonyVote adds or removes userID's vote on itemID. When adding, it refuses past maxVotes
// votes in the session (0 = unlimited) and returns false without error.
func SetCeremonyVote(sessionID, itemID, userID int, on bool, maxVotes int) (bool, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return false, err
	}
	defer CloseDatabase(pool)
	ctx := context.Background()
	if !on {
		_, err := pool.Exec(ctx, `DELETE FROM ceremony_votes WHERE item_id = $1 AND user_id = $2`, itemID, userID)
		return err == nil, err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	// Serialize one voter's votes in a session so two quick clicks can't both pass the limit.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1, $2)`, sessionID, userID); err != nil {
		return false, err
	}
	if maxVotes > 0 {
		var used int
		if err := tx.QueryRow(ctx, `
			SELECT COUNT(*) FROM ceremony_votes v JOIN ceremony_items i ON i.id = v.item_id
			WHERE i.session_id = $1 AND v.user_id = $2 AND v.item_id <> $3`, sessionID, userID, itemID).Scan(&used); err != nil {
			return false, err
		}
		if used >= maxVotes {
			return false, nil
		}
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO ceremony_votes (item_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, itemID, userID); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

// ClaimedOpenTasks lists a member's open claimed tasks in a project (optionally one sprint), board order.
func ClaimedOpenTasks(projectID, userID, sprintID, limit int) ([]RefinementTask, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	if limit <= 0 {
		limit = 20
	}
	rows, err := pool.Query(context.Background(), `
		SELECT t.id FROM tasks t
		WHERE `+liveProjectTaskSQL("t")+` AND t.claimed_by = $2 AND NOT COALESCE(t.completed, false)
		  AND ($3 = 0 OR t.sprint_id = $3)
		ORDER BY COALESCE(t.position, 0), t.id LIMIT $4`, projectID, userID, sprintID, limit)
	if err != nil {
		return nil, err
	}
	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	byID, err := ListRefinementTasks(projectID, ids)
	if err != nil {
		return nil, err
	}
	out := make([]RefinementTask, 0, len(ids))
	for _, id := range ids {
		if t, ok := byID[id]; ok {
			out = append(out, t)
		}
	}
	return out, nil
}

// ReparentCeremonyChildren moves every card stacked under fromParent to toParent (in column).
func ReparentCeremonyChildren(sessionID, fromParent, toParent int, column string) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	_, err = pool.Exec(context.Background(), `
		UPDATE ceremony_items SET parent_id = $3, column_key = $4, updated_at = NOW()
		WHERE session_id = $1 AND parent_id = $2`, sessionID, fromParent, toParent, column)
	return err
}
