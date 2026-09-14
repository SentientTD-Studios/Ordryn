package handlers

import (
	"testing"
)

func TestFormatProjectEventLabel(t *testing.T) {
	tests := []struct {
		eventType string
		metadata  map[string]interface{}
		want      string
	}{
		{eventType: "invited", want: "Invited member"},
		{eventType: "accepted", want: "Invite accepted"},
		{eventType: "role_changed", want: "Role changed"},
		{eventType: "removed", want: "Member removed"},
		{eventType: "left", want: "Member left"},
		{eventType: "link_created", want: "Share link created"},
		{eventType: "link_revoked", want: "Share link revoked"},
		{eventType: "invite_revoked", want: "Invite revoked"},
		{eventType: "status_added", metadata: map[string]interface{}{"name": "Review"}, want: "Status added · Review"},
		{eventType: "status_added", want: "Status added"},
		{eventType: "status_updated", metadata: map[string]interface{}{"name": "Review"}, want: "Status updated · Review"},
		{eventType: "status_updated", want: "Status updated"},
		{eventType: "status_deleted", metadata: map[string]interface{}{"name": "Review"}, want: "Status deleted · Review"},
		{eventType: "status_deleted", want: "Status deleted"},
		{eventType: "sprint_added", metadata: map[string]interface{}{"name": "Sprint 1"}, want: "Sprint added · Sprint 1"},
		{eventType: "sprint_added", metadata: map[string]interface{}{"name": "Sprint 2", "auto": true}, want: "Sprint added automatically · Sprint 2"},
		{eventType: "sprint_added", want: "Sprint added"},
		{eventType: "sprint_updated", metadata: map[string]interface{}{"name": "Sprint 1"}, want: "Sprint updated · Sprint 1"},
		{eventType: "sprint_updated", want: "Sprint updated"},
		{eventType: "sprint_deleted", metadata: map[string]interface{}{"name": "Sprint 1"}, want: "Sprint deleted · Sprint 1"},
		{eventType: "sprint_deleted", want: "Sprint deleted"},
		{eventType: "workflow_changed", metadata: map[string]interface{}{"mode": "kanban"}, want: "Workflow set to kanban"},
		{eventType: "workflow_changed", want: "Workflow changed"},
		{eventType: "renamed", metadata: map[string]interface{}{"name": "Roadmap"}, want: "Project renamed · Roadmap"},
		{eventType: "renamed", want: "Project renamed"},
		{eventType: "description_updated", want: "Description updated"},
		{eventType: "auto_sprint_settings_updated", want: "Auto-sprint settings updated"},
		{eventType: "github_repo_linked", metadata: map[string]interface{}{"full_name": "acme/widgets"}, want: "GitHub repo linked · acme/widgets"},
		{eventType: "github_repo_unlinked", want: "GitHub repo unlinked"},
		{eventType: "archived", want: "Project archived"},
		{eventType: "restored", want: "Project restored"},
		{eventType: "unknown_event", want: "unknown_event"},
	}
	for _, tt := range tests {
		t.Run(tt.eventType+"/"+tt.want, func(t *testing.T) {
			got := formatProjectEventLabel(tt.eventType, tt.metadata)
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}
