package domain

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"GoTodo/internal/storage"
)

// createSiteRoleUser makes a site role with perms and a user on it.
func createSiteRoleUser(t *testing.T, roleName string, perms []string) int {
	t.Helper()
	pool, err := storage.OpenDatabase()
	if err != nil {
		t.Fatal(err)
	}
	defer storage.CloseDatabase(pool)
	var roleID int
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO roles (name, permissions) VALUES ($1, $2)
		 ON CONFLICT (name) DO UPDATE SET permissions = EXCLUDED.permissions
		 RETURNING id`, roleName, perms).Scan(&roleID); err != nil {
		t.Fatalf("create role %s: %v", roleName, err)
	}
	var userID int
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO users (email, password, role_id) VALUES ($1, 'x', $2) RETURNING id`,
		fmt.Sprintf("%s@example.com", roleName), roleID).Scan(&userID); err != nil {
		t.Fatalf("create user for %s: %v", roleName, err)
	}
	return userID
}

func TestCreateProjectRequiresSitePermission(t *testing.T) {
	ctx := context.Background()
	const role = "site-perm-restricted"
	userID := createSiteRoleUser(t, role, []string{})

	if _, err := CreateProject(ctx, userID, "Not allowed", ""); !errors.Is(err, ErrForbidden) {
		t.Fatalf("create project without projects:create: err=%v, want ErrForbidden", err)
	}
	if _, err := CreateOrganizationForUser(ctx, userID, "Not allowed", ""); !errors.Is(err, ErrForbidden) {
		t.Fatalf("create org without orgs:create: err=%v, want ErrForbidden", err)
	}

	if err := storage.SetRolePermission(role, storage.SitePermProjectsCreate, true); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateProject(ctx, userID, "Allowed", ""); err != nil {
		t.Fatalf("create project after grant: %v", err)
	}
	// Granting projects:create alone must not open up organizations.
	if _, err := CreateOrganizationForUser(ctx, userID, "Still not allowed", ""); !errors.Is(err, ErrForbidden) {
		t.Fatalf("create org with only projects:create: err=%v, want ErrForbidden", err)
	}

	if err := storage.SetRolePermission(role, storage.SitePermProjectsCreate, false); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateProject(ctx, userID, "Revoked", ""); !errors.Is(err, ErrForbidden) {
		t.Fatalf("create project after revoke: err=%v, want ErrForbidden", err)
	}
}

func TestSiteAdminBypassesCreatePermissions(t *testing.T) {
	ctx := context.Background()
	adminID := createSiteRoleUser(t, "site-perm-admin", []string{storage.SitePermAdmin})

	if _, err := CreateProject(ctx, adminID, "Admin project", ""); err != nil {
		t.Fatalf("admin create project: %v", err)
	}
	if _, err := CreateOrganizationForUser(ctx, adminID, "Admin org", ""); err != nil {
		t.Fatalf("admin create org: %v", err)
	}
}

func TestSetRolePermissionIsIdempotent(t *testing.T) {
	const role = "site-perm-idempotent"
	createSiteRoleUser(t, role, []string{"add"})

	for i := 0; i < 2; i++ {
		if err := storage.SetRolePermission(role, storage.SitePermOrgsCreate, true); err != nil {
			t.Fatal(err)
		}
	}
	perms, err := storage.GetPermissionsByRoleID(mustRoleID(t, role))
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, p := range perms {
		if p == storage.SitePermOrgsCreate {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("orgs:create appears %d times in %v, want 1", n, perms)
	}
	if has, err := storage.RoleHasPermission(role, "add"); err != nil || !has {
		t.Fatalf("existing permission lost: has=%v err=%v", has, err)
	}
	if has, err := storage.RoleHasPermission("no-such-role", storage.SitePermOrgsCreate); err != nil || has {
		t.Fatalf("missing role: has=%v err=%v, want false/nil", has, err)
	}
}

func mustRoleID(t *testing.T, name string) int {
	t.Helper()
	id, err := storage.GetRoleIDByName(name)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
