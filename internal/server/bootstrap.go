package server

import (
	"fmt"
	"os"
	"strings"

	"GoTodo/internal/config"
	"GoTodo/internal/storage"

	"golang.org/x/crypto/bcrypt"
)

const bootstrapAPIKeyName = "bootstrap"

// RunBootstrap applies optional first-boot env configuration (idempotent).
//
//	GOTODO_BOOTSTRAP_ADMIN_EMAIL / GOTODO_BOOTSTRAP_ADMIN_PASSWORD — create admin if missing
//	GOTODO_BOOTSTRAP_ENABLE_API=true — set site_settings.enable_api
//	GOTODO_BOOTSTRAP_CREATE_API_KEY=true — mint a named "bootstrap" API key (once) for that admin
func RunBootstrap() error {
	email := strings.TrimSpace(os.Getenv("GOTODO_BOOTSTRAP_ADMIN_EMAIL"))
	password := os.Getenv("GOTODO_BOOTSTRAP_ADMIN_PASSWORD")
	enableAPI := strings.EqualFold(strings.TrimSpace(os.Getenv("GOTODO_BOOTSTRAP_ENABLE_API")), "true")
	createKey := strings.EqualFold(strings.TrimSpace(os.Getenv("GOTODO_BOOTSTRAP_CREATE_API_KEY")), "true")

	if email == "" && !enableAPI && !createKey {
		return nil
	}

	var adminID int
	if email != "" {
		if password == "" {
			return fmt.Errorf("GOTODO_BOOTSTRAP_ADMIN_PASSWORD is required when GOTODO_BOOTSTRAP_ADMIN_EMAIL is set")
		}
		exists, err := storage.UserExistsByEmail(email)
		if err != nil {
			return fmt.Errorf("bootstrap admin lookup: %w", err)
		}
		if exists {
			adminID, err = storage.GetUserIDByEmail(email)
			if err != nil {
				return fmt.Errorf("bootstrap admin id: %w", err)
			}
			fmt.Printf("Bootstrap: admin user %q already exists (id=%d); leaving as-is\n", email, adminID)
		} else {
			roleID, err := storage.GetRoleIDByName("admin")
			if err != nil {
				return fmt.Errorf("bootstrap admin role: %w", err)
			}
			hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
			if err != nil {
				return fmt.Errorf("bootstrap hash password: %w", err)
			}
			tz := config.Cfg.DefaultTimezone
			if tz == "" {
				tz = "America/New_York"
			}
			adminID, err = storage.CreateUser(email, string(hash), tz, roleID)
			if err != nil {
				return fmt.Errorf("bootstrap create admin: %w", err)
			}
			fmt.Printf("Bootstrap: created admin user %q (id=%d)\n", email, adminID)
		}
	}

	if enableAPI {
		if err := storage.EnsureEnableAPI(); err != nil {
			return fmt.Errorf("bootstrap enable API: %w", err)
		}
		fmt.Println("Bootstrap: site_settings.enable_api=true")
	}

	if createKey {
		if adminID == 0 {
			if email == "" {
				return fmt.Errorf("GOTODO_BOOTSTRAP_CREATE_API_KEY requires GOTODO_BOOTSTRAP_ADMIN_EMAIL")
			}
			var err error
			adminID, err = storage.GetUserIDByEmail(email)
			if err != nil {
				return fmt.Errorf("bootstrap API key user: %w", err)
			}
		}
		has, err := storage.HasActiveAPIKeyNamed(adminID, bootstrapAPIKeyName)
		if err != nil {
			return fmt.Errorf("bootstrap API key lookup: %w", err)
		}
		if has {
			fmt.Printf("Bootstrap: API key %q already exists for user id=%d; not reissuing\n", bootstrapAPIKeyName, adminID)
			return nil
		}
		plaintext, record, err := storage.CreateAPIKey(adminID, bootstrapAPIKeyName)
		if err != nil {
			return fmt.Errorf("bootstrap create API key: %w", err)
		}
		fmt.Printf("Bootstrap: created API key id=%d name=%q — store this now; it will not be shown again:\n%s\n",
			record.ID, record.Name, plaintext)
	}

	return nil
}
