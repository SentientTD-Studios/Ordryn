package handlers

import (
	"testing"
	"time"

	"GoTodo/internal/storage"
)

func TestParseInviteExpiration(t *testing.T) {
	// Empty string returns nil
	exp, err := parseInviteExpiration("")
	if err != nil || exp != nil {
		t.Fatalf("expected nil, nil; got %v, %v", exp, err)
	}

	// Date format
	exp, err = parseInviteExpiration("2026-12-31")
	if err != nil || exp == nil {
		t.Fatalf("failed to parse date: %v", err)
	}
	if exp.Year() != 2026 || exp.Month() != 12 || exp.Day() != 31 {
		t.Fatalf("unexpected date: %v", exp)
	}

	// RFC3339
	nowStr := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	exp, err = parseInviteExpiration(nowStr)
	if err != nil || exp == nil {
		t.Fatalf("failed to parse RFC3339: %v", err)
	}

	// Invalid format
	_, err = parseInviteExpiration("not-a-date")
	if err == nil {
		t.Fatal("expected error for invalid date format")
	}
}

func TestInviteToJSON_Status(t *testing.T) {
	// Pending invite
	future := time.Now().Add(24 * time.Hour)
	invPending := storage.Invite{
		ID:        1,
		Email:     "pending@example.com",
		Token:     "tok1",
		Used:      false,
		CreatedAt: time.Now(),
		ExpiresAt: &future,
	}
	m := inviteToJSON(invPending)
	if m["status"] != "pending" {
		t.Fatalf("expected status 'pending', got %v", m["status"])
	}

	// Used invite
	invUsed := storage.Invite{
		ID:        2,
		Email:     "used@example.com",
		Token:     "tok2",
		Used:      true,
		CreatedAt: time.Now(),
		ExpiresAt: &future,
	}
	m = inviteToJSON(invUsed)
	if m["status"] != "used" {
		t.Fatalf("expected status 'used', got %v", m["status"])
	}

	// Expired invite
	past := time.Now().Add(-24 * time.Hour)
	invExpired := storage.Invite{
		ID:        3,
		Email:     "expired@example.com",
		Token:     "tok3",
		Used:      false,
		CreatedAt: time.Now().Add(-48 * time.Hour),
		ExpiresAt: &past,
	}
	m = inviteToJSON(invExpired)
	if m["status"] != "expired" {
		t.Fatalf("expected status 'expired', got %v", m["status"])
	}
}

func TestInviteCreateRequest_ExpirationValidation(t *testing.T) {
	days := 7
	maxExpiry := time.Now().AddDate(0, 0, days)

	// Valid non-admin date within limit
	parsedWithin, err := parseInviteExpiration(time.Now().AddDate(0, 0, 3).Format("2006-01-02"))
	if err != nil || parsedWithin == nil {
		t.Fatalf("unexpected error parsing date: %v", err)
	}
	if parsedWithin.After(maxExpiry.Add(24 * time.Hour)) {
		t.Fatalf("date within limit was incorrectly marked as after limit")
	}

	// Exceeded non-admin date beyond limit
	parsedExceeded, err := parseInviteExpiration(time.Now().AddDate(0, 0, 30).Format("2006-01-02"))
	if err != nil || parsedExceeded == nil {
		t.Fatalf("unexpected error parsing date: %v", err)
	}
	if !parsedExceeded.After(maxExpiry.Add(24 * time.Hour)) {
		t.Fatalf("date beyond limit should be recognized as exceeding limit")
	}
}

