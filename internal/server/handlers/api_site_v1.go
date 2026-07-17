package handlers

import (
	"encoding/json"
	"net/http"

	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
)

type apiSiteResponse struct {
	SiteName                 string `json:"site_name"`
	EnableGlobalAnnouncement bool   `json:"enable_global_announcement"`
	GlobalAnnouncementText   string `json:"global_announcement_text"`
	AnnouncementDismissed    bool   `json:"announcement_dismissed"`
}

// APIV1Site returns public site metadata for the SPA shell.
// GET /api/v1/site
func APIV1Site(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	settings, err := storage.GetSiteSettings()
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load site settings.")
		return
	}
	dismissed := false
	if session, err := utils.GetSession(r); err == nil && session != nil {
		if v, ok := session.Values["announcement_dismissed"].(bool); ok && v {
			dismissed = true
		}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(apiSiteResponse{
		SiteName:                 settings.SiteName,
		EnableGlobalAnnouncement: settings.EnableGlobalAnnouncement,
		GlobalAnnouncementText:   settings.GlobalAnnouncementText,
		AnnouncementDismissed:    dismissed,
	})
}
