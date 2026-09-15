package hooks

import (
	"log"
	"time"

	"GoTodo/internal/storage"
)

// StartOverdueHookWorker notifies once per overdue project task per due date.
func StartOverdueHookWorker() {
	go func() {
		runOverduePass()
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			runOverduePass()
		}
	}()
}

func runOverduePass() {
	if !HasWork() {
		return
	}
	ids, err := storage.ListOverdueHookTasks(200)
	if err != nil {
		log.Printf("hooks: overdue list: %v", err)
		return
	}
	for _, id := range ids {
		ownerID, projectID, err := storage.TaskOwnerAndProject(id)
		if err != nil {
			continue
		}
		Dispatch(Event{
			Type:      EventTaskOverdue,
			TaskID:    id,
			ProjectID: projectID,
			OwnerID:   ownerID,
			Changed:   []string{"due_date"},
		})
		if err := storage.MarkOverdueHookSent(id); err != nil {
			log.Printf("hooks: overdue mark task=%d: %v", id, err)
		}
	}
}
