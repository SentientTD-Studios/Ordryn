package handlers

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func openapiModuleRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
}

func TestOpenAPISpecIncludesSavedViews(t *testing.T) {
	content, err := os.ReadFile(filepath.Join(openapiModuleRoot(t), "openapi.yaml"))
	if err != nil {
		t.Fatalf("read openapi.yaml: %v", err)
	}
	documentation := string(content)
	required := []string{
		"/api/v2/saved-views",
		"/api/v2/saved-views/{id}",
		"/api/v2/tasks",
		"/api/v2/projects/{id}",
		"/api/v2/tags/{id}",
		"TagPatch",
		"/api/v2/tasks/bulk",
		"/api/v2/tasks/undo",
		"/api/v2/me/password",
		"/api/v2/me/mfa",
		"/api/v2/api-keys",
		"/api/v2/import/preview",
		"/api/v2/import/confirm",
		"/api/v2/auth/forgot-password",
		"/api/v2/auth/reset-password",
		"/api/v2/calendar/sync",
		"/api/v2/announcements/dismiss",
		"/api/v2/me/github",
		"/api/v2/me/github/pat",
		"/api/v2/projects/{id}/github",
		"/api/v2/projects/{id}/extensions",
		"/api/v2/projects/{id}/custom-fields",
		"/api/v2/projects/{id}/sprints",
		"/api/v2/projects/{id}/inbound",
		"/api/v2/me/extensions",
		"/api/v2/me/extensions/{extensionId}/deliveries/{deliveryId}/retry",
		"/api/v2/projects/{id}/extensions/{extensionId}/deliveries/{deliveryId}/retry",
		"/api/v2/admin/extensions/{id}/deliveries/{deliveryId}/retry",
		"X-Ordryn-Timestamp",
		"/api/v2/tasks/{id}/github-issue",
		"/api/v2/webhooks/github",
		"/api/v2/webhooks/inbound",
		"/api/v2/ext/callback",
		"/api/v2/extensions/{id}/icon",
		"/api/v2/projects/{id}/extensions/{extensionId}/ui",
		"version: 2.0.0",
	}
	for _, value := range required {
		if !strings.Contains(documentation, value) {
			t.Errorf("openapi.yaml is missing %q", value)
		}
	}
}
