package handlers

import (
	"crypto/subtle"
	"net/http"
	"os"
	"runtime"
	"strings"

	"GoTodo/internal/live"
	"GoTodo/internal/metrics"
	"GoTodo/internal/storage"
	"GoTodo/internal/version"
)

// Metrics serves Prometheus metrics at /metrics. It is off (404) unless
// METRICS_TOKEN is set, and then needs "Authorization: Bearer <token>".
func Metrics(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(os.Getenv("METRICS_TOKEN"))
	if token == "" {
		http.NotFound(w, r)
		return
	}
	got := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
		w.Header().Set("WWW-Authenticate", `Bearer realm="metrics"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	gauges := []metrics.Gauge{
		{Name: "ordryn_build_info", Help: "Build version (always 1).", Labels: map[string]string{"version": version.Version}, Value: 1},
		{Name: "ordryn_live_streams", Help: "Live-update (SSE) streams this process is serving.", Value: float64(live.SubscriberCount())},
		{Name: "ordryn_go_goroutines", Help: "Goroutines.", Value: float64(runtime.NumGoroutine())},
		{Name: "ordryn_go_heap_alloc_bytes", Help: "Heap bytes in use.", Value: float64(ms.HeapAlloc)},
	}
	if c, err := storage.GetMetricsCounts(); err == nil {
		gauges = append(gauges,
			metrics.Gauge{Name: "ordryn_users", Help: "People accounts.", Labels: map[string]string{"state": "active"}, Value: float64(c.ActiveUsers)},
			metrics.Gauge{Name: "ordryn_users", Help: "People accounts.", Labels: map[string]string{"state": "deactivated"}, Value: float64(c.DeactivatedUsers)},
			metrics.Gauge{Name: "ordryn_projects", Help: "Projects.", Labels: map[string]string{"state": "live"}, Value: float64(c.Projects)},
			metrics.Gauge{Name: "ordryn_projects", Help: "Projects.", Labels: map[string]string{"state": "archived"}, Value: float64(c.ArchivedProjects)},
			metrics.Gauge{Name: "ordryn_tasks", Help: "Tasks not in Trash.", Labels: map[string]string{"state": "open"}, Value: float64(c.OpenTasks)},
			metrics.Gauge{Name: "ordryn_tasks", Help: "Tasks not in Trash.", Labels: map[string]string{"state": "done"}, Value: float64(c.DoneTasks)},
		)
		for status, n := range c.Deliveries {
			gauges = append(gauges, metrics.Gauge{Name: "ordryn_hook_deliveries", Help: "Extension hook deliveries by status (dead = gave up).",
				Labels: map[string]string{"status": status}, Value: float64(n)})
		}
		for status, n := range c.AgentRuns {
			gauges = append(gauges, metrics.Gauge{Name: "ordryn_agent_runs", Help: "AI agent runs by status.",
				Labels: map[string]string{"status": status}, Value: float64(n)})
		}
		gauges = append(gauges, metrics.Gauge{Name: "ordryn_database_up", Help: "1 when the database answered this scrape.", Value: 1})
	} else {
		gauges = append(gauges, metrics.Gauge{Name: "ordryn_database_up", Help: "1 when the database answered this scrape.", Value: 0})
	}
	metrics.Write(w, gauges)
}
