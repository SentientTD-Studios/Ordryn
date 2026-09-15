package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"GoTodo/internal/extensions"
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
)

type adminExtensionJSON struct {
	ID       string                    `json:"id"`
	Name     string                    `json:"name"`
	Version  string                    `json:"version"`
	HostAPI  int                       `json:"host_api"`
	Status   string                    `json:"status"`
	Error    string                    `json:"error,omitempty"`
	Manifest extensions.Manifest       `json:"manifest"`
	Settings storage.ExtensionSettings `json:"settings"`
	Secrets  map[string]bool           `json:"secrets"`
}

type adminExtensionsListJSON struct {
	Extensions []adminExtensionJSON `json:"extensions"`
}

type adminExtensionPatch struct {
	Enabled *bool `json:"enabled"`
}

// APIV1AdminExtensionsRouter handles /api/v1/admin/extensions and /{id}.
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
	if req.Enabled != nil {
		cur.Enabled = *req.Enabled
	}
	if err := storage.UpsertExtensionSettings(id, cur); err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to save extension settings.")
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
	} else {
		item.Status = "failed"
		item.Error = e.Error
	}
	return item, nil
}

func hookNameDeclared(m extensions.Manifest, name string) bool {
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
