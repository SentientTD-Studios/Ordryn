package storage

import "testing"

func TestSlugifyProjectName(t *testing.T) {
	cases := map[string]string{
		"Web App":           "web-app",
		"  Q4 -- Launch!! ": "q4-launch",
		"Café Ops":          "caf-ops",
		"2026":              "p-2026",
		"!!!":               "project",
		"x":                 "project",
		"A very long project name that keeps going and going on": "a-very-long-project-name-that-keeps-going-and-go",
	}
	for in, want := range cases {
		if got := SlugifyProjectName(in); got != want {
			t.Errorf("SlugifyProjectName(%q) = %q, want %q", in, got, want)
		}
		if _, err := NormalizeProjectSlug(SlugifyProjectName(in)); err != nil {
			t.Errorf("slug for %q is not valid: %v", in, err)
		}
	}
}

func TestNormalizeProjectSlug(t *testing.T) {
	ok := map[string]string{"Web-App": "web-app", " ops ": "ops", "v2": "v2"}
	for in, want := range ok {
		got, err := NormalizeProjectSlug(in)
		if err != nil || got != want {
			t.Errorf("NormalizeProjectSlug(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "a", "123", "-web", "web-", "web--app", "web app", "web_app"} {
		if _, err := NormalizeProjectSlug(bad); err == nil {
			t.Errorf("NormalizeProjectSlug(%q) should fail", bad)
		}
	}
}

func TestSlugCandidate(t *testing.T) {
	if got := slugCandidate("web", 1); got != "web" {
		t.Fatalf("got %q", got)
	}
	if got := slugCandidate("web", 3); got != "web-3" {
		t.Fatalf("got %q", got)
	}
	long := "a-very-long-project-name-that-keeps-going-and-go"
	got := slugCandidate(long, 12)
	if len(got) > MaxProjectSlugLength || got[len(got)-3:] != "-12" {
		t.Fatalf("got %q", got)
	}
}
