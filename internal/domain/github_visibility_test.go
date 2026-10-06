package domain

import (
	"context"
	"testing"

	"GoTodo/internal/storage"
)

func TestGetProjectGitHubRepoForUserOmitsWebhookSecretForMembers(t *testing.T) {
	ctx := context.Background()
	proj, err := CreateProject(ctx, 1, "GH Visibility", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := upsertTestMember(t, proj.ID, 2, testRoleEditor); err != nil {
		t.Fatalf("add editor: %v", err)
	}

	const secret = "webhook-secret-visibility"
	if _, err := storage.UpsertProjectGitHubRepo(proj.ID, 1, "acme", "widgets", 42, secret); err != nil {
		t.Fatalf("link repo: %v", err)
	}

	ownerView, err := GetProjectGitHubRepoForUser(ctx, 1, proj.ID)
	if err != nil {
		t.Fatalf("owner get: %v", err)
	}
	if !ownerView.Linked || ownerView.FullName != "acme/widgets" {
		t.Fatalf("owner view = %+v", ownerView)
	}
	if ownerView.HTMLURL == "" {
		t.Fatal("owner missing html_url")
	}
	if ownerView.WebhookSecret != secret {
		t.Fatalf("owner webhook_secret=%q want %q", ownerView.WebhookSecret, secret)
	}

	editorView, err := GetProjectGitHubRepoForUser(ctx, 2, proj.ID)
	if err != nil {
		t.Fatalf("editor get: %v", err)
	}
	if !editorView.Linked || editorView.FullName != "acme/widgets" || editorView.HTMLURL == "" {
		t.Fatalf("editor should see repo link, got %+v", editorView)
	}
	if editorView.WebhookSecret != "" {
		t.Fatalf("editor webhook_secret=%q, want empty", editorView.WebhookSecret)
	}
}
