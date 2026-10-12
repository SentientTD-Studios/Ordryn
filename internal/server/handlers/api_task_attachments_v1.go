package handlers

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"GoTodo/internal/domain"
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
)

type apiAttachmentJSON struct {
	ID          int    `json:"id"`
	TaskID      int    `json:"task_id"`
	UserID      int    `json:"user_id"`
	UserName    string `json:"user_name"`
	FileName    string `json:"file_name"`
	ContentType string `json:"content_type"`
	SizeBytes   int64  `json:"size_bytes"`
	CreatedAt   string `json:"created_at"`
}

func attachmentJSON(a storage.TaskAttachment) apiAttachmentJSON {
	return apiAttachmentJSON{
		ID: a.ID, TaskID: a.TaskID, UserID: a.UserID, UserName: a.UserName, FileName: a.FileName,
		ContentType: a.ContentType, SizeBytes: a.SizeBytes, CreatedAt: a.CreatedAt.UTC().Format(time.RFC3339),
	}
}

// inlineAttachmentTypes are shown in the browser; everything else downloads.
var inlineAttachmentTypes = map[string]bool{
	"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true, "application/pdf": true,
}

func writeAttachmentError(w http.ResponseWriter, err error, fallback string) {
	switch {
	case errors.Is(err, domain.ErrValidation):
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", sharingClientMessage(err, "Invalid file."))
	case errors.Is(err, domain.ErrNotFound):
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Not found.")
	case errors.Is(err, domain.ErrForbidden):
		utils.APIJSONError(w, http.StatusForbidden, "forbidden", sharingClientMessage(err, "Forbidden."))
	default:
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", fallback)
	}
}

// apiV1TaskAttachments handles /api/v2/tasks/{id}/attachments[/{attachmentId}].
func apiV1TaskAttachments(w http.ResponseWriter, r *http.Request, taskID int, rest []string) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	if len(rest) == 0 || (len(rest) == 1 && rest[0] == "") {
		switch r.Method {
		case http.MethodGet:
			list, err := domain.ListTaskAttachmentsForUser(r.Context(), userID, taskID)
			if err != nil {
				writeAttachmentError(w, err, "Failed to list attachments.")
				return
			}
			out := make([]apiAttachmentJSON, 0, len(list))
			for _, a := range list {
				out = append(out, attachmentJSON(a))
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{"attachments": out})
		case http.MethodPost:
			apiV1UploadTaskAttachment(w, r, userID, taskID)
		default:
			w.Header().Set("Allow", "GET, POST")
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		}
		return
	}
	if len(rest) != 1 {
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Not found.")
		return
	}
	id, err := strconv.Atoi(rest[0])
	if err != nil || id <= 0 {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid attachment id.")
		return
	}
	switch r.Method {
	case http.MethodGet:
		apiV1DownloadTaskAttachment(w, r, userID, taskID, id)
	case http.MethodDelete:
		if err := domain.DeleteTaskAttachmentForUser(r.Context(), userID, taskID, id); err != nil {
			writeAttachmentError(w, err, "Failed to delete attachment.")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "GET, DELETE")
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
	}
}

func apiV1UploadTaskAttachment(w http.ResponseWriter, r *http.Request, userID, taskID int) {
	enabled, maxBytes := domain.AttachmentSettings()
	if !enabled {
		utils.APIJSONError(w, http.StatusBadRequest, "not_enabled", "File attachments are turned off on this site.")
		return
	}
	// Leave room for the multipart envelope; the file itself is checked below.
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes+256*1024)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		utils.APIJSONError(w, http.StatusRequestEntityTooLarge, "too_large",
			"The file is larger than the "+domain.FormatBytes(maxBytes)+" limit.")
		return
	}
	defer r.MultipartForm.RemoveAll()
	file, header, err := r.FormFile("file")
	if err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Missing file upload (field name: file).")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Failed to read file.")
		return
	}
	a, err := domain.UploadTaskAttachmentForUser(r.Context(), userID, taskID, header.Filename, data)
	if err != nil {
		writeAttachmentError(w, err, "Failed to save attachment.")
		return
	}
	writeJSON(w, http.StatusCreated, attachmentJSON(*a))
}

func apiV1DownloadTaskAttachment(w http.ResponseWriter, r *http.Request, userID, taskID, id int) {
	a, data, err := domain.GetTaskAttachmentForUser(r.Context(), userID, taskID, id)
	if err != nil {
		writeAttachmentError(w, err, "Failed to load attachment.")
		return
	}
	contentType := strings.TrimSpace(strings.Split(a.ContentType, ";")[0])
	disposition := "attachment"
	if inlineAttachmentTypes[contentType] && r.URL.Query().Get("download") == "" {
		disposition = "inline"
	} else {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": a.FileName}))
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src 'self'; style-src 'unsafe-inline'; sandbox")
	w.Header().Set("Cache-Control", "private, max-age=0, must-revalidate")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
