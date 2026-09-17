package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"GoTodo/internal/extensions"
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
)

func TestAPIV1AdminExtensionsMethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v2/admin/extensions", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	APIV1AdminExtensionsRouter(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestAPIV1AdminExtensionsGetUnknown(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v2/admin/extensions/missing", nil)
	rec := httptest.NewRecorder()
	APIV1AdminExtensionsRouter(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAPIV1AdminExtensionsTestRemoved(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v2/admin/extensions/discord/test", nil)
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
	req := httptest.NewRequest(http.MethodGet, "/api/v2/projects/1/extensions", nil)
	rec := httptest.NewRecorder()
	APIV1ProjectsRouter(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAPIV1ProjectExtensionsListMethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v2/projects/1/extensions", strings.NewReader(`{}`))
	req = utils.SetAPIUserID(req, 1)
	rec := httptest.NewRecorder()
	APIV1ProjectsRouter(rec, req)
	if rec.Code != http.StatusMethodNotAllowed && rec.Code != http.StatusNotFound && rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAPIV1InboundWebhookMethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v2/webhooks/inbound", nil)
	rec := httptest.NewRecorder()
	APIV1InboundWebhook(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestAPIV1InboundWebhookInvalidJSON(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v2/webhooks/inbound", strings.NewReader(`not-json`))
	rec := httptest.NewRecorder()
	APIV1InboundWebhook(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAPIV1InboundWebhookDisabled(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v2/webhooks/inbound", strings.NewReader(`{"action":"create","title":"Ship","project_id":1}`))
	rec := httptest.NewRecorder()
	APIV1InboundWebhook(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAPIV1InboundWebhookRequiresProjectID(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v2/webhooks/inbound", strings.NewReader(`{"action":"create","title":"Ship"}`))
	rec := httptest.NewRecorder()
	APIV1InboundWebhook(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAPIV1MeExtensionsUnauthorized(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v2/me/extensions", nil)
	rec := httptest.NewRecorder()
	APIV1MeExtensions(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestProjectExtensionVisibleRequiresLoadedSurface(t *testing.T) {
	ok, err := projectExtensionVisible(extensions.Entry{Loaded: false, Manifest: extensions.Manifest{ID: "x"}})
	if err != nil || ok {
		t.Fatalf("unloaded visible=%v err=%v", ok, err)
	}
	ok, err = projectExtensionVisible(extensions.Entry{
		Loaded: true,
		Manifest: extensions.Manifest{
			ID:      "join-requests",
			Settings: []extensions.Setting{{Key: "webhook_url", Type: "secret", Label: "URL"}},
		},
	})
	if err != nil || ok {
		t.Fatalf("site-only visible=%v err=%v", ok, err)
	}
}

func TestPersonalInboxVisibleRequiresMemberScope(t *testing.T) {
	e := extensions.Entry{
		Loaded: true,
		Manifest: extensions.Manifest{
			ID:       "discord",
			Delivery: &extensions.Delivery{Type: extensions.DeliveryDiscordWebhook, URLFrom: "webhook_url"},
			Settings: []extensions.Setting{
				{Key: "webhook_url", Type: "secret", Label: "URL", Scope: extensions.ScopeProject},
			},
		},
	}
	if personalInboxVisible(e) {
		t.Fatal("project-only discord should not appear under profile integrations")
	}
	e.Manifest.Settings = append(e.Manifest.Settings, extensions.Setting{
		Key: "claimed_is_me", Type: "bool", Label: "Mine", Scope: extensions.ScopeMember,
	})
	if !personalInboxVisible(e) {
		t.Fatal("member-scoped settings should appear under profile integrations")
	}
}

func TestApplyHookFiltersIgnoresUndeclaredSettings(t *testing.T) {
	m := extensions.Manifest{Settings: []extensions.Setting{
		{Key: "skip_self", Type: "bool", Label: "Skip", Scope: extensions.ScopeProject},
	}}
	cur := storage.ExtensionProjectSettings{}
	skip := true
	pri := 3
	claimed := true
	applyHookFiltersToProject(m, &cur, projectExtensionPatch{SkipSelf: &skip, MinPriority: &pri, ClaimedOnly: &claimed})
	if !cur.SkipSelf {
		t.Fatal("declared skip_self should apply")
	}
	if cur.MinPriority != 0 || cur.ClaimedOnly {
		t.Fatal("undeclared filters should be ignored")
	}
}
