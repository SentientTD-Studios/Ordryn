package domain

import (
	"context"
	"testing"
	"time"

	"GoTodo/internal/storage"
)

func TestTimesheetAndWorkloadScopes(t *testing.T) {
	ctx := context.Background()
	owner := setupCompanySite(t, "gotodo_reports_test")
	ana := createCompanyAccount(t, owner, "ana", 0)
	ben := createCompanyAccount(t, owner, "ben", 0)

	proj, err := CreateProject(ctx, owner, "Billing", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SetProjectWorkflowMode(ctx, owner, proj.ID, storage.WorkflowKanban); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"ana", "ben"} {
		if err := ShareProjectForUser(ctx, owner, proj.ID, ProjectShareInput{Username: name, Level: storage.AccessFull}); err != nil {
			t.Fatalf("share with %s: %v", name, err)
		}
	}
	pid := proj.ID
	task, err := CreateTask(ctx, owner, CreateTaskInput{Title: "Invoices", ProjectID: &pid})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AddTimeEntryForUser(ctx, ana, task, 90, "drafting"); err != nil {
		t.Fatal(err)
	}
	if _, err := AddTimeEntryForUser(ctx, ben, task, 30, "review"); err != nil {
		t.Fatal(err)
	}
	if err := ClaimTaskForUser(ctx, ana, task); err != nil {
		t.Fatal(err)
	}

	today := time.Now().UTC().Format("2006-01-02")
	in := TimesheetInput{From: today, To: today, TZ: "UTC"}

	// The admin sees everyone's time; Ana (no View team workload) only her own.
	all, err := TimesheetForUser(ctx, owner, in)
	if err != nil {
		t.Fatal(err)
	}
	if all.TotalMinutes != 120 || len(all.Entries) != 2 {
		t.Fatalf("owner timesheet: %d minutes, %d entries", all.TotalMinutes, len(all.Entries))
	}
	mine, err := TimesheetForUser(ctx, ana, in)
	if err != nil {
		t.Fatal(err)
	}
	if mine.TotalMinutes != 90 || len(mine.Entries) != 1 || mine.Entries[0].UserID != ana {
		t.Fatalf("ana timesheet: %+v", mine)
	}

	bad := in
	bad.From = "2020-01-01"
	if _, err := TimesheetForUser(ctx, owner, bad); err == nil {
		t.Fatal("ranges over a year should be refused")
	}

	rows, projects, err := WorkloadForUser(ctx, owner, "UTC")
	if err != nil {
		t.Fatal(err)
	}
	if projects == 0 {
		t.Fatal("the admin should see workload on the project")
	}
	var anaRow *storage.WorkloadRow
	for i := range rows {
		if rows[i].UserID == ana {
			anaRow = &rows[i]
		}
	}
	if anaRow == nil || anaRow.ClaimedOpen != 1 || anaRow.MinutesWeek != 90 {
		t.Fatalf("ana workload: %+v", anaRow)
	}
	if _, n, _ := WorkloadForUser(ctx, ana, "UTC"); n != 0 {
		t.Fatalf("ana can't see team workload, got %d projects", n)
	}
}
