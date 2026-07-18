package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

// filterEntriesBySiteVersion removes any changelog entries with versions greater than current site version
func filterEntriesBySiteVersionWithSiteVersion(entries []ChangelogEntry, siteVersion string) []ChangelogEntry {
	sv := siteVersion
	if sv == "" {
		return entries
	}
	if !strings.HasPrefix(sv, "v") {
		sv = "v" + sv
	}
	// if site version isn't a valid semver, don't filter
	if !semver.IsValid(sv) {
		return entries
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

// fetchFromGitHub fetches releases from the GitHub API and maps them to ChangelogEntry
func fetchFromGitHub(repo string) ([]ChangelogEntry, error) {
	// Use Redis caching (with ETag) if available, otherwise in-memory TTL cache.
	ctx := context.Background()
	dataKey := fmt.Sprintf("changelog:data:%s", repo)
	etagKey := fmt.Sprintf("changelog:etag:%s", repo)

	var cachedJSON string
	var cachedETag string

	// Try Redis first
	if srvutils.RedisClient != nil {
		if v, err := srvutils.RedisClient.Get(ctx, dataKey).Result(); err == nil {
			cachedJSON = v
		}
		if e, err := srvutils.RedisClient.Get(ctx, etagKey).Result(); err == nil {
			cachedETag = e
		}
	} else {
		// In-memory fallback
		memCache.mu.RLock()
		if it, ok := memCache.m[repo]; ok && time.Now().Before(it.expiry) {
			cachedJSON = it.data
			cachedETag = it.etag
		}
		memCache.mu.RUnlock()
	}

	// repo expected as owner/repo
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases", repo)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	// Optionally use token for higher rate limits
	token := strings.TrimSpace(os.Getenv("GITHUB_TOKEN"))
	if token != "" {
		req.Header.Set("Authorization", "token "+token)
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	if cachedETag != "" {
		req.Header.Set("If-None-Match", cachedETag)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		// On network error, if cached JSON exists, return cached
		if cachedJSON != "" {
			var cached []ChangelogEntry
			_ = json.Unmarshal([]byte(cachedJSON), &cached)
			return cached, nil
		}
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		// 304 — use cached data
		if cachedJSON != "" {
			var cached []ChangelogEntry
			if err := json.Unmarshal([]byte(cachedJSON), &cached); err == nil {
				return cached, nil
			}
		}
		return nil, fmt.Errorf("received 304 but no cached data")
	}

	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body)
		// If we have cached payload, return it instead of failing
		if cachedJSON != "" {
			var cached []ChangelogEntry
			_ = json.Unmarshal([]byte(cachedJSON), &cached)
			return cached, nil
		}
		return nil, fmt.Errorf("github API returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		if cachedJSON != "" {
			var cached []ChangelogEntry
			_ = json.Unmarshal([]byte(cachedJSON), &cached)
			return cached, nil
		}
		return nil, err
	}

	// Minimal struct to decode releases
	var releases []struct {
		TagName     string `json:"tag_name"`
		Name        string `json:"name"`
		PublishedAt string `json:"published_at"`
		Body        string `json:"body"`
		Draft       bool   `json:"draft"`
		Prerelease  bool   `json:"prerelease"`
	}

	if err := json.Unmarshal(body, &releases); err != nil {
		if cachedJSON != "" {
			var cached []ChangelogEntry
			_ = json.Unmarshal([]byte(cachedJSON), &cached)
			return cached, nil
		}
		return nil, err
	}

	out := make([]ChangelogEntry, 0, len(releases))
	for _, r := range releases {
		if r.Draft {
			continue
		}
		date := r.PublishedAt
		// Trim time portion if present
		if strings.Contains(date, "T") {
			if t, err := time.Parse(time.RFC3339, date); err == nil {
				date = t.Format("2006-01-02")
			}
		}
		title := r.Name
		if title == "" {
			title = r.TagName
		}
		// Render the full markdown body from GitHub releases to HTML
		notes := parseNotesFromBody(r.Body)
		html := renderMarkdown(r.Body)
		// Normalize out leading breadcrumb-like paragraphs that duplicate the
		// release/version line (many release bodies include a short one-line
		// breadcrumb before the actual heading). Remove that leading block
		// when it contains the tag/version or the published date.
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

	// Marshal final payload and cache it with ETag
	finalB, _ := json.Marshal(out)
	newETag := resp.Header.Get("ETag")
	// Store in Redis if available
	if srvutils.RedisClient != nil {
		// cache for 10 minutes
		_ = srvutils.RedisClient.Set(ctx, dataKey, string(finalB), 10*time.Minute).Err()
		if newETag != "" {
			_ = srvutils.RedisClient.Set(ctx, etagKey, newETag, 10*time.Minute).Err()
		}
	} else {
		memCache.mu.Lock()
		memCache.m[repo] = memItem{data: string(finalB), etag: newETag, expiry: time.Now().Add(10 * time.Minute)}
		memCache.mu.Unlock()
	}

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

// renderMarkdown converts markdown text to HTML using goldmark.
func renderMarkdown(md string) string {
	if strings.TrimSpace(md) == "" {
		return ""
	}
	var buf bytes.Buffer
	if err := goldmark.Convert([]byte(md), &buf); err != nil {
		return ""
	}
	return buf.String()
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
