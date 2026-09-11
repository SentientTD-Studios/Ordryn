package handlers

import (
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
