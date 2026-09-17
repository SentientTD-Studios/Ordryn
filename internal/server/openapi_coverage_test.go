package server

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// requiredOpenAPIPaths is the logical /api/v2 surface that must appear in openapi.yaml.
// Keep in sync when adding routes (Phase A4+).
var requiredOpenAPIPaths = []string{
	"/api/v2/health",
	"/api/v2/site",
	"/api/v2/auth/register",
	"/api/v2/auth/login",
	"/api/v2/auth/mfa/verify",
	"/api/v2/auth/logout",
	"/api/v2/auth/username-available",
	"/api/v2/auth/device/code",
	"/api/v2/auth/device/token",
	"/api/v2/auth/device/status",
	"/api/v2/auth/device/approve",
	"/api/v2/auth/device/deny",
	"/api/v2/me",
	"/api/v2/me/password",
	"/api/v2/me/mfa",
	"/api/v2/me/mfa/setup",
	"/api/v2/me/mfa/enable",
	"/api/v2/me/mfa/disable",
	"/api/v2/me/mfa/recovery-codes",
	"/api/v2/me/username",
	"/api/v2/me/avatar",
	"/api/v2/api-keys",
	"/api/v2/api-keys/{id}",
	"/api/v2/tasks",
	"/api/v2/tasks/{id}",
	"/api/v2/tasks/reorder",
	"/api/v2/tasks/bulk",
	"/api/v2/tasks/undo",
	"/api/v2/tasks/{id}/events",
	"/api/v2/tasks/{id}/comments",
	"/api/v2/tasks/{id}/comments/{commentId}",
	"/api/v2/tasks/{id}/comments/{commentId}/revisions",
	"/api/v2/tasks/{id}/comments/{commentId}/restore",
	"/api/v2/users/search",
	"/api/v2/projects",
	"/api/v2/projects/reorder",
	"/api/v2/projects/{id}",
	"/api/v2/projects/{id}/archive",
	"/api/v2/projects/{id}/restore",
	"/api/v2/projects/{id}/members",
	"/api/v2/projects/{id}/members/{userId}",
	"/api/v2/projects/{id}/invites",
	"/api/v2/projects/{id}/invites/{inviteId}",
	"/api/v2/projects/{id}/events",
	"/api/v2/projects/{id}/sprints",
	"/api/v2/projects/{id}/sprints/{sprintId}",
	"/api/v2/projects/{id}/sprints/backlog",
	"/api/v2/project-invites",
	"/api/v2/project-invites/{id}/accept",
	"/api/v2/project-invites/{id}/decline",
	"/api/v2/share-links",
	"/api/v2/share-links/{id}",
	"/api/v2/share-links/view/{token}",
	"/api/v2/tags",
	"/api/v2/tags/{id}",
	"/api/v2/saved-views",
	"/api/v2/saved-views/{id}",
	"/api/v2/dashboard",
	"/api/v2/events",
	"/api/v2/calendar",
	"/api/v2/calendar/month",
	"/api/v2/calendar/regenerate",
	"/api/v2/calendar/sync",
	"/api/v2/export",
	"/api/v2/import/preview",
	"/api/v2/import/confirm",
	"/api/v2/import/cancel",
	"/api/v2/images",
	"/api/v2/auth/forgot-password",
	"/api/v2/auth/reset-password",
	"/api/v2/join-requests",
	"/api/v2/announcements/dismiss",
	"/api/v2/invites",
	"/api/v2/invites/{id}",
	"/api/v2/admin/settings",
	"/api/v2/admin/image-hosting/test",
	"/api/v2/admin/users",
	"/api/v2/admin/users/{id}/ban",
	"/api/v2/admin/users/{id}/unban",
	"/api/v2/admin/users/{id}/username",
	"/api/v2/admin/join-requests",
	"/api/v2/admin/join-requests/{id}/approve",
	"/api/v2/admin/join-requests/{id}/deny",
	"/api/v2/admin/invites",
	"/api/v2/admin/invites/{id}",
	"/api/v2/admin/email-audit",
	"/api/v2/admin/comment-audit",
	"/api/v2/admin/comment-audit/{id}/restore",
	"/api/v2/admin/extensions",
	"/api/v2/admin/extensions/{id}",
	"/api/v2/projects/{id}/extensions",
	"/api/v2/projects/{id}/extensions/{extensionId}",
	"/api/v2/projects/{id}/extensions/{extensionId}/test",
	"/api/v2/projects/{id}/extensions/{extensionId}/me",
	"/api/v2/projects/{id}/extensions/{extensionId}/me/test",
	"/api/v2/projects/{id}/inbound",
	"/api/v2/projects/{id}/custom-fields",
	"/api/v2/me/extensions",
	"/api/v2/me/extensions/{extensionId}",
	"/api/v2/me/extensions/{extensionId}/test",
	"/api/v2/webhooks/inbound",
	"/api/v2/ext/callback",
	"/api/v2/extensions/{id}/icon",
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// internal/server/openapi_coverage_test.go → repo root
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func openAPIPathSet(t *testing.T) map[string]struct{} {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(moduleRoot(t), "openapi.yaml"))
	if err != nil {
		t.Fatalf("read openapi.yaml: %v", err)
	}
	re := regexp.MustCompile(`(?m)^  (/api/v2[^:]+):`)
	matches := re.FindAllStringSubmatch(string(raw), -1)
	out := make(map[string]struct{}, len(matches))
	for _, m := range matches {
		out[m[1]] = struct{}{}
	}
	if len(out) == 0 {
		t.Fatal("no /api/v2 paths found in openapi.yaml")
	}
	return out
}

func TestOpenAPIContainsRequiredAPIV1Paths(t *testing.T) {
	paths := openAPIPathSet(t)
	for _, p := range requiredOpenAPIPaths {
		if _, ok := paths[p]; !ok {
			t.Errorf("openapi.yaml missing path %s", p)
		}
	}
}

func TestServerAPIV1RegistrationsCoveredByOpenAPI(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(moduleRoot(t), "internal", "server", "server.go"))
	if err != nil {
		t.Fatalf("read server.go: %v", err)
	}
	re := regexp.MustCompile(`handleAPI\("(/[^"]+)"`)
	matches := re.FindAllStringSubmatch(string(raw), -1)
	if len(matches) == 0 {
		t.Fatal("no handleAPI registrations found in server.go")
	}

	openapi := openAPIPathSet(t)
	for _, m := range matches {
		reg := "/api/v2" + strings.TrimSuffix(m[1], "/")
		if coveredByOpenAPI(reg, openapi) {
			continue
		}
		t.Errorf("server registration %q has no covering OpenAPI path", "/api/v2"+m[1])
	}
}

func coveredByOpenAPI(registration string, openapi map[string]struct{}) bool {
	if _, ok := openapi[registration]; ok {
		return true
	}
	// Collection routers also serve /{id} and nested paths.
	prefixes := []string{
		registration + "/{id}",
		registration + "/{id}/events",
		registration + "/reorder",
		registration + "/bulk",
		registration + "/undo",
		registration + "/password",
	}
	for _, p := range prefixes {
		if _, ok := openapi[p]; ok {
			return true
		}
	}
	// Trailing-slash duplicates of collections.
	for p := range openapi {
		if strings.HasPrefix(p, registration+"/") || p == registration {
			return true
		}
	}
	return false
}
