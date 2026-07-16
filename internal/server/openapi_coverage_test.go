package server

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// requiredOpenAPIPaths is the logical /api/v1 surface that must appear in openapi.yaml.
// Keep in sync when adding routes (Phase A4+).
var requiredOpenAPIPaths = []string{
	"/api/v1/health",
	"/api/v1/auth/register",
	"/api/v1/auth/login",
	"/api/v1/auth/logout",
	"/api/v1/auth/device/code",
	"/api/v1/auth/device/token",
	"/api/v1/me",
	"/api/v1/me/password",
	"/api/v1/api-keys",
	"/api/v1/api-keys/{id}",
	"/api/v1/tasks",
	"/api/v1/tasks/{id}",
	"/api/v1/tasks/reorder",
	"/api/v1/tasks/bulk",
	"/api/v1/tasks/undo",
	"/api/v1/tasks/{id}/events",
	"/api/v1/projects",
	"/api/v1/projects/{id}",
	"/api/v1/tags",
	"/api/v1/tags/{id}",
	"/api/v1/saved-views",
	"/api/v1/saved-views/{id}",
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
	re := regexp.MustCompile(`(?m)^  (/api/v1[^:]+):`)
	matches := re.FindAllStringSubmatch(string(raw), -1)
	out := make(map[string]struct{}, len(matches))
	for _, m := range matches {
		out[m[1]] = struct{}{}
	}
	if len(out) == 0 {
		t.Fatal("no /api/v1 paths found in openapi.yaml")
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
	re := regexp.MustCompile(`handleBoth\("(/api/v1[^"]+)"`)
	matches := re.FindAllStringSubmatch(string(raw), -1)
	if len(matches) == 0 {
		t.Fatal("no /api/v1 handleBoth registrations found in server.go")
	}

	openapi := openAPIPathSet(t)
	for _, m := range matches {
		reg := strings.TrimSuffix(m[1], "/")
		if coveredByOpenAPI(reg, openapi) {
			continue
		}
		t.Errorf("server registration %q has no covering OpenAPI path", m[1])
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
