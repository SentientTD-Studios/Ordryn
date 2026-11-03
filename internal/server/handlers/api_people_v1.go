package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"GoTodo/internal/domain"
	"GoTodo/internal/mailer"
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
)

type apiPersonTeamJSON struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Role     string `json:"role"`
	RoleName string `json:"role_name"`
}

type apiPersonJSON struct {
	ID        int                 `json:"id"`
	Email     string              `json:"email"`
	UserName  string              `json:"user_name"`
	FullName  string              `json:"full_name"`
	FirstName string              `json:"first_name"`
	LastName  string              `json:"last_name"`
	Active    bool                `json:"active"`
	RoleID    int                 `json:"role_id"`
	RoleName  string              `json:"role_name"`
	Teams     []apiPersonTeamJSON `json:"teams"`
	CanManage bool                `json:"can_manage"`
	// CanEditName is true when the caller may set this person's first and last name.
	CanEditName bool `json:"can_edit_name"`
	IsOwner     bool `json:"is_owner"`
}

type apiPeopleTeamJSON struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type apiPeopleSiteRoleJSON struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Label string `json:"label"`
}

type apiPeopleListJSON struct {
	People []apiPersonJSON `json:"people"`
	// ManagesEveryone is true for admins and the users:manage permission.
	ManagesEveryone bool `json:"manages_everyone"`
	// CanCreate is false for those who only edit names.
	CanCreate bool `json:"can_create"`
	// NamesRequired is true in company mode, where new accounts need a first and last name.
	NamesRequired bool                    `json:"names_required"`
	Teams         []apiPeopleTeamJSON     `json:"teams"`
	SiteRoles     []apiPeopleSiteRoleJSON `json:"site_roles"`
}

type apiPeopleCreateRequest struct {
	Email      string `json:"email"`
	UserName   string `json:"user_name"`
	FirstName  string `json:"first_name"`
	LastName   string `json:"last_name"`
	TeamID     int    `json:"team_id"`
	SiteRoleID int    `json:"site_role_id"`
}

type apiPersonNameRequest struct {
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
}

// APIV1PeopleRouter handles /api/v2/people: listing, creating, deactivating, and
// reactivating accounts within the caller's people-management reach.
func APIV1PeopleRouter(w http.ResponseWriter, r *http.Request) {
	actorID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	sub := utils.ParseAPIV1Subpath(r, "people")
	if sub == "" {
		switch r.Method {
		case http.MethodGet:
			apiV1PeopleList(w, r, actorID)
		case http.MethodPost:
			apiV1PeopleCreate(w, r, actorID)
		default:
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		}
		return
	}
	parts := strings.Split(sub, "/")
	if len(parts) == 1 {
		apiV1PersonName(w, r, actorID, parts[0])
		return
	}
	if len(parts) != 2 || (parts[1] != "deactivate" && parts[1] != "reactivate") {
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Unknown action.")
		return
	}
	if r.Method != http.MethodPost {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	targetID, err := strconv.Atoi(parts[0])
	if err != nil || targetID <= 0 {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid user id.")
		return
	}
	active := parts[1] == "reactivate"
	if err := domain.SetAccountActiveForUser(r.Context(), actorID, targetID, active); err != nil {
		writePeopleDomainError(w, err)
		return
	}
	event, from, to := "user_deactivated", "active", "deactivated"
	if active {
		event, from, to = "user_reactivated", "deactivated", "active"
	}
	logAdminEvent(r, event, "user", int64(targetID), storage.UserAuditLabel(targetID),
		map[string]interface{}{"from": from, "to": to})
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true, "active": active})
}

func apiV1PeopleList(w http.ResponseWriter, r *http.Request, actorID int) {
	people, access, err := domain.ListPeopleForUser(r.Context(), actorID)
	if err != nil {
		writePeopleDomainError(w, err)
		return
	}
	teams, siteRoles, err := domain.PeopleFormOptions(access)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load teams.")
		return
	}
	out := apiPeopleListJSON{
		People:          make([]apiPersonJSON, 0, len(people)),
		ManagesEveryone: access.Everyone,
		CanCreate:       access.Any(),
		NamesRequired:   storage.IsCompanyMode(),
		Teams:           make([]apiPeopleTeamJSON, 0, len(teams)),
		SiteRoles:       make([]apiPeopleSiteRoleJSON, 0, len(siteRoles)),
	}
	ownerID, _ := storage.SiteOwnerID()
	for _, p := range people {
		pj := apiPersonJSON{
			IsOwner:   p.ID == ownerID,
			ID:        p.ID,
			Email:     p.Email,
			UserName:  p.UserName,
			FullName:  p.FullName,
			FirstName: p.FirstName,
			LastName:  p.LastName,
			Active:    !p.IsBanned,
			RoleID:    p.RoleID,
			RoleName:  p.RoleName,
			Teams:     make([]apiPersonTeamJSON, 0, len(p.Teams)),
			CanManage: domain.CanManageAccount(actorID, access, p.ID),
			// Names only exist in company mode.
			CanEditName: domain.CanEditNames(actorID, access, p.ID),
		}
		for _, t := range p.Teams {
			pj.Teams = append(pj.Teams, apiPersonTeamJSON{ID: t.ID, Name: t.Name, Role: t.Role, RoleName: t.RoleName})
		}
		out.People = append(out.People, pj)
	}
	for _, t := range teams {
		out.Teams = append(out.Teams, apiPeopleTeamJSON{ID: t.ID, Name: t.Name})
	}
	for _, role := range siteRoles {
		out.SiteRoles = append(out.SiteRoles, apiPeopleSiteRoleJSON{ID: role.ID, Name: role.Name, Label: role.Label()})
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(out)
}

func apiV1PeopleCreate(w http.ResponseWriter, r *http.Request, actorID int) {
	var req apiPeopleCreateRequest
	if err := decodeJSONBody(r, &req); err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
		return
	}
	created, err := domain.CreateAccountForUser(r.Context(), actorID, domain.CreateAccountInput{
		Email:      req.Email,
		UserName:   req.UserName,
		FirstName:  req.FirstName,
		LastName:   req.LastName,
		TeamID:     req.TeamID,
		SiteRoleID: req.SiteRoleID,
	})
	if err != nil && (created == nil || created.UserID == 0) {
		writePeopleDomainError(w, err)
		return
	}
	if err != nil {
		// The account exists; report what's missing rather than failing the request.
		fmt.Printf("APIV1People create: %v\n", err)
	}
	email := strings.TrimSpace(req.Email)
	logAdminEvent(r, "user_created", "user", int64(created.UserID), storage.UserAuditLabel(created.UserID),
		map[string]interface{}{"team_id": req.TeamID, "site_role_id": req.SiteRoleID})

	resp := map[string]interface{}{"ok": true, "user_id": created.UserID, "email_sent": false}
	if created.Reset != nil {
		link := utils.AbsoluteURLForRequest(r,
			fmt.Sprintf("/reset-password?token=%s&id=%s", created.Reset.Token, created.Reset.ID))
		if emailNewAccount(email, link) {
			resp["email_sent"] = true
		} else {
			// Without working email, the creator passes the link on themselves.
			resp["set_password_url"] = link
		}
	} else {
		resp["warning"] = "The account was created, but its set-password link could not be made. Use Forgot password to send one."
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(resp)
}

// apiV1PersonName handles PATCH /api/v2/people/{id}: set a person's first and last name.
func apiV1PersonName(w http.ResponseWriter, r *http.Request, actorID int, rawID string) {
	if r.Method != http.MethodPatch {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	targetID, err := strconv.Atoi(rawID)
	if err != nil || targetID <= 0 {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid user id.")
		return
	}
	var req apiPersonNameRequest
	if err := decodeJSONBody(r, &req); err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
		return
	}
	before, _ := storage.GetUserProfileByID(targetID)
	if err := domain.SetPersonNameForUser(r.Context(), actorID, targetID, req.FirstName, req.LastName); err != nil {
		writePeopleDomainError(w, err)
		return
	}
	after, err := storage.GetUserProfileByID(targetID)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load account.")
		return
	}
	if before != nil {
		from := strings.TrimSpace(before.FirstName + " " + before.LastName)
		to := strings.TrimSpace(after.FirstName + " " + after.LastName)
		if from != to {
			logAdminEvent(r, "user_name_changed", "user", int64(targetID), storage.UserAuditLabel(targetID),
				map[string]interface{}{"from": from, "to": to})
		}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok": true, "first_name": after.FirstName, "last_name": after.LastName,
		"full_name": strings.TrimSpace(after.FirstName + " " + after.LastName),
	})
}

// emailNewAccount sends a new account its set-password link and reports whether it went out.
func emailNewAccount(email, link string) bool {
	settings, err := storage.GetSiteSettings()
	if err != nil || settings == nil {
		return false
	}
	cfg := storage.SiteEmailConfig(settings)
	if !mailer.Configured(cfg) {
		return false
	}
	siteName := "GoTodo"
	if strings.TrimSpace(settings.SiteName) != "" {
		siteName = settings.SiteName
	}
	subject := fmt.Sprintf("%s - Your new account", siteName)
	body := fmt.Sprintf(`Hello,

An account has been created for you on %s. Choose your password here:

%s

This link expires in %d days. If it has expired, use "Forgot password" on the sign-in page to get a new one.
`, siteName, link, int(domain.SetPasswordLinkTTL.Hours()/24))
	return mailer.SendEmail(cfg, mailer.TriggerSiteInvite, subject, body, email) == nil
}

func writePeopleDomainError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrForbidden):
		utils.APIJSONError(w, http.StatusForbidden, "forbidden", sharingClientMessage(err, "You can't manage this account."))
	case errors.Is(err, domain.ErrNotFound):
		utils.APIJSONError(w, http.StatusNotFound, "not_found", sharingClientMessage(err, "Not found."))
	case errors.Is(err, domain.ErrConflict):
		utils.APIJSONError(w, http.StatusConflict, "conflict", sharingClientMessage(err, "Conflict."))
	case errors.Is(err, domain.ErrValidation):
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", sharingClientMessage(err, "Invalid request."))
	default:
		fmt.Printf("APIV1People: %v\n", err)
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Internal server error.")
	}
}
