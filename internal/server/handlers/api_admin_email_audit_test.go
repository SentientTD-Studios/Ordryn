package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"GoTodo/internal/mailer"
	"GoTodo/internal/storage"
)

func TestAPIV1AdminEmailAuditMethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v2/admin/email-audit", nil)
	rec := httptest.NewRecorder()
	APIV1AdminEmailAudit(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestAPIV1AdminEmailAuditRejectsBadStatus(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v2/admin/email-audit?status=nope", nil)
	rec := httptest.NewRecorder()
	APIV1AdminEmailAudit(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestParseEmailAuditListQuery(t *testing.T) {
	tests := []struct {
		name    string
		rawURL  string
		wantErr string
		check   func(t *testing.T, f storage.EmailAuditFilter)
	}{
		{
			name:   "defaults",
			rawURL: "/api/v2/admin/email-audit",
			check: func(t *testing.T, f storage.EmailAuditFilter) {
				t.Helper()
				if f.Limit != 50 || f.Offset != 0 || f.Status != "" || f.Trigger != "" {
					t.Fatalf("unexpected defaults: %+v", f)
				}
			},
		},
		{
			name:   "filters",
			rawURL: "/api/v2/admin/email-audit?status=failed&trigger=password_reset&q=user@&limit=10&offset=20",
			check: func(t *testing.T, f storage.EmailAuditFilter) {
				t.Helper()
				if f.Status != mailer.StatusFailed || f.Trigger != mailer.TriggerPasswordReset {
					t.Fatalf("status/trigger = %q %q", f.Status, f.Trigger)
				}
				if f.Query != "user@" || f.Limit != 10 || f.Offset != 20 {
					t.Fatalf("query/limit/offset = %+v", f)
				}
			},
		},
		{
			name:   "date range",
			rawURL: "/api/v2/admin/email-audit?from=2026-08-01&to=2026-08-28",
			check: func(t *testing.T, f storage.EmailAuditFilter) {
				t.Helper()
				if f.From == nil || f.To == nil {
					t.Fatal("expected from and to")
				}
				if f.From.Format("2006-01-02") != "2026-08-01" {
					t.Fatalf("from = %s", f.From)
				}
				wantEnd := time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC).Add(24*time.Hour - time.Nanosecond)
				if !f.To.Equal(wantEnd) {
					t.Fatalf("to = %s, want %s", f.To, wantEnd)
				}
			},
		},
		{
			name:   "rate limited status",
			rawURL: "/api/v2/admin/email-audit?status=rate_limited",
			check: func(t *testing.T, f storage.EmailAuditFilter) {
				t.Helper()
				if f.Status != mailer.StatusRateLimited {
					t.Fatalf("status = %q", f.Status)
				}
			},
		},
		{
			name:    "bad status",
			rawURL:  "/api/v2/admin/email-audit?status=nope",
			wantErr: "status must be sent, failed, not_configured, or rate_limited.",
		},
		{
			name:    "bad trigger",
			rawURL:  "/api/v2/admin/email-audit?trigger=welcome",
			wantErr: "Unknown trigger.",
		},
		{
			name:    "bad from",
			rawURL:  "/api/v2/admin/email-audit?from=yesterday",
			wantErr: "from must be an RFC3339 timestamp or YYYY-MM-DD date.",
		},
		{
			name:    "bad limit",
			rawURL:  "/api/v2/admin/email-audit?limit=0",
			wantErr: "limit must be a positive integer.",
		},
		{
			name:    "bad offset",
			rawURL:  "/api/v2/admin/email-audit?offset=-1",
			wantErr: "offset must be a non-negative integer.",
		},
		{
			name:   "limit capped",
			rawURL: "/api/v2/admin/email-audit?limit=500",
			check: func(t *testing.T, f storage.EmailAuditFilter) {
				t.Helper()
				if f.Limit != 100 {
					t.Fatalf("limit = %d, want 100", f.Limit)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.rawURL, nil)
			f, errMsg := parseEmailAuditListQuery(req)
			if tt.wantErr != "" {
				if errMsg != tt.wantErr {
					t.Fatalf("err = %q, want %q", errMsg, tt.wantErr)
				}
				return
			}
			if errMsg != "" {
				t.Fatalf("unexpected err %q", errMsg)
			}
			if tt.check != nil {
				tt.check(t, f)
			}
		})
	}
}

func TestAPIEmailAuditJSONShape(t *testing.T) {
	raw, err := json.Marshal(apiEmailAuditListResponse{
		Items: []apiEmailAuditJSON{{
			ID:        1,
			CreatedAt: "2026-08-28T01:02:03Z",
			Trigger:   mailer.TriggerPasswordReset,
			ToEmail:   "user@example.com",
			Status:    mailer.StatusFailed,
			Error:     "connection refused",
			Provider:  mailer.ProviderSMTP,
		}},
		Total:  1,
		Limit:  50,
		Offset: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"items", "total", "limit", "offset"} {
		if _, ok := m[key]; !ok {
			t.Fatalf("missing key %q in %s", key, string(raw))
		}
	}
}
