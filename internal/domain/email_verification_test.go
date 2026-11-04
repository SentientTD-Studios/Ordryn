package domain

import (
	"context"
	"errors"
	"testing"

	"GoTodo/internal/storage"
)

func insertUnverifiedUser(t *testing.T, email string) int {
	t.Helper()
	pool, err := storage.OpenDatabase()
	if err != nil {
		t.Fatal(err)
	}
	var id int
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO users (email, password, role_id, user_name, email_verified_at)
		VALUES ($1, 'x', (SELECT id FROM roles WHERE name = 'user'), $1, NULL) RETURNING id`,
		email).Scan(&id); err != nil {
		t.Fatalf("insert %s: %v", email, err)
	}
	return id
}

func mustBeVerified(t *testing.T, userID int, want bool) {
	t.Helper()
	got, err := storage.IsEmailVerified(userID)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("user %d verified = %v, want %v", userID, got, want)
	}
}

// Existing accounts and every account not created by open sign-up count as
// verified, so the migration locks nobody out.
func TestAccountsAreVerifiedByDefault(t *testing.T) {
	id := insertUserWithRole(t, "default-verified@example.com", "user")
	mustBeVerified(t, id, true)
}

func TestEmailVerificationLinkWorksOnce(t *testing.T) {
	id := insertUnverifiedUser(t, "verify-once@example.com")
	mustBeVerified(t, id, false)

	v, err := storage.IssueEmailVerification(id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := storage.VerifyEmailWithToken(v.ID, "wrong-token"); !errors.Is(err, storage.ErrVerificationInvalid) {
		t.Fatalf("wrong token: err = %v", err)
	}
	mustBeVerified(t, id, false)

	got, err := storage.VerifyEmailWithToken(v.ID, v.Token)
	if err != nil || got != id {
		t.Fatalf("VerifyEmailWithToken = %d, %v; want %d", got, err, id)
	}
	mustBeVerified(t, id, true)

	if _, err := storage.VerifyEmailWithToken(v.ID, v.Token); !errors.Is(err, storage.ErrVerificationInvalid) {
		t.Fatalf("second use: err = %v", err)
	}
}

func TestNewVerificationLinkReplacesOld(t *testing.T) {
	id := insertUnverifiedUser(t, "verify-replace@example.com")
	first, err := storage.IssueEmailVerification(id)
	if err != nil {
		t.Fatal(err)
	}
	second, err := storage.IssueEmailVerification(id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := storage.VerifyEmailWithToken(first.ID, first.Token); !errors.Is(err, storage.ErrVerificationInvalid) {
		t.Fatalf("old link should stop working: err = %v", err)
	}
	if _, err := storage.VerifyEmailWithToken(second.ID, second.Token); err != nil {
		t.Fatalf("new link: %v", err)
	}
}

func TestExpiredVerificationLinkIsRejected(t *testing.T) {
	id := insertUnverifiedUser(t, "verify-expired@example.com")
	v, err := storage.IssueEmailVerification(id)
	if err != nil {
		t.Fatal(err)
	}
	pool, err := storage.OpenDatabase()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(),
		`UPDATE email_verifications SET expires_at = NOW() - INTERVAL '1 minute' WHERE id = $1`, v.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := storage.VerifyEmailWithToken(v.ID, v.Token); !errors.Is(err, storage.ErrVerificationInvalid) {
		t.Fatalf("expired link: err = %v", err)
	}
	mustBeVerified(t, id, false)
}

func TestVerificationRejectsMalformedID(t *testing.T) {
	if _, err := storage.VerifyEmailWithToken("not-a-uuid", "x"); !errors.Is(err, storage.ErrVerificationInvalid) {
		t.Fatalf("err = %v", err)
	}
}

// A password-reset link is delivered to the inbox, so using one proves the
// address too. This also lets someone who lost the confirmation email recover.
func TestPasswordResetConfirmsEmail(t *testing.T) {
	email := "verify-by-reset@example.com"
	id := insertUnverifiedUser(t, email)
	reset, err := storage.GenerateResetToken(email)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := storage.ResetPasswordWithToken(reset.ID, reset.Token, "new-hash"); err != nil {
		t.Fatal(err)
	}
	mustBeVerified(t, id, true)
}
