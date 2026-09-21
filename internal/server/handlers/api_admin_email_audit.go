package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"GoTodo/internal/mailer"
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
)

type apiEmailAuditJSON struct {
	ID        int    `json:"id"`
	CreatedAt string `json:"created_at"`
	Trigger   string `json:"trigger"`
	ToEmail   string `json:"to_email"`
	Status    string `json:"status"`
	Error     string `json:"error"`
	Provider  string `json:"provider"`
}

type apiEmailAuditListResponse struct {
	Items  []apiEmailAuditJSON `json:"items"`
	Total  int                 `json:"total"`
	Limit  int                 `json:"limit"`
	Offset int                 `json:"offset"`
}

// APIV1AdminEmailAudit handles GET /api/v2/admin/email-audit.
func APIV1AdminEmailAudit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	filter, errMsg := parseEmailAuditListQuery(r)
	if errMsg != "" {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", errMsg)
		return
	}
	items, total, err := storage.ListEmailAudit(filter)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load email audit log.")
		return
	}
	out := make([]apiEmailAuditJSON, 0, len(items))
	for _, row := range items {
		out = append(out, apiEmailAuditJSON{
			ID:        row.ID,
			CreatedAt: row.CreatedAt.UTC().Format(time.RFC3339),
			Trigger:   row.Trigger,
			ToEmail:   row.ToEmail,
			Status:    row.Status,
			Error:     row.Error,
			Provider:  row.Provider,
		})
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(apiEmailAuditListResponse{
		Items:  out,
		Total:  total,
		Limit:  filter.Limit,
		Offset: filter.Offset,
	})
}

func parseEmailAuditListQuery(r *http.Request) (storage.EmailAuditFilter, string) {
	q := r.URL.Query()
	f := storage.EmailAuditFilter{
		Query: strings.TrimSpace(q.Get("q")),
	}

	status := strings.TrimSpace(q.Get("status"))
	if status != "" {
		if !mailer.KnownStatus(status) {
			return f, "status must be sent, failed, not_configured, or rate_limited."
		}
		f.Status = status
	}

	trigger := strings.TrimSpace(q.Get("trigger"))
	if trigger != "" {
		if !mailer.KnownTrigger(trigger) {
			return f, "Unknown trigger."
		}
		f.Trigger = trigger
	}

	from, err := parseEmailAuditTime(q.Get("from"), false)
	if err != nil {
		return f, "from must be an RFC3339 timestamp or YYYY-MM-DD date."
	}
	f.From = from
	to, err := parseEmailAuditTime(q.Get("to"), true)
	if err != nil {
		return f, "to must be an RFC3339 timestamp or YYYY-MM-DD date."
	}
	f.To = to

	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		n, convErr := strconv.Atoi(raw)
		if convErr != nil || n <= 0 {
			return f, "limit must be a positive integer."
		}
		f.Limit = n
	}
	if f.Limit <= 0 {
		f.Limit = 50
	}
	if f.Limit > 100 {
		f.Limit = 100
	}
	if raw := strings.TrimSpace(q.Get("offset")); raw != "" {
		n, convErr := strconv.Atoi(raw)
		if convErr != nil || n < 0 {
			return f, "offset must be a non-negative integer."
		}
		f.Offset = n
	}
	return f, ""
}

func parseEmailAuditTime(raw string, endOfDay bool) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return &t, nil
	}
	t, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return nil, err
	}
	if endOfDay {
		t = t.Add(24*time.Hour - time.Nanosecond)
	}
	return &t, nil
}
