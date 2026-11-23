package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"GoTodo/internal/server/utils"
	"GoTodo/internal/statestore"
	"GoTodo/internal/storage"
	"GoTodo/internal/version"
)

// healthCheckTimeout bounds each dependency check so a starved database pool
// or a hung state store makes /health answer quickly with ok=false instead of
// hanging like everything else.
const healthCheckTimeout = 2 * time.Second

// APIV1Health is a public readiness probe for API clients and uptime monitors
// (no auth). It only says whether each dependency answered; the detailed
// numbers are admin-only at /api/v2/admin/system/health.
// GET /api/v2/health → { version, api_enabled, state_store, state_store_ok, redis_ok, db_ok, mode }
// redis_ok is a deprecated alias of state_store_ok, kept for older clients.
func APIV1Health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	dbCtx, cancelDB := context.WithTimeout(r.Context(), healthCheckTimeout)
	defer cancelDB()
	dbOK := storage.PingDatabase(dbCtx) == nil

	storeCtx, cancelStore := context.WithTimeout(r.Context(), healthCheckTimeout)
	defer cancelStore()
	_, storeErr := statestore.Ping(storeCtx)
	storeOK := storeErr == nil

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"version":        version.Version,
		"api_enabled":    utils.IsAPIEnabled(),
		"state_store":    stateStoreBackend(),
		"state_store_ok": storeOK,
		"redis_ok":       storeOK,
		"db_ok":          dbOK,
		"mode":           utils.GetRuntimeMode(),
	})
}

// stateStoreBackend names the connected state store ("redis" or "postgres"),
// or "" when none is connected.
func stateStoreBackend() string {
	if s := statestore.Current(); s != nil {
		return s.Backend()
	}
	return ""
}
