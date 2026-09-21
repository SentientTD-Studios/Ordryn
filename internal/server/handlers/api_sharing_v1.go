package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"GoTodo/internal/domain"
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
)

type apiProjectMemberJSON struct {
	UserID    int    `json:"user_id"`
	Email     string `json:"email"`
	UserName  string `json:"user_name"`
	Role      string `json:"role"`
	CreatedAt string `json:"created_at"`
}

type apiProjectInviteJSON struct {
	ID              int    `json:"id"`
	ProjectID       int    `json:"project_id"`
	Email           string `json:"email"`
	UserName        string `json:"user_name,omitempty"`
	Role            string `json:"role"`
	ExpiresAt       string `json:"expires_at"`
	CreatedAt       string `json:"created_at"`
	ProjectName     string `json:"project_name,omitempty"`
	InviterEmail    string `json:"inviter_email,omitempty"`
	InviterUserName string `json:"inviter_user_name,omitempty"`
}

type apiShareLinkJSON struct {
	ID        int     `json:"id"`
	Token     string  `json:"token"`
	URL       string  `json:"url"`
	ScopeType string  `json:"scope_type"`
	ScopeID   int     `json:"scope_id"`
	ExpiresAt *string `json:"expires_at,omitempty"`
	CreatedAt string  `json:"created_at"`
}

type apiProjectEventJSON struct {
	ID            int                    `json:"id"`
	ProjectID     int                    `json:"project_id"`
	ActorUserID   int                    `json:"actor_user_id"`
	ActorEmail    string                 `json:"actor_email,omitempty"`
	ActorUserName string                 `json:"actor_user_name,omitempty"`
	EventType     string                 `json:"event_type"`
	Source        string                 `json:"source"` // project | task
	TaskID        *int                   `json:"task_id,omitempty"`
	Label         string                 `json:"label"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt     string                 `json:"created_at"`
}

type apiInviteCreateRequest struct {
	Username string `json:"username"`
	Role     string `json:"role"`
}

type apiMemberPatchRequest struct {
	Role string `json:"role"`
}

type apiShareLinkCreateRequest struct {
	ScopeType string  `json:"scope_type"`
	ScopeID   int     `json:"scope_id"`
	ExpiresAt *string `json:"expires_at"`
}

func formatRFC3339(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

func optionalRFC3339(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := formatRFC3339(*t)
	return &s
}

func shareLinkURL(r *http.Request, token string) string {
	return utils.AbsoluteURLForRequest(r, "/s/"+token)
}

func projectInviteToJSON(inv storage.ProjectInvite) apiProjectInviteJSON {
	return apiProjectInviteJSON{
		ID:              inv.ID,
		ProjectID:       inv.ProjectID,
		Email:           inv.Email,
		UserName:        inv.UserName,
		Role:            inv.Role,
		ExpiresAt:       formatRFC3339(inv.ExpiresAt),
		CreatedAt:       formatRFC3339(inv.CreatedAt),
		ProjectName:     inv.ProjectName,
		InviterEmail:    inv.InviterEmail,
		InviterUserName: inv.InviterUserName,
	}
}

func shareLinkToJSON(r *http.Request, link storage.ShareLink) apiShareLinkJSON {
	return apiShareLinkJSON{
		ID:        link.ID,
		Token:     link.Token,
		URL:       shareLinkURL(r, link.Token),
		ScopeType: link.ScopeType,
		ScopeID:   link.ScopeID,
		ExpiresAt: optionalRFC3339(link.ExpiresAt),
		CreatedAt: formatRFC3339(link.CreatedAt),
	}
}

// handleProjectSubResource routes /api/v2/projects/{id}/members|invites|events...
// Returns true if the request was handled.
func handleProjectSubResource(w http.ResponseWriter, r *http.Request, sub string) bool {
	parts := strings.Split(sub, "/")
	if len(parts) < 2 {
		return false
	}
	projectID, err := strconv.Atoi(parts[0])
	if err != nil || projectID <= 0 {
		return false
	}
	switch parts[1] {
	case "members":
		apiV1ProjectMembers(w, r, projectID, parts[2:])
		return true
	case "invites":
		apiV1ProjectInvites(w, r, projectID, parts[2:])
		return true
	case "events":
		if len(parts) == 2 && r.Method == http.MethodGet {
			apiV1ProjectEvents(w, r, projectID)
			return true
		}
	case "statuses":
		handleProjectStatusesResource(w, r, projectID, parts[2:])
		return true
	case "sprints":
		handleProjectSprintsResource(w, r, projectID, parts[2:])
		return true
	case "github":
		if len(parts) == 2 {
			apiV1ProjectGitHub(w, r, projectID)
			return true
		}
	case "extensions":
		apiV1ProjectExtensions(w, r, projectID, parts[2:])
		return true
	case "inbound":
		if len(parts) == 2 {
			apiV1ProjectInbound(w, r, projectID)
			return true
		}
	case "custom-fields":
		if len(parts) != 2 {
			return false
		}
		apiV1ProjectCustomFields(w, r, projectID)
		return true
	case "archive":
		if len(parts) != 2 {
			return false
		}
		if r.Method != http.MethodPost {
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
			return true
		}
		apiV1ArchiveProject(w, r, projectID)
		return true
	case "restore":
		if len(parts) != 2 {
			return false
		}
		if r.Method != http.MethodPost {
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
			return true
		}
		apiV1RestoreProject(w, r, projectID)
		return true
	}
	return false
}

func apiV1ProjectMembers(w http.ResponseWriter, r *http.Request, projectID int, rest []string) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	proj, err := storage.GetAccessibleProjectByID(projectID, userID)
	if err != nil {
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Project not found.")
		return
	}

	if len(rest) == 0 {
		if r.Method != http.MethodGet {
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
			return
		}
		members, err := storage.ListProjectMembers(projectID)
		if err != nil {
			utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to list members.")
			return
		}
		out := make([]apiProjectMemberJSON, 0, len(members))
		for _, m := range members {
			out = append(out, apiProjectMemberJSON{
				UserID:    m.UserID,
				Email:     m.Email,
				UserName:  m.UserName,
				Role:      m.Role,
				CreatedAt: formatRFC3339(m.CreatedAt),
			})
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		json.NewEncoder(w).Encode(out)
		return
	}

	memberID, err := strconv.Atoi(rest[0])
	if err != nil || memberID <= 0 || len(rest) != 1 {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid member id.")
		return
	}
	_ = proj

	switch r.Method {
	case http.MethodPatch:
		var req apiMemberPatchRequest
		if err := decodeJSONBody(r, &req); err != nil {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
			return
		}
		if err := domain.UpdateProjectMemberRole(r.Context(), userID, projectID, memberID, req.Role); err != nil {
			writeSharingDomainError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case http.MethodDelete:
		if err := domain.RemoveProjectMember(r.Context(), userID, projectID, memberID); err != nil {
			writeSharingDomainError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
	}
}

func apiV1ProjectInvites(w http.ResponseWriter, r *http.Request, projectID int, rest []string) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	proj, err := storage.GetAccessibleProjectByID(projectID, userID)
	if err != nil {
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Project not found.")
		return
	}

	if len(rest) == 0 {
		switch r.Method {
		case http.MethodGet:
			invites, err := storage.ListProjectInvites(projectID)
			if err != nil {
				utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to list invites.")
				return
			}
			out := make([]apiProjectInviteJSON, 0, len(invites))
			for _, inv := range invites {
				out = append(out, projectInviteToJSON(inv))
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			json.NewEncoder(w).Encode(out)
		case http.MethodPost:
			var req apiInviteCreateRequest
			if err := decodeJSONBody(r, &req); err != nil {
				utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
				return
			}
			inv, err := domain.InviteToProject(r.Context(), userID, projectID, req.Username, req.Role)
			if err != nil {
				writeSharingDomainError(w, err)
				return
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(projectInviteToJSON(*inv))
		default:
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		}
		return
	}

	inviteID, err := strconv.Atoi(rest[0])
	if err != nil || inviteID <= 0 || len(rest) != 1 {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid invite id.")
		return
	}
	if r.Method != http.MethodDelete {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	if !storage.RoleCanManage(proj.Role) {
		utils.APIJSONError(w, http.StatusForbidden, "forbidden", "Only the owner can revoke invites.")
		return
	}
	if err := storage.DeleteProjectInvite(inviteID, projectID); err != nil {
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Invite not found.")
		return
	}
	_ = storage.LogProjectEvent(projectID, userID, "invite_revoked", map[string]interface{}{"invite_id": inviteID})
	w.WriteHeader(http.StatusNoContent)
}

func apiV1ProjectEvents(w http.ResponseWriter, r *http.Request, projectID int) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	proj, err := storage.GetAccessibleProjectByID(projectID, userID)
	if err != nil {
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Project not found.")
		return
	}
	_ = proj

	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}

	projectEvents, err := storage.GetProjectEvents(projectID, limit)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to list events.")
		return
	}

	out := make([]apiProjectEventJSON, 0, len(projectEvents))
	for _, ev := range projectEvents {
		out = append(out, apiProjectEventJSON{
			ID:            ev.ID,
			ProjectID:     ev.ProjectID,
			ActorUserID:   ev.ActorUserID,
			ActorEmail:    ev.ActorEmail,
			ActorUserName: ev.ActorUserName,
			EventType:     ev.EventType,
			Source:        "project",
			Label:         formatProjectEventLabel(ev.EventType, ev.Metadata),
			Metadata:      ev.Metadata,
			CreatedAt:     formatRFC3339(ev.CreatedAt),
		})
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(out)
}

func apiV1ArchiveProject(w http.ResponseWriter, r *http.Request, projectID int) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	project, err := domain.ArchiveProject(r.Context(), userID, projectID)
	if err != nil {
		writeWorkflowDomainError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(projectStorageToAPIJSON(project, storage.RoleOwner))
}

func apiV1RestoreProject(w http.ResponseWriter, r *http.Request, projectID int) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	project, err := domain.RestoreProject(r.Context(), userID, projectID)
	if err != nil {
		writeWorkflowDomainError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(projectStorageToAPIJSON(project, storage.RoleOwner))
}

func formatProjectEventLabel(eventType string, metadata map[string]interface{}) string {
	name := metadataString(metadata, "name")
	mode := metadataString(metadata, "mode")
	switch eventType {
	case "invited":
		return "Invited member"
	case "accepted":
		return "Invite accepted"
	case "role_changed":
		return "Role changed"
	case "removed":
		return "Member removed"
	case "left":
		return "Member left"
	case "link_created":
		return "Share link created"
	case "link_revoked":
		return "Share link revoked"
	case "invite_revoked":
		return "Invite revoked"
	case "status_added":
		if name != "" {
			return "Status added · " + name
		}
		return "Status added"
	case "status_updated":
		if name != "" {
			return "Status updated · " + name
		}
		return "Status updated"
	case "status_deleted":
		if name != "" {
			return "Status deleted · " + name
		}
		return "Status deleted"
	case "sprint_added":
		if metadataBool(metadata, "auto") {
			if name != "" {
				return "Sprint added automatically · " + name
			}
			return "Sprint added automatically"
		}
		if name != "" {
			return "Sprint added · " + name
		}
		return "Sprint added"
	case "sprint_updated":
		if name != "" {
			return "Sprint updated · " + name
		}
		return "Sprint updated"
	case "sprint_deleted":
		if name != "" {
			return "Sprint deleted · " + name
		}
		return "Sprint deleted"
	case "workflow_changed":
		if mode != "" {
			return "Workflow set to " + mode
		}
		return "Workflow changed"
	case "renamed":
		if name != "" {
			return "Project renamed · " + name
		}
		return "Project renamed"
	case "description_updated":
		return "Description updated"
	case "auto_sprint_settings_updated":
		return "Auto-sprint settings updated"
	case "github_repo_linked":
		if full := metadataString(metadata, "full_name"); full != "" {
			return "GitHub repo linked · " + full
		}
		return "GitHub repo linked"
	case "github_repo_unlinked":
		return "GitHub repo unlinked"
	case "archived":
		return "Project archived"
	case "restored":
		return "Project restored"
	default:
		return eventType
	}
}

func metadataString(metadata map[string]interface{}, key string) string {
	if metadata == nil {
		return ""
	}
	v, ok := metadata[key]
	if !ok || v == nil {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

func metadataBool(metadata map[string]interface{}, key string) bool {
	if metadata == nil {
		return false
	}
	v, ok := metadata[key]
	if !ok || v == nil {
		return false
	}
	b, ok := v.(bool)
	return ok && b
}

// APIV1ProjectInvitesRouter handles /api/v2/project-invites and accept/decline.
func APIV1ProjectInvitesRouter(w http.ResponseWriter, r *http.Request) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	email, err := storage.GetUserEmailByID(userID)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load user.")
		return
	}

	sub := utils.ParseAPIV1Subpath(r, "project-invites")
	if sub == "" {
		if r.Method != http.MethodGet {
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
			return
		}
		invites, err := storage.ListPendingInvitesForEmail(email)
		if err != nil {
			utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to list invites.")
			return
		}
		out := make([]apiProjectInviteJSON, 0, len(invites))
		for _, inv := range invites {
			out = append(out, projectInviteToJSON(inv))
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		json.NewEncoder(w).Encode(out)
		return
	}

	parts := strings.Split(sub, "/")
	if len(parts) != 2 {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid path.")
		return
	}
	inviteID, err := strconv.Atoi(parts[0])
	if err != nil || inviteID <= 0 {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid invite id.")
		return
	}
	if r.Method != http.MethodPost {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	switch parts[1] {
	case "accept":
		if err := domain.AcceptProjectInvite(r.Context(), userID, email, inviteID); err != nil {
			writeSharingDomainError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case "decline":
		if err := domain.DeclineProjectInvite(r.Context(), email, inviteID); err != nil {
			writeSharingDomainError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Not found.")
	}
}

// APIV1ShareLinksRouter handles authenticated share-link CRUD and public view.
func APIV1ShareLinksRouter(w http.ResponseWriter, r *http.Request) {
	sub := utils.ParseAPIV1Subpath(r, "share-links")

	// Public view: /api/v2/share-links/view/{token}
	if strings.HasPrefix(sub, "view/") {
		token := strings.TrimPrefix(sub, "view/")
		if token == "" || strings.Contains(token, "/") {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid token.")
			return
		}
		if r.Method != http.MethodGet {
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
			return
		}
		apiV1ShareLinkView(w, r, token)
		return
	}

	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}

	if sub == "" {
		switch r.Method {
		case http.MethodGet:
			scopeType := r.URL.Query().Get("scope_type")
			scopeID, _ := strconv.Atoi(r.URL.Query().Get("scope_id"))
			if scopeType == "" || scopeID <= 0 {
				utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "scope_type and scope_id are required.")
				return
			}
			links, err := domain.ListShareLinksForUser(r.Context(), userID, scopeType, scopeID)
			if err != nil {
				writeSharingDomainError(w, err)
				return
			}
			out := make([]apiShareLinkJSON, 0, len(links))
			for _, link := range links {
				out = append(out, shareLinkToJSON(r, link))
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			json.NewEncoder(w).Encode(out)
		case http.MethodPost:
			var req apiShareLinkCreateRequest
			if err := decodeJSONBody(r, &req); err != nil {
				utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
				return
			}
			var expiresAt *time.Time
			if req.ExpiresAt != nil && strings.TrimSpace(*req.ExpiresAt) != "" {
				t, err := time.Parse(time.RFC3339, strings.TrimSpace(*req.ExpiresAt))
				if err != nil {
					utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "expires_at must be RFC3339.")
					return
				}
				expiresAt = &t
			}
			link, err := domain.CreateShareLinkForScope(r.Context(), userID, req.ScopeType, req.ScopeID, expiresAt)
			if err != nil {
				writeSharingDomainError(w, err)
				return
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(shareLinkToJSON(r, *link))
		default:
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		}
		return
	}

	linkID, err := strconv.Atoi(sub)
	if err != nil || linkID <= 0 {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid share link id.")
		return
	}
	if r.Method != http.MethodDelete {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	if err := domain.RevokeShareLinkForUser(r.Context(), userID, linkID); err != nil {
		writeSharingDomainError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func apiV1ShareLinkView(w http.ResponseWriter, r *http.Request, token string) {
	_ = r
	link, err := storage.GetActiveShareLinkByToken(token)
	if err != nil {
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Share link not found.")
		return
	}
	tasks, err := storage.ListTasksForShareLink(link.ScopeType, link.ScopeID, link.CreatedBy)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load tasks.")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"scope_type": link.ScopeType,
		"scope_id":   link.ScopeID,
		"tasks":      tasks,
	})
}

// APIV1ShareLinkViewPublic serves GET /api/v2/share-links/view/{token} without auth.
func APIV1ShareLinkViewPublic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	sub := utils.ParseAPIV1Subpath(r, "share-links")
	token := strings.TrimPrefix(sub, "view/")
	token = strings.Trim(token, "/")
	if token == "" || strings.Contains(token, "/") {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid token.")
		return
	}
	apiV1ShareLinkView(w, r, token)
}

func writeSharingDomainError(w http.ResponseWriter, err error) {
	if errors.Is(err, domain.ErrValidation) {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", sharingClientMessage(err, "Invalid request."))
		return
	}
	if errors.Is(err, domain.ErrForbidden) {
		utils.APIJSONError(w, http.StatusForbidden, "forbidden", sharingClientMessage(err, "Forbidden."))
		return
	}
	if errors.Is(err, domain.ErrNotFound) {
		utils.APIJSONError(w, http.StatusNotFound, "not_found", sharingClientMessage(err, "Not found."))
		return
	}
	utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Request failed.")
}

// sharingClientMessage returns the detail after the sentinel prefix, or fallback.
func sharingClientMessage(err error, fallback string) string {
	msg := err.Error()
	for _, prefix := range []string{"validation: ", "not found: ", "forbidden: "} {
		if strings.HasPrefix(msg, prefix) {
			return strings.TrimPrefix(msg, prefix)
		}
	}
	if msg != "" && msg != domain.ErrValidation.Error() && msg != domain.ErrNotFound.Error() && msg != domain.ErrForbidden.Error() {
		return msg
	}
	return fallback
}
