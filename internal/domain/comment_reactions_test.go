package domain

import (
	"context"
	"errors"
	"testing"
)

func TestCommentReactionsAndResolve(t *testing.T) {
	ctx := context.Background()
	proj, err := CreateProject(ctx, 1, "Reactions "+uniqueHandle("p"), "")
	if err != nil {
		t.Fatal(err)
	}
	if err := upsertTestMember(t, proj.ID, 2, testRoleEditor); err != nil {
		t.Fatal(err)
	}
	if err := upsertTestMember(t, proj.ID, 3, testRoleViewer); err != nil {
		t.Fatal(err)
	}
	pid := proj.ID
	taskID, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Question", ProjectID: &pid})
	if err != nil {
		t.Fatal(err)
	}
	q, err := AddCommentForUser(ctx, 1, taskID, "Which API version?")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := ReactToCommentForUser(ctx, 2, taskID, q.ID, "nope", true); !errors.Is(err, ErrValidation) {
		t.Fatalf("unknown reaction: %v", err)
	}
	for _, u := range []int{2, 3} {
		if _, err := ReactToCommentForUser(ctx, u, taskID, q.ID, "+1", true); err != nil {
			t.Fatalf("react as %d: %v", u, err)
		}
	}
	// Reacting twice is a no-op.
	c, err := ReactToCommentForUser(ctx, 2, taskID, q.ID, "+1", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Reactions) != 1 || c.Reactions[0].Count != 2 || !c.Reactions[0].Mine {
		t.Fatalf("reactions: %+v", c.Reactions)
	}
	c, err = ReactToCommentForUser(ctx, 2, taskID, q.ID, "+1", false)
	if err != nil || c.Reactions[0].Count != 1 || c.Reactions[0].Mine {
		t.Fatalf("after removing: %+v %v", c.Reactions, err)
	}

	// The viewer (not the author, can't edit tasks) can't resolve; the editor can.
	if _, err := ResolveCommentForUser(ctx, 3, taskID, q.ID, true); !errors.Is(err, ErrForbidden) {
		t.Fatalf("viewer resolve: %v", err)
	}
	c, err = ResolveCommentForUser(ctx, 2, taskID, q.ID, true)
	if err != nil || c.ResolvedAt == nil || c.ResolvedByUserID != 2 {
		t.Fatalf("resolve: %+v %v", c, err)
	}
	list, err := ListCommentsForUser(ctx, 3, taskID)
	if err != nil || len(list) != 1 || list[0].ResolvedAt == nil || len(list[0].Reactions) != 1 {
		t.Fatalf("list: %+v %v", list, err)
	}
	c, err = ResolveCommentForUser(ctx, 1, taskID, q.ID, false)
	if err != nil || c.ResolvedAt != nil {
		t.Fatalf("reopen: %+v %v", c, err)
	}
}
