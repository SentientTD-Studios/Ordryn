package domain

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"GoTodo/internal/storage"
)

func strp(s string) *string { return &s }
func boolp(b bool) *bool    { return &b }

func newFeedbackTestBoard(t *testing.T, name string) (int, *storage.FeedbackBoard) {
	t.Helper()
	ctx := context.Background()
	proj, err := CreateProject(ctx, 1, name, "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	s, err := SaveFeedbackBoard(ctx, 1, proj.ID, FeedbackBoardInput{Password: strp("playtest-123")})
	if err != nil {
		t.Fatalf("create board: %v", err)
	}
	return proj.ID, s.Board
}

// submitFeedback submits with a form token old enough to pass the minimum age.
func submitFeedback(t *testing.T, board *storage.FeedbackBoard, visitorID, title string) *storage.FeedbackPost {
	t.Helper()
	now := time.Now()
	tok, err := IssueFeedbackFormToken(board.ID, visitorID, now.Add(-10*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	p, err := SubmitFeedbackPost(board, visitorID, FeedbackSubmission{Title: title, Body: "details", FormToken: tok}, now)
	if err != nil {
		t.Fatalf("submit %q: %v", title, err)
	}
	return p
}

func TestFeedbackBoardRequiresPasswordAndManager(t *testing.T) {
	ctx := context.Background()
	proj, err := CreateProject(ctx, 1, "Feedback Pw Proj", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SaveFeedbackBoard(ctx, 1, proj.ID, FeedbackBoardInput{Title: strp("No password")}); !errors.Is(err, ErrValidation) {
		t.Fatalf("create without password: err=%v want validation", err)
	}
	if _, err := SaveFeedbackBoard(ctx, 1, proj.ID, FeedbackBoardInput{Password: strp("12345")}); !errors.Is(err, ErrValidation) {
		t.Fatalf("short password: err=%v want validation", err)
	}

	pool, err := storage.OpenDatabase()
	if err != nil {
		t.Fatal(err)
	}
	defer storage.CloseDatabase(pool)
	if _, err := pool.Exec(ctx, `INSERT INTO users (id, email, password, role_id) VALUES (9301, 'outsider-fb@example.com', 'x', 1) ON CONFLICT DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveFeedbackBoard(ctx, 9301, proj.ID, FeedbackBoardInput{Password: strp("playtest-123")}); !errors.Is(err, ErrNotFound) && !errors.Is(err, ErrForbidden) {
		t.Fatalf("non-member create: err=%v want not found/forbidden", err)
	}
	if s, err := GetFeedbackBoardForManager(ctx, 1, proj.ID); err != nil || s != nil {
		t.Fatalf("board should not exist yet: s=%v err=%v", s, err)
	}
}

func TestFeedbackUnlockAndVisitorToken(t *testing.T) {
	ctx := context.Background()
	pid, board := newFeedbackTestBoard(t, "Feedback Unlock Proj")
	now := time.Now()

	if _, err := UnlockFeedbackBoard(board, "wrong-password", "", now); !errors.Is(err, ErrForbidden) {
		t.Fatalf("wrong password: err=%v", err)
	}
	tok, err := UnlockFeedbackBoard(board, "playtest-123", "", now)
	if err != nil {
		t.Fatalf("unlock: %v", err)
	}
	visitor, ok := VerifyFeedbackVisitorToken(board, tok, now)
	if !ok || visitor == "" {
		t.Fatal("fresh token should verify")
	}
	if _, ok := VerifyFeedbackVisitorToken(board, tok, now.Add(FeedbackVisitorTTL+time.Hour)); ok {
		t.Fatal("expired token should not verify")
	}
	tampered := tok[:len(tok)-2] + "xx"
	if _, ok := VerifyFeedbackVisitorToken(board, tampered, now); ok {
		t.Fatal("tampered token should not verify")
	}
	other := *board
	other.ID = board.ID + 1000
	if _, ok := VerifyFeedbackVisitorToken(&other, tok, now); ok {
		t.Fatal("token must be bound to its board")
	}

	// Changing the password signs everyone out, but re-unlocking keeps the visitor id.
	s, err := SaveFeedbackBoard(ctx, 1, pid, FeedbackBoardInput{Password: strp("new-password-9")})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := VerifyFeedbackVisitorToken(s.Board, tok, now); ok {
		t.Fatal("password change should invalidate old tokens")
	}
	prior := FeedbackVisitorIDFromToken(s.Board.ID, tok)
	if prior != visitor {
		t.Fatalf("prior visitor id = %q want %q", prior, visitor)
	}
	tok2, err := UnlockFeedbackBoard(s.Board, "new-password-9", prior, now)
	if err != nil {
		t.Fatal(err)
	}
	if v2, ok := VerifyFeedbackVisitorToken(s.Board, tok2, now); !ok || v2 != visitor {
		t.Fatalf("re-unlock should keep visitor id: %q ok=%v", v2, ok)
	}

	// Rotating the link also invalidates tokens and changes the slug.
	rotated, err := RotateFeedbackBoardLink(ctx, 1, pid)
	if err != nil {
		t.Fatal(err)
	}
	if rotated.Slug == s.Board.Slug {
		t.Fatal("slug should change")
	}
	if _, err := LoadPublicFeedbackBoard(s.Board.Slug); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old slug should be gone: %v", err)
	}
	if _, ok := VerifyFeedbackVisitorToken(rotated, tok2, now); ok {
		t.Fatal("rotation should invalidate tokens")
	}
}

func TestFeedbackSubmissionSpamChecks(t *testing.T) {
	ctx := context.Background()
	pid, board := newFeedbackTestBoard(t, "Feedback Spam Proj")
	visitor := "visitor-spam-1"
	now := time.Now()

	fresh, _ := IssueFeedbackFormToken(board.ID, visitor, now)
	if _, err := SubmitFeedbackPost(board, visitor, FeedbackSubmission{Title: "Too fast", FormToken: fresh}, now); !errors.Is(err, ErrValidation) {
		t.Fatalf("instant submit: err=%v want validation", err)
	}
	old, _ := IssueFeedbackFormToken(board.ID, visitor, now.Add(-FeedbackFormMaxAge-time.Minute))
	if _, err := SubmitFeedbackPost(board, visitor, FeedbackSubmission{Title: "Stale form", FormToken: old}, now); !errors.Is(err, ErrValidation) {
		t.Fatalf("stale form: err=%v want validation", err)
	}
	ok, _ := IssueFeedbackFormToken(board.ID, visitor, now.Add(-10*time.Second))
	if _, err := SubmitFeedbackPost(board, "someone-else", FeedbackSubmission{Title: "Borrowed form", FormToken: ok}, now); !errors.Is(err, ErrValidation) {
		t.Fatalf("form token for another visitor: err=%v want validation", err)
	}
	if _, err := SubmitFeedbackPost(board, visitor, FeedbackSubmission{Title: "Bot", Website: "http://spam", FormToken: ok}, now); !errors.Is(err, ErrFeedbackHoneypot) {
		t.Fatalf("honeypot: err=%v", err)
	}
	links := strings.Repeat("https://spam.example ", MaxFeedbackLinksPerPost+1)
	if _, err := SubmitFeedbackPost(board, visitor, FeedbackSubmission{Title: "Links", Body: links, FormToken: ok}, now); !errors.Is(err, ErrValidation) {
		t.Fatalf("link stuffing: err=%v", err)
	}
	if _, err := SubmitFeedbackPost(board, visitor, FeedbackSubmission{Title: "x", FormToken: ok}, now); !errors.Is(err, ErrValidation) {
		t.Fatalf("short title: err=%v", err)
	}

	p, err := SubmitFeedbackPost(board, visitor, FeedbackSubmission{
		Title: "  Boss\nstagger \x00 feels off ", Body: "line one\r\nline two\x07", AuthorName: "Tester", FormToken: ok,
	}, now)
	if err != nil {
		t.Fatalf("valid submit: %v", err)
	}
	if p.Title != "Boss stagger feels off" || p.Body != "line one\nline two" || p.State != storage.FeedbackStatePending {
		t.Fatalf("unexpected post: %+v", p)
	}

	// Closing submissions blocks new posts.
	s, err := SaveFeedbackBoard(ctx, 1, pid, FeedbackBoardInput{SubmissionsOpen: boolp(false)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SubmitFeedbackPost(s.Board, visitor, FeedbackSubmission{Title: "Closed board", FormToken: ok}, now); !errors.Is(err, ErrForbidden) {
		t.Fatalf("closed board: err=%v want forbidden", err)
	}

	// Disabled boards are invisible.
	if _, err := SaveFeedbackBoard(ctx, 1, pid, FeedbackBoardInput{Enabled: boolp(false)}); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPublicFeedbackBoard(board.Slug); !errors.Is(err, ErrNotFound) {
		t.Fatalf("disabled board: err=%v want not found", err)
	}
}

func TestFeedbackModerationVotesAndStatus(t *testing.T) {
	ctx := context.Background()
	pid, board := newFeedbackTestBoard(t, "Feedback Flow Proj")
	now := time.Now()

	a := submitFeedback(t, board, "visitor-a", "Add a map screen")
	b := submitFeedback(t, board, "visitor-a", "Dodge roll is too slow")

	// Pending posts are hidden from the public but listed for their author.
	view, err := GetPublicFeedbackView(board, "visitor-a", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Posts) != 0 || len(view.MyPending) != 2 {
		t.Fatalf("pending visibility: posts=%d mine=%d", len(view.Posts), len(view.MyPending))
	}
	if view, _ := GetPublicFeedbackView(board, "visitor-b", now); len(view.MyPending) != 0 {
		t.Fatal("other visitors must not see someone else's pending posts")
	}
	if _, err := VoteFeedbackPost(board, "visitor-b", a.ID, true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("vote on pending: err=%v want not found", err)
	}

	if _, err := ModerateFeedbackPost(ctx, 1, pid, a.ID, storage.FeedbackStateApproved); err != nil {
		t.Fatal(err)
	}
	if _, err := ModerateFeedbackPost(ctx, 1, pid, b.ID, storage.FeedbackStateRejected); err != nil {
		t.Fatal(err)
	}

	// Votes dedupe per visitor and can be withdrawn.
	for i := 0; i < 2; i++ {
		if n, err := VoteFeedbackPost(board, "visitor-b", a.ID, true); err != nil || n != 1 {
			t.Fatalf("vote %d: n=%d err=%v", i, n, err)
		}
	}
	if n, _ := VoteFeedbackPost(board, "visitor-c", a.ID, true); n != 2 {
		t.Fatalf("second voter: n=%d", n)
	}
	if n, _ := VoteFeedbackPost(board, "visitor-c", a.ID, false); n != 1 {
		t.Fatalf("unvote: n=%d", n)
	}
	if n, _ := VoteFeedbackPost(board, "visitor-c", a.ID, false); n != 1 {
		t.Fatalf("double unvote must not go below real count: n=%d", n)
	}

	view, err = GetPublicFeedbackView(board, "visitor-b", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Posts) != 1 || view.Posts[0].ID != a.ID || !view.Posts[0].VotedByViewer {
		t.Fatalf("public view: %+v", view.Posts)
	}
	if got := PublicFeedbackStatus(&view.Posts[0]); got != FeedbackStatusUnderReview {
		t.Fatalf("unlinked status = %q", got)
	}

	// Promoting creates a task in the project and links it.
	post, taskID, err := PromoteFeedbackPost(ctx, 1, pid, a.ID, nil)
	if err != nil {
		t.Fatalf("promote: %v", err)
	}
	if post.TaskID == nil || *post.TaskID != taskID {
		t.Fatalf("post not linked: %+v", post)
	}
	_, taskProject, err := storage.TaskOwnerAndProject(taskID)
	if err != nil || taskProject != pid {
		t.Fatalf("task project = %d err=%v want %d", taskProject, err, pid)
	}
	if got := PublicFeedbackStatus(post); got != FeedbackStatusPlanned {
		t.Fatalf("promoted status = %q want planned", got)
	}
	if _, _, err := PromoteFeedbackPost(ctx, 1, pid, a.ID, nil); !errors.Is(err, ErrConflict) {
		t.Fatalf("double promote: err=%v want conflict", err)
	}

	if err := SetTaskCompleted(ctx, 1, taskID, true); err != nil {
		t.Fatal(err)
	}
	post, err = storage.GetFeedbackPost(board.ID, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := PublicFeedbackStatus(post); got != FeedbackStatusDone {
		t.Fatalf("completed task status = %q want done", got)
	}

	// Linking to a task in another project is refused.
	otherProj, err := CreateProject(ctx, 1, "Feedback Other Proj", "")
	if err != nil {
		t.Fatal(err)
	}
	opid := otherProj.ID
	foreign := mustCreateTask(t, 1, CreateTaskInput{Title: "Elsewhere", ProjectID: &opid})
	if _, err := LinkFeedbackPostToTask(ctx, 1, pid, b.ID, foreign); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-project link: err=%v want not found", err)
	}
}
