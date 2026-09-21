package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
)

type apiKeyJSON struct {
	ID         int     `json:"id"`
	Name       string  `json:"name"`
	KeyPrefix  string  `json:"key_prefix"`
	CreatedAt  string  `json:"created_at"`
	LastUsedAt *string `json:"last_used_at,omitempty"`
}

type apiKeyNameRequest struct {
	Name string `json:"name"`
}

type apiKeyCreateResponse struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	KeyPrefix string `json:"key_prefix"`
	Key       string `json:"key"`
	CreatedAt string `json:"created_at"`
}

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
	plaintext, record, err := storage.CreateOrRotateAPIKey(userID, name)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to create API key.")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(apiKeyCreateResponse{
		ID:        record.ID,
		Name:      record.Name,
		KeyPrefix: record.KeyPrefix,
		Key:       plaintext,
		CreatedAt: record.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	})
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
	}
	if k.LastUsedAt != nil {
		s := k.LastUsedAt.Format("2006-01-02T15:04:05Z07:00")
		out.LastUsedAt = &s
	}
	return out
}
