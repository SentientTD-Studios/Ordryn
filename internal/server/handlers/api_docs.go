package handlers

import (
	"GoTodo/internal/server/utils"
	"net/http"
)

// DocumentationHomeHandler serves the documentation index at /documentation.
func DocumentationHomeHandler(w http.ResponseWriter, r *http.Request) {
	email, _, permissions, loggedIn := utils.GetSessionUser(r)

	context := map[string]interface{}{
		"LoggedIn":    loggedIn,
		"UserEmail":   email,
		"Permissions": permissions,
		"Title":       "GoTodo - Documentation",
	}

	utils.RenderTemplate(w, r, "documentation.html", context)
}

// APIDocsV1Handler serves the public REST API v1 documentation page.
func APIDocsV1Handler(w http.ResponseWriter, r *http.Request) {
	email, _, permissions, loggedIn := utils.GetSessionUser(r)

	context := map[string]interface{}{
		"LoggedIn":    loggedIn,
		"UserEmail":   email,
		"Permissions": permissions,
		"Title":       "GoTodo - REST API v1",
	}

	utils.RenderTemplate(w, r, "api_v1_docs.html", context)
}
