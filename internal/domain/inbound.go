package domain

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"GoTodo/internal/extensions"
	"GoTodo/internal/live"
	"GoTodo/internal/storage"
)

type InboundWebhookInput struct {
	ProjectID   int
	Action      string
	Title       string
	Description string
	TaskID      int
	Comment     string
	Field       string
	Value       string
}

// ApplyInboundWebhook creates a task or comment using the project's inbound secret.
func ApplyInboundWebhook(ctx context.Context, secretHeader, hmacHeader, timestampHeader string, body []byte, in InboundWebhookInput) error {
	if in.ProjectID <= 0 {
		return fmt.Errorf("%w: project_id is required", ErrValidation)
	}
	if !InboundWebhooksEnabled() {
		return fmt.Errorf("%w: inbound webhooks are disabled", ErrForbidden)
	}
	cfg, err := storage.GetProjectInboundWebhook(in.ProjectID)
	if err != nil {
		return err
	}
	if cfg == nil || !cfg.Enabled || !cfg.SecretSet {
		return ErrForbidden
	}
	if err := verifyInboundAuth(cfg.Secret, secretHeader, hmacHeader, timestampHeader, body); err != nil {
		return err
	}
	return applyInboundWithConfig(ctx, cfg, in)
}

func applyInboundWithConfig(ctx context.Context, cfg *storage.ProjectInboundWebhook, in InboundWebhookInput) error {
	if cfg == nil || !cfg.Enabled || !cfg.SecretSet {
		return ErrForbidden
	}
	action := strings.ToLower(strings.TrimSpace(in.Action))
	switch action {
	case "create":
		if !cfg.AllowCreate {
			return fmt.Errorf("%w: create is disabled", ErrForbidden)
		}
		title := strings.TrimSpace(in.Title)
		if title == "" {
			return fmt.Errorf("%w: title is required", ErrValidation)
		}
		pid := cfg.ProjectID
		_, err := CreateTask(ctx, cfg.ProjectOwnerID(), CreateTaskInput{
			Title:       title,
			Description: strings.TrimSpace(in.Description),
			ProjectID:   &pid,
		})
		if err != nil {
			_ = storage.RecordProjectInboundDelivery(cfg.ProjectID, err.Error())
			return err
		}
		_ = storage.RecordProjectInboundDelivery(cfg.ProjectID, "")
		return nil
	case "comment":
		if !cfg.AllowComment {
			return fmt.Errorf("%w: comment is disabled", ErrForbidden)
		}
		if in.TaskID <= 0 || strings.TrimSpace(in.Comment) == "" {
			return fmt.Errorf("%w: task_id and comment are required", ErrValidation)
		}
		_, projectID, err := storage.TaskOwnerAndProject(in.TaskID)
		if err != nil || projectID != cfg.ProjectID {
			return ErrNotFound
		}
		_, err = AddCommentForUser(ctx, cfg.ProjectOwnerID(), in.TaskID, in.Comment)
		if err != nil {
			_ = storage.RecordProjectInboundDelivery(cfg.ProjectID, err.Error())
			return err
		}
		_ = storage.RecordProjectInboundDelivery(cfg.ProjectID, "")
		return nil
	case extensions.ActionComplete:
		if !projectDeclaresInboundAction(cfg.ProjectID, extensions.ActionComplete) {
			return fmt.Errorf("%w: action must be create or comment", ErrValidation)
		}
		if in.TaskID <= 0 {
			return fmt.Errorf("%w: task_id is required", ErrValidation)
		}
		if err := requireInboundTaskInProject(in.TaskID, cfg.ProjectID); err != nil {
			return err
		}
		if err := SetTaskCompleted(ctx, cfg.ProjectOwnerID(), in.TaskID, true); err != nil {
			_ = storage.RecordProjectInboundDelivery(cfg.ProjectID, err.Error())
			return err
		}
		_ = storage.RecordProjectInboundDelivery(cfg.ProjectID, "")
		return nil
	case extensions.ActionSetField:
		if !projectDeclaresInboundAction(cfg.ProjectID, extensions.ActionSetField) {
			return fmt.Errorf("%w: action must be create or comment", ErrValidation)
		}
		if in.TaskID <= 0 || strings.TrimSpace(in.Field) == "" {
			return fmt.Errorf("%w: task_id and field are required", ErrValidation)
		}
		if err := requireInboundTaskInProject(in.TaskID, cfg.ProjectID); err != nil {
			return err
		}
		raw, err := json.Marshal(strings.TrimSpace(in.Value))
		if err != nil {
			return fmt.Errorf("%w: invalid field value", ErrValidation)
		}
		if strings.TrimSpace(in.Value) == "" {
			raw = []byte("null")
		}
		keys, err := ApplyTaskFields(in.TaskID, cfg.ProjectID, cfg.ProjectOwnerID(), map[string]json.RawMessage{
			strings.TrimSpace(in.Field): raw,
		})
		if err != nil {
			_ = storage.RecordProjectInboundDelivery(cfg.ProjectID, err.Error())
			return err
		}
		if len(keys) > 0 {
			live.AfterTaskChangeLive(cfg.ProjectOwnerID(), in.TaskID, live.TypeTaskUpdated)
			live.DispatchHook(cfg.ProjectOwnerID(), in.TaskID, live.TypeTaskUpdated, &live.TaskHookMeta{Changed: keys})
		}
		_ = storage.RecordProjectInboundDelivery(cfg.ProjectID, "")
		return nil
	default:
		return fmt.Errorf("%w: action must be create or comment", ErrValidation)
	}
}

func requireInboundTaskInProject(taskID, projectID int) error {
	_, pid, err := storage.TaskOwnerAndProject(taskID)
	if err != nil || pid != projectID {
		return ErrNotFound
	}
	return nil
}

func projectDeclaresInboundAction(projectID int, action string) bool {
	for _, e := range extensions.LoadedEntries() {
		if !e.Manifest.DeclaresAction(action) {
			continue
		}
		site, err := storage.GetExtensionSettings(e.ID)
		if err != nil || !site.Enabled {
			continue
		}
		proj, err := storage.GetExtensionProjectSettings(e.ID, projectID)
		if err != nil || !proj.Enabled {
			continue
		}
		return true
	}
	return false
}

func verifyInboundAuth(secret, customHeader, hmacHeader, timestampHeader string, body []byte) error {
	customHeader = strings.TrimSpace(customHeader)
	hmacHeader = strings.TrimSpace(hmacHeader)
	if customHeader != "" {
		if hmac.Equal([]byte(customHeader), []byte(secret)) {
			return nil
		}
		return ErrForbidden
	}
	if hmacHeader != "" {
		ts := strings.TrimSpace(timestampHeader)
		if ts == "" {
			return fmt.Errorf("%w: X-Ordryn-Timestamp is required", ErrForbidden)
		}
		unix, err := strconv.ParseInt(ts, 10, 64)
		if err != nil {
			return fmt.Errorf("%w: invalid X-Ordryn-Timestamp", ErrForbidden)
		}
		now := time.Now().UTC().Unix()
		skew := now - unix
		if skew < 0 {
			skew = -skew
		}
		if skew > 300 {
			return fmt.Errorf("%w: X-Ordryn-Timestamp is too old", ErrForbidden)
		}
		const prefix = "sha256="
		if !strings.HasPrefix(hmacHeader, prefix) {
			return ErrForbidden
		}
		want, err := hex.DecodeString(strings.TrimPrefix(hmacHeader, prefix))
		if err != nil {
			return ErrForbidden
		}
		mac := hmac.New(sha256.New, []byte(secret))
		_, _ = mac.Write([]byte(ts + "."))
		_, _ = mac.Write(body)
		if !hmac.Equal(mac.Sum(nil), want) {
			return ErrForbidden
		}
		return nil
	}
	return fmt.Errorf("%w: provide X-Ordryn-Webhook-Secret or X-Ordryn-Signature", ErrForbidden)
}

// InboundWebhooksEnabled reports whether Admin has allowed inbound webhooks.
func InboundWebhooksEnabled() bool {
	s, err := storage.GetSiteSettings()
	return err == nil && s != nil && s.EnableInboundWebhooks
}
