package domain

import (
	"errors"
	"strings"
	"testing"

	githubclient "GoTodo/internal/githubclient"
)

func TestMapGitHubRepoErrPrivateRepoHints(t *testing.T) {
	cases := []struct {
		name    string
		err     *githubclient.APIError
		token   string
		wantIs  error
		wantSub string
	}{
		{"classic without repo scope", &githubclient.APIError{Status: 404, Message: "Not Found", HasOAuthScopes: true, OAuthScopes: "public_repo"}, "ghp_x", ErrNotFound, `lacks the "repo" scope`},
		{"fine-grained", &githubclient.APIError{Status: 404, Message: "Not Found"}, "github_pat_x", ErrNotFound, "resource owner is \"acme\""},
		{"classic with repo scope", &githubclient.APIError{Status: 404, Message: "Not Found", HasOAuthScopes: true, OAuthScopes: "repo"}, "ghp_x", ErrNotFound, "collaborator or member"},
		{"saml sso", &githubclient.APIError{Status: 403, Message: "Resource protected by organization SAML enforcement.", SSO: "required; url=https://github.com/orgs/acme/sso?x=1"}, "gho_x", ErrForbidden, "https://github.com/orgs/acme/sso?x=1"},
		{"oauth restrictions", &githubclient.APIError{Status: 403, Message: "Although you appear to have the correct authorization credentials, the `acme` organization has enabled OAuth App access restrictions"}, "gho_x", ErrForbidden, "approve this app"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := mapGitHubRepoErr(tc.err, tc.token, "acme", "private", "Repository not found or inaccessible.")
			if !errors.Is(err, tc.wantIs) {
				t.Fatalf("err %v is not %v", err, tc.wantIs)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("err %q missing %q", err.Error(), tc.wantSub)
			}
		})
	}
}

func TestMapGitHubRepoErrNonAccessFallsThrough(t *testing.T) {
	err := mapGitHubRepoErr(&githubclient.APIError{Status: 422, Message: "Validation Failed"}, "ghp_x", "acme", "r", "Failed to create GitHub issue.")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("expected validation error, got %v", err)
	}
}

func TestGitHubTokenScopeWarning(t *testing.T) {
	if w := githubTokenScopeWarning(&githubclient.User{ScopesKnown: true, Scopes: []string{"public_repo"}}); w == "" {
		t.Error("expected warning for public_repo-only token")
	}
	if w := githubTokenScopeWarning(&githubclient.User{ScopesKnown: true, Scopes: []string{"repo"}}); w != "" {
		t.Errorf("unexpected warning %q", w)
	}
	if w := githubTokenScopeWarning(&githubclient.User{}); w != "" {
		t.Errorf("fine-grained tokens should not warn, got %q", w)
	}
}
