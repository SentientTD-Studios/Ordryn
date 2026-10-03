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

type apiProjectKeyJSON struct {
	ID          int      `json:"id"`
	Name        string   `json:"name"`
	KeyPrefix   string   `json:"key_prefix"`
	Scopes      []string `json:"scopes"`
	CreatedAt   string   `json:"created_at"`
	LastUsedAt  *string  `json:"last_used_at"`
	ExpiresAt   *string  `json:"expires_at"`
	Expired     bool     `json:"expired"`
	CreatedByID int      `json:"created_by_id"`
	CreatedBy   string   `json:"created_by,omitempty"`
}

type apiProjectKeyCreateRequest struct {
	Name      string   `json:"name"`
	Scopes    []string `json:"scopes"`
	ExpiresAt *string  `json:"expires_at"`
}

type apiProjectKeyCreateResponse struct {
	apiProjectKeyJSON
	Key string `json:"key"`
}

// apiV1ProjectAPIKeys handles /api/v2/projects/{id}/api-keys[/{keyId}].
func apiV1ProjectAPIKeys(w http.ResponseWriter, r *http.Request, projectID int, rest []string) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	if len(rest) == 0 {
		switch r.Method {
		case http.MethodGet:
			keys, err := domain.ListProjectAPIKeys(userID, projectID)
			if err != nil {
				writeProjectAPIKeyError(w, err)
				return
			}
			out := make([]apiProjectKeyJSON, 0, len(keys))
			for _, k := range keys {
				out = append(out, projectKeyToJSON(k))
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_ = json.NewEncoder(w).Encode(out)
		case http.MethodPost:
			apiV1CreateProjectAPIKey(w, r, userID, projectID)
		default:
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		}
		return
	}
	keyID, err := strconv.Atoi(rest[0])
	if len(rest) != 1 || err != nil || keyID <= 0 {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid API key id.")
		return
	}
	if r.Method != http.MethodDelete {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	if err := domain.RevokeProjectAPIKey(userID, projectID, keyID); err != nil {
		writeProjectAPIKeyError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func apiV1CreateProjectAPIKey(w http.ResponseWriter, r *http.Request, userID, projectID int) {
	if !utils.RedisAvailable() {
		utils.APIJSONError(w, http.StatusServiceUnavailable, "api_unavailable", "Redis is required for the REST API.")
		return
	}
	var req apiProjectKeyCreateRequest
	if err := decodeJSONBody(r, &req); err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
		return
	}
	in := domain.CreateProjectAPIKeyInput{Name: req.Name, Scopes: req.Scopes}
	if req.ExpiresAt != nil && strings.TrimSpace(*req.ExpiresAt) != "" {
		t, err := time.Parse(time.RFC3339, strings.TrimSpace(*req.ExpiresAt))
		if err != nil {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "expires_at must be an RFC 3339 timestamp.")
			return
		}
		in.ExpiresAt = &t
	}
	plaintext, rec, err := domain.CreateProjectAPIKey(userID, projectID, in)
	if err != nil {
		writeProjectAPIKeyError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(apiProjectKeyCreateResponse{
		apiProjectKeyJSON: projectKeyToJSON(*rec),
		Key:               plaintext,
	})
}

func projectKeyToJSON(k storage.APIKey) apiProjectKeyJSON {
	scopes := k.Scopes
	if scopes == nil {
		scopes = []string{}
	}
	return apiProjectKeyJSON{
		ID:          k.ID,
		Name:        k.Name,
		KeyPrefix:   k.KeyPrefix,
		Scopes:      scopes,
		CreatedAt:   formatRFC3339(k.CreatedAt),
		LastUsedAt:  optionalRFC3339(k.LastUsedAt),
		ExpiresAt:   optionalRFC3339(k.ExpiresAt),
		Expired:     k.ExpiresAt != nil && !k.ExpiresAt.After(time.Now()),
		CreatedByID: k.UserID,
		CreatedBy:   k.CreatorName,
	}
}

func writeProjectAPIKeyError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Not found.")
	case errors.Is(err, domain.ErrForbidden):
		utils.APIJSONError(w, http.StatusForbidden, "forbidden", "Only project managers can manage project API keys.")
	case errors.Is(err, domain.ErrConflict):
		utils.APIJSONError(w, http.StatusConflict, "conflict", sharingClientMessage(err, "Conflict."))
	case errors.Is(err, domain.ErrValidation):
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", sharingClientMessage(err, "Invalid request."))
	default:
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Request failed.")
	}
}
