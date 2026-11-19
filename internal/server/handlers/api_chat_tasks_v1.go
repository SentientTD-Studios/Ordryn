package handlers

import (
	"errors"
	"net/http"
	"regexp"
	"strings"

	"GoTodo/internal/domain"
	"GoTodo/internal/hooks"
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
)

// POST /api/v2/chat/links/{linkId}/tasks: Ordryn Chat turns a chat message
// into a task in the link's project, on behalf of one of its users.
//
// Allowed when the chat server has both "Task events" and "Task creation"
// turned on, the link is active, and the user (not banned, not an agent) may
// add tasks to the project; domain.CreateTask makes that last check. The task
// is created as that user.
//
// source_key (e.g. "chatmsg_42") is unique per chat server: asking again with
// the same key returns the task already made (200) rather than a new one
// (201), until that task is deleted. The task.created event for it carries
// the key, so chat can place its update next to the message.

var chatSourceKeyRe = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,100}$`)

// maxChatTaskTitle caps a title from chat, in characters.
const maxChatTaskTitle = 200

func apiV2ChatCreateTask(w http.ResponseWriter, r *http.Request, cs *storage.ChatServer, linkID string) {
	if r.Method != http.MethodPost {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	if !cs.TaskCreate {
		utils.APIJSONError(w, http.StatusForbidden, "task_create_disabled",
			"Creating tasks isn't allowed for this chat server. An Ordryn admin can turn on \"Task creation\" for it under Admin → Chat servers.")
		return
	}
	var req struct {
		UserID      int    `json:"user_id"`
		Title       string `json:"title"`
		Description string `json:"description"`
		SourceKey   string `json:"source_key"`
	}
	if err := decodeJSONBody(r, &req); err != nil || req.UserID <= 0 {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "user_id, title and source_key are required.")
		return
	}
	title := clipRunes(strings.Join(strings.Fields(req.Title), " "), maxChatTaskTitle)
	if title == "" || !chatSourceKeyRe.MatchString(req.SourceKey) {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "user_id, title and source_key are required.")
		return
	}
	description := clipRunes(strings.TrimSpace(req.Description), domain.MaxDescriptionLength())

	link, err := storage.GetChatLink(cs.ID, linkID)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load the link.")
		return
	}
	if link == nil {
		utils.APIJSONError(w, http.StatusNotFound, "link_not_found", "This chat server has no link by that id.")
		return
	}
	if !link.Active {
		utils.APIJSONError(w, http.StatusConflict, "link_paused", "This link is paused: "+link.PausedReason)
		return
	}
	ok, err := storage.ChatUserCanAct(req.UserID)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to check the user.")
		return
	}
	if !ok {
		utils.APIJSONError(w, http.StatusForbidden, "forbidden", "This person can't create tasks in this project.")
		return
	}

	existing, err := storage.ReserveChatTaskSource(cs.ID, req.SourceKey, linkID)
	if errors.Is(err, storage.ErrChatTaskInProgress) {
		utils.APIJSONError(w, http.StatusConflict, "in_progress", "A task is already being created for this message.")
		return
	}
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to create the task.")
		return
	}
	status, taskID := http.StatusOK, existing
	if taskID == 0 {
		projectID := link.ProjectID
		taskID, err = domain.CreateTask(r.Context(), req.UserID, domain.CreateTaskInput{
			Title:       title,
			Description: description,
			ProjectID:   &projectID,
			// Recorded before the task.created event goes out, so the event carries the key.
			AfterInsert: func(id int) error { return storage.SetChatTaskSourceTask(cs.ID, req.SourceKey, id) },
		})
		if err != nil {
			_ = storage.ReleaseChatTaskSource(cs.ID, req.SourceKey)
			switch {
			case errors.Is(err, domain.ErrForbidden):
				utils.APIJSONError(w, http.StatusForbidden, "forbidden", "This person can't create tasks in this project.")
			case errors.Is(err, domain.ErrValidation), errors.Is(err, domain.ErrConflict):
				utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", sharingClientMessage(err, "The project isn't accepting new tasks."))
			case errors.Is(err, storage.ErrChatTaskInProgress):
				utils.APIJSONError(w, http.StatusConflict, "in_progress", "A task is already being created for this message.")
			default:
				utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to create the task.")
			}
			return
		}
		status = http.StatusCreated
	}
	sum, err := storage.GetChatTaskSummary(taskID)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Task created but failed to load.")
		return
	}
	sum.URL = hooks.PublicTaskURL(taskID)
	writeJSONStatus(w, status, sum)
}

// clipRunes caps s at n characters.
func clipRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}
