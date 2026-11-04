package mailer

import (
	"errors"
	"fmt"
	"testing"
)

// Sign-up mail must never use up the core budget that password resets rely on.
func TestSignupMailHasItsOwnBudget(t *testing.T) {
	signupLimiter.reset()
	coreLimiter.reset()
	t.Cleanup(func() {
		signupLimiter.reset()
		coreLimiter.reset()
	})

	sent := 0
	for i := 0; i < signupGlobalLimit+10; i++ {
		if err := allowSendFor(TriggerEmailVerification, fmt.Sprintf("flood%d@example.com", i)); err == nil {
			sent++
		} else if !errors.Is(err, ErrRateLimited) {
			t.Fatal(err)
		}
	}
	if sent != signupGlobalLimit {
		t.Fatalf("sign-up sends allowed = %d, want %d", sent, signupGlobalLimit)
	}
	if err := allowSendFor(TriggerPasswordReset, "real-user@example.com"); err != nil {
		t.Fatalf("password reset blocked by a sign-up flood: %v", err)
	}
}

func TestSignupMailPerRecipientLimit(t *testing.T) {
	signupLimiter.reset()
	t.Cleanup(signupLimiter.reset)
	for i := 0; i < signupRecipientLimit; i++ {
		if err := allowSendFor(TriggerSignupExisting, "owner@example.com"); err != nil {
			t.Fatal(err)
		}
	}
	if err := allowSendFor(TriggerSignupExisting, "owner@example.com"); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("err = %v, want ErrRateLimited", err)
	}
}
