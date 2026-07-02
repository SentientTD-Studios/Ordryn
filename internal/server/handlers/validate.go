package handlers

import (
	"fmt"
	"net/http"
)

func ValidateDescription(w http.ResponseWriter, r *http.Request) {
	var description string
	switch r.Method {
	case http.MethodGet:
		description = r.URL.Query().Get("description")
	case http.MethodPost:
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Error parsing form data", http.StatusBadRequest)
			return
		}
		description = r.FormValue("description")
	default:
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	if len(description) > MaxDescriptionLength {
		w.Header().Set("HX-Trigger", "description-error")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "Description must be %d characters or less", MaxDescriptionLength)
		return
	}

	w.WriteHeader(http.StatusOK)
}
