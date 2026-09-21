package handlers

import (
	"fmt"
	"net/http"
	"time"

	"GoTodo/internal/live"
	"GoTodo/internal/server/utils"
)

const livePingInterval = 25 * time.Second

// APIV1Events streams task/project invalidation events for the current user.
// GET /api/v2/events (text/event-stream)
func APIV1Events(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Streaming unsupported.")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ch := live.SubscribeUser(r.Context(), userID)
	_, _ = fmt.Fprint(w, "event: ready\ndata: {}\n\n")
	flusher.Flush()
	ticker := time.NewTicker(livePingInterval)
	defer ticker.Stop()

	for {
		select {
		case payload, ok := <-ch:
			if !ok {
				return
			}
			_, _ = fmt.Fprintf(w, "event: task-update\ndata: %s\n\n", payload)
			flusher.Flush()
		case <-ticker.C:
			_, _ = fmt.Fprint(w, "event: ping\ndata: {}\n\n")
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}
