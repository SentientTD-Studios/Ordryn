package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"GoTodo/internal/server/utils"
	"GoTodo/internal/statestore"
	"GoTodo/internal/storage"
)

// healthCheckTimeout bounds each dependency check so a starved database pool
// or a hung state store makes /health answer quickly with ok=false instead of
// hanging like everything else.
const healthCheckTimeout = 2 * time.Second

// APIV1Health is a public readiness probe for uptime monitors (no auth). It
// answers only up or down: 200 {"ok":true} when the database and state store
// both answered, 503 {"ok":false} otherwise. The version, backends and
// per-dependency results are admin-only at /api/v2/admin/system/health, so an
// anonymous caller learns nothing about what the server runs.
// GET /api/v2/health → { ok }
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

	ok := dbOK && storeOK
	status := http.StatusOK
	if !ok {
		status = http.StatusServiceUnavailable
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": ok})
}

// stateStoreBackend names the connected state store ("redis" or "postgres"),
// or "" when none is connected.
func stateStoreBackend() string {
	if s := statestore.Current(); s != nil {
		return s.Backend()
	}
	return ""
}
