package githubclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	APIBase     = "https://api.github.com"
	OAuthAuth   = "https://github.com/login/oauth/authorize"
	OAuthToken  = "https://github.com/login/oauth/access_token"
	DefaultUA   = "Ordryn-GitHub-Integration"
	HTTPTimeout = 15 * time.Second
)

var (
	issueURLRe  = regexp.MustCompile(`(?i)^https?://(?:www\.)?github\.com/([^/]+)/([^/]+)/issues/(\d+)/?$`)
	repoSlashRe = regexp.MustCompile(`^([^/\s]+)/([^/\s]+)$`)
)

// Client talks to the GitHub REST API with a user token.
type Client struct {
	Token      string
	HTTPClient *http.Client
	APIBase    string
	UserAgent  string
}

func New(token string) *Client {
	return &Client{
		Token:      strings.TrimSpace(token),
		HTTPClient: &http.Client{Timeout: HTTPTimeout},
		APIBase:    APIBase,
		UserAgent:  DefaultUA,
	}
}

// User is the authenticated GitHub user.
type User struct {
	ID    int64  `json:"id"`
	Login string `json:"login"`

	// Scopes holds the token's OAuth scopes (X-OAuth-Scopes). Only classic
	// PATs and OAuth tokens report scopes; ScopesKnown is false otherwise
	// (fine-grained PATs, GitHub App tokens).
	Scopes      []string `json:"-"`
	ScopesKnown bool     `json:"-"`
}

// Repo is a subset of the repository payload.
type Repo struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	FullName    string `json:"full_name"`
	Private     bool   `json:"private"`
	HTMLURL     string `json:"html_url"`
	Permissions struct {
		Admin    bool `json:"admin"`
		Maintain bool `json:"maintain"`
		Push     bool `json:"push"`
	} `json:"permissions"`
	Owner struct {
		Login string `json:"login"`
		ID    int64  `json:"id"`
	} `json:"owner"`
}

// Issue is a subset of the issues payload.
type Issue struct {
	ID      int64  `json:"id"`
	Number  int    `json:"number"`
	Title   string `json:"title"`
	State   string `json:"state"`
	HTMLURL string `json:"html_url"`
	Body    string `json:"body"`
}

// APIError is a non-2xx GitHub response.
type APIError struct {
	Status  int
	Message string
	Body    string

	// OAuthScopes is the raw X-OAuth-Scopes header; HasOAuthScopes reports
	// whether GitHub sent it at all (classic PAT / OAuth tokens only).
	OAuthScopes    string
	HasOAuthScopes bool
	// SSO is the raw X-GitHub-SSO header, set when an org enforces SAML SSO
	// and the token has not been authorized for it.
	SSO string
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("github api: %s (status %d)", e.Message, e.Status)
	}
	return fmt.Sprintf("github api: status %d", e.Status)
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	_, err := c.doWithHeaders(ctx, method, path, body, out)
	return err
}

func (c *Client) doWithHeaders(ctx context.Context, method, path string, body any, out any) (http.Header, error) {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(b)
	}
	base := c.APIBase
	if base == "" {
		base = APIBase
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(base, "/")+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", c.UserAgent)
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: HTTPTimeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := ""
		var parsed struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(raw, &parsed)
		msg = parsed.Message
		apiErr := &APIError{Status: resp.StatusCode, Message: msg, Body: string(raw), SSO: resp.Header.Get("X-GitHub-SSO")}
		if vals, ok := resp.Header[http.CanonicalHeaderKey("X-OAuth-Scopes")]; ok {
			apiErr.HasOAuthScopes = true
			apiErr.OAuthScopes = strings.Join(vals, ",")
		}
		return resp.Header, apiErr
	}
	if out == nil || len(raw) == 0 || resp.StatusCode == http.StatusNoContent {
		return resp.Header, nil
	}
	return resp.Header, json.Unmarshal(raw, out)
}

// GetAuthenticatedUser returns the token owner.
func (c *Client) GetAuthenticatedUser(ctx context.Context) (*User, error) {
	var u User
	hdr, err := c.doWithHeaders(ctx, http.MethodGet, "/user", nil, &u)
	if err != nil {
		return nil, err
	}
	if vals, ok := hdr[http.CanonicalHeaderKey("X-OAuth-Scopes")]; ok {
		u.ScopesKnown = true
		u.Scopes = ParseScopes(strings.Join(vals, ","))
	}
	return &u, nil
}

// GetRepo fetches a repository and the caller's permissions on it.
func (c *Client) GetRepo(ctx context.Context, owner, repo string) (*Repo, error) {
	var r Repo
	path := fmt.Sprintf("/repos/%s/%s", url.PathEscape(owner), url.PathEscape(repo))
	if err := c.do(ctx, http.MethodGet, path, nil, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// GetIssue fetches a single issue by number.
func (c *Client) GetIssue(ctx context.Context, owner, repo string, number int) (*Issue, error) {
	var issue Issue
	path := fmt.Sprintf("/repos/%s/%s/issues/%d", url.PathEscape(owner), url.PathEscape(repo), number)
	if err := c.do(ctx, http.MethodGet, path, nil, &issue); err != nil {
		return nil, err
	}
	return &issue, nil
}

// CreateIssue creates an issue in the repository.
func (c *Client) CreateIssue(ctx context.Context, owner, repo, title, body string) (*Issue, error) {
	var issue Issue
	path := fmt.Sprintf("/repos/%s/%s/issues", url.PathEscape(owner), url.PathEscape(repo))
	payload := map[string]string{"title": title, "body": body}
	if err := c.do(ctx, http.MethodPost, path, payload, &issue); err != nil {
		return nil, err
	}
	return &issue, nil
}

// SetIssueState opens or closes an issue.
func (c *Client) SetIssueState(ctx context.Context, owner, repo string, number int, state string) (*Issue, error) {
	state = strings.ToLower(strings.TrimSpace(state))
	if state != "open" && state != "closed" {
		return nil, fmt.Errorf("state must be open or closed")
	}
	var issue Issue
	path := fmt.Sprintf("/repos/%s/%s/issues/%d", url.PathEscape(owner), url.PathEscape(repo), number)
	payload := map[string]string{"state": state}
	if err := c.do(ctx, http.MethodPatch, path, payload, &issue); err != nil {
		return nil, err
	}
	return &issue, nil
}

// ExchangeOAuthCode trades an OAuth code for an access token.
func ExchangeOAuthCode(ctx context.Context, clientID, clientSecret, code string) (string, error) {
	form := url.Values{}
	form.Set("client_id", clientID)
	form.Set("client_secret", clientSecret)
	form.Set("code", code)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, OAuthToken, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", DefaultUA)

	resp, err := (&http.Client{Timeout: HTTPTimeout}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("oauth token exchange failed: status %d", resp.StatusCode)
	}
	var parsed struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		Scope       string `json:"scope"`
		Error       string `json:"error"`
		ErrorDesc   string `json:"error_description"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", err
	}
	if parsed.Error != "" {
		return "", fmt.Errorf("oauth error: %s (%s)", parsed.Error, parsed.ErrorDesc)
	}
	if parsed.AccessToken == "" {
		return "", fmt.Errorf("oauth response missing access_token")
	}
	return parsed.AccessToken, nil
}

// AuthorizeURL builds the GitHub OAuth authorize URL.
func AuthorizeURL(clientID, redirectURI, state, scope string) string {
	q := url.Values{}
	q.Set("client_id", clientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("state", state)
	if scope != "" {
		q.Set("scope", scope)
	}
	return OAuthAuth + "?" + q.Encode()
}

// ParseRepoFullName parses "owner/repo" (optionally with a github.com URL prefix).
func ParseRepoFullName(raw string) (owner, repo string, err error) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimSuffix(raw, "/")
	if raw == "" {
		return "", "", fmt.Errorf("repository is required")
	}
	if strings.Contains(raw, "://") || strings.HasPrefix(strings.ToLower(raw), "github.com/") {
		u := raw
		if !strings.Contains(u, "://") {
			u = "https://" + u
		}
		parsed, perr := url.Parse(u)
		if perr != nil {
			return "", "", fmt.Errorf("invalid repository URL")
		}
		parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
		if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
			return "", "", fmt.Errorf("invalid repository URL")
		}
		return parts[0], strings.TrimSuffix(parts[1], ".git"), nil
	}
	m := repoSlashRe.FindStringSubmatch(raw)
	if m == nil {
		return "", "", fmt.Errorf("use owner/repo format")
	}
	return m[1], m[2], nil
}

// ParseIssueRef parses an issue number or github.com issue URL.
// When a URL is provided, owner/repo are returned as well.
func ParseIssueRef(raw string) (owner, repo string, number int, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", 0, fmt.Errorf("issue reference is required")
	}
	if m := issueURLRe.FindStringSubmatch(raw); m != nil {
		n, cerr := strconv.Atoi(m[3])
		if cerr != nil || n <= 0 {
			return "", "", 0, fmt.Errorf("invalid issue number")
		}
		return m[1], m[2], n, nil
	}
	if strings.HasPrefix(raw, "#") {
		raw = strings.TrimPrefix(raw, "#")
	}
	n, cerr := strconv.Atoi(raw)
	if cerr != nil || n <= 0 {
		return "", "", 0, fmt.Errorf("use an issue number or GitHub issue URL")
	}
	return "", "", n, nil
}

// ParseScopes splits an X-OAuth-Scopes header value ("repo, read:org").
func ParseScopes(raw string) []string {
	var out []string
	for _, p := range strings.Split(raw, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ScopesAllowPrivateRepos reports whether classic OAuth scopes grant access to
// private repositories (the "repo" scope; "public_repo" does not).
func ScopesAllowPrivateRepos(scopes []string) bool {
	for _, s := range scopes {
		if strings.EqualFold(s, "repo") {
			return true
		}
	}
	return false
}

// TokenKind classifies a GitHub token by its prefix.
// Returns "fine_grained", "classic", "oauth", "app", or "unknown".
func TokenKind(token string) string {
	token = strings.TrimSpace(token)
	switch {
	case strings.HasPrefix(token, "github_pat_"):
		return "fine_grained"
	case strings.HasPrefix(token, "ghp_"):
		return "classic"
	case strings.HasPrefix(token, "gho_"):
		return "oauth"
	case strings.HasPrefix(token, "ghu_"), strings.HasPrefix(token, "ghs_"):
		return "app"
	default:
		return "unknown"
	}
}

// SSOAuthorizeURL extracts the authorization URL from an X-GitHub-SSO header
// ("required; url=https://github.com/orgs/x/sso?authorization_request=...").
func SSOAuthorizeURL(header string) string {
	for _, part := range strings.Split(header, ";") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "url=") {
			return strings.TrimPrefix(part, "url=")
		}
	}
	return ""
}

// HasAdminAccess reports whether the authenticated user has admin on the repo.
func HasAdminAccess(r *Repo) bool {
	if r == nil {
		return false
	}
	return r.Permissions.Admin
}
