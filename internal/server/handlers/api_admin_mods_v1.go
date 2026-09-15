package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"GoTodo/internal/mods"
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
)

type adminModJSON struct {
	ID       string              `json:"id"`
	Name     string              `json:"name"`
	Version  string              `json:"version"`
	HostAPI  int                 `json:"host_api"`
	Status   string              `json:"status"`
	Error    string              `json:"error,omitempty"`
	Manifest mods.Manifest       `json:"manifest"`
	Settings storage.ModSettings `json:"settings"`
	Secrets  map[string]bool     `json:"secrets"`
}

type adminModsListJSON struct {
	Mods []adminModJSON `json:"mods"`
}

type adminModPatch struct {
	Enabled *bool `json:"enabled"`
}

// APIV1AdminModsRouter handles /api/v1/admin/mods and /{id}.
func APIV1AdminModsRouter(w http.ResponseWriter, r *http.Request) {
	sub := utils.ParseAPIV1Subpath(r, "admin/mods")
	if sub == "" {
		if r.Method != http.MethodGet {
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
			return
		}
		adminModsList(w, r)
		return
	}
	parts := strings.Split(sub, "/")
	id := parts[0]
	if id == "" {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid mod id.")
		return
	}
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			adminModGet(w, r, id)
		case http.MethodPatch:
			adminModPatchHandler(w, r, id)
		default:
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		}
		return
	}
	utils.APIJSONError(w, http.StatusNotFound, "not_found", "Not found.")
}

func adminModsList(w http.ResponseWriter, r *http.Request) {
	_ = r
	entries := mods.Snapshot()
	out := make([]adminModJSON, 0, len(entries))
	for _, e := range entries {
		item, err := adminModFromEntry(e)
		if err != nil {
			utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load mod settings.")
			return
		}
		out = append(out, item)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(adminModsListJSON{Mods: out})
}

func adminModGet(w http.ResponseWriter, r *http.Request, id string) {
	_ = r
	e, ok := mods.Get(id)
	if !ok {
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Mod not found.")
		return
	}
	item, err := adminModFromEntry(e)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load mod settings.")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(item)
}

func adminModPatchHandler(w http.ResponseWriter, r *http.Request, id string) {
	e, ok := mods.Get(id)
	if !ok {
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Mod not found.")
		return
	}
	if !e.Loaded {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Cannot configure a failed mod.")
		return
	}
	var req adminModPatch
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON.")
		return
	}
	cur, err := storage.GetModSettings(id)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load mod settings.")
		return
	}
	if req.Enabled != nil {
		cur.Enabled = *req.Enabled
	}
	if err := storage.UpsertModSettings(id, cur); err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to save mod settings.")
		return
	}
	item, err := adminModFromEntry(e)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load mod settings.")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(item)
}

func adminModFromEntry(e mods.Entry) (adminModJSON, error) {
	item := adminModJSON{
		ID:       e.ID,
		Name:     e.Manifest.Name,
		Version:  e.Manifest.Version,
		HostAPI:  e.Manifest.HostAPI,
		Manifest: e.Manifest,
		Secrets:  map[string]bool{},
		Settings: storage.ModSettings{},
	}
	if item.Name == "" {
		item.Name = e.ID
	}
	if e.Loaded {
		item.Status = "loaded"
		s, err := storage.GetModSettings(e.ID)
		if err != nil {
			return item, err
		}
		item.Settings = s
		for _, key := range e.Manifest.SiteSecretKeys() {
			item.Secrets[key] = storage.ModSecretIsSet(e.ID, 0, key)
		}
	} else {
		item.Status = "failed"
		item.Error = e.Error
	}
	return item, nil
}

func hookNameDeclared(m mods.Manifest, name string) bool {
	for _, h := range m.Hooks {
		if h.On == name {
			return true
		}
	}
	return false
}

func filterDeclaredTriggers(m mods.Manifest, triggers []string) []string {
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
