package domain

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
)

func TestVerifyInboundAuthHMACAndSecret(t *testing.T) {
	secret := "s3cret"
	body := []byte(`{"action":"create","title":"Ship"}`)
	if err := verifyInboundAuth(secret, secret, "", body); err != nil {
		t.Fatal(err)
	}
	if err := verifyInboundAuth(secret, "nope", "", body); !errors.Is(err, ErrForbidden) {
		t.Fatalf("err=%v", err)
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	hdr := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if err := verifyInboundAuth(secret, "", hdr, body); err != nil {
		t.Fatal(err)
	}
	if err := verifyInboundAuth(secret, "", "sha256=00", body); !errors.Is(err, ErrForbidden) {
		t.Fatalf("err=%v", err)
	}
	if err := verifyInboundAuth(secret, "", "", body); err == nil {
		t.Fatal("expected missing auth")
	}
}
