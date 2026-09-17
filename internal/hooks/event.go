package hooks

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"GoTodo/internal/storage"
)

const (
	EventTaskCreated         = "task.created"
	EventTaskUpdated         = "task.updated"
	EventTaskDeleted         = "task.deleted"
	EventTaskCommented       = "task.commented"
	EventTaskReordered       = "task.reordered"
	EventTaskClaimed         = "task.claimed"
	EventTaskUnclaimed       = "task.unclaimed"
	EventTaskDueChanged      = "task.due_changed"
	EventTaskMoved           = "task.moved"
	EventTaskProjectChanged  = "task.project_changed"
	EventTaskSprintChanged   = "task.sprint_changed"
	EventTaskTagged          = "task.tagged"
	EventTaskOverdue         = "task.overdue"
	EventTaskMentioned       = "task.mentioned"
	EventTaskCompleted       = "task.completed"
	EventTaskReopened        = "task.reopened"
	EventTaskDueSoon         = "task.due_soon"
	EventTaskArchived             = "task.archived"
	EventTaskRestored             = "task.restored"
	EventTaskStatusChanged        = "task.status_changed"
	EventTaskCommentEdited        = "task.comment_edited"
	EventTaskCommentDeleted       = "task.comment_deleted"
	EventTaskCommentRestored      = "task.comment_restored"
	EventProjectUpdated           = "project.updated"
	EventProjectCreated           = "project.created"
	EventProjectDeleted           = "project.deleted"
	EventProjectArchived          = "project.archived"
	EventProjectRestored          = "project.restored"
	EventProjectMemberJoined      = "project.member_joined"
	EventProjectMemberLeft        = "project.member_left"
	EventProjectMemberRoleChanged = "project.member_role_changed"
	EventProjectInviteSent        = "project.invite_sent"
	EventProjectInviteDeclined    = "project.invite_declined"
	EventSprintCreated            = "sprint.created"
	EventSprintUpdated            = "sprint.updated"
	EventSprintDeleted            = "sprint.deleted"
	EventSprintStarted            = "sprint.started"
	EventSprintEnded              = "sprint.ended"
	EventImportCompleted          = "import.completed"
	EventJoinRequest              = "join.request"
	EventJoinApproved             = "join.approved"
	EventJoinDenied               = "join.denied"
)

// Event is a domain change delivered to loaded extensions.
type Event struct {
	Type             string
	TaskID           int
	ProjectID        int
	ActorID          int
	OwnerID          int
	StatusChanged    bool
	OldStatus        string
	NewStatus        string
	Comment          string
	Count            int
	Changed          []string
	JoinEmail        string
	JoinMessage      string
	MentionedUserIDs []int
	Mentions         []string
	MemberID         int
	MemberName       string
	SprintID         int
	SprintName       string
	FieldChanges     []FieldChange
	Snapshot         *storage.HookTaskSnapshot
	Actor            string
	EventID          string
	OccurredAt       time.Time
	Immediate        bool // skip coalesce/quiet hours (test sends)
}

// FieldChange is an old/new pair included in structured JSON payloads.
type FieldChange struct {
	Field string `json:"field"`
	Old   string `json:"old,omitempty"`
	New   string `json:"new,omitempty"`
}

func (ev Event) isSiteEvent() bool {
	switch ev.Type {
	case EventJoinRequest, EventJoinApproved, EventJoinDenied:
		return true
	default:
		return false
	}
}

func (ev Event) isProjectLevel() bool {
	switch ev.Type {
	case EventProjectUpdated, EventProjectCreated, EventProjectDeleted,
		EventProjectArchived, EventProjectRestored,
		EventTaskReordered, EventProjectMemberJoined, EventProjectMemberLeft,
		EventProjectMemberRoleChanged, EventProjectInviteSent, EventProjectInviteDeclined,
		EventSprintCreated, EventSprintUpdated, EventSprintDeleted,
		EventSprintStarted, EventSprintEnded, EventImportCompleted:
		return true
	default:
		return false
	}
}

func eventVars(ev Event, snap *storage.HookTaskSnapshot, actor string) map[string]string {
	if snap == nil {
		snap = &storage.HookTaskSnapshot{}
	}
	status := snap.StatusName
	if ev.NewStatus != "" {
		status = ev.NewStatus
	}
	if status == "" && !ev.isProjectLevel() {
		if snap.Completed {
			status = "Completed"
		} else {
			status = "Open"
		}
	}
	title := truncateTitle(snap.Title)
	id := snap.ID
	if id <= 0 {
		id = ev.TaskID
	}
	project := snap.ProjectName
	tags := strings.Join(snap.Tags, ", ")
	count := ev.Count
	if count <= 0 && ev.Type == EventTaskReordered {
		count = 1
	}
	comment := truncateRunes(strings.TrimSpace(ev.Comment), 400)
	desc := truncateRunes(strings.TrimSpace(snap.Description), 2000)
	parentID := snap.ParentID
	vars := map[string]string{
		"task":         title,
		"name":         title,
		"status":       status,
		"old_status":   ev.OldStatus,
		"project":      project,
		"project_id":   strconv.Itoa(snap.ProjectID),
		"actor":        actor,
		"actor_id":     strconv.Itoa(ev.ActorID),
		"url":          publicTaskURL(id),
		"id":           strconv.Itoa(id),
		"priority":     priorityLabel(snap.Priority),
		"description":  desc,
		"parent_id":    strconv.Itoa(parentID),
		"estimate":     strconv.Itoa(snap.EstimatePoints),
		"comment":      comment,
		"claimed_by":   snap.ClaimedByName,
		"due_date":     snap.DueDate,
		"sprint":       snap.SprintName,
		"tags":         tags,
		"count":        strconv.Itoa(count),
		"join_email":   ev.JoinEmail,
		"join_message": truncateRunes(ev.JoinMessage, 200),
		"mentions":     strings.Join(ev.Mentions, ", "),
		"member":       ev.MemberName,
		"event":        ev.Type,
		"event_id":     ev.EventID,
		"occurred_at":  formatOccurred(ev.OccurredAt),
		"changed":      strings.Join(ev.Changed, ","),
	}
	if len(ev.FieldChanges) > 0 {
		if raw, err := json.Marshal(ev.FieldChanges); err == nil {
			vars["changes_json"] = string(raw)
		}
	}
	if oldPri := fieldChangeOld(ev.FieldChanges, "priority"); oldPri != "" {
		vars["old_priority"] = oldPri
	}
	if ev.SprintName != "" {
		vars["sprint"] = ev.SprintName
	}
	if len(snap.CustomFields) > 0 {
		if raw, err := json.Marshal(snap.CustomFields); err == nil {
			vars["fields"] = string(raw)
			vars["fields_json"] = string(raw)
		}
	}
	if snap.ProjectID > 0 && (id <= 0 || ev.isProjectLevel()) {
		vars["url"] = publicProjectURL(snap.ProjectID)
	}
	if ev.isSiteEvent() {
		vars["url"] = publicAdminJoinURL()
		if vars["name"] == "" {
			vars["name"] = ev.JoinEmail
			vars["task"] = ev.JoinEmail
		}
	}
	return vars
}

func formatOccurred(t time.Time) string {
	if t.IsZero() {
		t = time.Now().UTC()
	}
	return t.UTC().Format(time.RFC3339)
}

func applyMentions(actor string, mentionMap map[string]string) string {
	actor = strings.TrimSpace(actor)
	if actor == "" || len(mentionMap) == 0 {
		return actor
	}
	if v := strings.TrimSpace(mentionMap[strings.ToLower(actor)]); v != "" {
		return v
	}
	if v := strings.TrimSpace(mentionMap[actor]); v != "" {
		return v
	}
	return actor
}

func fieldChangeOld(changes []FieldChange, field string) string {
	for _, c := range changes {
		if c.Field == field {
			return c.Old
		}
	}
	return ""
}
