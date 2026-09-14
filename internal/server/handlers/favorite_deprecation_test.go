package handlers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"GoTodo/internal/server/utils"
)

func TestSetFavoriteDeprecationNotice(t *testing.T) {
	rec := httptest.NewRecorder()
	setFavoriteDeprecationNotice(rec)
	if got := rec.Header().Get("Deprecation"); got != "true" {
		t.Fatalf("Deprecation header = %q, want true", got)
	}
	warning := rec.Header().Get("Warning")
	if !strings.Contains(warning, "299") {
		t.Fatalf("Warning header %q missing warn-code 299", warning)
	}
	if !strings.Contains(warning, "v2") {
		t.Fatalf("Warning header %q missing v2 removal notice", warning)
	}
	if !strings.Contains(warning, FavoriteDeprecationMessage) {
		t.Fatalf("Warning header %q missing deprecation message", warning)
	}
}

func TestFavoriteDeprecationNoticeIfUsed(t *testing.T) {
	t.Run("unused leaves headers empty", func(t *testing.T) {
		rec := httptest.NewRecorder()
		if got := favoriteDeprecationNoticeIfUsed(rec, false); got != "" {
			t.Fatalf("notice = %q, want empty", got)
		}
		if rec.Header().Get("Deprecation") != "" {
			t.Fatalf("unexpected Deprecation header %q", rec.Header().Get("Deprecation"))
		}
		if rec.Header().Get("Warning") != "" {
			t.Fatalf("unexpected Warning header %q", rec.Header().Get("Warning"))
		}
	})
	t.Run("used sets headers and notice", func(t *testing.T) {
		rec := httptest.NewRecorder()
		if got := favoriteDeprecationNoticeIfUsed(rec, true); got != FavoriteDeprecationMessage {
			t.Fatalf("notice = %q, want %q", got, FavoriteDeprecationMessage)
		}
		if rec.Header().Get("Deprecation") != "true" {
			t.Fatal("expected Deprecation: true")
		}
	})
}

func TestCreateTaskFavoriteDeprecationHeaders(t *testing.T) {
	t.Run("favorite field sets deprecation headers", func(t *testing.T) {
		rec := postAuthenticatedTasks(t, http.MethodPost, "/api/v1/tasks", `{"title":"Star me","favorite":true}`)
		if rec.Header().Get("Deprecation") != "true" {
			t.Fatalf("Deprecation = %q, want true; status=%d body=%s", rec.Header().Get("Deprecation"), rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Header().Get("Warning"), "v2") {
			t.Fatalf("Warning = %q, want v2 notice", rec.Header().Get("Warning"))
		}
	})
	t.Run("omitted favorite does not deprecate", func(t *testing.T) {
		rec := postAuthenticatedTasks(t, http.MethodPost, "/api/v1/tasks", `{"title":"Plain"}`)
		if rec.Header().Get("Deprecation") != "" {
			t.Fatalf("unexpected Deprecation header %q on request without favorite", rec.Header().Get("Deprecation"))
		}
	})
}

func TestPatchTaskFavoriteDeprecationHeaders(t *testing.T) {
	t.Run("favorite field sets deprecation headers", func(t *testing.T) {
		rec := postAuthenticatedTasks(t, http.MethodPatch, "/api/v1/tasks/1", `{"favorite":false}`)
		if rec.Header().Get("Deprecation") != "true" {
			t.Fatalf("Deprecation = %q, want true; status=%d body=%s", rec.Header().Get("Deprecation"), rec.Code, rec.Body.String())
		}
	})
	t.Run("title-only patch does not deprecate", func(t *testing.T) {
		rec := postAuthenticatedTasks(t, http.MethodPatch, "/api/v1/tasks/1", `{"title":"Keep"}`)
		if rec.Header().Get("Deprecation") != "" {
			t.Fatalf("unexpected Deprecation header %q", rec.Header().Get("Deprecation"))
		}
	})
}

func TestReorderFavoriteGroupDeprecationHeaders(t *testing.T) {
	t.Run("favorite true sets deprecation headers", func(t *testing.T) {
		rec := postAuthenticatedTasks(t, http.MethodPost, "/api/v1/tasks/reorder", `{"task_ids":[1,2],"favorite":true}`)
		if rec.Header().Get("Deprecation") != "true" {
			t.Fatalf("Deprecation = %q, want true; status=%d body=%s", rec.Header().Get("Deprecation"), rec.Code, rec.Body.String())
		}
	})
	t.Run("favorite false does not deprecate", func(t *testing.T) {
		rec := postAuthenticatedTasks(t, http.MethodPost, "/api/v1/tasks/reorder", `{"task_ids":[1,2],"favorite":false}`)
		if rec.Header().Get("Deprecation") != "" {
			t.Fatalf("unexpected Deprecation header %q for non-favorite reorder", rec.Header().Get("Deprecation"))
		}
	})
}

func postAuthenticatedTasks(t *testing.T, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = utils.SetAPIUserID(req, 1)
	rec := httptest.NewRecorder()
	APIV1TasksRouter(rec, req)
	return rec
}
