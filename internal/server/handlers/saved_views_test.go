package handlers

import (
	"GoTodo/internal/storage"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidateSavedViewFilterRejectsInvalidValues(t *testing.T) {
	cases := []struct {
		name   string
		filter storage.SavedViewFilter
	}{
		{name: "project", filter: storage.SavedViewFilter{Project: "-1"}},
		{name: "status", filter: storage.SavedViewFilter{Status: "open"}},
		{name: "due", filter: storage.SavedViewFilter{Due: "tomorrow"}},
		{name: "completed", filter: storage.SavedViewFilter{Completed: "month"}},
		{name: "priority", filter: storage.SavedViewFilter{Priority: "4"}},
		{name: "tag", filter: storage.SavedViewFilter{Tag: "zero"}},
		{name: "sort", filter: storage.SavedViewFilter{Sort: "name"}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := validateSavedViewFilter(&testCase.filter); err == nil {
				t.Fatalf("expected %s filter to be rejected", testCase.name)
			}
		})
	}
}

func TestAPIV1SavedViewsRequiresAuthentication(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/saved-views", nil)
	response := httptest.NewRecorder()

	APIV1SavedViewsRouter(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected HTTP %d, got %d", http.StatusUnauthorized, response.Code)
	}
	if !strings.Contains(response.Body.String(), `"error":"unauthorized"`) {
		t.Fatalf("unexpected response: %s", response.Body.String())
	}
}
