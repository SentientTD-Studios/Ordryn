package handlers

import (
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

const maxSavedViewNameLen = 80

type savedViewJSON struct {
	ID     int                    `json:"id"`
	Name   string                 `json:"name"`
	Filter storage.SavedViewFilter `json:"filter"`
}

func filterToSavedViewFilter(fc FilterContext) storage.SavedViewFilter {
	return storage.SavedViewFilter{
		Project:   fc.Project,
		Status:    fc.Status,
		Due:       fc.Due,
		Completed: fc.Completed,
		Priority:  fc.Priority,
		Tag:       fc.Tag,
		Sort:      fc.Sort,
		Search:    fc.Search,
	}
}

func savedViewFilterToContext(f storage.SavedViewFilter) FilterContext {
	return FilterContext{
		Project:   f.Project,
		Status:    f.Status,
		Due:       f.Due,
		Completed: f.Completed,
		Priority:  f.Priority,
		Tag:       f.Tag,
		Sort:      f.Sort,
		Search:    f.Search,
	}
}

func parseSavedViewFilterFromForm(r *http.Request) storage.SavedViewFilter {
	fc := filterContextFromRequest(r)
	return filterToSavedViewFilter(fc)
}

func requireSavedViewUser(w http.ResponseWriter, r *http.Request) (*int, bool) {
	_, _, _, loggedIn := utils.GetSessionUser(r)
	if !loggedIn {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
		return nil, false
	}
	uid := utils.GetSessionUserID(r)
	if uid == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
		return nil, false
	}
	return uid, true
}

// APISavedViewsJSON returns the user's saved filter views.
func APISavedViewsJSON(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	uid, ok := requireSavedViewUser(w, r)
	if !ok {
		return
	}
	views, err := storage.ListSavedViewsForUser(*uid)
	if err != nil {
		http.Error(w, "Failed to load saved views", http.StatusInternalServerError)
		return
	}
	out := make([]savedViewJSON, 0, len(views))
	for _, v := range views {
		out = append(out, savedViewJSON{ID: v.ID, Name: v.Name, Filter: v.Filter})
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(out)
}

// APISavedViewsSave creates or updates a saved view.
func APISavedViewsSave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	uid, ok := requireSavedViewUser(w, r)
	if !ok {
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "name is required"})
		return
	}
	if len(name) > maxSavedViewNameLen {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "name too long"})
		return
	}

	filter := parseSavedViewFilterFromForm(r)
	idStr := strings.TrimSpace(r.FormValue("id"))
	renameOnly := r.FormValue("rename_only") == "true" || r.FormValue("rename_only") == "1"

	if idStr != "" {
		id, err := strconv.Atoi(idStr)
		if err != nil || id <= 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid id"})
			return
		}
		if _, err := storage.GetSavedViewByID(id, *uid); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
			return
		}
		var filterPtr *storage.SavedViewFilter
		if !renameOnly {
			filterPtr = &filter
		}
		if err := storage.UpdateSavedView(id, *uid, name, filterPtr); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": "update failed"})
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		json.NewEncoder(w).Encode(savedViewJSON{ID: id, Name: name, Filter: filter})
		return
	}

	count, err := storage.CountSavedViewsForUser(*uid)
	if err != nil {
		http.Error(w, "Failed to count saved views", http.StatusInternalServerError)
		return
	}
	if count >= storage.MaxSavedViewsPerUser {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "maximum saved views reached"})
		return
	}

	created, err := storage.CreateSavedView(*uid, name, filter, count)
	if err != nil {
		if strings.Contains(err.Error(), "unique") || strings.Contains(err.Error(), "duplicate") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(map[string]string{"error": "a view with this name already exists"})
			return
		}
		http.Error(w, "Failed to save view", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(savedViewJSON{ID: created.ID, Name: created.Name, Filter: created.Filter})
}

// APISavedViewsDelete removes a saved view.
func APISavedViewsDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	uid, ok := requireSavedViewUser(w, r)
	if !ok {
		return
	}
	idStr := strings.TrimSpace(r.FormValue("id"))
	id, err := strconv.Atoi(idStr)
	if err != nil || id <= 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid id"})
		return
	}
	if err := storage.DeleteSavedView(id, *uid); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
}
