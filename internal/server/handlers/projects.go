package handlers

import (
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

const MaxProjectNameLength = 50

// ProjectsPageHandler shows the user's projects and a simple create form.
func ProjectsPageHandler(w http.ResponseWriter, r *http.Request) {
	_, _, _, loggedIn := utils.GetSessionUser(r)
	if !loggedIn {
		utils.SetFlash(w, r, "You don't have permission to access this.")
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	uidPtr := utils.GetSessionUserID(r)
	if uidPtr == nil {
		utils.SetFlash(w, r, "You don't have permission to access this.")
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	projects, err := storage.GetProjectsForUser(*uidPtr)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error fetching projects: %v", err), http.StatusInternalServerError)
		return
	}

	ctx := map[string]interface{}{
		"LoggedIn": loggedIn,
		"Projects": projects,
	}
	utils.RenderTemplate(w, r, "projects.html", ctx)
}

// APICreateProject handles creating a new project for the logged-in user.
func APICreateProject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}
	_, _, _, loggedIn := utils.GetSessionUser(r)
	if !loggedIn {
		http.Redirect(w, r, "/", http.StatusUnauthorized)
		return
	}

	uidPtr := utils.GetSessionUserID(r)
	if uidPtr == nil {
		http.Redirect(w, r, "/", http.StatusUnauthorized)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Error parsing form", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))

	// Validate project name length
	if len(name) > MaxProjectNameLength {
		w.Header().Set("X-Validation-Error", "true")
		w.Header().Set("HX-Trigger", "project-name-error")
		w.Header().Set("HX-Retarget", "#project-name-error")
		w.Header().Set("HX-Reswap", "innerHTML")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "Project name must be %d characters or less", MaxProjectNameLength)
		return
	}

	if name == "" {
		w.Header().Set("X-Validation-Error", "true")
		w.Header().Set("HX-Trigger", "project-name-error")
		w.Header().Set("HX-Retarget", "#project-name-error")
		w.Header().Set("HX-Reswap", "innerHTML")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "Project name is required")
		return
	}

	newProject, err := storage.CreateProject(*uidPtr, name)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create project: %v", err), http.StatusInternalServerError)
		return
	}

	// If this is an HTMX request, return the updated list fragment
	if r.Header.Get("HX-Request") == "true" {
		projects, err := storage.GetProjectsForUser(*uidPtr)
		if err != nil {
			http.Error(w, fmt.Sprintf("Error fetching projects: %v", err), http.StatusInternalServerError)
			return
		}
		ctx := map[string]interface{}{
			"Projects": projects,
		}
		w.Header().Set("HX-Trigger", fmt.Sprintf("projects-changed set-project-filter:%d", newProject.ID))
		utils.RenderTemplate(w, r, "projects_list.html", ctx)
		return
	}

	// Fallback: redirect back to the projects page
	basePath := utils.GetBasePath()
	w.Header().Set("HX-Redirect", basePath+"/projects")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, " ")
}

// APIDeleteProject deletes a project owned by the logged-in user.
func APIDeleteProject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}
	_, _, _, loggedIn := utils.GetSessionUser(r)
	if !loggedIn {
		http.Redirect(w, r, "/", http.StatusUnauthorized)
		return
	}

	uidPtr := utils.GetSessionUserID(r)
	if uidPtr == nil {
		http.Redirect(w, r, "/", http.StatusUnauthorized)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Error parsing form", http.StatusBadRequest)
		return
	}
	idStr := r.FormValue("id")
	if idStr == "" {
		http.Error(w, "Project id required", http.StatusBadRequest)
		return
	}
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "Invalid project id", http.StatusBadRequest)
		return
	}

	// Delete the project (ownership enforced in storage layer)
	err = storage.DeleteProject(id, *uidPtr)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to delete project: %v", err), http.StatusInternalServerError)
		return
	}

	// If HTMX request, return updated fragment
	if r.Header.Get("HX-Request") == "true" {
		projects, err := storage.GetProjectsForUser(*uidPtr)
		if err != nil {
			http.Error(w, fmt.Sprintf("Error fetching projects: %v", err), http.StatusInternalServerError)
			return
		}
		ctx := map[string]interface{}{
			"Projects": projects,
		}
		// Notify client that projects changed so JS can refresh selects
		w.Header().Set("HX-Trigger", "projects-changed")
		utils.RenderTemplate(w, r, "projects_list.html", ctx)
		return
	}

	// Fallback: redirect back to the projects page
	basePath := utils.GetBasePath()
	w.Header().Set("HX-Redirect", basePath+"/projects")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, " ")
}

// APIUpdateProject renames a project owned by the logged-in user.
func APIUpdateProject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}
	_, _, _, loggedIn := utils.GetSessionUser(r)
	if !loggedIn {
		http.Redirect(w, r, "/", http.StatusUnauthorized)
		return
	}

	uidPtr := utils.GetSessionUserID(r)
	if uidPtr == nil {
		http.Redirect(w, r, "/", http.StatusUnauthorized)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Error parsing form", http.StatusBadRequest)
		return
	}

	idStr := r.FormValue("id")
	if idStr == "" {
		http.Error(w, "Project id required", http.StatusBadRequest)
		return
	}
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "Invalid project id", http.StatusBadRequest)
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	if len(name) > MaxProjectNameLength {
		w.Header().Set("X-Validation-Error", "true")
		w.Header().Set("HX-Trigger", "project-name-error")
		w.Header().Set("HX-Retarget", "#project-name-error")
		w.Header().Set("HX-Reswap", "innerHTML")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "Project name must be %d characters or less", MaxProjectNameLength)
		return
	}
	if name == "" {
		w.Header().Set("X-Validation-Error", "true")
		w.Header().Set("HX-Trigger", "project-name-error")
		w.Header().Set("HX-Retarget", "#project-name-error")
		w.Header().Set("HX-Reswap", "innerHTML")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "Project name is required")
		return
	}

	if _, err := storage.GetProjectByID(id, *uidPtr); err != nil {
		http.Error(w, "Project not found.", http.StatusNotFound)
		return
	}

	if err := storage.UpdateProject(id, *uidPtr, name); err != nil {
		http.Error(w, fmt.Sprintf("Failed to update project: %v", err), http.StatusInternalServerError)
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		projects, err := storage.GetProjectsForUser(*uidPtr)
		if err != nil {
			http.Error(w, fmt.Sprintf("Error fetching projects: %v", err), http.StatusInternalServerError)
			return
		}
		ctx := map[string]interface{}{
			"Projects": projects,
		}
		w.Header().Set("HX-Trigger", "projects-changed")
		utils.RenderTemplate(w, r, "projects_list.html", ctx)
		return
	}

	basePath := utils.GetBasePath()
	w.Header().Set("HX-Redirect", basePath+"/projects")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, " ")
}

// APIProjectsJSON returns a JSON list of the user's projects (id and name)
func APIProjectsJSON(w http.ResponseWriter, r *http.Request) {
	_, _, _, loggedIn := utils.GetSessionUser(r)
	if !loggedIn {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"Unauthorized"}`))
		return
	}
	uidPtr := utils.GetSessionUserID(r)
	if uidPtr == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"Unauthorized"}`))
		return
	}
	projects, err := storage.GetProjectsForUser(*uidPtr)
	if err != nil {
		http.Error(w, "Failed to fetch projects", http.StatusInternalServerError)
		return
	}
	type pj struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	out := make([]pj, 0, len(projects))
	for _, p := range projects {
		out = append(out, pj{ID: p.ID, Name: p.Name})
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(out)
}
