package server

import (
	"GoTodo/internal/server/handlers"
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
	"fmt"
	"net/http"
	"os"
	"strings"
)

// Literally just used to prevent favicon.ico from being requested
func serveFavicon(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "internal/server/public/favicon.svg")
}

func StartServer() error {
	err := utils.InitializeTemplates()
	if err != nil {
		fmt.Println("Error initializing templates: ", err)
		return fmt.Errorf("failed to initialize templates: %v", err)
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	addr := fmt.Sprintf(":%s", port)

	// Initialize Redis client for rate limiting (optional)
	if err := utils.InitRedis(); err != nil {
		fmt.Printf("Warning: Redis init failed: %v\n", err)
	}

	// Run DB migrations (create tables / add columns as needed)
	if err := storage.RunMigrations(); err != nil {
		fmt.Printf("Warning: migrations completed with errors: %v\n", err)
	}

	// Preload changelog from GitHub at startup to avoid runtime API calls
	if err := handlers.PreloadChangelog(); err != nil {
		fmt.Printf("Warning: Preloading changelog failed: %v\n", err)
	}

	fs := http.FileServer(http.Dir("internal/server/public"))
	publicHandler := http.StripPrefix("/public/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		if r.URL.Query().Has("v") || strings.HasPrefix(r.URL.Path, "vendor/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "public, max-age=3600")
		}
		fs.ServeHTTP(w, r)
	}))
	http.Handle("/public/", publicHandler)

	// Regular page handlers (no HTMX requirement)
	http.HandleFunc("/", handlers.HomeHandler)
	http.HandleFunc("/favicon.ico", serveFavicon)
	http.HandleFunc("/signup", handlers.SignupPageHandler)
	http.HandleFunc("/register", handlers.RegisterHandler)
	http.HandleFunc("/about", handlers.AboutHandler)
	http.HandleFunc("/changelog", handlers.ChangelogHandler)
	http.HandleFunc("/search", handlers.SearchHandler)
	http.HandleFunc("/profile", handlers.ProfilePage)
	http.HandleFunc("/projects", utils.RequireAuth(handlers.ProjectsPageHandler))
	http.HandleFunc("/import", utils.RequireAuth(handlers.ImportPageHandler))
	http.HandleFunc("/createinvite", utils.RequirePermission("createinvites", handlers.CreateInvitePageHandler))
	http.HandleFunc("/admin", utils.RequirePermission("admin", handlers.AdminPageHandler))
	http.HandleFunc("/admin/", utils.RequirePermission("admin", handlers.AdminPageHandler))
	http.HandleFunc("/forgot-password", handlers.ForgotPasswordPage)
	http.HandleFunc("/password-reset", handlers.PasswordResetPage)

	// API endpoints - all require HTMX header
	// Auth endpoints with rate limiting
	http.HandleFunc("/api/signup", utils.RequireHTMX(utils.RateLimitMiddleware(5, 0.05, 900, utils.KeyByIP)(handlers.APISignup)))
	http.HandleFunc("/api/login", utils.RequireHTMX(utils.RateLimitMiddleware(10, 1.0, 60, utils.KeyByIP)(handlers.APILogin)))
	http.HandleFunc("/api/logout", utils.RequireHTMX(handlers.APILogout))
	http.HandleFunc("/api/forgot-password", utils.RequireHTMX(utils.RateLimitMiddleware(5, 0.05, 900, utils.KeyByIP)(handlers.APIForgotPassword)))
	http.HandleFunc("/api/reset-password", utils.RequireHTMX(handlers.APIResetPassword))

	// Task endpoints
	http.HandleFunc("/api/fetch-tasks", utils.RequireHTMX(handlers.APIReturnTasks))
	http.HandleFunc("/api/add-task", utils.RequireHTMX(utils.RateLimitMiddleware(60, 1.0, 60, utils.KeyByUser)(handlers.APIAddTask)))
	http.HandleFunc("/api/edit", utils.RequireHTMX(handlers.APIEditTaskForm))
	http.HandleFunc("/api/edit-task", utils.RequireHTMX(utils.RateLimitMiddleware(60, 1.0, 60, utils.KeyByUser)(handlers.APIEditTask)))
	http.HandleFunc("/api/confirm", utils.RequireHTMX(handlers.APIConfirmDelete))
	http.HandleFunc("/api/delete-task", utils.RequireHTMX(utils.RateLimitMiddleware(60, 1.0, 60, utils.KeyByUser)(handlers.APIDeleteTask)))
	http.HandleFunc("/api/update-status", utils.RequireHTMX(handlers.APIUpdateTaskStatus))
	http.HandleFunc("/api/toggle-favorite", utils.RequireHTMX(handlers.APIToggleFavorite))
	http.HandleFunc("/api/reorder-tasks", utils.RequireHTMX(handlers.APIReorderTasks))

	// Partials
	http.HandleFunc("/partials/login", utils.RequireHTMX(handlers.APIGetLoginPartial))

	// Changelog pagination
	http.HandleFunc("/changelog/page", handlers.ChangelogPageHandler)

	// Projects API endpoints
	http.HandleFunc("/api/projects/create", utils.RequireHTMX(utils.RequireAuth(handlers.APICreateProject)))
	http.HandleFunc("/api/projects/update", utils.RequireHTMX(utils.RequireAuth(handlers.APIUpdateProject)))
	http.HandleFunc("/api/projects/delete", utils.RequireHTMX(utils.RequireAuth(handlers.APIDeleteProject)))
	http.HandleFunc("/api/projects/json", utils.RequireHTMX(utils.RequireAuth(handlers.APIProjectsJSON)))
	http.HandleFunc("/api/bulk-update", utils.RequireHTMX(utils.RateLimitMiddleware(60, 1.0, 60, utils.KeyByUser)(handlers.APIBulkUpdate)))
	http.HandleFunc("/api/export", utils.RequireAuth(handlers.APIExportTasks))
	http.HandleFunc("/api/import", utils.RequireHTMX(utils.RequireAuth(handlers.APIImportTasks)))
	http.HandleFunc("/api/tags/delete", utils.RequireHTMX(utils.RequireAuth(handlers.APIDeleteTag)))

	// Profile API endpoints
	http.HandleFunc("/api/update-timezone", utils.RequireHTMX(handlers.APIUpdateTimezone))
	http.HandleFunc("/api/update-profile", utils.RequireHTMX(handlers.APIUpdateProfile))
	http.HandleFunc("/api/change-password", utils.RequireHTMX(handlers.APIChangePassword))

	// Invite API endpoints
	http.HandleFunc("/api/create-invite", utils.RequireHTMX(utils.RequirePermission("createinvites", handlers.APICreateInvite)))
	http.HandleFunc("/api/invites", utils.RequireHTMX(utils.RequirePermission("createinvites", handlers.APIGetInvites)))
	http.HandleFunc("/api/confirm-invite-delete", utils.RequireHTMX(utils.RequirePermission("createinvites", handlers.APIConfirmDeleteInvite)))

	// Ban/unban user actions (admin only)
	http.HandleFunc("/api/ban-user", utils.RequireHTMX(utils.RequirePermission("createinvites", handlers.APIBanUser)))
	http.HandleFunc("/api/unban-user", utils.RequireHTMX(utils.RequirePermission("createinvites", handlers.APIUnbanUser)))

	// Admin API endpoints
	http.HandleFunc("/api/admin/update-settings", utils.RequirePermission("admin", handlers.APIUpdateSiteSettings))

	// Handle PUT and DELETE for invites with path parameters
	http.HandleFunc("/api/invite/", func(w http.ResponseWriter, r *http.Request) {
		// Check HTMX header first
		if r.Header.Get("HX-Request") != "true" {
			basePath := utils.GetBasePath()
			http.Redirect(w, r, basePath+"/", http.StatusSeeOther)
			return
		}

		switch r.Method {
		case http.MethodPut:
			utils.RequirePermission("createinvites", handlers.APIUpdateInvite)(w, r)
		case http.MethodDelete:
			utils.RequirePermission("createinvites", handlers.APIDeleteInvite)(w, r)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// Dismiss announcement endpoint
	http.HandleFunc("/api/dismiss-announcement", handlers.APIDismissAnnouncement)

	fmt.Printf("Starting server on %s\n", addr)
	return http.ListenAndServe(addr, utils.SecurityHeadersMiddleware(http.DefaultServeMux))
}
