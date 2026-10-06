package domain

import (
	"context"
	"strconv"
	"testing"
	"time"

	"GoTodo/internal/storage"
)

func TestListShareLinksForUserProjectMembersSeeOwnerLinks(t *testing.T) {
	ctx := context.Background()
	proj, err := CreateProject(ctx, 1, "Share Link Visibility", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := upsertTestMember(t, proj.ID, 2, testRoleEditor); err != nil {
		t.Fatalf("add editor: %v", err)
	}

	link, err := CreateShareLinkForScope(ctx, 1, storage.ShareScopeProject, proj.ID, nil)
	if err != nil {
		t.Fatalf("create link: %v", err)
	}

	ownerLinks, err := ListShareLinksForUser(ctx, 1, storage.ShareScopeProject, proj.ID)
	if err != nil {
		t.Fatalf("owner list: %v", err)
	}
	if len(ownerLinks) == 0 {
		t.Fatal("owner should see share links")
	}

	editorLinks, err := ListShareLinksForUser(ctx, 2, storage.ShareScopeProject, proj.ID)
	if err != nil {
		t.Fatalf("editor list: %v", err)
	}
	found := false
	for _, l := range editorLinks {
		if l.ID == link.ID && l.Token == link.Token {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("editor should see owner's project share link, got %+v", editorLinks)
	}
}

func TestPublicSharePathForTask(t *testing.T) {
	ctx := context.Background()

	t.Run("no share", func(t *testing.T) {
		proj, err := CreateProject(ctx, 1, "Share Path No Link", "")
		if err != nil {
			t.Fatalf("create project: %v", err)
		}
		taskID, err := CreateTask(ctx, 1, CreateTaskInput{Title: "No share task", ProjectID: &proj.ID})
		if err != nil {
			t.Fatalf("create task: %v", err)
		}
		if got := PublicSharePathForTask(taskID); got != "" {
			t.Fatalf("got %q, want empty", got)
		}
	})

	t.Run("active share", func(t *testing.T) {
		proj, err := CreateProject(ctx, 1, "Share Path Active", "")
		if err != nil {
			t.Fatalf("create project: %v", err)
		}
		taskID, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Shared task", ProjectID: &proj.ID})
		if err != nil {
			t.Fatalf("create task: %v", err)
		}
		link, err := CreateShareLinkForScope(ctx, 1, storage.ShareScopeProject, proj.ID, nil)
		if err != nil {
			t.Fatalf("create link: %v", err)
		}
		want := "/s/" + link.Token + "?task=" + strconv.Itoa(taskID)
		if got := PublicSharePathForTask(taskID); got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("expired ignored", func(t *testing.T) {
		proj, err := CreateProject(ctx, 1, "Share Path Expired", "")
		if err != nil {
			t.Fatalf("create project: %v", err)
		}
		taskID, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Expired share task", ProjectID: &proj.ID})
		if err != nil {
			t.Fatalf("create task: %v", err)
		}
		expired := time.Now().Add(-time.Hour)
		if _, err := CreateShareLinkForScope(ctx, 1, storage.ShareScopeProject, proj.ID, &expired); err != nil {
			t.Fatalf("create expired link: %v", err)
		}
		if got := PublicSharePathForTask(taskID); got != "" {
			t.Fatalf("got %q, want empty", got)
		}
	})

	t.Run("revoked ignored", func(t *testing.T) {
		proj, err := CreateProject(ctx, 1, "Share Path Revoked", "")
		if err != nil {
			t.Fatalf("create project: %v", err)
		}
		taskID, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Revoked share task", ProjectID: &proj.ID})
		if err != nil {
			t.Fatalf("create task: %v", err)
		}
		link, err := CreateShareLinkForScope(ctx, 1, storage.ShareScopeProject, proj.ID, nil)
		if err != nil {
			t.Fatalf("create link: %v", err)
		}
		if err := RevokeShareLinkForUser(ctx, 1, link.ID); err != nil {
			t.Fatalf("revoke link: %v", err)
		}
		if got := PublicSharePathForTask(taskID); got != "" {
			t.Fatalf("got %q, want empty", got)
		}
	})

	t.Run("newest active wins", func(t *testing.T) {
		proj, err := CreateProject(ctx, 1, "Share Path Newest", "")
		if err != nil {
			t.Fatalf("create project: %v", err)
		}
		taskID, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Newest share task", ProjectID: &proj.ID})
		if err != nil {
			t.Fatalf("create task: %v", err)
		}
		older, err := CreateShareLinkForScope(ctx, 1, storage.ShareScopeProject, proj.ID, nil)
		if err != nil {
			t.Fatalf("create older link: %v", err)
		}
		newer, err := CreateShareLinkForScope(ctx, 1, storage.ShareScopeProject, proj.ID, nil)
		if err != nil {
			t.Fatalf("create newer link: %v", err)
		}
		want := "/s/" + newer.Token + "?task=" + strconv.Itoa(taskID)
		if got := PublicSharePathForTask(taskID); got != want {
			t.Fatalf("got %q, want %q (older token %s)", got, want, older.Token)
		}
	})
}
