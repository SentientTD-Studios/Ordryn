package storage

import (
	"context"
	"fmt"
	"log"
	"strings"
)

const (
	RED   = "\033[31m"
	GREEN = "\033[32m"
	RESET = "\033[0m"
)

// This will solely add new columns we need later down the line..it's dumb, but this is how I'm handling it for now
func AddColumns() (bool, error) {
	pool, err := OpenDatabase()
	defer CloseDatabase(pool)

	_, err = pool.Exec(context.Background(), "ALTER TABLE tasks ADD COLUMN IF NOT EXISTS time_stamp TIMESTAMP default NOW()")
	if err != nil {
		log.Printf("Error in AddColumns: %v\n", err)
		return false, err
	}

	return true, nil
}

func RemoveColumns() (bool, error) {
	pool, err := OpenDatabase()
	defer CloseDatabase(pool)

	_, err = pool.Exec(context.Background(), "ALTER TABLE tasks DROP COLUMN IF EXISTS time_stamp")
	if err != nil {
		log.Printf("Error in RemoveColumns: %v\n", err)
		return false, err
	}

	return true, nil
}

func CreateDatabase() {
	pool, err := OpenDatabase()
	if err != nil {
		log.Fatalf("Unable to connect to database: %v\n", err)
	}
	defer CloseDatabase(pool)

	if err := CreateTasksTable(); err != nil {
		log.Fatalf("Unable to create table: %v\n", err)
	}
	fmt.Println("Database connection appears to be " + GREEN + "successful" + RESET + ".")
}

func GetNextID() int {
	pool, err := OpenDatabase()
	defer CloseDatabase(pool)

	var nextID int
	err = pool.QueryRow(context.Background(), "SELECT COALESCE(MAX(id), 0) FROM tasks").Scan(&nextID)

	if err != nil {
		log.Printf("Error in GetNextID: %v\n", err)
		return 1
	}

	return nextID + 1
}

func DeleteAllTasks() {
	fmt.Print("\nAre you sure you want to delete all tasks? (y/n): ")
	var confirm string
	_, err := fmt.Scanln(&confirm)
	if err != nil {
		fmt.Println("Invalid ID")
		return
	}
	if confirm == "y" {
		pool, err := OpenDatabase()
		defer CloseDatabase(pool)

		_, err = pool.Exec(context.Background(), "DELETE FROM tasks")
		if err != nil {
			log.Printf("Error in DeleteAllTasks: %v\n", err)
		} else {
			fmt.Println("All tasks deleted successfully!")
		}
	} else {
		fmt.Println("Deletion cancelled.")
	}
}

func CreateTable(tableName string, columns []string) error {
	pool, err := OpenDatabase()
	if err != nil {
		return fmt.Errorf("failed to open database: %v", err)
	}
	defer CloseDatabase(pool)

	query := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (%s)", tableName, strings.Join(columns, ", "))

	_, err = pool.Exec(context.Background(), query)
	if err != nil {
		return fmt.Errorf("failed to create table %s: %v", tableName, err)
	}

	fmt.Printf("Table %s created successfully\n", tableName)
	return nil
}

// CreateUsersTable creates the users table with predefined columns
func CreateUsersTable() error {
	columns := []string{
		"id SERIAL PRIMARY KEY",
		"email VARCHAR(255) UNIQUE NOT NULL",
		"password VARCHAR(255) NOT NULL",
		"role_id INTEGER NOT NULL",
		"is_banned BOOLEAN DEFAULT FALSE",
		"items_per_page INTEGER DEFAULT 15",
		"created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP",
		"updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP",
	}

	return CreateTable("users", columns)
}

// MigrateUsersAddIsBanned adds is_banned boolean column to users table
func MigrateUsersAddIsBanned() error {
	pool, err := OpenDatabase()
	if err != nil {
		return fmt.Errorf("failed to open database: %v", err)
	}
	defer CloseDatabase(pool)

	_, err = pool.Exec(context.Background(), "ALTER TABLE users ADD COLUMN IF NOT EXISTS is_banned BOOLEAN DEFAULT FALSE")
	if err != nil {
		return fmt.Errorf("failed to add is_banned column to users table: %v", err)
	}
	return nil
}

// MigrateUsersAddCalendarToken adds a unique calendar feed token column to users.
func MigrateUsersAddCalendarToken() error {
	pool, err := OpenDatabase()
	if err != nil {
		return fmt.Errorf("failed to open database: %v", err)
	}
	defer CloseDatabase(pool)

	_, err = pool.Exec(context.Background(), "ALTER TABLE users ADD COLUMN IF NOT EXISTS calendar_token VARCHAR(64) UNIQUE")
	if err != nil {
		return fmt.Errorf("failed to add calendar_token column to users table: %v", err)
	}
	return nil
}

func CreateInvitesTable() error {
	columns := []string{
		"id SERIAL PRIMARY KEY",
		"email VARCHAR(255) UNIQUE NOT NULL",
		"token VARCHAR(255) UNIQUE NOT NULL",
		"inviteused INTEGER DEFAULT 0",
	}
	return CreateTable("invites", columns)
}

// MigrateInvitesTable adds the inviteused column if it doesn't exist
func MigrateInvitesTable() error {
	pool, err := OpenDatabase()
	if err != nil {
		return fmt.Errorf("failed to open database: %v", err)
	}
	defer CloseDatabase(pool)

	// Add inviteused column if it doesn't exist
	_, err = pool.Exec(context.Background(), "ALTER TABLE invites ADD COLUMN IF NOT EXISTS inviteused INTEGER DEFAULT 0")
	if err != nil {
		return fmt.Errorf("failed to add inviteused column to invites table: %v", err)
	}
	return nil
}

func CreateRolesTable() error {
	columns := []string{
		"id SERIAL PRIMARY KEY",
		"name VARCHAR(50) UNIQUE NOT NULL",
		"permissions TEXT[] NOT NULL",
	}
	return CreateTable("roles", columns)
}

func CreateTasksTable() error {
	columns := []string{
		"id SERIAL PRIMARY KEY",
		"title TEXT NOT NULL",
		"description TEXT",
		"completed BOOLEAN DEFAULT FALSE",
		"time_stamp TIMESTAMP DEFAULT NOW()",
		"is_favorite BOOLEAN DEFAULT FALSE",
		"position INTEGER DEFAULT 0",
		"user_id INTEGER",
	}
	return CreateTable("tasks", columns)
}

// CreateProjectsTable creates the projects table for organizing tasks
func CreateProjectsTable() error {
	columns := []string{
		"id SERIAL PRIMARY KEY",
		"user_id INTEGER NOT NULL",
		"name TEXT NOT NULL",
		"created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP",
		"updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP",
	}
	return CreateTable("projects", columns)
}

// MigrateTasksTable adds a user_id column and a foreign key constraint to the tasks table
func MigrateTasksTable() error {
	pool, err := OpenDatabase()
	if err != nil {
		return fmt.Errorf("failed to open database: %v", err)
	}
	defer CloseDatabase(pool)

	// Add user_id column
	_, err = pool.Exec(context.Background(), "ALTER TABLE tasks ADD COLUMN IF NOT EXISTS user_id INTEGER")
	if err != nil {
		return fmt.Errorf("failed to add user_id column to tasks table: %v", err)
	}

	// Add foreign key constraint
	_, err = pool.Exec(context.Background(), "ALTER TABLE tasks ADD CONSTRAINT fk_tasks_users FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE")
	if err != nil {
		return fmt.Errorf("failed to add foreign key constraint to tasks table: %v", err)
	}
	return nil
}

// MigrateTasksAddIsFavorite adds is_favorite boolean column to tasks table
func MigrateTasksAddIsFavorite() error {
	pool, err := OpenDatabase()
	if err != nil {
		return fmt.Errorf("failed to open database: %v", err)
	}
	defer CloseDatabase(pool)

	_, err = pool.Exec(context.Background(), "ALTER TABLE tasks ADD COLUMN IF NOT EXISTS is_favorite BOOLEAN DEFAULT FALSE")
	if err != nil {
		return fmt.Errorf("failed to add is_favorite column to tasks table: %v", err)
	}
	return nil
}

// MigrateTasksAddPosition adds position integer column to tasks table
func MigrateTasksAddPosition() error {
	pool, err := OpenDatabase()
	if err != nil {
		return fmt.Errorf("failed to open database: %v", err)
	}
	defer CloseDatabase(pool)

	_, err = pool.Exec(context.Background(), "ALTER TABLE tasks ADD COLUMN IF NOT EXISTS position INTEGER DEFAULT 0")
	if err != nil {
		return fmt.Errorf("failed to add position column to tasks table: %v", err)
	}
	return nil
}

// MigrateTasksAddProjectID adds a nullable project_id column to tasks
func MigrateTasksAddProjectID() error {
	pool, err := OpenDatabase()
	if err != nil {
		return fmt.Errorf("failed to open database: %v", err)
	}
	defer CloseDatabase(pool)

	// Add project_id column
	_, err = pool.Exec(context.Background(), "ALTER TABLE tasks ADD COLUMN IF NOT EXISTS project_id INTEGER")
	if err != nil {
		return fmt.Errorf("failed to add project_id column to tasks table: %v", err)
	}

	// Add foreign key constraint to projects (ON DELETE SET NULL so tasks remain)
	_, err = pool.Exec(context.Background(), "ALTER TABLE tasks ADD CONSTRAINT fk_tasks_projects FOREIGN KEY (project_id) REFERENCES projects (id) ON DELETE SET NULL")
	if err != nil {
		// If the constraint already exists or projects table not yet present this may fail; return error to be logged by caller
		return fmt.Errorf("failed to add foreign key constraint to tasks.project_id: %v", err)
	}
	return nil
}

// MigrateTasksAddDateModified adds date_modified timestamp column to tasks table
func MigrateTasksAddDateModified() error {
	pool, err := OpenDatabase()
	if err != nil {
		return fmt.Errorf("failed to open database: %v", err)
	}
	defer CloseDatabase(pool)

	// Add date_modified column (nullable)
	_, err = pool.Exec(context.Background(), "ALTER TABLE tasks ADD COLUMN IF NOT EXISTS date_modified TIMESTAMP")
	if err != nil {
		return fmt.Errorf("failed to add date_modified column to tasks table: %v", err)
	}

	return nil
}

// MigrateTasksAddDueDate adds due_date column to tasks table
func MigrateTasksAddDueDate() error {
	pool, err := OpenDatabase()
	if err != nil {
		return fmt.Errorf("failed to open database: %v", err)
	}
	defer CloseDatabase(pool)

	// Add due_date column, nullable
	_, err = pool.Exec(context.Background(), "ALTER TABLE tasks ADD COLUMN IF NOT EXISTS due_date DATE")
	if err != nil {
		return fmt.Errorf("failed to add due_date column to tasks table: %v", err)
	}

	return nil
}

// MigrateTasksAddPriority adds priority column to tasks table.
func MigrateTasksAddPriority() error {
	pool, err := OpenDatabase()
	if err != nil {
		return fmt.Errorf("failed to open database: %v", err)
	}
	defer CloseDatabase(pool)

	_, err = pool.Exec(context.Background(), "ALTER TABLE tasks ADD COLUMN IF NOT EXISTS priority SMALLINT NOT NULL DEFAULT 0")
	if err != nil {
		return fmt.Errorf("failed to add priority column to tasks table: %v", err)
	}

	_, err = pool.Exec(context.Background(), "CREATE INDEX IF NOT EXISTS idx_tasks_user_priority ON tasks(user_id, priority)")
	if err != nil {
		return fmt.Errorf("failed to create priority index: %v", err)
	}

	return nil
}

// MigrateUsersAddTimezone adds timezone column to users table
func MigrateUsersAddTimezone() error {
	pool, err := OpenDatabase()
	if err != nil {
		return fmt.Errorf("failed to open database: %v", err)
	}
	defer CloseDatabase(pool)

	// Add timezone column with default value America/New_York (GMT-5)
	_, err = pool.Exec(context.Background(), "ALTER TABLE users ADD COLUMN IF NOT EXISTS timezone VARCHAR(100) DEFAULT 'America/New_York'")
	if err != nil {
		return fmt.Errorf("failed to add timezone column to users table: %v", err)
	}
	return nil
}

func MigrateUsersAddName() error {
	pool, err := OpenDatabase()
	if err != nil {
		return fmt.Errorf("failed to open database: %v", err)
	}
	defer CloseDatabase(pool)

	// name column
	_, err = pool.Exec(context.Background(), "ALTER TABLE users ADD COLUMN IF NOT EXISTS user_name VARCHAR(100)")
	if err != nil {
		return fmt.Errorf("failed to add user_name column to users table: %v", err)
	}

	return nil
}

// MigrateUsersAddItemsPerPage adds items_per_page column to users table
func MigrateUsersAddItemsPerPage() error {
	pool, err := OpenDatabase()
	if err != nil {
		return fmt.Errorf("failed to open database: %v", err)
	}
	defer CloseDatabase(pool)

	_, err = pool.Exec(context.Background(), "ALTER TABLE users ADD COLUMN IF NOT EXISTS items_per_page INTEGER DEFAULT 15")
	if err != nil {
		return fmt.Errorf("failed to add items_per_page column to users table: %v", err)
	}
	return nil
}

// MigrateUsersAddDigestSettings adds email digest preference columns.
func MigrateUsersAddDigestSettings() error {
	pool, err := OpenDatabase()
	if err != nil {
		return fmt.Errorf("failed to open database: %v", err)
	}
	defer CloseDatabase(pool)

	_, err = pool.Exec(context.Background(), "ALTER TABLE users ADD COLUMN IF NOT EXISTS digest_enabled BOOLEAN DEFAULT FALSE")
	if err != nil {
		return fmt.Errorf("failed to add digest_enabled: %v", err)
	}
	_, err = pool.Exec(context.Background(), "ALTER TABLE users ADD COLUMN IF NOT EXISTS digest_hour INTEGER DEFAULT 8")
	if err != nil {
		return fmt.Errorf("failed to add digest_hour: %v", err)
	}
	_, err = pool.Exec(context.Background(), "ALTER TABLE users ADD COLUMN IF NOT EXISTS last_digest_sent DATE")
	if err != nil {
		return fmt.Errorf("failed to add last_digest_sent: %v", err)
	}
	return nil
}

type User struct {
	ID           int
	Email        string
	Password     string
	UserName     string
	ItemsPerPage int
}

func GetUserByEmail(email string) (*User, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	var user User
	// include items_per_page (use COALESCE to ensure default)
	// Use COALESCE for user_name as well since it can be NULL for newly created users
	err = pool.QueryRow(context.Background(), "SELECT id, email, password, COALESCE(user_name, ''), COALESCE(items_per_page, 15) FROM users WHERE email=$1", email).Scan(&user.ID, &user.Email, &user.Password, &user.UserName, &user.ItemsPerPage)
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// IsUserBanned returns true if the user with the given email has is_banned = true
func IsUserBanned(email string) (bool, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return false, err
	}
	defer CloseDatabase(pool)

	var isB bool
	err = pool.QueryRow(context.Background(), "SELECT COALESCE(is_banned, FALSE) FROM users WHERE email = $1", email).Scan(&isB)
	if err != nil {
		return false, err
	}
	return isB, nil
}

func GetPermissionsByRoleID(roleID int) ([]string, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	var permissions []string
	err = pool.QueryRow(context.Background(), "SELECT permissions FROM roles WHERE id=$1", roleID).Scan(&permissions)
	if err != nil {
		return nil, err
	}
	return permissions, nil
}

func GetDefaultRoleID() (int, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return 0, err
	}
	defer CloseDatabase(pool)

	var roleID int
	err = pool.QueryRow(context.Background(), "SELECT id FROM roles WHERE name = 'user'").Scan(&roleID)
	if err != nil {
		return 1, nil
	}
	return roleID, nil
}
