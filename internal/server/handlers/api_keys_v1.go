package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"GoTodo/internal/domain"
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
)

type apiKeyJSON struct {
	ID         int     `json:"id"`
	Name       string  `json:"name"`
	KeyPrefix  string  `json:"key_prefix"`
	CreatedAt  string  `json:"created_at"`
	LastUsedAt *string `json:"last_used_at,omitempty"`
	// Kind is "standard" (full access as you) or "local_agent".
	Kind string `json:"kind"`
	// Scopes, ProjectID, and ProjectName apply to local agent keys only.
	Scopes      []string `json:"scopes"`
	ProjectID   *int     `json:"project_id"`
	ProjectName string   `json:"project_name,omitempty"`
	ExpiresAt   *string  `json:"expires_at"`
	Expired     bool     `json:"expired"`
}

type apiKeyNameRequest struct {
	Name string `json:"name"`
}

type apiKeyCreateRequest struct {
	Name string `json:"name"`
	// Kind is "standard" (default) or "local_agent".
	Kind      string   `json:"kind"`
	Scopes    []string `json:"scopes"`
	ProjectID *int     `json:"project_id"`
	ExpiresAt *string  `json:"expires_at"`
}

type apiKeyCreateResponse struct {
	apiKeyJSON
	Key string `json:"key"`
}

const apiKeyKindStandard = "standard"

func normalizeAPIKeyName(raw string) (string, string) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", "Key name is required."
	}
	if len(name) > 80 {
		return "", "Key name is too long."
	}
	return name, ""
}

// APIV1APIKeysRouter handles /api/v2/api-keys and /api/v2/api-keys/{id}.
func APIV1APIKeysRouter(w http.ResponseWriter, r *http.Request) {
	sub := utils.ParseAPIV1Subpath(r, "api-keys")
	if sub == "" {
		switch r.Method {
		case http.MethodGet:
			apiV1ListAPIKeys(w, r)
		case http.MethodPost:
			apiV1CreateAPIKey(w, r)
		default:
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		}
		return
	}
	id, err := strconv.Atoi(sub)
	if err != nil || id <= 0 {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid API key id.")
		return
	}
	switch r.Method {
	case http.MethodPatch:
		apiV1RenameAPIKey(w, r, id)
	case http.MethodDelete:
		apiV1RevokeAPIKey(w, r, id)
	default:
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
	}
}

func apiV1ListAPIKeys(w http.ResponseWriter, r *http.Request) {
	userID, ok := utils.GetAPIUserID(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	keys, err := storage.ListAPIKeysForUser(userID)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to list API keys.")
		return
	}
	out := make([]apiKeyJSON, 0, len(keys))
	for _, k := range keys {
		out = append(out, apiKeyToJSON(k))
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(out)
}

func apiV1CreateAPIKey(w http.ResponseWriter, r *http.Request) {
	if !utils.RedisAvailable() {
		utils.APIJSONError(w, http.StatusServiceUnavailable, "api_unavailable", "Redis is required for the REST API.")
		return
	}
	userID, ok := utils.GetAPIUserID(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	// A key can only mint keys as broad as itself: keys made with a key are
	// refused outright (only a signed-in session reaches this), but say so
	// plainly in case the chain ever changes.
	if p, ok := utils.GetAPIKeyPrincipal(r); ok && (p.IsLocalAgent || p.IsProjectScoped()) {
		utils.APIJSONError(w, http.StatusForbidden, "forbidden", "This API key cannot create API keys.")
		return
	}
	var req apiKeyCreateRequest
	if err := decodeJSONBody(r, &req); err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
		return
	}
	var plaintext string
	var record *storage.APIKey
	var err error
	switch strings.ToLower(strings.TrimSpace(req.Kind)) {
	case "", apiKeyKindStandard:
		if len(req.Scopes) > 0 || req.ProjectID != nil || req.ExpiresAt != nil {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request",
				`scopes, project_id, and expires_at apply to "local_agent" keys only.`)
			return
		}
		name, msg := normalizeAPIKeyName(req.Name)
		if msg != "" {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", msg)
			return
		}
		plaintext, record, err = storage.CreateOrRotateAPIKey(userID, name)
		if err != nil {
			utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to create API key.")
			return
		}
	case storage.APIKeyKindLocalAgent:
		in := domain.CreateLocalAgentKeyInput{Name: req.Name, Scopes: req.Scopes, ProjectID: req.ProjectID}
		if req.ExpiresAt != nil && strings.TrimSpace(*req.ExpiresAt) != "" {
			t, perr := time.Parse(time.RFC3339, strings.TrimSpace(*req.ExpiresAt))
			if perr != nil {
				utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "expires_at must be an RFC 3339 timestamp.")
				return
			}
			in.ExpiresAt = &t
		}
		plaintext, record, err = domain.CreateLocalAgentKey(userID, in)
		if err != nil {
			if errors.Is(err, domain.ErrValidation) {
				utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", sharingClientMessage(err, "Invalid request."))
				return
			}
			utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to create API key.")
			return
		}
		if record.LimitProjectID != nil {
			if p, perr := storage.GetAccessibleProjectByID(*record.LimitProjectID, userID); perr == nil && p != nil {
				record.LimitProjectName = p.Name
			}
		}
	default:
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", `kind must be "standard" or "local_agent".`)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(apiKeyCreateResponse{apiKeyJSON: apiKeyToJSON(*record), Key: plaintext})
}

func apiV1RenameAPIKey(w http.ResponseWriter, r *http.Request, keyID int) {
	userID, ok := utils.GetAPIUserID(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	var req apiKeyNameRequest
	if err := decodeJSONBody(r, &req); err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
		return
	}
	name, msg := normalizeAPIKeyName(req.Name)
	if msg != "" {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", msg)
		return
	}
	record, err := storage.UpdateAPIKeyName(keyID, userID, name)
	if err != nil {
		if errors.Is(err, storage.ErrAPIKeyNameExists) {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "An API key with that name already exists.")
			return
		}
		if errors.Is(err, storage.ErrAPIKeyNotFound) {
			utils.APIJSONError(w, http.StatusNotFound, "not_found", "API key not found.")
			return
		}
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to rename API key.")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(apiKeyToJSON(*record))
}

func apiV1RevokeAPIKey(w http.ResponseWriter, r *http.Request, keyID int) {
	userID, ok := utils.GetAPIUserID(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	if err := storage.RevokeAPIKey(keyID, userID); err != nil {
		if errors.Is(err, storage.ErrAPIKeyNotFound) {
			utils.APIJSONError(w, http.StatusNotFound, "not_found", "API key not found.")
			return
		}
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to revoke API key.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func apiKeyToJSON(k storage.APIKey) apiKeyJSON {
	out := apiKeyJSON{
		ID:        k.ID,
		Name:      k.Name,
		KeyPrefix: k.KeyPrefix,
		CreatedAt: k.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		Kind:      apiKeyKindStandard,
		Scopes:    []string{},
		ExpiresAt: optionalRFC3339(k.ExpiresAt),
		Expired:   k.ExpiresAt != nil && !k.ExpiresAt.After(time.Now()),
	}
	if k.Kind == storage.APIKeyKindLocalAgent {
		out.Kind = storage.APIKeyKindLocalAgent
		if k.Scopes != nil {
			out.Scopes = k.Scopes
		}
		out.ProjectID = k.LimitProjectID
		out.ProjectName = k.LimitProjectName
	}
	if k.LastUsedAt != nil {
		s := k.LastUsedAt.Format("2006-01-02T15:04:05Z07:00")
		out.LastUsedAt = &s
	}
	return out
}
