package domain

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"testing"
	"time"
)

func TestApplyInboundWebhookDisabled(t *testing.T) {
	err := ApplyInboundWebhook(context.Background(), "", "", "", []byte(`{"action":"create","title":"Ship"}`), InboundWebhookInput{
		ProjectID: 1,
		Action:    "create",
		Title:     "Ship",
	})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("err=%v", err)
	}
}

func TestApplyInboundWebhookRequiresProjectID(t *testing.T) {
	err := ApplyInboundWebhook(context.Background(), "secret", "", "", []byte(`{"action":"create","title":"Ship"}`), InboundWebhookInput{
		Action: "create",
		Title:  "Ship",
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("err=%v", err)
	}
}

func TestVerifyInboundAuthHMACAndSecret(t *testing.T) {
	secret := "s3cret"
	body := []byte(`{"action":"create","title":"Ship"}`)
	if err := verifyInboundAuth(secret, secret, "", "", body); err != nil {
		t.Fatal(err)
	}
	if err := verifyInboundAuth(secret, "nope", "", "", body); !errors.Is(err, ErrForbidden) {
		t.Fatalf("err=%v", err)
	}
	ts := strconv.FormatInt(time.Now().UTC().Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(ts + "."))
	_, _ = mac.Write(body)
	hdr := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if err := verifyInboundAuth(secret, "", hdr, ts, body); err != nil {
		t.Fatal(err)
	}
	if err := verifyInboundAuth(secret, "", hdr, "", body); !errors.Is(err, ErrForbidden) {
		t.Fatalf("missing timestamp err=%v", err)
	}
	if err := verifyInboundAuth(secret, "", hdr, strconv.FormatInt(time.Now().UTC().Unix()-1000, 10), body); !errors.Is(err, ErrForbidden) {
		t.Fatalf("stale timestamp err=%v", err)
	}
	if err := verifyInboundAuth(secret, "", "sha256=00", ts, body); !errors.Is(err, ErrForbidden) {
		t.Fatalf("err=%v", err)
	}
	if err := verifyInboundAuth(secret, "", "", "", body); err == nil {
		t.Fatal("expected missing auth")
	}
}
