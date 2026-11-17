package domain

import (
	"context"
	"errors"
	"testing"
	"time"

	"GoTodo/internal/storage"
)

func TestTimeBlocksPlanLogAndSprintTotals(t *testing.T) {
	ctx := context.Background()
	owner := setupCompanySite(t, "gotodo_time_blocks_test")
	ana := createCompanyAccount(t, owner, "ana", 0)
	viv := createCompanyAccount(t, owner, "viv", 0)

	proj, err := CreateProject(ctx, owner, "Launch", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SetProjectWorkflowMode(ctx, owner, proj.ID, storage.WorkflowKanban); err != nil {
		t.Fatal(err)
	}
	if err := ShareProjectForUser(ctx, owner, proj.ID, ProjectShareInput{Username: "ana", Level: storage.AccessFull}); err != nil {
		t.Fatal(err)
	}
	if err := ShareProjectForUser(ctx, owner, proj.ID, ProjectShareInput{Username: "viv", Level: storage.AccessView}); err != nil {
		t.Fatal(err)
	}
	sprint, err := CreateProjectSprintForUser(ctx, owner, proj.ID, CreateProjectSprintInput{Name: "S1"})
	if err != nil {
		t.Fatal(err)
	}
	pid, sid := proj.ID, sprint.ID
	est := 6.0
	task, err := CreateTask(ctx, owner, CreateTaskInput{Title: "Ship it", ProjectID: &pid, SprintID: &sid, EstimateHours: &est})
	if err != nil {
		t.Fatal(err)
	}

	// Two blocks on the same task, the first spanning several hours.
	day := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -1)
	first, err := CreateTimeBlockForUser(ctx, ana, task, TimeBlockSpan{StartAt: day.Add(9 * time.Hour), EndAt: day.Add(11*time.Hour + 30*time.Minute)})
	if err != nil {
		t.Fatalf("create block: %v", err)
	}
	if first.Minutes() != 150 {
		t.Fatalf("first block minutes = %d", first.Minutes())
	}
	second, err := CreateTimeBlockForUser(ctx, ana, task, TimeBlockSpan{StartAt: day.Add(14 * time.Hour), EndAt: day.Add(15 * time.Hour)})
	if err != nil {
		t.Fatalf("create second block: %v", err)
	}

	// Spans are validated.
	for name, span := range map[string]TimeBlockSpan{
		"backwards":  {StartAt: day.Add(10 * time.Hour), EndAt: day.Add(9 * time.Hour)},
		"off grid":   {StartAt: day.Add(9*time.Hour + 5*time.Minute), EndAt: day.Add(10 * time.Hour)},
		"over a day": {StartAt: day, EndAt: day.Add(25 * time.Hour)},
	} {
		if _, err := CreateTimeBlockForUser(ctx, ana, task, span); !errors.Is(err, ErrValidation) {
			t.Fatalf("%s: err = %v, want validation", name, err)
		}
	}

	// Blocks are private: Ana sees hers; the owner sees none of them.
	mine, err := ListTimeBlocksForUser(ctx, ana, day, day.AddDate(0, 0, 1))
	if err != nil || len(mine) != 2 {
		t.Fatalf("ana blocks = %d, %v", len(mine), err)
	}
	if mine[0].TaskTitle != "Ship it" || mine[0].ProjectName != "Launch" {
		t.Fatalf("block task info = %+v", mine[0])
	}
	theirs, err := ListTaskTimeBlocksForUser(ctx, owner, task)
	if err != nil || len(theirs) != 0 {
		t.Fatalf("owner sees %d of ana's blocks, %v", len(theirs), err)
	}
	if _, err := MoveTimeBlockForUser(ctx, owner, first.ID, TimeBlockSpan{StartAt: day, EndAt: day.Add(time.Hour)}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("owner moving ana's block: %v", err)
	}
	if _, err := ListTimeBlocksForUser(ctx, ana, day, day.AddDate(0, 0, MaxTimeBlockRangeDays+1)); !errors.Is(err, ErrValidation) {
		t.Fatalf("long range: %v", err)
	}

	// Resize the second block to two hours.
	moved, err := MoveTimeBlockForUser(ctx, ana, second.ID, TimeBlockSpan{StartAt: day.Add(14 * time.Hour), EndAt: day.Add(16 * time.Hour)})
	if err != nil || moved.Minutes() != 120 {
		t.Fatalf("resize: %+v, %v", moved, err)
	}

	// Logging records the block's length on the day it happened, once.
	logged, entry, err := LogTimeBlockForUser(ctx, ana, first.ID, "deep work")
	if err != nil {
		t.Fatalf("log block: %v", err)
	}
	if entry.Minutes != 150 || !entry.SpentAt.Equal(first.StartAt) || logged.TimeEntryID != entry.ID {
		t.Fatalf("entry = %+v, block = %+v", entry, logged)
	}
	if _, _, err := LogTimeBlockForUser(ctx, ana, first.ID, ""); !errors.Is(err, ErrConflict) {
		t.Fatalf("log twice: %v", err)
	}
	if _, err := MoveTimeBlockForUser(ctx, ana, first.ID, TimeBlockSpan{StartAt: day, EndAt: day.Add(time.Hour)}); !errors.Is(err, ErrConflict) {
		t.Fatalf("move logged block: %v", err)
	}
	if _, _, err := LogTimeBlockForUser(ctx, ana, second.ID, ""); err != nil {
		t.Fatalf("log second: %v", err)
	}

	// A future block can't be logged yet.
	future, err := CreateTimeBlockForUser(ctx, ana, task, TimeBlockSpan{StartAt: day.AddDate(0, 0, 3), EndAt: day.AddDate(0, 0, 3).Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := LogTimeBlockForUser(ctx, ana, future.ID, ""); !errors.Is(err, ErrValidation) {
		t.Fatalf("log future block: %v", err)
	}

	// A viewer can plan time but not log it.
	vb, err := CreateTimeBlockForUser(ctx, viv, task, TimeBlockSpan{StartAt: day.Add(9 * time.Hour), EndAt: day.Add(10 * time.Hour)})
	if err != nil {
		t.Fatalf("viewer block: %v", err)
	}
	if _, _, err := LogTimeBlockForUser(ctx, viv, vb.ID, ""); !errors.Is(err, ErrForbidden) {
		t.Fatalf("viewer log: %v", err)
	}

	// Deleting a block keeps its logged time; deleting the entry frees the block.
	if err := DeleteTimeBlockForUser(ctx, ana, second.ID); err != nil {
		t.Fatal(err)
	}
	if err := DeleteTimeEntryForUser(ctx, ana, task, entry.ID); err != nil {
		t.Fatal(err)
	}
	freed, err := storage.GetTimeBlock(first.ID)
	if err != nil || freed.TimeEntryID != 0 {
		t.Fatalf("block after entry delete: %+v, %v", freed, err)
	}

	// The sprint adds up its tasks' estimates and logged time.
	sprints, err := ListProjectSprintsForUser(ctx, owner, proj.ID)
	if err != nil || len(sprints) != 1 {
		t.Fatalf("sprints: %v, %v", sprints, err)
	}
	if sprints[0].TimeSpentMinutes != 120 || sprints[0].EstimateHours != 6 {
		t.Fatalf("sprint totals = %d min, %g h", sprints[0].TimeSpentMinutes, sprints[0].EstimateHours)
	}

	// Trashed tasks drop off the calendar.
	if err := DeleteTask(ctx, owner, task); err != nil {
		t.Fatal(err)
	}
	mine, err = ListTimeBlocksForUser(ctx, ana, day, day.AddDate(0, 0, 7))
	if err != nil || len(mine) != 0 {
		t.Fatalf("blocks after trash = %d, %v", len(mine), err)
	}
}

func TestTimeEntriesOnPersonalTasks(t *testing.T) {
	ctx := context.Background()
	// Personal tasks need a non-company site: the shared test database.
	owner, other := 1, 2

	task, err := CreateTask(ctx, owner, CreateTaskInput{Title: "Taxes"})
	if err != nil {
		t.Fatal(err)
	}
	// Ten days back keeps other tests' entries out of this timesheet day.
	yesterday := time.Now().Add(-10 * 24 * time.Hour)
	entry, err := AddTimeEntryAtForUser(ctx, owner, task, 45, "receipts", &yesterday)
	if err != nil {
		t.Fatalf("log personal time: %v", err)
	}
	if entry.SpentAt.Sub(yesterday).Abs() > time.Second {
		t.Fatalf("spent_at = %v, want %v", entry.SpentAt, yesterday)
	}
	tomorrow := time.Now().Add(24 * time.Hour)
	if _, err := AddTimeEntryAtForUser(ctx, owner, task, 10, "", &tomorrow); !errors.Is(err, ErrValidation) {
		t.Fatalf("future entry: %v", err)
	}
	if _, err := AddTimeEntryForUser(ctx, other, task, 10, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("someone else's personal task: %v", err)
	}

	// The owner's timesheet includes it on the day it was spent.
	day := yesterday.UTC().Format("2006-01-02")
	ts, err := TimesheetForUser(ctx, owner, TimesheetInput{From: day, To: day, TZ: "UTC"})
	if err != nil {
		t.Fatal(err)
	}
	if ts.TotalMinutes != 45 || len(ts.Entries) != 1 || ts.Entries[0].ProjectID != 0 {
		t.Fatalf("timesheet = %+v", ts)
	}
	other2, err := TimesheetForUser(ctx, other, TimesheetInput{From: day, To: day, TZ: "UTC"})
	if err != nil || other2.TotalMinutes != 0 {
		t.Fatalf("other's timesheet = %+v, %v", other2, err)
	}
}
