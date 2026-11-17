package storage

import (
	"context"
	"fmt"
	"time"
)

// CommentReactionCodes are the reactions people can leave, in display order.
var CommentReactionCodes = []string{"+1", "-1", "tada", "heart", "smile", "eyes", "rocket", "check"}

// ValidReactionCode reports whether code is one of CommentReactionCodes.
func ValidReactionCode(code string) bool {
	for _, c := range CommentReactionCodes {
		if c == code {
			return true
		}
	}
	return false
}

// CommentReaction summarizes one reaction on a comment.
type CommentReaction struct {
	Code  string
	Count int
	// Mine is true when the viewer left it.
	Mine bool
	// Names of up to 10 people who left it, for the tooltip.
	Names []string
}

// MigrateCommentReactions adds reactions and resolved state to comments.
func MigrateCommentReactions() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	for _, s := range []string{
		`CREATE TABLE IF NOT EXISTS task_comment_reactions (
			comment_id INTEGER NOT NULL REFERENCES task_comments(id) ON DELETE CASCADE,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			code VARCHAR(16) NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (comment_id, user_id, code)
		)`,
		`ALTER TABLE task_comments ADD COLUMN IF NOT EXISTS resolved_at TIMESTAMPTZ`,
		`ALTER TABLE task_comments ADD COLUMN IF NOT EXISTS resolved_by_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL`,
	} {
		if _, err := pool.Exec(context.Background(), s); err != nil {
			return fmt.Errorf("comment reactions: %w", err)
		}
	}
	return nil
}

// SetCommentReaction adds or removes the viewer's reaction.
func SetCommentReaction(commentID, userID int, code string, on bool) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	if on {
		_, err = pool.Exec(context.Background(),
			`INSERT INTO task_comment_reactions (comment_id, user_id, code) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`,
			commentID, userID, code)
	} else {
		_, err = pool.Exec(context.Background(),
			`DELETE FROM task_comment_reactions WHERE comment_id = $1 AND user_id = $2 AND code = $3`, commentID, userID, code)
	}
	return err
}

// SetCommentResolved marks a comment answered (or reopens it).
func SetCommentResolved(commentID, userID int, on bool) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	if on {
		_, err = pool.Exec(context.Background(),
			`UPDATE task_comments SET resolved_at = NOW(), resolved_by_user_id = $2 WHERE id = $1 AND resolved_at IS NULL`,
			commentID, userID)
	} else {
		_, err = pool.Exec(context.Background(),
			`UPDATE task_comments SET resolved_at = NULL, resolved_by_user_id = NULL WHERE id = $1`, commentID)
	}
	return err
}

// AttachCommentExtras fills reactions (from viewerID's point of view) and the
// resolved state on comments.
func AttachCommentExtras(comments []TaskComment, viewerID int) error {
	if len(comments) == 0 {
		return nil
	}
	ids := make([]int, len(comments))
	idx := map[int]int{}
	for i, c := range comments {
		ids[i] = c.ID
		idx[c.ID] = i
		comments[i].Reactions = []CommentReaction{}
	}
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	ctx := context.Background()

	rows, err := pool.Query(ctx, `
		SELECT c.id, c.resolved_at, COALESCE(c.resolved_by_user_id, 0),
		       COALESCE((SELECT COALESCE(NULLIF(u.user_name, ''), u.email) FROM users u WHERE u.id = c.resolved_by_user_id), '')
		FROM task_comments c WHERE c.id = ANY($1)`, ids)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id, by int
		var at *time.Time
		var name string
		if err := rows.Scan(&id, &at, &by, &name); err != nil {
			rows.Close()
			return err
		}
		c := &comments[idx[id]]
		c.ResolvedAt, c.ResolvedByUserID, c.ResolvedByName = at, by, name
	}
	rows.Close()

	rows, err = pool.Query(ctx, `
		SELECT r.comment_id, r.code, COUNT(*), BOOL_OR(r.user_id = $2),
		       (ARRAY_AGG(COALESCE(NULLIF(u.user_name, ''), u.email) ORDER BY r.created_at))[1:10]
		FROM task_comment_reactions r JOIN users u ON u.id = r.user_id
		WHERE r.comment_id = ANY($1)
		GROUP BY r.comment_id, r.code`, ids, viewerID)
	if err != nil {
		return err
	}
	defer rows.Close()
	byComment := map[int]map[string]CommentReaction{}
	for rows.Next() {
		var id int
		var r CommentReaction
		if err := rows.Scan(&id, &r.Code, &r.Count, &r.Mine, &r.Names); err != nil {
			return err
		}
		if byComment[id] == nil {
			byComment[id] = map[string]CommentReaction{}
		}
		byComment[id][r.Code] = r
	}
	for id, m := range byComment {
		c := &comments[idx[id]]
		for _, code := range CommentReactionCodes {
			if r, ok := m[code]; ok {
				c.Reactions = append(c.Reactions, r)
			}
		}
	}
	return rows.Err()
}
