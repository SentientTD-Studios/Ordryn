package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"GoTodo/internal/crypto/secret"
	"GoTodo/internal/hooks"
	"GoTodo/internal/live"
	"GoTodo/internal/storage"
	"GoTodo/internal/username"

	"github.com/google/uuid"
)

// Agent limits.
const (
	MaxAgentNameLen         = 80
	MaxAgentDescriptionLen  = 500
	MaxAgentInstructionsLen = 8000
	MaxAgentRunNoteLen      = 2000
	MaxAgentRunSummaryLen   = 4000
	MaxAgentRunsPerHour     = 500
	DefaultAgentRunsPerHour = 20
	MaxProjectAgents        = 10
)

// AgentInput creates or patches an agent. Nil fields are left unchanged on
// patch and take their defaults on create. Handle is only used on create.
type AgentInput struct {
	Handle       string
	Name         *string
	Description  *string
	Instructions *string
	Enabled      *bool
	Role         *string
	WebhookURL   *string

	TriggerOnMention *bool
	TriggerStatusIDs *[]int
	TriggerBy        *string
	TriggerRoleSlugs *[]string
	TriggerUserIDs   *[]int
	ClaimOnDispatch  *bool

	AllowedStatusIDs *[]int
	EditableFields   *[]string
	CanComplete      *bool
	CanCreateTasks   *bool
	CanComment       *bool
	MaxRunsPerHour   *int
}

// ListProjectAgents returns a project's agents for a manager.
func ListProjectAgents(userID, projectID int) ([]storage.ProjectAgent, error) {
	if _, err := requireProjectManage(projectID, userID); err != nil {
		return nil, err
	}
	return storage.ListProjectAgents(projectID)
}

// GetProjectAgent returns one agent for a manager.
func GetProjectAgent(userID, projectID, agentID int) (*storage.ProjectAgent, error) {
	if _, err := requireProjectManage(projectID, userID); err != nil {
		return nil, err
	}
	return loadAgent(projectID, agentID)
}

func loadAgent(projectID, agentID int) (*storage.ProjectAgent, error) {
	a, err := storage.GetProjectAgent(projectID, agentID)
	if errors.Is(err, storage.ErrAgentNotFound) {
		return nil, ErrNotFound
	}
	return a, err
}

// CreateProjectAgent adds an AI agent member to a project.
func CreateProjectAgent(userID, projectID int, in AgentInput) (*storage.ProjectAgent, error) {
	proj, err := requireProjectManage(projectID, userID)
	if err != nil {
		return nil, err
	}
	if proj.Archived {
		return nil, fmt.Errorf("%w: restore the project before adding agents", ErrValidation)
	}
	existing, err := storage.ListProjectAgents(projectID)
	if err != nil {
		return nil, err
	}
	if len(existing) >= MaxProjectAgents {
		return nil, fmt.Errorf("%w: a project can have at most %d agents", ErrValidation, MaxProjectAgents)
	}

	handle := username.Normalize(in.Handle)
	if msg := username.FormatError(handle); msg != "" {
		return nil, fmt.Errorf("%w: handle: %s", ErrValidation, msg)
	}
	if taken, err := storage.UsernameTaken(handle, 0); err != nil {
		return nil, err
	} else if taken {
		return nil, fmt.Errorf("%w: the handle @%s is already taken", ErrConflict, handle)
	}

	a := storage.ProjectAgent{
		ProjectID:        projectID,
		Handle:           handle,
		Enabled:          true,
		CreatedBy:        userID,
		TriggerOnMention: true,
		TriggerStatusIDs: []int{},
		TriggerBy:        storage.AgentTriggerByManagers,
		ClaimOnDispatch:  true,
		AllowedStatusIDs: []int{},
		EditableFields:   []string{storage.AgentFieldStatus},
		CanComment:       true,
		MaxRunsPerHour:   DefaultAgentRunsPerHour,
	}
	if in.Name == nil || strings.TrimSpace(*in.Name) == "" {
		a.Name = handle
	}
	if err := applyAgentInput(&a, in); err != nil {
		return nil, err
	}
	// There is no site-wide default role to fall back on, so the manager must pick one.
	if a.Role == "" {
		return nil, fmt.Errorf("%w: choose a role for the agent", ErrValidation)
	}
	created, err := storage.CreateProjectAgent(a)
	if errors.Is(err, storage.ErrUsernameTaken) {
		return nil, fmt.Errorf("%w: the handle @%s is already taken", ErrConflict, handle)
	}
	if err != nil {
		return nil, err
	}
	invalidateAgentCache()
	_ = storage.LogProjectEvent(projectID, userID, "agent_added", map[string]interface{}{
		"agent_id": created.ID, "name": created.Name, "handle": created.Handle, "role": created.Role,
	})
	live.AfterProjectChange(userID, projectID, live.TypeProjectUpdated)
	return created, nil
}

// UpdateProjectAgent changes an agent's settings, role, or guardrails.
func UpdateProjectAgent(userID, projectID, agentID int, in AgentInput) (*storage.ProjectAgent, error) {
	if _, err := requireProjectManage(projectID, userID); err != nil {
		return nil, err
	}
	a, err := loadAgent(projectID, agentID)
	if err != nil {
		return nil, err
	}
	oldRole := a.Role
	if err := applyAgentInput(a, in); err != nil {
		return nil, err
	}
	if err := storage.UpdateProjectAgent(*a); err != nil {
		if errors.Is(err, storage.ErrAgentNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if a.Role != oldRole {
		if err := storage.UpsertProjectMember(projectID, a.UserID, a.Role); err != nil {
			return nil, err
		}
	}
	invalidateAgentCache()
	_ = storage.LogProjectEvent(projectID, userID, "agent_updated", map[string]interface{}{
		"agent_id": a.ID, "name": a.Name,
	})
	live.AfterProjectChange(userID, projectID, live.TypeProjectUpdated)
	return loadAgent(projectID, agentID)
}

// RemoveProjectAgent retires an agent and revokes its access.
func RemoveProjectAgent(userID, projectID, agentID int) error {
	if _, err := requireProjectManage(projectID, userID); err != nil {
		return err
	}
	a, err := loadAgent(projectID, agentID)
	if err != nil {
		return err
	}
	if err := storage.RemoveProjectAgent(projectID, agentID); err != nil {
		if errors.Is(err, storage.ErrAgentNotFound) {
			return ErrNotFound
		}
		return err
	}
	invalidateAgentCache()
	_ = storage.LogProjectEvent(projectID, userID, "agent_removed", map[string]interface{}{
		"agent_id": a.ID, "name": a.Name, "handle": a.Handle,
	})
	live.AfterProjectChange(userID, projectID, live.TypeProjectUpdated)
	return nil
}

func applyAgentInput(a *storage.ProjectAgent, in AgentInput) error {
	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if name == "" {
			return fmt.Errorf("%w: name is required", ErrValidation)
		}
		if utf8.RuneCountInString(name) > MaxAgentNameLen {
			return fmt.Errorf("%w: name is too long", ErrValidation)
		}
		a.Name = name
	}
	if in.Description != nil {
		d := strings.TrimSpace(*in.Description)
		if utf8.RuneCountInString(d) > MaxAgentDescriptionLen {
			return fmt.Errorf("%w: description is too long (max %d characters)", ErrValidation, MaxAgentDescriptionLen)
		}
		a.Description = d
	}
	if in.Instructions != nil {
		s := strings.TrimSpace(*in.Instructions)
		if utf8.RuneCountInString(s) > MaxAgentInstructionsLen {
			return fmt.Errorf("%w: instructions are too long (max %d characters)", ErrValidation, MaxAgentInstructionsLen)
		}
		a.Instructions = s
	}
	if in.Enabled != nil {
		a.Enabled = *in.Enabled
	}
	if in.Role != nil {
		role := strings.ToLower(strings.TrimSpace(*in.Role))
		if !storage.ValidInviteRoleForProject(a.ProjectID, role) {
			return fmt.Errorf("%w: unknown role %q", ErrValidation, role)
		}
		if storage.HasProjectPerm(a.ProjectID, role, storage.PermProjectManage) {
			return fmt.Errorf("%w: agents cannot have a role that manages the project", ErrValidation)
		}
		a.Role = role
	}
	if in.WebhookURL != nil {
		u := strings.TrimSpace(*in.WebhookURL)
		if u != "" {
			if err := hooks.ValidateAgentWebhookURL(u); err != nil {
				return fmt.Errorf("%w: %s", ErrValidation, err.Error())
			}
		}
		a.WebhookURL = u
	}
	if in.TriggerOnMention != nil {
		a.TriggerOnMention = *in.TriggerOnMention
	}
	if in.TriggerStatusIDs != nil {
		ids, err := validProjectStatusIDs(a.ProjectID, *in.TriggerStatusIDs)
		if err != nil {
			return err
		}
		a.TriggerStatusIDs = ids
	}
	if in.TriggerBy != nil {
		switch v := strings.ToLower(strings.TrimSpace(*in.TriggerBy)); v {
		case storage.AgentTriggerByManagers, storage.AgentTriggerByWriters, storage.AgentTriggerBySelected:
			a.TriggerBy = v
		default:
			return fmt.Errorf("%w: trigger_by must be %q, %q, or %q", ErrValidation,
				storage.AgentTriggerByManagers, storage.AgentTriggerByWriters, storage.AgentTriggerBySelected)
		}
	}
	if in.TriggerRoleSlugs != nil {
		slugs, err := validTriggerRoles(a.ProjectID, *in.TriggerRoleSlugs)
		if err != nil {
			return err
		}
		a.TriggerRoleSlugs = slugs
	}
	if in.TriggerUserIDs != nil {
		ids, err := validTriggerUsers(a.ProjectID, *in.TriggerUserIDs)
		if err != nil {
			return err
		}
		a.TriggerUserIDs = ids
	}
	if a.TriggerBy == storage.AgentTriggerBySelected && len(a.TriggerRoleSlugs) == 0 && len(a.TriggerUserIDs) == 0 {
		return fmt.Errorf("%w: pick at least one role or member who can call this agent", ErrValidation)
	}
	if in.ClaimOnDispatch != nil {
		a.ClaimOnDispatch = *in.ClaimOnDispatch
	}
	if in.AllowedStatusIDs != nil {
		ids, err := validProjectStatusIDs(a.ProjectID, *in.AllowedStatusIDs)
		if err != nil {
			return err
		}
		a.AllowedStatusIDs = ids
	}
	if in.EditableFields != nil {
		fields, err := normalizeAgentFields(*in.EditableFields)
		if err != nil {
			return err
		}
		a.EditableFields = fields
	}
	if in.CanComplete != nil {
		a.CanComplete = *in.CanComplete
	}
	if in.CanCreateTasks != nil {
		a.CanCreateTasks = *in.CanCreateTasks
	}
	if in.CanComment != nil {
		a.CanComment = *in.CanComment
	}
	if in.MaxRunsPerHour != nil {
		n := *in.MaxRunsPerHour
		if n < 1 || n > MaxAgentRunsPerHour {
			return fmt.Errorf("%w: max_runs_per_hour must be between 1 and %d", ErrValidation, MaxAgentRunsPerHour)
		}
		a.MaxRunsPerHour = n
	}
	return nil
}

// validTriggerRoles keeps role slugs that exist on the project (owner included).
func validTriggerRoles(projectID int, raw []string) ([]string, error) {
	out := make([]string, 0, len(raw))
	seen := make(map[string]bool, len(raw))
	for _, s := range raw {
		slug := strings.ToLower(strings.TrimSpace(s))
		if slug == "" || seen[slug] {
			continue
		}
		if slug != storage.RoleOwner && !storage.ValidInviteRoleForProject(projectID, slug) {
			return nil, fmt.Errorf("%w: unknown role %q", ErrValidation, slug)
		}
		seen[slug] = true
		out = append(out, slug)
	}
	return out, nil
}

// validTriggerUsers keeps ids of current, human project members.
func validTriggerUsers(projectID int, raw []int) ([]int, error) {
	out := make([]int, 0, len(raw))
	seen := make(map[int]bool, len(raw))
	for _, id := range raw {
		if seen[id] {
			continue
		}
		role, err := storage.GetProjectRole(projectID, id)
		if err != nil {
			return nil, err
		}
		if id <= 0 || role == "" {
			return nil, fmt.Errorf("%w: user %d is not a member of this project", ErrValidation, id)
		}
		if storage.IsAgentUser(id) {
			return nil, fmt.Errorf("%w: agents cannot call other agents", ErrValidation)
		}
		seen[id] = true
		out = append(out, id)
	}
	return out, nil
}

func validProjectStatusIDs(projectID int, ids []int) ([]int, error) {
	out := make([]int, 0, len(ids))
	seen := make(map[int]bool, len(ids))
	for _, id := range ids {
		if seen[id] {
			continue
		}
		if _, err := storage.GetProjectStatus(projectID, id); err != nil {
			return nil, fmt.Errorf("%w: status %d is not a column on this board", ErrValidation, id)
		}
		seen[id] = true
		out = append(out, id)
	}
	return out, nil
}

func normalizeAgentFields(raw []string) ([]string, error) {
	want := make(map[string]bool, len(raw))
	for _, f := range raw {
		f = strings.ToLower(strings.TrimSpace(f))
		if f == "" {
			continue
		}
		known := false
		for _, k := range storage.AgentEditableFields {
			if f == k {
				known = true
				break
			}
		}
		if !known {
			return nil, fmt.Errorf("%w: unknown editable field %q (allowed: %s)", ErrValidation, f,
				strings.Join(storage.AgentEditableFields, ", "))
		}
		want[f] = true
	}
	out := make([]string, 0, len(want))
	for _, k := range storage.AgentEditableFields {
		if want[k] {
			out = append(out, k)
		}
	}
	return out, nil
}

// --- Keys and webhook secrets ---

// AgentKeyScopes are the scopes every agent key gets; guardrails narrow them.
var AgentKeyScopes = []string{storage.APIScopeTasksRead, storage.APIScopeTasksWrite, storage.APIScopeCommentsWrite}

// CreateAgentAPIKey mints a project key that acts as the agent.
func CreateAgentAPIKey(userID, projectID, agentID int, name string, expiresAt *time.Time) (string, *storage.APIKey, error) {
	if _, err := requireProjectManage(projectID, userID); err != nil {
		return "", nil, err
	}
	a, err := loadAgent(projectID, agentID)
	if err != nil {
		return "", nil, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = a.Handle + " key"
	}
	if len(name) > 80 {
		return "", nil, fmt.Errorf("%w: key name is too long", ErrValidation)
	}
	if err := validateAPIKeyExpiry(expiresAt, time.Now()); err != nil {
		return "", nil, err
	}
	plaintext, rec, err := storage.CreateProjectAPIKey(projectID, a.UserID, name, AgentKeyScopes, expiresAt)
	if errors.Is(err, storage.ErrAPIKeyNameExists) {
		return "", nil, fmt.Errorf("%w: a key with that name already exists in this project", ErrConflict)
	}
	if err != nil {
		return "", nil, err
	}
	_ = storage.LogProjectEvent(projectID, userID, "agent_key_created", map[string]interface{}{
		"agent_id": a.ID, "name": a.Name, "key_name": rec.Name,
	})
	return plaintext, rec, nil
}

// ListAgentAPIKeys returns an agent's active keys.
func ListAgentAPIKeys(userID, projectID, agentID int) ([]storage.APIKey, error) {
	if _, err := requireProjectManage(projectID, userID); err != nil {
		return nil, err
	}
	a, err := loadAgent(projectID, agentID)
	if err != nil {
		return nil, err
	}
	return storage.ListAgentAPIKeys(a.UserID)
}

// RevokeAgentAPIKey revokes one of an agent's keys.
func RevokeAgentAPIKey(userID, projectID, agentID, keyID int) error {
	if _, err := requireProjectManage(projectID, userID); err != nil {
		return err
	}
	a, err := loadAgent(projectID, agentID)
	if err != nil {
		return err
	}
	if err := storage.RevokeAgentAPIKey(a.UserID, keyID); err != nil {
		if errors.Is(err, storage.ErrAPIKeyNotFound) {
			return ErrNotFound
		}
		return err
	}
	return nil
}

// RotateAgentWebhookSecret replaces the signing secret and returns it once.
func RotateAgentWebhookSecret(userID, projectID, agentID int) (string, error) {
	if _, err := requireProjectManage(projectID, userID); err != nil {
		return "", err
	}
	a, err := loadAgent(projectID, agentID)
	if err != nil {
		return "", err
	}
	plain := "whsec_" + strings.ReplaceAll(uuid.NewString()+uuid.NewString(), "-", "")
	enc, err := secret.Encrypt(plain)
	if err != nil {
		return "", err
	}
	if err := storage.SetAgentWebhookSecret(a.ID, enc); err != nil {
		return "", err
	}
	return plain, nil
}

// TestAgentWebhook sends a sample run to the agent's webhook.
func TestAgentWebhook(userID, projectID, agentID int) error {
	proj, err := requireProjectManage(projectID, userID)
	if err != nil {
		return err
	}
	a, err := loadAgent(projectID, agentID)
	if err != nil {
		return err
	}
	if a.WebhookURL == "" {
		return fmt.Errorf("%w: set a webhook URL first", ErrValidation)
	}
	run := storage.AgentRun{
		ID: 0, AgentID: a.ID, ProjectID: projectID, TaskID: 0,
		Trigger: AgentTriggerTest, TriggeredBy: userID, Status: storage.AgentRunQueued,
		Note: "This is a test delivery from GoTodo. No task is attached.", CreatedAt: time.Now().UTC(),
	}
	body, err := json.Marshal(agentRunPayload(a, proj.Name, &run, &storage.HookTaskSnapshot{
		Title: "Test task", ProjectID: projectID, ProjectName: proj.Name,
	}))
	if err != nil {
		return err
	}
	err = postAgentRun(a, body)
	storage.RecordAgentDelivery(a.ID, errString(err))
	if err != nil {
		return fmt.Errorf("%w: delivery failed: %s", ErrValidation, err.Error())
	}
	return nil
}

// AgentTriggerTest marks the sample payload from TestAgentWebhook.
const AgentTriggerTest = "test"

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// --- Runs ---

// canTriggerAgent reports whether actorID may start runs for agent.
func canTriggerAgent(a *storage.ProjectAgent, actorID int) bool {
	if actorID <= 0 || storage.IsAgentUser(actorID) {
		return false
	}
	role, err := storage.GetProjectRole(a.ProjectID, actorID)
	if err != nil || role == "" {
		return false
	}
	switch a.TriggerBy {
	case storage.AgentTriggerByWriters:
		return storage.RoleCanWriteTask(a.ProjectID, role)
	case storage.AgentTriggerBySelected:
		// Only the listed roles and members; managers are not implied.
		return containsInt(a.TriggerUserIDs, actorID) || slugListed(a.TriggerRoleSlugs, role)
	default:
		return storage.RoleCanManageProject(a.ProjectID, role)
	}
}

func slugListed(slugs []string, role string) bool {
	for _, s := range slugs {
		if strings.EqualFold(s, role) {
			return true
		}
	}
	return false
}

// DispatchAgentRun is the "Send to agent" action on a task.
func DispatchAgentRun(actorID, taskID, agentID int, note string) (*storage.AgentRun, error) {
	canRead, _, projectID, err := storage.CanUserAccessTask(taskID, actorID)
	if err != nil {
		return nil, err
	}
	if !canRead || projectID <= 0 {
		return nil, ErrNotFound
	}
	a, err := loadAgent(projectID, agentID)
	if err != nil {
		return nil, err
	}
	if !canTriggerAgent(a, actorID) {
		switch a.TriggerBy {
		case storage.AgentTriggerByWriters:
			return nil, fmt.Errorf("%w: only members who can edit tasks can send work to %s", ErrForbidden, a.Name)
		case storage.AgentTriggerBySelected:
			return nil, fmt.Errorf("%w: you are not on the list of people who can send work to %s", ErrForbidden, a.Name)
		}
		return nil, fmt.Errorf("%w: only project managers can send work to %s", ErrForbidden, a.Name)
	}
	note = strings.TrimSpace(note)
	if utf8.RuneCountInString(note) > MaxAgentRunNoteLen {
		return nil, fmt.Errorf("%w: note is too long (max %d characters)", ErrValidation, MaxAgentRunNoteLen)
	}
	return queueAgentRun(a, taskID, storage.AgentTriggerManual, actorID, note)
}

// queueAgentRun applies the agent's limits, records the run, optionally
// claims the task, and delivers the webhook in the background.
func queueAgentRun(a *storage.ProjectAgent, taskID int, trigger string, actorID int, note string) (*storage.AgentRun, error) {
	if !a.Enabled {
		return nil, fmt.Errorf("%w: %s is paused", ErrConflict, a.Name)
	}
	if a.Role == "" {
		return nil, fmt.Errorf("%w: %s is no longer a member of this project", ErrConflict, a.Name)
	}
	if pid, err := storage.GetTaskProjectID(taskID); err != nil || pid != a.ProjectID {
		return nil, ErrNotFound
	}
	n, err := storage.CountAgentRunsSince(a.ID, time.Now().Add(-time.Hour))
	if err != nil {
		return nil, err
	}
	if n >= a.MaxRunsPerHour {
		return nil, fmt.Errorf("%w: %s reached its limit of %d runs per hour", ErrConflict, a.Name, a.MaxRunsPerHour)
	}
	run, err := storage.CreateAgentRun(storage.AgentRun{
		AgentID: a.ID, ProjectID: a.ProjectID, TaskID: taskID,
		Trigger: trigger, TriggeredBy: actorID, Note: note,
	})
	if errors.Is(err, storage.ErrAgentRunOpen) {
		return nil, fmt.Errorf("%w: %s is already working on this task", ErrConflict, a.Name)
	}
	if err != nil {
		return nil, err
	}

	if a.ClaimOnDispatch && storage.HasProjectPerm(a.ProjectID, a.Role, storage.PermTasksClaim) {
		if mode, err := storage.GetProjectWorkflowMode(a.ProjectID); err == nil && mode == storage.WorkflowKanban {
			claimer := a.UserID
			if err := storage.SetTaskClaimedBy(taskID, &claimer); err == nil {
				_ = storage.LogTaskEvent(taskID, a.UserID, "claimed", map[string]interface{}{"claimed_by": a.UserID})
			}
		}
	}
	_ = storage.LogTaskEvent(taskID, actorID, "agent_run_queued", map[string]interface{}{
		"agent_id": a.ID, "agent_name": a.Name, "run_id": run.ID, "trigger": trigger,
	})
	live.AfterTaskChangeLive(actorID, taskID, live.TypeTaskUpdated)
	go deliverAgentRun(a, run)
	return run, nil
}

func deliverAgentRun(a *storage.ProjectAgent, run *storage.AgentRun) {
	if a.WebhookURL == "" {
		return // poll / MCP mode: the agent picks the run up from its queue.
	}
	snap, err := storage.GetHookTaskSnapshot(run.TaskID)
	if err != nil || snap == nil {
		storage.MarkAgentRunDelivery(run.ID, "task could not be loaded")
		return
	}
	body, err := json.Marshal(agentRunPayload(a, snap.ProjectName, run, snap))
	if err != nil {
		return
	}
	err = postAgentRun(a, body)
	if err != nil {
		log.Printf("agents: deliver run %d to agent %d: %v", run.ID, a.ID, err)
	}
	storage.MarkAgentRunDelivery(run.ID, errString(err))
	storage.RecordAgentDelivery(a.ID, errString(err))
}

func postAgentRun(a *storage.ProjectAgent, body []byte) error {
	signing := ""
	if enc, err := storage.GetAgentWebhookSecretEnc(a.ID); err == nil && enc != "" {
		if plain, err := secret.Decrypt(enc); err == nil {
			signing = plain
		}
	}
	return hooks.PostAgentWebhook(a.WebhookURL, signing, "agent.run", uuid.NewString(), body)
}

// agentRunPayload is the JSON body sent to an agent webhook. It carries the
// run, the task, and the guardrails; never an API key.
func agentRunPayload(a *storage.ProjectAgent, projectName string, run *storage.AgentRun, snap *storage.HookTaskSnapshot) map[string]any {
	base := hooks.PublicBaseURL()
	taskURL := ""
	if base != "" && run.TaskID > 0 {
		taskURL = base + "/tasks/" + strconv.Itoa(run.TaskID)
	}
	triggeredBy := map[string]any{"id": run.TriggeredBy}
	if run.TriggeredBy > 0 {
		if p, err := storage.GetUserProfileByID(run.TriggeredBy); err == nil && p != nil {
			triggeredBy["username"] = p.UserName
		}
	}
	tags := snap.Tags
	if tags == nil {
		tags = []string{}
	}
	return map[string]any{
		"event": "agent.run",
		"run": map[string]any{
			"id":           run.ID,
			"trigger":      run.Trigger,
			"note":         run.Note,
			"triggered_by": triggeredBy,
			"created_at":   run.CreatedAt.UTC().Format(time.RFC3339),
		},
		"agent": map[string]any{
			"id":           a.ID,
			"name":         a.Name,
			"handle":       a.Handle,
			"instructions": a.Instructions,
		},
		"project": map[string]any{
			"id":          a.ProjectID,
			"name":        projectName,
			"github_repo": agentRepoJSON(a.ProjectID),
		},
		"task": map[string]any{
			"id":           run.TaskID,
			"title":        snap.Title,
			"description":  snap.Description,
			"status":       snap.StatusName,
			"status_id":    snap.StatusID,
			"priority":     snap.Priority,
			"due_date":     snap.DueDate,
			"tags":         tags,
			"fields":       snap.CustomFields,
			"url":          taskURL,
			"github_issue": agentIssueJSON(run.TaskID),
		},
		"guardrails": agentGuardrailsJSON(a),
		"api": map[string]any{
			"base_url": base + "/api/v2",
			"mcp_url":  base + "/api/v2/mcp",
		},
	}
}

// agentRepoJSON is the project's linked GitHub repo, or nil. It is optional
// context: agents that can work in a checkout use it to know where.
func agentRepoJSON(projectID int) map[string]any {
	repo, err := storage.GetProjectGitHubRepo(projectID)
	if err != nil || repo == nil {
		return nil
	}
	full := repo.GitHubOwner + "/" + repo.GitHubRepo
	return map[string]any{"full_name": full, "url": "https://github.com/" + full}
}

// agentIssueJSON is the task's linked GitHub issue, or nil.
func agentIssueJSON(taskID int) map[string]any {
	if taskID <= 0 {
		return nil
	}
	issue, err := storage.GetTaskGitHubIssue(taskID)
	if err != nil || issue == nil {
		return nil
	}
	return map[string]any{
		"number": issue.IssueNumber, "url": issue.IssueURL, "state": issue.IssueState, "title": issue.IssueTitle,
	}
}

// agentGuardrailsJSON describes what the agent may do, for payloads and MCP.
func agentGuardrailsJSON(a *storage.ProjectAgent) map[string]any {
	statuses := make([]map[string]any, 0, len(a.AllowedStatusIDs))
	for _, id := range a.AllowedStatusIDs {
		if st, err := storage.GetProjectStatus(a.ProjectID, id); err == nil {
			statuses = append(statuses, map[string]any{"id": st.ID, "name": st.Name, "is_done": st.IsDone})
		}
	}
	return map[string]any{
		"editable_fields":    a.EditableFields,
		"allowed_status_ids": a.AllowedStatusIDs,
		"allowed_statuses":   statuses,
		"can_complete":       a.CanComplete,
		"can_create_tasks":   a.CanCreateTasks,
		"can_comment":        a.CanComment,
		"role":               a.Role,
	}
}

// AgentGuardrails exposes agentGuardrailsJSON to the API layer.
func AgentGuardrails(a *storage.ProjectAgent) map[string]any {
	return agentGuardrailsJSON(a)
}

// ListProjectAgentRuns returns run history for a manager.
func ListProjectAgentRuns(userID, projectID, agentID, limit int) ([]storage.AgentRun, error) {
	if _, err := requireProjectManage(projectID, userID); err != nil {
		return nil, err
	}
	return storage.ListAgentRuns(storage.AgentRunFilter{ProjectID: projectID, AgentID: agentID, Limit: limit})
}

// TaskAgentRuns is what the task page shows: past runs and who you can send it to.
type TaskAgentRuns struct {
	Runs   []storage.AgentRun
	Agents []storage.ProjectAgent // agents the caller may dispatch
}

// ListTaskAgentRuns returns runs on a task for anyone who can read it.
func ListTaskAgentRuns(userID, taskID int) (*TaskAgentRuns, error) {
	canRead, _, projectID, err := storage.CanUserAccessTask(taskID, userID)
	if err != nil {
		return nil, err
	}
	if !canRead {
		return nil, ErrNotFound
	}
	out := &TaskAgentRuns{Runs: []storage.AgentRun{}, Agents: []storage.ProjectAgent{}}
	if projectID <= 0 {
		return out, nil
	}
	runs, err := storage.ListAgentRuns(storage.AgentRunFilter{TaskID: taskID, Limit: 20})
	if err != nil {
		return nil, err
	}
	out.Runs = runs
	agents, err := storage.ListEnabledAgentsForProject(projectID)
	if err != nil {
		return nil, err
	}
	for i := range agents {
		if canTriggerAgent(&agents[i], userID) {
			out.Agents = append(out.Agents, agents[i])
		}
	}
	return out, nil
}

// CancelAgentRun stops an open run. Managers and whoever started it may cancel.
func CancelAgentRun(userID, runID int) (*storage.AgentRun, error) {
	run, err := storage.GetAgentRun(runID)
	if errors.Is(err, storage.ErrAgentRunNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if run.TriggeredBy != userID {
		if _, err := requireProjectManage(run.ProjectID, userID); err != nil {
			return nil, err
		}
	}
	if err := storage.TransitionAgentRun(runID, 0,
		[]string{storage.AgentRunQueued, storage.AgentRunRunning}, storage.AgentRunCancelled, ""); err != nil {
		return nil, fmt.Errorf("%w: this run has already finished", ErrConflict)
	}
	releaseAgentClaim(run)
	_ = storage.LogTaskEvent(run.TaskID, userID, "agent_run_cancelled", map[string]interface{}{
		"agent_id": run.AgentID, "agent_name": run.AgentName, "run_id": run.ID,
	})
	live.AfterTaskChangeLive(userID, run.TaskID, live.TypeTaskUpdated)
	return storage.GetAgentRun(runID)
}

func releaseAgentClaim(run *storage.AgentRun) {
	a, err := storage.GetProjectAgent(run.ProjectID, run.AgentID)
	if err != nil {
		return
	}
	if claimed, err := storage.GetTaskClaimedBy(run.TaskID); err == nil && claimed == a.UserID {
		_ = storage.SetTaskClaimedBy(run.TaskID, nil)
	}
}

// --- Agent-side (called with the agent's own key) ---

// AgentForUser returns the agent behind an agent account.
func AgentForUser(agentUserID int) (*storage.ProjectAgent, error) {
	a, err := storage.GetAgentByUserID(agentUserID)
	if errors.Is(err, storage.ErrAgentNotFound) {
		return nil, ErrNotFound
	}
	return a, err
}

// GetAgentSelf describes the calling agent: identity, standing instructions,
// guardrails, and the board's statuses, so it knows what it may do.
func GetAgentSelf(agentUserID int) (map[string]any, error) {
	a, err := AgentForUser(agentUserID)
	if err != nil {
		return nil, err
	}
	projectName := ""
	if snap, err := storage.GetHookProjectSnapshot(a.ProjectID); err == nil && snap != nil {
		projectName = snap.ProjectName
	}
	statuses := []map[string]any{}
	if list, err := storage.ListProjectStatuses(a.ProjectID); err == nil {
		for _, st := range list {
			statuses = append(statuses, map[string]any{
				"id": st.ID, "name": st.Name, "description": st.Description, "is_done": st.IsDone,
			})
		}
	}
	return map[string]any{
		"id":           a.ID,
		"user_id":      a.UserID,
		"name":         a.Name,
		"handle":       a.Handle,
		"description":  a.Description,
		"instructions": a.Instructions,
		"project":      map[string]any{"id": a.ProjectID, "name": projectName, "github_repo": agentRepoJSON(a.ProjectID)},
		"statuses":     statuses,
		"guardrails":   agentGuardrailsJSON(a),
	}, nil
}

// ListAgentQueue returns the calling agent's runs, open ones by default.
func ListAgentQueue(agentUserID int, statuses []string, limit int) ([]storage.AgentRun, error) {
	a, err := AgentForUser(agentUserID)
	if err != nil {
		return nil, err
	}
	if len(statuses) == 0 {
		statuses = []string{storage.AgentRunQueued, storage.AgentRunRunning}
	}
	return storage.ListAgentRuns(storage.AgentRunFilter{AgentID: a.ID, Statuses: statuses, Limit: limit})
}

// GetAgentRunForAgent returns one of the calling agent's runs.
func GetAgentRunForAgent(agentUserID, runID int) (*storage.AgentRun, error) {
	a, err := AgentForUser(agentUserID)
	if err != nil {
		return nil, err
	}
	run, err := storage.GetAgentRun(runID)
	if err != nil || run.AgentID != a.ID {
		return nil, ErrNotFound
	}
	return run, nil
}

// StartAgentRun marks a queued run as running.
func StartAgentRun(agentUserID, runID int) (*storage.AgentRun, error) {
	a, err := AgentForUser(agentUserID)
	if err != nil {
		return nil, err
	}
	if err := storage.TransitionAgentRun(runID, a.ID,
		[]string{storage.AgentRunQueued}, storage.AgentRunRunning, ""); err != nil {
		if _, gerr := GetAgentRunForAgent(agentUserID, runID); gerr != nil {
			return nil, gerr
		}
		return nil, fmt.Errorf("%w: only queued runs can be started", ErrConflict)
	}
	run, err := storage.GetAgentRun(runID)
	if err != nil {
		return nil, err
	}
	live.AfterTaskChangeLive(agentUserID, run.TaskID, live.TypeTaskUpdated)
	return run, nil
}

// FinishAgentRun records the agent's result and releases its claim.
func FinishAgentRun(agentUserID, runID int, status, summary string) (*storage.AgentRun, error) {
	a, err := AgentForUser(agentUserID)
	if err != nil {
		return nil, err
	}
	status = strings.ToLower(strings.TrimSpace(status))
	if status != storage.AgentRunSucceeded && status != storage.AgentRunFailed {
		return nil, fmt.Errorf("%w: status must be %q or %q", ErrValidation, storage.AgentRunSucceeded, storage.AgentRunFailed)
	}
	summary = strings.TrimSpace(summary)
	if utf8.RuneCountInString(summary) > MaxAgentRunSummaryLen {
		return nil, fmt.Errorf("%w: summary is too long (max %d characters)", ErrValidation, MaxAgentRunSummaryLen)
	}
	if err := storage.TransitionAgentRun(runID, a.ID,
		[]string{storage.AgentRunQueued, storage.AgentRunRunning}, status, summary); err != nil {
		if _, gerr := GetAgentRunForAgent(agentUserID, runID); gerr != nil {
			return nil, gerr
		}
		return nil, fmt.Errorf("%w: this run has already finished", ErrConflict)
	}
	run, err := storage.GetAgentRun(runID)
	if err != nil {
		return nil, err
	}
	releaseAgentClaim(run)
	_ = storage.LogTaskEvent(run.TaskID, agentUserID, "agent_run_finished", map[string]interface{}{
		"agent_id": a.ID, "agent_name": a.Name, "run_id": run.ID, "status": status,
	})
	live.AfterTaskChangeLive(agentUserID, run.TaskID, live.TypeTaskUpdated)
	return run, nil
}

// --- Automatic triggers ---

// StartAgentDispatcher hooks agents into the task event stream so mentions
// and status changes can start runs.
func StartAgentDispatcher() {
	hooks.RegisterSink(hooks.Sink{
		Name:   "agents",
		Active: agentsActive,
		Handle: HandleAgentEvent,
	})
}

var agentCache struct {
	sync.Mutex
	at     time.Time
	active bool
}

// agentsActive is cached briefly so idle sites pay nothing per event.
func agentsActive() bool {
	agentCache.Lock()
	defer agentCache.Unlock()
	if time.Since(agentCache.at) < 30*time.Second {
		return agentCache.active
	}
	n, err := storage.CountActiveAgents()
	agentCache.active = err == nil && n > 0
	agentCache.at = time.Now()
	return agentCache.active
}

func invalidateAgentCache() {
	agentCache.Lock()
	agentCache.at = time.Time{}
	agentCache.Unlock()
}

// HandleAgentEvent starts runs for mentions and trigger-status moves. Events
// caused by agents are ignored so agents can never trigger each other.
func HandleAgentEvent(ev hooks.Event) {
	if ev.TaskID <= 0 || ev.ProjectID <= 0 || ev.ActorID <= 0 {
		return
	}
	switch ev.Type {
	case hooks.EventTaskMentioned, hooks.EventTaskStatusChanged:
	default:
		return
	}
	if storage.IsAgentUser(ev.ActorID) {
		return
	}
	agents, err := storage.ListEnabledAgentsForProject(ev.ProjectID)
	if err != nil || len(agents) == 0 {
		return
	}

	switch ev.Type {
	case hooks.EventTaskMentioned:
		mentioned := make(map[int]bool, len(ev.MentionedUserIDs))
		for _, id := range ev.MentionedUserIDs {
			mentioned[id] = true
		}
		for i := range agents {
			a := &agents[i]
			if !a.TriggerOnMention || !mentioned[a.UserID] {
				continue
			}
			autoQueue(a, ev, storage.AgentTriggerMention, ev.Comment)
		}
	case hooks.EventTaskStatusChanged:
		st, err := storage.GetTaskProjectStatus(ev.TaskID)
		if err != nil || st == nil {
			return
		}
		for i := range agents {
			a := &agents[i]
			if !containsInt(a.TriggerStatusIDs, st.ID) {
				continue
			}
			autoQueue(a, ev, storage.AgentTriggerStatus, "Task moved to "+st.Name)
		}
	}
}

func autoQueue(a *storage.ProjectAgent, ev hooks.Event, trigger, note string) {
	if !canTriggerAgent(a, ev.ActorID) {
		return
	}
	if utf8.RuneCountInString(note) > MaxAgentRunNoteLen {
		note = string([]rune(note)[:MaxAgentRunNoteLen])
	}
	if _, err := queueAgentRun(a, ev.TaskID, trigger, ev.ActorID, note); err != nil &&
		!errors.Is(err, ErrConflict) {
		log.Printf("agents: auto-queue agent %d task %d: %v", a.ID, ev.TaskID, err)
	}
}

func containsInt(list []int, v int) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
