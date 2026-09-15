package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"GoTodo/internal/domain"
	"GoTodo/internal/extensions"
	"GoTodo/internal/hooks"
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
)

type projectExtensionJSON struct {
	ID          string                           `json:"id"`
	Name        string                           `json:"name"`
	Version     string                           `json:"version"`
	HostAPI     int                              `json:"host_api"`
	SiteEnabled bool                             `json:"site_enabled"`
	Manifest    extensions.Manifest              `json:"manifest"`
	Settings    storage.ExtensionProjectSettings `json:"settings"`
	Secrets     map[string]bool                  `json:"secrets"`
}

type projectExtensionsListJSON struct {
	Extensions []projectExtensionJSON `json:"extensions"`
}

type projectExtensionPatch struct {
	Enabled    *bool             `json:"enabled"`
	Triggers   *[]string         `json:"triggers"`
	Templates  map[string]string `json:"templates"`
	StatusOnly *bool             `json:"status_only"`
	WebhookURL *string           `json:"webhook_url"`
}

func apiV1ProjectExtensions(w http.ResponseWriter, r *http.Request, projectID int, rest []string) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	proj, err := domain.RequireProjectExtensionOwner(userID, projectID)
	if err != nil {
		writeProjectExtensionError(w, err)
		return
	}

	if len(rest) == 0 {
		if r.Method != http.MethodGet {
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
			return
		}
		projectExtensionsList(w, projectID)
		return
	}

	extensionID := strings.TrimSpace(rest[0])
	if extensionID == "" {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid extension id.")
		return
	}
	if len(rest) == 1 {
		if r.Method != http.MethodPatch {
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
			return
		}
		projectExtensionPatchHandler(w, r, projectID, extensionID)
		return
	}
	if len(rest) == 2 && rest[1] == "test" {
		if r.Method != http.MethodPost {
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
			return
		}
		projectExtensionTest(w, r, projectID, extensionID, proj.Name)
		return
	}
	utils.APIJSONError(w, http.StatusNotFound, "not_found", "Not found.")
}

func projectExtensionsList(w http.ResponseWriter, projectID int) {
	entries := extensions.Snapshot()
	out := make([]projectExtensionJSON, 0)
	for _, e := range entries {
		if !e.Loaded || !e.Manifest.HasProjectSurface() {
			continue
		}
		item, err := projectExtensionFromEntry(e, projectID)
		if err != nil {
			utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load extension settings.")
			return
		}
		out = append(out, item)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(projectExtensionsListJSON{Extensions: out})
}

func projectExtensionPatchHandler(w http.ResponseWriter, r *http.Request, projectID int, extensionID string) {
	e, ok := extensions.Get(extensionID)
	if !ok || !e.Loaded || !e.Manifest.HasProjectSurface() {
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Extension not found.")
		return
	}
	var req projectExtensionPatch
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON.")
		return
	}
	cur, err := storage.GetExtensionProjectSettings(extensionID, projectID)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load extension settings.")
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
		if e.Manifest.Delivery != nil {
			if err := hooks.ValidateDeliveryURL(e.Manifest.Delivery, url); err != nil {
				utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", err.Error()+".")
				return
			}
		}
		key := "webhook_url"
		if e.Manifest.Delivery != nil && strings.TrimSpace(e.Manifest.Delivery.URLFrom) != "" {
			key = strings.TrimSpace(e.Manifest.Delivery.URLFrom)
		}
		if err := storage.SetExtensionSecret(extensionID, projectID, key, url); err != nil {
			utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to save webhook URL.")
			return
		}
	}
	if err := storage.UpsertExtensionProjectSettings(extensionID, projectID, cur); err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to save extension settings.")
		return
	}
	item, err := projectExtensionFromEntry(e, projectID)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load extension settings.")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(item)
}

func projectExtensionTest(w http.ResponseWriter, r *http.Request, projectID int, extensionID, projectName string) {
	_ = r
	e, ok := extensions.Get(extensionID)
	if !ok || !e.Loaded || !e.Manifest.HasProjectSettings() {
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Extension not found.")
		return
	}
	if err := hooks.DeliverTest(extensionID, projectID, projectName); err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "message": "Test message sent."})
}

func projectExtensionFromEntry(e extensions.Entry, projectID int) (projectExtensionJSON, error) {
	item := projectExtensionJSON{
		ID:       e.ID,
		Name:     e.Manifest.Name,
		Version:  e.Manifest.Version,
		HostAPI:  e.Manifest.HostAPI,
		Manifest: e.Manifest,
		Secrets:  map[string]bool{},
		Settings: storage.ExtensionProjectSettings{Triggers: []string{}, Templates: map[string]string{}},
	}
	if item.Name == "" {
		item.Name = e.ID
	}
	site, err := storage.GetExtensionSettings(e.ID)
	if err != nil {
		return item, err
	}
	item.SiteEnabled = site.Enabled
	s, err := storage.GetExtensionProjectSettings(e.ID, projectID)
	if err != nil {
		return item, err
	}
	item.Settings = s
	for _, key := range e.Manifest.ProjectSecretKeys() {
		item.Secrets[key] = storage.ExtensionSecretIsSet(e.ID, projectID, key)
	}
	return item, nil
}

func writeProjectExtensionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Project not found.")
	case errors.Is(err, domain.ErrForbidden):
		utils.APIJSONError(w, http.StatusForbidden, "forbidden", "Only the project owner can manage extensions.")
	default:
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Request failed.")
	}
}
