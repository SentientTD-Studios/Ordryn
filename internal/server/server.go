package server

import (
	"GoTodo/internal/domain"
	"GoTodo/internal/extensions"
	"GoTodo/internal/hooks"
	"GoTodo/internal/live"
	"GoTodo/internal/mailer"
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

// handleAPI registers a REST path on the canonical /api/v2 prefix and the
// /api/v1 compatibility alias, including BASE_PATH mounts.
func handleAPI(suffix string, fn http.HandlerFunc) {
	handleBoth("/api/v2"+suffix, fn)
	handleBoth("/api/v1"+suffix, fn)
}

func StartServer() error {
	if err := utils.LoadRuntimeConfig(); err != nil {
		return fmt.Errorf("config: %w", err)
	}

	mode := utils.ResolveMode(os.Args[1:])
	utils.SetRuntimeMode(mode)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	addr := fmt.Sprintf(":%s", port)

	if err := utils.InitRedis(); err != nil {
		return fmt.Errorf("redis: %w", err)
	}
	live.Init(utils.RedisClient)

	if err := storage.RunMigrations(); err != nil {
		fmt.Printf("Warning: migrations completed with errors: %v\n", err)
	}

	extensions.Load()
	if err := domain.SyncCustomFieldDefs(); err != nil {
		fmt.Printf("Warning: custom field sync failed: %v\n", err)
	}

	if err := RunBootstrap(); err != nil {
		return fmt.Errorf("bootstrap failed: %w", err)
	}

	mailer.SetAuditor(storage.RecordEmailAudit)
	storage.StartEmailAuditPurgeWorker()
	domain.StartAutoSprintWorker()
	hooks.StartOverdueHookWorker()
	hooks.StartDeliveryWorker()

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
	handleAPI("/health", handlers.APIV1Health)
	handleAPI("/site", handlers.APIV1Site)

	authPublic := utils.AuthPublicChain
	handleAPI("/auth/register", authPublic(handlers.APIV1AuthRegister))
	handleAPI("/auth/login", authPublic(handlers.APIV1AuthLogin))
	handleAPI("/auth/mfa/verify", authPublic(handlers.APIV1AuthMFAVerify))
	handleAPI("/auth/logout", handlers.APIV1AuthLogout)
	handleAPI("/auth/username-available", authPublic(handlers.APIV1UsernameAvailable))
	handleAPI("/auth/forgot-password", utils.RateLimitMiddleware(5, 0.05, 900, utils.KeyByIP)(handlers.APIV1ForgotPassword))
	handleAPI("/join-requests", utils.RateLimitMiddleware(5, 0.05, 900, utils.KeyByIP)(handlers.APIV1JoinRequestsCreate))
	handleAPI("/auth/reset-password", handlers.APIV1ResetPasswordRouter)
	handleAPI("/me", utils.AuthMeChain(handlers.APIV1Me))
	handleAPI("/me/password", utils.AuthSessionChain(handlers.APIV1ChangePassword))
	handleAPI("/me/mfa", utils.AuthSessionChain(handlers.APIV1MeMFA))
	handleAPI("/me/mfa/setup", utils.AuthSessionChain(handlers.APIV1MeMFASetup))
	handleAPI("/me/mfa/enable", utils.AuthSessionChain(handlers.APIV1MeMFAEnable))
	handleAPI("/me/mfa/disable", utils.AuthSessionChain(handlers.APIV1MeMFADisable))
	handleAPI("/me/mfa/recovery-codes", utils.AuthSessionChain(handlers.APIV1MeMFARecoveryCodes))
	handleAPI("/me/username", utils.AuthSessionChain(handlers.APIV1ClaimUsername))
	handleAPI("/me/avatar", utils.AuthSessionChain(handlers.APIV1MeAvatar))
	handleAPI("/me/github", utils.AuthSessionChain(handlers.APIV1MeGitHub))
	handleAPI("/me/github/pat", utils.AuthSessionChain(handlers.APIV1MeGitHubPAT))
	handleAPI("/me/github/oauth/start", utils.AuthSessionChain(handlers.APIV1MeGitHubOAuthStart))
	handleAPI("/me/extensions", utils.AuthSessionChain(handlers.APIV1MeExtensions))
	handleAPI("/me/extensions/", utils.AuthSessionChain(handlers.APIV1MeExtensions))
	handleAPI("/auth/github/callback", handlers.APIV1GitHubOAuthCallback)
	handleAPI("/webhooks/github", handlers.APIV1GitHubWebhook)
	handleAPI("/webhooks/inbound", utils.RateLimitMiddleware(30, 0.5, 60, utils.KeyByIP)(handlers.APIV1InboundWebhook))
	handleAPI("/ext/callback", utils.RateLimitMiddleware(60, 1.0, 60, utils.KeyByIPAndBearer)(handlers.APIV1ExtCallback))
	handleAPI("/api-keys", utils.AuthSessionChain(handlers.APIV1APIKeysRouter))
	handleAPI("/api-keys/", utils.AuthSessionChain(handlers.APIV1APIKeysRouter))

	devicePublic := handlers.DeviceAuthPublicChain
	handleAPI("/auth/device/code", devicePublic(handlers.APIDeviceCode))
	handleAPI("/auth/device/token", devicePublic(handlers.APIDeviceToken))
	handleAPI("/auth/device/status", utils.RequireAPIEnabled(utils.RequireAPIRedis(handlers.APIV1DeviceStatus)))
	handleAPI("/auth/device/approve", utils.AuthSessionChain(handlers.APIV1DeviceApprove))
	handleAPI("/auth/device/deny", utils.AuthSessionChain(handlers.APIV1DeviceDeny))

	v1 := utils.APIChain
	handleAPI("/tasks", v1(handlers.APIV1TasksRouter))
	handleAPI("/tasks/", v1(handlers.APIV1TasksRouter))
	handleAPI("/notifications", v1(handlers.APIV1NotificationsRouter))
	handleAPI("/notifications/", v1(handlers.APIV1NotificationsRouter))
	handleAPI("/users/search", v1(utils.RateLimitMiddleware(20, 0.4, 60, utils.KeyByUser)(handlers.APIV1UsersSearch)))
	handleAPI("/projects", v1(handlers.APIV1ProjectsRouter))
	handleAPI("/projects/", v1(handlers.APIV1ProjectsRouter))
	handleAPI("/project-invites", v1(handlers.APIV1ProjectInvitesRouter))
	handleAPI("/project-invites/", v1(handlers.APIV1ProjectInvitesRouter))
	handleAPI("/share-links/view/", handlers.APIV1ShareLinkViewPublic)
	handleAPI("/share-links", v1(handlers.APIV1ShareLinksRouter))
	handleAPI("/share-links/", v1(handlers.APIV1ShareLinksRouter))
	handleAPI("/tags", v1(handlers.APIV1TagsRouter))
	handleAPI("/tags/", v1(handlers.APIV1TagsRouter))
	handleAPI("/saved-views", v1(handlers.APIV1SavedViewsRouter))
	handleAPI("/saved-views/", v1(handlers.APIV1SavedViewsRouter))
	handleAPI("/dashboard", v1(handlers.APIV1Dashboard))
	handleAPI("/events", v1(handlers.APIV1Events))
	handleAPI("/calendar", v1(handlers.APIV1CalendarRouter))
	handleAPI("/calendar/", v1(handlers.APIV1CalendarRouter))
	handleAPI("/export", v1(handlers.APIV1Export))
	handleAPI("/import", v1(handlers.APIV1ImportRouter))
	handleAPI("/import/", v1(handlers.APIV1ImportRouter))
	handleAPI("/images", v1(handlers.APIV1Images))
	handleBoth("/uploads/", handlers.ServeLocalImage)
	handleAPI("/invites", utils.InviteAPIChain(handlers.APIV1InvitesRouter))
	handleAPI("/invites/", utils.InviteAPIChain(handlers.APIV1InvitesRouter))
	handleAPI("/admin/settings", utils.AdminAPIChain(handlers.APIV1AdminSettings))
	handleAPI("/admin/image-hosting/test", utils.AdminAPIChain(handlers.APIV1AdminImageHostingTest))
	handleAPI("/admin/users", utils.AdminAPIChain(handlers.APIV1AdminUsersRouter))
	handleAPI("/admin/users/", utils.AdminAPIChain(handlers.APIV1AdminUsersRouter))
	handleAPI("/admin/join-requests", utils.AdminAPIChain(handlers.APIV1AdminJoinRequestsRouter))
	handleAPI("/admin/join-requests/", utils.AdminAPIChain(handlers.APIV1AdminJoinRequestsRouter))
	handleAPI("/admin/invites", utils.AdminAPIChain(handlers.APIV1AdminInvitesRouter))
	handleAPI("/admin/invites/", utils.AdminAPIChain(handlers.APIV1AdminInvitesRouter))
	handleAPI("/admin/email-audit", utils.AdminAPIChain(handlers.APIV1AdminEmailAudit))
	handleAPI("/admin/comment-audit", utils.AdminAPIChain(handlers.APIV1AdminCommentAuditRouter))
	handleAPI("/admin/comment-audit/", utils.AdminAPIChain(handlers.APIV1AdminCommentAuditRouter))
	handleAPI("/admin/extensions", utils.AdminAPIChain(handlers.APIV1AdminExtensionsRouter))
	handleAPI("/admin/extensions/", utils.AdminAPIChain(handlers.APIV1AdminExtensionsRouter))
	handleAPI("/extensions/", v1(handlers.APIV1ExtensionsStatic))
	handleAPI("/announcements/dismiss", utils.AuthSessionChain(handlers.APIV1DismissAnnouncement))

	handleBoth("/cal/", handlers.CalendarFeedHandler)
}

func registerFullModeRoutes() {
	handleBoth("/favicon.ico", serveFavicon)
	handleBoth("/changelog", handlers.ChangelogHandler)
	handleBoth("/openapi.yaml", handlers.OpenAPISpecHandler)
	handleBoth("/documentation/api/v2", documentationAPIV1Redirect)
	handleBoth("/documentation/api/v1", documentationAPIV1Redirect)

	// Aliases that are not Vue routes (SPA catch-all serves real routes directly).
	for from, to := range map[string]string{
		"/signup":         "/register",
		"/profile":        "/settings",
		"/password-reset": "/reset-password",
		"/createinvite":   "/invites",
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
	target := utils.PublicPath("/docs/api/v2")
	if q := r.URL.RawQuery; q != "" {
		target += "?" + q
	}
	http.Redirect(w, r, target, http.StatusTemporaryRedirect)
}
