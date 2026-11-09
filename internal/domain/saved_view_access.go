package domain

import "GoTodo/internal/storage"

// Who may share and change saved queries. A query is personal (its owner
// only), shared with an organization's members, or shared with everyone on
// the site. Whoever opens a shared query runs it with their own access, so
// sharing never shows anyone a task they could not already open.

// CanShareQueryWithOrg reports whether userID may share a query with orgID:
// any member may. The company organization is not offered; a site-wide
// query covers everyone instead.
func CanShareQueryWithOrg(userID, orgID int) bool {
	if userID <= 0 || orgID <= 0 {
		return false
	}
	org, err := storage.GetOrganizationWithRole(orgID, userID)
	if err != nil || org == nil || org.IsCompany {
		return false
	}
	return org.Role != ""
}

// CanManageOrgQueries reports whether userID may edit or delete any query
// shared with orgID, not only their own. That is admins, people who manage
// the organization, and members whose role has queries:manage (the
// organization role, or the site role on a company site).
func CanManageOrgQueries(userID, orgID int) bool {
	if userID <= 0 || orgID <= 0 {
		return false
	}
	if storage.UserHasSitePermission(userID, storage.SitePermAdmin) {
		return true
	}
	org, err := storage.GetOrganizationWithRole(orgID, userID)
	if err != nil || org == nil || org.Role == "" {
		return false
	}
	if CanManageOrganization(org, userID) {
		return true
	}
	if storage.IsCompanyMode() {
		return storage.UserHasSitePermission(userID, storage.PermQueriesManage)
	}
	return storage.HasOrgPerm(orgID, org.Role, storage.PermQueriesManage)
}

// CanManageSiteQueries reports whether userID may share queries with the
// whole site and edit or delete the ones shared there.
func CanManageSiteQueries(userID int) bool {
	return storage.UserHasSitePermission(userID, storage.SitePermQueriesSite)
}

// CanEditSavedView reports whether userID may change or delete view. The
// caller must already have checked userID can open it.
func CanEditSavedView(view storage.SavedView, userID int) bool {
	switch view.Scope {
	case storage.SavedViewScopeOrg:
		if view.UserID == userID && CanShareQueryWithOrg(userID, view.OrgID()) {
			return true
		}
		return CanManageOrgQueries(userID, view.OrgID())
	case storage.SavedViewScopeSite:
		return CanManageSiteQueries(userID)
	default:
		return view.UserID == userID
	}
}
