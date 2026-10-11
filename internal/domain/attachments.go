package domain

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"

	"GoTodo/internal/storage"
)

const maxAttachmentNameLen = 200

// AttachmentSettings reports whether uploads are on and the per-file limit.
func AttachmentSettings() (enabled bool, maxBytes int64) {
	enabled, maxBytes, err := storage.GetAttachmentSettings()
	if err != nil {
		return false, storage.DefaultAttachmentMaxBytes
	}
	return enabled, maxBytes
}

// cleanAttachmentName keeps the last path element of a client file name, drops
// control characters, and caps its length.
func cleanAttachmentName(name string) string {
	name = strings.ReplaceAll(name, `\`, "/")
	name = path.Base(strings.TrimSpace(name))
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '"' {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == "/" {
		name = "file"
	}
	if utf8.RuneCountInString(name) > maxAttachmentNameLen {
		ext := path.Ext(name)
		if utf8.RuneCountInString(ext) > 20 {
			ext = ""
		}
		runes := []rune(strings.TrimSuffix(name, ext))
		name = string(runes[:maxAttachmentNameLen-utf8.RuneCountInString(ext)]) + ext
	}
	return name
}

func requireTaskRead(taskID, userID int) (writeRole string, projectID int, err error) {
	canRead, writeRole, projectID, err := storage.CanUserAccessTask(taskID, userID)
	if err != nil {
		return "", 0, err
	}
	if !canRead {
		return "", 0, ErrNotFound
	}
	return writeRole, projectID, nil
}

// ListTaskAttachmentsForUser lists a visible task's attachments.
func ListTaskAttachmentsForUser(ctx context.Context, userID, taskID int) ([]storage.TaskAttachment, error) {
	if _, _, err := requireTaskRead(taskID, userID); err != nil {
		return nil, err
	}
	return storage.ListTaskAttachments(taskID)
}

// UploadTaskAttachmentForUser stores a file on a task the user can edit. It
// fails with ErrValidation when uploads are off or the file is too large.
func UploadTaskAttachmentForUser(ctx context.Context, userID, taskID int, fileName string, data []byte) (*storage.TaskAttachment, error) {
	if err := requireTaskPermOnTask(taskID, userID, storage.PermTasksEdit); err != nil {
		return nil, err
	}
	enabled, maxBytes := AttachmentSettings()
	if !enabled {
		return nil, fmt.Errorf("%w: file attachments are turned off on this site", ErrValidation)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("%w: the file is empty", ErrValidation)
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("%w: files can be at most %s", ErrValidation, FormatBytes(maxBytes))
	}
	contentType := http.DetectContentType(data)
	a, err := storage.AddTaskAttachment(taskID, userID, cleanAttachmentName(fileName), contentType, data)
	if errors.Is(err, storage.ErrAttachmentLimit) {
		return nil, fmt.Errorf("%w: %v", ErrValidation, err)
	}
	return a, err
}

// GetTaskAttachmentForUser returns one attachment and its bytes from a visible task.
func GetTaskAttachmentForUser(ctx context.Context, userID, taskID, id int) (*storage.TaskAttachment, []byte, error) {
	if _, _, err := requireTaskRead(taskID, userID); err != nil {
		return nil, nil, err
	}
	a, err := storage.GetTaskAttachment(taskID, id)
	if err != nil {
		return nil, nil, ErrNotFound
	}
	data, err := storage.GetTaskAttachmentData(taskID, id)
	if err != nil {
		return nil, nil, ErrNotFound
	}
	return a, data, nil
}

// DeleteTaskAttachmentForUser removes an attachment. Anyone who can edit the
// task may remove their own uploads; removing someone else's needs project:manage.
func DeleteTaskAttachmentForUser(ctx context.Context, userID, taskID, id int) error {
	writeRole, projectID, err := requireTaskRead(taskID, userID)
	if err != nil {
		return err
	}
	if err := denyMissingTaskPerm(projectID, writeRole, storage.PermTasksEdit); err != nil {
		return err
	}
	a, err := storage.GetTaskAttachment(taskID, id)
	if err != nil {
		return ErrNotFound
	}
	if a.UserID != userID && projectID > 0 && !storage.HasProjectPerm(projectID, writeRole, storage.PermProjectManage) {
		return fmt.Errorf("%w: only the uploader or a project manager can remove this file", ErrForbidden)
	}
	return storage.DeleteTaskAttachment(taskID, id)
}

// FormatBytes renders a byte count as a short human size (e.g. "5 MB").
func FormatBytes(n int64) string {
	const unit = 1024
	switch {
	case n >= unit*unit:
		v := float64(n) / (unit * unit)
		if v == float64(int64(v)) {
			return fmt.Sprintf("%d MB", int64(v))
		}
		return fmt.Sprintf("%.1f MB", v)
	case n >= unit:
		return fmt.Sprintf("%d KB", n/unit)
	}
	return fmt.Sprintf("%d bytes", n)
}
