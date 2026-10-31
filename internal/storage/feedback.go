package storage

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// Feedback board limits (shared by domain validation and the schema).
const (
	MaxFeedbackBoardTitleLen = 120
	MaxFeedbackBoardIntroLen = 2000
	MaxFeedbackPostTitleLen  = 140
	MaxFeedbackPostBodyLen   = 4000
	MaxFeedbackAuthorLen     = 60
)

// Feedback post moderation states.
const (
	FeedbackStatePending  = "pending"
	FeedbackStateApproved = "approved"
	FeedbackStateRejected = "rejected"
)

// ErrFeedbackNotFound is returned when a board or post does not exist.
var ErrFeedbackNotFound = errors.New("feedback not found")

// FeedbackBoard is a password-gated public feedback board for one project.
type FeedbackBoard struct {
	ID              int
	ProjectID       int
	Slug            string
	Title           string
	Intro           string
	PasswordHash    string
	PasswordVersion int
	Enabled         bool
	SubmissionsOpen bool
	CreatedBy       *int
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// FeedbackPost is a visitor submission. PublicStatus is derived from the
// linked task (see feedbackPostSelect) and is empty for unlinked posts.
type FeedbackPost struct {
	ID          int
	BoardID     int
	Title       string
	Body        string
	AuthorName  string
	State       string
	TaskID      *int
	VisitorHash string
	VoteCount   int
	CreatedAt   time.Time
	ModeratedAt *time.Time
	ModeratedBy *int
	// Derived from the linked task, for status display.
	TaskCompleted  bool
	TaskStatusDone bool
	TaskStatusDef  bool
	TaskHasStatus  bool
	TaskTrashed    bool
	// VotedByViewer is filled per request for public listings.
	VotedByViewer bool
}

// CreateFeedbackTables creates feedback board, post, and vote tables.
func CreateFeedbackTables() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	stmts := []string{
		`CREATE TABLE IF NOT EXISTS feedback_boards (
			id SERIAL PRIMARY KEY,
			project_id INTEGER NOT NULL UNIQUE REFERENCES projects(id) ON DELETE CASCADE,
			slug VARCHAR(64) NOT NULL UNIQUE,
			title VARCHAR(120) NOT NULL,
			intro TEXT NOT NULL DEFAULT '',
			password_hash TEXT NOT NULL,
			password_version INTEGER NOT NULL DEFAULT 1,
			enabled BOOLEAN NOT NULL DEFAULT TRUE,
			submissions_open BOOLEAN NOT NULL DEFAULT TRUE,
			created_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS feedback_posts (
			id SERIAL PRIMARY KEY,
			board_id INTEGER NOT NULL REFERENCES feedback_boards(id) ON DELETE CASCADE,
			title VARCHAR(140) NOT NULL,
			body TEXT NOT NULL DEFAULT '',
			author_name VARCHAR(60) NOT NULL DEFAULT '',
			state VARCHAR(16) NOT NULL DEFAULT 'pending',
			task_id INTEGER REFERENCES tasks(id) ON DELETE SET NULL,
			visitor_hash VARCHAR(64) NOT NULL,
			vote_count INTEGER NOT NULL DEFAULT 0,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			moderated_at TIMESTAMPTZ,
			moderated_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
			CHECK (state IN ('pending', 'approved', 'rejected'))
		)`,
		`CREATE INDEX IF NOT EXISTS idx_feedback_posts_board_state ON feedback_posts(board_id, state, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_feedback_posts_task ON feedback_posts(task_id) WHERE task_id IS NOT NULL`,
		`CREATE TABLE IF NOT EXISTS feedback_votes (
			post_id INTEGER NOT NULL REFERENCES feedback_posts(id) ON DELETE CASCADE,
			visitor_hash VARCHAR(64) NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (post_id, visitor_hash)
		)`,
	}
	for _, stmt := range stmts {
		if _, err := pool.Exec(context.Background(), stmt); err != nil {
			return err
		}
	}
	return nil
}

// NewFeedbackSlug returns an unguessable URL slug (128 bits, base64url).
func NewFeedbackSlug() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

const feedbackBoardCols = `id, project_id, slug, title, intro, password_hash, password_version,
	enabled, submissions_open, created_by, created_at, updated_at`

func scanFeedbackBoard(row pgx.Row) (*FeedbackBoard, error) {
	var b FeedbackBoard
	err := row.Scan(&b.ID, &b.ProjectID, &b.Slug, &b.Title, &b.Intro, &b.PasswordHash, &b.PasswordVersion,
		&b.Enabled, &b.SubmissionsOpen, &b.CreatedBy, &b.CreatedAt, &b.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrFeedbackNotFound
	}
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// GetFeedbackBoardByProject loads the board for a project.
func GetFeedbackBoardByProject(projectID int) (*FeedbackBoard, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	return scanFeedbackBoard(pool.QueryRow(context.Background(),
		`SELECT `+feedbackBoardCols+` FROM feedback_boards WHERE project_id = $1`, projectID))
}

// GetFeedbackBoardBySlug loads a board by its public slug. Boards whose project
// is archived are treated as missing.
func GetFeedbackBoardBySlug(slug string) (*FeedbackBoard, error) {
	if slug == "" {
		return nil, ErrFeedbackNotFound
	}
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	return scanFeedbackBoard(pool.QueryRow(context.Background(),
		`SELECT b.id, b.project_id, b.slug, b.title, b.intro, b.password_hash, b.password_version,
			b.enabled, b.submissions_open, b.created_by, b.created_at, b.updated_at
		 FROM feedback_boards b
		 JOIN projects p ON p.id = b.project_id
		 WHERE b.slug = $1 AND COALESCE(p.archived, FALSE) = FALSE`, slug))
}

// CreateFeedbackBoard inserts a board for a project.
func CreateFeedbackBoard(projectID, createdBy int, slug, title, intro, passwordHash string, enabled, submissionsOpen bool) (*FeedbackBoard, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	return scanFeedbackBoard(pool.QueryRow(context.Background(),
		`INSERT INTO feedback_boards (project_id, slug, title, intro, password_hash, enabled, submissions_open, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 RETURNING `+feedbackBoardCols,
		projectID, slug, title, intro, passwordHash, enabled, submissionsOpen, createdBy))
}

// UpdateFeedbackBoard saves settings. A non-empty passwordHash replaces the
// password and bumps password_version, which invalidates every visitor cookie.
func UpdateFeedbackBoard(boardID int, title, intro string, enabled, submissionsOpen bool, passwordHash string) (*FeedbackBoard, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	return scanFeedbackBoard(pool.QueryRow(context.Background(),
		`UPDATE feedback_boards SET
			title = $2, intro = $3, enabled = $4, submissions_open = $5,
			password_hash = CASE WHEN $6 = '' THEN password_hash ELSE $6 END,
			password_version = CASE WHEN $6 = '' THEN password_version ELSE password_version + 1 END,
			updated_at = NOW()
		 WHERE id = $1
		 RETURNING `+feedbackBoardCols,
		boardID, title, intro, enabled, submissionsOpen, passwordHash))
}

// RotateFeedbackBoardSlug replaces the public slug (old links stop working)
// and bumps password_version so existing visitor cookies are invalidated.
func RotateFeedbackBoardSlug(boardID int, slug string) (*FeedbackBoard, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	return scanFeedbackBoard(pool.QueryRow(context.Background(),
		`UPDATE feedback_boards SET slug = $2, password_version = password_version + 1, updated_at = NOW()
		 WHERE id = $1 RETURNING `+feedbackBoardCols, boardID, slug))
}

// DeleteFeedbackBoard removes a board with its posts and votes.
func DeleteFeedbackBoard(boardID int) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	_, err = pool.Exec(context.Background(), `DELETE FROM feedback_boards WHERE id = $1`, boardID)
	return err
}

// feedbackPostSelect joins the linked task so status can be derived without
// exposing internal column names to the public page.
const feedbackPostSelect = `SELECT fp.id, fp.board_id, fp.title, fp.body, fp.author_name, fp.state, fp.task_id,
	fp.visitor_hash, fp.vote_count, fp.created_at, fp.moderated_at, fp.moderated_by,
	COALESCE(t.completed, FALSE),
	COALESCE(ps.is_done, FALSE),
	COALESCE(ps.is_default, FALSE),
	ps.id IS NOT NULL,
	t.deleted_at IS NOT NULL
	FROM feedback_posts fp
	LEFT JOIN tasks t ON t.id = fp.task_id
	LEFT JOIN project_statuses ps ON ps.id = t.status_id`

func scanFeedbackPost(row pgx.Row) (*FeedbackPost, error) {
	var p FeedbackPost
	err := row.Scan(&p.ID, &p.BoardID, &p.Title, &p.Body, &p.AuthorName, &p.State, &p.TaskID,
		&p.VisitorHash, &p.VoteCount, &p.CreatedAt, &p.ModeratedAt, &p.ModeratedBy,
		&p.TaskCompleted, &p.TaskStatusDone, &p.TaskStatusDef, &p.TaskHasStatus, &p.TaskTrashed)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrFeedbackNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func queryFeedbackPosts(sql string, args ...any) ([]FeedbackPost, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	rows, err := pool.Query(context.Background(), sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FeedbackPost{}
	for rows.Next() {
		p, err := scanFeedbackPost(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

// ListFeedbackPosts returns posts on a board in one moderation state.
// Approved posts are ordered by votes; others newest first.
func ListFeedbackPosts(boardID int, state string, limit int) ([]FeedbackPost, error) {
	if limit <= 0 || limit > 500 {
		limit = 500
	}
	order := `fp.created_at DESC, fp.id DESC`
	if state == FeedbackStateApproved {
		order = `fp.vote_count DESC, fp.created_at DESC, fp.id DESC`
	}
	return queryFeedbackPosts(feedbackPostSelect+`
		WHERE fp.board_id = $1 AND fp.state = $2
		ORDER BY `+order+` LIMIT $3`, boardID, state, limit)
}

// ListFeedbackPostsByVisitor returns a visitor's own posts in one state.
func ListFeedbackPostsByVisitor(boardID int, visitorHash, state string, limit int) ([]FeedbackPost, error) {
	return queryFeedbackPosts(feedbackPostSelect+`
		WHERE fp.board_id = $1 AND fp.visitor_hash = $2 AND fp.state = $3
		ORDER BY fp.created_at DESC, fp.id DESC LIMIT $4`, boardID, visitorHash, state, limit)
}

// GetFeedbackPost loads one post on a board.
func GetFeedbackPost(boardID, postID int) (*FeedbackPost, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	return scanFeedbackPost(pool.QueryRow(context.Background(),
		feedbackPostSelect+` WHERE fp.board_id = $1 AND fp.id = $2`, boardID, postID))
}

// CountFeedbackPosts returns how many posts on a board are in a state.
func CountFeedbackPosts(boardID int, state string) (int, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return 0, err
	}
	defer CloseDatabase(pool)
	var n int
	err = pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM feedback_posts WHERE board_id = $1 AND state = $2`, boardID, state).Scan(&n)
	return n, err
}

// CreateFeedbackPost inserts a pending submission.
func CreateFeedbackPost(boardID int, title, body, authorName, visitorHash string) (*FeedbackPost, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	var id int
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO feedback_posts (board_id, title, body, author_name, visitor_hash)
		 VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		boardID, title, body, authorName, visitorHash).Scan(&id); err != nil {
		return nil, err
	}
	return GetFeedbackPost(boardID, id)
}

// SetFeedbackPostState moderates a post.
func SetFeedbackPostState(boardID, postID int, state string, moderatorID int) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	tag, err := pool.Exec(context.Background(),
		`UPDATE feedback_posts SET state = $3, moderated_at = NOW(), moderated_by = $4
		 WHERE board_id = $1 AND id = $2`, boardID, postID, state, moderatorID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrFeedbackNotFound
	}
	return nil
}

// LinkFeedbackPostTask links a post to a task and approves it.
func LinkFeedbackPostTask(boardID, postID, taskID, moderatorID int) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	tag, err := pool.Exec(context.Background(),
		`UPDATE feedback_posts SET task_id = $3, state = 'approved', moderated_at = NOW(), moderated_by = $4
		 WHERE board_id = $1 AND id = $2`, boardID, postID, taskID, moderatorID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrFeedbackNotFound
	}
	return nil
}

// DeleteFeedbackPost removes a post and its votes.
func DeleteFeedbackPost(boardID, postID int) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	tag, err := pool.Exec(context.Background(),
		`DELETE FROM feedback_posts WHERE board_id = $1 AND id = $2`, boardID, postID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrFeedbackNotFound
	}
	return nil
}

// SetFeedbackVote adds or removes a visitor's vote on an approved post and
// returns the new vote count. Duplicate votes and missing votes are no-ops.
func SetFeedbackVote(boardID, postID int, visitorHash string, voted bool) (int, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return 0, err
	}
	defer CloseDatabase(pool)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	var state string
	if err := tx.QueryRow(ctx,
		`SELECT state FROM feedback_posts WHERE board_id = $1 AND id = $2 FOR UPDATE`,
		boardID, postID).Scan(&state); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, ErrFeedbackNotFound
		}
		return 0, err
	}
	if state != FeedbackStateApproved {
		return 0, ErrFeedbackNotFound
	}

	var delta int
	if voted {
		tag, err := tx.Exec(ctx,
			`INSERT INTO feedback_votes (post_id, visitor_hash) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
			postID, visitorHash)
		if err != nil {
			return 0, err
		}
		delta = int(tag.RowsAffected())
	} else {
		tag, err := tx.Exec(ctx,
			`DELETE FROM feedback_votes WHERE post_id = $1 AND visitor_hash = $2`, postID, visitorHash)
		if err != nil {
			return 0, err
		}
		delta = -int(tag.RowsAffected())
	}
	var count int
	if err := tx.QueryRow(ctx,
		`UPDATE feedback_posts SET vote_count = GREATEST(vote_count + $2, 0) WHERE id = $1 RETURNING vote_count`,
		postID, delta).Scan(&count); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return count, nil
}

// FeedbackVotedPostIDs returns which of the given posts the visitor voted on.
func FeedbackVotedPostIDs(visitorHash string, postIDs []int) (map[int]bool, error) {
	out := map[int]bool{}
	if len(postIDs) == 0 {
		return out, nil
	}
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	rows, err := pool.Query(context.Background(),
		`SELECT post_id FROM feedback_votes WHERE visitor_hash = $1 AND post_id = ANY($2)`,
		visitorHash, postIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}
