package handlers

import (
	"GoTodo/internal/server/utils"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestParseProjectFromPath(t *testing.T) {
	orig := utils.BasePath
	t.Cleanup(func() { utils.BasePath = orig })

	tests := []struct {
		name     string
		basePath string
		path     string
		want     string
	}{
		{name: "root numeric id", basePath: "/", path: "/p/8", want: "8"},
		{name: "root none", basePath: "/", path: "/p/none", want: "none"},
		{name: "subpath numeric id", basePath: "/gotodo", path: "/gotodo/p/12", want: "12"},
		{name: "subpath none", basePath: "/gotodo", path: "/gotodo/p/none", want: "none"},
		{name: "invalid segment", basePath: "/", path: "/p/not-a-number", want: ""},
		{name: "empty segment", basePath: "/", path: "/p/", want: ""},
		{name: "nested path", basePath: "/", path: "/p/8/extra", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			utils.BasePath = tt.basePath
			if got := parseProjectFromPath(tt.path); got != tt.want {
				t.Fatalf("parseProjectFromPath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestProjectFilterPageURL(t *testing.T) {
	q := url.Values{}
	q.Set("project", "8")
	q.Set("status", "incomplete")

	got := projectFilterPageURL("/", "8", q)
	want := "/p/8?status=incomplete"
	if got != want {
		t.Fatalf("projectFilterPageURL() = %q, want %q", got, want)
	}

	q2 := url.Values{}
	q2.Set("project", "0")
	got2 := projectFilterPageURL("/gotodo", "0", q2)
	if got2 != "/gotodo/p/none" {
		t.Fatalf("no-project URL = %q", got2)
	}

	q3 := url.Values{}
	q3.Set("project", "8")
	got3 := homeURLWithQuery("/", q3)
	if got3 != "/" {
		t.Fatalf("homeURLWithQuery() = %q, want /", got3)
	}
}

func TestHomeHandlerLegacyProjectRedirect(t *testing.T) {
	orig := utils.BasePath
	t.Cleanup(func() { utils.BasePath = orig })
	utils.BasePath = "/"

	req := httptest.NewRequest("GET", "/?project=8&status=incomplete", nil)
	rec := httptest.NewRecorder()

	HomeHandler(rec, req)

	if rec.Code != http.StatusMovedPermanently {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMovedPermanently)
	}
	if loc := rec.Header().Get("Location"); loc != "/p/8?status=incomplete" {
		t.Fatalf("Location = %q, want /p/8?status=incomplete", loc)
	}
}

func TestProjectFilterHandlerInvalidPathRedirect(t *testing.T) {
	orig := utils.BasePath
	t.Cleanup(func() { utils.BasePath = orig })
	utils.BasePath = "/"

	req := httptest.NewRequest("GET", "/p/not-a-number", nil)
	rec := httptest.NewRecorder()

	ProjectFilterHandler(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusSeeOther)
	}
	if loc := rec.Header().Get("Location"); loc != "/" {
		t.Fatalf("Location = %q, want /", loc)
	}
}
