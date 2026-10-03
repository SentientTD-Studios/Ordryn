package handlers

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
)

type apiAuditEventJSON struct {
	Key           string                 `json:"key"`
	Source        string                 `json:"source"`
	ID            int64                  `json:"id"`
	CreatedAt     string                 `json:"created_at"`
	EventType     string                 `json:"event_type"`
	Summary       string                 `json:"summary,omitempty"`
	ActorUserID   int                    `json:"actor_user_id"`
	ActorUserName string                 `json:"actor_user_name"`
	ActorEmail    string                 `json:"actor_email"`
	TargetType    string                 `json:"target_type"`
	TargetID      int64                  `json:"target_id"`
	TargetLabel   string                 `json:"target_label"`
	ProjectID     int                    `json:"project_id"`
	ProjectName   string                 `json:"project_name"`
	Metadata      map[string]interface{} `json:"metadata"`
}

type apiAuditListResponse struct {
	Items  []apiAuditEventJSON `json:"items"`
	Total  int                 `json:"total"`
	Limit  int                 `json:"limit"`
	Offset int                 `json:"offset"`
}

type apiAuditEventTypeJSON struct {
	Source    string `json:"source"`
	EventType string `json:"event_type"`
}

type apiAuditProjectJSON struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Archived bool   `json:"archived"`
}

type apiAuditFacetsJSON struct {
	EventTypes []apiAuditEventTypeJSON `json:"event_types"`
	Projects   []apiAuditProjectJSON   `json:"projects"`
}

// APIV1AdminAuditRouter handles /api/v2/admin/audit, /audit/export (CSV) and /audit/facets.
func APIV1AdminAuditRouter(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	switch strings.Trim(utils.ParseAPIV1Subpath(r, "admin/audit"), "/") {
	case "":
		apiV1AdminAuditList(w, r)
	case "export":
		apiV1AdminAuditExport(w, r)
	case "facets":
		apiV1AdminAuditFacets(w, r)
	default:
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Not found.")
	}
}

func apiV1AdminAuditList(w http.ResponseWriter, r *http.Request) {
	filter, errMsg := parseAuditListQuery(r)
	if errMsg != "" {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", errMsg)
		return
	}
	items, total, err := storage.ListAuditEvents(r.Context(), filter)
	if err != nil {
		log.Printf("admin audit: list: %v", err)
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load audit log.")
		return
	}
	out := make([]apiAuditEventJSON, 0, len(items))
	for _, ev := range items {
		out = append(out, auditEventToJSON(ev))
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(apiAuditListResponse{
		Items:  out,
		Total:  total,
		Limit:  filter.Limit,
		Offset: filter.Offset,
	})
}

// apiV1AdminAuditFacets returns the filter choices: event types on record and all projects.
func apiV1AdminAuditFacets(w http.ResponseWriter, r *http.Request) {
	types, err := storage.ListAuditEventTypes(r.Context())
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load audit filters.")
		return
	}
	projects, err := storage.ListAuditProjects(r.Context())
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load audit filters.")
		return
	}
	out := apiAuditFacetsJSON{
		EventTypes: make([]apiAuditEventTypeJSON, 0, len(types)),
		Projects:   make([]apiAuditProjectJSON, 0, len(projects)),
	}
	for _, t := range types {
		out.EventTypes = append(out.EventTypes, apiAuditEventTypeJSON{Source: t.Source, EventType: t.EventType})
	}
	for _, p := range projects {
		out.Projects = append(out.Projects, apiAuditProjectJSON{ID: p.ID, Name: p.Name, Archived: p.Archived})
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(out)
}

var auditCSVHeader = []string{
	"created_at", "source", "event_type", "summary",
	"actor_user_id", "actor_user_name", "actor_email",
	"target_type", "target_id", "target_label",
	"project_id", "project_name", "from", "to", "metadata",
}

func apiV1AdminAuditExport(w http.ResponseWriter, r *http.Request) {
	filter, errMsg := parseAuditListQuery(r)
	if errMsg != "" {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", errMsg)
		return
	}
	filename := "audit-" + time.Now().UTC().Format("20060102-150405") + ".csv"
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.Header().Set("Cache-Control", "no-store")

	cw := csv.NewWriter(w)
	_ = cw.Write(auditCSVHeader)
	err := storage.EachAuditEvent(r.Context(), filter, func(ev storage.AuditEvent) error {
		return cw.Write(auditEventCSVRow(ev))
	})
	cw.Flush()
	if err != nil {
		// Headers are already sent; the truncated file is the best we can do.
		log.Printf("admin audit: export: %v", err)
	}
}

func auditEventToJSON(ev storage.AuditEvent) apiAuditEventJSON {
	meta := ev.Metadata
	if meta == nil {
		meta = map[string]interface{}{}
	}
	return apiAuditEventJSON{
		Key:           ev.Source + ":" + strconv.FormatInt(ev.ID, 10),
		Source:        ev.Source,
		ID:            ev.ID,
		CreatedAt:     ev.CreatedAt.UTC().Format(time.RFC3339),
		EventType:     ev.EventType,
		Summary:       auditEventSummary(ev),
		ActorUserID:   ev.ActorUserID,
		ActorUserName: ev.ActorUserName,
		ActorEmail:    ev.ActorEmail,
		TargetType:    ev.TargetType,
		TargetID:      ev.TargetID,
		TargetLabel:   ev.TargetLabel,
		ProjectID:     ev.ProjectID,
		ProjectName:   ev.ProjectName,
		Metadata:      meta,
	}
}

// auditEventSummary reuses the task activity labels; other sources render client-side.
func auditEventSummary(ev storage.AuditEvent) string {
	if ev.Source != storage.AuditSourceTask {
		return ""
	}
	label := formatEventLabel(ev.EventType, ev.Metadata)
	if label == ev.EventType {
		return ""
	}
	return label
}

func auditEventCSVRow(ev storage.AuditEvent) []string {
	metaJSON := "{}"
	if len(ev.Metadata) > 0 {
		if b, err := json.Marshal(ev.Metadata); err == nil {
			metaJSON = string(b)
		}
	}
	row := []string{
		ev.CreatedAt.UTC().Format(time.RFC3339),
		ev.Source,
		ev.EventType,
		auditEventSummary(ev),
		strconv.Itoa(ev.ActorUserID),
		ev.ActorUserName,
		ev.ActorEmail,
		ev.TargetType,
		strconv.FormatInt(ev.TargetID, 10),
		ev.TargetLabel,
		strconv.Itoa(ev.ProjectID),
		ev.ProjectName,
		auditMetaString(ev.Metadata["from"]),
		auditMetaString(ev.Metadata["to"]),
		metaJSON,
	}
	for i, cell := range row {
		row[i] = csvSafeCell(cell)
	}
	return row
}

func auditMetaString(v interface{}) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	default:
		if b, err := json.Marshal(t); err == nil {
			return string(b)
		}
		return fmt.Sprint(t)
	}
}

// csvSafeCell neutralizes spreadsheet formula injection in user-controlled text.
func csvSafeCell(s string) string {
	if s == "" {
		return s
	}
	switch s[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + s
	}
	return s
}

func parseAuditListQuery(r *http.Request) (storage.AuditFilter, string) {
	q := r.URL.Query()
	f := storage.AuditFilter{Limit: 50}

	if raw := strings.TrimSpace(q.Get("user_id")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			return f, "user_id must be a positive integer."
		}
		f.UserID = n
	}
	if raw := strings.TrimSpace(q.Get("project_id")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			return f, "project_id must be a positive integer."
		}
		f.ProjectID = n
	}
	if src := strings.TrimSpace(q.Get("source")); src != "" {
		if !storage.KnownAuditSource(src) {
			return f, "source must be task, project, comment, email, or admin."
		}
		f.Source = src
	}
	if et := strings.TrimSpace(q.Get("event_type")); et != "" {
		if len(et) > 64 {
			return f, "event_type is too long."
		}
		f.EventType = et
	}

	since, err := parseEmailAuditTime(q.Get("since"), false)
	if err != nil {
		return f, "since must be an RFC3339 timestamp or YYYY-MM-DD date."
	}
	f.Since = since
	until, err := parseEmailAuditTime(q.Get("until"), true)
	if err != nil {
		return f, "until must be an RFC3339 timestamp or YYYY-MM-DD date."
	}
	f.Until = until
	if f.Since != nil && f.Until != nil && f.Until.Before(*f.Since) {
		return f, "until must not be before since."
	}

	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			return f, "limit must be a positive integer."
		}
		f.Limit = n
	}
	if f.Limit > 100 {
		f.Limit = 100
	}
	if raw := strings.TrimSpace(q.Get("offset")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			return f, "offset must be a non-negative integer."
		}
		f.Offset = n
	}
	return f, ""
}
