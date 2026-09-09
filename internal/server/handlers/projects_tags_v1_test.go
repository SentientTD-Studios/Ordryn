package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"GoTodo/internal/server/utils"
)

func TestAPIV1ProjectsUnauthorized(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil)
	rec := httptest.NewRecorder()
	APIV1ProjectsRouter(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestAPIV1ProjectsCreateValidation(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "empty json object", body: `{}`},
		{name: "empty name", body: `{"name":""}`},
		{name: "whitespace name", body: `{"name":"   "}`},
		{name: "too long", body: `{"name":"` + strings.Repeat("a", 51) + `"}`},
		{name: "invalid json", body: `{`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/projects", bytes.NewBufferString(tt.body))
			req.Header.Set("Content-Type", "application/json")
			req = utils.SetAPIUserID(req, 1)
			rec := httptest.NewRecorder()
			APIV1ProjectsRouter(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
			}
		})
	}
}

func TestAPIV1ProjectsPatchValidation(t *testing.T) {
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/projects/1", bytes.NewBufferString(`{"name":""}`))
	req.Header.Set("Content-Type", "application/json")
	req = utils.SetAPIUserID(req, 1)
	rec := httptest.NewRecorder()
	APIV1ProjectsRouter(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestAPIV1ProjectsPatchNothingToUpdate(t *testing.T) {
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/projects/1", bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req = utils.SetAPIUserID(req, 1)
	rec := httptest.NewRecorder()
	APIV1ProjectsRouter(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestAPIV1ProjectsReorderValidation(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "empty ids", body: `{"project_ids":[]}`},
		{name: "missing ids", body: `{}`},
		{name: "invalid json", body: `{`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/reorder", bytes.NewBufferString(tt.body))
			req.Header.Set("Content-Type", "application/json")
			req = utils.SetAPIUserID(req, 1)
			rec := httptest.NewRecorder()
			APIV1ProjectsRouter(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
			}
		})
	}
}

func TestAPIV1ProjectsReorderMethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/reorder", nil)
	req = utils.SetAPIUserID(req, 1)
	rec := httptest.NewRecorder()
	APIV1ProjectsRouter(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestAPIV1ProjectsCreateDescriptionTooLong(t *testing.T) {
	body := `{"name":"ok","description":"` + strings.Repeat("d", 1001) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = utils.SetAPIUserID(req, 1)
	rec := httptest.NewRecorder()
	APIV1ProjectsRouter(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestAPIV1ProjectsInvalidID(t *testing.T) {
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/projects/abc", nil)
	req = utils.SetAPIUserID(req, 1)
	rec := httptest.NewRecorder()
	APIV1ProjectsRouter(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestAPIV1ProjectsMethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodPut, "/api/v1/projects", nil)
	req = utils.SetAPIUserID(req, 1)
	rec := httptest.NewRecorder()
	APIV1ProjectsRouter(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestAPIV1ProjectsArchiveMethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/1/archive", nil)
	req = utils.SetAPIUserID(req, 1)
	rec := httptest.NewRecorder()
	APIV1ProjectsRouter(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestAPIV1ProjectsRestoreMethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/1/restore", nil)
	req = utils.SetAPIUserID(req, 1)
	rec := httptest.NewRecorder()
	APIV1ProjectsRouter(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestAPIV1ProjectsArchiveUnauthorized(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/1/archive", nil)
	rec := httptest.NewRecorder()
	APIV1ProjectsRouter(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestAPIV1TagsPatchValidation(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "empty name", body: `{"name":""}`},
		{name: "too long name", body: `{"name":"` + strings.Repeat("t", 51) + `"}`},
		{name: "too long color", body: `{"color":"` + strings.Repeat("c", 21) + `"}`},
		{name: "empty json object", body: `{}`},
		{name: "invalid json", body: `{`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPatch, "/api/v1/tags/1", bytes.NewBufferString(tt.body))
			req.Header.Set("Content-Type", "application/json")
			req = utils.SetAPIUserID(req, 1)
			rec := httptest.NewRecorder()
			APIV1TagsRouter(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
			}
			var payload map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if payload["error"] != "invalid_request" {
				t.Fatalf("error = %q, want invalid_request", payload["error"])
			}
		})
	}
}

func TestAPIV1TagsPatchMethodRequiresID(t *testing.T) {
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/tags", bytes.NewBufferString(`{"name":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	req = utils.SetAPIUserID(req, 1)
	rec := httptest.NewRecorder()
	APIV1TagsRouter(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestAPIV1ProjectsBacklogSprintEndpoints(t *testing.T) {
	t.Run("patch project with blank backlog_name", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/projects/1", bytes.NewBufferString(`{"backlog_name":"   "}`))
		req.Header.Set("Content-Type", "application/json")
		req = utils.SetAPIUserID(req, 1)
		rec := httptest.NewRecorder()
		APIV1ProjectsRouter(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
		}
	})

	t.Run("patch backlog sprint with dates rejected", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/projects/1/sprints/backlog", bytes.NewBufferString(`{"name":"Icebox","start_date":"2026-10-01"}`))
		req.Header.Set("Content-Type", "application/json")
		req = utils.SetAPIUserID(req, 1)
		rec := httptest.NewRecorder()
		APIV1ProjectsRouter(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "Dates cannot be set") {
			t.Fatalf("expected Dates cannot be set error, got: %s", rec.Body.String())
		}
	})

	t.Run("patch project with too long backlog_description", func(t *testing.T) {
		tooLong := strings.Repeat("d", 81)
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/projects/1", bytes.NewBufferString(`{"backlog_description":"`+tooLong+`"}`))
		req.Header.Set("Content-Type", "application/json")
		req = utils.SetAPIUserID(req, 1)
		rec := httptest.NewRecorder()
		APIV1ProjectsRouter(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
		}
	})

	t.Run("patch backlog sprint with nothing to update", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/projects/1/sprints/backlog", bytes.NewBufferString(`{}`))
		req.Header.Set("Content-Type", "application/json")
		req = utils.SetAPIUserID(req, 1)
		rec := httptest.NewRecorder()
		APIV1ProjectsRouter(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "Nothing to update") {
			t.Fatalf("expected Nothing to update error, got: %s", rec.Body.String())
		}
	})

	t.Run("delete backlog sprint rejected", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/projects/1/sprints/backlog", nil)
		req = utils.SetAPIUserID(req, 1)
		rec := httptest.NewRecorder()
		APIV1ProjectsRouter(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "cannot be deleted") {
			t.Fatalf("expected cannot be deleted error, got: %s", rec.Body.String())
		}
	})
}

