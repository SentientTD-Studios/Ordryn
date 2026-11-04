package domain

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"GoTodo/internal/storage"
)

func sessionVersionOf(t *testing.T, userID int) int {
	t.Helper()
	state, err := storage.GetSessionState(userID)
	if err != nil {
		t.Fatal(err)
	}
	return state.Version
}

func TestSessionVersionRaisedByPasswordChangeAndDeactivation(t *testing.T) {
	id := insertUserWithRole(t, "session-version@example.com", "user")
	start := sessionVersionOf(t, id)

	if err := storage.UpdatePasswordByID(id, "new-hash"); err != nil {
		t.Fatal(err)
	}
	if got := sessionVersionOf(t, id); got != start+1 {
		t.Fatalf("after password change version = %d, want %d", got, start+1)
	}

	if err := storage.SetUserBanned(id, true); err != nil {
		t.Fatal(err)
	}
	state, _ := storage.GetSessionState(id)
	if state.Active || state.Version != start+2 {
		t.Fatalf("after deactivation state = %+v, want inactive at version %d", state, start+2)
	}

	// Reactivating must not bring old sessions back.
	if err := storage.SetUserBanned(id, false); err != nil {
		t.Fatal(err)
	}
	state, _ = storage.GetSessionState(id)
	if !state.Active || state.Version != start+2 {
		t.Fatalf("after reactivation state = %+v, want active at version %d", state, start+2)
	}

	v, err := storage.BumpSessionVersion(id)
	if err != nil || v != start+3 {
		t.Fatalf("BumpSessionVersion = %d, %v; want %d", v, err, start+3)
	}
}

func TestSessionStateForMissingAccountIsInactive(t *testing.T) {
	state, err := storage.GetSessionState(987654321)
	if err != nil || state.Active {
		t.Fatalf("missing account: state = %+v, err = %v; want inactive, no error", state, err)
	}
}

func TestResetTokenIsHashedAndWorksOnce(t *testing.T) {
	email := "reset-once@example.com"
	id := insertUserWithRole(t, email, "user")
	startVersion := sessionVersionOf(t, id)

	reset, err := storage.GenerateResetToken(id, email)
	if err != nil {
		t.Fatal(err)
	}

	pool, err := storage.OpenDatabase()
	if err != nil {
		t.Fatal(err)
	}
	var stored *string
	var hash string
	if err := pool.QueryRow(context.Background(),
		`SELECT token, token_hash FROM password_reset WHERE id = $1`, reset.ID).Scan(&stored, &hash); err != nil {
		t.Fatal(err)
	}
	if stored != nil || hash == "" || hash == reset.Token {
		t.Fatalf("token should be stored only as a hash; token=%v hash=%q", stored, hash)
	}

	if _, err := storage.ValidateResetToken(reset.ID, reset.Token); err != nil {
		t.Fatalf("fresh token should validate: %v", err)
	}
	if _, err := storage.ValidateResetToken(reset.ID, "wrong"); !errors.Is(err, storage.ErrResetTokenInvalid) {
		t.Fatalf("wrong token: err = %v, want ErrResetTokenInvalid", err)
	}

	got, err := storage.ResetPasswordWithToken(reset.ID, reset.Token, "reset-hash")
	if err != nil || got != email {
		t.Fatalf("ResetPasswordWithToken = %q, %v; want %q", got, err, email)
	}
	if hash, _ := storage.GetPasswordHashByID(id); hash != "reset-hash" {
		t.Fatalf("password hash = %q, want reset-hash", hash)
	}
	if v := sessionVersionOf(t, id); v != startVersion+1 {
		t.Fatalf("reset should sign out sessions: version = %d, want %d", v, startVersion+1)
	}

	if _, err := storage.ResetPasswordWithToken(reset.ID, reset.Token, "again"); !errors.Is(err, storage.ErrResetTokenInvalid) {
		t.Fatalf("second use: err = %v, want ErrResetTokenInvalid", err)
	}
}

func TestResetTokenConcurrentUseSucceedsOnce(t *testing.T) {
	email := "reset-race@example.com"
	id := insertUserWithRole(t, email, "user")
	reset, err := storage.GenerateResetToken(id, email)
	if err != nil {
		t.Fatal(err)
	}

	const attempts = 8
	var wg sync.WaitGroup
	var mu sync.Mutex
	successes := 0
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := storage.ResetPasswordWithToken(reset.ID, reset.Token, "race-hash"); err == nil {
				mu.Lock()
				successes++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if successes != 1 {
		t.Fatalf("%d concurrent resets succeeded; want exactly 1", successes)
	}
}

func TestExpiredResetTokenIsRejected(t *testing.T) {
	email := "reset-expired@example.com"
	id := insertUserWithRole(t, email, "user")
	reset, err := storage.GenerateResetTokenTTL(id, email, -time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := storage.ValidateResetToken(reset.ID, reset.Token); !errors.Is(err, storage.ErrResetTokenInvalid) {
		t.Fatalf("validate expired: err = %v", err)
	}
	if _, err := storage.ResetPasswordWithToken(reset.ID, reset.Token, "x"); !errors.Is(err, storage.ErrResetTokenInvalid) {
		t.Fatalf("reset with expired: err = %v", err)
	}
}

func TestResetTokenRejectsMalformedID(t *testing.T) {
	if _, err := storage.ValidateResetToken("not-a-uuid", "x"); !errors.Is(err, storage.ErrResetTokenInvalid) {
		t.Fatalf("err = %v, want ErrResetTokenInvalid", err)
	}
	if _, err := storage.ResetPasswordWithToken("not-a-uuid", "x", "y"); !errors.Is(err, storage.ErrResetTokenInvalid) {
		t.Fatalf("err = %v, want ErrResetTokenInvalid", err)
	}
}

// Accounts whose emails differ only by case can exist; a reset link must change
// only the account it was issued for.
func TestResetChangesOnlyItsOwnAccount(t *testing.T) {
	allowEmailCaseDuplicates(t)
	older := insertUserWithPassword(t, "Twin@Example.com", "hash-older")
	newer := insertUserWithPassword(t, "twin@example.com", "hash-newer")
	t.Cleanup(func() {
		deleteUser(t, older)
		deleteUser(t, newer)
	})
	olderVersion := sessionVersionOf(t, older)

	reset, err := storage.GenerateResetToken(newer, "twin@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := storage.ResetPasswordWithToken(reset.ID, reset.Token, "hash-reset"); err != nil {
		t.Fatal(err)
	}
	if h, _ := storage.GetPasswordHashByID(newer); h != "hash-reset" {
		t.Fatalf("target account hash = %q, want hash-reset", h)
	}
	if h, _ := storage.GetPasswordHashByID(older); h != "hash-older" {
		t.Fatalf("other account's password changed to %q", h)
	}
	if v := sessionVersionOf(t, older); v != olderVersion {
		t.Fatalf("other account was signed out: version %d -> %d", olderVersion, v)
	}
}

// Links sent before the upgrade stored the token in plain text; the migration
// must hash them in place so they keep working.
func TestMigrationHashesLegacyResetTokens(t *testing.T) {
	email := "reset-legacy@example.com"
	insertUserWithRole(t, email, "user")
	pool, err := storage.OpenDatabase()
	if err != nil {
		t.Fatal(err)
	}
	const legacyID = "6f1c1d3e-2b6a-4e8e-9a51-0d4f3a2b1c00"
	const legacyToken = "legacy-plain-token"
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO password_reset (id, email, token, expires_at) VALUES ($1, $2, $3, $4)`,
		legacyID, email, legacyToken, time.Now().UTC().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	if err := storage.MigratePasswordResetHashTokens(); err != nil {
		t.Fatal(err)
	}

	var stored *string
	if err := pool.QueryRow(context.Background(),
		`SELECT token FROM password_reset WHERE id = $1`, legacyID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != nil {
		t.Fatalf("plain-text token should be cleared, still %q", *stored)
	}
	if _, err := storage.ValidateResetToken(legacyID, legacyToken); err != nil {
		t.Fatalf("legacy link should still work after migration: %v", err)
	}
}
