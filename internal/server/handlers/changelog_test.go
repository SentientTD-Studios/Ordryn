package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRenderMarkdownAutolinksBareURLs(t *testing.T) {
	md := `## What's Changed
* Feature by @user in https://github.com/owner/repo/pull/123
* Commit https://github.com/owner/repo/commit/abc123
**Full Changelog**: https://github.com/owner/repo/compare/v1.0.0...v1.1.0
`
	html := renderMarkdown(md)
	for _, url := range []string{
		"https://github.com/owner/repo/pull/123",
		"https://github.com/owner/repo/commit/abc123",
		"https://github.com/owner/repo/compare/v1.0.0...v1.1.0",
	} {
		want := `<a target="_blank" rel="noopener noreferrer" href="` + url + `"`
		if !strings.Contains(html, want) {
			t.Fatalf("expected clickable link for %s, got:\n%s", url, html)
		}
	}
}

func TestRenderMarkdownKeepsExplicitMarkdownLinks(t *testing.T) {
	html := renderMarkdown(`See [#456](https://github.com/owner/repo/pull/456)`)
	want := `<a target="_blank" rel="noopener noreferrer" href="https://github.com/owner/repo/pull/456">#456</a>`
	if !strings.Contains(html, want) {
		t.Fatalf("expected markdown link rendered, got:\n%s", html)
	}
}

func TestFilterEntriesBySiteVersionKeepsUpToCurrent(t *testing.T) {
	entries := []ChangelogEntry{
		{Version: "v1.1.0"},
		{Version: "v1.2.0"},
		{Version: "v1.3.0"},
		{Version: "not-semver"},
	}
	got := filterEntriesBySiteVersionWithSiteVersion(entries, "v1.2.0")
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	if got[0].Version != "v1.1.0" || got[1].Version != "v1.2.0" || got[2].Version != "not-semver" {
		t.Fatalf("got %#v", got)
	}
}

func TestFilterEntriesBySiteVersionHidesNewerReleases(t *testing.T) {
	entries := []ChangelogEntry{
		{Version: "v3.22.0"},
		{Version: "v4.0.0"},
	}
	got := filterEntriesBySiteVersionWithSiteVersion(entries, "v3.22.0")
	if len(got) != 1 || got[0].Version != "v3.22.0" {
		t.Fatalf("v3.22.0 must not see v4.0.0 notes, got %#v", got)
	}
}

func TestFilterEntriesBySiteVersionDevHidesReleases(t *testing.T) {
	entries := []ChangelogEntry{
		{Version: "v9.9.9"},
		{Version: "v1.0.0"},
	}
	if got := filterEntriesBySiteVersionWithSiteVersion(entries, "dev"); len(got) != 0 {
		t.Fatalf("dev builds must not show versioned changelog entries, got %#v", got)
	}
	if got := filterEntriesBySiteVersionWithSiteVersion(entries, ""); len(got) != 0 {
		t.Fatalf("empty site version must not show versioned changelog entries, got %#v", got)
	}
}

func TestGitHubNextPageURL(t *testing.T) {
	current := "https://api.github.com/repos/o/r/releases?per_page=100&page=1"
	link := `<https://api.github.com/repos/o/r/releases?per_page=100&page=2>; rel="next", <https://api.github.com/repos/o/r/releases?per_page=100&page=3>; rel="last"`
	got := githubNextPageURL(link, current)
	want := "https://api.github.com/repos/o/r/releases?per_page=100&page=2"
	if got != want {
		t.Fatalf("next = %q, want %q", got, want)
	}
	if githubNextPageURL(`<https://evil.example/x>; rel="next"`, current) != "" {
		t.Fatal("off-host next links must be ignored")
	}
	if githubNextPageURL(`<https://api.github.com/repos/o/r/releases?page=2>; rel="prev"`, current) != "" {
		t.Fatal("prev links must be ignored")
	}
}

func TestChangelogEntriesFromGitHubReleasesSkipsDrafts(t *testing.T) {
	got := changelogEntriesFromGitHubReleases([]githubRelease{
		{TagName: "v1.0.0", Name: "v1.0.0", PublishedAt: "2026-01-16T13:56:20Z", Body: "ok", Draft: false},
		{TagName: "v0.9.0", Name: "draft", PublishedAt: "2026-01-01T00:00:00Z", Body: "no", Draft: true},
	})
	if len(got) != 1 || got[0].Version != "v1.0.0" || got[0].Date != "2026-01-16" {
		t.Fatalf("got %#v", got)
	}
}

func resetChangelogMemCache() {
	memCache.mu.Lock()
	memCache.m = make(map[string]memItem)
	memCache.mu.Unlock()
}

func TestFetchFromGitHubPaginatesAllReleases(t *testing.T) {
	resetChangelogMemCache()
	t.Cleanup(resetChangelogMemCache)

	var pages []string
	var perPages []string
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		perPages = append(perPages, r.URL.Query().Get("per_page"))
		page := r.URL.Query().Get("page")
		if page == "" {
			page = "1"
		}
		pages = append(pages, page)
		switch page {
		case "1":
			w.Header().Set("ETag", `"page-1"`)
			w.Header().Set("Link", fmt.Sprintf(
				`<%s/repos/o/r/releases?per_page=100&page=2>; rel="next"`,
				srv.URL,
			))
			_ = json.NewEncoder(w).Encode([]githubRelease{{
				TagName:     "v0.18.0-beta",
				Name:        "v0.18.0-beta",
				PublishedAt: "2026-07-08T00:00:00Z",
				Body:        "thirtieth",
				Prerelease:  true,
			}})
		case "2":
			_ = json.NewEncoder(w).Encode([]githubRelease{{
				TagName:     "v0.5.0-beta",
				Name:        "v0.5.0-beta-1 - dev",
				PublishedAt: "2026-01-16T13:56:20Z",
				Body:        "first",
				Prerelease:  true,
			}})
		default:
			_ = json.NewEncoder(w).Encode([]githubRelease{})
		}
	}))
	t.Cleanup(srv.Close)

	origBase := githubAPIBase
	githubAPIBase = srv.URL
	t.Cleanup(func() { githubAPIBase = origBase })

	got, err := fetchFromGitHub("o/r")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(pages, ",") != "1,2" {
		t.Fatalf("pages fetched = %v, want 1 then 2", pages)
	}
	if strings.Join(perPages, ",") != "100,100" {
		t.Fatalf("per_page = %v, want 100 on every page", perPages)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2: %#v", len(got), got)
	}
	if got[0].Version != "v0.18.0-beta" || got[1].Version != "v0.5.0-beta" {
		t.Fatalf("versions = %q, %q", got[0].Version, got[1].Version)
	}
	if got[1].Title != "v0.5.0-beta-1 - dev" {
		t.Fatalf("oldest title = %q", got[1].Title)
	}
}

func TestFetchFromGitHubUsesCachedPayloadOn304(t *testing.T) {
	resetChangelogMemCache()
	t.Cleanup(resetChangelogMemCache)

	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.Header.Get("If-None-Match") == `"cached"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"cached"`)
		_ = json.NewEncoder(w).Encode([]githubRelease{{
			TagName:     "v1.0.0",
			Name:        "v1.0.0",
			PublishedAt: "2026-07-18T00:00:00Z",
			Body:        "stable",
		}})
	}))
	t.Cleanup(srv.Close)

	origBase := githubAPIBase
	githubAPIBase = srv.URL
	t.Cleanup(func() { githubAPIBase = origBase })

	first, err := fetchFromGitHub("cache/r")
	if err != nil {
		t.Fatal(err)
	}
	second, err := fetchFromGitHub("cache/r")
	if err != nil {
		t.Fatal(err)
	}
	if hits != 2 {
		t.Fatalf("hits = %d, want 2", hits)
	}
	if len(first) != 1 || len(second) != 1 || second[0].Version != "v1.0.0" {
		t.Fatalf("first=%#v second=%#v", first, second)
	}
}
