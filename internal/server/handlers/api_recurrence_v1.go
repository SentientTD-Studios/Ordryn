package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"GoTodo/internal/domain"
	"GoTodo/internal/recurrence"
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
)

// apiRecurrenceJSON is the repeat rule as returned on tasks and by
// GET /api/v2/tasks/{id}/recurrence.
type apiRecurrenceJSON struct {
	Frequency  string  `json:"frequency"`
	Interval   int     `json:"interval"`
	Weekdays   []int   `json:"weekdays"`
	MonthDay   *int    `json:"month_day"`
	Basis      string  `json:"basis"`
	EndsOn     *string `json:"ends_on"`
	EndAfter   *int    `json:"end_after"`
	Occurrence int     `json:"occurrence"`
	SeriesID   int     `json:"series_id"`
	Summary    string  `json:"summary"`
	NextDue    string  `json:"next_due,omitempty"`
}

// apiRecurrenceInput is the rule payload accepted on create, PATCH, and PUT.
type apiRecurrenceInput struct {
	Frequency string `json:"frequency"`
	Interval  int    `json:"interval"`
	Weekdays  []int  `json:"weekdays"`
	MonthDay  *int   `json:"month_day"`
	Basis     string `json:"basis"`
	EndsOn    string `json:"ends_on"`
	EndAfter  *int   `json:"end_after"`
}

func (in apiRecurrenceInput) toDomain() domain.RecurrenceInput {
	out := domain.RecurrenceInput{
		Frequency: in.Frequency,
		Interval:  in.Interval,
		Weekdays:  in.Weekdays,
		Basis:     in.Basis,
		EndsOn:    in.EndsOn,
	}
	if in.MonthDay != nil {
		out.MonthDay = *in.MonthDay
	}
	if in.EndAfter != nil {
		out.EndAfter = *in.EndAfter
	}
	return out
}

// optionalRecurrence distinguishes omitted / null (clear) / object (set) on PATCH.
type optionalRecurrence struct {
	Set   bool
	Null  bool
	Value apiRecurrenceInput
}

func (o *optionalRecurrence) UnmarshalJSON(b []byte) error {
	o.Set = true
	if string(b) == "null" {
		o.Null = true
		return nil
	}
	return json.Unmarshal(b, &o.Value)
}

type apiRecurrenceHistoryItem struct {
	TaskID      int     `json:"task_id"`
	Title       string  `json:"title"`
	DueDate     string  `json:"due_date"`
	Completed   bool    `json:"completed"`
	CompletedAt *string `json:"completed_at"`
	CreatedAt   string  `json:"created_at"`
}

type apiRecurrenceDetailJSON struct {
	TaskID     int                        `json:"task_id"`
	Recurrence *apiRecurrenceJSON         `json:"recurrence"`
	SeriesID   *int                       `json:"series_id"`
	PrevTaskID *int                       `json:"prev_task_id"`
	NextTaskID *int                       `json:"next_task_id"`
	CanEdit    bool                       `json:"can_edit"`
	IsSubtask  bool                       `json:"is_subtask"`
	History    []apiRecurrenceHistoryItem `json:"history"`
}

func recurrenceToAPIJSON(r storage.TaskRecurrence) *apiRecurrenceJSON {
	rule := domain.RuleFromStorage(r)
	out := &apiRecurrenceJSON{
		Frequency:  rule.Frequency,
		Interval:   rule.Interval,
		Weekdays:   rule.Weekdays,
		Basis:      rule.Basis,
		Occurrence: r.Occurrence,
		SeriesID:   r.SeriesID,
		Summary:    rule.Summary(),
	}
	if out.Weekdays == nil {
		out.Weekdays = []int{}
	}
	if rule.MonthDay > 0 {
		md := rule.MonthDay
		out.MonthDay = &md
	}
	if rule.EndsOn != "" {
		e := rule.EndsOn
		out.EndsOn = &e
	}
	if rule.EndAfter > 0 {
		n := rule.EndAfter
		out.EndAfter = &n
	}
	return out
}

func optionalPositive(v int) *int {
	if v <= 0 {
		return nil
	}
	return &v
}

// validateRecurrenceInputForAPI checks a rule's shape before any write so a
// bad rule on create does not leave a half-created task behind.
func validateRecurrenceInputForAPI(in apiRecurrenceInput) error {
	d := in.toDomain()
	rule := recurrence.Rule{
		Frequency: d.Frequency, Interval: d.Interval, Weekdays: d.Weekdays, MonthDay: d.MonthDay,
		Basis: d.Basis, EndsOn: d.EndsOn, EndAfter: d.EndAfter,
	}
	rule.Normalize()
	return rule.Validate()
}

func writeRecurrenceDomainError(w http.ResponseWriter, err error, fallback string) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Task not found.")
	case errors.Is(err, domain.ErrValidation), errors.Is(err, recurrence.ErrInvalid):
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", err.Error())
	case errors.Is(err, domain.ErrForbidden):
		utils.APIJSONError(w, http.StatusForbidden, "forbidden", sharingClientMessage(err, "Forbidden."))
	case errors.Is(err, domain.ErrConflict):
		utils.APIJSONError(w, http.StatusConflict, "conflict", err.Error())
	default:
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", fallback)
	}
}

// apiV1TaskRecurrence handles GET/PUT/DELETE /api/v2/tasks/{id}/recurrence.
func apiV1TaskRecurrence(w http.ResponseWriter, r *http.Request, taskID int) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	switch r.Method {
	case http.MethodGet:
	case http.MethodPut:
		var req apiRecurrenceInput
		if err := decodeJSONBody(r, &req); err != nil {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
			return
		}
		in := req.toDomain()
		if _, err := domain.SetTaskRecurrence(r.Context(), userID, taskID, &in); err != nil {
			writeRecurrenceDomainError(w, err, "Failed to save recurrence.")
			return
		}
	case http.MethodDelete:
		if _, err := domain.SetTaskRecurrence(r.Context(), userID, taskID, nil); err != nil {
			writeRecurrenceDomainError(w, err, "Failed to clear recurrence.")
			return
		}
	default:
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	writeTaskRecurrenceDetail(w, r, userID, taskID)
}

func writeTaskRecurrenceDetail(w http.ResponseWriter, r *http.Request, userID, taskID int) {
	detail, err := domain.GetTaskRecurrenceDetail(r.Context(), userID, taskID)
	if err != nil {
		writeRecurrenceDomainError(w, err, "Failed to load recurrence.")
		return
	}
	out := apiRecurrenceDetailJSON{
		TaskID:     taskID,
		SeriesID:   optionalPositive(detail.SeriesID),
		PrevTaskID: optionalPositive(detail.PrevTaskID),
		NextTaskID: optionalPositive(detail.NextTaskID),
		CanEdit:    detail.CanEdit,
		IsSubtask:  detail.IsSubtask,
		History:    make([]apiRecurrenceHistoryItem, 0, len(detail.History)),
	}
	if detail.Rule != nil {
		rec, err := storage.GetTaskRecurrence(taskID)
		if err == nil && rec != nil {
			out.Recurrence = recurrenceToAPIJSON(*rec)
			out.Recurrence.NextDue = detail.Rule.NextDue
		}
	}
	for _, h := range detail.History {
		item := apiRecurrenceHistoryItem{
			TaskID:    h.TaskID,
			Title:     h.Title,
			DueDate:   h.DueDate,
			Completed: h.Completed,
			CreatedAt: h.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
		}
		if h.CompletedAt != nil {
			s := h.CompletedAt.UTC().Format("2006-01-02T15:04:05Z07:00")
			item.CompletedAt = &s
		}
		out.History = append(out.History, item)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(out)
}

// isEmptyTaskUpdate reports whether a PATCH carries no task field changes.
func isEmptyTaskUpdate(in domain.UpdateTaskInput) bool {
	return in.Title == nil && in.Description == nil && in.DueDate == nil && !in.ClearDue &&
		in.ProjectID == nil && in.ParentID == nil && in.Priority == nil && in.Completed == nil &&
		in.TagIDs == nil && in.StatusID == nil && in.EstimatePoints == nil && in.SprintID == nil &&
		in.Fields == nil
}
