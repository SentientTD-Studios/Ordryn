package domain

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"GoTodo/internal/extensions"
	"GoTodo/internal/storage"
)

type ExtensionCallbackInput struct {
	Action  string
	TaskID  int
	Comment string
	Field   string
	Value   string
}

// ApplyExtensionCallback runs a scoped inbound action using a project callback token.
func ApplyExtensionCallback(ctx context.Context, rawToken string, in ExtensionCallbackInput) (any, error) {
	tok, err := storage.GetCallbackTokenLookup(rawToken)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid callback token", ErrForbidden)
	}
	entry, ok := extensions.Get(tok.ExtensionID)
	if !ok || !entry.Loaded {
		return nil, fmt.Errorf("%w: extension is not loaded", ErrForbidden)
	}
	site, err := storage.GetExtensionSettings(tok.ExtensionID)
	if err != nil || !site.Enabled {
		return nil, fmt.Errorf("%w: extension is not enabled", ErrForbidden)
	}
	if tok.ProjectID > 0 {
		proj, err := storage.GetExtensionProjectSettings(tok.ExtensionID, tok.ProjectID)
		if err != nil || !proj.Enabled {
			return nil, fmt.Errorf("%w: extension is not enabled for this project", ErrForbidden)
		}
	}

	action := strings.ToLower(strings.TrimSpace(in.Action))
	switch action {
	case "get":
		if !entry.Manifest.HasPermission(extensions.PermTasksRead) {
			return nil, fmt.Errorf("%w: tasks:read is not granted", ErrForbidden)
		}
		snap, err := callbackTaskSnapshot(in.TaskID, tok)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"id":         snap.ID,
			"title":      snap.Title,
			"completed":  snap.Completed,
			"priority":   snap.Priority,
			"project_id": snap.ProjectID,
			"project":    snap.ProjectName,
			"status":     snap.StatusName,
			"due_date":   snap.DueDate,
			"sprint":     snap.SprintName,
			"claimed_by": snap.ClaimedByName,
			"tags":       snap.Tags,
			"fields":     snap.CustomFields,
		}, nil
	case extensions.ActionComplete:
		if !entry.Manifest.HasPermission(extensions.PermTasksWrite) {
			return nil, fmt.Errorf("%w: tasks:write is not granted", ErrForbidden)
		}
		actor, err := callbackActor(tok)
		if err != nil {
			return nil, err
		}
		if _, err := callbackTaskSnapshot(in.TaskID, tok); err != nil {
			return nil, err
		}
		if err := SetTaskCompleted(ctx, actor, in.TaskID, true); err != nil {
			return nil, err
		}
		return map[string]string{"status": "ok"}, nil
	case extensions.ActionComment:
		if !entry.Manifest.HasPermission(extensions.PermCommentsWrite) {
			return nil, fmt.Errorf("%w: comments:write is not granted", ErrForbidden)
		}
		if strings.TrimSpace(in.Comment) == "" {
			return nil, fmt.Errorf("%w: comment is required", ErrValidation)
		}
		actor, err := callbackActor(tok)
		if err != nil {
			return nil, err
		}
		if _, err := callbackTaskSnapshot(in.TaskID, tok); err != nil {
			return nil, err
		}
		if _, err := AddCommentForUser(ctx, actor, in.TaskID, in.Comment); err != nil {
			return nil, err
		}
		return map[string]string{"status": "ok"}, nil
	case extensions.ActionSetField:
		if !entry.Manifest.HasPermission(extensions.PermTasksWrite) {
			return nil, fmt.Errorf("%w: tasks:write is not granted", ErrForbidden)
		}
		if strings.TrimSpace(in.Field) == "" {
			return nil, fmt.Errorf("%w: field is required", ErrValidation)
		}
		actor, err := callbackActor(tok)
		if err != nil {
			return nil, err
		}
		snap, err := callbackTaskSnapshot(in.TaskID, tok)
		if err != nil {
			return nil, err
		}
		raw, err := json.Marshal(strings.TrimSpace(in.Value))
		if err != nil {
			return nil, fmt.Errorf("%w: invalid field value", ErrValidation)
		}
		if strings.TrimSpace(in.Value) == "" {
			raw = []byte("null")
		}
		if err := ApplyTaskFields(in.TaskID, snap.ProjectID, actor, map[string]json.RawMessage{
			strings.TrimSpace(in.Field): raw,
		}); err != nil {
			return nil, err
		}
		return map[string]string{"status": "ok"}, nil
	default:
		return nil, fmt.Errorf("%w: action must be get, complete, comment, or set_field", ErrValidation)
	}
}

func callbackActor(tok *storage.ExtensionCallbackToken) (int, error) {
	if tok.UserID > 0 {
		return tok.UserID, nil
	}
	if tok.ProjectID <= 0 {
		return 0, fmt.Errorf("%w: callback destination has no actor", ErrForbidden)
	}
	snap, err := storage.GetHookProjectSnapshot(tok.ProjectID)
	if err != nil || snap == nil || snap.OwnerID <= 0 {
		return 0, ErrNotFound
	}
	return snap.OwnerID, nil
}

func callbackTaskSnapshot(taskID int, tok *storage.ExtensionCallbackToken) (*storage.HookTaskSnapshot, error) {
	if taskID <= 0 {
		return nil, fmt.Errorf("%w: task_id is required", ErrValidation)
	}
	snap, err := storage.GetHookTaskSnapshot(taskID)
	if err != nil || snap == nil {
		return nil, ErrNotFound
	}
	if tok.ProjectID > 0 && snap.ProjectID != tok.ProjectID {
		return nil, ErrNotFound
	}
	if tok.ProjectID <= 0 && tok.UserID > 0 {
		canRead, _, _, err := storage.CanUserAccessTask(taskID, tok.UserID)
		if err != nil || !canRead {
			return nil, ErrNotFound
		}
	}
	return snap, nil
}
