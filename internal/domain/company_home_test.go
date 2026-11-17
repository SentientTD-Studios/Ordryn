package domain

import (
	"context"
	"testing"
)

func TestCompanyHomeSummaryCountsWorkAndProjects(t *testing.T) {
	ctx := context.Background()
	const today = "2032-03-10"
	proj, err := CreateProject(ctx, 1, "Home Summary", "")
	if err != nil {
		t.Fatal(err)
	}
	pid := proj.ID
	for _, due := range []string{"2032-03-01", "2032-03-10", "2032-03-12", ""} {
		if _, err := CreateTask(ctx, 1, CreateTaskInput{Title: "w " + due, ProjectID: &pid, DueDate: due}); err != nil {
			t.Fatal(err)
		}
	}
	before, err := GetCompanyHomeSummary(ctx, 1, today)
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	var found *CompanyHomeProject
	for i := range before.Projects {
		if before.Projects[i].ID == pid {
			found = &before.Projects[i]
		}
	}
	if found == nil {
		if before.TotalProjects <= CompanyHomeMaxProjects {
			t.Fatalf("project missing: %+v", before.Projects)
		}
	} else if found.Open != 4 || found.Overdue != 1 {
		t.Fatalf("counts=%+v want open 4 overdue 1", *found)
	}
	if before.MyWork.Open < 4 || before.MyWork.Overdue < 1 || before.MyWork.DueToday < 1 || before.MyWork.DueWeek < 2 {
		t.Fatalf("my work=%+v", before.MyWork)
	}
	if before.Teams == nil {
		t.Fatal("teams should be an empty list, not nil")
	}
}
