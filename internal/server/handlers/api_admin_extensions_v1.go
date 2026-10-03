package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"GoTodo/internal/domain"
	"GoTodo/internal/extensions"
	"GoTodo/internal/hooks"
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
)

type adminExtensionJSON struct {
	ID         string                      `json:"id"`
	Name       string                      `json:"name"`
	Version    string                      `json:"version"`
	HostAPI    int                         `json:"host_api"`
	Status     string                      `json:"status"`
	Error      string                      `json:"error,omitempty"`
	Manifest   extensions.Manifest         `json:"manifest"`
	Settings   storage.ExtensionSettings   `json:"settings"`
	Secrets    map[string]bool             `json:"secrets"`
	Deliveries []storage.ExtensionDelivery `json:"deliveries,omitempty"`
}

type adminExtensionsListJSON struct {
	Extensions []adminExtensionJSON `json:"extensions"`
}

type adminExtensionsReloadJSON struct {
	OK         bool                 `json:"ok"`
	Message    string               `json:"message"`
	Extensions []adminExtensionJSON `json:"extensions"`
}

type adminExtensionPatch struct {
	Enabled    *bool              `json:"enabled"`
	WebhookURL *string            `json:"webhook_url"`
	Triggers   *[]string          `json:"triggers"`
	Templates  *map[string]string `json:"templates"`
}

// APIV1AdminExtensionsRouter handles /api/v2/admin/extensions and /{id}.
func APIV1AdminExtensionsRouter(w http.ResponseWriter, r *http.Request) {
	sub := utils.ParseAPIV1Subpath(r, "admin/extensions")
	if sub == "" {
		if r.Method != http.MethodGet {
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
			return
		}
		adminExtensionsList(w, r)
		return
	}
	parts := strings.Split(sub, "/")
	id := parts[0]
	if id == "" {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid extension id.")
		return
	}
	if len(parts) == 1 {
		if id == "reload" {
			if r.Method != http.MethodPost {
				utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
				return
			}
			adminExtensionsReload(w, r)
			return
		}
		switch r.Method {
		case http.MethodGet:
			adminExtensionGet(w, r, id)
		case http.MethodPatch:
			adminExtensionPatchHandler(w, r, id)
		default:
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		}
		return
	}
	if len(parts) == 2 && parts[1] == "test" && r.Method == http.MethodPost {
		projectExtensionTest(w, r, 0, 0, id, "Site")
		return
	}
	if len(parts) == 4 && parts[1] == "deliveries" && parts[3] == "retry" && r.Method == http.MethodPost {
		projectExtensionRetry(w, r, 0, 0, id, parts[2])
		return
	}
	utils.APIJSONError(w, http.StatusNotFound, "not_found", "Not found.")
}

func adminExtensionsList(w http.ResponseWriter, r *http.Request) {
	_ = r
	entries := extensions.Snapshot()
	out := make([]adminExtensionJSON, 0, len(entries))
	for _, e := range entries {
		item, err := adminExtensionFromEntry(e)
		if err != nil {
			utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load extension settings.")
			return
		}
		out = append(out, item)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(adminExtensionsListJSON{Extensions: out})
}

func adminExtensionsReload(w http.ResponseWriter, r *http.Request) {
	userID, _ := apiUserFromRequest(r)
	entries, err := domain.ReloadExtensions(userID)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to sync custom fields: "+err.Error())
		return
	}
	out := make([]adminExtensionJSON, 0, len(entries))
	loadedCount := 0
	failedCount := 0
	for _, e := range entries {
		if e.Loaded {
			loadedCount++
		} else if e.Error != "" {
			failedCount++
		}
		item, err := adminExtensionFromEntry(e)
		if err != nil {
			utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load extension settings.")
			return
		}
		out = append(out, item)
	}
	msg := fmt.Sprintf("Reloaded %d extensions (%d loaded, %d failed).", len(entries), loadedCount, failedCount)
	if failedCount == 0 {
		msg = fmt.Sprintf("Successfully reloaded %d extensions.", loadedCount)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(adminExtensionsReloadJSON{
		OK:         true,
		Message:    msg,
		Extensions: out,
	})
}

func adminExtensionGet(w http.ResponseWriter, r *http.Request, id string) {
	_ = r
	e, ok := extensions.Get(id)
	if !ok {
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Extension not found.")
		return
	}
	item, err := adminExtensionFromEntry(e)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load extension settings.")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(item)
}

func adminExtensionPatchHandler(w http.ResponseWriter, r *http.Request, id string) {
	e, ok := extensions.Get(id)
	if !ok {
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Extension not found.")
		return
	}
	if !e.Loaded {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Cannot configure a failed extension.")
		return
	}
	var req adminExtensionPatch
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON.")
		return
	}
	cur, err := storage.GetExtensionSettings(id)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load extension settings.")
		return
	}
	auditMeta := map[string]interface{}{}
	if req.Enabled != nil {
		if cur.Enabled != *req.Enabled {
			auditMeta["enabled"] = map[string]interface{}{"from": cur.Enabled, "to": *req.Enabled}
		}
		cur.Enabled = *req.Enabled
	}
	if req.Triggers != nil {
		next := filterDeclaredTriggers(e.Manifest, *req.Triggers)
		if strings.Join(cur.Triggers, ",") != strings.Join(next, ",") {
			auditMeta["triggers"] = map[string]interface{}{"from": cur.Triggers, "to": next}
		}
		cur.Triggers = next
	}
	if req.Templates != nil {
		auditMeta["templates_updated"] = true
	}
	if req.WebhookURL != nil && strings.TrimSpace(*req.WebhookURL) != "" {
		auditMeta["webhook_url_updated"] = true
	}
	if req.Templates != nil {
		cur.Templates = *req.Templates
	}
	if req.WebhookURL != nil && strings.TrimSpace(*req.WebhookURL) != "" {
		url := strings.TrimSpace(*req.WebhookURL)
		if e.Manifest.Delivery != nil {
			if err := hooks.ValidateDeliveryURL(e.Manifest.Delivery, url); err != nil {
				utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", err.Error()+".")
				return
			}
			key := e.Manifest.Delivery.DestinationKey()
			if key == "" {
				key = "webhook_url"
			}
			if err := storage.SetExtensionSecret(id, 0, key, url); err != nil {
				utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to save webhook URL.")
				return
			}
		}
	}
	if err := storage.UpsertExtensionSettings(id, cur); err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to save extension settings.")
		return
	}
	if len(auditMeta) > 0 {
		logAdminEvent(r, "extension_updated", "extension", 0, id, auditMeta)
	}
	item, err := adminExtensionFromEntry(e)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load extension settings.")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(item)
}

func adminExtensionFromEntry(e extensions.Entry) (adminExtensionJSON, error) {
	item := adminExtensionJSON{
		ID:       e.ID,
		Name:     e.Manifest.Name,
		Version:  e.Manifest.Version,
		HostAPI:  e.Manifest.HostAPI,
		Manifest: e.Manifest,
		Secrets:  map[string]bool{},
		Settings: storage.ExtensionSettings{},
	}
	if item.Name == "" {
		item.Name = e.ID
	}
	if e.Loaded {
		item.Status = "loaded"
		s, err := storage.GetExtensionSettings(e.ID)
		if err != nil {
			return item, err
		}
		item.Settings = s
		for _, key := range e.Manifest.SiteSecretKeys() {
			item.Secrets[key] = storage.ExtensionSecretIsSet(e.ID, 0, key)
		}
		if rows, err := storage.ListRecentDeliveries(e.ID, 0, 0, 10); err == nil {
			item.Deliveries = rows
		}
	} else {
		item.Status = "failed"
		item.Error = e.Error
	}
	return item, nil
}

func hookNameDeclared(m extensions.Manifest, name string) bool {
	if name == "*" {
		return true
	}
	for _, h := range m.Hooks {
		if h.On == name {
			return true
		}
	}
	return false
}

func filterDeclaredTriggers(m extensions.Manifest, triggers []string) []string {
	out := make([]string, 0, len(triggers))
	seen := map[string]struct{}{}
	for _, t := range triggers {
		t = strings.TrimSpace(t)
		if t == "*" {
			if _, ok := seen[t]; ok {
				continue
			}
			seen[t] = struct{}{}
			out = append(out, t)
			continue
		}
		if !hookNameDeclared(m, t) {
			continue
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	return out
}
