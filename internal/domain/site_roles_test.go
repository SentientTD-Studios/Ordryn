package domain

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"

	"GoTodo/internal/storage"
)

var siteRoleTestSeq atomic.Int64

// ensureSiteRoleID upserts a site role by name and returns its id.
func ensureSiteRoleID(t *testing.T, name string, perms []string) int {
	t.Helper()
	pool, err := storage.OpenDatabase()
	if err != nil {
		t.Fatal(err)
	}
	defer storage.CloseDatabase(pool)
	var id int
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO roles (name, permissions) VALUES ($1, $2)
		 ON CONFLICT (name) DO UPDATE SET permissions = EXCLUDED.permissions
		 RETURNING id`, name, perms).Scan(&id); err != nil {
		t.Fatalf("ensure role %s: %v", name, err)
	}
	return id
}

// newUserWithSiteRole creates an account on the given role.
func newUserWithSiteRole(t *testing.T, roleID int) int {
	t.Helper()
	pool, err := storage.OpenDatabase()
	if err != nil {
		t.Fatal(err)
	}
	defer storage.CloseDatabase(pool)
	email := fmt.Sprintf("site-role-%d@example.com", siteRoleTestSeq.Add(1))
	var id int
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO users (email, password, role_id) VALUES ($1, 'x', $2) RETURNING id`, email, roleID).Scan(&id); err != nil {
		t.Fatalf("create user: %v", err)
	}
	return id
}

func newSiteAdmin(t *testing.T) int {
	t.Helper()
	return newUserWithSiteRole(t, ensureSiteRoleID(t, storage.AdminRoleName, []string{storage.SitePermAdmin}))
}

func TestSiteRolesRequireAdmin(t *testing.T) {
	ctx := context.Background()
	if _, _, err := ListSiteRolesForAdmin(ctx, 2); !errors.Is(err, ErrForbidden) {
		t.Fatalf("list as non-admin: err=%v, want ErrForbidden", err)
	}
	if _, err := CreateSiteRoleForAdmin(ctx, 2, "Sneaky", nil); !errors.Is(err, ErrForbidden) {
		t.Fatalf("create as non-admin: err=%v, want ErrForbidden", err)
	}
	if _, _, err := SetUserSiteRoleForAdmin(ctx, 2, 3, 1); !errors.Is(err, ErrForbidden) {
		t.Fatalf("assign as non-admin: err=%v, want ErrForbidden", err)
	}
}

func TestCreateSiteRoleValidation(t *testing.T) {
	ctx := context.Background()
	admin := newSiteAdmin(t)

	role, err := CreateSiteRoleForAdmin(ctx, admin, "  Project Lead  ", []string{storage.SitePermProjectsCreate, storage.SitePermProjectsCreate})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if role.Name != "Project Lead" || len(role.Permissions) != 1 || role.Permissions[0] != storage.SitePermProjectsCreate {
		t.Fatalf("created role = %+v, want trimmed name and deduped perms", role)
	}
	if _, err := CreateSiteRoleForAdmin(ctx, admin, "project lead", nil); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate name (case-insensitive): err=%v, want ErrConflict", err)
	}
	if _, err := CreateSiteRoleForAdmin(ctx, admin, "ADMIN", nil); !errors.Is(err, ErrConflict) {
		t.Fatalf("reserved name: err=%v, want ErrConflict", err)
	}
	if _, err := CreateSiteRoleForAdmin(ctx, admin, "Escalate", []string{storage.SitePermAdmin}); !errors.Is(err, ErrValidation) {
		t.Fatalf("admin permission on custom role: err=%v, want ErrValidation", err)
	}
	if _, err := CreateSiteRoleForAdmin(ctx, admin, "  ", nil); !errors.Is(err, ErrValidation) {
		t.Fatalf("blank name: err=%v, want ErrValidation", err)
	}
}

func TestUpdateSiteRoleRules(t *testing.T) {
	ctx := context.Background()
	admin := newSiteAdmin(t)
	adminRoleID := ensureSiteRoleID(t, storage.AdminRoleName, []string{storage.SitePermAdmin})

	perms := []string{}
	if _, err := UpdateSiteRoleForAdmin(ctx, admin, adminRoleID, nil, &perms); !errors.Is(err, ErrValidation) {
		t.Fatalf("edit admin role: err=%v, want ErrValidation", err)
	}
	userRoleID, err := storage.GetRoleIDByName(storage.MemberRoleName)
	if err != nil {
		t.Fatal(err)
	}
	newName := "Member"
	if _, err := UpdateSiteRoleForAdmin(ctx, admin, userRoleID, &newName, nil); !errors.Is(err, ErrValidation) {
		t.Fatalf("rename user role: err=%v, want ErrValidation", err)
	}

	// Permissions outside the catalog (legacy or admin) survive a catalog edit.
	legacyID := ensureSiteRoleID(t, "Legacy Viewer", []string{"viewall", storage.SitePermOrgsCreate})
	next := []string{storage.SitePermProjectsCreate}
	updated, err := UpdateSiteRoleForAdmin(ctx, admin, legacyID, nil, &next)
	if err != nil {
		t.Fatalf("update perms: %v", err)
	}
	got := map[string]bool{}
	for _, p := range updated.Permissions {
		got[p] = true
	}
	if !got["viewall"] || !got[storage.SitePermProjectsCreate] || got[storage.SitePermOrgsCreate] {
		t.Fatalf("permissions = %v, want viewall kept, projects:create added, orgs:create removed", updated.Permissions)
	}
}

func TestAssignSiteRoleTakesEffectImmediately(t *testing.T) {
	ctx := context.Background()
	admin := newSiteAdmin(t)
	userRoleID, err := storage.GetRoleIDByName(storage.MemberRoleName)
	if err != nil {
		t.Fatal(err)
	}
	restricted := ensureSiteRoleID(t, "No Create", []string{})
	target := newUserWithSiteRole(t, restricted)
	if storage.UserHasSitePermission(target, storage.SitePermOrgsCreate) {
		t.Fatal("target should start without orgs:create")
	}

	lead, err := CreateSiteRoleForAdmin(ctx, admin, "Org Lead", []string{storage.SitePermOrgsCreate})
	if err != nil {
		t.Fatal(err)
	}
	from, to, err := SetUserSiteRoleForAdmin(ctx, admin, target, lead.ID)
	if err != nil {
		t.Fatalf("assign: %v", err)
	}
	if from.ID != restricted || to.ID != lead.ID {
		t.Fatalf("from=%d to=%d, want %d -> %d", from.ID, to.ID, restricted, lead.ID)
	}
	if !storage.UserHasSitePermission(target, storage.SitePermOrgsCreate) {
		t.Fatal("new role's permission should apply without a new login")
	}
	if _, err := CreateOrganizationForUser(ctx, target, "Lead Org", ""); err != nil {
		t.Fatalf("create org with granted role: %v", err)
	}

	if _, _, err := SetUserSiteRoleForAdmin(ctx, admin, admin, userRoleID); !errors.Is(err, ErrValidation) {
		t.Fatalf("change own role: err=%v, want ErrValidation", err)
	}
	if _, _, err := SetUserSiteRoleForAdmin(ctx, admin, target, 999999); !errors.Is(err, ErrValidation) {
		t.Fatalf("unknown role: err=%v, want ErrValidation", err)
	}
	if _, _, err := SetUserSiteRoleForAdmin(ctx, admin, 999999, userRoleID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown user: err=%v, want ErrNotFound", err)
	}
}

func TestDeleteSiteRoleMovesUsersToUserRole(t *testing.T) {
	ctx := context.Background()
	admin := newSiteAdmin(t)
	userRoleID, err := storage.GetRoleIDByName(storage.MemberRoleName)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := DeleteSiteRoleForAdmin(ctx, admin, userRoleID); !errors.Is(err, ErrValidation) {
		t.Fatalf("delete built-in: err=%v, want ErrValidation", err)
	}

	role, err := CreateSiteRoleForAdmin(ctx, admin, "Temporary", nil)
	if err != nil {
		t.Fatal(err)
	}
	member := newUserWithSiteRole(t, role.ID)
	deleted, moved, err := DeleteSiteRoleForAdmin(ctx, admin, role.ID)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if deleted.Name != "Temporary" || moved != 1 {
		t.Fatalf("deleted=%q moved=%d, want Temporary/1", deleted.Name, moved)
	}
	roleID, _, err := storage.GetUserSiteRoleID(member)
	if err != nil {
		t.Fatal(err)
	}
	if roleID != userRoleID {
		t.Fatalf("member role_id=%d, want user role %d", roleID, userRoleID)
	}
	if _, _, err := DeleteSiteRoleForAdmin(ctx, admin, role.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete twice: err=%v, want ErrNotFound", err)
	}
}
