package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"GoTodo/internal/storage"
)

func TestAPIV1AdminAuditRouterMethodNotAllowed(t *testing.T) {
	for _, path := range []string{"/api/v2/admin/audit", "/api/v2/admin/audit/export"} {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		rec := httptest.NewRecorder()
		APIV1AdminAuditRouter(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s: status = %d, want %d", path, rec.Code, http.StatusMethodNotAllowed)
		}
	}
}

func TestAPIV1AdminAuditRouterUnknownSubpath(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v2/admin/audit/nope", nil)
	rec := httptest.NewRecorder()
	APIV1AdminAuditRouter(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestAPIV1AdminAuditRejectsBadParams(t *testing.T) {
	for _, q := range []string{
		"user_id=abc", "project_id=0", "source=users", "limit=0", "offset=-1",
		"since=invalid", "since=2026-02-01&until=2026-01-01",
	} {
		for _, path := range []string{"/api/v2/admin/audit?", "/api/v2/admin/audit/export?"} {
			req := httptest.NewRequest(http.MethodGet, path+q, nil)
			rec := httptest.NewRecorder()
			APIV1AdminAuditRouter(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("%s%s: status = %d, want %d", path, q, rec.Code, http.StatusBadRequest)
			}
		}
	}
}

func TestParseAuditListQuery(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/admin/audit?user_id=5&project_id=3&source=task&event_type=status_changed&since=2026-08-01&until=2026-08-28&limit=500&offset=10", nil)
	f, errMsg := parseAuditListQuery(req)
	if errMsg != "" {
		t.Fatalf("unexpected error: %s", errMsg)
	}
	if f.UserID != 5 || f.ProjectID != 3 || f.Source != "task" || f.EventType != "status_changed" {
		t.Fatalf("filter = %+v", f)
	}
	if f.Limit != 100 || f.Offset != 10 {
		t.Fatalf("limit/offset = %d/%d, want 100/10", f.Limit, f.Offset)
	}
	if f.Since == nil || f.Since.Format("2006-01-02") != "2026-08-01" {
		t.Fatalf("since = %v", f.Since)
	}
	wantEnd := time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC).Add(24*time.Hour - time.Nanosecond)
	if f.Until == nil || !f.Until.Equal(wantEnd) {
		t.Fatalf("until = %v, want %v (inclusive end of day)", f.Until, wantEnd)
	}

	f, errMsg = parseAuditListQuery(httptest.NewRequest(http.MethodGet, "/api/v2/admin/audit", nil))
	if errMsg != "" || f.Limit != 50 || f.Offset != 0 || f.Since != nil || f.Until != nil {
		t.Fatalf("defaults = %+v (%s)", f, errMsg)
	}
}

func TestAuditEventCSVRowEscapesFormulas(t *testing.T) {
	ev := storage.AuditEvent{
		Source:      storage.AuditSourceTask,
		ID:          7,
		CreatedAt:   time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC),
		EventType:   "title_changed",
		TargetType:  "task",
		TargetID:    42,
		TargetLabel: "=HYPERLINK(\"http://evil\")",
		Metadata:    map[string]interface{}{"from": "+old", "to": "new"},
	}
	row := auditEventCSVRow(ev)
	if len(row) != len(auditCSVHeader) {
		t.Fatalf("row has %d cells, header has %d", len(row), len(auditCSVHeader))
	}
	byName := map[string]string{}
	for i, h := range auditCSVHeader {
		byName[h] = row[i]
	}
	if byName["target_label"] != "'=HYPERLINK(\"http://evil\")" {
		t.Errorf("target_label = %q", byName["target_label"])
	}
	if byName["from"] != "'+old" || byName["to"] != "new" {
		t.Errorf("from/to = %q/%q", byName["from"], byName["to"])
	}
	if byName["created_at"] != "2026-09-01T12:00:00Z" {
		t.Errorf("created_at = %q", byName["created_at"])
	}
	if byName["summary"] != "Title · new" {
		t.Errorf("summary = %q", byName["summary"])
	}
}

func TestSiteSettingsAuditDiffOmitsSecrets(t *testing.T) {
	before := &storage.SiteSettings{SiteName: "Old", DefaultTimezone: "UTC", ImageS3SecretKeyEnc: "enc-a"}
	after := &storage.SiteSettings{SiteName: "New", DefaultTimezone: "UTC", ImageS3SecretKeyEnc: "enc-b", AuditRetentionDays: 30}
	changes := siteSettingsAuditDiff(before, after)

	name, ok := changes["site_name"].(map[string]interface{})
	if !ok || name["from"] != "Old" || name["to"] != "New" {
		t.Fatalf("site_name change = %#v", changes["site_name"])
	}
	if _, ok := changes["audit_retention_days"]; !ok {
		t.Error("expected audit_retention_days change")
	}
	if _, ok := changes["default_timezone"]; ok {
		t.Error("unchanged field should not be reported")
	}
	secret, ok := changes["image_s3_secret_key"].(map[string]interface{})
	if !ok || secret["changed"] != true || len(secret) != 1 {
		t.Fatalf("secret change = %#v; must not include values", changes["image_s3_secret_key"])
	}
	if _, ok := changes["image_s3_secret_key_set"]; ok {
		t.Error("_set flags should be folded into the secret entry")
	}
}
