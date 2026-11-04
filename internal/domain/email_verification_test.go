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
	if _, err := storage.VerifyEmailWithToken(v.ID, "wrong-token", anyPassword); !errors.Is(err, storage.ErrVerificationInvalid) {
		t.Fatalf("wrong token: err = %v", err)
	}
	mustBeVerified(t, id, false)

	got, err := storage.VerifyEmailWithToken(v.ID, v.Token, anyPassword)
	if err != nil || got != id {
		t.Fatalf("VerifyEmailWithToken = %d, %v; want %d", got, err, id)
	}
	mustBeVerified(t, id, true)

	if _, err := storage.VerifyEmailWithToken(v.ID, v.Token, anyPassword); !errors.Is(err, storage.ErrVerificationInvalid) {
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
	if _, err := storage.VerifyEmailWithToken(first.ID, first.Token, anyPassword); !errors.Is(err, storage.ErrVerificationInvalid) {
		t.Fatalf("old link should stop working: err = %v", err)
	}
	if _, err := storage.VerifyEmailWithToken(second.ID, second.Token, anyPassword); err != nil {
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
	if _, err := storage.VerifyEmailWithToken(v.ID, v.Token, anyPassword); !errors.Is(err, storage.ErrVerificationInvalid) {
		t.Fatalf("expired link: err = %v", err)
	}
	mustBeVerified(t, id, false)
}

func TestVerificationRejectsMalformedID(t *testing.T) {
	if _, err := storage.VerifyEmailWithToken("not-a-uuid", "x", anyPassword); !errors.Is(err, storage.ErrVerificationInvalid) {
		t.Fatalf("err = %v", err)
	}
}

// A password-reset link is delivered to the inbox, so using one proves the
// address too. This also lets someone who lost the confirmation email recover.
func TestPasswordResetConfirmsEmail(t *testing.T) {
	email := "verify-by-reset@example.com"
	id := insertUnverifiedUser(t, email)
	reset, err := storage.GenerateResetToken(id, email)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := storage.ResetPasswordWithToken(reset.ID, reset.Token, "new-hash"); err != nil {
		t.Fatal(err)
	}
	mustBeVerified(t, id, true)
}

// anyPassword stands in for the bcrypt check where a test isn't about the password.
func anyPassword(string) bool { return true }

func passwordIs(want string) func(string) bool {
	return func(hash string) bool { return hash == want }
}

// A confirmation link needs the account's password too: a wrong one is refused
// and leaves the link usable, so the right one still works afterwards.
func TestVerificationNeedsAccountPassword(t *testing.T) {
	id := insertUnverifiedUser(t, "verify-password@example.com") // stored password is 'x'
	v, err := storage.IssueEmailVerification(id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := storage.VerifyEmailWithToken(v.ID, v.Token, passwordIs("not-x")); !errors.Is(err, storage.ErrVerificationPassword) {
		t.Fatalf("wrong password: err = %v", err)
	}
	mustBeVerified(t, id, false)
	if _, err := storage.VerifyEmailWithToken(v.ID, v.Token, nil); !errors.Is(err, storage.ErrVerificationPassword) {
		t.Fatalf("no password check: err = %v", err)
	}
	if _, err := storage.VerifyEmailWithToken(v.ID, v.Token, passwordIs("x")); err != nil {
		t.Fatalf("right password: %v", err)
	}
	mustBeVerified(t, id, true)
}

// Pre-hijacking: someone signs up first with another person's address. When the
// owner signs up too, their details replace the first sign-up's, so the first
// sign-up's password can't be activated by the owner's confirmation.
func TestRepeatSignUpReplacesUnconfirmedCredentials(t *testing.T) {
	id := insertUnverifiedUser(t, "prehijack@example.com")
	attackerLink, err := storage.IssueEmailVerification(id)
	if err != nil {
		t.Fatal(err)
	}
	before := sessionVersionOf(t, id)

	replaced, err := storage.ReplaceUnverifiedRegistration(id, "owner-hash", "Europe/Paris", "prehijack-owner")
	if err != nil || !replaced {
		t.Fatalf("replace = %v, %v", replaced, err)
	}
	hash, err := storage.GetPasswordHashByID(id)
	if err != nil {
		t.Fatal(err)
	}
	if hash != "owner-hash" {
		t.Fatalf("password hash = %q, want the latest sign-up's", hash)
	}
	if after := sessionVersionOf(t, id); after <= before {
		t.Fatalf("session_version %d -> %d, want a bump", before, after)
	}
	// Links issued before the repeat sign-up stop working.
	if _, err := storage.VerifyEmailWithToken(attackerLink.ID, attackerLink.Token, anyPassword); !errors.Is(err, storage.ErrVerificationInvalid) {
		t.Fatalf("old link: err = %v", err)
	}
	// A new link with the old password ('x') is refused; with the owner's it works.
	v, err := storage.IssueEmailVerification(id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := storage.VerifyEmailWithToken(v.ID, v.Token, passwordIs("x")); !errors.Is(err, storage.ErrVerificationPassword) {
		t.Fatalf("first sign-up's password: err = %v", err)
	}
	if _, err := storage.VerifyEmailWithToken(v.ID, v.Token, passwordIs("owner-hash")); err != nil {
		t.Fatalf("owner's password: %v", err)
	}
	mustBeVerified(t, id, true)

	// Once confirmed, a sign-up can no longer change the account.
	replaced, err = storage.ReplaceUnverifiedRegistration(id, "late-hash", "", "prehijack-late")
	if err != nil || replaced {
		t.Fatalf("replace after confirmation = %v, %v; want false", replaced, err)
	}
}

// A confirmation link left over after the email was confirmed another way (a
// password reset) does nothing.
func TestVerificationLinkUnusableOnceConfirmed(t *testing.T) {
	email := "verify-after-reset@example.com"
	id := insertUnverifiedUser(t, email)
	v, err := storage.IssueEmailVerification(id)
	if err != nil {
		t.Fatal(err)
	}
	reset, err := storage.GenerateResetToken(id, email)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := storage.ResetPasswordWithToken(reset.ID, reset.Token, "new-hash"); err != nil {
		t.Fatal(err)
	}
	if _, err := storage.VerifyEmailWithToken(v.ID, v.Token, anyPassword); !errors.Is(err, storage.ErrVerificationInvalid) {
		t.Fatalf("link after reset: err = %v", err)
	}
}

func resetPassword(t *testing.T, userID int, email string) {
	t.Helper()
	reset, err := storage.GenerateResetToken(userID, email)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := storage.ResetPasswordWithToken(reset.ID, reset.Token, "new-hash"); err != nil {
		t.Fatal(err)
	}
}

func apiKeyReviewNotices(t *testing.T, userID int) int {
	t.Helper()
	items, _, err := storage.ListUserNotifications(userID, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, item := range items {
		if item.Type == storage.NotificationAPIKeyReview {
			n++
		}
	}
	return n
}

// API keys survive a password reset, but the owner is asked to check them, even
// if they turned other notifications off.
func TestPasswordResetKeepsAPIKeysAndAsksForReview(t *testing.T) {
	email := "reset-keeps-keys@example.com"
	id := insertUserWithRole(t, email, "user")
	key, _, err := storage.CreateAPIKey(id, "before-reset")
	if err != nil {
		t.Fatal(err)
	}
	pool, err := storage.OpenDatabase()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO user_notification_optouts (user_id, notification_type) VALUES ($1, $2)`,
		id, storage.NotificationAPIKeyReview); err != nil {
		t.Fatal(err)
	}

	resetPassword(t, id, email)

	if _, err := storage.LookupAPIKey(key); err != nil {
		t.Fatalf("API key should keep working after a reset: %v", err)
	}
	if got := apiKeyReviewNotices(t, id); got != 1 {
		t.Fatalf("API key review notices = %d, want 1", got)
	}
}

// Without any active key there's nothing to review.
func TestPasswordResetWithoutAPIKeysSendsNoReview(t *testing.T) {
	email := "reset-no-keys@example.com"
	id := insertUserWithRole(t, email, "user")
	resetPassword(t, id, email)
	if got := apiKeyReviewNotices(t, id); got != 0 {
		t.Fatalf("API key review notices = %d, want 0", got)
	}
}
