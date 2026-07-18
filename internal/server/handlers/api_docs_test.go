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
		"/api/v1/saved-views",
		"/api/v1/saved-views/{id}",
		"/api/v1/tasks",
		"/api/v1/projects/{id}",
		"/api/v1/tags/{id}",
		"/api/v1/tasks/bulk",
		"/api/v1/tasks/undo",
		"/api/v1/me/password",
		"/api/v1/api-keys",
		"/api/v1/import/preview",
		"/api/v1/import/confirm",
		"/api/v1/auth/forgot-password",
		"/api/v1/auth/reset-password",
		"/api/v1/calendar/sync",
		"/api/v1/announcements/dismiss",
	}
	for _, value := range required {
		if !strings.Contains(documentation, value) {
			t.Errorf("openapi.yaml is missing %q", value)
		}
	}
}
