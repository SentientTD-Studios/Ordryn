package handlers

import (
	"net/http"
	"time"

	"GoTodo/internal/domain"
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
)

func auditStreamJSON(s *storage.AuditStreamSettings) map[string]interface{} {
	ts := func(t *time.Time) interface{} {
		if t == nil {
			return nil
		}
		return t.UTC().Format(time.RFC3339)
	}
	return map[string]interface{}{
		"enabled": s.Enabled, "url": s.URL, "secret_set": s.SecretEnc != "", "auth_header_set": s.HeaderValueEnc != "",
		"include_project": s.IncludeProject, "include_task": s.IncludeTask,
		"last_success_at": ts(s.LastSuccessAt), "last_error": s.LastError, "last_error_at": ts(s.LastErrorAt),
	}
}

// APIV1AdminAuditStream handles GET/PUT /api/v2/admin/audit-stream and
// POST /api/v2/admin/audit-stream/test (admins only).
func APIV1AdminAuditStream(w http.ResponseWriter, r *http.Request) {
	if utils.ParseAPIV1Subpath(r, "admin/audit-stream") == "test" {
		if r.Method != http.MethodPost {
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
			return
		}
		if err := domain.SendAuditStreamTest(r.Context()); err != nil {
			utils.APIJSONError(w, http.StatusBadGateway, "audit_stream_failed", "Test delivery failed: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}
	switch r.Method {
	case http.MethodGet:
		s, err := storage.GetAuditStreamSettings()
		if err != nil {
			utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load.")
			return
		}
		writeJSON(w, http.StatusOK, auditStreamJSON(s))
	case http.MethodPut:
		var req struct {
			Enabled        *bool   `json:"enabled"`
			URL            *string `json:"url"`
			Secret         *string `json:"secret"`
			AuthHeader     *string `json:"auth_header"`
			IncludeProject *bool   `json:"include_project"`
			IncludeTask    *bool   `json:"include_task"`
		}
		if err := decodeJSONBody(r, &req); err != nil {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
			return
		}
		s, err := domain.UpdateAuditStream(domain.AuditStreamInput{
			Enabled: req.Enabled, URL: req.URL, Secret: req.Secret, AuthHeader: req.AuthHeader,
			IncludeProject: req.IncludeProject, IncludeTask: req.IncludeTask,
		})
		if err != nil {
			writePeopleDomainError(w, err)
			return
		}
		logAdminEvent(r, "audit_stream_updated", "site", 1, "Audit stream", map[string]interface{}{
			"enabled": s.Enabled, "include_project": s.IncludeProject, "include_task": s.IncludeTask,
		})
		writeJSON(w, http.StatusOK, auditStreamJSON(s))
	default:
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
	}
}
