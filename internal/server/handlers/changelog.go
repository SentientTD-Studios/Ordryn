package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"GoTodo/internal/config"
	srvutils "GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
	"GoTodo/internal/version"

	"golang.org/x/mod/semver"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

// ChangelogEntry is the public structure returned to the client
type ChangelogEntry struct {
	Version    string   `json:"version"`
	Date       string   `json:"date"`
	Title      string   `json:"title"`
	Notes      []string `json:"notes"`
	Html       string   `json:"html,omitempty"`
	Prerelease bool     `json:"prerelease,omitempty"`
}

// In-memory fallback cache
type memItem struct {
	data   string
	etag   string
	expiry time.Time
}

var memCache = struct {
	m  map[string]memItem
	mu sync.RWMutex
}{m: make(map[string]memItem)}

const (
	githubReleasesPerPage  = 100
	githubReleasesMaxPages = 20
	changelogCacheVersion  = "v2"
)

// githubAPIBase is the GitHub REST API origin. Tests may override it.
var githubAPIBase = "https://api.github.com"

type githubRelease struct {
	TagName     string `json:"tag_name"`
	Name        string `json:"name"`
	PublishedAt string `json:"published_at"`
	Body        string `json:"body"`
	Draft       bool   `json:"draft"`
	Prerelease  bool   `json:"prerelease"`
}

// ChangelogHandler serves the changelog JSON; it will attempt to pull from
// GitHub releases when GITHUB_REPO is set (owner/repo). If that fails, it
// falls back to config/changelog.json.
func ChangelogHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Respect toggle: prefer DB-backed setting, fall back to config.Cfg
	showChangelog := config.Cfg.ShowChangelog
	if s, err := storage.GetSiteSettings(); err == nil && s != nil {
		showChangelog = s.ShowChangelog
	}
	if !showChangelog {
		http.NotFound(w, r)
		return
	}

	// Site version for filtering is always the baked-in binary version
	siteVersion := version.Version

	// If GITHUB_REPO is configured, try fetching releases
	repo := strings.TrimSpace(os.Getenv("GITHUB_REPO"))
	if repo != "" {
		if entries, err := fetchFromGitHub(repo); err == nil {
			entries = filterEntriesBySiteVersionWithSiteVersion(entries, siteVersion)
			respondJSON(w, entries)
			return
		}
		// else fall through to local file
	}

	// Local fallback
	cfgPath := filepath.Join("config", "changelog.json")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		respondJSON(w, []ChangelogEntry{})
		return
	}

	// Validate and return
	var v []ChangelogEntry
	if err := json.Unmarshal(data, &v); err != nil {
		respondJSON(w, []ChangelogEntry{})
		return
	}
	// Render HTML for any local entries (join notes into markdown list and convert)
	for i := range v {
		if v[i].Html == "" {
			if len(v[i].Notes) > 0 {
				md := ""
				for _, n := range v[i].Notes {
					md += "- " + n + "\n"
				}
				v[i].Html = renderMarkdown(md)
			}
		}
		// Attempt to normalize/remove any leading breadcrumb-like block
		if v[i].Html != "" {
			v[i].Html = normalizeReleaseHTML(v[i].Html, v[i].Version, v[i].Title, v[i].Date)
		}
	}
	// Filter out releases that are newer than the current site version (baked-in)
	v = filterEntriesBySiteVersionWithSiteVersion(v, siteVersion)
	respondJSON(w, v)
}

// filterEntriesBySiteVersionWithSiteVersion keeps changelog entries at or below
// the running binary version so instances do not see notes for releases they
// are not running. Unstamped versions ("dev", empty, non-semver) hide all
// versioned entries rather than leaking newer GitHub releases.
func filterEntriesBySiteVersionWithSiteVersion(entries []ChangelogEntry, siteVersion string) []ChangelogEntry {
	sv := strings.TrimSpace(siteVersion)
	if sv == "" {
		return []ChangelogEntry{}
	}
	if !strings.HasPrefix(sv, "v") {
		sv = "v" + sv
	}
	// Unstamped/"dev" binaries must not see GitHub releases they may not be running.
	if !semver.IsValid(sv) {
		return []ChangelogEntry{}
	}

	out := make([]ChangelogEntry, 0, len(entries))
	for _, e := range entries {
		ev := e.Version
		if ev == "" {
			out = append(out, e)
			continue
		}
		if !strings.HasPrefix(ev, "v") {
			ev = "v" + ev
		}
		if !semver.IsValid(ev) {
			// keep entries we can't parse
			out = append(out, e)
			continue
		}
		// include if entry version is less than or equal to site version
		if semver.Compare(ev, sv) <= 0 {
			out = append(out, e)
		}
	}
	return out
}

func respondJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(v)
}

func changelogCacheKeys(repo string) (dataKey, etagKey, memKey string) {
	dataKey = fmt.Sprintf("changelog:data:%s:%s", changelogCacheVersion, repo)
	etagKey = fmt.Sprintf("changelog:etag:%s:%s", changelogCacheVersion, repo)
	memKey = repo + "#" + changelogCacheVersion
	return
}

func cachedChangelogEntries(raw string) ([]ChangelogEntry, bool) {
	if raw == "" {
		return nil, false
	}
	var cached []ChangelogEntry
	if err := json.Unmarshal([]byte(raw), &cached); err != nil {
		return nil, false
	}
	return cached, true
}

func githubReleasesListURL(repo string, page int) string {
	if page < 1 {
		page = 1
	}
	return fmt.Sprintf("%s/repos/%s/releases?per_page=%d&page=%d",
		strings.TrimRight(githubAPIBase, "/"), repo, githubReleasesPerPage, page)
}

// githubNextPageURL returns the rel=next URL from a GitHub Link header when it
// points at the same API host. Empty string means there is no further page.
func githubNextPageURL(linkHeader, currentURL string) string {
	next := ""
	for _, part := range strings.Split(linkHeader, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		low := strings.ToLower(part)
		if !strings.Contains(low, `rel="next"`) && !strings.Contains(low, `rel='next'`) {
			continue
		}
		start := strings.Index(part, "<")
		end := strings.Index(part, ">")
		if start == -1 || end <= start {
			continue
		}
		next = strings.TrimSpace(part[start+1 : end])
		break
	}
	if next == "" {
		return ""
	}
	parsed, err := url.Parse(next)
	if err != nil || parsed.Host == "" {
		return ""
	}
	cur, err := url.Parse(currentURL)
	if err != nil || cur.Host == "" {
		return ""
	}
	if !strings.EqualFold(parsed.Host, cur.Host) || !strings.EqualFold(parsed.Scheme, cur.Scheme) {
		return ""
	}
	return next
}

func changelogEntriesFromGitHubReleases(releases []githubRelease) []ChangelogEntry {
	out := make([]ChangelogEntry, 0, len(releases))
	for _, r := range releases {
		if r.Draft {
			continue
		}
		date := r.PublishedAt
		if strings.Contains(date, "T") {
			if t, err := time.Parse(time.RFC3339, date); err == nil {
				date = t.Format("2006-01-02")
			}
		}
		title := r.Name
		if title == "" {
			title = r.TagName
		}
		notes := parseNotesFromBody(r.Body)
		html := renderMarkdown(r.Body)
		html = normalizeReleaseHTML(html, r.TagName, title, date)
		out = append(out, ChangelogEntry{
			Version:    r.TagName,
			Date:       date,
			Title:      title,
			Notes:      notes,
			Html:       html,
			Prerelease: r.Prerelease,
		})
	}
	return out
}

func storeChangelogCache(ctx context.Context, dataKey, etagKey, memKey, payload, etag string) {
	if srvutils.RedisClient != nil {
		_ = srvutils.RedisClient.Set(ctx, dataKey, payload, 10*time.Minute).Err()
		if etag != "" {
			_ = srvutils.RedisClient.Set(ctx, etagKey, etag, 10*time.Minute).Err()
		}
		return
	}
	memCache.mu.Lock()
	memCache.m[memKey] = memItem{data: payload, etag: etag, expiry: time.Now().Add(10 * time.Minute)}
	memCache.mu.Unlock()
}

func newGitHubReleasesRequest(rawURL, etag string) (*http.Request, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	token := strings.TrimSpace(os.Getenv("GITHUB_TOKEN"))
	if token != "" {
		req.Header.Set("Authorization", "token "+token)
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "Ordryn-Changelog")
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	return req, nil
}

// fetchFromGitHub fetches every release page from the GitHub API (default page
// size is 30; we request 100 and follow Link rel=next) and maps them to ChangelogEntry.
func fetchFromGitHub(repo string) ([]ChangelogEntry, error) {
	ctx := context.Background()
	dataKey, etagKey, memKey := changelogCacheKeys(repo)

	var cachedJSON string
	var cachedETag string

	if srvutils.RedisClient != nil {
		if v, err := srvutils.RedisClient.Get(ctx, dataKey).Result(); err == nil {
			cachedJSON = v
		}
		if e, err := srvutils.RedisClient.Get(ctx, etagKey).Result(); err == nil {
			cachedETag = e
		}
	} else {
		memCache.mu.RLock()
		if it, ok := memCache.m[memKey]; ok && time.Now().Before(it.expiry) {
			cachedJSON = it.data
			cachedETag = it.etag
		}
		memCache.mu.RUnlock()
	}

	client := &http.Client{Timeout: 15 * time.Second}
	pageURL := githubReleasesListURL(repo, 1)
	var all []githubRelease
	var firstETag string

	for page := 1; page <= githubReleasesMaxPages && pageURL != ""; page++ {
		etag := ""
		if page == 1 {
			etag = cachedETag
		}
		req, err := newGitHubReleasesRequest(pageURL, etag)
		if err != nil {
			if cached, ok := cachedChangelogEntries(cachedJSON); ok {
				return cached, nil
			}
			return nil, err
		}

		resp, err := client.Do(req)
		if err != nil {
			if cached, ok := cachedChangelogEntries(cachedJSON); ok {
				return cached, nil
			}
			if len(all) > 0 {
				break
			}
			return nil, err
		}

		if page == 1 && resp.StatusCode == http.StatusNotModified {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if cached, ok := cachedChangelogEntries(cachedJSON); ok {
				return cached, nil
			}
			return nil, fmt.Errorf("received 304 but no cached data")
		}

		if resp.StatusCode != http.StatusOK {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if cached, ok := cachedChangelogEntries(cachedJSON); ok {
				return cached, nil
			}
			if len(all) > 0 {
				break
			}
			return nil, fmt.Errorf("github API returned status %d", resp.StatusCode)
		}

		body, err := io.ReadAll(resp.Body)
		next := githubNextPageURL(resp.Header.Get("Link"), pageURL)
		if firstETag == "" {
			firstETag = resp.Header.Get("ETag")
		}
		resp.Body.Close()
		if err != nil {
			if cached, ok := cachedChangelogEntries(cachedJSON); ok {
				return cached, nil
			}
			if len(all) > 0 {
				break
			}
			return nil, err
		}

		var releases []githubRelease
		if err := json.Unmarshal(body, &releases); err != nil {
			if cached, ok := cachedChangelogEntries(cachedJSON); ok {
				return cached, nil
			}
			if len(all) > 0 {
				break
			}
			return nil, err
		}

		all = append(all, releases...)
		if next == "" && len(releases) == githubReleasesPerPage {
			next = githubReleasesListURL(repo, page+1)
		}
		pageURL = next
		if len(releases) == 0 {
			break
		}
	}

	out := changelogEntriesFromGitHubReleases(all)
	finalB, _ := json.Marshal(out)
	storeChangelogCache(ctx, dataKey, etagKey, memKey, string(finalB), firstETag)
	return out, nil
}

func parseNotesFromBody(body string) []string {
	if strings.TrimSpace(body) == "" {
		return nil
	}
	lines := strings.Split(body, "\n")
	notes := make([]string, 0, len(lines))
	for _, l := range lines {
		s := strings.TrimSpace(l)
		if s == "" {
			continue
		}
		// strip common bullet markers
		if strings.HasPrefix(s, "- ") || strings.HasPrefix(s, "* ") || strings.HasPrefix(s, "• ") {
			if len(s) > 2 {
				s = strings.TrimSpace(s[2:])
			} else {
				s = ""
			}
		}
		if s != "" {
			notes = append(notes, s)
		}
	}
	return notes
}

// changelogMarkdown renders GitHub-style release notes (GFM + autolinks).
var changelogMarkdown = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
)

// renderMarkdown converts markdown text to HTML using goldmark with GFM
// so bare URLs in auto-generated release notes become clickable links.
func renderMarkdown(md string) string {
	if strings.TrimSpace(md) == "" {
		return ""
	}
	var buf bytes.Buffer
	if err := changelogMarkdown.Convert([]byte(md), &buf); err != nil {
		return ""
	}
	// Open external links in a new tab from the changelog modal.
	return linkifyAnchors(buf.String())
}

// linkifyAnchors adds target=_blank to anchors that don't already set a target.
func linkifyAnchors(htmlStr string) string {
	const needle = "<a href="
	const attrs = `<a target="_blank" rel="noopener noreferrer" href=`
	var b strings.Builder
	rest := htmlStr
	for {
		i := strings.Index(rest, needle)
		if i == -1 {
			b.WriteString(rest)
			break
		}
		b.WriteString(rest[:i])
		// Skip if this <a already has a target= attribute before href (unlikely with goldmark).
		b.WriteString(attrs)
		rest = rest[i+len(needle):]
	}
	return b.String()
}

// stripTags removes any HTML tags from s (naive) and returns plain text.
func stripTags(s string) string {
	var b strings.Builder
	inTag := false
	for _, r := range s {
		if r == '<' {
			inTag = true
			continue
		}
		if r == '>' {
			inTag = false
			continue
		}
		if !inTag {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// normalizeReleaseHTML attempts to remove a leading breadcrumb-like block
// (commonly a short paragraph or div that repeats the version and date)
// from the rendered HTML. It only strips the first block when it appears to
// contain the version or date to avoid removing legitimate headings.
func normalizeReleaseHTML(htmlStr, version, title, date string) string {
	s := strings.TrimSpace(htmlStr)
	if s == "" {
		return htmlStr
	}
	lower := strings.ToLower(s)
	// only consider removing when the first token is a paragraph/div/pre
	if !(strings.HasPrefix(lower, "<p") || strings.HasPrefix(lower, "<div") || strings.HasPrefix(lower, "<pre")) {
		return htmlStr
	}
	// find end of opening tag
	openEnd := strings.Index(s, ">")
	if openEnd == -1 {
		return htmlStr
	}
	openTag := s[1:openEnd]
	tagName := strings.Fields(openTag)[0]
	if tagName == "" {
		return htmlStr
	}
	closeTag := "</" + tagName + ">"
	closeIdx := strings.Index(strings.ToLower(s), strings.ToLower(closeTag))
	if closeIdx == -1 {
		return htmlStr
	}
	inner := s[openEnd+1 : closeIdx]
	innerText := strings.TrimSpace(stripTags(inner))
	if innerText == "" {
		// empty block — strip it
		return strings.TrimSpace(s[closeIdx+len(closeTag):])
	}
	lowInner := strings.ToLower(innerText)
	v := strings.ToLower(strings.TrimSpace(version))
	d := strings.ToLower(strings.TrimSpace(date))
	t := strings.ToLower(strings.TrimSpace(title))
	// remove if the inner block contains the version or the date or looks like a breadcrumb (contains ' - ' and the version)
	if (v != "" && strings.Contains(lowInner, v)) || (d != "" && strings.Contains(lowInner, d)) || (t != "" && strings.Contains(lowInner, t) && strings.Contains(lowInner, v)) || strings.Contains(lowInner, " - ") {
		return strings.TrimSpace(s[closeIdx+len(closeTag):])
	}
	return htmlStr
}

// PreloadChangelog attempts to fetch releases once (used at startup)
// It populates the Redis or in-memory cache via fetchFromGitHub.
func PreloadChangelog() error {
	if !config.Cfg.ShowChangelog {
		return nil
	}
	repo := strings.TrimSpace(os.Getenv("GITHUB_REPO"))
	if repo == "" {
		return nil
	}
	_, err := fetchFromGitHub(repo)
	return err
}
