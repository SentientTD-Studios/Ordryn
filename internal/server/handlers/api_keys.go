package handlers

import (
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

// APIProfileKeysJSON returns the user's API keys (when API is enabled).
func APIProfileKeysJSON(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !utils.IsAPIEnabled() {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]struct{}{})
		return
	}
	uid, ok := requireProfileUser(w, r)
	if !ok {
		return
	}
	keys, err := storage.ListAPIKeysForUser(*uid)
	if err != nil {
		http.Error(w, "Failed to load API keys", http.StatusInternalServerError)
		return
	}
	type keyOut struct {
		ID         int     `json:"id"`
		Name       string  `json:"name"`
		KeyPrefix  string  `json:"key_prefix"`
		CreatedAt  string  `json:"created_at"`
		LastUsedAt *string `json:"last_used_at,omitempty"`
	}
	out := make([]keyOut, 0, len(keys))
	for _, k := range keys {
		ko := keyOut{
			ID:        k.ID,
			Name:      k.Name,
			KeyPrefix: k.KeyPrefix,
			CreatedAt: k.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		}
		if k.LastUsedAt != nil {
			s := k.LastUsedAt.Format("2006-01-02T15:04:05Z07:00")
			ko.LastUsedAt = &s
		}
		out = append(out, ko)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(out)
}

// APICreateAPIKey creates a new named API key and returns the plaintext once.
func APICreateAPIKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !utils.IsAPIEnabled() {
		utils.APIJSONError(w, http.StatusForbidden, "api_disabled", "The REST API is not enabled.")
		return
	}
	if !utils.RedisAvailable() {
		utils.APIJSONError(w, http.StatusServiceUnavailable, "api_unavailable", "Redis is required for the REST API.")
		return
	}
	uid, ok := requireProfileUser(w, r)
	if !ok {
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Key name is required.")
		return
	}
	if len(name) > 80 {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Key name is too long.")
		return
	}
	plaintext, record, err := storage.CreateOrRotateAPIKey(*uid, name)
	if err != nil {
		http.Error(w, "Failed to create API key", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":         record.ID,
		"name":       record.Name,
		"key_prefix": record.KeyPrefix,
		"key":        plaintext,
		"created_at": record.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	})
}

// APIRevokeAPIKey revokes an API key owned by the user.
func APIRevokeAPIKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	uid, ok := requireProfileUser(w, r)
	if !ok {
		return
	}
	idStr := strings.TrimSpace(r.FormValue("id"))
	id, err := strconv.Atoi(idStr)
	if err != nil || id <= 0 {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid key id.")
		return
	}
	if err := storage.RevokeAPIKey(id, *uid); err != nil {
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "API key not found.")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]string{"status": "revoked"})
}

func requireProfileUser(w http.ResponseWriter, r *http.Request) (*int, bool) {
	_, _, _, loggedIn := utils.GetSessionUser(r)
	if !loggedIn {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
		return nil, false
	}
	uid := utils.GetSessionUserID(r)
	if uid == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
		return nil, false
	}
	return uid, true
}

// ListAPIKeysForProfilePage loads keys for the profile template.
func ListAPIKeysForProfilePage(userID int) []map[string]interface{} {
	if !utils.IsAPIEnabled() {
		return nil
	}
	keys, err := storage.ListAPIKeysForUser(userID)
	if err != nil {
		return nil
	}
	out := make([]map[string]interface{}, 0, len(keys))
	for _, k := range keys {
		entry := map[string]interface{}{
			"ID":        k.ID,
			"Name":      k.Name,
			"KeyPrefix": k.KeyPrefix,
			"CreatedAt": k.CreatedAt.Format("Jan 2, 2006"),
		}
		if k.LastUsedAt != nil {
			entry["LastUsedAt"] = k.LastUsedAt.Format("Jan 2, 2006 15:04")
		}
		out = append(out, entry)
	}
	return out
}

// GetUserTimezoneByID returns the user's timezone or UTC on failure.
func GetUserTimezoneByID(userID int) string {
	pool, err := storage.OpenDatabase()
	if err != nil {
		return "UTC"
	}
	defer storage.CloseDatabase(pool)
	var tz string
	if err := pool.QueryRow(context.Background(),
		`SELECT COALESCE(NULLIF(timezone, ''), 'UTC') FROM users WHERE id = $1`, userID).Scan(&tz); err != nil {
		return "UTC"
	}
	return tz
}
