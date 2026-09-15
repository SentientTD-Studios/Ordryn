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
	handleBoth("/api/v1/health", handlers.APIV1Health)
	handleBoth("/api/v1/site", handlers.APIV1Site)

	authPublic := utils.AuthPublicChain
	handleBoth("/api/v1/auth/register", authPublic(handlers.APIV1AuthRegister))
	handleBoth("/api/v1/auth/login", authPublic(handlers.APIV1AuthLogin))
	handleBoth("/api/v1/auth/mfa/verify", authPublic(handlers.APIV1AuthMFAVerify))
	handleBoth("/api/v1/auth/logout", handlers.APIV1AuthLogout)
	handleBoth("/api/v1/auth/username-available", authPublic(handlers.APIV1UsernameAvailable))
	handleBoth("/api/v1/auth/forgot-password", utils.RateLimitMiddleware(5, 0.05, 900, utils.KeyByIP)(handlers.APIV1ForgotPassword))
	handleBoth("/api/v1/join-requests", utils.RateLimitMiddleware(5, 0.05, 900, utils.KeyByIP)(handlers.APIV1JoinRequestsCreate))
	handleBoth("/api/v1/auth/reset-password", handlers.APIV1ResetPasswordRouter)
	handleBoth("/api/v1/me", utils.AuthMeChain(handlers.APIV1Me))
	handleBoth("/api/v1/me/password", utils.AuthSessionChain(handlers.APIV1ChangePassword))
	handleBoth("/api/v1/me/mfa", utils.AuthSessionChain(handlers.APIV1MeMFA))
	handleBoth("/api/v1/me/mfa/setup", utils.AuthSessionChain(handlers.APIV1MeMFASetup))
	handleBoth("/api/v1/me/mfa/enable", utils.AuthSessionChain(handlers.APIV1MeMFAEnable))
	handleBoth("/api/v1/me/mfa/disable", utils.AuthSessionChain(handlers.APIV1MeMFADisable))
	handleBoth("/api/v1/me/mfa/recovery-codes", utils.AuthSessionChain(handlers.APIV1MeMFARecoveryCodes))
	handleBoth("/api/v1/me/username", utils.AuthSessionChain(handlers.APIV1ClaimUsername))
	handleBoth("/api/v1/me/avatar", utils.AuthSessionChain(handlers.APIV1MeAvatar))
	handleBoth("/api/v1/me/github", utils.AuthSessionChain(handlers.APIV1MeGitHub))
	handleBoth("/api/v1/me/github/pat", utils.AuthSessionChain(handlers.APIV1MeGitHubPAT))
	handleBoth("/api/v1/me/github/oauth/start", utils.AuthSessionChain(handlers.APIV1MeGitHubOAuthStart))
	handleBoth("/api/v1/me/extensions", utils.AuthSessionChain(handlers.APIV1MeExtensions))
	handleBoth("/api/v1/me/extensions/", utils.AuthSessionChain(handlers.APIV1MeExtensions))
	handleBoth("/api/v1/auth/github/callback", handlers.APIV1GitHubOAuthCallback)
	handleBoth("/api/v1/webhooks/github", handlers.APIV1GitHubWebhook)
	handleBoth("/api/v1/webhooks/inbound", handlers.APIV1InboundWebhook)
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
	handleBoth("/api/v1/notifications", v1(handlers.APIV1NotificationsRouter))
	handleBoth("/api/v1/notifications/", v1(handlers.APIV1NotificationsRouter))
	handleBoth("/api/v1/users/search", v1(utils.RateLimitMiddleware(20, 0.4, 60, utils.KeyByUser)(handlers.APIV1UsersSearch)))
	handleBoth("/api/v1/projects", v1(handlers.APIV1ProjectsRouter))
	handleBoth("/api/v1/projects/", v1(handlers.APIV1ProjectsRouter))
	handleBoth("/api/v1/project-invites", v1(handlers.APIV1ProjectInvitesRouter))
	handleBoth("/api/v1/project-invites/", v1(handlers.APIV1ProjectInvitesRouter))
	handleBoth("/api/v1/share-links/view/", handlers.APIV1ShareLinkViewPublic)
	handleBoth("/api/v1/share-links", v1(handlers.APIV1ShareLinksRouter))
	handleBoth("/api/v1/share-links/", v1(handlers.APIV1ShareLinksRouter))
	handleBoth("/api/v1/tags", v1(handlers.APIV1TagsRouter))
	handleBoth("/api/v1/tags/", v1(handlers.APIV1TagsRouter))
	handleBoth("/api/v1/saved-views", v1(handlers.APIV1SavedViewsRouter))
	handleBoth("/api/v1/saved-views/", v1(handlers.APIV1SavedViewsRouter))
	handleBoth("/api/v1/dashboard", v1(handlers.APIV1Dashboard))
	handleBoth("/api/v1/events", v1(handlers.APIV1Events))
	handleBoth("/api/v1/calendar", v1(handlers.APIV1CalendarRouter))
	handleBoth("/api/v1/calendar/", v1(handlers.APIV1CalendarRouter))
	handleBoth("/api/v1/export", v1(handlers.APIV1Export))
	handleBoth("/api/v1/import", v1(handlers.APIV1ImportRouter))
	handleBoth("/api/v1/import/", v1(handlers.APIV1ImportRouter))
	handleBoth("/api/v1/images", v1(handlers.APIV1Images))
	handleBoth("/uploads/", handlers.ServeLocalImage)
	handleBoth("/api/v1/invites", utils.InviteAPIChain(handlers.APIV1InvitesRouter))
	handleBoth("/api/v1/invites/", utils.InviteAPIChain(handlers.APIV1InvitesRouter))
	handleBoth("/api/v1/admin/settings", utils.AdminAPIChain(handlers.APIV1AdminSettings))
	handleBoth("/api/v1/admin/image-hosting/test", utils.AdminAPIChain(handlers.APIV1AdminImageHostingTest))
	handleBoth("/api/v1/admin/users", utils.AdminAPIChain(handlers.APIV1AdminUsersRouter))
	handleBoth("/api/v1/admin/users/", utils.AdminAPIChain(handlers.APIV1AdminUsersRouter))
	handleBoth("/api/v1/admin/join-requests", utils.AdminAPIChain(handlers.APIV1AdminJoinRequestsRouter))
	handleBoth("/api/v1/admin/join-requests/", utils.AdminAPIChain(handlers.APIV1AdminJoinRequestsRouter))
	handleBoth("/api/v1/admin/invites", utils.AdminAPIChain(handlers.APIV1AdminInvitesRouter))
	handleBoth("/api/v1/admin/invites/", utils.AdminAPIChain(handlers.APIV1AdminInvitesRouter))
	handleBoth("/api/v1/admin/email-audit", utils.AdminAPIChain(handlers.APIV1AdminEmailAudit))
	handleBoth("/api/v1/admin/comment-audit", utils.AdminAPIChain(handlers.APIV1AdminCommentAuditRouter))
	handleBoth("/api/v1/admin/comment-audit/", utils.AdminAPIChain(handlers.APIV1AdminCommentAuditRouter))
	handleBoth("/api/v1/admin/extensions", utils.AdminAPIChain(handlers.APIV1AdminExtensionsRouter))
	handleBoth("/api/v1/admin/extensions/", utils.AdminAPIChain(handlers.APIV1AdminExtensionsRouter))
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
	target := utils.PublicPath("/docs/api/v1")
	if q := r.URL.RawQuery; q != "" {
		target += "?" + q
	}
	http.Redirect(w, r, target, http.StatusTemporaryRedirect)
}
