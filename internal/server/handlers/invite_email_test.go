package handlers

import (
	"net/http/httptest"
	"testing"

	"GoTodo/internal/server/utils"
)

func TestSiteInviteRegisterURL(t *testing.T) {
	origBase := utils.GetBasePath()
	t.Cleanup(func() {
		utils.BasePath = origBase
	})
	utils.BasePath = ""

	req := httptest.NewRequest("POST", "/api/v2/invites", nil)
	req.Host = "gotodo.example"
	req.Header.Set("X-Forwarded-Proto", "https")

	got := siteInviteRegisterURL(req, "user@example.com", "abc123")
	want := "https://gotodo.example/register?email=user%40example.com&invite=abc123"
	if got != want {
		t.Fatalf("siteInviteRegisterURL() = %q, want %q", got, want)
	}
}
