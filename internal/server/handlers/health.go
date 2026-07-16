package handlers

import (
	"encoding/json"
	"net/http"

	"GoTodo/internal/server/utils"
	"GoTodo/internal/version"
)

// APIV1Health is a public readiness probe for API clients (no auth).
// GET /api/v1/health → { version, api_enabled, redis_ok, mode }
func APIV1Health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"version":     version.Version,
		"api_enabled": utils.IsAPIEnabled(),
		"redis_ok":    utils.RedisAvailable(),
		"mode":        utils.GetRuntimeMode(),
	})
}
