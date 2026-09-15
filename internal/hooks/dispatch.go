package hooks

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"GoTodo/internal/config"
	"GoTodo/internal/extensions"
	"GoTodo/internal/storage"
)

// Event is a domain change delivered to loaded extensions.
type Event struct {
	Type          string
	TaskID        int
	ProjectID     int
	ActorID       int
	StatusChanged bool
	OldStatus     string
	NewStatus     string
	// Snapshot, when set, skips loading the task (owner Test).
	Snapshot *storage.HookTaskSnapshot
	Actor    string
}

// HasWork reports whether any extension is loaded (skip goroutine in tests).
func HasWork() bool {
	return extensions.LoadedCount() > 0
}

// Dispatch delivers ev to matching extensions. Safe to call from a goroutine.
func Dispatch(ev Event) {
	if ev.TaskID <= 0 && ev.Snapshot == nil {
		return
	}
	for _, entry := range extensions.LoadedEntries() {
		deliverToExtension(entry, ev)
	}
}

func deliverToExtension(entry extensions.Entry, ev Event) {
	if !entry.Loaded {
		return
	}
	site, err := storage.GetExtensionSettings(entry.ID)
	if err != nil {
		log.Printf("hooks: load settings %s: %v", entry.ID, err)
		return
	}
	snap, err := snapshotFor(ev)
	if err != nil {
		log.Printf("hooks: snapshot task=%d: %v", ev.TaskID, err)
		return
	}
	if snap == nil {
		return
	}
	if ev.ProjectID <= 0 {
		ev.ProjectID = snap.ProjectID
	}
	projectID := snap.ProjectID
	if projectID <= 0 {
		return
	}
	project, err := storage.GetExtensionProjectSettings(entry.ID, projectID)
	if err != nil {
		log.Printf("hooks: load project settings %s project=%d: %v", entry.ID, projectID, err)
		return
	}
	if !ShouldDeliver(entry.Manifest, site, project, ev, projectID) {
		return
	}
	tmpl := templateFor(entry.Manifest, project.Templates, ev.Type)
	if strings.TrimSpace(tmpl) == "" {
		return
	}
	actor := strings.TrimSpace(ev.Actor)
	if actor == "" && ev.ActorID > 0 {
		if p, err := storage.GetUserProfileByID(ev.ActorID); err == nil && p != nil {
			actor = p.UserName
			if actor == "" {
				actor = p.Email
			}
		}
	}
	status := snap.StatusName
	if ev.NewStatus != "" {
		status = ev.NewStatus
	}
	if status == "" {
		if snap.Completed {
			status = "Completed"
		} else {
			status = "Open"
		}
	}
	title := truncateTitle(snap.Title)
	vars := map[string]string{
		"task":       title,
		"name":       title,
		"status":     status,
		"old_status": ev.OldStatus,
		"project":    snap.ProjectName,
		"actor":      actor,
		"url":        publicTaskURL(snap.ID),
		"id":         strconv.Itoa(snap.ID),
		"priority":   priorityLabel(snap.Priority),
	}
	msg := Interpolate(tmpl, vars)
	sent, err := deliver(entry.Manifest, msg, projectID, ev.Type, vars)
	if err != nil {
		log.Printf("hooks: %s: %v", entry.ID, err)
		_ = storage.RecordExtensionProjectDelivery(entry.ID, projectID, err.Error())
		return
	}
	if sent {
		_ = storage.RecordExtensionProjectDelivery(entry.ID, projectID, "")
	}
}

func snapshotFor(ev Event) (*storage.HookTaskSnapshot, error) {
	if ev.Snapshot != nil {
		return ev.Snapshot, nil
	}
	if ev.TaskID <= 0 {
		return nil, fmt.Errorf("missing task")
	}
	return storage.GetHookTaskSnapshot(ev.TaskID)
}

func deliver(m extensions.Manifest, msg string, projectID int, eventType string, vars map[string]string) (sent bool, err error) {
	if m.Delivery == nil {
		return false, nil
	}
	if projectID <= 0 {
		return false, nil
	}
	typ := strings.TrimSpace(m.Delivery.Type)
	switch typ {
	case extensions.DeliveryDiscordWebhook, extensions.DeliverySlackWebhook, extensions.DeliveryTeamsWebhook, extensions.DeliveryHTTPWebhook:
		url, err := storage.GetExtensionSecret(m.ID, projectID, strings.TrimSpace(m.Delivery.URLFrom))
		if err != nil {
			return false, err
		}
		if strings.TrimSpace(url) == "" {
			return false, nil
		}
		return true, sendWebhook(typ, strings.TrimSpace(m.Delivery.Format), url, msg, eventType, vars)
	default:
		return false, fmt.Errorf("unsupported delivery %q", m.Delivery.Type)
	}
}

func publicTaskURL(taskID int) string {
	base := strings.TrimSpace(os.Getenv("PUBLIC_URL"))
	if base == "" {
		base = strings.TrimSpace(config.Cfg.BasePath)
	}
	if !strings.Contains(base, "://") {
		return ""
	}
	return strings.TrimSuffix(base, "/") + "/tasks/" + strconv.Itoa(taskID)
}

// DeliverTest sends a fake event to this project's webhook (bypasses enabled/filters).
func DeliverTest(extensionID string, projectID int, projectName string) error {
	if projectID <= 0 {
		return fmt.Errorf("project required")
	}
	entry, ok := extensions.Get(extensionID)
	if !ok || !entry.Loaded {
		return fmt.Errorf("extension not loaded")
	}
	if entry.Manifest.Delivery == nil {
		return fmt.Errorf("extension has no delivery")
	}
	settings, err := storage.GetExtensionProjectSettings(extensionID, projectID)
	if err != nil {
		return err
	}
	tmpl := templateFor(entry.Manifest, settings.Templates, "task.updated")
	if strings.TrimSpace(tmpl) == "" {
		tmpl = "Task {name} updated to {status} in project {project}"
	}
	if strings.TrimSpace(projectName) == "" {
		projectName = "Test project"
	}
	ev := Event{
		Type:          "task.updated",
		StatusChanged: true,
		OldStatus:     "In progress",
		NewStatus:     "Done",
		Actor:         "owner",
		Snapshot: &storage.HookTaskSnapshot{
			ID:          0,
			Title:       "Test task",
			Completed:   false,
			Priority:    2,
			ProjectID:   projectID,
			ProjectName: projectName,
			StatusName:  "Done",
		},
	}
	title := truncateTitle(ev.Snapshot.Title)
	vars := map[string]string{
		"task":       title,
		"name":       title,
		"status":     ev.NewStatus,
		"old_status": ev.OldStatus,
		"project":    ev.Snapshot.ProjectName,
		"actor":      ev.Actor,
		"url":        publicTaskURL(1),
		"id":         "1",
		"priority":   priorityLabel(ev.Snapshot.Priority),
	}
	msg := Interpolate(tmpl, vars)
	sent, err := deliver(entry.Manifest, msg, projectID, ev.Type, vars)
	if err != nil {
		_ = storage.RecordExtensionProjectDelivery(extensionID, projectID, err.Error())
		return err
	}
	if !sent {
		return fmt.Errorf("webhook URL is not set")
	}
	_ = storage.RecordExtensionProjectDelivery(extensionID, projectID, "")
	return nil
}
