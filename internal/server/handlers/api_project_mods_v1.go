package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"GoTodo/internal/domain"
	"GoTodo/internal/hooks"
	"GoTodo/internal/mods"
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
)

type projectModJSON struct {
	ID          string                     `json:"id"`
	Name        string                     `json:"name"`
	Version     string                     `json:"version"`
	HostAPI     int                        `json:"host_api"`
	SiteEnabled bool                       `json:"site_enabled"`
	Manifest    mods.Manifest              `json:"manifest"`
	Settings    storage.ModProjectSettings `json:"settings"`
	Secrets     map[string]bool            `json:"secrets"`
}

type projectModsListJSON struct {
	Mods []projectModJSON `json:"mods"`
}

type projectModPatch struct {
	Enabled    *bool             `json:"enabled"`
	Triggers   *[]string         `json:"triggers"`
	Templates  map[string]string `json:"templates"`
	StatusOnly *bool             `json:"status_only"`
	WebhookURL *string           `json:"webhook_url"`
}

func apiV1ProjectMods(w http.ResponseWriter, r *http.Request, projectID int, rest []string) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	proj, err := domain.RequireProjectModOwner(userID, projectID)
	if err != nil {
		writeProjectModError(w, err)
		return
	}

	if len(rest) == 0 {
		if r.Method != http.MethodGet {
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
			return
		}
		projectModsList(w, projectID)
		return
	}

	modID := strings.TrimSpace(rest[0])
	if modID == "" {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid mod id.")
		return
	}
	if len(rest) == 1 {
		if r.Method != http.MethodPatch {
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
			return
		}
		projectModPatchHandler(w, r, projectID, modID)
		return
	}
	if len(rest) == 2 && rest[1] == "test" {
		if r.Method != http.MethodPost {
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
			return
		}
		projectModTest(w, r, projectID, modID, proj.Name)
		return
	}
	utils.APIJSONError(w, http.StatusNotFound, "not_found", "Not found.")
}

func projectModsList(w http.ResponseWriter, projectID int) {
	entries := mods.Snapshot()
	out := make([]projectModJSON, 0)
	for _, e := range entries {
		if !e.Loaded || !e.Manifest.HasProjectSettings() {
			continue
		}
		item, err := projectModFromEntry(e, projectID)
		if err != nil {
			utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load mod settings.")
			return
		}
		out = append(out, item)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(projectModsListJSON{Mods: out})
}

func projectModPatchHandler(w http.ResponseWriter, r *http.Request, projectID int, modID string) {
	e, ok := mods.Get(modID)
	if !ok || !e.Loaded || !e.Manifest.HasProjectSettings() {
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Mod not found.")
		return
	}
	var req projectModPatch
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON.")
		return
	}
	cur, err := storage.GetModProjectSettings(modID, projectID)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load mod settings.")
		return
	}
	if req.Enabled != nil {
		cur.Enabled = *req.Enabled
	}
	if req.Triggers != nil {
		cur.Triggers = filterDeclaredTriggers(e.Manifest, *req.Triggers)
	}
	if req.Templates != nil {
		if cur.Templates == nil {
			cur.Templates = map[string]string{}
		}
		for k, v := range req.Templates {
			if !hookNameDeclared(e.Manifest, k) {
				continue
			}
			cur.Templates[k] = v
		}
	}
	if req.StatusOnly != nil {
		cur.StatusOnly = *req.StatusOnly
	}
	if req.WebhookURL != nil && strings.TrimSpace(*req.WebhookURL) != "" {
		url := strings.TrimSpace(*req.WebhookURL)
		if e.Manifest.Delivery != nil && strings.TrimSpace(e.Manifest.Delivery.Type) == "discord.webhook" {
			if err := hooks.ValidateDiscordWebhookURL(url); err != nil {
				utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", err.Error()+".")
				return
			}
		}
		key := "webhook_url"
		if e.Manifest.Delivery != nil && strings.TrimSpace(e.Manifest.Delivery.URLFrom) != "" {
			key = strings.TrimSpace(e.Manifest.Delivery.URLFrom)
		}
		if err := storage.SetModSecret(modID, projectID, key, url); err != nil {
			utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to save webhook URL.")
			return
		}
	}
	if err := storage.UpsertModProjectSettings(modID, projectID, cur); err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to save mod settings.")
		return
	}
	item, err := projectModFromEntry(e, projectID)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load mod settings.")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(item)
}

func projectModTest(w http.ResponseWriter, r *http.Request, projectID int, modID, projectName string) {
	_ = r
	e, ok := mods.Get(modID)
	if !ok || !e.Loaded || !e.Manifest.HasProjectSettings() {
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Mod not found.")
		return
	}
	if err := hooks.DeliverTest(modID, projectID, projectName); err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "message": "Test message sent."})
}

func projectModFromEntry(e mods.Entry, projectID int) (projectModJSON, error) {
	item := projectModJSON{
		ID:       e.ID,
		Name:     e.Manifest.Name,
		Version:  e.Manifest.Version,
		HostAPI:  e.Manifest.HostAPI,
		Manifest: e.Manifest,
		Secrets:  map[string]bool{},
		Settings: storage.ModProjectSettings{Triggers: []string{}, Templates: map[string]string{}},
	}
	if item.Name == "" {
		item.Name = e.ID
	}
	site, err := storage.GetModSettings(e.ID)
	if err != nil {
		return item, err
	}
	item.SiteEnabled = site.Enabled
	s, err := storage.GetModProjectSettings(e.ID, projectID)
	if err != nil {
		return item, err
	}
	item.Settings = s
	for _, key := range e.Manifest.ProjectSecretKeys() {
		item.Secrets[key] = storage.ModSecretIsSet(e.ID, projectID, key)
	}
	return item, nil
}

func writeProjectModError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Project not found.")
	case errors.Is(err, domain.ErrForbidden):
		utils.APIJSONError(w, http.StatusForbidden, "forbidden", "Only the project owner can manage mods.")
	default:
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Request failed.")
	}
}
