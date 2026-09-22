package domain

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"GoTodo/internal/extensions"
	"GoTodo/internal/live"
	"GoTodo/internal/storage"
)

const maxStorePayloadBytes = 64 << 10

var storeKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9:_-]{0,63}$`)

// StoreConflictError is a 409 when If-Match / revision does not match.
type StoreConflictError struct {
	Current *storage.ExtensionStoreDoc
}

func (e *StoreConflictError) Error() string {
	return "conflict"
}

func (e *StoreConflictError) Unwrap() error {
	return ErrConflict
}

func requireEnabledStoreExtension(extensionID string, projectID int, perm string) (extensions.Entry, error) {
	entry, ok := extensions.Get(extensionID)
	if !ok || !entry.Loaded {
		return extensions.Entry{}, fmt.Errorf("%w: extension is not loaded", ErrForbidden)
	}
	if perm != "" && !entry.Manifest.HasPermission(perm) {
		return extensions.Entry{}, fmt.Errorf("%w: %s is not granted", ErrForbidden, perm)
	}
	site, err := storage.GetExtensionSettings(extensionID)
	if err != nil {
		return extensions.Entry{}, err
	}
	if !site.Enabled {
		return extensions.Entry{}, fmt.Errorf("%w: extension is not enabled", ErrForbidden)
	}
	proj, err := storage.GetExtensionProjectSettings(extensionID, projectID)
	if err != nil {
		return extensions.Entry{}, err
	}
	if !proj.Enabled {
		return extensions.Entry{}, fmt.Errorf("%w: extension is not enabled for this project", ErrForbidden)
	}
	return entry, nil
}

func normalizeStoreKey(key string) (string, error) {
	key = strings.TrimSpace(key)
	if !storeKeyPattern.MatchString(key) || strings.Contains(key, "..") {
		return "", fmt.Errorf("%w: store key is invalid", ErrValidation)
	}
	return key, nil
}

// GetExtensionStoreDoc returns a project document for an enabled store:read extension.
func GetExtensionStoreDoc(userID, projectID int, extensionID, key string) (*storage.ExtensionStoreDoc, error) {
	if _, err := RequireProjectExtensionMember(userID, projectID); err != nil {
		return nil, err
	}
	if _, err := requireEnabledStoreExtension(extensionID, projectID, extensions.PermStoreRead); err != nil {
		return nil, err
	}
	key, err := normalizeStoreKey(key)
	if err != nil {
		return nil, err
	}
	doc, err := storage.GetExtensionStoreDoc(extensionID, projectID, key)
	if err != nil {
		return nil, err
	}
	if doc == nil {
		return nil, ErrNotFound
	}
	return doc, nil
}

// ListExtensionStoreKeys lists document keys for an enabled store:read extension.
func ListExtensionStoreKeys(userID, projectID int, extensionID string) ([]string, error) {
	if _, err := RequireProjectExtensionMember(userID, projectID); err != nil {
		return nil, err
	}
	if _, err := requireEnabledStoreExtension(extensionID, projectID, extensions.PermStoreRead); err != nil {
		return nil, err
	}
	keys, err := storage.ListExtensionStoreKeys(extensionID, projectID)
	if err != nil {
		return nil, err
	}
	if keys == nil {
		keys = []string{}
	}
	return keys, nil
}

// PutExtensionStoreDoc writes a document when revision matches.
func PutExtensionStoreDoc(userID, projectID int, extensionID, key string, revision int, value json.RawMessage) (*storage.ExtensionStoreDoc, error) {
	proj, err := RequireProjectExtensionMember(userID, projectID)
	if err != nil {
		return nil, err
	}
	if !storage.RoleCanWrite(proj.Role) {
		return nil, fmt.Errorf("%w: viewers cannot write extension store documents", ErrForbidden)
	}
	if _, err := requireEnabledStoreExtension(extensionID, projectID, extensions.PermStoreWrite); err != nil {
		return nil, err
	}
	key, err = normalizeStoreKey(key)
	if err != nil {
		return nil, err
	}
	if revision < 0 {
		return nil, fmt.Errorf("%w: revision must be 0 or greater", ErrValidation)
	}
	if len(value) == 0 {
		value = json.RawMessage([]byte("null"))
	}
	if !json.Valid(value) {
		return nil, fmt.Errorf("%w: value must be JSON", ErrValidation)
	}
	if len(value) > maxStorePayloadBytes {
		return nil, fmt.Errorf("%w: value must be %d bytes or less", ErrValidation, maxStorePayloadBytes)
	}
	written, current, err := storage.PutExtensionStoreDoc(extensionID, projectID, key, revision, value, userID)
	if err != nil {
		return nil, err
	}
	if current != nil {
		return nil, &StoreConflictError{Current: current}
	}
	if written == nil {
		return nil, &StoreConflictError{Current: nil}
	}
	live.AfterExtensionStoreChange(userID, projectID, extensionID, key)
	return written, nil
}
