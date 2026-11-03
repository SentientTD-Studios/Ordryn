package domain

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"GoTodo/internal/storage"

	"golang.org/x/crypto/bcrypt"
)

// SetPasswordLinkTTL is how long a new account's set-password link stays valid.
const SetPasswordLinkTTL = 7 * 24 * time.Hour

// PeopleAccess is what an account may do with other people's accounts.
//
// Nobody changes the site owner's account. The owner manages everyone else, admins
// included. Admins manage everyone but admins. The users:manage site permission
// manages every account except admins and other people managers. In company mode,
// the team:people site permission manages the members of the teams its holder is on
// (the Company team excluded), except others holding team:people.
type PeopleAccess struct {
	Owner    bool
	Admin    bool
	Everyone bool
	TeamIDs  []int
	// EditNames is people:profile in company mode: set anyone's name within the
	// Everyone reach, without managing their account.
	EditNames bool
}

// Any reports whether the account may manage anyone at all.
func (a PeopleAccess) Any() bool {
	return a.Everyone || len(a.TeamIDs) > 0
}

// SeesPeople reports whether the account gets the People page.
func (a PeopleAccess) SeesPeople() bool {
	return a.Any() || a.EditNames
}

// PeopleAccessFor returns the actor's people-management reach.
func PeopleAccessFor(actorUserID int) (PeopleAccess, error) {
	var a PeopleAccess
	if actorUserID <= 0 || storage.IsUserBannedByID(actorUserID) {
		return a, nil
	}
	a.Admin = storage.UserHasSitePermission(actorUserID, storage.SitePermAdmin)
	a.Owner = a.Admin && storage.IsSiteOwner(actorUserID)
	a.Everyone = a.Admin || storage.UserHasSitePermission(actorUserID, storage.SitePermUsersManage)
	a.EditNames = storage.IsCompanyMode() && storage.UserHasSitePermission(actorUserID, storage.SitePermProfilesEdit)
	if storage.IsCompanyMode() && storage.UserHasSitePermission(actorUserID, storage.SitePermTeamPeople) {
		teams, err := storage.ListUserTeams(actorUserID)
		if err != nil {
			return a, err
		}
		a.TeamIDs = teams
	}
	return a, nil
}

// isPeopleManager reports whether the account holds admin or users:manage, which
// puts it beyond the reach of anyone but an admin.
func isPeopleManager(userID int) bool {
	return storage.UserHasSitePermission(userID, storage.SitePermAdmin) ||
		storage.UserHasSitePermission(userID, storage.SitePermUsersManage)
}

// canManageAccount returns nil when access covers targetUserID.
func canManageAccount(actorUserID int, access PeopleAccess, targetUserID int) error {
	if targetUserID == actorUserID {
		return fmt.Errorf("%w: you can't change your own account here; ask someone else", ErrValidation)
	}
	if storage.IsSystemUser(targetUserID) || storage.IsAgentUser(targetUserID) {
		return fmt.Errorf("%w: agent and automation accounts are managed per project", ErrForbidden)
	}
	if storage.IsSiteOwner(targetUserID) {
		return fmt.Errorf("%w: nobody can change the site owner's account", ErrForbidden)
	}
	if storage.UserHasSitePermission(targetUserID, storage.SitePermAdmin) {
		if access.Owner {
			return nil
		}
		return fmt.Errorf("%w: only the site owner can change an admin's account", ErrForbidden)
	}
	if access.Admin {
		return nil
	}
	if isPeopleManager(targetUserID) {
		return fmt.Errorf("%w: only an admin can change the account of an admin or people manager", ErrForbidden)
	}
	if access.Everyone {
		return nil
	}
	if len(access.TeamIDs) > 0 && storage.UserHasSitePermission(targetUserID, storage.SitePermTeamPeople) {
		return fmt.Errorf("%w: team managers can't change each other's accounts", ErrForbidden)
	}
	for _, teamID := range access.TeamIDs {
		role, err := storage.GetOrganizationRole(teamID, targetUserID)
		if err != nil {
			return err
		}
		if role != "" {
			return nil
		}
	}
	return fmt.Errorf("%w: you can only manage the members of your teams", ErrForbidden)
}

// ListPeopleForUser returns the accounts the actor may manage.
func ListPeopleForUser(ctx context.Context, actorUserID int) ([]storage.Person, PeopleAccess, error) {
	_ = ctx
	access, err := PeopleAccessFor(actorUserID)
	if err != nil {
		return nil, access, err
	}
	if !access.SeesPeople() {
		return nil, access, fmt.Errorf("%w: you don't manage anyone's account", ErrForbidden)
	}
	people, err := storage.ListPeople(access.Everyone || access.EditNames, access.TeamIDs)
	return people, access, err
}

// SetAccountActiveForUser deactivates or reactivates an account. A deactivated
// account can't sign in, and its sessions and API keys stop working.
func SetAccountActiveForUser(ctx context.Context, actorUserID, targetUserID int, active bool) error {
	_ = ctx
	access, err := PeopleAccessFor(actorUserID)
	if err != nil {
		return err
	}
	if !access.Any() {
		return ErrForbidden
	}
	if err := canManageAccount(actorUserID, access, targetUserID); err != nil {
		return err
	}
	if !active && storage.UserHasSitePermission(targetUserID, storage.SitePermAdmin) {
		admins, err := storage.CountActiveAdmins()
		if err != nil {
			return err
		}
		if admins <= 1 {
			return fmt.Errorf("%w: the site needs at least one active admin", ErrValidation)
		}
	}
	if err := storage.SetUserBanned(targetUserID, !active); err != nil {
		return ErrNotFound
	}
	return nil
}

// CreateAccountInput is a new account created by a people manager.
type CreateAccountInput struct {
	Email    string
	UserName string
	// FirstName and LastName are required in company mode.
	FirstName string
	LastName  string
	// TeamID puts the person on a company-mode team; zero means no team.
	TeamID int
	// SiteRoleID zero means the default User role.
	SiteRoleID int
}

// CreatedAccount is the new account and the token for its set-password link.
type CreatedAccount struct {
	UserID int
	Reset  *storage.PasswordReset
}

// CreateAccountForUser creates an account with no usable password and returns a
// set-password token for it. Team managers may only create accounts on their own
// teams, with the default site role and a team role that doesn't manage the team.
func CreateAccountForUser(ctx context.Context, actorUserID int, in CreateAccountInput) (*CreatedAccount, error) {
	_ = ctx
	access, err := PeopleAccessFor(actorUserID)
	if err != nil {
		return nil, err
	}
	if !access.Any() {
		return nil, ErrForbidden
	}

	email := strings.TrimSpace(in.Email)
	if email == "" || !strings.Contains(email, "@") {
		return nil, fmt.Errorf("%w: a valid email address is required", ErrValidation)
	}
	if exists, err := storage.UserExistsByEmail(email); err != nil {
		return nil, err
	} else if exists {
		return nil, fmt.Errorf("%w: an account with this email already exists", ErrConflict)
	}
	first, err := normalizePersonName(in.FirstName, "first name")
	if err != nil {
		return nil, err
	}
	last, err := normalizePersonName(in.LastName, "last name")
	if err != nil {
		return nil, err
	}
	if storage.IsCompanyMode() && (first == "" || last == "") {
		return nil, fmt.Errorf("%w: first and last name are required", ErrValidation)
	}
	userName, available, err := UsernameAvailable(in.UserName, 0)
	if err != nil {
		return nil, err
	}
	if !available {
		return nil, fmt.Errorf("%w: that username is already taken", ErrConflict)
	}

	if in.TeamID > 0 {
		if !storage.IsCompanyMode() {
			return nil, fmt.Errorf("%w: teams are only used in company mode", ErrValidation)
		}
		org, err := storage.GetOrganizationByID(in.TeamID)
		if err != nil || org == nil {
			return nil, fmt.Errorf("%w: team not found", ErrNotFound)
		}
		if org.IsCompany {
			return nil, fmt.Errorf("%w: everyone joins the Company team automatically; choose another team or none", ErrValidation)
		}
		if !access.Everyone && !slices.Contains(access.TeamIDs, in.TeamID) {
			return nil, fmt.Errorf("%w: you can only add people to your teams", ErrForbidden)
		}
	} else if !access.Everyone {
		return nil, fmt.Errorf("%w: choose one of your teams for the new account", ErrValidation)
	}

	roleID, err := storage.GetDefaultRoleID()
	if err != nil {
		return nil, err
	}
	if in.SiteRoleID > 0 && in.SiteRoleID != roleID {
		if !access.Everyone {
			return nil, fmt.Errorf("%w: only admins and people managers can choose a site role", ErrForbidden)
		}
		role, err := storage.GetSiteRole(in.SiteRoleID)
		if err != nil {
			return nil, fmt.Errorf("%w: site role not found", ErrValidation)
		}
		if slices.Contains(role.Permissions, storage.SitePermAdmin) && !access.Owner {
			return nil, fmt.Errorf("%w: only the site owner can create admins", ErrForbidden)
		}
		if slices.Contains(role.Permissions, storage.SitePermUsersManage) && !access.Admin {
			return nil, fmt.Errorf("%w: only an admin can create people managers", ErrForbidden)
		}
		roleID = role.ID
	}

	// Nobody knows this password; the person sets their own from the link.
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(hex.EncodeToString(secret)), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	timezone := "America/New_York"
	if settings, err := storage.GetSiteSettings(); err == nil && settings.DefaultTimezone != "" {
		timezone = settings.DefaultTimezone
	}
	userID, err := storage.RegisterUser(email, string(hash), timezone, userName, roleID, 0)
	if err != nil {
		return nil, err
	}
	if first != "" || last != "" {
		if err := storage.SetUserNames(userID, first, last); err != nil {
			return &CreatedAccount{UserID: userID}, fmt.Errorf("account created, but saving its name failed: %w", err)
		}
	}
	if in.TeamID > 0 {
		if _, err := storage.AddOrganizationMember(in.TeamID, userID, storage.CompanyMemberRole); err != nil {
			return &CreatedAccount{UserID: userID}, fmt.Errorf("account created, but adding it to the team failed: %w", err)
		}
	}
	reset, err := storage.GenerateResetTokenTTL(email, SetPasswordLinkTTL)
	if err != nil {
		return &CreatedAccount{UserID: userID}, fmt.Errorf("account created, but the set-password link failed: %w", err)
	}
	return &CreatedAccount{UserID: userID, Reset: reset}, nil
}

// AddTeamMemberForUser puts an existing account straight onto a company-mode team.
// Everyone already belongs to the company, so there is no invite to accept.
func AddTeamMemberForUser(ctx context.Context, actorUserID, orgID int, rawUsername string) (*storage.OrganizationMember, error) {
	_ = ctx
	org, err := requireOrgManage(orgID, actorUserID)
	if err != nil {
		return nil, err
	}
	if org.IsCompany {
		return nil, errCompanyNoInvite
	}
	name, err := PrepareUsername(rawUsername)
	if err != nil {
		return nil, err
	}
	user, err := storage.GetUserByUsername(name)
	if err != nil || user == nil {
		return nil, fmt.Errorf("%w: username not found", ErrNotFound)
	}
	if storage.IsAgentUser(user.ID) {
		return nil, fmt.Errorf("%w: agent accounts are managed per project", ErrValidation)
	}
	existing, err := storage.GetOrganizationRole(orgID, user.ID)
	if err != nil {
		return nil, err
	}
	if existing != "" {
		return nil, fmt.Errorf("%w: %s is already on this team", ErrValidation, user.UserName)
	}
	if _, err := storage.AddOrganizationMember(orgID, user.ID, storage.CompanyMemberRole); err != nil {
		return nil, err
	}
	members, err := storage.ListOrganizationMembers(orgID)
	if err != nil {
		return nil, err
	}
	for i := range members {
		if members[i].UserID == user.ID {
			return &members[i], nil
		}
	}
	return nil, errors.New("added team member not found")
}

// CanManageAccount reports whether access covers targetUserID.
func CanManageAccount(actorUserID int, access PeopleAccess, targetUserID int) bool {
	return canManageAccount(actorUserID, access, targetUserID) == nil
}

// PeopleTeamOption is a team the actor can add a new account to.
type PeopleTeamOption struct {
	storage.TeamRef
}

// PeopleFormOptions returns the teams and site roles the actor may use for a new account.
func PeopleFormOptions(access PeopleAccess) ([]PeopleTeamOption, []storage.SiteRole, error) {
	teams, err := storage.ListTeams()
	if err != nil {
		return nil, nil, err
	}
	outTeams := []PeopleTeamOption{}
	if storage.IsCompanyMode() {
		for _, t := range teams {
			if access.Everyone || slices.Contains(access.TeamIDs, t.ID) {
				outTeams = append(outTeams, PeopleTeamOption{TeamRef: t})
			}
		}
	}

	outRoles := []storage.SiteRole{}
	if access.Everyone {
		roles, err := storage.ListSiteRoles()
		if err != nil {
			return nil, nil, err
		}
		for _, role := range roles {
			if slices.Contains(role.Permissions, storage.SitePermAdmin) && !access.Owner {
				continue
			}
			if slices.Contains(role.Permissions, storage.SitePermUsersManage) && !access.Admin {
				continue
			}
			outRoles = append(outRoles, role)
		}
	}
	return outTeams, outRoles, nil
}

// normalizePersonName trims a first or last name and checks its length.
func normalizePersonName(raw, field string) (string, error) {
	name := strings.Join(strings.Fields(raw), " ")
	if len(name) > storage.MaxPersonNameLen {
		return "", fmt.Errorf("%w: %s must be %d characters or less", ErrValidation, field, storage.MaxPersonNameLen)
	}
	return name, nil
}

// canEditNames reports whether the actor may set targetUserID's name. Names are
// managed like a company directory: people can't change their own unless they hold
// people:profile (admins do). Otherwise the target must be within the actor's people
// reach, where people:profile counts as reaching everyone.
func canEditNames(actorUserID int, access PeopleAccess, targetUserID int) error {
	if !storage.IsCompanyMode() {
		return fmt.Errorf("%w: names are managed in company mode only", ErrValidation)
	}
	if targetUserID == actorUserID {
		if access.EditNames {
			return nil
		}
		return fmt.Errorf("%w: ask someone who manages people to change your name", ErrForbidden)
	}
	if access.EditNames {
		access.Everyone = true
	}
	if !access.Any() {
		return fmt.Errorf("%w: you can't change people's names", ErrForbidden)
	}
	return canManageAccount(actorUserID, access, targetUserID)
}

// SetPersonNameForUser sets someone's first and last name.
func SetPersonNameForUser(ctx context.Context, actorUserID, targetUserID int, first, last string) error {
	_ = ctx
	access, err := PeopleAccessFor(actorUserID)
	if err != nil {
		return err
	}
	if err := canEditNames(actorUserID, access, targetUserID); err != nil {
		return err
	}
	first, err = normalizePersonName(first, "first name")
	if err != nil {
		return err
	}
	last, err = normalizePersonName(last, "last name")
	if err != nil {
		return err
	}
	if first == "" || last == "" {
		return fmt.Errorf("%w: first and last name are required", ErrValidation)
	}
	if err := storage.SetUserNames(targetUserID, first, last); err != nil {
		return ErrNotFound
	}
	return nil
}

// CanEditNames reports whether access lets the actor set targetUserID's name.
func CanEditNames(actorUserID int, access PeopleAccess, targetUserID int) bool {
	return canEditNames(actorUserID, access, targetUserID) == nil
}
