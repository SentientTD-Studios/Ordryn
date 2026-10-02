package storage

import (
	"strings"
	"testing"
	"time"
)

func TestBuildUnifiedAuditQueryArgs(t *testing.T) {
	since := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	until := since.Add(24 * time.Hour)
	tests := []struct {
		name     string
		filter   AuditFilter
		wantArgs int
		wantSQL  []string
	}{
		{"empty filter", AuditFilter{Limit: 50}, 2, nil},
		{"user", AuditFilter{UserID: 5}, 3, []string{"a.actor_user_id = $1", "LIMIT $2 OFFSET $3"}},
		{"project", AuditFilter{ProjectID: 3}, 3, []string{"a.project_id = $1"}},
		{"source", AuditFilter{Source: AuditSourceAdmin}, 3, []string{"a.source = $1"}},
		{"event type", AuditFilter{EventType: "status_changed"}, 3, []string{"a.event_type = $1"}},
		{"date range", AuditFilter{Since: &since, Until: &until}, 4, []string{"a.created_at >= $1", "a.created_at <= $2"}},
		{
			"all filters",
			AuditFilter{UserID: 1, ProjectID: 2, Source: "task", EventType: "created", Since: &since, Until: &until, Limit: 10, Offset: 20},
			8,
			[]string{"LIMIT $7 OFFSET $8"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query, args := buildUnifiedAuditQuery(tt.filter)
			if len(args) != tt.wantArgs {
				t.Fatalf("args = %d, want %d", len(args), tt.wantArgs)
			}
			for _, s := range tt.wantSQL {
				if !strings.Contains(query, s) {
					t.Errorf("query missing %q", s)
				}
			}
			if got := args[len(args)-2]; got != tt.filter.Limit {
				t.Errorf("limit arg = %v, want %d", got, tt.filter.Limit)
			}
			if got := args[len(args)-1]; got != tt.filter.Offset {
				t.Errorf("offset arg = %v, want %d", got, tt.filter.Offset)
			}
		})
	}
}

func TestBuildUnifiedAuditQueryParameterizesUserInput(t *testing.T) {
	evil := "x'; DROP TABLE users; --"
	query, args := buildUnifiedAuditQuery(AuditFilter{EventType: evil, Source: evil})
	if strings.Contains(query, evil) {
		t.Fatal("user input must be passed as a bind parameter, not interpolated")
	}
	if args[0] != evil || args[1] != evil {
		t.Fatalf("args = %v", args)
	}
}

func TestBuildUnifiedAuditQueryCoversAllSources(t *testing.T) {
	query, _ := buildUnifiedAuditQuery(AuditFilter{})
	for _, table := range []string{"task_events", "project_events", "task_comment_revisions", "email_audit", "admin_events"} {
		if !strings.Contains(query, "FROM "+table) {
			t.Errorf("query does not read %s", table)
		}
	}
	if !strings.Contains(query, "ORDER BY a.created_at DESC") {
		t.Error("query should order newest first")
	}
}

func TestClampAuditRetentionDays(t *testing.T) {
	for in, want := range map[int]int{-5: 0, 0: 0, 30: 30, MaxAuditRetentionDays + 1: MaxAuditRetentionDays} {
		if got := ClampAuditRetentionDays(in); got != want {
			t.Errorf("ClampAuditRetentionDays(%d) = %d, want %d", in, got, want)
		}
	}
}

func TestKnownAuditSource(t *testing.T) {
	for _, s := range []string{"task", "project", "comment", "email", "admin"} {
		if !KnownAuditSource(s) {
			t.Errorf("%q should be known", s)
		}
	}
	if KnownAuditSource("users") {
		t.Error("users should not be a known source")
	}
}
