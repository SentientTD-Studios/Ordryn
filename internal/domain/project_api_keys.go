package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"GoTodo/internal/storage"
)

// MaxProjectAPIKeyLifetime caps how far out a project key's expiry may be set.
const MaxProjectAPIKeyLifetime = 5 * 365 * 24 * time.Hour

type CreateProjectAPIKeyInput struct {
	Name      string
	Scopes    []string
	ExpiresAt *time.Time
}

// NormalizeAPIKeyScopes validates and de-duplicates scopes, returning them in
// canonical order. At least one scope is required.
func NormalizeAPIKeyScopes(raw []string) ([]string, error) {
	want := make(map[string]bool, len(raw))
	for _, s := range raw {
		s = strings.ToLower(strings.TrimSpace(s))
		if s == "" {
			continue
		}
		known := false
		for _, k := range storage.APIKeyScopes {
			if s == k {
				known = true
				break
			}
		}
		if !known {
			return nil, fmt.Errorf("%w: unknown scope %q (allowed: %s)", ErrValidation, s, strings.Join(storage.APIKeyScopes, ", "))
		}
		want[s] = true
	}
	out := make([]string, 0, len(want))
	for _, k := range storage.APIKeyScopes {
		if want[k] {
			out = append(out, k)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: choose at least one scope", ErrValidation)
	}
	return out, nil
}

func validateAPIKeyExpiry(expiresAt *time.Time, now time.Time) error {
	if expiresAt == nil {
		return nil
	}
	if !expiresAt.After(now) {
		return fmt.Errorf("%w: expiry must be in the future", ErrValidation)
	}
	if expiresAt.Sub(now) > MaxProjectAPIKeyLifetime {
		return fmt.Errorf("%w: expiry can be at most 5 years out", ErrValidation)
	}
	return nil
}

// ListProjectAPIKeys returns a project's keys for a project manager.
func ListProjectAPIKeys(userID, projectID int) ([]storage.APIKey, error) {
	if _, err := requireProjectManage(projectID, userID); err != nil {
		return nil, err
	}
	return storage.ListProjectAPIKeys(projectID)
}

// CreateProjectAPIKey mints a project-scoped key acting as the calling manager.
func CreateProjectAPIKey(userID, projectID int, in CreateProjectAPIKeyInput) (string, *storage.APIKey, error) {
	if _, err := requireProjectManage(projectID, userID); err != nil {
		return "", nil, err
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return "", nil, fmt.Errorf("%w: key name is required", ErrValidation)
	}
	if len(name) > 80 {
		return "", nil, fmt.Errorf("%w: key name is too long", ErrValidation)
	}
	scopes, err := NormalizeAPIKeyScopes(in.Scopes)
	if err != nil {
		return "", nil, err
	}
	if err := validateAPIKeyExpiry(in.ExpiresAt, time.Now()); err != nil {
		return "", nil, err
	}
	plaintext, rec, err := storage.CreateProjectAPIKey(projectID, userID, name, scopes, in.ExpiresAt)
	if errors.Is(err, storage.ErrAPIKeyNameExists) {
		return "", nil, fmt.Errorf("%w: a key with that name already exists in this project", ErrConflict)
	}
	return plaintext, rec, err
}

// RevokeProjectAPIKey revokes any of a project's keys; managers may revoke
// keys other managers created.
func RevokeProjectAPIKey(userID, projectID, keyID int) error {
	if _, err := requireProjectManage(projectID, userID); err != nil {
		return err
	}
	if err := storage.RevokeProjectAPIKey(projectID, keyID); err != nil {
		if errors.Is(err, storage.ErrAPIKeyNotFound) {
			return ErrNotFound
		}
		return err
	}
	return nil
}
