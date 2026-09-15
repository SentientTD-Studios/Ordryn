package hooks

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"GoTodo/internal/config"
	"GoTodo/internal/extensions"
	"GoTodo/internal/storage"

	"github.com/google/uuid"
)

// HasWork reports whether any extension is loaded (skip goroutine in tests).
func HasWork() bool {
	return extensions.LoadedCount() > 0
}

// Dispatch delivers ev to matching extensions. Safe to call from a goroutine.
func Dispatch(ev Event) {
	if ev.TaskID <= 0 && ev.Snapshot == nil && ev.ProjectID <= 0 && !ev.isSiteEvent() {
		return
	}
	if ev.EventID == "" {
		ev.EventID = uuid.NewString()
	}
	if ev.OccurredAt.IsZero() {
		ev.OccurredAt = time.Now().UTC()
	}
	for _, entry := range extensions.LoadedEntries() {
		deliverToExtension(entry, ev)
	}
}

func deliverToExtension(entry extensions.Entry, ev Event) {
	if !entry.Loaded || entry.Manifest.Delivery == nil {
		return
	}
	site, err := storage.GetExtensionSettings(entry.ID)
	if err != nil {
		log.Printf("hooks: load settings %s: %v", entry.ID, err)
		return
	}
	if ev.isSiteEvent() {
		deliverSite(entry, site, ev)
		return
	}
	snap, err := snapshotFor(ev)
	if err != nil {
		log.Printf("hooks: snapshot: %v", err)
		return
	}
	if snap == nil {
		return
	}
	ev.Snapshot = snap
	if ev.ProjectID <= 0 {
		ev.ProjectID = snap.ProjectID
	}
	if ev.OwnerID <= 0 {
		ev.OwnerID = snap.OwnerID
	}
	if ev.ProjectID <= 0 {
		deliverPersonal(entry, site, ev, snap)
		return
	}
	deliverProjectTeam(entry, site, ev, snap)
	deliverProjectMembers(entry, site, ev, snap)
}

func snapshotFor(ev Event) (*storage.HookTaskSnapshot, error) {
	if ev.Snapshot != nil {
		return ev.Snapshot, nil
	}
	if ev.TaskID > 0 {
		return storage.GetHookTaskSnapshot(ev.TaskID)
	}
	if ev.ProjectID > 0 && ev.isProjectLevel() {
		return storage.GetHookProjectSnapshot(ev.ProjectID)
	}
	return nil, fmt.Errorf("missing snapshot")
}

func deliverSite(entry extensions.Entry, site storage.ExtensionSettings, ev Event) {
	if !shouldDeliverSite(entry.Manifest, site, ev) {
		return
	}
	tmpl := templateFor(entry.Manifest, site.Templates, ev.Type)
	if strings.TrimSpace(tmpl) == "" {
		tmpl = templateFor(entry.Manifest, nil, ev.Type)
	}
	if strings.TrimSpace(tmpl) == "" {
		tmpl = "New join request from {join_email}"
	}
	actor := resolveActor(ev)
	vars := eventVars(ev, ev.Snapshot, actor)
	msg := Interpolate(tmpl, vars)
	sendDestination(entry, destContext{
		ProjectID: 0,
		UserID:    0,
		Event:     ev,
		Vars:      vars,
		Message:   msg,
		Immediate: ev.Immediate,
	})
}

func deliverProjectTeam(entry extensions.Entry, site storage.ExtensionSettings, ev Event, snap *storage.HookTaskSnapshot) {
	project, err := storage.GetExtensionProjectSettings(entry.ID, ev.ProjectID)
	if err != nil {
		log.Printf("hooks: load project settings %s project=%d: %v", entry.ID, ev.ProjectID, err)
		return
	}
	dest := destFromProject(project)
	if !shouldDeliverDest(entry.Manifest, site, dest, ev, ev.ProjectID) {
		return
	}
	tmpl := templateFor(entry.Manifest, dest.Templates, ev.Type)
	if strings.TrimSpace(tmpl) == "" {
		return
	}
	actor := applyMentions(resolveActor(ev), dest.MentionMap)
	vars := eventVars(ev, snap, actor)
	msg := Interpolate(tmpl, vars)
	sendDestination(entry, destContext{
		ProjectID: ev.ProjectID,
		UserID:    0,
		Event:     ev,
		Vars:      vars,
		Message:   msg,
		Dest:      dest,
		Immediate: ev.Immediate,
		Timezone:  storage.UserTimezone(snap.OwnerID),
	})
}

func deliverProjectMembers(entry extensions.Entry, site storage.ExtensionSettings, ev Event, snap *storage.HookTaskSnapshot) {
	members, err := storage.ListEnabledMemberSettings(entry.ID, ev.ProjectID)
	if err != nil {
		log.Printf("hooks: member settings %s project=%d: %v", entry.ID, ev.ProjectID, err)
		return
	}
	for _, mem := range members {
		dest := destFromMember(mem.Settings, mem.UserID, false)
		if dest.MentionMap == nil {
			if p, err := storage.GetExtensionProjectSettings(entry.ID, ev.ProjectID); err == nil {
				dest.MentionMap = p.MentionMap
			}
		}
		if !shouldDeliverDest(entry.Manifest, site, dest, ev, ev.ProjectID) {
			continue
		}
		tmpl := templateFor(entry.Manifest, dest.Templates, ev.Type)
		if strings.TrimSpace(tmpl) == "" {
			continue
		}
		actor := applyMentions(resolveActor(ev), dest.MentionMap)
		vars := eventVars(ev, snap, actor)
		msg := Interpolate(tmpl, vars)
		sendDestination(entry, destContext{
			ProjectID: ev.ProjectID,
			UserID:    mem.UserID,
			Event:     ev,
			Vars:      vars,
			Message:   msg,
			Dest:      dest,
			Immediate: ev.Immediate,
			Timezone:  storage.UserTimezone(mem.UserID),
		})
	}
}

func deliverPersonal(entry extensions.Entry, site storage.ExtensionSettings, ev Event, snap *storage.HookTaskSnapshot) {
	ownerID := snap.OwnerID
	if ownerID <= 0 {
		ownerID = ev.OwnerID
	}
	if ownerID <= 0 {
		return
	}
	mem, err := storage.GetExtensionMemberSettings(entry.ID, 0, ownerID)
	if err != nil || !mem.Enabled {
		return
	}
	dest := destFromMember(mem, ownerID, true)
	if !shouldDeliverDest(entry.Manifest, site, dest, ev, 0) {
		return
	}
	tmpl := templateFor(entry.Manifest, dest.Templates, ev.Type)
	if strings.TrimSpace(tmpl) == "" {
		return
	}
	actor := resolveActor(ev)
	vars := eventVars(ev, snap, actor)
	msg := Interpolate(tmpl, vars)
	sendDestination(entry, destContext{
		ProjectID: 0,
		UserID:    ownerID,
		Event:     ev,
		Vars:      vars,
		Message:   msg,
		Dest:      dest,
		Immediate: ev.Immediate,
		Timezone:  storage.UserTimezone(ownerID),
	})
}

func resolveActor(ev Event) string {
	actor := strings.TrimSpace(ev.Actor)
	if actor == "" && ev.ActorID > 0 {
		if p, err := storage.GetUserProfileByID(ev.ActorID); err == nil && p != nil {
			actor = p.UserName
			if actor == "" {
				actor = p.Email
			}
		}
	}
	return actor
}

type destContext struct {
	ProjectID int
	UserID    int
	Event     Event
	Vars      map[string]string
	Message   string
	Dest      destFilter
	Immediate bool
	Timezone  string
}

func sendDestination(entry extensions.Entry, ctx destContext) {
	now := ctx.Event.OccurredAt
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if !ctx.Immediate && inQuietHours(ctx.Dest.QuietHoursStart, ctx.Dest.QuietHoursEnd, ctx.Timezone, now) {
		enqueueDigest(entry, ctx)
		return
	}
	if delay := digestDelay(ctx.Dest.Digest); !ctx.Immediate && delay > 0 {
		enqueueDigest(entry, ctx)
		return
	}
	if !ctx.Immediate {
		enqueueOrSend(entry, ctx)
		return
	}
	_, err := deliverNow(entry, ctx)
	recordLast(entry.ID, ctx.ProjectID, ctx.UserID, err)
}

func deliver(m extensions.Manifest, msg string, projectID int, eventType string, vars map[string]string) (sent bool, err error) {
	if m.Delivery == nil {
		return false, nil
	}
	if projectID <= 0 {
		return false, nil
	}
	entry := extensions.Entry{ID: m.ID, Loaded: true, Manifest: m}
	_, err = deliverNow(entry, destContext{
		ProjectID: projectID,
		Message:   msg,
		Event:     Event{Type: eventType, Immediate: true},
		Vars:      vars,
		Immediate: true,
	})
	if err != nil && strings.Contains(err.Error(), "unsupported delivery") {
		return false, err
	}
	if err != nil && strings.Contains(err.Error(), "not set") {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func recordLast(extensionID string, projectID, userID int, err error) {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	if userID > 0 {
		_ = storage.RecordExtensionMemberDelivery(extensionID, projectID, userID, msg)
		return
	}
	if projectID > 0 {
		_ = storage.RecordExtensionProjectDelivery(extensionID, projectID, msg)
		return
	}
	_ = storage.RecordExtensionSiteDelivery(extensionID, msg)
}

func deliverNow(entry extensions.Entry, ctx destContext) (string, error) {
	m := entry.Manifest
	if m.Delivery == nil {
		return "", nil
	}
	typ := strings.TrimSpace(m.Delivery.Type)
	key := m.Delivery.DestinationKey()
	signing, _ := storage.GetExtensionSecretForUser(m.ID, ctx.ProjectID, ctx.UserID, storage.SigningSecretKey)
	threadID := ""
	if ctx.Event.Type == EventTaskCommented && ctx.Event.TaskID > 0 && ctx.ProjectID > 0 {
		threadID, _ = storage.GetHookTaskMessageID(m.ID, ctx.ProjectID, ctx.Event.TaskID)
	}
	wait := ctx.Event.Type == EventTaskCreated && ctx.Event.TaskID > 0

	switch typ {
	case extensions.DeliveryNtfyWebhook:
		u, err := storage.GetExtensionSecretForUser(m.ID, ctx.ProjectID, ctx.UserID, key)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(u) == "" {
			return "", fmt.Errorf("webhook URL is not set")
		}
		auth, _ := storage.GetExtensionSecretForUser(m.ID, ctx.ProjectID, ctx.UserID, "ntfy_auth")
		return "", sendNtfy(u, auth, ctx.Message, ctx.Vars, signing)
	case extensions.DeliveryDiscordWebhook, extensions.DeliverySlackWebhook, extensions.DeliveryTeamsWebhook, extensions.DeliveryHTTPWebhook:
		u, err := storage.GetExtensionSecretForUser(m.ID, ctx.ProjectID, ctx.UserID, key)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(u) == "" {
			return "", fmt.Errorf("webhook URL is not set")
		}
		messageID, err := sendWebhookOpts(typ, strings.TrimSpace(m.Delivery.Format), u, ctx.Message, ctx.Event.Type, ctx.Vars, sendOpts{
			SigningSecret: signing,
			Wait:          wait,
			ThreadID:      threadID,
		})
		if err == nil && messageID != "" && ctx.Event.TaskID > 0 && ctx.ProjectID > 0 {
			_ = storage.SetHookTaskMessageID(m.ID, ctx.ProjectID, ctx.Event.TaskID, messageID)
		}
		return messageID, err
	default:
		return "", fmt.Errorf("unsupported delivery %q", typ)
	}
}

func publicTaskURL(taskID int) string {
	if taskID <= 0 {
		return publicBaseURL()
	}
	base := publicBaseURL()
	if base == "" {
		return ""
	}
	return base + "/tasks/" + strconv.Itoa(taskID)
}

func publicProjectURL(projectID int) string {
	base := publicBaseURL()
	if base == "" || projectID <= 0 {
		return base
	}
	return base + "/tasks?project=" + strconv.Itoa(projectID)
}

func publicAdminJoinURL() string {
	base := publicBaseURL()
	if base == "" {
		return ""
	}
	return base + "/admin/join-requests"
}

func publicBaseURL() string {
	base := strings.TrimSpace(os.Getenv("PUBLIC_URL"))
	if base == "" {
		base = strings.TrimSpace(config.Cfg.BasePath)
	}
	if !strings.Contains(base, "://") {
		return ""
	}
	return strings.TrimSuffix(base, "/")
}

func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

func randomSecret() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return uuid.NewString()
	}
	return hex.EncodeToString(b)
}

func queuedPayloadJSON(ctx destContext) string {
	raw, err := json.Marshal(queuedPayload{
		Message:   ctx.Message,
		EventType: ctx.Event.Type,
		Vars:      ctx.Vars,
		EventID:   ctx.Event.EventID,
		TaskID:    ctx.Event.TaskID,
	})
	if err != nil {
		return ctx.Message
	}
	return string(raw)
}

type queuedPayload struct {
	Message   string            `json:"message"`
	EventType string            `json:"event_type"`
	Vars      map[string]string `json:"vars"`
	EventID   string            `json:"event_id"`
	TaskID    int               `json:"task_id"`
}

func parseQueuedPayload(raw string) queuedPayload {
	var p queuedPayload
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return queuedPayload{Message: raw}
	}
	return p
}

// DeliverTest sends a fake event to this project's team webhook (bypasses enabled/filters).
func DeliverTest(extensionID string, projectID int, projectName string) error {
	return DeliverTestForUser(extensionID, projectID, 0, projectName)
}

// DeliverTestForUser sends a fake event to a team (userID 0) or member destination.
func DeliverTestForUser(extensionID string, projectID, userID int, projectName string) error {
	if projectID < 0 {
		return fmt.Errorf("project required")
	}
	entry, ok := extensions.Get(extensionID)
	if !ok || !entry.Loaded {
		return fmt.Errorf("extension not loaded")
	}
	if entry.Manifest.Delivery == nil {
		return fmt.Errorf("extension has no delivery")
	}
	if strings.TrimSpace(projectName) == "" {
		projectName = "Test project"
	}
	ev := Event{
		Type:          EventTaskUpdated,
		StatusChanged: true,
		OldStatus:     "In progress",
		NewStatus:     "Done",
		Actor:         "owner",
		Immediate:     true,
		EventID:       uuid.NewString(),
		OccurredAt:    time.Now().UTC(),
		ProjectID:     projectID,
		Snapshot: &storage.HookTaskSnapshot{
			ID:          1,
			Title:       "Test task",
			Completed:   false,
			Priority:    2,
			ProjectID:   projectID,
			ProjectName: projectName,
			StatusName:  "Done",
			DueDate:     time.Now().UTC().Format("2006-01-02"),
			Tags:        []string{"test"},
		},
	}
	vars := eventVars(ev, ev.Snapshot, ev.Actor)
	tmpl := ""
	if userID > 0 {
		s, err := storage.GetExtensionMemberSettings(extensionID, projectID, userID)
		if err != nil {
			return err
		}
		tmpl = templateFor(entry.Manifest, s.Templates, EventTaskUpdated)
	} else if projectID > 0 {
		s, err := storage.GetExtensionProjectSettings(extensionID, projectID)
		if err != nil {
			return err
		}
		tmpl = templateFor(entry.Manifest, s.Templates, EventTaskUpdated)
	}
	if strings.TrimSpace(tmpl) == "" {
		tmpl = "Task {name} updated to {status} in project {project}"
	}
	msg := Interpolate(tmpl, vars)
	_, err := deliverNow(entry, destContext{
		ProjectID: projectID,
		UserID:    userID,
		Event:     ev,
		Vars:      vars,
		Message:   msg,
		Immediate: true,
	})
	recordLast(extensionID, projectID, userID, err)
	if err != nil {
		return err
	}
	return nil
}

// RotateSigningSecret replaces the HMAC secret and returns the new value (show once).
func RotateSigningSecret(extensionID string, projectID, userID int) (string, error) {
	sec := randomSecret()
	if err := storage.SetExtensionSecretForUser(extensionID, projectID, userID, storage.SigningSecretKey, sec); err != nil {
		return "", err
	}
	return sec, nil
}
func EnsureSigningSecret(extensionID string, projectID, userID int) (string, error) {
	cur, err := storage.GetExtensionSecretForUser(extensionID, projectID, userID, storage.SigningSecretKey)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(cur) != "" {
		return cur, nil
	}
	sec := randomSecret()
	if err := storage.SetExtensionSecretForUser(extensionID, projectID, userID, storage.SigningSecretKey, sec); err != nil {
		return "", err
	}
	return sec, nil
}

// SampleJSONBody returns the structured JSON payload used by http.webhook format=json.
func SampleJSONBody() string {
	vars := map[string]string{
		"id": "42", "name": "Ship", "task": "Ship", "status": "Done", "old_status": "In progress",
		"project": "Ordryn", "actor": "ada", "url": "https://todo.example.com/tasks/42", "priority": "High",
		"comment": "", "claimed_by": "ada", "due_date": "2026-09-20", "sprint": "Sprint 1", "tags": "release",
		"event_id": "00000000-0000-0000-0000-000000000001", "occurred_at": "2026-09-15T16:00:00Z", "changed": "status",
		"fields_json": `{"severity.level":"high"}`,
	}
	raw, _ := marshalWebhookPayload(extensions.DeliveryHTTPWebhook, extensions.DeliveryFormatJSON, "Task Ship updated to Done in project Ordryn", "task.updated", vars)
	return string(raw)
}
