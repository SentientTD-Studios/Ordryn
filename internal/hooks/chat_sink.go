package hooks

import (
	"log"
	"strconv"
	"time"

	"GoTodo/internal/storage"

	"github.com/google/uuid"
)

// Ordryn Chat project links: chat servers allowed "task events" under Admin →
// Chat servers can link a channel to a project, and the chat sink queues that
// project's activity for them (read with GET /api/v2/chat/events).

// chatSkipEvents are left out of chat: noisy, or nothing a channel needs.
var chatSkipEvents = map[string]bool{
	EventTaskReordered:         true,
	EventTaskMentioned:         true, // the comment itself arrives as task.commented
	EventTaskCommentEdited:     true,
	EventTaskCommentDeleted:    true,
	EventTaskCommentRestored:   true,
	EventProjectInviteSent:     true,
	EventProjectInviteDeclined: true,
	EventImportCompleted:       true,
}

// ChatEventPayload is what a chat server receives for one project event.
type ChatEventPayload struct {
	EventID     string `json:"event_id"`
	Type        string `json:"type"`
	ProjectID   int    `json:"project_id"`
	ProjectName string `json:"project_name"`
	TaskID      int    `json:"task_id,omitempty"`
	Title       string `json:"title,omitempty"`
	Status      string `json:"status,omitempty"`
	OldStatus   string `json:"old_status,omitempty"`
	Actor       string `json:"actor,omitempty"`
	ActorID     int    `json:"actor_id,omitempty"`
	Comment     string `json:"comment,omitempty"`
	URL         string `json:"url,omitempty"`
	DueDate     string `json:"due_date,omitempty"`
	ClaimedBy   string `json:"claimed_by,omitempty"`
	Priority    string `json:"priority,omitempty"`
	Sprint      string `json:"sprint,omitempty"`
	Member      string `json:"member,omitempty"`
	Changed     string `json:"changed,omitempty"`
	OccurredAt  string `json:"occurred_at"`
}

// StartChatSink registers the chat sink with the event stream.
func StartChatSink() {
	RegisterSink(Sink{
		Name:   "chat",
		Active: func() bool { return storage.CountChatLinks() > 0 },
		Handle: HandleChatEvent,
	})
}

// HandleChatEvent queues ev for every chat link to its project whose linker
// can still manage the project; links whose linker lost access are paused.
func HandleChatEvent(ev Event) {
	if chatSkipEvents[ev.Type] || ev.isSiteEvent() {
		return
	}
	snap, err := snapshotFor(ev)
	if err != nil || snap == nil {
		return
	}
	projectID := ev.ProjectID
	if projectID <= 0 {
		projectID = snap.ProjectID
	}
	if projectID <= 0 {
		return // personal tasks never go to chat
	}
	targets, err := storage.ChatLinksForProject(projectID)
	if err != nil {
		log.Printf("hooks: chat links for project %d: %v", projectID, err)
		return
	}
	if len(targets) == 0 {
		return
	}
	if ev.EventID == "" {
		ev.EventID = uuid.NewString()
	}
	if ev.OccurredAt.IsZero() {
		ev.OccurredAt = time.Now().UTC()
	}
	vars := eventVars(ev, snap, resolveActor(ev))
	taskID := 0
	if !ev.isProjectLevel() {
		taskID, _ = strconv.Atoi(vars["id"])
	}
	payload := ChatEventPayload{
		EventID: ev.EventID, Type: ev.Type, ProjectID: projectID, ProjectName: snap.ProjectName,
		TaskID: taskID, Status: vars["status"], OldStatus: vars["old_status"],
		Actor: vars["actor"], ActorID: ev.ActorID, Comment: vars["comment"], URL: vars["url"],
		DueDate: vars["due_date"], ClaimedBy: vars["claimed_by"], Priority: vars["priority"],
		Sprint: vars["sprint"], Member: vars["member"], Changed: vars["changed"], OccurredAt: vars["occurred_at"],
	}
	if taskID > 0 {
		payload.Title = vars["task"]
	}

	allowed := map[int]string{} // linker → reason ("" = allowed)
	for _, t := range targets {
		reason, seen := allowed[t.UserID]
		if !seen {
			_, reason, err = storage.ChatLinkCheck(t.UserID, projectID)
			if err != nil {
				log.Printf("hooks: chat link check: %v", err)
				continue
			}
			allowed[t.UserID] = reason
		}
		if reason != "" {
			if t.Active {
				if err := storage.SetChatLinkState(t.ChatServerID, t.LinkID, false, reason); err != nil {
					log.Printf("hooks: pause chat link: %v", err)
				}
			}
			continue
		}
		if !t.Active {
			_ = storage.SetChatLinkState(t.ChatServerID, t.LinkID, true, "")
		}
		if err := storage.InsertChatEvent(t.ChatServerID, t.LinkID, payload); err != nil {
			log.Printf("hooks: queue chat event: %v", err)
		}
	}
}
