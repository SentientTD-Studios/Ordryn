package domain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"GoTodo/internal/live"
	"GoTodo/internal/recurrence"
	"GoTodo/internal/storage"
)

// RecurrenceUndoWindow is how long after spawning the next occurrence a
// reopen of the completed task rolls the spawn back (accidental check-off).
const RecurrenceUndoWindow = 15 * time.Minute

// recurrenceNow is swappable in tests.
var recurrenceNow = time.Now

// RecurrenceInput is the client-facing rule payload.
type RecurrenceInput struct {
	Frequency string
	Interval  int
	Weekdays  []int
	// MonthDay: 0 = derive from the task's due date (or today).
	MonthDay int
	Basis    string
	EndsOn   string
	EndAfter int
}

// TaskRecurrenceView is a rule plus derived display data.
type TaskRecurrenceView struct {
	Rule       recurrence.Rule
	Occurrence int
	SeriesID   int
	Summary    string
	// NextDue previews the due date the next occurrence would get if the task
	// were completed today ("" when the series would end).
	NextDue string
}

// TaskRecurrenceDetail is the rule (if active) plus series history for a task.
type TaskRecurrenceDetail struct {
	Rule         *TaskRecurrenceView
	SeriesID     int
	PrevTaskID   int
	NextTaskID   int
	History      []storage.RecurrenceSeriesItem
	CanEdit      bool
	IsSubtask    bool
	HasDueDate   bool
	TaskComplete bool
}

// RuleFromStorage converts a stored rule into the pure recurrence.Rule.
func RuleFromStorage(r storage.TaskRecurrence) recurrence.Rule {
	return recurrence.Rule{
		Frequency: r.Frequency,
		Interval:  r.Interval,
		Weekdays:  recurrence.WeekdaysFromMask(r.WeekdaysMask),
		MonthDay:  r.MonthDay,
		Basis:     r.Basis,
		EndsOn:    r.EndsOn,
		EndAfter:  r.EndAfter,
	}
}

// ViewForRecurrence builds the display view for a stored rule.
func ViewForRecurrence(r storage.TaskRecurrence, dueDate, timezone string) TaskRecurrenceView {
	rule := RuleFromStorage(r)
	v := TaskRecurrenceView{
		Rule:       rule,
		Occurrence: r.Occurrence,
		SeriesID:   r.SeriesID,
		Summary:    rule.Summary(),
	}
	if rule.EndAfter > 0 && r.Occurrence >= rule.EndAfter {
		return v
	}
	if next, ok, err := rule.Next(dueDate, recurrence.TodayIn(timezone, recurrenceNow())); err == nil && ok {
		v.NextDue = next.Format(recurrence.DateLayout)
	}
	return v
}

// normalizeRecurrenceInput validates a rule and pins weekday / month-day
// anchors from the task's due date so the cadence stays stable over time.
func normalizeRecurrenceInput(in RecurrenceInput, dueDate, timezone string) (recurrence.Rule, error) {
	rule := recurrence.Rule{
		Frequency: in.Frequency,
		Interval:  in.Interval,
		Weekdays:  in.Weekdays,
		MonthDay:  in.MonthDay,
		Basis:     in.Basis,
		EndsOn:    in.EndsOn,
		EndAfter:  in.EndAfter,
	}
	rule.Normalize()
	if err := rule.Validate(); err != nil {
		return rule, fmt.Errorf("%w: %s", ErrValidation, strings.TrimPrefix(err.Error(), recurrence.ErrInvalid.Error()+": "))
	}
	anchor := recurrence.TodayIn(timezone, recurrenceNow())
	if strings.TrimSpace(dueDate) != "" {
		if d, err := time.Parse(recurrence.DateLayout, strings.TrimSpace(dueDate)); err == nil {
			anchor = d
		}
	}
	if rule.Frequency == recurrence.Weekly && len(rule.Weekdays) == 0 {
		rule.Weekdays = []int{int(anchor.Weekday())}
	}
	if rule.Frequency == recurrence.Monthly && rule.MonthDay == 0 {
		rule.MonthDay = anchor.Day()
	}
	return rule, nil
}

func userTimezone(userID int) string {
	pool, err := storage.OpenDatabase()
	if err != nil {
		return "UTC"
	}
	defer storage.CloseDatabase(pool)
	var tz string
	if err := pool.QueryRow(context.Background(),
		`SELECT COALESCE(NULLIF(timezone, ''), 'UTC') FROM users WHERE id = $1`, userID).Scan(&tz); err != nil {
		return "UTC"
	}
	return tz
}

// SetTaskRecurrence attaches, replaces, or (in == nil) clears a task's repeat rule.
func SetTaskRecurrence(ctx context.Context, userID, taskID int, in *RecurrenceInput) (*storage.TaskRecurrence, error) {
	_ = ctx
	if err := requireTaskPermOnTask(taskID, userID, storage.PermTasksEdit); err != nil {
		return nil, err
	}
	src, err := storage.GetTaskRecurrenceSource(taskID)
	if err != nil {
		return nil, ErrNotFound
	}
	existing, err := storage.GetTaskRecurrence(taskID)
	if err != nil {
		return nil, err
	}

	if in == nil {
		removed, err := storage.DeleteTaskRecurrence(taskID)
		if err != nil {
			return nil, err
		}
		if removed {
			_ = storage.LogTaskEvent(taskID, userID, "recurrence_cleared", nil)
			live.AfterTaskChangeLive(userID, taskID, live.TypeTaskUpdated)
		}
		return nil, nil
	}

	if src.ParentID > 0 {
		return nil, fmt.Errorf("%w: subtasks cannot repeat; set the rule on the parent task", ErrValidation)
	}
	rule, err := normalizeRecurrenceInput(*in, src.DueDate, userTimezone(userID))
	if err != nil {
		return nil, err
	}

	rec := storage.TaskRecurrence{
		TaskID:       taskID,
		Frequency:    rule.Frequency,
		Interval:     rule.Interval,
		WeekdaysMask: rule.WeekdayMask(),
		MonthDay:     rule.MonthDay,
		Basis:        rule.Basis,
		EndsOn:       rule.EndsOn,
		EndAfter:     rule.EndAfter,
		Occurrence:   1,
		SeriesID:     taskID,
		CreatedBy:    userID,
	}
	if existing != nil {
		rec.Occurrence = existing.Occurrence
		rec.SeriesID = existing.SeriesID
		if existing.CreatedBy > 0 {
			rec.CreatedBy = existing.CreatedBy
		}
	} else if seriesID, _, err := storage.GetTaskRecurrenceLinks(taskID); err == nil && seriesID > 0 {
		// Re-enabling a series that was cleared earlier keeps its history.
		rec.SeriesID = seriesID
	}
	if err := storage.SaveTaskRecurrence(rec); err != nil {
		return nil, err
	}
	_ = storage.LogTaskEvent(taskID, userID, "recurrence_set", map[string]interface{}{"summary": rule.Summary()})
	live.AfterTaskChangeLive(userID, taskID, live.TypeTaskUpdated)
	return storage.GetTaskRecurrence(taskID)
}

// GetTaskRecurrenceDetail returns the active rule and series history for a readable task.
func GetTaskRecurrenceDetail(ctx context.Context, userID, taskID int) (*TaskRecurrenceDetail, error) {
	_ = ctx
	canRead, writeRole, projectID, err := storage.CanUserAccessTask(taskID, userID)
	if err != nil {
		return nil, err
	}
	if !canRead {
		return nil, ErrNotFound
	}
	src, err := storage.GetTaskRecurrenceSource(taskID)
	if err != nil {
		return nil, ErrNotFound
	}
	pool, err := storage.OpenDatabase()
	if err != nil {
		return nil, err
	}
	var completed bool
	_ = pool.QueryRow(context.Background(), `SELECT COALESCE(completed,false) FROM tasks WHERE id = $1`, taskID).Scan(&completed)
	storage.CloseDatabase(pool)

	out := &TaskRecurrenceDetail{
		CanEdit:      denyMissingTaskPerm(projectID, writeRole, storage.PermTasksEdit) == nil,
		IsSubtask:    src.ParentID > 0,
		HasDueDate:   src.DueDate != "",
		TaskComplete: completed,
	}
	rec, err := storage.GetTaskRecurrence(taskID)
	if err != nil {
		return nil, err
	}
	if rec != nil {
		view := ViewForRecurrence(*rec, src.DueDate, userTimezone(userID))
		out.Rule = &view
		out.SeriesID = rec.SeriesID
	}
	seriesID, prevID, err := storage.GetTaskRecurrenceLinks(taskID)
	if err == nil {
		if out.SeriesID == 0 {
			out.SeriesID = seriesID
		}
		out.PrevTaskID = prevID
	}
	if succ, err := storage.FindRecurrenceSuccessor(taskID, RecurrenceUndoWindow); err == nil && succ != nil {
		if ok, _, _, _ := storage.CanUserAccessTask(succ.TaskID, userID); ok {
			out.NextTaskID = succ.TaskID
		}
	}
	if out.SeriesID > 0 {
		out.History, _ = storage.ListRecurrenceSeries(out.SeriesID, userID, 25)
	}
	return out, nil
}

// afterCompletionChanged runs the side effects of a task's completed flag
// flipping: outbound hooks, then recurrence (spawn on complete, roll back an
// accidental spawn on reopen). Every completion path funnels through here.
func afterCompletionChanged(userID, taskID int, completed bool) {
	dispatchCompletedHook(userID, taskID, completed)
	notifyWatchersCompletion(userID, taskID, completed)
	if completed {
		if _, err := SpawnNextOccurrence(context.Background(), userID, taskID); err != nil {
			log.Printf("recurrence: spawn after completing task %d: %v", taskID, err)
		}
		return
	}
	if err := undoRecentRecurrenceSpawn(userID, taskID); err != nil {
		log.Printf("recurrence: undo spawn after reopening task %d: %v", taskID, err)
	}
}

// SpawnNextOccurrence creates the next task in a recurring series after
// taskID was completed. It returns the new task id, or 0 when the task does
// not recur or its series has ended. Safe to call more than once: the rule is
// claimed atomically so only one caller spawns.
func SpawnNextOccurrence(ctx context.Context, actorID, taskID int) (int, error) {
	rec, err := storage.ClaimTaskRecurrence(taskID)
	if err != nil || rec == nil {
		return 0, err
	}
	restore := func() {
		if err := storage.SaveTaskRecurrence(*rec); err != nil {
			log.Printf("recurrence: restore rule on task %d: %v", taskID, err)
		}
	}

	src, err := storage.GetTaskRecurrenceSource(taskID)
	if err != nil {
		restore()
		return 0, err
	}
	rule := RuleFromStorage(*rec)

	if rule.EndAfter > 0 && rec.Occurrence >= rule.EndAfter {
		_ = storage.LogTaskEvent(taskID, actorID, "recurrence_ended", map[string]interface{}{"reason": "count", "occurrence": rec.Occurrence})
		live.AfterTaskChangeLive(actorID, taskID, live.TypeTaskUpdated)
		return 0, nil
	}
	tz := userTimezone(src.OwnerID)
	next, ok, err := rule.Next(src.DueDate, recurrence.TodayIn(tz, recurrenceNow()))
	if err != nil {
		restore()
		return 0, err
	}
	if !ok {
		_ = storage.LogTaskEvent(taskID, actorID, "recurrence_ended", map[string]interface{}{"reason": "ends_on", "ends_on": rule.EndsOn})
		live.AfterTaskChangeLive(actorID, taskID, live.TypeTaskUpdated)
		return 0, nil
	}
	nextDue := next.Format(recurrence.DateLayout)

	in := CreateTaskInput{
		Title:          src.Title,
		Description:    src.Description,
		DueDate:        nextDue,
		Priority:       src.Priority,
		EstimatePoints: nil,
		TagIDs:         copyableTagIDs(taskID),
		Fields:         copyableFieldValues(taskID),
	}
	if src.ProjectID > 0 {
		pid := src.ProjectID
		in.ProjectID = &pid
		if mode, err := storage.GetProjectWorkflowMode(src.ProjectID); err == nil && mode == storage.WorkflowKanban {
			in.EstimatePoints = src.EstimatePoints
		}
	}

	// Create as the task's owner so the series survives whoever completes it;
	// fall back to the completer when the owner can no longer add tasks.
	creator := src.OwnerID
	newID, err := CreateTask(ctx, creator, in)
	if err != nil && actorID > 0 && actorID != creator {
		creator = actorID
		newID, err = CreateTask(ctx, creator, in)
	}
	if err != nil {
		restore()
		_ = storage.LogTaskEvent(taskID, actorID, "recurrence_failed", map[string]interface{}{"error": err.Error()})
		return 0, err
	}

	if err := storage.LinkRecurrenceTask(newID, rec.SeriesID, taskID); err != nil {
		log.Printf("recurrence: link task %d to series %d: %v", newID, rec.SeriesID, err)
	}
	nextRec := *rec
	nextRec.TaskID = newID
	nextRec.Occurrence = rec.Occurrence + 1
	if err := storage.SaveTaskRecurrence(nextRec); err != nil {
		log.Printf("recurrence: attach rule to task %d: %v", newID, err)
	}
	copySubtasksForOccurrence(ctx, creator, taskID, newID)
	if err := storage.CopyTaskWatchers(taskID, newID); err != nil {
		log.Printf("recurrence: copy watchers %d -> %d: %v", taskID, newID, err)
	}
	if src.ClaimedBy > 0 && src.ProjectID > 0 {
		claimer := src.ClaimedBy
		if canRead, _, _, err := storage.CanUserAccessTask(newID, claimer); err == nil && canRead {
			_ = storage.SetTaskClaimedBy(newID, &claimer)
		}
	}

	_ = storage.LogTaskEvent(taskID, actorID, "recurrence_next", map[string]interface{}{
		"next_id": newID, "due_date": nextDue, "occurrence": nextRec.Occurrence,
	})
	_ = storage.LogTaskEvent(newID, actorID, "recurrence_created", map[string]interface{}{
		"from_id": taskID, "occurrence": nextRec.Occurrence,
	})
	// CreateTask already announced the task; announce again now that the rule,
	// subtasks and claim are attached so open clients pick them up.
	live.AfterTaskChangeLive(actorID, newID, live.TypeTaskUpdated)
	live.AfterTaskChangeLive(actorID, taskID, live.TypeTaskUpdated)
	return newID, nil
}

// undoRecentRecurrenceSpawn rolls back the occurrence spawned from taskID when
// taskID is reopened shortly after completion and the new occurrence is still
// untouched (not completed, no finished subtasks, no comments).
func undoRecentRecurrenceSpawn(actorID, taskID int) error {
	if rec, err := storage.GetTaskRecurrence(taskID); err != nil || rec != nil {
		return err // still has its own rule: nothing was spawned
	}
	succ, err := storage.FindRecurrenceSuccessor(taskID, RecurrenceUndoWindow)
	if err != nil || succ == nil {
		return err
	}
	if !succ.Recent || succ.Completed || succ.ChildProgress || succ.CommentCount > 0 {
		return nil
	}
	rec, err := storage.ClaimTaskRecurrence(succ.TaskID)
	if err != nil || rec == nil {
		return err
	}
	rec.TaskID = taskID
	if rec.Occurrence > 1 {
		rec.Occurrence--
	}
	if err := storage.SaveTaskRecurrence(*rec); err != nil {
		return err
	}
	live.AfterTaskChange(actorID, succ.TaskID, live.TypeTaskDeleted)
	if err := storage.DeleteTaskByID(succ.TaskID); err != nil {
		return err
	}
	_ = storage.LogTaskEvent(taskID, actorID, "recurrence_undone", map[string]interface{}{"removed_id": succ.TaskID})
	live.AfterTaskChangeLive(actorID, taskID, live.TypeTaskUpdated)
	return nil
}

// dropRecurrenceForSubtask clears a rule when a task becomes a subtask.
func dropRecurrenceForSubtask(userID, taskID int) {
	removed, err := storage.DeleteTaskRecurrence(taskID)
	if err != nil {
		log.Printf("recurrence: clear rule on nested task %d: %v", taskID, err)
		return
	}
	if removed {
		_ = storage.LogTaskEvent(taskID, userID, "recurrence_cleared", map[string]interface{}{"reason": "nested"})
	}
}

func copyableTagIDs(taskID int) []int {
	tags, err := storage.GetTagsForTask(taskID)
	if err != nil {
		return nil
	}
	ids := make([]int, 0, len(tags))
	for _, t := range tags {
		if t.Protected || storage.IsSystemTagName(t.Name) {
			continue
		}
		ids = append(ids, t.ID)
	}
	return ids
}

func copyableFieldValues(taskID int) map[string]json.RawMessage {
	values, err := storage.GetCustomFieldValuesForTasks([]int{taskID})
	if err != nil || len(values[taskID]) == 0 {
		return nil
	}
	return values[taskID]
}

func copySubtasksForOccurrence(ctx context.Context, creatorID, fromParent, toParent int) {
	children, err := storage.ListRecurrenceChildSources(fromParent)
	if err != nil {
		log.Printf("recurrence: list subtasks of %d: %v", fromParent, err)
		return
	}
	for _, c := range children {
		parent := toParent
		in := CreateTaskInput{
			Title:       c.Title,
			Description: c.Description,
			Priority:    c.Priority,
			ParentID:    &parent,
			TagIDs:      copyableTagIDs(c.TaskID),
		}
		if _, err := CreateTask(ctx, creatorID, in); err != nil {
			if !errors.Is(err, ErrValidation) {
				log.Printf("recurrence: copy subtask %d to %d: %v", c.TaskID, toParent, err)
			}
		}
	}
}
