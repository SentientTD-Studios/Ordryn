package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAPIV1AdminCommentAuditMethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v2/admin/comment-audit", nil)
	rec := httptest.NewRecorder()
	APIV1AdminCommentAuditRouter(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestParseCommentAuditListQuery(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v2/admin/comment-audit?kind=nope", nil)
	_, errMsg := parseCommentAuditListQuery(req)
	if errMsg == "" {
		t.Fatal("expected invalid kind error")
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v2/admin/comment-audit?kind=edit&limit=10&offset=2&q=hello", nil)
	f, errMsg := parseCommentAuditListQuery(req)
	if errMsg != "" {
		t.Fatalf("err=%q", errMsg)
	}
	if f.Kind != "edit" || f.Limit != 10 || f.Offset != 2 || f.Query != "hello" {
		t.Fatalf("filter %+v", f)
	}
}
