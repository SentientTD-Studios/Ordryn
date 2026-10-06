package domain

import (
	"context"
	"testing"

	"GoTodo/internal/storage"
)

// Owner is the only site role, so tests create the roles they need on the project or
// organization, the same way a manager would. These mirror the old site defaults.
const (
	testRoleEditor    = "editor"
	testRoleViewer    = "viewer"
	testRoleDeveloper = "developer"
	testRoleQA        = "qa"
)

var testRoleDefs = map[string]struct {
	name  string
	perms []string
}{
	testRoleEditor: {"Editor", []string{
		storage.PermTasksCreate, storage.PermTasksEdit, storage.PermTasksDelete, storage.PermTasksArchive,
		storage.PermTasksRestore, storage.PermTasksComplete, storage.PermTasksClaim, storage.PermTasksReorder,
		storage.PermTasksStatus, storage.PermTasksSprint, storage.PermProjectTags, storage.PermTimeWrite,
		storage.PermExtensionsWrite,
	}},
	testRoleViewer: {"Viewer", []string{}},
	testRoleDeveloper: {"Developer", []string{
		storage.PermTasksCreate, storage.PermTasksEdit, storage.PermTasksDelete, storage.PermTasksArchive,
		storage.PermTasksRestore, storage.PermTasksComplete, storage.PermTasksClaim, storage.PermTasksReorder,
		storage.PermTasksStatus, storage.PermTasksSprint, storage.PermProjectTags, storage.PermTimeWrite,
		storage.PermExtensionsWrite,
	}},
	testRoleQA: {"QA", []string{
		storage.PermTasksComplete, storage.PermTasksClaim, storage.PermTasksReorder, storage.PermTasksStatus,
		storage.PermTimeWrite,
	}},
}

// ensureTestProjectRole creates a fixture role where the project resolves roles: on its
// organization when it has one (locked projects only read organization roles), else on the project.
func ensureTestProjectRole(t *testing.T, projectID int, slug string) {
	t.Helper()
	def, ok := testRoleDefs[slug]
	if !ok || storage.ResolveRoleDef(projectID, slug) != nil {
		return
	}
	bind, err := storage.GetProjectOrgBinding(projectID)
	if err != nil {
		t.Fatalf("org binding for project %d: %v", projectID, err)
	}
	if bind != nil && bind.OrganizationID != nil {
		ensureTestOrgRole(t, *bind.OrganizationID, slug)
		return
	}
	pid := projectID
	if _, err := storage.CreateProjectRoleDef(&pid, slug, def.name, "", def.perms, false, 10); err != nil {
		t.Fatalf("create project role %s: %v", slug, err)
	}
}

func ensureTestOrgRole(t *testing.T, orgID int, slug string) {
	t.Helper()
	def, ok := testRoleDefs[slug]
	if !ok {
		return
	}
	if existing, err := storage.GetOrganizationRoleBySlug(orgID, slug); err == nil && existing != nil {
		return
	}
	if _, err := storage.CreateOrganizationRoleDef(orgID, slug, def.name, "", def.perms, 10); err != nil {
		t.Fatalf("create org role %s: %v", slug, err)
	}
}

func upsertTestMember(t *testing.T, projectID, userID int, role string) error {
	t.Helper()
	ensureTestProjectRole(t, projectID, role)
	return storage.UpsertProjectMember(projectID, userID, role)
}

func upsertTestOrgMember(t *testing.T, orgID, userID int, role string) error {
	t.Helper()
	ensureTestOrgRole(t, orgID, role)
	return storage.UpsertOrganizationMember(orgID, userID, role)
}

func inviteToTestProject(t *testing.T, ctx context.Context, actorUserID, projectID int, username, role string) (*storage.ProjectInvite, error) {
	t.Helper()
	ensureTestProjectRole(t, projectID, role)
	return InviteToProject(ctx, actorUserID, projectID, username, role)
}

func inviteToTestOrg(t *testing.T, ctx context.Context, actorUserID, orgID int, username, role string) (*storage.OrganizationInvite, error) {
	t.Helper()
	ensureTestOrgRole(t, orgID, role)
	return InviteToOrganization(ctx, actorUserID, orgID, username, role)
}
