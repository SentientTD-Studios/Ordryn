package storage

import (
	"context"
	"fmt"
)

// RunMigrations attempts to create required tables and apply non-destructive migrations.
// It continues on errors, but returns an aggregated error if all operations fail.
func RunMigrations() error {
	errCount := 0

	if err := CreateUsersTable(); err != nil {
		fmt.Printf("migration: CreateUsersTable failed: %v\n", err)
		errCount++
	}
	if err := CreateRolesTable(); err != nil {
		fmt.Printf("migration: CreateRolesTable failed: %v\n", err)
		errCount++
	}
	if err := CreateInvitesTable(); err != nil {
		fmt.Printf("migration: CreateInvitesTable failed: %v\n", err)
		errCount++
	}
	if err := CreateTasksTable(); err != nil {
		fmt.Printf("migration: CreateTasksTable failed: %v\n", err)
		errCount++
	}
	// Ensure projects table exists
	if err := CreateProjectsTable(); err != nil {
		fmt.Printf("migration: CreateProjectsTable failed: %v\n", err)
		errCount++
	}

	// Non-breaking column migrations
	if err := MigrateUsersAddTimezone(); err != nil {
		fmt.Printf("migration: MigrateUsersAddTimezone failed: %v\n", err)
		errCount++
	}
	if err := MigrateUsersAddName(); err != nil {
		fmt.Printf("migration: MigrateUsersAddName failed: %v\n", err)
		errCount++
	}
	if err := MigrateUsersAddItemsPerPage(); err != nil {
		fmt.Printf("migration: MigrateUsersAddItemsPerPage failed: %v\n", err)
		errCount++
	}
	if err := MigrateUsersAddIsBanned(); err != nil {
		fmt.Printf("migration: MigrateUsersAddIsBanned failed: %v\n", err)
		errCount++
	}
	if err := MigrateUsersAddCalendarToken(); err != nil {
		fmt.Printf("migration: MigrateUsersAddCalendarToken failed: %v\n", err)
		errCount++
	}
	if err := MigrateUsersAddDigestSettings(); err != nil {
		fmt.Printf("migration: MigrateUsersAddDigestSettings failed: %v\n", err)
		errCount++
	}
	if err := MigrateTasksAddIsFavorite(); err != nil {
		fmt.Printf("migration: MigrateTasksAddIsFavorite failed: %v\n", err)
		errCount++
	}
	if err := MigrateTasksAddPosition(); err != nil {
		fmt.Printf("migration: MigrateTasksAddPosition failed: %v\n", err)
		errCount++
	}
	// Add project_id column to tasks (nullable)
	if err := MigrateTasksAddProjectID(); err != nil {
		fmt.Printf("migration: MigrateTasksAddProjectID failed: %v\n", err)
		errCount++
	}
	// Add date_modified column to tasks
	if err := MigrateTasksAddDateModified(); err != nil {
		fmt.Printf("migration: MigrateTasksAddDateModified failed: %v\n", err)
		errCount++
	}
	// Add due_date column to tasks
	if err := MigrateTasksAddDueDate(); err != nil {
		fmt.Printf("migration: MigrateTasksAddDueDate failed: %v\n", err)
		errCount++
	}
	if err := MigrateTasksAddPriority(); err != nil {
		fmt.Printf("migration: MigrateTasksAddPriority failed: %v\n", err)
		errCount++
	}
	if err := CreateTagsTables(); err != nil {
		fmt.Printf("migration: CreateTagsTables failed: %v\n", err)
		errCount++
	}
	if err := CreateTaskEventsTable(); err != nil {
		fmt.Printf("migration: CreateTaskEventsTable failed: %v\n", err)
		errCount++
	}
	if err := CreateSavedViewsTable(); err != nil {
		fmt.Printf("migration: CreateSavedViewsTable failed: %v\n", err)
		errCount++
	}

	// Ensure site_settings table exists
	if err := CreateSiteSettingsTable(); err != nil {
		fmt.Printf("migration: CreateSiteSettingsTable failed: %v\n", err)
		errCount++
	}
	if err := MigrateSiteSettingsAddRegistrationOptions(); err != nil {
		fmt.Printf("migration: MigrateSiteSettingsAddRegistrationOptions failed: %v\n", err)
		errCount++
	}
	if err := MigrateSiteSettingsAddMetaDescription(); err != nil {
		fmt.Printf("migration: MigrateSiteSettingsAddMetaDescription failed: %v\n", err)
		errCount++
	}
	if err := MigrateSiteSettingsAddGlobalAnnouncement(); err != nil {
		fmt.Printf("migration: MigrateSiteSettingsAddGlobalAnnouncement failed: %v\n", err)
		errCount++
	}

	if err := CreateSavedViewsTable(); err != nil {
		fmt.Printf("migration: CreateSavedViewsTable failed: %v\n", err)
		errCount++
	}

	if err := MigrateSiteSettingsAddEnableAPI(); err != nil {
		fmt.Printf("migration: MigrateSiteSettingsAddEnableAPI failed: %v\n", err)
		errCount++
	}

	if err := CreateAPIKeysTable(); err != nil {
		fmt.Printf("migration: CreateAPIKeysTable failed: %v\n", err)
		errCount++
	}

	// Ensure password_reset table exists
	if err := CreatePasswordResetTable(); err != nil {
		fmt.Printf("migration: CreatePasswordResetTable failed: %v\n", err)
		errCount++
	}

	// Ensure 'admin' permission exists on the admin role
	if err := MigrateEnsureAdminPermission(); err != nil {
		fmt.Printf("migration: MigrateEnsureAdminPermission failed: %v\n", err)
		errCount++
	}

	// Ensure default 'user' role exists for signups
	if err := MigrateEnsureUserRole(); err != nil {
		fmt.Printf("migration: MigrateEnsureUserRole failed: %v\n", err)
		errCount++
	}

	// Ensure tasks.user_id foreign key exists
	if err := MigrateTasksUserFK(); err != nil {
		fmt.Printf("migration: MigrateTasksUserFK failed: %v\n", err)
		errCount++
	}

	if errCount == 0 {
		return nil
	}
	return fmt.Errorf("migrations completed with %d errors (see logs)", errCount)
}

// MigrateEnsureAdminPermission ensures the roles table contains an 'admin' role
// and that its permissions array includes the "admin" permission.
func MigrateEnsureAdminPermission() error {
	pool, err := OpenDatabase()
	if err != nil {
		return fmt.Errorf("failed to open database: %v", err)
	}
	defer CloseDatabase(pool)

	// Required permissions for admin role
	required := []string{"add", "edit", "delete", "viewall", "createinvites", "admin"}

	// Try to read existing permissions for role 'admin'
	var perms []string
	err = pool.QueryRow(context.Background(), "SELECT permissions FROM roles WHERE name = 'admin'").Scan(&perms)
	if err != nil {
		// Role doesn't exist (or other error) — attempt to create it with full permission set
		_, insErr := pool.Exec(context.Background(), "INSERT INTO roles (name, permissions) VALUES ($1, $2)", "admin", required)
		if insErr != nil {
			return fmt.Errorf("failed to create admin role: %v (scan error: %v)", insErr, err)
		}
		return nil
	}

	// Compute missing permissions and append them individually to avoid duplicates
	have := map[string]bool{}
	for _, p := range perms {
		have[p] = true
	}
	missing := make([]string, 0)
	for _, r := range required {
		if !have[r] {
			missing = append(missing, r)
		}
	}
	if len(missing) == 0 {
		return nil
	}

	for _, m := range missing {
		// Append only if not present (WHERE clause protects against duplicates)
		_, err = pool.Exec(context.Background(), "UPDATE roles SET permissions = array_append(permissions, $1) WHERE name = 'admin' AND NOT (permissions @> $2)", m, []string{m})
		if err != nil {
			return fmt.Errorf("failed to append permission %s: %v", m, err)
		}
	}
	return nil
}

// MigrateEnsureUserRole ensures the roles table contains a 'user' role with
// standard task permissions for new signups.
func MigrateEnsureUserRole() error {
	pool, err := OpenDatabase()
	if err != nil {
		return fmt.Errorf("failed to open database: %v", err)
	}
	defer CloseDatabase(pool)

	perms := []string{"add", "edit", "delete"}
	var existingID int
	err = pool.QueryRow(context.Background(), "SELECT id FROM roles WHERE name = 'user'").Scan(&existingID)
	if err == nil {
		return nil
	}

	_, insErr := pool.Exec(context.Background(), "INSERT INTO roles (name, permissions) VALUES ($1, $2)", "user", perms)
	if insErr != nil {
		return fmt.Errorf("failed to create user role: %v", insErr)
	}
	return nil
}

// MigrateTasksUserFK adds a foreign key from tasks.user_id to users.id when missing.
func MigrateTasksUserFK() error {
	pool, err := OpenDatabase()
	if err != nil {
		return fmt.Errorf("failed to open database: %v", err)
	}
	defer CloseDatabase(pool)

	var exists bool
	err = pool.QueryRow(context.Background(),
		"SELECT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_tasks_users')",
	).Scan(&exists)
	if err != nil {
		return fmt.Errorf("failed to check tasks user fk: %v", err)
	}
	if exists {
		return nil
	}

	_, err = pool.Exec(context.Background(), "ALTER TABLE tasks ADD COLUMN IF NOT EXISTS user_id INTEGER")
	if err != nil {
		return fmt.Errorf("failed to ensure user_id column on tasks: %v", err)
	}

	_, err = pool.Exec(context.Background(),
		"ALTER TABLE tasks ADD CONSTRAINT fk_tasks_users FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE",
	)
	if err != nil {
		return fmt.Errorf("failed to add tasks user foreign key: %v", err)
	}
	return nil
}
