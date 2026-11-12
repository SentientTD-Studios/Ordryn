package domain

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
)

func TestMaintenanceGuard(t *testing.T) {
	setupCompanySite(t, "gotodo_maintenance_test")
	t.Cleanup(func() { _, _ = storage.SetMaintenance(false, "", 0) })

	ok := utils.MaintenanceGuard(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	do := func(method, path string) int {
		rec := httptest.NewRecorder()
		ok(rec, httptest.NewRequest(method, path, nil))
		return rec.Code
	}

	if got := do(http.MethodPost, "/api/v2/tasks"); got != http.StatusNoContent {
		t.Fatalf("off: %d", got)
	}
	m, err := storage.SetMaintenance(true, "Upgrading to v5", 0)
	if err != nil || !m.Enabled || m.StartedAt == nil {
		t.Fatalf("enable: %+v %v", m, err)
	}
	if got := do(http.MethodPost, "/api/v2/tasks"); got != http.StatusServiceUnavailable {
		t.Fatalf("anonymous write during maintenance: %d", got)
	}
	if got := do(http.MethodGet, "/api/v2/tasks"); got != http.StatusNoContent {
		t.Fatalf("reads stay open: %d", got)
	}
	if got := do(http.MethodPost, "/api/v2/auth/login"); got != http.StatusNoContent {
		t.Fatalf("sign-in stays open: %d", got)
	}
	if _, err := storage.SetMaintenance(false, "", 0); err != nil {
		t.Fatal(err)
	}
	if got := do(http.MethodPost, "/api/v2/tasks"); got != http.StatusNoContent {
		t.Fatalf("after: %d", got)
	}
}
