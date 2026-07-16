package handlers

import (
	"os"
	"strings"
	"testing"
)

func TestAPIV1DocsIncludeSavedViews(t *testing.T) {
	content, err := os.ReadFile("../templates/api_v1_docs.html")
	if err != nil {
		t.Fatalf("read API v1 documentation template: %v", err)
	}

	documentation := string(content)
	required := []string{
		`id="saved-views"`,
		`/api/v1/saved-views`,
		`/api/v1/saved-views/{id}`,
		`GET /api/v1/tasks`,
		`/api/v1/projects/{id}`,
		`POST</span></td><td><code>/api/v1/projects`,
		`PATCH</span></td><td><code>/api/v1/tags/{id}`,
		`/api/v1/tasks/bulk`,
		`/api/v1/tasks/undo`,
		`/api/v1/me/password`,
		`/api/v1/api-keys`,
		`name_conflict`,
		`limit_reached`,
	}
	for _, value := range required {
		if !strings.Contains(documentation, value) {
			t.Errorf("API v1 documentation is missing %q", value)
		}
	}
}
