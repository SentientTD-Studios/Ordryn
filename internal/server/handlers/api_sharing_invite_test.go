package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"GoTodo/internal/domain"
)

func TestSharingClientMessage(t *testing.T) {
	if got := sharingClientMessage(fmt.Errorf("%w: username not found", domain.ErrNotFound), "Not found."); got != "username not found" {
		t.Fatalf("not found wrap: got %q", got)
	}
	if got := sharingClientMessage(fmt.Errorf("%w: user is already a member", domain.ErrValidation), "Invalid."); got != "user is already a member" {
		t.Fatalf("validation wrap: got %q", got)
	}
	if got := sharingClientMessage(domain.ErrNotFound, "Not found."); got != "Not found." {
		t.Fatalf("bare not found: got %q", got)
	}
	if got := sharingClientMessage(domain.ErrForbidden, "Forbidden."); got != "Forbidden." {
		t.Fatalf("bare forbidden: got %q", got)
	}
}

func TestWriteSharingDomainError_UsernameNotFound(t *testing.T) {
	rec := httptest.NewRecorder()
	writeSharingDomainError(rec, fmt.Errorf("%w: username not found", domain.ErrNotFound))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d", rec.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["error"] != "not_found" {
		t.Fatalf("error=%q", body["error"])
	}
	if body["message"] != "username not found" {
		t.Fatalf("message=%q", body["message"])
	}
}

func TestWriteSharingDomainError_AlreadyMember(t *testing.T) {
	rec := httptest.NewRecorder()
	writeSharingDomainError(rec, fmt.Errorf("%w: user is already a member", domain.ErrValidation))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d", rec.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["message"] != "user is already a member" {
		t.Fatalf("message=%q", body["message"])
	}
}

func TestWriteSharingDomainError_Internal(t *testing.T) {
	rec := httptest.NewRecorder()
	writeSharingDomainError(rec, errors.New("boom"))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d", rec.Code)
	}
}
