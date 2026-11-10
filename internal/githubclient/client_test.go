package githubclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseRepoFullName(t *testing.T) {
	cases := []struct {
		in          string
		owner, repo string
		ok          bool
	}{
		{"acme/widgets", "acme", "widgets", true},
		{" https://github.com/acme/widgets ", "acme", "widgets", true},
		{"https://github.com/acme/widgets.git", "acme", "widgets", true},
		{"github.com/acme/widgets", "acme", "widgets", true},
		{"", "", "", false},
		{"onlyowner", "", "", false},
	}
	for _, tc := range cases {
		o, r, err := ParseRepoFullName(tc.in)
		if tc.ok && err != nil {
			t.Fatalf("%q: unexpected err %v", tc.in, err)
		}
		if !tc.ok && err == nil {
			t.Fatalf("%q: expected error", tc.in)
		}
		if tc.ok && (o != tc.owner || r != tc.repo) {
			t.Fatalf("%q: got %s/%s want %s/%s", tc.in, o, r, tc.owner, tc.repo)
		}
	}
}

func TestParseIssueRef(t *testing.T) {
	o, r, n, err := ParseIssueRef("https://github.com/acme/widgets/issues/42")
	if err != nil || o != "acme" || r != "widgets" || n != 42 {
		t.Fatalf("url parse: %v %s/%s #%d", err, o, r, n)
	}
	o, r, n, err = ParseIssueRef("#7")
	if err != nil || o != "" || r != "" || n != 7 {
		t.Fatalf("number parse: %v %s/%s #%d", err, o, r, n)
	}
	if _, _, _, err := ParseIssueRef("nope"); err == nil {
		t.Fatal("expected error")
	}
}

func TestTokenKind(t *testing.T) {
	cases := map[string]string{
		"github_pat_abc": "fine_grained",
		"ghp_abc":        "classic",
		"gho_abc":        "oauth",
		"ghu_abc":        "app",
		"abc":            "unknown",
	}
	for in, want := range cases {
		if got := TokenKind(in); got != want {
			t.Errorf("TokenKind(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestScopesAllowPrivateRepos(t *testing.T) {
	if !ScopesAllowPrivateRepos(ParseScopes("read:org, repo")) {
		t.Error("repo scope should allow private repos")
	}
	if ScopesAllowPrivateRepos(ParseScopes("public_repo, read:org")) {
		t.Error("public_repo should not allow private repos")
	}
	if ScopesAllowPrivateRepos(ParseScopes("")) {
		t.Error("empty scopes should not allow private repos")
	}
}

func TestSSOAuthorizeURL(t *testing.T) {
	got := SSOAuthorizeURL("required; url=https://github.com/orgs/acme/sso?authorization_request=x")
	if got != "https://github.com/orgs/acme/sso?authorization_request=x" {
		t.Errorf("unexpected url %q", got)
	}
	if SSOAuthorizeURL("partial-results; organizations=1,2") != "" {
		t.Error("expected empty url")
	}
}

func TestGetRepoCapturesScopeAndSSOHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/user":
			w.Header().Set("X-OAuth-Scopes", "public_repo, read:org")
			_, _ = w.Write([]byte(`{"id":1,"login":"me"}`))
		case "/repos/acme/private":
			w.Header().Set("X-OAuth-Scopes", "public_repo")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"Not Found"}`))
		case "/repos/sso/repo":
			w.Header().Set("X-GitHub-SSO", "required; url=https://github.com/orgs/sso/sso?x=1")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"message":"Resource protected by organization SAML enforcement."}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	c := New("ghp_test")
	c.APIBase = srv.URL

	u, err := c.GetAuthenticatedUser(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !u.ScopesKnown || ScopesAllowPrivateRepos(u.Scopes) {
		t.Fatalf("unexpected scopes %+v known=%v", u.Scopes, u.ScopesKnown)
	}

	_, err = c.GetRepo(context.Background(), "acme", "private")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 404 || !apiErr.HasOAuthScopes || apiErr.OAuthScopes != "public_repo" {
		t.Fatalf("unexpected err %#v", err)
	}

	_, err = c.GetRepo(context.Background(), "sso", "repo")
	if !errors.As(err, &apiErr) || apiErr.SSO == "" || apiErr.HasOAuthScopes {
		t.Fatalf("unexpected err %#v", err)
	}
}
