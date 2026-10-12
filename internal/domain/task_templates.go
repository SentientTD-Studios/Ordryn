package domain

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"
	"unicode/utf8"

	"GoTodo/internal/storage"
)

const (
	maxTemplateNameLen     = 80
	maxTemplateTitleLen    = 500
	maxTemplateSubtasks    = 20
	maxTemplateTags        = 5
	maxTemplateDueOffset   = 365
	maxTemplatesPerScope   = 100
	maxTemplateTagNameRune = 50
)

// TaskTemplateInput is the editable content of a template.
type TaskTemplateInput struct {
	Name           string
	Title          string
	Description    string
	Priority       int
	TagNames       []string
	EstimatePoints *int
	DueOffsetDays  *int
	Subtasks       []string
}

// TaskTemplateView is a template plus whether the caller may edit or delete it.
type TaskTemplateView struct {
	storage.TaskTemplate
	CanManage bool
}

// ApplyTaskTemplateInput picks where a template's task lands. ProjectID is only
// used by personal templates; project templates always create in their project.
type ApplyTaskTemplateInput struct {
	ProjectID *int
	SprintID  *int
}

func normalizeTemplateInput(in TaskTemplateInput) (TaskTemplateInput, error) {
	out := in
	out.Name = strings.TrimSpace(in.Name)
	out.Title = strings.TrimSpace(in.Title)
	out.Description = strings.TrimSpace(in.Description)
	if out.Name == "" || utf8.RuneCountInString(out.Name) > maxTemplateNameLen {
		return out, fmt.Errorf("%w: name is required and must be %d characters or less", ErrValidation, maxTemplateNameLen)
	}
	if out.Title == "" || utf8.RuneCountInString(out.Title) > maxTemplateTitleLen {
		return out, fmt.Errorf("%w: title is required and must be %d characters or less", ErrValidation, maxTemplateTitleLen)
	}
	if err := validateDescriptionLength(out.Description, MaxDescriptionLength()); err != nil {
		return out, err
	}
	if out.Priority < 0 || out.Priority > 3 {
		return out, fmt.Errorf("%w: priority must be 0-3", ErrValidation)
	}
	if out.EstimatePoints != nil && (*out.EstimatePoints < 0 || *out.EstimatePoints > storage.MaxEstimatePoints) {
		return out, fmt.Errorf("%w: estimate_points must be 0-%d", ErrValidation, storage.MaxEstimatePoints)
	}
	if out.DueOffsetDays != nil && (*out.DueOffsetDays < 0 || *out.DueOffsetDays > maxTemplateDueOffset) {
		return out, fmt.Errorf("%w: due_offset_days must be 0-%d", ErrValidation, maxTemplateDueOffset)
	}
	out.TagNames = []string{}
	seen := map[string]bool{}
	for _, raw := range in.TagNames {
		name := strings.TrimSpace(raw)
		key := strings.ToLower(name)
		if name == "" || seen[key] || storage.IsSystemTagName(name) {
			continue
		}
		if utf8.RuneCountInString(name) > maxTemplateTagNameRune {
			return out, fmt.Errorf("%w: tag names must be %d characters or less", ErrValidation, maxTemplateTagNameRune)
		}
		seen[key] = true
		out.TagNames = append(out.TagNames, name)
	}
	if len(out.TagNames) > maxTemplateTags {
		return out, fmt.Errorf("%w: a template can have at most %d tags", ErrValidation, maxTemplateTags)
	}
	out.Subtasks = []string{}
	for _, raw := range in.Subtasks {
		title := strings.TrimSpace(raw)
		if title == "" {
			continue
		}
		if utf8.RuneCountInString(title) > maxTemplateTitleLen {
			return out, fmt.Errorf("%w: subtask titles must be %d characters or less", ErrValidation, maxTemplateTitleLen)
		}
		out.Subtasks = append(out.Subtasks, title)
	}
	if len(out.Subtasks) > maxTemplateSubtasks {
		return out, fmt.Errorf("%w: a template can have at most %d subtasks", ErrValidation, maxTemplateSubtasks)
	}
	return out, nil
}

func projectRoleFor(userID, projectID int) (string, error) {
	if _, err := storage.GetAccessibleProjectByID(projectID, userID); err != nil {
		return "", ErrNotFound
	}
	role, _ := storage.GetProjectRole(projectID, userID)
	return role, nil
}

// canManageTemplate: the owner of a personal template; for a project template,
// its creator or anyone who can manage the project.
func canManageTemplate(userID int, t *storage.TaskTemplate) bool {
	if t.ProjectID == nil {
		return t.OwnerID == userID
	}
	role, err := projectRoleFor(userID, *t.ProjectID)
	if err != nil {
		return false
	}
	return t.OwnerID == userID || storage.HasProjectPerm(*t.ProjectID, role, storage.PermProjectManage)
}

// visibleTemplate loads a template the user may see: their own personal one, or
// one from a project they belong to. Anything else is not found.
func visibleTemplate(userID, id int) (*storage.TaskTemplate, error) {
	t, err := storage.GetTaskTemplate(id)
	if err != nil {
		return nil, ErrNotFound
	}
	if t.ProjectID == nil {
		if t.OwnerID != userID {
			return nil, ErrNotFound
		}
		return t, nil
	}
	if _, err := projectRoleFor(userID, *t.ProjectID); err != nil {
		return nil, ErrNotFound
	}
	return t, nil
}

// ListTaskTemplatesForUser returns the user's personal templates and, when
// projectID > 0, that project's shared templates.
func ListTaskTemplatesForUser(ctx context.Context, userID, projectID int) ([]TaskTemplateView, error) {
	var projects []int
	if projectID > 0 {
		if _, err := projectRoleFor(userID, projectID); err != nil {
			return nil, err
		}
		projects = []int{projectID}
	}
	list, err := storage.ListTaskTemplates(userID, projects)
	if err != nil {
		return nil, err
	}
	out := make([]TaskTemplateView, 0, len(list))
	for i := range list {
		out = append(out, TaskTemplateView{TaskTemplate: list[i], CanManage: canManageTemplate(userID, &list[i])})
	}
	return out, nil
}

// CreateTaskTemplateForUser saves a new template. projectID nil makes it
// personal; otherwise the user needs tasks:create in that project.
func CreateTaskTemplateForUser(ctx context.Context, userID int, projectID *int, in TaskTemplateInput) (*TaskTemplateView, error) {
	in, err := normalizeTemplateInput(in)
	if err != nil {
		return nil, err
	}
	if projectID != nil && *projectID <= 0 {
		projectID = nil
	}
	if projectID != nil {
		role, err := projectRoleFor(userID, *projectID)
		if err != nil {
			return nil, err
		}
		if !storage.HasProjectPerm(*projectID, role, storage.PermTasksCreate) {
			return nil, fmt.Errorf("%w: your role cannot create tasks in this project", ErrForbidden)
		}
	}
	n, err := storage.CountTaskTemplates(userID, projectID)
	if err != nil {
		return nil, err
	}
	if n >= maxTemplatesPerScope {
		return nil, fmt.Errorf("%w: limit of %d templates reached", ErrValidation, maxTemplatesPerScope)
	}
	t := &storage.TaskTemplate{
		OwnerID: userID, ProjectID: projectID, Name: in.Name, Title: in.Title, Description: in.Description,
		Priority: in.Priority, TagNames: in.TagNames, EstimatePoints: in.EstimatePoints,
		DueOffsetDays: in.DueOffsetDays, Subtasks: in.Subtasks,
	}
	if err := storage.SaveTaskTemplate(t); err != nil {
		return nil, err
	}
	saved, err := storage.GetTaskTemplate(t.ID)
	if err != nil {
		return nil, err
	}
	return &TaskTemplateView{TaskTemplate: *saved, CanManage: true}, nil
}

// UpdateTaskTemplateForUser replaces a template's content.
func UpdateTaskTemplateForUser(ctx context.Context, userID, id int, in TaskTemplateInput) (*TaskTemplateView, error) {
	t, err := visibleTemplate(userID, id)
	if err != nil {
		return nil, err
	}
	if !canManageTemplate(userID, t) {
		return nil, fmt.Errorf("%w: only the template's creator or a project manager can change it", ErrForbidden)
	}
	in, err = normalizeTemplateInput(in)
	if err != nil {
		return nil, err
	}
	t.Name, t.Title, t.Description, t.Priority = in.Name, in.Title, in.Description, in.Priority
	t.TagNames, t.EstimatePoints, t.DueOffsetDays, t.Subtasks = in.TagNames, in.EstimatePoints, in.DueOffsetDays, in.Subtasks
	if err := storage.SaveTaskTemplate(t); err != nil {
		return nil, err
	}
	saved, err := storage.GetTaskTemplate(id)
	if err != nil {
		return nil, err
	}
	return &TaskTemplateView{TaskTemplate: *saved, CanManage: true}, nil
}

// DeleteTaskTemplateForUser removes a template.
func DeleteTaskTemplateForUser(ctx context.Context, userID, id int) error {
	t, err := visibleTemplate(userID, id)
	if err != nil {
		return err
	}
	if !canManageTemplate(userID, t) {
		return fmt.Errorf("%w: only the template's creator or a project manager can delete it", ErrForbidden)
	}
	return storage.DeleteTaskTemplate(id)
}

// ApplyTaskTemplateForUser creates a task (and its subtasks) from a template and
// returns the new task's id. Tags are matched by name in the target project (or
// the user's personal tags); names with no matching tag there are skipped.
func ApplyTaskTemplateForUser(ctx context.Context, userID, id int, in ApplyTaskTemplateInput) (int, error) {
	t, err := visibleTemplate(userID, id)
	if err != nil {
		return 0, err
	}
	var target *int
	if t.ProjectID != nil {
		target = t.ProjectID
	} else if in.ProjectID != nil && *in.ProjectID > 0 {
		target = in.ProjectID
	}

	tagIDs := make([]int, 0, len(t.TagNames))
	for _, name := range t.TagNames {
		if tag, err := storage.FindTagByName(userID, target, name); err == nil {
			tagIDs = append(tagIDs, tag.ID)
		}
	}

	due := ""
	if t.DueOffsetDays != nil {
		loc, err := time.LoadLocation(storage.UserTimezone(userID))
		if err != nil {
			loc = time.UTC
		}
		due = time.Now().In(loc).AddDate(0, 0, *t.DueOffsetDays).Format("2006-01-02")
	}

	projectArg := 0
	if target != nil {
		projectArg = *target
	}
	// Estimates and sprints only exist on kanban boards; drop them elsewhere
	// rather than refuse the whole template.
	estimate, sprint := t.EstimatePoints, in.SprintID
	if mode, err := storage.GetProjectWorkflowMode(projectArg); projectArg == 0 || err != nil || mode != storage.WorkflowKanban {
		estimate, sprint = nil, nil
	}
	parentID, err := CreateTask(ctx, userID, CreateTaskInput{
		Title:          t.Title,
		Description:    t.Description,
		DueDate:        due,
		ProjectID:      &projectArg,
		Priority:       t.Priority,
		TagIDs:         tagIDs,
		EstimatePoints: estimate,
		SprintID:       sprint,
	})
	if err != nil {
		return 0, err
	}
	for _, title := range t.Subtasks {
		pid := parentID
		if _, err := CreateTask(ctx, userID, CreateTaskInput{Title: title, ParentID: &pid}); err != nil {
			// The parent already exists; report the task rather than lose it.
			log.Printf("task template %d: subtask %q: %v", id, title, err)
		}
	}
	return parentID, nil
}
