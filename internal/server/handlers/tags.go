package handlers

import (
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
	"encoding/json"
	"net/http"
	"strconv"
)

// APITagsJSON returns JSON list of user's tags.
func APITagsJSON(w http.ResponseWriter, r *http.Request) {
	_, _, _, loggedIn := utils.GetSessionUser(r)
	if !loggedIn {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"Unauthorized"}`))
		return
	}
	uidPtr := utils.GetSessionUserID(r)
	if uidPtr == nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	tags, err := storage.GetTagsForUser(*uidPtr)
	if err != nil {
		http.Error(w, "Failed to fetch tags", http.StatusInternalServerError)
		return
	}

	type outTag struct {
		ID    int    `json:"id"`
		Name  string `json:"name"`
		Color string `json:"color"`
	}
	out := make([]outTag, 0, len(tags))
	for _, t := range tags {
		out = append(out, outTag{ID: t.ID, Name: t.Name, Color: t.Color})
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(out)
}

// APIDeleteTag deletes a tag owned by the user.
func APIDeleteTag(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}
	_, _, _, loggedIn := utils.GetSessionUser(r)
	if !loggedIn {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	uidPtr := utils.GetSessionUserID(r)
	if uidPtr == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Error parsing form", http.StatusBadRequest)
		return
	}
	id, err := strconv.Atoi(r.FormValue("id"))
	if err != nil {
		http.Error(w, "Invalid tag id", http.StatusBadRequest)
		return
	}

	if err := storage.DeleteTag(id, *uidPtr); err != nil {
		http.Error(w, "Failed to delete tag", http.StatusInternalServerError)
		return
	}

	w.Header().Set("HX-Trigger", "tags-changed")
	w.WriteHeader(http.StatusOK)
}
