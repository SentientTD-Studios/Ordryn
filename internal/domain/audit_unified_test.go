package domain

import (
	"context"
	"testing"
	"time"

	"GoTodo/internal/mailer"
	"GoTodo/internal/storage"
)

func TestUnifiedAuditListsAllSources(t *testing.T) {
	ctx := context.Background()
	proj, err := CreateProject(ctx, 1, "Unified Audit Proj", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	pid := proj.ID
	taskID, err := CreateTask(ctx, 1, CreateTaskInput{Title: "Audited task", ProjectID: &pid})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	newTitle := "Audited task v2"
	if _, err := UpdateTask(ctx, 1, taskID, UpdateTaskInput{Title: &newTitle}); err != nil {
		t.Fatalf("update task: %v", err)
	}
	c, err := AddCommentForUser(ctx, 1, taskID, "first draft")
	if err != nil {
		t.Fatalf("add comment: %v", err)
	}
	if _, err := EditCommentForUser(ctx, 1, taskID, c.ID, "second draft"); err != nil {
		t.Fatalf("edit comment: %v", err)
	}
	if err := storage.LogProjectEvent(pid, 1, "renamed", map[string]interface{}{"from": "a", "to": "b"}); err != nil {
		t.Fatalf("project event: %v", err)
	}
	if err := storage.LogAdminEvent(1, "user_banned", "user", 3, "user3", map[string]interface{}{"from": "active", "to": "banned"}); err != nil {
		t.Fatalf("admin event: %v", err)
	}
	if err := storage.InsertEmailAudit(mailer.AuditEntry{Trigger: mailer.TriggerPasswordReset, ToEmail: "x@example.com", Status: mailer.StatusSent}); err != nil {
		t.Fatalf("email audit: %v", err)
	}

	items, total, err := storage.ListAuditEvents(ctx, storage.AuditFilter{Limit: 100})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total < len(items) || len(items) == 0 {
		t.Fatalf("total=%d items=%d", total, len(items))
	}
	seen := map[string]bool{}
	for i, ev := range items {
		seen[ev.Source] = true
		if i > 0 && ev.CreatedAt.After(items[i-1].CreatedAt) {
			t.Fatalf("rows not newest-first at %d", i)
		}
	}
	for _, src := range []string{"task", "project", "comment", "email", "admin"} {
		if !seen[src] {
			t.Errorf("missing source %q in unified audit", src)
		}
	}

	// Project filter keeps task, project and comment rows for that project only.
	byProject, _, err := storage.ListAuditEvents(ctx, storage.AuditFilter{ProjectID: pid, Limit: 100})
	if err != nil {
		t.Fatalf("list by project: %v", err)
	}
	var sawTitle, sawComment bool
	for _, ev := range byProject {
		if ev.ProjectID != pid {
			t.Fatalf("project filter leaked %+v", ev)
		}
		if ev.Source == "task" && ev.EventType == "title_changed" {
			sawTitle = ev.Metadata["from"] == "Audited task" && ev.Metadata["to"] == newTitle
		}
		if ev.Source == "comment" && ev.EventType == "comment_edit" {
			sawComment = ev.Metadata["from"] == "first draft" && ev.Metadata["to"] == "second draft"
			if ev.TargetLabel != newTitle || ev.ProjectName != "Unified Audit Proj" {
				t.Errorf("comment row labels = %q / %q", ev.TargetLabel, ev.ProjectName)
			}
		}
	}
	if !sawTitle {
		t.Error("title_changed with from/to not found")
	}
	if !sawComment {
		t.Error("comment_edit with before/after bodies not found")
	}

	// Source + event type + actor filters combine.
	admin, n, err := storage.ListAuditEvents(ctx, storage.AuditFilter{Source: "admin", EventType: "user_banned", UserID: 1, Limit: 10})
	if err != nil {
		t.Fatalf("list admin: %v", err)
	}
	if n < 1 || len(admin) < 1 || admin[0].TargetLabel != "user3" || admin[0].ActorUserID != 1 {
		t.Fatalf("admin rows = %+v (total %d)", admin, n)
	}
	// Email rows have no actor, so a user filter excludes them.
	emails, _, err := storage.ListAuditEvents(ctx, storage.AuditFilter{Source: "email", UserID: 1, Limit: 10})
	if err != nil {
		t.Fatalf("list email: %v", err)
	}
	if len(emails) != 0 {
		t.Fatalf("email rows with user filter = %d, want 0", len(emails))
	}

	future := time.Now().Add(time.Hour)
	none, n, err := storage.ListAuditEvents(ctx, storage.AuditFilter{Since: &future, Limit: 10})
	if err != nil || len(none) != 0 || n != 0 {
		t.Fatalf("future since: rows=%d total=%d err=%v", len(none), n, err)
	}

	exported := 0
	if err := storage.EachAuditEvent(ctx, storage.AuditFilter{ProjectID: pid}, func(storage.AuditEvent) error {
		exported++
		return nil
	}); err != nil {
		t.Fatalf("export: %v", err)
	}
	if exported != len(byProject) {
		t.Fatalf("export rows = %d, list rows = %d", exported, len(byProject))
	}

	types, err := storage.ListAuditEventTypes(ctx)
	if err != nil {
		t.Fatalf("event types: %v", err)
	}
	found := false
	for _, et := range types {
		if et.Source == "comment" && et.EventType == "comment_edit" {
			found = true
		}
	}
	if !found {
		t.Errorf("event types missing comment_edit: %+v", types)
	}
}

func TestPurgeAuditEventsRespectsRetention(t *testing.T) {
	pool, err := storage.OpenDatabase()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := storage.LogAdminEvent(1, "purge_old", "test", 0, "", nil); err != nil {
		t.Fatal(err)
	}
	if err := storage.LogAdminEvent(1, "purge_new", "test", 0, "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE admin_events SET created_at = NOW() - INTERVAL '40 days' WHERE event_type = 'purge_old'`); err != nil {
		t.Fatal(err)
	}

	if n, err := storage.PurgeAuditEvents(0); err != nil || n != 0 {
		t.Fatalf("retention 0 must keep everything: n=%d err=%v", n, err)
	}
	if _, err := storage.PurgeAuditEvents(30); err != nil {
		t.Fatalf("purge: %v", err)
	}
	var oldCount, newCount int
	_ = pool.QueryRow(ctx, `SELECT COUNT(*) FROM admin_events WHERE event_type = 'purge_old'`).Scan(&oldCount)
	_ = pool.QueryRow(ctx, `SELECT COUNT(*) FROM admin_events WHERE event_type = 'purge_new'`).Scan(&newCount)
	if oldCount != 0 || newCount != 1 {
		t.Fatalf("after purge old=%d new=%d, want 0/1", oldCount, newCount)
	}
}
