package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"GoTodo/internal/domain"
	"GoTodo/internal/server/utils"
)

func TestApplyRelativeReorder(t *testing.T) {
	all := []int{1, 2, 3, 4, 5, 6}
	got, err := domain.ApplyRelativeReorder(all, []int{3, 1, 2})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []int{3, 1, 2, 4, 5, 6}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("full page = %v, want %v", got, want)
	}

	// Filtered subset (e.g. incomplete-only): keep completed slots, reorder subset.
	all = []int{10, 20, 30, 40, 50} // 10,40 completed; 20,30,50 incomplete
	got, err = domain.ApplyRelativeReorder(all, []int{50, 20, 30})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want = []int{10, 50, 20, 40, 30}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("subset = %v, want %v", got, want)
	}

	got, err = domain.ApplyRelativeReorder(all, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(got, all) {
		t.Fatalf("empty order mutated list: %v", got)
	}
}

func TestAPIV1ReorderUnauthorized(t *testing.T) {
	body := bytes.NewBufferString(`{"task_ids":[1,2],"favorite":false}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/reorder", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	APIV1TasksRouter(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
	var payload map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload["error"] != "unauthorized" {
		t.Fatalf("error = %q, want unauthorized", payload["error"])
	}
}

func TestAPIV1ReorderInvalidBody(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "missing task_ids", body: `{"favorite":false}`},
		{name: "empty task_ids", body: `{"task_ids":[],"favorite":false}`},
		{name: "missing favorite", body: `{"task_ids":[1,2]}`},
		{name: "invalid json", body: `{`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/reorder", bytes.NewBufferString(tt.body))
			req.Header.Set("Content-Type", "application/json")
			req = utils.SetAPIUserID(req, 1)
			rec := httptest.NewRecorder()

			APIV1TasksRouter(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
			}
		})
	}
}

func TestAPIV1ReorderMethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/reorder", nil)
	req = utils.SetAPIUserID(req, 1)
	rec := httptest.NewRecorder()

	APIV1TasksRouter(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}
