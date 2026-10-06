package domain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"GoTodo/internal/hooks"
	"GoTodo/internal/live"
	"GoTodo/internal/storage"
)

// Automation limits.
const (
	MaxProjectAutomationRules  = 25
	MaxAutomationActions       = 5
	MaxAutomationRuleNameLen   = 80
	MaxAutomationTextLen       = 1000
	MaxAutomationDays          = 365
	AutomationRunsPerHourLimit = 200
	// AutomationErrorStreakLimit pauses a rule after this many failed runs in a row.
	AutomationErrorStreakLimit = 10
	// automationTimedBatch caps tasks one timed rule handles per worker pass.
	automationTimedBatch = 500
	// automationPreviewScan caps tasks a dry run inspects for event rules.
	automationPreviewScan  = 500
	automationPreviewLimit = 50
)

// Event trigger types. They match hook event names so the sink can map them directly.
const (
	AutomationOnCreated       = hooks.EventTaskCreated
	AutomationOnStatusChanged = hooks.EventTaskStatusChanged
	AutomationOnCompleted     = hooks.EventTaskCompleted
	AutomationOnReopened      = hooks.EventTaskReopened
	AutomationOnClaimed       = hooks.EventTaskClaimed
	AutomationOnUnclaimed     = hooks.EventTaskUnclaimed
	AutomationOnDueChanged    = hooks.EventTaskDueChanged
	AutomationOnTagged        = hooks.EventTaskTagged
	AutomationOnSprintChanged = hooks.EventTaskSprintChanged
	AutomationOnCommented     = hooks.EventTaskCommented
	// AutomationOnUnblocked fires when a task's last open blocker is completed.
	AutomationOnUnblocked = "task.unblocked"
)

// Action types.
const (
	AutomationActSetStatus   = "set_status"
	AutomationActSetPriority = "set_priority"
	AutomationActAddTag      = "add_tag"
	AutomationActRemoveTag   = "remove_tag"
	AutomationActAssign      = "assign"
	AutomationActUnassign    = "unassign"
	AutomationActSetSprint   = "set_sprint"
	AutomationActSetDue      = "set_due"
	AutomationActComplete    = "complete"
	AutomationActReopen      = "reopen"
	AutomationActArchive     = "archive"
	AutomationActComment     = "comment"
	AutomationActNotify      = "notify"
	AutomationActQueueAgent  = "queue_agent"
)

// automationTriggerInfo describes a trigger for validation and the UI.
type automationTriggerInfo struct {
	Type       string
	Timed      bool
	KanbanOnly bool
}

var automationTriggers = []automationTriggerInfo{
	{Type: AutomationOnCreated},
	{Type: AutomationOnStatusChanged, KanbanOnly: true},
	{Type: AutomationOnCompleted},
	{Type: AutomationOnReopened},
	{Type: AutomationOnClaimed, KanbanOnly: true},
	{Type: AutomationOnUnclaimed, KanbanOnly: true},
	{Type: AutomationOnDueChanged},
	{Type: AutomationOnTagged},
	{Type: AutomationOnSprintChanged, KanbanOnly: true},
	{Type: AutomationOnCommented},
	{Type: AutomationOnUnblocked},
	{Type: storage.AutomationTimedOverdue, Timed: true},
	{Type: storage.AutomationTimedDueSoon, Timed: true},
	{Type: storage.AutomationTimedCompleted, Timed: true},
	{Type: storage.AutomationTimedInStatus, Timed: true, KanbanOnly: true},
	{Type: storage.AutomationTimedInactive, Timed: true},
	{Type: storage.AutomationTimedSprintEnded, Timed: true, KanbanOnly: true},
}

func automationTrigger(t string) (automationTriggerInfo, bool) {
	for _, info := range automationTriggers {
		if info.Type == t {
			return info, true
		}
	}
	return automationTriggerInfo{}, false
}

func timedAutomationTriggerTypes() []string {
	out := []string{}
	for _, info := range automationTriggers {
		if info.Timed {
			out = append(out, info.Type)
		}
	}
	return out
}

var kanbanOnlyActions = map[string]bool{
	AutomationActSetStatus: true, AutomationActAssign: true, AutomationActUnassign: true,
	AutomationActSetSprint: true, AutomationActQueueAgent: true,
}

// AutomationTriggerConfig narrows a trigger. Days applies to timed triggers;
// the status lists apply to task.status_changed; TagIDs to task.tagged.
type AutomationTriggerConfig struct {
	Days          int   `json:"days,omitempty"`
	FromStatusIDs []int `json:"from_status_ids,omitempty"`
	ToStatusIDs   []int `json:"to_status_ids,omitempty"`
	TagIDs        []int `json:"tag_ids,omitempty"`
}

// AutomationConditions are the rule's "if" filters. Every set filter must match.
type AutomationConditions struct {
	StatusIDs        []int  `json:"status_ids,omitempty"`
	ExcludeStatusIDs []int  `json:"exclude_status_ids,omitempty"`
	MinPriority      int    `json:"min_priority,omitempty"`
	TagsAny          []int  `json:"tags_any,omitempty"`
	TagsNone         []int  `json:"tags_none,omitempty"`
	Sprint           string `json:"sprint,omitempty"` // "", none, current, any, specific
	SprintID         int    `json:"sprint_id,omitempty"`
	Assignee         string `json:"assignee,omitempty"` // "", unassigned, assigned, user
	AssigneeID       int    `json:"assignee_id,omitempty"`
	FieldKey         string `json:"field_key,omitempty"`
	FieldValue       string `json:"field_value,omitempty"`
	HasDue           string `json:"has_due,omitempty"`    // "", yes, no
	Completion       string `json:"completion,omitempty"` // "", open, done
	TaskKind         string `json:"task_kind,omitempty"`  // "", root, subtask
}

// AutomationAction is one "then" step. Only the fields its Type uses are kept.
type AutomationAction struct {
	Type     string `json:"type"`
	StatusID int    `json:"status_id,omitempty"`
	Priority *int   `json:"priority,omitempty"`
	TagID    int    `json:"tag_id,omitempty"`
	UserID   int    `json:"user_id,omitempty"`
	Sprint   string `json:"sprint,omitempty"` // current, next, backlog, specific
	SprintID int    `json:"sprint_id,omitempty"`
	Days     *int   `json:"days,omitempty"`
	Body     string `json:"body,omitempty"`
	Target   string `json:"target,omitempty"` // notify: watchers, assignee
	AgentID  int    `json:"agent_id,omitempty"`
}

// AutomationRuleInput creates or patches a rule. Nil fields are left unchanged on patch.
type AutomationRuleInput struct {
	Name          *string
	Enabled       *bool
	TriggerType   *string
	TriggerConfig *AutomationTriggerConfig
	Conditions    *AutomationConditions
	Actions       *[]AutomationAction
	RecipeID      string
}

// AutomationChange is one applied action, stored in the run log.
type AutomationChange struct {
	Action string `json:"action"`
	Field  string `json:"field,omitempty"`
	From   string `json:"from,omitempty"`
	To     string `json:"to,omitempty"`
	Error  string `json:"error,omitempty"`
}

// AutomationPreviewTask is a task a rule would act on now.
type AutomationPreviewTask struct {
	ID    int
	Title string
}

// --- CRUD ---

// ListAutomationRules returns a project's rules for a manager.
func ListAutomationRules(userID, projectID int) ([]storage.AutomationRule, error) {
	if _, err := requireProjectManage(projectID, userID); err != nil {
		return nil, err
	}
	return storage.ListAutomationRules(projectID)
}

// GetAutomationRule returns one rule for a manager.
func GetAutomationRule(userID, projectID, ruleID int) (*storage.AutomationRule, error) {
	if _, err := requireProjectManage(projectID, userID); err != nil {
		return nil, err
	}
	return loadAutomationRule(projectID, ruleID)
}

func loadAutomationRule(projectID, ruleID int) (*storage.AutomationRule, error) {
	r, err := storage.GetAutomationRule(projectID, ruleID)
	if errors.Is(err, storage.ErrAutomationRuleNotFound) {
		return nil, ErrNotFound
	}
	return r, err
}

// CreateAutomationRule adds a rule to a project.
func CreateAutomationRule(userID, projectID int, in AutomationRuleInput) (*storage.AutomationRule, error) {
	proj, err := requireProjectManage(projectID, userID)
	if err != nil {
		return nil, err
	}
	if proj.Archived {
		return nil, fmt.Errorf("%w: restore the project before adding rules", ErrValidation)
	}
	n, err := storage.CountAutomationRules(projectID)
	if err != nil {
		return nil, err
	}
	if n >= MaxProjectAutomationRules {
		return nil, fmt.Errorf("%w: a project can have at most %d rules", ErrValidation, MaxProjectAutomationRules)
	}
	if storage.SystemUserID() == 0 {
		if _, err := storage.EnsureSystemUser(); err != nil {
			return nil, err
		}
	}
	r := storage.AutomationRule{
		ProjectID:     projectID,
		Enabled:       true,
		TriggerConfig: json.RawMessage(`{}`),
		Conditions:    json.RawMessage(`{}`),
		Actions:       json.RawMessage(`[]`),
		RecipeID:      truncateString(strings.TrimSpace(in.RecipeID), 40),
		CreatedBy:     userID,
	}
	if in.Name == nil || in.TriggerType == nil || in.Actions == nil {
		return nil, fmt.Errorf("%w: name, trigger, and at least one action are required", ErrValidation)
	}
	if err := applyAutomationInput(&r, proj.WorkflowMode, in); err != nil {
		return nil, err
	}
	created, err := storage.CreateAutomationRule(r)
	if err != nil {
		return nil, err
	}
	invalidateAutomationCache()
	_ = storage.LogProjectEvent(projectID, userID, "automation_rule_added", map[string]interface{}{
		"rule_id": created.ID, "name": created.Name, "trigger": created.TriggerType,
	})
	live.AfterProjectChange(userID, projectID, live.TypeProjectUpdated)
	return created, nil
}

// UpdateAutomationRule edits a rule.
func UpdateAutomationRule(userID, projectID, ruleID int, in AutomationRuleInput) (*storage.AutomationRule, error) {
	proj, err := requireProjectManage(projectID, userID)
	if err != nil {
		return nil, err
	}
	r, err := loadAutomationRule(projectID, ruleID)
	if err != nil {
		return nil, err
	}
	wasEnabled := r.Enabled
	if err := applyAutomationInput(r, proj.WorkflowMode, in); err != nil {
		return nil, err
	}
	if r.Enabled && !wasEnabled && proj.Archived {
		return nil, fmt.Errorf("%w: restore the project before enabling rules", ErrValidation)
	}
	updated, err := storage.UpdateAutomationRule(*r, userID)
	if errors.Is(err, storage.ErrAutomationRuleNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	invalidateAutomationCache()
	_ = storage.LogProjectEvent(projectID, userID, "automation_rule_updated", map[string]interface{}{
		"rule_id": updated.ID, "name": updated.Name, "enabled": updated.Enabled,
	})
	live.AfterProjectChange(userID, projectID, live.TypeProjectUpdated)
	return updated, nil
}

// DeleteAutomationRule removes a rule and its run history.
func DeleteAutomationRule(userID, projectID, ruleID int) error {
	if _, err := requireProjectManage(projectID, userID); err != nil {
		return err
	}
	r, err := loadAutomationRule(projectID, ruleID)
	if err != nil {
		return err
	}
	if err := storage.DeleteAutomationRule(projectID, ruleID); err != nil {
		if errors.Is(err, storage.ErrAutomationRuleNotFound) {
			return ErrNotFound
		}
		return err
	}
	invalidateAutomationCache()
	_ = storage.LogProjectEvent(projectID, userID, "automation_rule_removed", map[string]interface{}{
		"rule_id": ruleID, "name": r.Name,
	})
	live.AfterProjectChange(userID, projectID, live.TypeProjectUpdated)
	return nil
}

// ReorderAutomationRules sets the display and evaluation order of rules.
func ReorderAutomationRules(userID, projectID int, ids []int) ([]storage.AutomationRule, error) {
	if _, err := requireProjectManage(projectID, userID); err != nil {
		return nil, err
	}
	if err := storage.ReorderAutomationRules(projectID, ids); err != nil {
		return nil, err
	}
	return storage.ListAutomationRules(projectID)
}

// ListAutomationRuns returns recent rule runs for a manager.
func ListAutomationRuns(userID, projectID, ruleID, taskID, limit int) ([]storage.AutomationRuleRun, error) {
	if _, err := requireProjectManage(projectID, userID); err != nil {
		return nil, err
	}
	return storage.ListAutomationRuns(projectID, ruleID, taskID, limit)
}

func truncateString(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max])
}

// --- validation ---

func applyAutomationInput(r *storage.AutomationRule, workflowMode string, in AutomationRuleInput) error {
	kanban := workflowMode == storage.WorkflowKanban
	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if name == "" {
			return fmt.Errorf("%w: rule name is required", ErrValidation)
		}
		if utf8.RuneCountInString(name) > MaxAutomationRuleNameLen {
			return fmt.Errorf("%w: rule name must be %d characters or less", ErrValidation, MaxAutomationRuleNameLen)
		}
		r.Name = name
	}
	if in.Enabled != nil {
		r.Enabled = *in.Enabled
	}
	if in.TriggerType != nil {
		r.TriggerType = strings.TrimSpace(*in.TriggerType)
	}
	info, ok := automationTrigger(r.TriggerType)
	if !ok {
		return fmt.Errorf("%w: unknown trigger %q", ErrValidation, r.TriggerType)
	}
	if info.KanbanOnly && !kanban {
		return fmt.Errorf("%w: this trigger needs a kanban project", ErrValidation)
	}

	cfg := AutomationTriggerConfig{}
	if in.TriggerConfig != nil {
		cfg = *in.TriggerConfig
	} else {
		_ = json.Unmarshal(r.TriggerConfig, &cfg)
	}
	if err := normalizeTriggerConfig(r.ProjectID, info, &cfg); err != nil {
		return err
	}
	raw, _ := json.Marshal(cfg)
	r.TriggerConfig = raw

	cond := AutomationConditions{}
	if in.Conditions != nil {
		cond = *in.Conditions
	} else {
		_ = json.Unmarshal(r.Conditions, &cond)
	}
	if err := normalizeConditions(r.ProjectID, kanban, &cond); err != nil {
		return err
	}
	raw, _ = json.Marshal(cond)
	r.Conditions = raw

	var actions []AutomationAction
	if in.Actions != nil {
		actions = *in.Actions
	} else {
		_ = json.Unmarshal(r.Actions, &actions)
	}
	if len(actions) == 0 {
		return fmt.Errorf("%w: add at least one action", ErrValidation)
	}
	if len(actions) > MaxAutomationActions {
		return fmt.Errorf("%w: a rule can have at most %d actions", ErrValidation, MaxAutomationActions)
	}
	for i := range actions {
		if err := normalizeAction(r.ProjectID, kanban, &actions[i]); err != nil {
			return fmt.Errorf("%w (action %d)", err, i+1)
		}
	}
	raw, _ = json.Marshal(actions)
	r.Actions = raw
	return nil
}

func normalizeTriggerConfig(projectID int, info automationTriggerInfo, cfg *AutomationTriggerConfig) error {
	if info.Timed {
		if cfg.Days < 0 || cfg.Days > MaxAutomationDays {
			return fmt.Errorf("%w: days must be between 0 and %d", ErrValidation, MaxAutomationDays)
		}
		if cfg.Days == 0 && (info.Type == storage.AutomationTimedCompleted || info.Type == storage.AutomationTimedInactive ||
			info.Type == storage.AutomationTimedInStatus) {
			return fmt.Errorf("%w: days must be at least 1", ErrValidation)
		}
	} else {
		cfg.Days = 0
	}
	if info.Type == AutomationOnStatusChanged {
		if err := checkProjectStatuses(projectID, cfg.FromStatusIDs); err != nil {
			return err
		}
		if err := checkProjectStatuses(projectID, cfg.ToStatusIDs); err != nil {
			return err
		}
	} else {
		cfg.FromStatusIDs, cfg.ToStatusIDs = nil, nil
	}
	if info.Type == AutomationOnTagged {
		if err := checkProjectTags(projectID, cfg.TagIDs); err != nil {
			return err
		}
	} else {
		cfg.TagIDs = nil
	}
	return nil
}

func normalizeConditions(projectID int, kanban bool, c *AutomationConditions) error {
	if !kanban && (len(c.StatusIDs) > 0 || len(c.ExcludeStatusIDs) > 0 || c.Sprint != "" || c.Assignee != "") {
		return fmt.Errorf("%w: status, sprint, and assignee filters need a kanban project", ErrValidation)
	}
	if err := checkProjectStatuses(projectID, c.StatusIDs); err != nil {
		return err
	}
	if err := checkProjectStatuses(projectID, c.ExcludeStatusIDs); err != nil {
		return err
	}
	if c.MinPriority < 0 || c.MinPriority > 3 {
		return fmt.Errorf("%w: minimum priority must be 0-3", ErrValidation)
	}
	if err := checkProjectTags(projectID, c.TagsAny); err != nil {
		return err
	}
	if err := checkProjectTags(projectID, c.TagsNone); err != nil {
		return err
	}
	switch c.Sprint {
	case "", "none", "current", "any":
		c.SprintID = 0
	case "specific":
		if _, err := storage.GetProjectSprint(projectID, c.SprintID); err != nil {
			return fmt.Errorf("%w: unknown sprint", ErrValidation)
		}
	default:
		return fmt.Errorf("%w: unknown sprint filter %q", ErrValidation, c.Sprint)
	}
	switch c.Assignee {
	case "", "unassigned", "assigned":
		c.AssigneeID = 0
	case "user":
		if !isHumanProjectMember(projectID, c.AssigneeID) {
			return fmt.Errorf("%w: assignee must be a project member", ErrValidation)
		}
	default:
		return fmt.Errorf("%w: unknown assignee filter %q", ErrValidation, c.Assignee)
	}
	c.FieldKey = strings.TrimSpace(c.FieldKey)
	c.FieldValue = strings.TrimSpace(c.FieldValue)
	if c.FieldKey == "" {
		c.FieldValue = ""
	}
	if len(c.FieldKey) > 64 || len(c.FieldValue) > 200 {
		return fmt.Errorf("%w: custom field filter is too long", ErrValidation)
	}
	switch c.HasDue {
	case "", "yes", "no":
	default:
		return fmt.Errorf("%w: unknown due date filter %q", ErrValidation, c.HasDue)
	}
	switch c.Completion {
	case "", "open", "done":
	default:
		return fmt.Errorf("%w: unknown completion filter %q", ErrValidation, c.Completion)
	}
	switch c.TaskKind {
	case "", "root", "subtask":
	default:
		return fmt.Errorf("%w: unknown task filter %q", ErrValidation, c.TaskKind)
	}
	return nil
}

func normalizeAction(projectID int, kanban bool, a *AutomationAction) error {
	a.Type = strings.TrimSpace(a.Type)
	if kanbanOnlyActions[a.Type] && !kanban {
		return fmt.Errorf("%w: %s needs a kanban project", ErrValidation, a.Type)
	}
	out := AutomationAction{Type: a.Type}
	switch a.Type {
	case AutomationActSetStatus:
		if err := checkProjectStatuses(projectID, []int{a.StatusID}); err != nil || a.StatusID <= 0 {
			return fmt.Errorf("%w: choose a status", ErrValidation)
		}
		out.StatusID = a.StatusID
	case AutomationActSetPriority:
		if a.Priority == nil || *a.Priority < 0 || *a.Priority > 3 {
			return fmt.Errorf("%w: priority must be 0-3", ErrValidation)
		}
		p := *a.Priority
		out.Priority = &p
	case AutomationActAddTag, AutomationActRemoveTag:
		if a.TagID <= 0 {
			return fmt.Errorf("%w: choose a tag", ErrValidation)
		}
		if err := checkProjectTags(projectID, []int{a.TagID}); err != nil {
			return err
		}
		out.TagID = a.TagID
	case AutomationActAssign:
		if !isHumanProjectMember(projectID, a.UserID) {
			return fmt.Errorf("%w: assignee must be a project member", ErrValidation)
		}
		out.UserID = a.UserID
	case AutomationActSetSprint:
		switch a.Sprint {
		case "current", "next", "backlog":
		case "specific":
			if _, err := storage.GetProjectSprint(projectID, a.SprintID); err != nil {
				return fmt.Errorf("%w: unknown sprint", ErrValidation)
			}
			out.SprintID = a.SprintID
		default:
			return fmt.Errorf("%w: choose current, next, backlog, or a specific sprint", ErrValidation)
		}
		out.Sprint = a.Sprint
	case AutomationActSetDue:
		if a.Days == nil || *a.Days < 0 || *a.Days > MaxAutomationDays {
			return fmt.Errorf("%w: due date offset must be 0-%d days", ErrValidation, MaxAutomationDays)
		}
		d := *a.Days
		out.Days = &d
	case AutomationActComplete, AutomationActReopen, AutomationActArchive, AutomationActUnassign:
	case AutomationActComment:
		body := strings.TrimSpace(a.Body)
		if body == "" {
			return fmt.Errorf("%w: comment text is required", ErrValidation)
		}
		if utf8.RuneCountInString(body) > MaxAutomationTextLen {
			return fmt.Errorf("%w: comment must be %d characters or less", ErrValidation, MaxAutomationTextLen)
		}
		out.Body = body
	case AutomationActNotify:
		switch a.Target {
		case "watchers", "assignee":
		default:
			return fmt.Errorf("%w: notify watchers or the assignee", ErrValidation)
		}
		body := strings.TrimSpace(a.Body)
		if body == "" {
			return fmt.Errorf("%w: notification text is required", ErrValidation)
		}
		if utf8.RuneCountInString(body) > 200 {
			return fmt.Errorf("%w: notification must be 200 characters or less", ErrValidation)
		}
		out.Target, out.Body = a.Target, body
	case AutomationActQueueAgent:
		ag, err := storage.GetProjectAgent(projectID, a.AgentID)
		if err != nil || ag == nil {
			return fmt.Errorf("%w: choose an AI agent in this project", ErrValidation)
		}
		out.AgentID = a.AgentID
		if note := strings.TrimSpace(a.Body); note != "" {
			out.Body = truncateString(note, MaxAgentRunNoteLen)
		}
	default:
		return fmt.Errorf("%w: unknown action %q", ErrValidation, a.Type)
	}
	*a = out
	return nil
}

func checkProjectStatuses(projectID int, ids []int) error {
	if len(ids) == 0 {
		return nil
	}
	list, err := storage.ListProjectStatuses(projectID)
	if err != nil {
		return err
	}
	ok := map[int]bool{}
	for _, s := range list {
		ok[s.ID] = true
	}
	for _, id := range ids {
		if !ok[id] {
			return fmt.Errorf("%w: unknown status %d", ErrValidation, id)
		}
	}
	return nil
}

func checkProjectTags(projectID int, ids []int) error {
	if len(ids) == 0 {
		return nil
	}
	list, err := storage.GetProjectTags(projectID)
	if err != nil {
		return err
	}
	ok := map[int]bool{}
	for _, t := range list {
		if !t.Protected {
			ok[t.ID] = true
		}
	}
	for _, id := range ids {
		if !ok[id] {
			return fmt.Errorf("%w: unknown tag %d", ErrValidation, id)
		}
	}
	return nil
}

func isHumanProjectMember(projectID, userID int) bool {
	if userID <= 0 {
		return false
	}
	members, err := storage.ListProjectMembers(projectID)
	if err != nil {
		return false
	}
	for _, m := range members {
		if m.UserID == userID && !m.IsAgent {
			return true
		}
	}
	return false
}

// --- engine ---

var automationCache struct {
	sync.Mutex
	at     time.Time
	active bool
}

// automationActive is cached briefly so sites without rules pay nothing per event.
func automationActive() bool {
	automationCache.Lock()
	defer automationCache.Unlock()
	if time.Since(automationCache.at) < 30*time.Second {
		return automationCache.active
	}
	n, err := storage.CountEnabledAutomationRules()
	automationCache.active = err == nil && n > 0
	automationCache.at = time.Now()
	return automationCache.active
}

func invalidateAutomationCache() {
	automationCache.Lock()
	automationCache.at = time.Time{}
	automationCache.Unlock()
}

// StartAutomation hooks project rules into the task event stream and starts
// the worker for timed rules.
func StartAutomation() {
	hooks.RegisterSink(hooks.Sink{
		Name:   "automation",
		Active: automationActive,
		Handle: HandleAutomationEvent,
	})
	go func() {
		RunTimedAutomation()
		ticker := time.NewTicker(15 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			RunTimedAutomation()
		}
	}()
}

// HandleAutomationEvent runs a project's event rules for ev. Changes made by
// the automation account never trigger rules, so rules cannot chain or loop.
func HandleAutomationEvent(ev hooks.Event) {
	if ev.TaskID <= 0 || ev.ProjectID <= 0 || storage.IsSystemUser(ev.ActorID) {
		return
	}
	info, ok := automationTrigger(ev.Type)
	if !ok || info.Timed {
		return
	}
	runAutomationEvent(ev)
}

// triggerAutomationUnblocked runs task.unblocked rules for a task whose last
// blocker was just completed by actorID.
func triggerAutomationUnblocked(actorID, taskID int) {
	if storage.IsSystemUser(actorID) || !automationActive() {
		return
	}
	pid, err := storage.GetTaskProjectID(taskID)
	if err != nil || pid <= 0 {
		return
	}
	ev := hooks.Event{Type: AutomationOnUnblocked, TaskID: taskID, ProjectID: pid, ActorID: actorID}
	automationAsync(func() { runAutomationEvent(ev) })
}

// automationAsync runs f off the request path; tests swap it for a direct call.
var automationAsync = func(f func()) { go f() }

func runAutomationEvent(ev hooks.Event) {
	rules, err := storage.ListEnabledAutomationRules(ev.ProjectID, []string{ev.Type})
	if err != nil || len(rules) == 0 {
		return
	}
	if projectArchived(ev.ProjectID) || storage.TaskIsArchived(ev.TaskID) {
		return
	}
	snap, err := storage.GetHookTaskSnapshot(ev.TaskID)
	if err != nil || snap == nil || snap.ProjectID != ev.ProjectID {
		return
	}
	for i := range rules {
		rule := &rules[i]
		var cfg AutomationTriggerConfig
		_ = json.Unmarshal(rule.TriggerConfig, &cfg)
		if !eventMatchesTrigger(ev, cfg, snap) {
			continue
		}
		runAutomationRule(rule, snap, ev.Type)
		// Later rules see the task as earlier rules left it.
		if fresh, err := storage.GetHookTaskSnapshot(ev.TaskID); err == nil && fresh != nil {
			snap = fresh
		}
	}
}

func projectArchived(projectID int) bool {
	return storage.ProjectIsArchived(projectID)
}

// eventMatchesTrigger applies a trigger's own narrowing (from/to status, tags added).
func eventMatchesTrigger(ev hooks.Event, cfg AutomationTriggerConfig, snap *storage.HookTaskSnapshot) bool {
	switch ev.Type {
	case AutomationOnStatusChanged:
		if len(cfg.ToStatusIDs) > 0 && !containsInt(cfg.ToStatusIDs, snap.StatusID) {
			return false
		}
		if len(cfg.FromStatusIDs) > 0 {
			from := strings.TrimSpace(ev.OldStatus)
			if from == "" {
				return false
			}
			names := statusNamesByID(snap.ProjectID)
			matched := false
			for _, id := range cfg.FromStatusIDs {
				if strings.EqualFold(names[id], from) {
					matched = true
					break
				}
			}
			if !matched {
				return false
			}
		}
	case AutomationOnTagged:
		if len(cfg.TagIDs) > 0 {
			added := addedTagIDs(ev)
			if added == nil {
				// No before/after detail: fall back to the task's current tags.
				added = snap.TagIDs
			}
			hit := false
			for _, id := range cfg.TagIDs {
				if containsInt(added, id) {
					hit = true
					break
				}
			}
			if !hit {
				return false
			}
		}
	}
	return true
}

// addedTagIDs reads the tag ids added by a task.tagged event, or nil when unknown.
func addedTagIDs(ev hooks.Event) []int {
	for _, fc := range ev.FieldChanges {
		if fc.Field != "tags" {
			continue
		}
		before := parseIDList(fc.Old)
		out := []int{}
		for _, id := range parseIDList(fc.New) {
			if !containsInt(before, id) {
				out = append(out, id)
			}
		}
		return out
	}
	return nil
}

func parseIDList(s string) []int {
	out := []int{}
	for _, part := range strings.Split(s, ",") {
		if id, err := strconv.Atoi(strings.TrimSpace(part)); err == nil && id > 0 {
			out = append(out, id)
		}
	}
	return out
}

func statusNamesByID(projectID int) map[int]string {
	out := map[int]string{}
	list, err := storage.ListProjectStatuses(projectID)
	if err != nil {
		return out
	}
	for _, s := range list {
		out[s.ID] = s.Name
	}
	return out
}

// RunTimedAutomation runs every enabled timed rule once. It is safe to call
// directly (tests, admin tools); the worker calls it every 15 minutes.
func RunTimedAutomation() {
	if !automationActive() {
		return
	}
	rules, err := storage.ListEnabledTimedAutomationRules(timedAutomationTriggerTypes())
	if err != nil {
		log.Printf("automation: list timed rules: %v", err)
		return
	}
	for i := range rules {
		runTimedRule(&rules[i])
	}
}

func runTimedRule(rule *storage.AutomationRule) {
	var cfg AutomationTriggerConfig
	_ = json.Unmarshal(rule.TriggerConfig, &cfg)
	cands, err := storage.ListAutomationCandidates(storage.AutomationTimedQuery{
		ProjectID: rule.ProjectID, RuleID: rule.ID, TriggerType: rule.TriggerType,
		Days: cfg.Days, Limit: automationTimedBatch,
	})
	if err != nil {
		log.Printf("automation: rule %d candidates: %v", rule.ID, err)
		return
	}
	for _, c := range cands {
		snap, err := storage.GetHookTaskSnapshot(c.TaskID)
		if err != nil || snap == nil {
			continue
		}
		var cond AutomationConditions
		_ = json.Unmarshal(rule.Conditions, &cond)
		if !conditionsMatch(cond, snap) {
			continue
		}
		// Mark first so a failing action does not retry every pass.
		if err := storage.MarkAutomationEpisode(rule.ID, c.TaskID, c.Episode); err != nil {
			continue
		}
		if !runAutomationRule(rule, snap, rule.TriggerType) {
			return // paused
		}
	}
}

// runAutomationRule checks conditions and applies a rule's actions to one
// task. It returns false when the rule is (now) paused.
func runAutomationRule(rule *storage.AutomationRule, snap *storage.HookTaskSnapshot, trigger string) bool {
	var cond AutomationConditions
	_ = json.Unmarshal(rule.Conditions, &cond)
	if !conditionsMatch(cond, snap) {
		return true
	}
	var actions []AutomationAction
	if err := json.Unmarshal(rule.Actions, &actions); err != nil || len(actions) == 0 {
		return true
	}
	if n, err := storage.CountRecentAutomationRuns(rule.ID, time.Now().Add(-time.Hour)); err == nil && n >= AutomationRunsPerHourLimit {
		pauseAutomationRule(rule, fmt.Sprintf("Paused after more than %d runs in an hour.", AutomationRunsPerHourLimit))
		return false
	}

	sys := storage.SystemUserID()
	if sys <= 0 {
		return true
	}
	changes := []AutomationChange{}
	var errs []string
	for _, a := range actions {
		ch, err := applyAutomationAction(sys, rule, a, snap)
		if err != nil {
			errs = append(errs, err.Error())
			changes = append(changes, AutomationChange{Action: a.Type, Error: automationErrorText(err)})
			continue
		}
		if ch == nil {
			continue // already in the wanted state
		}
		changes = append(changes, *ch)
		if fresh, err := storage.GetHookTaskSnapshot(snap.ID); err == nil && fresh != nil {
			snap = fresh
		}
	}
	applied := 0
	for _, c := range changes {
		if c.Error == "" {
			applied++
		}
	}
	if applied == 0 && len(errs) == 0 {
		return true
	}
	outcome := storage.AutomationRunApplied
	errText := ""
	if len(errs) > 0 {
		outcome = storage.AutomationRunError
		errText = truncateString(strings.Join(errs, "; "), 500)
	}
	raw, _ := json.Marshal(changes)
	streak, err := storage.RecordAutomationRun(storage.AutomationRuleRun{
		RuleID: rule.ID, ProjectID: rule.ProjectID, TaskID: snap.ID, Trigger: trigger,
		Outcome: outcome, Changes: raw, Error: errText,
	})
	if err != nil {
		log.Printf("automation: record run rule=%d task=%d: %v", rule.ID, snap.ID, err)
	}
	if applied > 0 {
		_ = storage.LogTaskEvent(snap.ID, sys, "automation_rule", map[string]interface{}{
			"rule_id": rule.ID, "rule_name": rule.Name, "trigger": trigger,
		})
	}
	if streak >= AutomationErrorStreakLimit {
		pauseAutomationRule(rule, fmt.Sprintf("Paused after %d failed runs in a row. Last error: %s",
			AutomationErrorStreakLimit, errText))
		return false
	}
	return true
}

func automationErrorText(err error) string {
	msg := err.Error()
	for _, prefix := range []string{ErrValidation.Error() + ": ", ErrForbidden.Error() + ": ", ErrConflict.Error() + ": "} {
		msg = strings.TrimPrefix(msg, prefix)
	}
	return truncateString(msg, 300)
}

func pauseAutomationRule(rule *storage.AutomationRule, reason string) {
	if err := storage.PauseAutomationRule(rule.ID, reason); err != nil {
		log.Printf("automation: pause rule %d: %v", rule.ID, err)
		return
	}
	rule.Enabled = false
	invalidateAutomationCache()
	_ = storage.LogProjectEvent(rule.ProjectID, storage.SystemUserID(), "automation_rule_paused", map[string]interface{}{
		"rule_id": rule.ID, "name": rule.Name, "reason": reason,
	})
	live.AfterProjectChange(storage.SystemUserID(), rule.ProjectID, live.TypeProjectUpdated)
}

// conditionsMatch reports whether a task passes every set filter.
func conditionsMatch(c AutomationConditions, snap *storage.HookTaskSnapshot) bool {
	if len(c.StatusIDs) > 0 && !containsInt(c.StatusIDs, snap.StatusID) {
		return false
	}
	if len(c.ExcludeStatusIDs) > 0 && containsInt(c.ExcludeStatusIDs, snap.StatusID) {
		return false
	}
	if c.MinPriority > 0 && snap.Priority < c.MinPriority {
		return false
	}
	if len(c.TagsAny) > 0 {
		hit := false
		for _, id := range c.TagsAny {
			if containsInt(snap.TagIDs, id) {
				hit = true
				break
			}
		}
		if !hit {
			return false
		}
	}
	for _, id := range c.TagsNone {
		if containsInt(snap.TagIDs, id) {
			return false
		}
	}
	switch c.Sprint {
	case "none":
		if snap.SprintID != 0 {
			return false
		}
	case "any":
		if snap.SprintID == 0 {
			return false
		}
	case "specific":
		if snap.SprintID != c.SprintID {
			return false
		}
	case "current":
		cur := currentSprintID(snap.ProjectID, time.Now())
		if cur == 0 || snap.SprintID != cur {
			return false
		}
	}
	switch c.Assignee {
	case "unassigned":
		if snap.ClaimedBy != 0 {
			return false
		}
	case "assigned":
		if snap.ClaimedBy == 0 {
			return false
		}
	case "user":
		if snap.ClaimedBy != c.AssigneeID {
			return false
		}
	}
	if c.FieldKey != "" && !strings.EqualFold(strings.TrimSpace(snap.CustomFields[c.FieldKey]), c.FieldValue) {
		return false
	}
	switch c.HasDue {
	case "yes":
		if snap.DueDate == "" {
			return false
		}
	case "no":
		if snap.DueDate != "" {
			return false
		}
	}
	switch c.Completion {
	case "open":
		if snap.Completed {
			return false
		}
	case "done":
		if !snap.Completed {
			return false
		}
	}
	switch c.TaskKind {
	case "root":
		if snap.ParentID != 0 {
			return false
		}
	case "subtask":
		if snap.ParentID == 0 {
			return false
		}
	}
	return true
}

// currentSprintID returns the dated sprint running on now, or 0.
func currentSprintID(projectID int, now time.Time) int {
	list, err := storage.ListProjectSprints(projectID)
	if err != nil {
		return 0
	}
	for _, s := range list {
		if s.StartDate != nil && s.EndDate != nil && storage.SprintIsActive(s.StartDate, s.EndDate, now) {
			return s.ID
		}
	}
	return 0
}

// nextSprintID returns the earliest sprint starting after now, or 0.
func nextSprintID(projectID int, now time.Time) int {
	list, err := storage.ListProjectSprints(projectID)
	if err != nil {
		return 0
	}
	today := storage.FormatSprintDate(now)
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].StartDate == nil || list[j].StartDate == nil {
			return list[j].StartDate == nil && list[i].StartDate != nil
		}
		return list[i].StartDate.Before(*list[j].StartDate)
	})
	for _, s := range list {
		if s.StartDate != nil && storage.FormatSprintDate(*s.StartDate) > today {
			return s.ID
		}
	}
	return 0
}

func sprintName(projectID, sprintID int) string {
	if sprintID <= 0 {
		return "Backlog"
	}
	if s, err := storage.GetProjectSprint(projectID, sprintID); err == nil && s != nil {
		return s.Name
	}
	return "#" + strconv.Itoa(sprintID)
}

func tagName(projectID, tagID int) string {
	list, err := storage.GetProjectTags(projectID)
	if err == nil {
		for _, t := range list {
			if t.ID == tagID {
				return t.Name
			}
		}
	}
	return "#" + strconv.Itoa(tagID)
}

// applyAutomationAction performs one action as the automation account. It
// returns nil, nil when the task is already in the wanted state.
func applyAutomationAction(sys int, rule *storage.AutomationRule, a AutomationAction, snap *storage.HookTaskSnapshot) (*AutomationChange, error) {
	ctx := context.Background()
	taskID := snap.ID
	switch a.Type {
	case AutomationActSetStatus:
		if snap.StatusID == a.StatusID {
			return nil, nil
		}
		names := statusNamesByID(snap.ProjectID)
		if _, ok := names[a.StatusID]; !ok {
			return nil, fmt.Errorf("%w: status no longer exists", ErrValidation)
		}
		id := a.StatusID
		idp := &id
		if _, err := UpdateTask(ctx, sys, taskID, UpdateTaskInput{StatusID: &idp}); err != nil {
			return nil, err
		}
		return &AutomationChange{Action: a.Type, Field: "status", From: names[snap.StatusID], To: names[a.StatusID]}, nil

	case AutomationActSetPriority:
		if a.Priority == nil || snap.Priority == *a.Priority {
			return nil, nil
		}
		p := *a.Priority
		if _, err := UpdateTask(ctx, sys, taskID, UpdateTaskInput{Priority: &p}); err != nil {
			return nil, err
		}
		return &AutomationChange{Action: a.Type, Field: "priority", From: priorityEventLabel(snap.Priority), To: priorityEventLabel(p)}, nil

	case AutomationActAddTag, AutomationActRemoveTag:
		has := containsInt(snap.TagIDs, a.TagID)
		if (a.Type == AutomationActAddTag) == has {
			return nil, nil
		}
		current, err := storage.GetTagsForTask(taskID)
		if err != nil {
			return nil, err
		}
		ids := []int{}
		for _, t := range current {
			if t.Protected || t.ID == a.TagID {
				continue
			}
			ids = append(ids, t.ID)
		}
		if a.Type == AutomationActAddTag {
			if err := checkProjectTags(snap.ProjectID, []int{a.TagID}); err != nil {
				return nil, fmt.Errorf("%w: tag no longer exists", ErrValidation)
			}
			ids = append(ids, a.TagID)
		}
		if _, err := UpdateTask(ctx, sys, taskID, UpdateTaskInput{TagIDs: &ids}); err != nil {
			return nil, err
		}
		ch := &AutomationChange{Action: a.Type, Field: "tags"}
		if a.Type == AutomationActAddTag {
			ch.To = tagName(snap.ProjectID, a.TagID)
		} else {
			ch.From = tagName(snap.ProjectID, a.TagID)
		}
		return ch, nil

	case AutomationActAssign:
		if snap.ClaimedBy == a.UserID {
			return nil, nil
		}
		if !isHumanProjectMember(snap.ProjectID, a.UserID) {
			return nil, fmt.Errorf("%w: assignee is no longer a project member", ErrValidation)
		}
		if err := assignTaskAsAutomation(sys, taskID, a.UserID, snap.ClaimedBy); err != nil {
			return nil, err
		}
		name, email := storage.UserNameAndEmail(a.UserID)
		if name == "" {
			name = email
		}
		return &AutomationChange{Action: a.Type, Field: "assignee", From: snap.ClaimedByName, To: name}, nil

	case AutomationActUnassign:
		if snap.ClaimedBy == 0 {
			return nil, nil
		}
		if err := UnclaimTaskForUser(ctx, sys, taskID); err != nil {
			return nil, err
		}
		return &AutomationChange{Action: a.Type, Field: "assignee", From: snap.ClaimedByName}, nil

	case AutomationActSetSprint:
		target := 0
		switch a.Sprint {
		case "current":
			target = currentSprintID(snap.ProjectID, time.Now())
			if target == 0 {
				return nil, fmt.Errorf("%w: no sprint is running", ErrValidation)
			}
		case "next":
			target = nextSprintID(snap.ProjectID, time.Now())
			if target == 0 {
				return nil, fmt.Errorf("%w: no upcoming sprint", ErrValidation)
			}
		case "specific":
			target = a.SprintID
		}
		if snap.SprintID == target {
			return nil, nil
		}
		sp := &target
		if target == 0 {
			sp = nil
		}
		if _, err := UpdateTask(ctx, sys, taskID, UpdateTaskInput{SprintID: &sp}); err != nil {
			return nil, err
		}
		return &AutomationChange{Action: a.Type, Field: "sprint",
			From: sprintName(snap.ProjectID, snap.SprintID), To: sprintName(snap.ProjectID, target)}, nil

	case AutomationActSetDue:
		days := 0
		if a.Days != nil {
			days = *a.Days
		}
		loc, err := time.LoadLocation(storage.UserTimezone(snap.OwnerID))
		if err != nil {
			loc = time.UTC
		}
		due := time.Now().In(loc).AddDate(0, 0, days).Format("2006-01-02")
		if snap.DueDate == due {
			return nil, nil
		}
		if _, err := UpdateTask(ctx, sys, taskID, UpdateTaskInput{DueDate: &due}); err != nil {
			return nil, err
		}
		return &AutomationChange{Action: a.Type, Field: "due_date", From: snap.DueDate, To: due}, nil

	case AutomationActComplete, AutomationActReopen:
		want := a.Type == AutomationActComplete
		if snap.Completed == want {
			return nil, nil
		}
		if err := SetTaskCompleted(ctx, sys, taskID, want); err != nil {
			return nil, err
		}
		return &AutomationChange{Action: a.Type, Field: "completed",
			From: strconv.FormatBool(snap.Completed), To: strconv.FormatBool(want)}, nil

	case AutomationActArchive:
		if storage.TaskIsArchived(taskID) {
			return nil, nil
		}
		if err := ArchiveTask(ctx, sys, taskID); err != nil {
			return nil, err
		}
		return &AutomationChange{Action: a.Type, Field: "archived", To: "true"}, nil

	case AutomationActComment:
		body := hooks.Interpolate(a.Body, automationVars(rule, snap))
		if strings.TrimSpace(body) == "" {
			return nil, nil
		}
		if _, err := AddCommentForUser(ctx, sys, taskID, body); err != nil {
			return nil, err
		}
		return &AutomationChange{Action: a.Type, To: truncateString(body, 200)}, nil

	case AutomationActNotify:
		msg := truncateString(strings.TrimSpace(hooks.Interpolate(a.Body, automationVars(rule, snap))), 200)
		if msg == "" {
			return nil, nil
		}
		body := "Automation · " + rule.Name
		if a.Target == "assignee" {
			if snap.ClaimedBy <= 0 {
				return nil, nil
			}
			id, err := storage.CreateUserNotification(storage.UserNotification{
				UserID: snap.ClaimedBy, ActorUserID: sys, Type: NotificationTaskActivity,
				ProjectID: snap.ProjectID, TaskID: taskID, Title: msg, Body: body,
			})
			if err != nil {
				return nil, err
			}
			if id > 0 {
				live.Push(live.Event{Type: live.TypeNotificationCreated, TaskID: taskID}, []int{snap.ClaimedBy})
			}
		} else {
			notifyTaskWatchers(sys, taskID, NotificationTaskActivity, msg, body)
		}
		return &AutomationChange{Action: a.Type, Field: a.Target, To: msg}, nil

	case AutomationActQueueAgent:
		ag, err := storage.GetProjectAgent(snap.ProjectID, a.AgentID)
		if err != nil || ag == nil {
			return nil, fmt.Errorf("%w: AI agent was removed", ErrValidation)
		}
		note := a.Body
		if note == "" {
			note = "Queued by automation rule: " + rule.Name
		}
		run, err := queueAgentRun(ag, taskID, storage.AgentTriggerAutomation, sys, note)
		if err != nil {
			if errors.Is(err, ErrConflict) && strings.Contains(err.Error(), "already working") {
				return nil, nil
			}
			return nil, err
		}
		return &AutomationChange{Action: a.Type, Field: "agent", To: ag.Name + " · run #" + strconv.Itoa(run.ID)}, nil
	}
	return nil, fmt.Errorf("%w: unknown action %q", ErrValidation, a.Type)
}

// assignTaskAsAutomation sets claimed_by to a member on the automation
// account's behalf, mirroring ClaimTaskForUser's logging and notices.
func assignTaskAsAutomation(sys, taskID, assigneeID, prev int) error {
	canRead, role, projectID, err := storage.CanUserAccessTask(taskID, sys)
	if err != nil {
		return err
	}
	if !canRead {
		return ErrNotFound
	}
	if err := denyMissingTaskPerm(projectID, role, storage.PermTasksClaim); err != nil {
		return err
	}
	if mode, err := storage.GetProjectWorkflowMode(projectID); err != nil || mode != storage.WorkflowKanban {
		return fmt.Errorf("%w: only kanban project tasks can be assigned", ErrValidation)
	}
	claimer := assigneeID
	if err := storage.SetTaskClaimedBy(taskID, &claimer); err != nil {
		return err
	}
	meta := map[string]interface{}{"claimed_by": assigneeID}
	if prev > 0 {
		meta["previous_claimed_by"] = prev
	}
	_ = storage.LogTaskEvent(taskID, sys, "claimed", meta)
	live.AfterTaskChangeLive(sys, taskID, live.TypeTaskUpdated)
	live.DispatchHook(sys, taskID, live.TypeTaskClaimed, &live.TaskHookMeta{Changed: []string{"claimed_by"}})
	autoWatchTask(assigneeID, taskID)
	if _, err := storage.CreateUserNotification(storage.UserNotification{
		UserID: assigneeID, ActorUserID: sys, Type: NotificationTaskActivity, ProjectID: projectID,
		TaskID: taskID, Title: "Assigned to you: " + taskTitleOrID(taskID),
	}); err == nil {
		live.Push(live.Event{Type: live.TypeNotificationCreated, TaskID: taskID}, []int{assigneeID})
	}
	notifyTaskWatchers(sys, taskID, NotificationTaskActivity, "Claimed: "+taskTitleOrID(taskID), "")
	return nil
}

// automationVars are the {placeholders} available to comment and notify text.
func automationVars(rule *storage.AutomationRule, snap *storage.HookTaskSnapshot) map[string]string {
	assignee := ""
	if snap.ClaimedBy > 0 {
		if name, _ := storage.UserNameAndEmail(snap.ClaimedBy); name != "" {
			assignee = "@" + name
		} else {
			assignee = snap.ClaimedByName
		}
	}
	return map[string]string{
		"task":        snap.Title,
		"id":          strconv.Itoa(snap.ID),
		"status":      snap.StatusName,
		"priority":    priorityEventLabel(snap.Priority),
		"due_date":    snap.DueDate,
		"assignee":    assignee,
		"claimed_by":  assignee,
		"sprint":      snap.SprintName,
		"project":     snap.ProjectName,
		"tags":        strings.Join(snap.Tags, ", "),
		"rule":        rule.Name,
		"today":       time.Now().UTC().Format("2006-01-02"),
		"url":         hooks.PublicTaskURL(snap.ID),
		"description": truncateString(snap.Description, 500),
	}
}

// --- dry run ---

// PreviewAutomationRule lists tasks a rule would act on right now. Timed rules
// list their next batch; event rules list open tasks passing the "if" filters.
func PreviewAutomationRule(userID, projectID int, in AutomationRuleInput) ([]AutomationPreviewTask, error) {
	proj, err := requireProjectManage(projectID, userID)
	if err != nil {
		return nil, err
	}
	r := storage.AutomationRule{ProjectID: projectID, Name: "Preview", TriggerConfig: json.RawMessage(`{}`),
		Conditions: json.RawMessage(`{}`), Actions: json.RawMessage(`[]`)}
	// Only the trigger and filters decide which tasks match, so an
	// unfinished action list must not block a dry run.
	noop := []AutomationAction{{Type: AutomationActComplete}}
	in.Actions = &noop
	if in.Name == nil {
		name := "Preview"
		in.Name = &name
	}
	if err := applyAutomationInput(&r, proj.WorkflowMode, in); err != nil {
		return nil, err
	}
	var cond AutomationConditions
	_ = json.Unmarshal(r.Conditions, &cond)
	info, _ := automationTrigger(r.TriggerType)

	var ids []int
	if info.Timed {
		var cfg AutomationTriggerConfig
		_ = json.Unmarshal(r.TriggerConfig, &cfg)
		// RuleID 0 has no marks, so this shows every task in its current episode.
		cands, err := storage.ListAutomationCandidates(storage.AutomationTimedQuery{
			ProjectID: projectID, TriggerType: r.TriggerType, Days: cfg.Days, Limit: automationPreviewScan,
		})
		if err != nil {
			return nil, err
		}
		for _, c := range cands {
			ids = append(ids, c.TaskID)
		}
	} else {
		ids, err = storage.ListProjectTaskIDs(projectID, automationPreviewScan)
		if err != nil {
			return nil, err
		}
	}
	out := []AutomationPreviewTask{}
	for _, id := range ids {
		snap, err := storage.GetHookTaskSnapshot(id)
		if err != nil || snap == nil || !conditionsMatch(cond, snap) {
			continue
		}
		if !info.Timed && storage.TaskIsArchived(id) {
			continue
		}
		out = append(out, AutomationPreviewTask{ID: snap.ID, Title: snap.Title})
		if len(out) >= automationPreviewLimit {
			break
		}
	}
	return out, nil
}
