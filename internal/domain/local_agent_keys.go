package domain

import (
	"fmt"
	"strings"
	"time"

	"GoTodo/internal/storage"
)

// CreateLocalAgentKeyInput describes a personal key for an AI agent that runs
// on the owner's machine (Claude Code, Codex, Gemini CLI, a script, ...).
type CreateLocalAgentKeyInput struct {
	Name string
	// Scopes default to tasks:read only. tasks:read is always required, since
	// every write needs the agent to read the task first.
	Scopes []string
	// ProjectID, when set, limits the key to one project the owner belongs to.
	ProjectID *int
	ExpiresAt *time.Time
}

// localAgentKeyProjectAccess reports whether userID can open projectID.
// Swappable in tests.
var localAgentKeyProjectAccess = func(projectID, userID int) bool {
	p, err := storage.GetAccessibleProjectByID(projectID, userID)
	return err == nil && p != nil
}

// NormalizeLocalAgentKeyInput validates in and returns its name and scopes.
func NormalizeLocalAgentKeyInput(userID int, in CreateLocalAgentKeyInput, now time.Time) (string, []string, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return "", nil, fmt.Errorf("%w: key name is required", ErrValidation)
	}
	if len(name) > 80 {
		return "", nil, fmt.Errorf("%w: key name is too long", ErrValidation)
	}
	raw := in.Scopes
	if len(raw) == 0 {
		raw = []string{storage.APIScopeTasksRead}
	}
	scopes, err := NormalizeAPIKeyScopes(raw)
	if err != nil {
		return "", nil, err
	}
	hasRead := false
	for _, s := range scopes {
		if s == storage.APIScopeTasksRead {
			hasRead = true
		}
	}
	if !hasRead {
		return "", nil, fmt.Errorf("%w: local agent keys need the %s scope", ErrValidation, storage.APIScopeTasksRead)
	}
	if in.ProjectID != nil {
		if *in.ProjectID <= 0 || !localAgentKeyProjectAccess(*in.ProjectID, userID) {
			return "", nil, fmt.Errorf("%w: project not found", ErrValidation)
		}
	}
	if err := validateAPIKeyExpiry(in.ExpiresAt, now); err != nil {
		return "", nil, err
	}
	return name, scopes, nil
}

// CreateLocalAgentKey mints a local agent key acting as userID. A key with the
// same name is rotated, like other personal keys.
func CreateLocalAgentKey(userID int, in CreateLocalAgentKeyInput) (string, *storage.APIKey, error) {
	if !storage.UserHasSitePermission(userID, storage.SitePermAgentsLocal) {
		return "", nil, fmt.Errorf("%w: your role can't use local AI agents", ErrForbidden)
	}
	name, scopes, err := NormalizeLocalAgentKeyInput(userID, in, time.Now())
	if err != nil {
		return "", nil, err
	}
	return storage.CreateOrRotateLocalAgentAPIKey(userID, name, scopes, in.ProjectID, in.ExpiresAt)
}
