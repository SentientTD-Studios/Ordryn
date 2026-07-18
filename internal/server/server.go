package server

import (
	"GoTodo/internal/server/digest"
	"GoTodo/internal/server/handlers"
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
	"fmt"
	"net/http"
	"os"
)

func serveFavicon(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "internal/server/public/favicon.svg")
}

func routePaths() []string {
	base := utils.PublicPathPrefix()
	if base == "" {
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
	utils.LoadRuntimeConfig()

	mode := utils.ResolveMode(os.Args[1:])
	utils.SetRuntimeMode(mode)

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
		registerFullModeRoutes()
	}

	fmt.Printf("Starting server on %s (mode=%s)\n", addr, mode)
	return http.ListenAndServe(addr, utils.SecurityHeadersMiddleware(http.DefaultServeMux))
}

func registerAPIV1Routes() {
	handleBoth("/api/v1/health", handlers.APIV1Health)
	handleBoth("/api/v1/site", handlers.APIV1Site)

	authPublic := utils.AuthPublicChain
	handleBoth("/api/v1/auth/register", authPublic(handlers.APIV1AuthRegister))
	handleBoth("/api/v1/auth/login", authPublic(handlers.APIV1AuthLogin))
	handleBoth("/api/v1/auth/logout", handlers.APIV1AuthLogout)
	handleBoth("/api/v1/auth/forgot-password", utils.RateLimitMiddleware(5, 0.05, 900, utils.KeyByIP)(handlers.APIV1ForgotPassword))
	handleBoth("/api/v1/auth/reset-password", handlers.APIV1ResetPasswordRouter)
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
	handleBoth("/api/v1/import", v1(handlers.APIV1ImportRouter))
	handleBoth("/api/v1/import/", v1(handlers.APIV1ImportRouter))
	handleBoth("/api/v1/invites", utils.InviteAPIChain(handlers.APIV1InvitesRouter))
	handleBoth("/api/v1/invites/", utils.InviteAPIChain(handlers.APIV1InvitesRouter))
	handleBoth("/api/v1/admin/settings", utils.AdminAPIChain(handlers.APIV1AdminSettings))
	handleBoth("/api/v1/admin/users", utils.AdminAPIChain(handlers.APIV1AdminUsersRouter))
	handleBoth("/api/v1/admin/users/", utils.AdminAPIChain(handlers.APIV1AdminUsersRouter))
	handleBoth("/api/v1/announcements/dismiss", utils.AuthSessionChain(handlers.APIV1DismissAnnouncement))

	handleBoth("/cal/", handlers.CalendarFeedHandler)
}

func registerFullModeRoutes() {
	handleBoth("/favicon.ico", serveFavicon)
	handleBoth("/changelog", handlers.ChangelogHandler)
	handleBoth("/openapi.yaml", handlers.OpenAPISpecHandler)
	handleBoth("/documentation/api/v1", documentationAPIV1Redirect)

	// Aliases that are not Vue routes (SPA catch-all serves real routes directly).
	for from, to := range map[string]string{
		"/signup":          "/register",
		"/profile":         "/settings",
		"/password-reset":  "/reset-password",
		"/createinvite":    "/invites",
		"/calendar":        "/settings",
	} {
		handleBoth(from, spaAliasRedirect(to))
	}
}

func spaAliasRedirect(path string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		target := utils.PublicPath(path)
		if q := r.URL.RawQuery; q != "" {
			target += "?" + q
		}
		http.Redirect(w, r, target, http.StatusTemporaryRedirect)
	}
}

// documentationAPIV1Redirect sends the legacy docs URL to the SPA API reference page.
// The OpenAPI contract remains at /openapi.yaml.
func documentationAPIV1Redirect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	target := utils.PublicPath("/docs/api/v1")
	if q := r.URL.RawQuery; q != "" {
		target += "?" + q
	}
	http.Redirect(w, r, target, http.StatusTemporaryRedirect)
}
