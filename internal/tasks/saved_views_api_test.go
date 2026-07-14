package tasks_test

import (
	"GoTodo/internal/server/handlers"
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type savedViewResponse struct {
	ID        int                     `json:"id"`
	Name      string                  `json:"name"`
	Filter    storage.SavedViewFilter `json:"filter"`
	SortOrder int                     `json:"sort_order"`
	CreatedAt time.Time               `json:"created_at"`
	UpdatedAt time.Time               `json:"updated_at"`
}

func TestAPIV1SavedViewsCRUD(t *testing.T) {
	create := performSavedViewRequest(t, http.MethodPost, "/api/v1/saved-views", `{
		"name": "  Overdue work  ",
		"filter": {
			"project": "1",
			"status": "incomplete",
			"due": "overdue",
			"priority": "2",
			"tag": "1",
			"sort": "priority",
			"search": "  release  "
		}
	}`, 1, "user@example.com")
	assertSavedViewStatus(t, create, http.StatusCreated)

	var created savedViewResponse
	decodeSavedViewResponse(t, create, &created)
	if created.ID <= 0 || created.Name != "Overdue work" {
		t.Fatalf("unexpected created view: %+v", created)
	}
	if created.Filter.Search != "release" || created.Filter.Due != "overdue" {
		t.Fatalf("filter was not normalized: %+v", created.Filter)
	}
	if location := create.Header().Get("Location"); location != fmt.Sprintf("/api/v1/saved-views/%d", created.ID) {
		t.Fatalf("unexpected Location header %q", location)
	}

	duplicate := performSavedViewRequest(t, http.MethodPost, "/api/v1/saved-views",
		`{"name":"Overdue work","filter":{}}`, 1, "user@example.com")
	assertSavedViewStatus(t, duplicate, http.StatusConflict)
	assertSavedViewErrorCode(t, duplicate, "name_conflict")

	list := performSavedViewRequest(t, http.MethodGet, "/api/v1/saved-views", "", 1, "user@example.com")
	assertSavedViewStatus(t, list, http.StatusOK)
	var views []savedViewResponse
	decodeSavedViewResponse(t, list, &views)
	if len(views) != 1 || views[0].ID != created.ID {
		t.Fatalf("unexpected saved view list: %+v", views)
	}

	otherUserList := performSavedViewRequest(t, http.MethodGet, "/api/v1/saved-views", "", 2, "other@example.com")
	assertSavedViewStatus(t, otherUserList, http.StatusOK)
	var otherViews []savedViewResponse
	decodeSavedViewResponse(t, otherUserList, &otherViews)
	if len(otherViews) != 0 {
		t.Fatalf("another user can see saved views: %+v", otherViews)
	}

	get := performSavedViewRequest(t, http.MethodGet,
		fmt.Sprintf("/api/v1/saved-views/%d", created.ID), "", 1, "user@example.com")
	assertSavedViewStatus(t, get, http.StatusOK)

	otherUserGet := performSavedViewRequest(t, http.MethodGet,
		fmt.Sprintf("/api/v1/saved-views/%d", created.ID), "", 2, "other@example.com")
	assertSavedViewStatus(t, otherUserGet, http.StatusNotFound)

	incompletePut := performSavedViewRequest(t, http.MethodPut,
		fmt.Sprintf("/api/v1/saved-views/%d", created.ID), `{"name":"Today"}`, 1, "user@example.com")
	assertSavedViewStatus(t, incompletePut, http.StatusBadRequest)

	update := performSavedViewRequest(t, http.MethodPut,
		fmt.Sprintf("/api/v1/saved-views/%d", created.ID), `{
			"name": "Today",
			"filter": {
				"project": "none",
				"status": "completed",
				"due": "TODAY",
				"completed": "WEEK",
				"priority": "3",
				"tag": "1",
				"sort": "PRIORITY",
				"search": ""
			},
			"sort_order": 4
		}`, 1, "user@example.com")
	assertSavedViewStatus(t, update, http.StatusOK)

	var updated savedViewResponse
	decodeSavedViewResponse(t, update, &updated)
	if updated.Name != "Today" || updated.Filter.Status != "complete" ||
		updated.Filter.Completed != "week" ||
		updated.Filter.Due != "today" || updated.Filter.Sort != "priority" ||
		updated.SortOrder != 4 {
		t.Fatalf("unexpected updated view: %+v", updated)
	}
	if updated.UpdatedAt.Before(updated.CreatedAt) {
		t.Fatalf("updated_at predates created_at: %+v", updated)
	}

	patch := performSavedViewRequest(t, http.MethodPatch,
		fmt.Sprintf("/api/v1/saved-views/%d", created.ID),
		`{"sort_order":2}`, 1, "user@example.com")
	assertSavedViewStatus(t, patch, http.StatusOK)

	otherUserPatch := performSavedViewRequest(t, http.MethodPatch,
		fmt.Sprintf("/api/v1/saved-views/%d", created.ID),
		`{"name":"Stolen"}`, 2, "other@example.com")
	assertSavedViewStatus(t, otherUserPatch, http.StatusNotFound)

	invalid := performSavedViewRequest(t, http.MethodPatch,
		fmt.Sprintf("/api/v1/saved-views/%d", created.ID),
		`{"filter":{"priority":"9"}}`, 1, "user@example.com")
	assertSavedViewStatus(t, invalid, http.StatusBadRequest)
	assertSavedViewErrorCode(t, invalid, "invalid_request")

	overflow := performSavedViewRequest(t, http.MethodPatch,
		fmt.Sprintf("/api/v1/saved-views/%d", created.ID),
		`{"sort_order":2147483648}`, 1, "user@example.com")
	assertSavedViewStatus(t, overflow, http.StatusBadRequest)

	for index := 1; index < storage.MaxSavedViewsPerUser; index++ {
		response := performSavedViewRequest(t, http.MethodPost, "/api/v1/saved-views",
			fmt.Sprintf(`{"name":"View %d","filter":{}}`, index), 1, "user@example.com")
		assertSavedViewStatus(t, response, http.StatusCreated)
	}
	overLimit := performSavedViewRequest(t, http.MethodPost, "/api/v1/saved-views",
		`{"name":"One too many","filter":{}}`, 1, "user@example.com")
	assertSavedViewStatus(t, overLimit, http.StatusConflict)
	assertSavedViewErrorCode(t, overLimit, "limit_reached")

	otherUserDelete := performSavedViewRequest(t, http.MethodDelete,
		fmt.Sprintf("/api/v1/saved-views/%d", created.ID), "", 2, "other@example.com")
	assertSavedViewStatus(t, otherUserDelete, http.StatusNotFound)

	deleteResponse := performSavedViewRequest(t, http.MethodDelete,
		fmt.Sprintf("/api/v1/saved-views/%d", created.ID), "", 1, "user@example.com")
	assertSavedViewStatus(t, deleteResponse, http.StatusNoContent)
	if deleteResponse.Body.Len() != 0 {
		t.Fatalf("expected empty delete response, got %q", deleteResponse.Body.String())
	}

	deletedGet := performSavedViewRequest(t, http.MethodGet,
		fmt.Sprintf("/api/v1/saved-views/%d", created.ID), "", 1, "user@example.com")
	assertSavedViewStatus(t, deletedGet, http.StatusNotFound)
}

func performSavedViewRequest(
	t *testing.T,
	method string,
	path string,
	body string,
	userID int,
	email string,
) *httptest.ResponseRecorder {
	t.Helper()

	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	_ = email
	request = utils.SetAPIUserID(request, userID)

	response := httptest.NewRecorder()
	handlers.APIV1SavedViewsRouter(response, request)
	return response
}

func assertSavedViewStatus(t *testing.T, response *httptest.ResponseRecorder, expected int) {
	t.Helper()
	if response.Code != expected {
		t.Fatalf("expected HTTP %d, got %d: %s", expected, response.Code, response.Body.String())
	}
}

func assertSavedViewErrorCode(t *testing.T, response *httptest.ResponseRecorder, expected string) {
	t.Helper()
	var payload map[string]string
	decodeSavedViewResponse(t, response, &payload)
	if payload["error"] != expected {
		t.Fatalf("expected error code %q, got %#v", expected, payload)
	}
}

func decodeSavedViewResponse(t *testing.T, response *httptest.ResponseRecorder, destination any) {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(response.Body.Bytes()))
	if err := decoder.Decode(destination); err != nil {
		t.Fatalf("decode response %q: %v", response.Body.String(), err)
	}
}
