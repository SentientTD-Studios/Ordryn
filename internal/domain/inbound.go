package domain

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"GoTodo/internal/extensions"
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
func ApplyInboundWebhook(ctx context.Context, secretHeader, hmacHeader string, body []byte, in InboundWebhookInput) error {
	if !InboundWebhooksEnabled() {
		return fmt.Errorf("%w: inbound webhooks are disabled", ErrForbidden)
	}
	if in.ProjectID <= 0 {
		cfg, err := findInboundByAuth(secretHeader, hmacHeader, body)
		if err != nil {
			return err
		}
		in.ProjectID = cfg.ProjectID
		return applyInboundWithConfig(ctx, cfg, in)
	}
	cfg, err := storage.GetProjectInboundWebhook(in.ProjectID)
	if err != nil {
		return err
	}
	if cfg == nil || !cfg.Enabled || !cfg.SecretSet {
		return ErrForbidden
	}
	if err := verifyInboundAuth(cfg.Secret, secretHeader, hmacHeader, body); err != nil {
		return err
	}
	return applyInboundWithConfig(ctx, cfg, in)
}

func findInboundByAuth(secretHeader, hmacHeader string, body []byte) (*storage.ProjectInboundWebhook, error) {
	rows, err := storage.ListEnabledProjectInboundWebhooks()
	if err != nil {
		return nil, err
	}
	for _, cfg := range rows {
		if cfg == nil || !cfg.SecretSet {
			continue
		}
		if err := verifyInboundAuth(cfg.Secret, secretHeader, hmacHeader, body); err == nil {
			return cfg, nil
		}
	}
	return nil, ErrForbidden
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
		if err := ApplyTaskFields(in.TaskID, cfg.ProjectID, cfg.ProjectOwnerID(), map[string]json.RawMessage{
			strings.TrimSpace(in.Field): raw,
		}); err != nil {
			_ = storage.RecordProjectInboundDelivery(cfg.ProjectID, err.Error())
			return err
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

func verifyInboundAuth(secret, customHeader, hmacHeader string, body []byte) error {
	customHeader = strings.TrimSpace(customHeader)
	hmacHeader = strings.TrimSpace(hmacHeader)
	if customHeader != "" {
		if hmac.Equal([]byte(customHeader), []byte(secret)) {
			return nil
		}
		return ErrForbidden
	}
	if hmacHeader != "" {
		const prefix = "sha256="
		if !strings.HasPrefix(hmacHeader, prefix) {
			return ErrForbidden
		}
		want, err := hex.DecodeString(strings.TrimPrefix(hmacHeader, prefix))
		if err != nil {
			return ErrForbidden
		}
		mac := hmac.New(sha256.New, []byte(secret))
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
