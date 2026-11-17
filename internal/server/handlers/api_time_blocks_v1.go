package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"GoTodo/internal/domain"
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
)

type apiTimeBlockJSON struct {
	ID            int    `json:"id"`
	TaskID        int    `json:"task_id"`
	TaskTitle     string `json:"task_title"`
	TaskCompleted bool   `json:"task_completed"`
	ProjectID     int    `json:"project_id,omitempty"`
	ProjectName   string `json:"project_name,omitempty"`
	StartAt       string `json:"start_at"`
	EndAt         string `json:"end_at"`
	Minutes       int    `json:"minutes"`
	Logged        bool   `json:"logged"`
	TimeEntryID   int    `json:"time_entry_id,omitempty"`
	CreatedAt     string `json:"created_at"`
}

type apiTimeBlockSpanRequest struct {
	TaskID  int    `json:"task_id"`
	StartAt string `json:"start_at"`
	EndAt   string `json:"end_at"`
}

type apiTimeBlockLogRequest struct {
	Note string `json:"note"`
}

func timeBlockToAPIJSON(b storage.TimeBlock) apiTimeBlockJSON {
	return apiTimeBlockJSON{
		ID:            b.ID,
		TaskID:        b.TaskID,
		TaskTitle:     b.TaskTitle,
		TaskCompleted: b.TaskCompleted,
		ProjectID:     b.ProjectID,
		ProjectName:   b.ProjectName,
		StartAt:       b.StartAt.UTC().Format(time.RFC3339),
		EndAt:         b.EndAt.UTC().Format(time.RFC3339),
		Minutes:       b.Minutes(),
		Logged:        b.TimeEntryID != 0,
		TimeEntryID:   b.TimeEntryID,
		CreatedAt:     b.CreatedAt.UTC().Format(time.RFC3339),
	}
}

func timeBlocksToAPIJSON(blocks []storage.TimeBlock) []apiTimeBlockJSON {
	out := make([]apiTimeBlockJSON, 0, len(blocks))
	for _, b := range blocks {
		out = append(out, timeBlockToAPIJSON(b))
	}
	return out
}

func parseTimeBlockSpan(req apiTimeBlockSpanRequest) (domain.TimeBlockSpan, bool) {
	start, err1 := time.Parse(time.RFC3339, strings.TrimSpace(req.StartAt))
	end, err2 := time.Parse(time.RFC3339, strings.TrimSpace(req.EndAt))
	if err1 != nil || err2 != nil {
		return domain.TimeBlockSpan{}, false
	}
	return domain.TimeBlockSpan{StartAt: start, EndAt: end}, true
}

func writeTimeBlockJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// APIV1TimeBlocksRouter serves /api/v2/time-blocks: the caller's own calendar
// of time set aside for tasks.
//
//	GET    /time-blocks?from=&to=   blocks overlapping [from, to) (RFC 3339)
//	POST   /time-blocks             {task_id, start_at, end_at}
//	PATCH  /time-blocks/{id}        {start_at, end_at}
//	DELETE /time-blocks/{id}
//	POST   /time-blocks/{id}/log    {note} records the block as time spent
func APIV1TimeBlocksRouter(w http.ResponseWriter, r *http.Request) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	sub := strings.Trim(utils.ParseAPIV1Subpath(r, "time-blocks"), "/")
	var parts []string
	if sub != "" {
		parts = strings.Split(sub, "/")
	}

	if len(parts) == 0 {
		switch r.Method {
		case http.MethodGet:
			q := r.URL.Query()
			from, err1 := time.Parse(time.RFC3339, strings.TrimSpace(q.Get("from")))
			to, err2 := time.Parse(time.RFC3339, strings.TrimSpace(q.Get("to")))
			if err1 != nil || err2 != nil {
				utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "from and to must be RFC 3339 timestamps.")
				return
			}
			blocks, err := domain.ListTimeBlocksForUser(r.Context(), userID, from, to)
			if err != nil {
				writeWorkflowDomainError(w, err)
				return
			}
			writeTimeBlockJSON(w, http.StatusOK, timeBlocksToAPIJSON(blocks))
		case http.MethodPost:
			var req apiTimeBlockSpanRequest
			if err := decodeJSONBody(r, &req); err != nil {
				utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
				return
			}
			span, ok := parseTimeBlockSpan(req)
			if !ok || req.TaskID <= 0 {
				utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "task_id, start_at and end_at (RFC 3339) are required.")
				return
			}
			b, err := domain.CreateTimeBlockForUser(r.Context(), userID, req.TaskID, span)
			if err != nil {
				writeWorkflowDomainError(w, err)
				return
			}
			writeTimeBlockJSON(w, http.StatusCreated, timeBlockToAPIJSON(*b))
		default:
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		}
		return
	}

	blockID, err := strconv.Atoi(parts[0])
	if err != nil || blockID <= 0 || len(parts) > 2 {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid time block id.")
		return
	}

	if len(parts) == 2 {
		if parts[1] != "log" || r.Method != http.MethodPost {
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
			return
		}
		var req apiTimeBlockLogRequest
		if r.ContentLength != 0 {
			if err := decodeJSONBody(r, &req); err != nil {
				utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
				return
			}
		}
		b, entry, err := domain.LogTimeBlockForUser(r.Context(), userID, blockID, req.Note)
		if err != nil {
			writeWorkflowDomainError(w, err)
			return
		}
		writeTimeBlockJSON(w, http.StatusOK, map[string]interface{}{
			"block":      timeBlockToAPIJSON(*b),
			"time_entry": timeEntryToAPIJSON(*entry),
		})
		return
	}

	switch r.Method {
	case http.MethodPatch:
		var req apiTimeBlockSpanRequest
		if err := decodeJSONBody(r, &req); err != nil {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
			return
		}
		span, ok := parseTimeBlockSpan(req)
		if !ok {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "start_at and end_at (RFC 3339) are required.")
			return
		}
		b, err := domain.MoveTimeBlockForUser(r.Context(), userID, blockID, span)
		if err != nil {
			writeWorkflowDomainError(w, err)
			return
		}
		writeTimeBlockJSON(w, http.StatusOK, timeBlockToAPIJSON(*b))
	case http.MethodDelete:
		if err := domain.DeleteTimeBlockForUser(r.Context(), userID, blockID); err != nil {
			writeWorkflowDomainError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
	}
}

// handleTaskTimeBlocks serves GET /tasks/{id}/time-blocks: the caller's own blocks on the task.
func handleTaskTimeBlocks(w http.ResponseWriter, r *http.Request, taskID int) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	if r.Method != http.MethodGet {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	blocks, err := domain.ListTaskTimeBlocksForUser(r.Context(), userID, taskID)
	if err != nil {
		writeWorkflowDomainError(w, err)
		return
	}
	writeTimeBlockJSON(w, http.StatusOK, timeBlocksToAPIJSON(blocks))
}
