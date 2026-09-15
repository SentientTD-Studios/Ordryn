package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"GoTodo/internal/server/utils"
)

func TestAPIV1AdminExtensionsMethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/extensions", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	APIV1AdminExtensionsRouter(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestAPIV1AdminExtensionsGetUnknown(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/extensions/missing", nil)
	rec := httptest.NewRecorder()
	APIV1AdminExtensionsRouter(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAPIV1AdminExtensionsTestRemoved(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/extensions/discord/test", nil)
	rec := httptest.NewRecorder()
	APIV1AdminExtensionsRouter(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAdminExtensionPatchJSONIgnoresTriggers(t *testing.T) {
	body := `{"enabled":true,"triggers":["task.updated"],"status_only":true,"webhook_url":"https://example.com"}`
	var req adminExtensionPatch
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatal(err)
	}
	if req.Enabled == nil || !*req.Enabled {
		t.Fatal("enabled")
	}
}

func TestAPIV1ProjectExtensionsUnauthorized(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/1/extensions", nil)
	rec := httptest.NewRecorder()
	APIV1ProjectsRouter(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAPIV1ProjectExtensionsListMethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/1/extensions", strings.NewReader(`{}`))
	req = utils.SetAPIUserID(req, 1)
	rec := httptest.NewRecorder()
	APIV1ProjectsRouter(rec, req)
	if rec.Code != http.StatusMethodNotAllowed && rec.Code != http.StatusNotFound && rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
