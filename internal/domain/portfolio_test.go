package domain

import (
	"context"
	"errors"
	"testing"
	"time"

	"GoTodo/internal/storage"
)

func TestPortfolioAndStatusUpdates(t *testing.T) {
	ctx := context.Background()
	owner := setupCompanySite(t, "gotodo_portfolio_test")
	outsider := createCompanyAccount(t, owner, "outsider", 0)

	proj, err := CreateProject(ctx, owner, "Launch", "")
	if err != nil {
		t.Fatal(err)
	}
	pid := proj.ID
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	if _, err := CreateTask(ctx, owner, CreateTaskInput{Title: "Late", ProjectID: &pid, DueDate: yesterday}); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateTask(ctx, owner, CreateTaskInput{Title: "Open", ProjectID: &pid}); err != nil {
		t.Fatal(err)
	}

	if _, err := PostProjectStatusUpdate(ctx, owner, pid, "great", ""); !errors.Is(err, ErrValidation) {
		t.Fatalf("bad health: %v", err)
	}
	if _, err := PostProjectStatusUpdate(ctx, outsider, pid, storage.HealthAtRisk, "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("outsider: %v", err)
	}
	if _, err := PostProjectStatusUpdate(ctx, owner, pid, storage.HealthOnTrack, "Kickoff done"); err != nil {
		t.Fatal(err)
	}
	if _, err := PostProjectStatusUpdate(ctx, owner, pid, storage.HealthAtRisk, "Vendor slipped"); err != nil {
		t.Fatal(err)
	}

	rows, err := ListPortfolioForUser(ctx, owner, "UTC", false)
	if err != nil {
		t.Fatal(err)
	}
	var row *PortfolioRow
	for i := range rows {
		if rows[i].Project.ID == pid {
			row = &rows[i]
		}
	}
	if row == nil {
		t.Fatal("project missing from portfolio")
	}
	if row.Counts.Open != 2 || row.Counts.Overdue != 1 {
		t.Fatalf("counts: %+v", row.Counts)
	}
	if row.Latest == nil || row.Latest.Health != storage.HealthAtRisk || !row.CanPost {
		t.Fatalf("latest: %+v canPost=%v", row.Latest, row.CanPost)
	}

	list, err := ListProjectStatusUpdatesForUser(ctx, owner, pid, 10)
	if err != nil || len(list) != 2 {
		t.Fatalf("history: %v %v", list, err)
	}
	if err := DeleteProjectStatusUpdateForUser(ctx, owner, pid, list[0].ID); err != nil {
		t.Fatal(err)
	}
	if list, _ := ListProjectStatusUpdatesForUser(ctx, owner, pid, 10); len(list) != 1 {
		t.Fatalf("after delete: %d", len(list))
	}
}
