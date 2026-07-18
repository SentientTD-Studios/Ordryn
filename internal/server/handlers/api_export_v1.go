package handlers

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"GoTodo/internal/server/utils"
	"GoTodo/internal/tasks"
)

// APIV1Export exports tasks as CSV or JSON for the authenticated user.
func APIV1Export(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	userID, ok := utils.GetAPIUserID(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	if format == "" {
		format = "json"
	}
	if format != "csv" && format != "json" {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "format must be csv or json.")
		return
	}

	tz := GetUserTimezoneByID(userID)
	fc := filterContextFromRequest(r)
	taskList, err := tasks.FetchAllTasksForUserWithFilters(&userID, tz, fc.ToListFilters(), fc.Search)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to export tasks.")
		return
	}

	stamp := time.Now().UTC().Format("20060102T150405Z")
	if format == "json" {
		out := make([]exportTaskJSON, 0, len(taskList))
		for _, t := range taskList {
			out = append(out, taskToExportJSON(t))
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="gotodo-export-%s.json"`, stamp))
		_ = json.NewEncoder(w).Encode(out)
		return
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="gotodo-export-%s.csv"`, stamp))
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"id", "title", "description", "completed", "due_date", "project", "priority", "favorite", "tags"})
	for _, t := range taskList {
		_ = cw.Write(taskToCSVRow(t))
	}
	cw.Flush()
}
