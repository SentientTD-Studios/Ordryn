package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"GoTodo/internal/live"
	"GoTodo/internal/server/utils"
)

func TestAPIV1EventsMethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v2/events", nil)
	req = utils.SetAPIUserID(req, 1)
	rec := httptest.NewRecorder()
	APIV1Events(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestAPIV1EventsUnauthorized(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v2/events", nil)
	rec := httptest.NewRecorder()
	APIV1Events(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAPIV1EventsReadyAndUpdate(t *testing.T) {
	live.Init(nil)
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/v2/events", nil).WithContext(ctx)
	req = utils.SetAPIUserID(req, 9)
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		APIV1Events(rec, req)
		close(done)
	}()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(rec.Body.String(), "event: ready") {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !strings.Contains(rec.Body.String(), "event: ready") {
		cancel()
		<-done
		t.Fatalf("missing ready event: %q", rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		cancel()
		<-done
		t.Fatalf("content-type = %q", ct)
	}

	live.Push(live.Event{Type: live.TypeTaskCreated, TaskID: 4, ActorID: 1}, []int{9})

	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(rec.Body.String(), "event: task-update") && strings.Contains(rec.Body.String(), "task.created") {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	body := rec.Body.String()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("handler did not return after cancel")
	}
	if !strings.Contains(body, "event: task-update") {
		t.Fatalf("missing task-update: %q", body)
	}
	if !strings.Contains(body, `"type":"task.created"`) {
		t.Fatalf("missing payload: %q", body)
	}
}
