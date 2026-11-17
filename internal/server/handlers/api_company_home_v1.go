package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"GoTodo/internal/domain"
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
)

type apiCompanyHomeProjectJSON struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Slug    string `json:"slug"`
	Open    int    `json:"open"`
	Overdue int    `json:"overdue"`
}

type apiCompanyHomeTeamJSON struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Members int    `json:"members"`
}

type apiCompanyHomeSummaryJSON struct {
	Today  string `json:"today"`
	MyWork struct {
		Open     int `json:"open"`
		Overdue  int `json:"overdue"`
		DueToday int `json:"due_today"`
		DueWeek  int `json:"due_week"`
	} `json:"my_work"`
	Projects      []apiCompanyHomeProjectJSON `json:"projects"`
	TotalProjects int                         `json:"total_projects"`
	Teams         []apiCompanyHomeTeamJSON    `json:"teams"`
}

// APIV1CompanyHome handles GET /api/v2/company-home: the company home page in
// company mode. content is Markdown edited in site settings; summary is the
// caller's own work, projects and teams.
func APIV1CompanyHome(w http.ResponseWriter, r *http.Request) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	if r.Method != http.MethodGet {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	if !storage.IsCompanyMode() {
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Only used in company mode.")
		return
	}
	content, err := storage.GetCompanyHome()
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load the home page.")
		return
	}
	tz := GetUserTimezoneByID(userID)
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.UTC
	}
	today := time.Now().In(loc).Format("2006-01-02")
	sum, err := domain.GetCompanyHomeSummary(r.Context(), userID, today)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load the home page.")
		return
	}
	var s apiCompanyHomeSummaryJSON
	s.Today = today
	s.MyWork.Open, s.MyWork.Overdue = sum.MyWork.Open, sum.MyWork.Overdue
	s.MyWork.DueToday, s.MyWork.DueWeek = sum.MyWork.DueToday, sum.MyWork.DueWeek
	s.TotalProjects = sum.TotalProjects
	s.Projects = make([]apiCompanyHomeProjectJSON, 0, len(sum.Projects))
	for _, p := range sum.Projects {
		s.Projects = append(s.Projects, apiCompanyHomeProjectJSON{ID: p.ID, Name: p.Name, Slug: p.Slug, Open: p.Open, Overdue: p.Overdue})
	}
	s.Teams = make([]apiCompanyHomeTeamJSON, 0, len(sum.Teams))
	for _, t := range sum.Teams {
		s.Teams = append(s.Teams, apiCompanyHomeTeamJSON{ID: t.ID, Name: t.Name, Members: t.Members})
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"content":  content,
		"can_edit": storage.UserHasSitePermission(userID, storage.SitePermSettingsManage),
		"summary":  s,
	})
}
