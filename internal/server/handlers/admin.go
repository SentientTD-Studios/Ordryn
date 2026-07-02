package handlers

import (
	"GoTodo/internal/config"
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
	"GoTodo/internal/version"
	"encoding/json"
	"net/http"
	"os"
	"strings"
)

// AdminPageHandler shows the admin settings page
func AdminPageHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}
	_, _, permissions, _, loggedIn, _ := utils.GetSessionUserWithTimezone(r)
	if !loggedIn {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	// Prefer DB-backed settings for mutable fields; site version is always baked into the binary
	siteName := config.Cfg.SiteName
	siteVersion := version.Version
	defaultTz := config.Cfg.DefaultTimezone
	showChangelog := config.Cfg.ShowChangelog
	enableRegistration := true
	inviteOnly := true
	metaDescription := ""
	enableGlobalAnnouncement := false
	globalAnnouncementText := ""
	validationError := ""

	if s, err := storage.GetSiteSettings(); err == nil && s != nil {
		if s.SiteName != "" {
			siteName = s.SiteName
		}
		if s.DefaultTimezone != "" {
			defaultTz = s.DefaultTimezone
		}
		showChangelog = s.ShowChangelog
		enableRegistration = s.EnableRegistration
		inviteOnly = s.InviteOnly
		metaDescription = s.MetaDescription
		enableGlobalAnnouncement = s.EnableGlobalAnnouncement
		globalAnnouncementText = s.GlobalAnnouncementText
	}

	// Check if there's a validation error and form data in session
	session, err := utils.GetSession(r)
	if err == nil && session != nil {
		if formData := session.Values["admin_form_data"]; formData != nil {
			if formMap, ok := formData.(map[string]string); ok {
				if val, exists := formMap["global_announcement_text"]; exists {
					globalAnnouncementText = val
				}
				if val, exists := formMap["validation_error"]; exists {
					validationError = val
				}
			}
			// Clear the form data from session
			delete(session.Values, "admin_form_data")
			_ = session.Save(r, w)
		}
	}

	users, _ := storage.ListUsers()

	context := map[string]interface{}{
		"LoggedIn":                 loggedIn,
		"Permissions":              permissions,
		"Users":                    users,
		"SiteName":                 siteName,
		"SiteVersion":              siteVersion,
		"DefaultTimezone":          defaultTz,
		"ShowChangelog":            showChangelog,
		"EnableRegistration":       enableRegistration,
		"InviteOnly":               inviteOnly,
		"MetaDescription":          metaDescription,
		"EnableGlobalAnnouncement": enableGlobalAnnouncement,
		"GlobalAnnouncementText":   globalAnnouncementText,
		"ValidationError":          validationError,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := utils.RenderTemplate(w, r, "admin.html", context); err != nil {
		http.Error(w, "Error rendering template: "+err.Error(), http.StatusInternalServerError)
	}
}

// APIUpdateSiteSettings updates site-wide settings (only for admins)
func APIUpdateSiteSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.SetFlash(w, r, "Invalid request method.")
		http.Redirect(w, r, utils.GetBasePath()+"/admin", http.StatusSeeOther)
		return
	}

	// We expect the route to be protected by RequirePermission("admin", ...)

	siteName := strings.TrimSpace(r.FormValue("site_name"))
	defaultTz := strings.TrimSpace(r.FormValue("default_timezone"))
	metaDescription := strings.TrimSpace(r.FormValue("meta_description"))
	globalAnnouncementText := strings.TrimSpace(r.FormValue("global_announcement_text"))
	showChangelogStr := r.FormValue("show_changelog")
	enableRegistrationStr := r.FormValue("enable_registration")
	inviteOnlyStr := r.FormValue("invite_only")
	enableGlobalAnnouncementStr := r.FormValue("enable_global_announcement")

	if siteName == "" {
		utils.SetFlash(w, r, "Site name is required.")
		http.Redirect(w, r, utils.GetBasePath()+"/admin", http.StatusSeeOther)
		return
	}
	if defaultTz == "" {
		utils.SetFlash(w, r, "Default timezone is required.")
		http.Redirect(w, r, utils.GetBasePath()+"/admin", http.StatusSeeOther)
		return
	}

	// Validate global announcement text length
	if len(globalAnnouncementText) > 500 {
		// Store form data in session to preserve user input
		session, err := utils.GetSession(r)
		if err == nil && session != nil {
			session.Values["admin_form_data"] = map[string]string{
				"global_announcement_text": globalAnnouncementText,
				"validation_error":         "Global announcement text must be 500 characters or less.",
			}
			_ = session.Save(r, w)
		}
		http.Redirect(w, r, utils.GetBasePath()+"/admin", http.StatusSeeOther)
		return
	}

	// Update in-memory config
	config.Cfg.SiteName = siteName
	config.Cfg.DefaultTimezone = defaultTz
	if showChangelogStr == "true" || showChangelogStr == "on" {
		config.Cfg.ShowChangelog = true
	} else {
		config.Cfg.ShowChangelog = false
	}

	enableRegistration := enableRegistrationStr == "true" || enableRegistrationStr == "on"
	inviteOnly := inviteOnlyStr == "true" || inviteOnlyStr == "on"
	enableGlobalAnnouncement := enableGlobalAnnouncementStr == "true" || enableGlobalAnnouncementStr == "on"

	// Persist to DB when possible; fall back to config file if DB unavailable
	ss := storage.SiteSettings{
		SiteName:        siteName,
		DefaultTimezone: defaultTz,
		ShowChangelog:   config.Cfg.ShowChangelog,
		// Do NOT persist site version from the app; site version is baked into the binary only.
		SiteVersion:              "",
		EnableRegistration:       enableRegistration,
		InviteOnly:               inviteOnly,
		MetaDescription:          metaDescription,
		EnableGlobalAnnouncement: enableGlobalAnnouncement,
		GlobalAnnouncementText:   globalAnnouncementText,
	}
	if err := storage.UpsertSiteSettings(ss); err != nil {
		// fallback: persist to config file
		out, err := json.MarshalIndent(config.Cfg, "", "  ")
		if err != nil {
			utils.SetFlash(w, r, "Failed to save settings.")
			http.Redirect(w, r, utils.GetBasePath()+"/admin", http.StatusSeeOther)
			return
		}
		if err := os.WriteFile("config/config.json", out, 0644); err != nil {
			utils.SetFlash(w, r, "Failed to write config file.")
			http.Redirect(w, r, utils.GetBasePath()+"/admin", http.StatusSeeOther)
			return
		}
	}

	// Redirect back to admin page
	utils.SetFlash(w, r, "Site Settings Saved")
	http.Redirect(w, r, utils.GetBasePath()+"/admin", http.StatusSeeOther)
}

// Note: bumping site version is intentionally disabled from within the site.
