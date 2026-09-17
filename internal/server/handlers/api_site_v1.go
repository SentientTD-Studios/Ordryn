package handlers

import (
	"encoding/json"
	"net/http"

	"GoTodo/internal/domain"
	"GoTodo/internal/imagehost"
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
)

type apiSiteResponse struct {
	SiteName                 string `json:"site_name"`
	ShowChangelog            bool   `json:"show_changelog"`
	EnableRegistration       bool   `json:"enable_registration"`
	InviteOnly               bool   `json:"invite_only"`
	EnableJoinRequests       bool   `json:"enable_join_requests"`
	MetaDescription          string `json:"meta_description"`
	EnableGlobalAnnouncement bool   `json:"enable_global_announcement"`
	GlobalAnnouncementText   string `json:"global_announcement_text"`
	AnnouncementDismissed    bool   `json:"announcement_dismissed"`
	GitHubOAuthConfigured    bool   `json:"github_oauth_configured"`
	ImageHostingEnabled      bool   `json:"image_hosting_enabled"`
	ImageMaxBytes            int64  `json:"image_max_bytes"`
	AllowUserInvites         bool   `json:"allow_user_invites"`
	UserInviteLimit          int    `json:"user_invite_limit"`
	InviteExpirationDays     int    `json:"invite_expiration_days"`
	EnableInboundWebhooks    bool   `json:"enable_inbound_webhooks"`
}

// APIV1Site returns public site metadata for the SPA shell.
// GET /api/v2/site
func APIV1Site(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	settings, err := storage.GetSiteSettings()
	if err != nil || settings == nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load site settings.")
		return
	}
	dismissed := false
	if session, err := utils.GetSession(r); err == nil && session != nil {
		if v, ok := session.Values["announcement_dismissed"].(bool); ok && v {
			dismissed = true
		}
	}
	imageEnabled := false
	imageMax := imagehost.DefaultMaxBytes
	if cfg, err := settings.ImageHostingConfig(); err == nil {
		imageEnabled = cfg.Enabled()
		imageMax = imagehost.ClampMaxBytes(cfg.MaxBytes)
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(apiSiteResponse{
		SiteName:                 settings.SiteName,
		ShowChangelog:            settings.ShowChangelog,
		EnableRegistration:       settings.EnableRegistration,
		InviteOnly:               settings.InviteOnly,
		EnableJoinRequests:       settings.EnableJoinRequests,
		MetaDescription:          settings.MetaDescription,
		EnableGlobalAnnouncement: settings.EnableGlobalAnnouncement,
		GlobalAnnouncementText:   settings.GlobalAnnouncementText,
		AnnouncementDismissed:    dismissed,
		GitHubOAuthConfigured:    domain.GitHubOAuthConfigured(),
		ImageHostingEnabled:      imageEnabled,
		ImageMaxBytes:            imageMax,
		AllowUserInvites:         settings.AllowUserInvites,
		UserInviteLimit:          settings.UserInviteLimit,
		InviteExpirationDays:     settings.InviteExpirationDays,
		EnableInboundWebhooks:    settings.EnableInboundWebhooks,
	})
}
