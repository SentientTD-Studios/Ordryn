package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
	"GoTodo/internal/version"
)

// healthCheckTimeout bounds each dependency check so a starved database pool
// or a hung Redis makes /health answer quickly with ok=false instead of
// hanging like everything else.
const healthCheckTimeout = 2 * time.Second

// APIV1Health is a public readiness probe for API clients and uptime monitors
// (no auth). It only says whether each dependency answered; the detailed
// numbers are admin-only at /api/v2/admin/system/health.
// GET /api/v2/health → { version, api_enabled, redis_ok, db_ok, mode }
func APIV1Health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	dbCtx, cancelDB := context.WithTimeout(r.Context(), healthCheckTimeout)
	defer cancelDB()
	dbOK := storage.PingDatabase(dbCtx) == nil

	redisCtx, cancelRedis := context.WithTimeout(r.Context(), healthCheckTimeout)
	defer cancelRedis()
	_, redisErr := utils.RedisPing(redisCtx)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"version":     version.Version,
		"api_enabled": utils.IsAPIEnabled(),
		"redis_ok":    redisErr == nil,
		"db_ok":       dbOK,
		"mode":        utils.GetRuntimeMode(),
	})
}
