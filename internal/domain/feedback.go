package domain

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"GoTodo/internal/config"
	"GoTodo/internal/storage"

	"golang.org/x/crypto/bcrypt"
)

// Public feedback boards: a password-gated page where outside people (e.g.
// playtesters) submit feedback into a moderation inbox and vote on approved
// posts. Visitors never get an account; after unlocking a board they hold a
// signed per-board token that identifies their browser for votes and limits.

const (
	MinFeedbackPasswordLen = 6
	MaxFeedbackPasswordLen = 128

	// FeedbackVisitorTTL is how long an unlocked board stays unlocked.
	FeedbackVisitorTTL = 30 * 24 * time.Hour
	// Form tokens must be at least this old when submitted (bots post instantly)
	// and no older than the max (stale forms re-fetch a token).
	FeedbackFormMinAge = 3 * time.Second
	FeedbackFormMaxAge = 6 * time.Hour

	// MaxFeedbackPendingPerBoard caps the moderation inbox so a flood cannot
	// grow it without bound; the manager clears it to reopen submissions.
	MaxFeedbackPendingPerBoard = 300
	// MaxFeedbackLinksPerPost rejects link-stuffed spam.
	MaxFeedbackLinksPerPost = 5
	// MaxFeedbackPublicPosts bounds the public listing.
	MaxFeedbackPublicPosts = 200
)

// Public status values shown to visitors, derived from the linked task.
const (
	FeedbackStatusUnderReview = "under_review"
	FeedbackStatusPlanned     = "planned"
	FeedbackStatusInProgress  = "in_progress"
	FeedbackStatusDone        = "done"
)

var feedbackURLPattern = regexp.MustCompile(`(?i)https?://|www\.`)

// ── Tokens ──────────────────────────────────────────────────────────────────

func feedbackSigningKey() ([]byte, error) {
	key, err := config.SessionKey()
	if err != nil {
		return nil, err
	}
	// Derive a purpose-specific key so feedback tokens can never be replayed
	// as any other signed value.
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte("ordryn-feedback-v1"))
	return mac.Sum(nil), nil
}

func signFeedbackPayload(payload string) (string, error) {
	key, err := feedbackSigningKey()
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." +
		base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func verifyFeedbackPayload(token string) (string, bool) {
	dot := strings.IndexByte(token, '.')
	if dot <= 0 || dot == len(token)-1 || len(token) > 512 {
		return "", false
	}
	payload, err := base64.RawURLEncoding.DecodeString(token[:dot])
	if err != nil {
		return "", false
	}
	sig, err := base64.RawURLEncoding.DecodeString(token[dot+1:])
	if err != nil {
		return "", false
	}
	key, err := feedbackSigningKey()
	if err != nil {
		return "", false
	}
	mac := hmac.New(sha256.New, key)
	mac.Write(payload)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return "", false
	}
	return string(payload), true
}

func newFeedbackVisitorID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// FeedbackVisitorHash is the stored identity for a visitor (never the raw id).
func FeedbackVisitorHash(visitorID string) string {
	sum := sha256.Sum256([]byte("feedback-visitor|" + visitorID))
	return hex.EncodeToString(sum[:])
}

// IssueFeedbackVisitorToken signs a token proving the holder unlocked a board.
// It is bound to the board and its password version, so changing the password
// or rotating the link signs everyone out.
func IssueFeedbackVisitorToken(board *storage.FeedbackBoard, visitorID string, now time.Time) (string, error) {
	payload := fmt.Sprintf("v|%d|%d|%s|%d", board.ID, board.PasswordVersion, visitorID, now.Unix())
	return signFeedbackPayload(payload)
}

// VerifyFeedbackVisitorToken returns the visitor id when the token is valid
// for this board right now.
func VerifyFeedbackVisitorToken(board *storage.FeedbackBoard, token string, now time.Time) (string, bool) {
	payload, ok := verifyFeedbackPayload(token)
	if !ok {
		return "", false
	}
	parts := strings.Split(payload, "|")
	if len(parts) != 5 || parts[0] != "v" {
		return "", false
	}
	boardID, err1 := strconv.Atoi(parts[1])
	pwVersion, err2 := strconv.Atoi(parts[2])
	issued, err3 := strconv.ParseInt(parts[4], 10, 64)
	if err1 != nil || err2 != nil || err3 != nil {
		return "", false
	}
	if boardID != board.ID || pwVersion != board.PasswordVersion || parts[3] == "" {
		return "", false
	}
	issuedAt := time.Unix(issued, 0)
	if now.Sub(issuedAt) > FeedbackVisitorTTL || issuedAt.After(now.Add(time.Minute)) {
		return "", false
	}
	return parts[3], true
}

// FeedbackVisitorIDFromToken returns the visitor id from any correctly signed
// token for this board, ignoring expiry and password version. Used only to
// keep the same visitor identity (and so the same votes) across re-unlocks.
func FeedbackVisitorIDFromToken(boardID int, token string) string {
	payload, ok := verifyFeedbackPayload(token)
	if !ok {
		return ""
	}
	parts := strings.Split(payload, "|")
	if len(parts) != 5 || parts[0] != "v" || parts[1] != strconv.Itoa(boardID) {
		return ""
	}
	return parts[3]
}

// IssueFeedbackFormToken signs a short-lived token embedded in the submit form.
func IssueFeedbackFormToken(boardID int, visitorID string, now time.Time) (string, error) {
	return signFeedbackPayload(fmt.Sprintf("f|%d|%s|%d", boardID, visitorID, now.UnixMilli()))
}

func checkFeedbackFormToken(boardID int, visitorID, token string, now time.Time) error {
	payload, ok := verifyFeedbackPayload(token)
	if !ok {
		return fmt.Errorf("%w: the form expired; reload the page and try again", ErrValidation)
	}
	parts := strings.Split(payload, "|")
	if len(parts) != 4 || parts[0] != "f" {
		return fmt.Errorf("%w: the form expired; reload the page and try again", ErrValidation)
	}
	id, err1 := strconv.Atoi(parts[1])
	issued, err2 := strconv.ParseInt(parts[3], 10, 64)
	if err1 != nil || err2 != nil || id != boardID || parts[2] != visitorID {
		return fmt.Errorf("%w: the form expired; reload the page and try again", ErrValidation)
	}
	age := now.Sub(time.UnixMilli(issued))
	if age < FeedbackFormMinAge {
		return fmt.Errorf("%w: that was quick; wait a moment and send again", ErrValidation)
	}
	if age > FeedbackFormMaxAge {
		return fmt.Errorf("%w: the form expired; reload the page and try again", ErrValidation)
	}
	return nil
}

// ── Validation helpers ──────────────────────────────────────────────────────

// cleanFeedbackLine collapses a single-line field: control characters and
// newlines become spaces, runs of whitespace collapse.
func cleanFeedbackLine(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == ' ' || r == ' ' {
			return ' '
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

// cleanFeedbackBody keeps newlines and tabs but drops other control characters.
func cleanFeedbackBody(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	return strings.TrimSpace(s)
}

func runeLen(s string) int { return len([]rune(s)) }

func validateFeedbackPassword(pw string) error {
	n := runeLen(pw)
	if n < MinFeedbackPasswordLen {
		return fmt.Errorf("%w: password must be at least %d characters", ErrValidation, MinFeedbackPasswordLen)
	}
	if n > MaxFeedbackPasswordLen {
		return fmt.Errorf("%w: password must be %d characters or less", ErrValidation, MaxFeedbackPasswordLen)
	}
	return nil
}

// PublicFeedbackStatus maps a post's linked task onto a coarse public status,
// so internal column names never leak to visitors.
func PublicFeedbackStatus(p *storage.FeedbackPost) string {
	if p.TaskID == nil || p.TaskTrashed {
		return FeedbackStatusUnderReview
	}
	if p.TaskCompleted || p.TaskStatusDone {
		return FeedbackStatusDone
	}
	if p.TaskHasStatus && !p.TaskStatusDef {
		return FeedbackStatusInProgress
	}
	return FeedbackStatusPlanned
}

// ── Manager side ────────────────────────────────────────────────────────────

// FeedbackBoardInput is a partial update; nil fields are left unchanged.
// Password is required when creating a board.
type FeedbackBoardInput struct {
	Title           *string
	Intro           *string
	Enabled         *bool
	SubmissionsOpen *bool
	Password        *string
}

// FeedbackBoardSummary is the manager view of a board.
type FeedbackBoardSummary struct {
	Board        *storage.FeedbackBoard
	PendingCount int
}

// GetFeedbackBoardForManager returns the project's board, or nil when none exists.
func GetFeedbackBoardForManager(ctx context.Context, userID, projectID int) (*FeedbackBoardSummary, error) {
	_ = ctx
	if _, err := requireProjectManage(projectID, userID); err != nil {
		return nil, err
	}
	board, err := storage.GetFeedbackBoardByProject(projectID)
	if errors.Is(err, storage.ErrFeedbackNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	pending, err := storage.CountFeedbackPosts(board.ID, storage.FeedbackStatePending)
	if err != nil {
		return nil, err
	}
	return &FeedbackBoardSummary{Board: board, PendingCount: pending}, nil
}

// SaveFeedbackBoard creates the project's board or updates it.
func SaveFeedbackBoard(ctx context.Context, userID, projectID int, in FeedbackBoardInput) (*FeedbackBoardSummary, error) {
	proj, err := requireProjectManage(projectID, userID)
	if err != nil {
		return nil, err
	}
	existing, err := storage.GetFeedbackBoardByProject(projectID)
	if err != nil && !errors.Is(err, storage.ErrFeedbackNotFound) {
		return nil, err
	}

	title, intro := "", ""
	enabled, open := true, true
	if existing != nil {
		title, intro, enabled, open = existing.Title, existing.Intro, existing.Enabled, existing.SubmissionsOpen
	} else {
		title = proj.Name + " feedback"
	}
	if in.Title != nil {
		title = cleanFeedbackLine(*in.Title)
	}
	if in.Intro != nil {
		intro = cleanFeedbackBody(*in.Intro)
	}
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	if in.SubmissionsOpen != nil {
		open = *in.SubmissionsOpen
	}
	if title == "" {
		return nil, fmt.Errorf("%w: title is required", ErrValidation)
	}
	if runeLen(title) > storage.MaxFeedbackBoardTitleLen {
		return nil, fmt.Errorf("%w: title must be %d characters or less", ErrValidation, storage.MaxFeedbackBoardTitleLen)
	}
	if runeLen(intro) > storage.MaxFeedbackBoardIntroLen {
		return nil, fmt.Errorf("%w: intro must be %d characters or less", ErrValidation, storage.MaxFeedbackBoardIntroLen)
	}

	hash := ""
	if in.Password != nil && *in.Password != "" {
		if err := validateFeedbackPassword(*in.Password); err != nil {
			return nil, err
		}
		h, err := bcrypt.GenerateFromPassword([]byte(*in.Password), bcrypt.DefaultCost)
		if err != nil {
			return nil, err
		}
		hash = string(h)
	}

	var board *storage.FeedbackBoard
	if existing == nil {
		if hash == "" {
			return nil, fmt.Errorf("%w: a password is required for a feedback board", ErrValidation)
		}
		slug, err := storage.NewFeedbackSlug()
		if err != nil {
			return nil, err
		}
		board, err = storage.CreateFeedbackBoard(projectID, userID, slug, title, intro, hash, enabled, open)
		if err != nil {
			return nil, err
		}
		_ = storage.LogProjectEvent(projectID, userID, "feedback_board_created", nil)
	} else {
		board, err = storage.UpdateFeedbackBoard(existing.ID, title, intro, enabled, open, hash)
		if err != nil {
			return nil, err
		}
		if hash != "" {
			_ = storage.LogProjectEvent(projectID, userID, "feedback_password_changed", nil)
		}
		if existing.Enabled != enabled || existing.SubmissionsOpen != open {
			_ = storage.LogProjectEvent(projectID, userID, "feedback_board_updated", map[string]interface{}{
				"enabled": enabled, "submissions_open": open,
			})
		}
	}
	pending, err := storage.CountFeedbackPosts(board.ID, storage.FeedbackStatePending)
	if err != nil {
		return nil, err
	}
	return &FeedbackBoardSummary{Board: board, PendingCount: pending}, nil
}

func requireManagedFeedbackBoard(userID, projectID int) (*storage.FeedbackBoard, error) {
	if _, err := requireProjectManage(projectID, userID); err != nil {
		return nil, err
	}
	board, err := storage.GetFeedbackBoardByProject(projectID)
	if errors.Is(err, storage.ErrFeedbackNotFound) {
		return nil, fmt.Errorf("%w: this project has no feedback board", ErrNotFound)
	}
	return board, err
}

// RotateFeedbackBoardLink issues a new public URL; the old one stops working.
func RotateFeedbackBoardLink(ctx context.Context, userID, projectID int) (*storage.FeedbackBoard, error) {
	_ = ctx
	board, err := requireManagedFeedbackBoard(userID, projectID)
	if err != nil {
		return nil, err
	}
	slug, err := storage.NewFeedbackSlug()
	if err != nil {
		return nil, err
	}
	board, err = storage.RotateFeedbackBoardSlug(board.ID, slug)
	if err != nil {
		return nil, err
	}
	_ = storage.LogProjectEvent(projectID, userID, "feedback_link_rotated", nil)
	return board, nil
}

// DeleteFeedbackBoardForUser removes the board and every submission on it.
func DeleteFeedbackBoardForUser(ctx context.Context, userID, projectID int) error {
	_ = ctx
	board, err := requireManagedFeedbackBoard(userID, projectID)
	if err != nil {
		return err
	}
	if err := storage.DeleteFeedbackBoard(board.ID); err != nil {
		return err
	}
	_ = storage.LogProjectEvent(projectID, userID, "feedback_board_deleted", nil)
	return nil
}

// ListFeedbackPostsForManager lists posts in one moderation state.
func ListFeedbackPostsForManager(ctx context.Context, userID, projectID int, state string) ([]storage.FeedbackPost, error) {
	_ = ctx
	switch state {
	case storage.FeedbackStatePending, storage.FeedbackStateApproved, storage.FeedbackStateRejected:
	default:
		return nil, fmt.Errorf("%w: state must be pending, approved or rejected", ErrValidation)
	}
	board, err := requireManagedFeedbackBoard(userID, projectID)
	if err != nil {
		return nil, err
	}
	return storage.ListFeedbackPosts(board.ID, state, 500)
}

// ModerateFeedbackPost approves or rejects a post.
func ModerateFeedbackPost(ctx context.Context, userID, projectID, postID int, state string) (*storage.FeedbackPost, error) {
	_ = ctx
	if state != storage.FeedbackStateApproved && state != storage.FeedbackStateRejected && state != storage.FeedbackStatePending {
		return nil, fmt.Errorf("%w: invalid state", ErrValidation)
	}
	board, err := requireManagedFeedbackBoard(userID, projectID)
	if err != nil {
		return nil, err
	}
	if err := storage.SetFeedbackPostState(board.ID, postID, state, userID); err != nil {
		return nil, feedbackStorageErr(err)
	}
	p, err := storage.GetFeedbackPost(board.ID, postID)
	return p, feedbackStorageErr(err)
}

// PromoteFeedbackPost creates a task in the project from a post, links them,
// and approves the post so visitors can follow its status.
func PromoteFeedbackPost(ctx context.Context, userID, projectID, postID int, statusID *int) (*storage.FeedbackPost, int, error) {
	board, err := requireManagedFeedbackBoard(userID, projectID)
	if err != nil {
		return nil, 0, err
	}
	post, err := storage.GetFeedbackPost(board.ID, postID)
	if err != nil {
		return nil, 0, feedbackStorageErr(err)
	}
	if post.TaskID != nil && !post.TaskTrashed {
		return nil, 0, fmt.Errorf("%w: this submission is already linked to a task", ErrConflict)
	}
	desc := post.Body
	credit := "Submitted via the feedback board"
	if post.AuthorName != "" {
		credit += " by " + post.AuthorName
	}
	if desc != "" {
		desc += "\n\n---\n"
	}
	desc += "_" + credit + "._"

	pid := projectID
	taskID, err := CreateTask(ctx, userID, CreateTaskInput{
		Title:       post.Title,
		Description: desc,
		ProjectID:   &pid,
		StatusID:    statusID,
	})
	if err != nil {
		return nil, 0, err
	}
	if err := storage.LinkFeedbackPostTask(board.ID, postID, taskID, userID); err != nil {
		return nil, 0, feedbackStorageErr(err)
	}
	_ = storage.LogProjectEvent(projectID, userID, "feedback_post_promoted", map[string]interface{}{
		"task_id": taskID, "post_id": postID,
	})
	post, err = storage.GetFeedbackPost(board.ID, postID)
	return post, taskID, feedbackStorageErr(err)
}

// LinkFeedbackPostToTask links a post to an existing task in the project
// (e.g. a duplicate of something already tracked) and approves it.
func LinkFeedbackPostToTask(ctx context.Context, userID, projectID, postID, taskID int) (*storage.FeedbackPost, error) {
	_ = ctx
	board, err := requireManagedFeedbackBoard(userID, projectID)
	if err != nil {
		return nil, err
	}
	_, taskProject, err := storage.TaskOwnerAndProject(taskID)
	if err != nil || taskProject != projectID {
		return nil, fmt.Errorf("%w: task not found in this project", ErrNotFound)
	}
	if err := storage.LinkFeedbackPostTask(board.ID, postID, taskID, userID); err != nil {
		return nil, feedbackStorageErr(err)
	}
	p, err := storage.GetFeedbackPost(board.ID, postID)
	return p, feedbackStorageErr(err)
}

// DeleteFeedbackPostForUser removes a post permanently.
func DeleteFeedbackPostForUser(ctx context.Context, userID, projectID, postID int) error {
	_ = ctx
	board, err := requireManagedFeedbackBoard(userID, projectID)
	if err != nil {
		return err
	}
	return feedbackStorageErr(storage.DeleteFeedbackPost(board.ID, postID))
}

func feedbackStorageErr(err error) error {
	if errors.Is(err, storage.ErrFeedbackNotFound) {
		return fmt.Errorf("%w: submission not found", ErrNotFound)
	}
	return err
}

// ── Public side ─────────────────────────────────────────────────────────────

// LoadPublicFeedbackBoard returns an enabled board by slug. Disabled boards,
// archived projects, and unknown slugs all look the same: not found.
func LoadPublicFeedbackBoard(slug string) (*storage.FeedbackBoard, error) {
	if slug == "" || len(slug) > 64 {
		return nil, ErrNotFound
	}
	board, err := storage.GetFeedbackBoardBySlug(slug)
	if errors.Is(err, storage.ErrFeedbackNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if !board.Enabled {
		return nil, ErrNotFound
	}
	return board, nil
}

// UnlockFeedbackBoard checks the password and returns a visitor token.
// An existing visitor id is kept so votes survive re-entering the password.
func UnlockFeedbackBoard(board *storage.FeedbackBoard, password, existingVisitorID string, now time.Time) (string, error) {
	if len(password) > MaxFeedbackPasswordLen*4 ||
		bcrypt.CompareHashAndPassword([]byte(board.PasswordHash), []byte(password)) != nil {
		return "", fmt.Errorf("%w: incorrect password", ErrForbidden)
	}
	visitorID := existingVisitorID
	if visitorID == "" {
		id, err := newFeedbackVisitorID()
		if err != nil {
			return "", err
		}
		visitorID = id
	}
	return IssueFeedbackVisitorToken(board, visitorID, now)
}

// PublicFeedbackView is what an unlocked visitor sees.
type PublicFeedbackView struct {
	Board     *storage.FeedbackBoard
	Posts     []storage.FeedbackPost
	MyPending []storage.FeedbackPost
	FormToken string
}

// GetPublicFeedbackView lists approved posts (by votes) and the visitor's own
// posts that are still waiting for review.
func GetPublicFeedbackView(board *storage.FeedbackBoard, visitorID string, now time.Time) (*PublicFeedbackView, error) {
	hash := FeedbackVisitorHash(visitorID)
	posts, err := storage.ListFeedbackPosts(board.ID, storage.FeedbackStateApproved, MaxFeedbackPublicPosts)
	if err != nil {
		return nil, err
	}
	ids := make([]int, 0, len(posts))
	for _, p := range posts {
		ids = append(ids, p.ID)
	}
	voted, err := storage.FeedbackVotedPostIDs(hash, ids)
	if err != nil {
		return nil, err
	}
	for i := range posts {
		posts[i].VotedByViewer = voted[posts[i].ID]
	}
	mine, err := storage.ListFeedbackPostsByVisitor(board.ID, hash, storage.FeedbackStatePending, 20)
	if err != nil {
		return nil, err
	}
	token, err := IssueFeedbackFormToken(board.ID, visitorID, now)
	if err != nil {
		return nil, err
	}
	return &PublicFeedbackView{Board: board, Posts: posts, MyPending: mine, FormToken: token}, nil
}

// FeedbackSubmission is a visitor's new post. Website is a honeypot field
// hidden from people; anything in it marks the request as a bot.
type FeedbackSubmission struct {
	Title      string
	Body       string
	AuthorName string
	Website    string
	FormToken  string
}

// ErrFeedbackHoneypot is returned for bot submissions. Handlers answer it with
// a normal success response so bots get no signal.
var ErrFeedbackHoneypot = errors.New("feedback honeypot")

// SubmitFeedbackPost validates and stores a pending submission.
func SubmitFeedbackPost(board *storage.FeedbackBoard, visitorID string, in FeedbackSubmission, now time.Time) (*storage.FeedbackPost, error) {
	if !board.SubmissionsOpen {
		return nil, fmt.Errorf("%w: this board is not accepting new submissions right now", ErrForbidden)
	}
	if strings.TrimSpace(in.Website) != "" {
		return nil, ErrFeedbackHoneypot
	}
	if err := checkFeedbackFormToken(board.ID, visitorID, in.FormToken, now); err != nil {
		return nil, err
	}
	title := cleanFeedbackLine(in.Title)
	body := cleanFeedbackBody(in.Body)
	author := cleanFeedbackLine(in.AuthorName)
	if runeLen(title) < 3 {
		return nil, fmt.Errorf("%w: add a short title (at least 3 characters)", ErrValidation)
	}
	if runeLen(title) > storage.MaxFeedbackPostTitleLen {
		return nil, fmt.Errorf("%w: title must be %d characters or less", ErrValidation, storage.MaxFeedbackPostTitleLen)
	}
	if runeLen(body) > storage.MaxFeedbackPostBodyLen {
		return nil, fmt.Errorf("%w: details must be %d characters or less", ErrValidation, storage.MaxFeedbackPostBodyLen)
	}
	if runeLen(author) > storage.MaxFeedbackAuthorLen {
		return nil, fmt.Errorf("%w: name must be %d characters or less", ErrValidation, storage.MaxFeedbackAuthorLen)
	}
	if len(feedbackURLPattern.FindAllStringIndex(title+" "+body, -1)) > MaxFeedbackLinksPerPost {
		return nil, fmt.Errorf("%w: too many links; remove some and try again", ErrValidation)
	}
	pending, err := storage.CountFeedbackPosts(board.ID, storage.FeedbackStatePending)
	if err != nil {
		return nil, err
	}
	if pending >= MaxFeedbackPendingPerBoard {
		return nil, fmt.Errorf("%w: this board has too many submissions waiting for review; try again later", ErrForbidden)
	}
	return storage.CreateFeedbackPost(board.ID, title, body, author, FeedbackVisitorHash(visitorID))
}

// VoteFeedbackPost sets the visitor's vote on an approved post.
func VoteFeedbackPost(board *storage.FeedbackBoard, visitorID string, postID int, voted bool) (int, error) {
	n, err := storage.SetFeedbackVote(board.ID, postID, FeedbackVisitorHash(visitorID), voted)
	if errors.Is(err, storage.ErrFeedbackNotFound) {
		return 0, fmt.Errorf("%w: post not found", ErrNotFound)
	}
	return n, err
}
