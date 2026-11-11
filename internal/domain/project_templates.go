package domain

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"GoTodo/internal/storage"
)

// SetProjectTemplateForUser marks a project as a template (or not). Managers only.
func SetProjectTemplateForUser(ctx context.Context, userID, projectID int, on bool) error {
	_ = ctx
	if _, err := requireProjectManage(projectID, userID); err != nil {
		return err
	}
	return storage.SetProjectIsTemplate(projectID, on)
}

// ProjectTemplate is a template the user can start a project from.
type ProjectTemplate struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	WorkflowMode string `json:"workflow_mode"`
	OpenTasks    int    `json:"open_tasks"`
}

// ListProjectTemplatesForUser returns the templates among projects the user can open.
func ListProjectTemplatesForUser(ctx context.Context, userID int) ([]ProjectTemplate, error) {
	_ = ctx
	projects, err := storage.GetAccessibleProjects(userID)
	if err != nil {
		return nil, err
	}
	ids := make([]int, 0, len(projects))
	for _, p := range projects {
		ids = append(ids, p.ID)
	}
	tmpl, err := storage.TemplateProjectIDs(ids)
	if err != nil {
		return nil, err
	}
	out := []ProjectTemplate{}
	for _, p := range projects {
		if !tmpl[p.ID] {
			continue
		}
		tasks, _ := storage.ListTemplateTasks(p.ID)
		out = append(out, ProjectTemplate{ID: p.ID, Name: p.Name, Description: p.Description,
			WorkflowMode: p.WorkflowMode, OpenTasks: len(tasks)})
	}
	return out, nil
}

// FromTemplateInput creates a project from a template.
type FromTemplateInput struct {
	TemplateID   int
	Name         string
	Description  *string
	IncludeTasks bool
}

// FromTemplateResult is the new project and what couldn't be copied.
type FromTemplateResult struct {
	Project      *storage.Project
	TasksCopied  int
	RulesCopied  int
	RulesSkipped int
	Warnings     []string
}

// CreateProjectFromTemplate copies a template's setup into a new project the
// user owns: workflow mode, board columns (with WIP limits and gates), project
// roles, tags, sprint settings, automation rules, and optionally its open tasks.
// Rules that name a specific sprint, person, or agent can't carry over and are skipped.
func CreateProjectFromTemplate(ctx context.Context, userID int, in FromTemplateInput) (*FromTemplateResult, error) {
	src, err := storage.GetAccessibleProjectByID(in.TemplateID, userID)
	if err != nil || !storage.ProjectIsTemplate(in.TemplateID) {
		return nil, ErrNotFound
	}
	desc := src.Description
	if in.Description != nil {
		desc = *in.Description
	}
	proj, err := CreateProject(ctx, userID, in.Name, desc)
	if err != nil {
		return nil, err
	}
	res := &FromTemplateResult{Project: proj}
	warn := func(format string, a ...any) { res.Warnings = append(res.Warnings, fmt.Sprintf(format, a...)) }

	// Project roles first: status gates and the copied rules may name them.
	if _, roles, err := listProjectOwnRoles(ctx, userID, src.ID); err == nil {
		for _, r := range roles {
			if _, err := CreateProjectCustomRoleForUser(ctx, userID, proj.ID, CreateSiteProjectRoleInput{
				Slug: r.Slug, Name: r.Name, Description: r.Description, Permissions: r.Permissions, SortOrder: r.SortOrder,
			}); err != nil {
				warn("role %q: %v", r.Name, err)
			}
		}
	}

	statusMap := map[int]int{}
	if src.WorkflowMode == storage.WorkflowKanban {
		if _, err := SetProjectWorkflowMode(ctx, userID, proj.ID, storage.WorkflowKanban); err != nil {
			return res, err
		}
		statusMap, err = copyStatuses(ctx, userID, src.ID, proj.ID, warn)
		if err != nil {
			return res, err
		}
	}

	tagMap := map[int]int{}
	if tags, err := ListTags(ctx, userID, &src.ID); err == nil {
		existing, _ := ListTags(ctx, userID, &proj.ID)
		byName := map[string]int{}
		for _, t := range existing {
			byName[strings.ToLower(t.Name)] = t.ID
		}
		for _, t := range tags {
			if t.ProjectID == nil || *t.ProjectID != src.ID {
				continue // personal or site tags aren't the template's to copy
			}
			if id, ok := byName[strings.ToLower(t.Name)]; ok {
				tagMap[t.ID] = id
				continue
			}
			nt, err := CreateTag(ctx, userID, t.Name, &proj.ID)
			if err != nil {
				warn("tag %q: %v", t.Name, err)
				continue
			}
			if t.Color != "" {
				color := t.Color
				_, _ = UpdateTag(ctx, userID, nt.ID, nil, &color)
			}
			tagMap[t.ID] = nt.ID
		}
	}

	if src.AutoCreateNextSprint {
		on := true
		length, lock := src.AutoSprintLengthDays, src.AutoSprintLockDaysBefore
		if _, err := UpdateProject(ctx, userID, proj.ID, nil, nil, nil, nil, nil,
			&AutoSprintPatch{Enabled: &on, LengthDays: &length, LockDaysBefore: &lock}); err != nil {
			warn("sprint settings: %v", err)
		}
	}

	if rules, err := ListAutomationRules(userID, src.ID); err == nil {
		for _, r := range rules {
			input, ok := remapRule(r, statusMap, tagMap)
			if !ok {
				res.RulesSkipped++
				continue
			}
			if _, err := CreateAutomationRule(userID, proj.ID, input); err != nil {
				res.RulesSkipped++
				warn("rule %q: %v", r.Name, err)
				continue
			}
			res.RulesCopied++
		}
	}

	if in.IncludeTasks {
		n, err := copyTemplateTasks(ctx, userID, src.ID, proj.ID, statusMap, tagMap)
		res.TasksCopied = n
		if err != nil {
			warn("tasks: %v", err)
		}
	}
	_ = storage.LogProjectEvent(proj.ID, userID, "created_from_template", map[string]interface{}{
		"template_id": src.ID, "template_name": src.Name,
	})
	return res, nil
}

// listProjectOwnRoles returns the roles defined on the project itself.
func listProjectOwnRoles(ctx context.Context, userID, projectID int) ([]storage.ProjectPermInfo, []storage.ProjectRoleDef, error) {
	roles, catalog, err := ListProjectRolesForUser(ctx, userID, projectID)
	if err != nil {
		return nil, nil, err
	}
	own := []storage.ProjectRoleDef{}
	for _, r := range roles {
		if r.ProjectID != nil && *r.ProjectID == projectID && !r.IsSystem && r.Slug != storage.RoleOwner {
			own = append(own, r)
		}
	}
	return catalog, own, nil
}

// copyStatuses makes dst's board columns match src's (names, done/default,
// descriptions, WIP limits, gates, order) and returns src id -> dst id.
func copyStatuses(ctx context.Context, userID, srcID, dstID int, warn func(string, ...any)) (map[int]int, error) {
	srcStatuses, err := storage.ListProjectStatuses(srcID)
	if err != nil {
		return nil, err
	}
	dstStatuses, err := storage.ListProjectStatuses(dstID)
	if err != nil {
		return nil, err
	}
	byName := map[string]storage.ProjectStatus{}
	for _, s := range dstStatuses {
		byName[strings.ToLower(s.Name)] = s
	}
	m := map[int]int{}
	order := []int{}
	for _, s := range srcStatuses {
		if d, ok := byName[strings.ToLower(s.Name)]; ok {
			m[s.ID] = d.ID
			delete(byName, strings.ToLower(s.Name))
		} else {
			created, err := CreateProjectStatusForUser(ctx, userID, dstID, CreateProjectStatusInput{Name: s.Name, Description: s.Description})
			if err != nil {
				warn("column %q: %v", s.Name, err)
				continue
			}
			m[s.ID] = created.ID
		}
		order = append(order, m[s.ID])
	}
	// Done and default flags, descriptions, and WIP limits, once every column exists.
	for _, s := range srcStatuses {
		id, ok := m[s.ID]
		if !ok {
			continue
		}
		done, def, desc := s.IsDone, s.IsDefault, s.Description
		wip := s.WIPLimit
		in := UpdateProjectStatusInput{IsDone: &done, Description: &desc, WIPLimit: &wip}
		if def {
			in.IsDefault = &def
		}
		if _, err := UpdateProjectStatusForUser(ctx, userID, dstID, id, in); err != nil {
			warn("column %q: %v", s.Name, err)
		}
		if len(s.EnterRoleSlugs) > 0 || len(s.LeaveRoleSlugs) > 0 {
			if _, err := UpdateStatusGatesForUser(ctx, userID, dstID, id, s.EnterRoleSlugs, s.LeaveRoleSlugs); err != nil {
				warn("gates on %q: %v", s.Name, err)
			}
		}
	}
	// Columns the new board started with that the template doesn't have.
	var fallback *int
	if len(order) > 0 {
		fallback = &order[0]
	}
	for _, extra := range byName {
		if err := DeleteProjectStatusForUser(ctx, userID, dstID, extra.ID, fallback); err != nil {
			warn("removing column %q: %v", extra.Name, err)
		}
	}
	if len(order) > 0 {
		if err := ReorderProjectStatusesForUser(ctx, userID, dstID, order); err != nil {
			warn("column order: %v", err)
		}
	}
	return m, nil
}

func remapIDs(ids []int, m map[int]int) ([]int, bool) {
	out := make([]int, 0, len(ids))
	for _, id := range ids {
		n, ok := m[id]
		if !ok {
			return nil, false
		}
		out = append(out, n)
	}
	return out, true
}

// remapRule rewrites a rule's status and tag ids for the new project. It
// returns false when the rule names something that can't be copied.
func remapRule(r storage.AutomationRule, statuses, tags map[int]int) (AutomationRuleInput, bool) {
	var trig AutomationTriggerConfig
	var cond AutomationConditions
	var acts []AutomationAction
	if json.Unmarshal(orEmpty(r.TriggerConfig), &trig) != nil || json.Unmarshal(orEmpty(r.Conditions), &cond) != nil ||
		json.Unmarshal(orEmptyList(r.Actions), &acts) != nil {
		return AutomationRuleInput{}, false
	}
	var ok bool
	if trig.FromStatusIDs, ok = remapIDs(trig.FromStatusIDs, statuses); !ok {
		return AutomationRuleInput{}, false
	}
	if trig.ToStatusIDs, ok = remapIDs(trig.ToStatusIDs, statuses); !ok {
		return AutomationRuleInput{}, false
	}
	if trig.TagIDs, ok = remapIDs(trig.TagIDs, tags); !ok {
		return AutomationRuleInput{}, false
	}
	if cond.SprintID != 0 || cond.AssigneeID != 0 {
		return AutomationRuleInput{}, false
	}
	for _, list := range []*[]int{&cond.StatusIDs, &cond.ExcludeStatusIDs} {
		if *list, ok = remapIDs(*list, statuses); !ok {
			return AutomationRuleInput{}, false
		}
	}
	for _, list := range []*[]int{&cond.TagsAny, &cond.TagsNone} {
		if *list, ok = remapIDs(*list, tags); !ok {
			return AutomationRuleInput{}, false
		}
	}
	for i := range acts {
		a := &acts[i]
		if a.UserID != 0 || a.SprintID != 0 || a.AgentID != 0 {
			return AutomationRuleInput{}, false
		}
		if a.StatusID != 0 {
			if a.StatusID, ok = statuses[a.StatusID]; !ok {
				return AutomationRuleInput{}, false
			}
		}
		if a.TagID != 0 {
			if a.TagID, ok = tags[a.TagID]; !ok {
				return AutomationRuleInput{}, false
			}
		}
	}
	name, enabled, trigger := r.Name, r.Enabled, r.TriggerType
	return AutomationRuleInput{Name: &name, Enabled: &enabled, TriggerType: &trigger,
		TriggerConfig: &trig, Conditions: &cond, Actions: &acts, RecipeID: r.RecipeID}, true
}

func orEmpty(b json.RawMessage) json.RawMessage {
	if len(b) == 0 {
		return json.RawMessage(`{}`)
	}
	return b
}

func orEmptyList(b json.RawMessage) json.RawMessage {
	if len(b) == 0 {
		return json.RawMessage(`[]`)
	}
	return b
}

// copyTemplateTasks copies open tasks (and their open subtasks) into dst.
func copyTemplateTasks(ctx context.Context, userID, srcID, dstID int, statuses, tags map[int]int) (int, error) {
	tasks, err := storage.ListTemplateTasks(srcID)
	if err != nil {
		return 0, err
	}
	newID := map[int]int{}
	copied := 0
	for _, t := range tasks {
		in := CreateTaskInput{Title: t.Title, Description: t.Description, Priority: t.Priority,
			ProjectID: &dstID, EstimatePoints: t.EstimatePoints}
		if t.ParentID != 0 {
			parent, ok := newID[t.ParentID]
			if !ok {
				continue // its parent was completed, so it didn't come along
			}
			in.ParentID = &parent
		}
		if s, ok := statuses[t.StatusID]; ok {
			s := s
			in.StatusID = &s
		}
		for _, tag := range t.TagIDs {
			if n, ok := tags[tag]; ok {
				in.TagIDs = append(in.TagIDs, n)
			}
		}
		id, err := CreateTask(ctx, userID, in)
		if err != nil {
			return copied, err
		}
		newID[t.ID] = id
		copied++
	}
	return copied, nil
}
