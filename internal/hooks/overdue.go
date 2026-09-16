package hooks

import (
	"log"
	"time"

	"GoTodo/internal/storage"
)

// StartOverdueHookWorker notifies once per overdue and due-soon project task per due date.
func StartOverdueHookWorker() {
	go func() {
		runDueHookPasses()
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			runDueHookPasses()
		}
	}()
}

func runDueHookPasses() {
	if !HasWork() {
		return
	}
	runDueHookPass(EventTaskDueSoon, storage.ListDueSoonHookTasks, storage.MarkDueSoonHookSent, "due-soon")
	runDueHookPass(EventTaskOverdue, storage.ListOverdueHookTasks, storage.MarkOverdueHookSent, "overdue")
	runSprintHookPass(EventSprintStarted)
	runSprintHookPass(EventSprintEnded)
}

func runSprintHookPass(eventType string) {
	rows, err := storage.ListSprintLifecycleHooks(eventType, 200)
	if err != nil {
		log.Printf("hooks: sprint %s list: %v", eventType, err)
		return
	}
	for _, row := range rows {
		ok, err := storage.TryMarkSprintHookSent(row.SprintID, eventType)
		if err != nil || !ok {
			continue
		}
		Dispatch(Event{
			Type:       eventType,
			ProjectID:  row.ProjectID,
			SprintID:   row.SprintID,
			SprintName: row.Name,
			Changed:    []string{"sprint"},
		})
	}
}

func runDueHookPass(eventType string, list func(int) ([]int, error), mark func(int) error, label string) {
	ids, err := list(200)
	if err != nil {
		log.Printf("hooks: %s list: %v", label, err)
		return
	}
	for _, id := range ids {
		ownerID, projectID, err := storage.TaskOwnerAndProject(id)
		if err != nil {
			continue
		}
		Dispatch(Event{
			Type:      eventType,
			TaskID:    id,
			ProjectID: projectID,
			OwnerID:   ownerID,
			Changed:   []string{"due_date"},
		})
		if err := mark(id); err != nil {
			log.Printf("hooks: %s mark task=%d: %v", label, id, err)
		}
	}
}
