package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestChatLinkEndpointsNeedChatCredentials(t *testing.T) {
	for _, tc := range []struct {
		method, path string
		h            http.HandlerFunc
	}{
		{http.MethodGet, "/api/v2/chat/projects?user_id=1", APIV2ChatProjects},
		{http.MethodPut, "/api/v2/chat/links/abc", APIV2ChatLinks},
		{http.MethodDelete, "/api/v2/chat/links/abc", APIV2ChatLinks},
		{http.MethodPost, "/api/v2/chat/links/abc/tasks", APIV2ChatLinks},
		{http.MethodGet, "/api/v2/chat/events", APIV2ChatEvents},
	} {
		rec := httptest.NewRecorder()
		tc.h(rec, httptest.NewRequest(tc.method, tc.path, nil))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without credentials: status %d, want 401", tc.method, tc.path, rec.Code)
		}
	}
}

func TestChatLinkIDPattern(t *testing.T) {
	for _, ok := range []string{"a", "lnk_12", "A-b_9", "0123456789012345678901234567890123456789012345678901234567890123"} {
		if !chatLinkIDRe.MatchString(ok) {
			t.Errorf("%q should be a valid link id", ok)
		}
	}
	for _, bad := range []string{"", "a/b", "a b", "../x", "01234567890123456789012345678901234567890123456789012345678901234"} {
		if chatLinkIDRe.MatchString(bad) {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

func TestChatSourceKeyPattern(t *testing.T) {
	for _, ok := range []string{"chatmsg_42", "a", "x.y:z-1"} {
		if !chatSourceKeyRe.MatchString(ok) {
			t.Errorf("%q should be a valid source key", ok)
		}
	}
	for _, bad := range []string{"", "a b", "a/b", string(make([]byte, 101))} {
		if chatSourceKeyRe.MatchString(bad) {
			t.Errorf("%q should be rejected", bad)
		}
	}
}
