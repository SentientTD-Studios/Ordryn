package handlers

import (
	"GoTodo/internal/server/utils"
	"GoTodo/internal/sessionstore"
	"GoTodo/internal/storage"
	"GoTodo/internal/tasks"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type exportTaskJSON struct {
	ID           int              `json:"id"`
	Title        string           `json:"title"`
	Description  string           `json:"description"`
	Completed    bool             `json:"completed"`
	DueDate      string           `json:"due_date"`
	Project      string           `json:"project"`
	Priority     int              `json:"priority"`
	Favorite     bool             `json:"favorite"`
	Position     int              `json:"position"`
	Tags         []exportTagJSON  `json:"tags"`
	CreatedAt    string           `json:"created_at"`
	ModifiedAt   string           `json:"modified_at"`
}

type exportTagJSON struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

// APIExportTasks exports tasks matching active filters as CSV or JSON.
func APIExportTasks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	email, _, _, timezone, loggedIn, _ := utils.GetSessionUserWithTimezone(r)
	if !loggedIn {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if isBanned, err := storage.IsUserBanned(email); err == nil && isBanned {
		sessionstore.ClearSessionCookie(w, r)
		http.Redirect(w, r, utils.GetBasePath()+"/", http.StatusSeeOther)
		return
	}

	userID := utils.GetSessionUserID(r)
	if userID == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	if format == "" {
		format = "csv"
	}
	if format != "csv" && format != "json" {
		http.Error(w, "Invalid format", http.StatusBadRequest)
		return
	}

	fc := filterContextFromRequest(r)
	search := fc.Search
	filters := fc.ToListFilters()

	taskList, err := tasks.FetchAllTasksForUserWithFilters(userID, timezone, filters, search)
	if err != nil {
		http.Error(w, "Failed to export tasks", http.StatusInternalServerError)
		return
	}

	filenameDate := time.Now().Format("2006-01-02")
	if format == "json" {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="gotodo-export-%s.json"`, filenameDate))
		out := make([]exportTaskJSON, 0, len(taskList))
		for _, t := range taskList {
			out = append(out, taskToExportJSON(t))
		}
		json.NewEncoder(w).Encode(out)
		return
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="gotodo-export-%s.csv"`, filenameDate))
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"id", "title", "description", "completed", "due_date", "project", "priority", "favorite", "position", "tags", "created_at", "modified_at"})
	for _, t := range taskList {
		_ = cw.Write(taskToCSVRow(t))
	}
	cw.Flush()
}

func taskToExportJSON(t tasks.Task) exportTaskJSON {
	tags := make([]exportTagJSON, 0, len(t.Tags))
	for _, tg := range t.Tags {
		tags = append(tags, exportTagJSON{ID: tg.ID, Name: tg.Name, Color: tg.Color})
	}
	return exportTaskJSON{
		ID:          t.ID,
		Title:       t.Title,
		Description: t.Description,
		Completed:   t.Completed,
		DueDate:     t.DueDate,
		Project:     t.ProjectName,
		Priority:    t.Priority,
		Favorite:    t.IsFavorite,
		Position:    t.Position,
		Tags:        tags,
		CreatedAt:   t.DateCreated,
		ModifiedAt:  t.DateModified,
	}
}

func taskToCSVRow(t tasks.Task) []string {
	tagNames := make([]string, 0, len(t.Tags))
	for _, tg := range t.Tags {
		tagNames = append(tagNames, tg.Name)
	}
	return []string{
		strconv.Itoa(t.ID),
		t.Title,
		t.Description,
		strconv.FormatBool(t.Completed),
		t.DueDate,
		t.ProjectName,
		strconv.Itoa(t.Priority),
		strconv.FormatBool(t.IsFavorite),
		strconv.Itoa(t.Position),
		strings.Join(tagNames, ";"),
		t.DateCreated,
		t.DateModified,
	}
}
