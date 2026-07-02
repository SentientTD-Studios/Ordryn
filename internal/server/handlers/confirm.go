package handlers

import (
	"GoTodo/internal/server/utils"
	"net/http"
)

func APIConfirmDelete(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "Task ID is required", http.StatusBadRequest)
		return
	}

	page := r.URL.Query().Get("page")
	if page == "" {
		page = "1"
	}
	fc := filterContextFromRequest(r)

	data := struct {
		ID          string
		CurrentPage string
		FilterQuery string
	}{
		ID:          id,
		CurrentPage: page,
		FilterQuery: fc.QuerySuffix(),
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	err := utils.Templates.ExecuteTemplate(w, "confirm.html", data)
	if err != nil {
		http.Error(w, "Error rendering template: "+err.Error(), http.StatusInternalServerError)
	}
}
