package domain

import (
	"testing"

	"GoTodo/internal/storage"
)

func TestSavedViewSharingPermissions(t *testing.T) {
	owner := createSiteRoleUser(t, "svq-owner", []string{storage.SitePermOrgsCreate})
	member := createSiteRoleUser(t, "svq-member", []string{})
	other := createSiteRoleUser(t, "svq-other", []string{})
	curator := createSiteRoleUser(t, "svq-curator", []string{})
	outsider := createSiteRoleUser(t, "svq-outsider", []string{})
	siteManager := createSiteRoleUser(t, "svq-site", []string{storage.SitePermQueriesSite})

	org, err := storage.CreateOrganization(owner, "Query Guild", "")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	if _, err := storage.CreateOrganizationRoleDef(org.ID, "svq-reader", "Reader", "", []string{}, 10); err != nil {
		t.Fatalf("reader role: %v", err)
	}
	if _, err := storage.CreateOrganizationRoleDef(org.ID, "svq-curator", "Curator", "", []string{storage.PermQueriesManage}, 11); err != nil {
		t.Fatalf("curator role: %v", err)
	}
	for _, m := range []struct {
		id   int
		role string
	}{{member, "svq-reader"}, {other, "svq-reader"}, {curator, "svq-curator"}} {
		if err := storage.UpsertOrganizationMember(org.ID, m.id, m.role); err != nil {
			t.Fatalf("add member: %v", err)
		}
	}

	// Any member may share with the organization; outsiders may not.
	for _, tc := range []struct {
		name string
		user int
		want bool
	}{{"owner", owner, true}, {"member", member, true}, {"outsider", outsider, false}} {
		if got := CanShareQueryWithOrg(tc.user, org.ID); got != tc.want {
			t.Errorf("CanShareQueryWithOrg(%s) = %v, want %v", tc.name, got, tc.want)
		}
	}

	// Only managers of the organization's queries edit other people's.
	for _, tc := range []struct {
		name string
		user int
		want bool
	}{{"owner", owner, true}, {"curator", curator, true}, {"member", member, false}, {"outsider", outsider, false}, {"site manager", siteManager, false}} {
		if got := CanManageOrgQueries(tc.user, org.ID); got != tc.want {
			t.Errorf("CanManageOrgQueries(%s) = %v, want %v", tc.name, got, tc.want)
		}
	}

	orgID := org.ID
	orgView := storage.SavedView{UserID: member, Scope: storage.SavedViewScopeOrg, OrganizationID: &orgID}
	for _, tc := range []struct {
		name string
		user int
		want bool
	}{{"author", member, true}, {"other member", other, false}, {"curator", curator, true}, {"org owner", owner, true}} {
		if got := CanEditSavedView(orgView, tc.user); got != tc.want {
			t.Errorf("edit org view as %s = %v, want %v", tc.name, got, tc.want)
		}
	}
	// An author who leaves the organization loses their edit rights.
	if err := storage.RemoveOrganizationMember(org.ID, member); err != nil {
		t.Fatalf("remove member: %v", err)
	}
	if CanEditSavedView(orgView, member) {
		t.Error("an author who left the organization can still edit its query")
	}

	// Site-wide queries need the site permission, even for their author.
	siteView := storage.SavedView{UserID: member, Scope: storage.SavedViewScopeSite}
	if !CanEditSavedView(siteView, siteManager) {
		t.Error("site query manager cannot edit a site query")
	}
	if CanEditSavedView(siteView, member) {
		t.Error("author without queries:site can edit a site query")
	}
	if CanManageSiteQueries(owner) {
		t.Error("orgs:create alone grants site-wide queries")
	}

	personal := storage.SavedView{UserID: member, Scope: storage.SavedViewScopePersonal}
	if !CanEditSavedView(personal, member) || CanEditSavedView(personal, owner) {
		t.Error("personal queries must be editable by their owner only")
	}
}
