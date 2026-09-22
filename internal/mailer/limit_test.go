package mailer

import (
	"errors"
	"testing"
	"time"
)

func TestAllowCoreSendRecipientCap(t *testing.T) {
	lim := newCoreSendLimiter()
	lim.recipientN = 2
	lim.globalN = 100
	for i := 0; i < 2; i++ {
		if err := lim.allow("user@example.com"); err != nil {
			t.Fatalf("send %d: %v", i, err)
		}
	}
	if err := lim.allow("user@example.com"); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("err = %v, want ErrRateLimited", err)
	}
	if err := lim.allow("other@example.com"); err != nil {
		t.Fatalf("other recipient: %v", err)
	}
}

func TestAllowCoreSendGlobalCap(t *testing.T) {
	lim := newCoreSendLimiter()
	lim.recipientN = 100
	lim.globalN = 3
	for i := 0; i < 3; i++ {
		to := "user" + string(rune('a'+i)) + "@example.com"
		if err := lim.allow(to); err != nil {
			t.Fatalf("send %d: %v", i, err)
		}
	}
	if err := lim.allow("last@example.com"); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("err = %v, want ErrRateLimited", err)
	}
}

func TestAllowCoreSendWindowExpires(t *testing.T) {
	lim := newCoreSendLimiter()
	lim.recipientN = 1
	lim.recipientW = time.Minute
	now := time.Now()
	lim.now = func() time.Time { return now }
	if err := lim.allow("user@example.com"); err != nil {
		t.Fatal(err)
	}
	if err := lim.allow("user@example.com"); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("err = %v", err)
	}
	lim.now = func() time.Time { return now.Add(2 * time.Minute) }
	if err := lim.allow("user@example.com"); err != nil {
		t.Fatalf("after window: %v", err)
	}
}
