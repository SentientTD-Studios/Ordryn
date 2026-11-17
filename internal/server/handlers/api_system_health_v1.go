package handlers

import (
	"context"
	"net/http"
	"runtime"
	"time"

	"GoTodo/internal/live"
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
	"GoTodo/internal/version"
	"GoTodo/internal/workers"
)

// APIV1AdminSystemHealth handles GET /api/v2/admin/system/health (admins
// only): database pool and Redis latency, process stats, and the state of each
// background worker. Backups, the audit stream and automation rules report
// their own status on their admin pages, so they aren't repeated here beyond
// the scheduled-backup worker's run state.
func APIV1AdminSystemHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}

	dbCtx, cancelDB := context.WithTimeout(r.Context(), healthCheckTimeout)
	defer cancelDB()
	db := storage.SharedPoolStats(dbCtx)

	redisCtx, cancelRedis := context.WithTimeout(r.Context(), healthCheckTimeout)
	defer cancelRedis()
	redisLatency, redisErr := utils.RedisPing(redisCtx)
	redis := map[string]interface{}{
		"ok":      redisErr == nil,
		"ping_ms": float64(redisLatency.Microseconds()) / 1000,
	}
	if redisErr != nil {
		redis["error"] = redisErr.Error()
	}

	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	started := workers.ProcessStart()

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"checked_at": time.Now().UTC(),
		"process": map[string]interface{}{
			"version":          version.Version,
			"started_at":       started.UTC(),
			"uptime_seconds":   int64(time.Since(started).Seconds()),
			"goroutines":       runtime.NumGoroutine(),
			"heap_alloc_bytes": ms.HeapAlloc,
			"sys_bytes":        ms.Sys,
			"gc_cycles":        ms.NumGC,
			"live_streams":     live.SubscriberCount(),
		},
		"database": db,
		"redis":    redis,
		"workers":  workers.Snapshots(),
	})
}
