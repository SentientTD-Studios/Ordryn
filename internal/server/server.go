package server

import (
	"GoTodo/internal/server/digest"
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

func routePaths() []string {
	base := strings.TrimSuffix(utils.GetBasePath(), "/")
	if base == "" || base == "/" {
		return []string{""}
	}
	return []string{"", base}
}

func handle(path string, fn http.HandlerFunc) {
	http.HandleFunc(path, fn)
}

func handleBoth(suffix string, fn http.HandlerFunc) {
	for _, prefix := range routePaths() {
		handle(prefix+suffix, fn)
	}
}

func StartServer() error {
	mode := utils.ResolveMode(os.Args[1:])
	ui := utils.ResolveUI(os.Args[1:])
	utils.SetRuntimeMode(mode)
	utils.SetRuntimeUI(ui)
	utils.LoadRuntimeConfig()

	if mode == utils.ModeFull {
		if err := utils.InitializeTemplates(); err != nil {
			fmt.Println("Error initializing templates: ", err)
			return fmt.Errorf("failed to initialize templates: %v", err)
		}
	} else {
		fmt.Println("Starting in API-only mode (GOTODO_MODE=api / --mode=api); skipping templates and web UI routes")
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	addr := fmt.Sprintf(":%s", port)

	if err := utils.InitRedis(); err != nil {
		fmt.Printf("Warning: Redis init failed: %v\n", err)
	}

	if err := storage.RunMigrations(); err != nil {
		fmt.Printf("Warning: migrations completed with errors: %v\n", err)
	}

	if err := RunBootstrap(); err != nil {
		return fmt.Errorf("bootstrap failed: %w", err)
	}

	digest.StartDigestWorker()

	registerAPIV1Routes()

	if mode == utils.ModeFull {
		if err := handlers.PreloadChangelog(); err != nil {
			fmt.Printf("Warning: Preloading changelog failed: %v\n", err)
		}
		registerSPARoutes()
		registerStaticAndWebRoutes()
		registerHTMXAPIRoutes()
	}

	fmt.Printf("Starting server on %s (mode=%s ui=%s)\n", addr, mode, ui)
	return http.ListenAndServe(addr, utils.SecurityHeadersMiddleware(http.DefaultServeMux))
}

// registerAPIV1Routes registers JSON API routes used by SPA / Android / API-only hosts.
func registerAPIV1Routes() {
	handleBoth("/api/v1/health", handlers.APIV1Health)

	authPublic := utils.AuthPublicChain
	handleBoth("/api/v1/auth/register", authPublic(handlers.APIV1AuthRegister))
	handleBoth("/api/v1/auth/login", authPublic(handlers.APIV1AuthLogin))
	handleBoth("/api/v1/auth/logout", utils.RequireAPIEnabled(handlers.APIV1AuthLogout))
	handleBoth("/api/v1/me", utils.AuthSessionChain(handlers.APIV1Me))
	handleBoth("/api/v1/me/password", utils.AuthSessionChain(handlers.APIV1ChangePassword))
	handleBoth("/api/v1/api-keys", utils.AuthSessionChain(handlers.APIV1APIKeysRouter))
	handleBoth("/api/v1/api-keys/", utils.AuthSessionChain(handlers.APIV1APIKeysRouter))

	devicePublic := handlers.DeviceAuthPublicChain
	handleBoth("/api/v1/auth/device/code", devicePublic(handlers.APIDeviceCode))
	handleBoth("/api/v1/auth/device/token", devicePublic(handlers.APIDeviceToken))
	handleBoth("/api/v1/auth/device/status", utils.RequireAPIEnabled(utils.RequireAPIRedis(handlers.APIV1DeviceStatus)))
	handleBoth("/api/v1/auth/device/approve", utils.AuthSessionChain(handlers.APIV1DeviceApprove))
	handleBoth("/api/v1/auth/device/deny", utils.AuthSessionChain(handlers.APIV1DeviceDeny))

	v1 := utils.APIChain
	handleBoth("/api/v1/tasks", v1(handlers.APIV1TasksRouter))
	handleBoth("/api/v1/tasks/", v1(handlers.APIV1TasksRouter))
	handleBoth("/api/v1/projects", v1(handlers.APIV1ProjectsRouter))
	handleBoth("/api/v1/projects/", v1(handlers.APIV1ProjectsRouter))
	handleBoth("/api/v1/tags", v1(handlers.APIV1TagsRouter))
	handleBoth("/api/v1/tags/", v1(handlers.APIV1TagsRouter))
	handleBoth("/api/v1/saved-views", v1(handlers.APIV1SavedViewsRouter))
	handleBoth("/api/v1/saved-views/", v1(handlers.APIV1SavedViewsRouter))
	handleBoth("/api/v1/dashboard", v1(handlers.APIV1Dashboard))
	handleBoth("/api/v1/calendar", v1(handlers.APIV1CalendarRouter))
	handleBoth("/api/v1/calendar/", v1(handlers.APIV1CalendarRouter))
	handleBoth("/api/v1/export", v1(handlers.APIV1Export))
	handleBoth("/api/v1/invites", utils.InviteAPIChain(handlers.APIV1InvitesRouter))
	handleBoth("/api/v1/invites/", utils.InviteAPIChain(handlers.APIV1InvitesRouter))
	handleBoth("/api/v1/admin/settings", utils.AdminAPIChain(handlers.APIV1AdminSettings))
	handleBoth("/api/v1/admin/users", utils.AdminAPIChain(handlers.APIV1AdminUsersRouter))
	handleBoth("/api/v1/admin/users/", utils.AdminAPIChain(handlers.APIV1AdminUsersRouter))

	// Tokenized ICS feed does not require the HTML UI.
	handleBoth("/cal/", handlers.CalendarFeedHandler)
}

func registerStaticAndWebRoutes() {
	fs := http.FileServer(http.Dir("internal/server/public"))
	for _, prefix := range routePaths() {
		publicPath := prefix + "/public/"
		p := publicPath
		http.Handle(publicPath, http.StripPrefix(p, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Has("v") || strings.HasPrefix(r.URL.Path, "vendor/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				w.Header().Set("Cache-Control", "public, max-age=3600")
			}
			fs.ServeHTTP(w, r)
		})))
	}

	if utils.GetRuntimeUI() == utils.UISPA {
		handleBoth("/", spaRootRedirect)
	} else {
		handleBoth("/", handlers.HomeHandler)
	}
	handleBoth("/p/", handlers.ProjectFilterHandler)
	handleBoth("/favicon.ico", serveFavicon)
	handleBoth("/signup", handlers.SignupPageHandler)
	handleBoth("/register", handlers.RegisterHandler)
	handleBoth("/about", handlers.AboutHandler)
	handleBoth("/documentation", handlers.DocumentationHomeHandler)
	handleBoth("/documentation/api/v1", handlers.APIDocsV1Handler)
	handleBoth("/changelog", handlers.ChangelogHandler)
	handleBoth("/search", handlers.SearchHandler)
	handleBoth("/profile", utils.RequireAuth(handlers.ProfilePage))
	handleBoth("/projects", utils.RequireAuth(handlers.ProjectsPageHandler))
	handleBoth("/dashboard", utils.RequireAuth(handlers.DashboardPageHandler))
	handleBoth("/calendar", utils.RequireAuth(handlers.CalendarPageHandler))
	handleBoth("/import", utils.RequireAuth(handlers.ImportPageHandler))
	handleBoth("/createinvite", utils.RequirePermission("createinvites", handlers.CreateInvitePageHandler))
	handleBoth("/admin", utils.RequirePermission("admin", handlers.AdminPageHandler))
	handleBoth("/admin/", utils.RequirePermission("admin", handlers.AdminPageHandler))
	handleBoth("/forgot-password", handlers.ForgotPasswordPage)
	handleBoth("/password-reset", handlers.PasswordResetPage)
	if utils.GetRuntimeUI() == utils.UISPA {
		handleBoth("/auth/device", spaDeviceAuthRedirect)
	} else {
		handleBoth("/auth/device", handlers.DeviceAuthPageHandler)
	}
	handleBoth("/changelog/page", handlers.ChangelogPageHandler)
}

func registerHTMXAPIRoutes() {
	handleBoth("/api/signup", utils.RequireHTMX(utils.RateLimitMiddleware(5, 0.05, 900, utils.KeyByIP)(handlers.APISignup)))
	handleBoth("/api/login", utils.RequireHTMX(utils.RateLimitMiddleware(10, 1.0, 60, utils.KeyByIP)(handlers.APILogin)))
	handleBoth("/api/logout", utils.RequireHTMX(handlers.APILogout))
	handleBoth("/api/forgot-password", utils.RequireHTMX(utils.RateLimitMiddleware(5, 0.05, 900, utils.KeyByIP)(handlers.APIForgotPassword)))
	handleBoth("/api/reset-password", utils.RequireHTMX(handlers.APIResetPassword))

	handleBoth("/api/fetch-tasks", utils.RequireHTMX(handlers.APIReturnTasks))
	handleBoth("/api/add-task", utils.RequireHTMX(utils.RateLimitMiddleware(60, 1.0, 60, utils.KeyByUser)(handlers.APIAddTask)))
	handleBoth("/api/edit", utils.RequireHTMX(handlers.APIEditTaskForm))
	handleBoth("/api/edit-task", utils.RequireHTMX(utils.RateLimitMiddleware(60, 1.0, 60, utils.KeyByUser)(handlers.APIEditTask)))
	handleBoth("/api/confirm", utils.RequireHTMX(handlers.APIConfirmDelete))
	handleBoth("/api/delete-task", utils.RequireHTMX(utils.RateLimitMiddleware(60, 1.0, 60, utils.KeyByUser)(handlers.APIDeleteTask)))
	handleBoth("/api/update-status", utils.RequireHTMX(handlers.APIUpdateTaskStatus))
	handleBoth("/api/toggle-favorite", utils.RequireHTMX(handlers.APIToggleFavorite))
	handleBoth("/api/reorder-tasks", utils.RequireHTMX(handlers.APIReorderTasks))

	handleBoth("/partials/login", utils.RequireHTMX(handlers.APIGetLoginPartial))

	handleBoth("/api/projects/create", utils.RequireHTMX(utils.RequireAuth(handlers.APICreateProject)))
	handleBoth("/api/projects/update", utils.RequireHTMX(utils.RequireAuth(handlers.APIUpdateProject)))
	handleBoth("/api/projects/delete", utils.RequireHTMX(utils.RequireAuth(handlers.APIDeleteProject)))
	handleBoth("/api/projects/json", utils.RequireHTMX(utils.RequireAuth(handlers.APIProjectsJSON)))
	handleBoth("/api/bulk-update", utils.RequireHTMX(utils.RateLimitMiddleware(60, 1.0, 60, utils.KeyByUser)(handlers.APIBulkUpdate)))
	handleBoth("/api/undo-delete", utils.RequireHTMX(utils.RequireAuth(handlers.APIUndoDelete)))
	handleBoth("/api/task-events", utils.RequireHTMX(utils.RequireAuth(handlers.APITaskEvents)))
	handleBoth("/api/users", utils.RequireHTMX(utils.RequirePermission("admin", handlers.APIGetUsers)))
	handleBoth("/api/export", utils.RequireAuth(handlers.APIExportTasks))
	handleBoth("/api/import/preview", utils.RequireHTMX(utils.RequireAuth(handlers.APIImportPreview)))
	handleBoth("/api/import/confirm", utils.RequireHTMX(utils.RequireAuth(handlers.APIImportConfirm)))
	handleBoth("/api/import/cancel", utils.RequireHTMX(utils.RequireAuth(handlers.APIImportCancel)))
	handleBoth("/api/validate-description", utils.RequireHTMX(handlers.ValidateDescription))
	handleBoth("/api/tags/json", utils.RequireAuth(handlers.APITagsJSON))
	handleBoth("/api/tags/update", utils.RequireHTMX(utils.RequireAuth(handlers.APIUpdateTag)))
	handleBoth("/api/tags/delete", utils.RequireHTMX(utils.RequireAuth(handlers.APIDeleteTag)))
	handleBoth("/api/duplicate-task", utils.RequireHTMX(utils.RateLimitMiddleware(60, 1.0, 60, utils.KeyByUser)(handlers.APIDuplicateTask)))

	handleBoth("/api/saved-views/json", utils.RequireAuth(handlers.APISavedViewsJSON))
	handleBoth("/api/saved-views/save", utils.RequireHTMX(utils.RequireAuth(handlers.APISavedViewsSave)))
	handleBoth("/api/saved-views/delete", utils.RequireHTMX(utils.RequireAuth(handlers.APISavedViewsDelete)))

	handleBoth("/api/profile/api-keys/json", utils.RequireAuth(handlers.APIProfileKeysJSON))
	handleBoth("/api/profile/api-keys/create", utils.RequireHTMX(utils.RequireAuth(handlers.APICreateAPIKey)))
	handleBoth("/api/profile/api-keys/revoke", utils.RequireHTMX(utils.RequireAuth(handlers.APIRevokeAPIKey)))

	handleBoth("/api/auth/device/approve", utils.RequireHTMX(utils.RequireAuth(utils.RequireCSRF(handlers.APIDeviceApprove))))
	handleBoth("/api/auth/device/deny", utils.RequireHTMX(utils.RequireAuth(utils.RequireCSRF(handlers.APIDeviceDeny))))

	handleBoth("/api/update-profile", utils.RequireHTMX(utils.RequireAuth(utils.RequireCSRF(handlers.APIUpdateProfile))))
	handleBoth("/api/change-password", utils.RequireHTMX(utils.RequireAuth(utils.RequireCSRF(handlers.APIChangePassword))))
	handleBoth("/api/calendar/regenerate-token", utils.RequireHTMX(utils.RequireAuth(handlers.APICalendarRegenerateToken)))
	handleBoth("/api/calendar/sync-due-dates", utils.RequireHTMX(utils.RequireAuth(handlers.APICalendarSyncDueDates)))

	handleBoth("/api/create-invite", utils.RequireHTMX(utils.RequirePermission("createinvites", handlers.APICreateInvite)))
	handleBoth("/api/invites", utils.RequireHTMX(utils.RequirePermission("createinvites", handlers.APIGetInvites)))
	handleBoth("/api/confirm-invite-delete", utils.RequireHTMX(utils.RequirePermission("createinvites", handlers.APIConfirmDeleteInvite)))
	handleBoth("/api/ban-user", utils.RequireHTMX(utils.RequirePermission("admin", handlers.APIBanUser)))
	handleBoth("/api/unban-user", utils.RequireHTMX(utils.RequirePermission("admin", handlers.APIUnbanUser)))
	handleBoth("/api/admin/update-settings", utils.RequirePermission("admin", handlers.APIUpdateSiteSettings))
	handleBoth("/api/dismiss-announcement", handlers.APIDismissAnnouncement)

	handleBoth("/api/invite/", func(w http.ResponseWriter, r *http.Request) {
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
}
