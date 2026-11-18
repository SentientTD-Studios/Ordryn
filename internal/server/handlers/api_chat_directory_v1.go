package handlers

import (
	"errors"
	"net/http"

	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
)

// GET /api/v2/chat/directory: the read-only user directory for Ordryn Chat
// directory sync. A registered chat server authenticates with HTTP Basic
// (client ID : client secret), and needs "Directory sync" allowed under
// Admin → Chat servers. These credentials reach nothing else.

type apiChatDirectoryUserJSON struct {
	ID       int    `json:"id"`
	Email    string `json:"email"`
	UserName string `json:"user_name"`
	FullName string `json:"full_name"`
	IsBanned bool   `json:"is_banned"`
	RoleID   int    `json:"role_id"`
	RoleName string `json:"role_name"`
}

type apiChatDirectoryRoleJSON struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Label   string `json:"label"`
	BuiltIn bool   `json:"built_in"`
}

type apiChatDirectoryJSON struct {
	Users                   []apiChatDirectoryUserJSON  `json:"users"`
	SiteRoles               []apiChatDirectoryRoleJSON  `json:"site_roles"`
	OrganizationMemberships []apiAdminOrgMembershipJSON `json:"organization_memberships"`
}

// ChatDirectoryChain rate-limits the directory endpoint per IP. It sits
// outside the session/API-key chains: chat credentials aren't user keys.
func ChatDirectoryChain(handler http.HandlerFunc) http.HandlerFunc {
	return utils.RequireAPIEnabled(
		utils.RequireAPIRedis(
			utils.RateLimitMiddleware(30, 0.5, 120, utils.KeyByIP)(handler),
		),
	)
}

// APIV2ChatDirectory serves GET /api/v2/chat/directory.
func APIV2ChatDirectory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	clientID, secret, ok := r.BasicAuth()
	if !ok {
		w.Header().Set("WWW-Authenticate", `Basic realm="Ordryn Chat"`)
		utils.APIJSONError(w, http.StatusUnauthorized, "invalid_client", "Send the chat server's client ID and secret (HTTP Basic).")
		return
	}
	cs, err := storage.AuthenticateChatServer(clientID, secret)
	if errors.Is(err, storage.ErrChatClientRejected) {
		utils.APIJSONError(w, http.StatusUnauthorized, "invalid_client",
			"This chat server isn't allowed to use Ordryn. An Ordryn admin can add it under Admin → Chat servers.")
		return
	}
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to check the chat server.")
		return
	}
	if !cs.DirectorySync {
		utils.APIJSONError(w, http.StatusForbidden, "directory_sync_disabled",
			"Directory sync isn't allowed for this chat server. An Ordryn admin can turn it on under Admin → Chat servers.")
		return
	}

	users, err := storage.ListUsers()
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to list users.")
		return
	}
	out := apiChatDirectoryJSON{
		Users:                   make([]apiChatDirectoryUserJSON, 0, len(users)),
		SiteRoles:               []apiChatDirectoryRoleJSON{},
		OrganizationMemberships: []apiAdminOrgMembershipJSON{},
	}
	for _, u := range users {
		if storage.IsSystemUser(u.ID) {
			continue // @automation can't sign in to anything
		}
		out.Users = append(out.Users, apiChatDirectoryUserJSON{
			ID: u.ID, Email: u.Email, UserName: u.UserName, FullName: u.FullName,
			IsBanned: u.IsBanned, RoleID: u.RoleID, RoleName: u.RoleName,
		})
	}

	roles, err := storage.ListSiteRoles()
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to list site roles.")
		return
	}
	for _, role := range roles {
		out.SiteRoles = append(out.SiteRoles, apiChatDirectoryRoleJSON{ID: role.ID, Name: role.Name, Label: role.Label(), BuiltIn: role.BuiltIn()})
	}

	memberships, err := listOrgMembershipsJSON()
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to list organization memberships.")
		return
	}
	out.OrganizationMemberships = memberships
	writeJSONStatus(w, http.StatusOK, out)
}
