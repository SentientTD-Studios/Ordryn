package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAPIV1SiteMethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/site", nil)
	rec := httptest.NewRecorder()
	APIV1Site(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestAPISiteResponsePublicFields(t *testing.T) {
	raw, err := json.Marshal(apiSiteResponse{
		SiteName:            "Demo",
		EnableRegistration:  true,
		InviteOnly:          false,
		EnableJoinRequests:  true,
		MetaDescription:     "Hello",
		ImageHostingEnabled: true,
		ImageMaxBytes:       5242880,
	})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		"site_name",
		"enable_registration",
		"invite_only",
		"enable_join_requests",
		"meta_description",
		"show_changelog",
		"github_oauth_configured",
		"image_hosting_enabled",
		"image_max_bytes",
		"allow_user_invites",
		"user_invite_limit",
		"invite_expiration_days",
	} {
		if _, ok := m[key]; !ok {
			t.Fatalf("missing key %q in %s", key, string(raw))
		}
	}
}
