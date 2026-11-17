package domain

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"testing"

	"github.com/fxamacker/cbor/v2"

	"GoTodo/internal/storage"
)

var testRP = PasskeyRP{ID: "todo.example.test", Origin: "https://todo.example.test", DisplayName: "Test"}

var b64 = base64.RawURLEncoding

// softAuthenticator is a minimal passkey authenticator for tests: one P-256 key,
// "none" attestation, user presence and verification always set.
type softAuthenticator struct {
	key    *ecdsa.PrivateKey
	credID []byte
	handle []byte
	count  uint32
}

func newSoftAuthenticator(t *testing.T) *softAuthenticator {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	id := make([]byte, 16)
	_, _ = rand.Read(id)
	return &softAuthenticator{key: k, credID: id}
}

func (a *softAuthenticator) authData(rpID string, attested bool) []byte {
	h := sha256.Sum256([]byte(rpID))
	out := append([]byte{}, h[:]...)
	flags := byte(0x01 | 0x04) // UP | UV
	if attested {
		flags |= 0x40 // AT
	}
	out = append(out, flags)
	a.count++
	var c [4]byte
	binary.BigEndian.PutUint32(c[:], a.count)
	out = append(out, c[:]...)
	if attested {
		out = append(out, make([]byte, 16)...) // AAGUID
		var l [2]byte
		binary.BigEndian.PutUint16(l[:], uint16(len(a.credID)))
		out = append(out, l[:]...)
		out = append(out, a.credID...)
		x := a.key.PublicKey.X.FillBytes(make([]byte, 32))
		y := a.key.PublicKey.Y.FillBytes(make([]byte, 32))
		cose, _ := cbor.Marshal(map[int]any{1: 2, 3: -7, -1: 1, -2: x, -3: y})
		out = append(out, cose...)
	}
	return out
}

func challengeFrom(t *testing.T, opts json.RawMessage) (challenge string, userID string) {
	t.Helper()
	var o struct {
		PublicKey struct {
			Challenge string `json:"challenge"`
			User      struct {
				ID string `json:"id"`
			} `json:"user"`
		} `json:"publicKey"`
	}
	if err := json.Unmarshal(opts, &o); err != nil {
		t.Fatalf("options: %v", err)
	}
	return o.PublicKey.Challenge, o.PublicKey.User.ID
}

func (a *softAuthenticator) create(t *testing.T, opts json.RawMessage, origin string) []byte {
	t.Helper()
	challenge, userID := challengeFrom(t, opts)
	h, err := b64.DecodeString(userID)
	if err != nil {
		t.Fatalf("user id: %v", err)
	}
	a.handle = h
	cd, _ := json.Marshal(map[string]string{"type": "webauthn.create", "challenge": challenge, "origin": origin})
	att, _ := cbor.Marshal(map[string]any{"fmt": "none", "attStmt": map[string]any{}, "authData": a.authData(testRP.ID, true)})
	resp, _ := json.Marshal(map[string]any{
		"id": b64.EncodeToString(a.credID), "rawId": b64.EncodeToString(a.credID), "type": "public-key",
		"response": map[string]string{
			"clientDataJSON":    b64.EncodeToString(cd),
			"attestationObject": b64.EncodeToString(att),
		},
	})
	return resp
}

func (a *softAuthenticator) get(t *testing.T, opts json.RawMessage, origin string) []byte {
	t.Helper()
	challenge, _ := challengeFrom(t, opts)
	cd, _ := json.Marshal(map[string]string{"type": "webauthn.get", "challenge": challenge, "origin": origin})
	ad := a.authData(testRP.ID, false)
	sum := sha256.Sum256(cd)
	digest := sha256.Sum256(append(append([]byte{}, ad...), sum[:]...))
	sig, err := ecdsa.SignASN1(rand.Reader, a.key, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	resp, _ := json.Marshal(map[string]any{
		"id": b64.EncodeToString(a.credID), "rawId": b64.EncodeToString(a.credID), "type": "public-key",
		"response": map[string]string{
			"clientDataJSON":    b64.EncodeToString(cd),
			"authenticatorData": b64.EncodeToString(ad),
			"signature":         b64.EncodeToString(sig),
			"userHandle":        b64.EncodeToString(a.handle),
		},
	})
	return resp
}

func TestPasskeyRegisterAndSignIn(t *testing.T) {
	ctx := context.Background()
	const uid = 1

	// A wrong password stops registration (the fixture's stored hash isn't bcrypt).
	if _, _, err := BeginPasskeyRegistration(ctx, uid, "nope", false, testRP); !errors.Is(err, ErrValidation) {
		t.Fatalf("wrong password err=%v, want validation", err)
	}

	auth := newSoftAuthenticator(t)
	opts, token, err := BeginPasskeyRegistration(ctx, uid, "", true, testRP)
	if err != nil {
		t.Fatalf("begin registration: %v", err)
	}
	// Another account can't finish someone else's ceremony.
	if _, err := FinishPasskeyRegistration(ctx, 2, token, "x", auth.create(t, opts, testRP.Origin), testRP); !errors.Is(err, ErrValidation) {
		t.Fatalf("other user finish err=%v", err)
	}
	// The ceremony was used up by that attempt: start again.
	opts, token, err = BeginPasskeyRegistration(ctx, uid, "", true, testRP)
	if err != nil {
		t.Fatal(err)
	}
	pk, err := FinishPasskeyRegistration(ctx, uid, token, "  My   laptop ", auth.create(t, opts, testRP.Origin), testRP)
	if err != nil {
		t.Fatalf("finish registration: %v", err)
	}
	if pk.Name != "My laptop" {
		t.Fatalf("name=%q", pk.Name)
	}

	// Sign in with it.
	lopts, ltoken, err := BeginPasskeyLogin(ctx, testRP)
	if err != nil {
		t.Fatalf("begin login: %v", err)
	}
	got, err := FinishPasskeyLogin(ctx, ltoken, auth.get(t, lopts, testRP.Origin), testRP)
	if err != nil || got != uid {
		t.Fatalf("finish login: user=%d err=%v", got, err)
	}
	// A token works once.
	if _, err := FinishPasskeyLogin(ctx, ltoken, auth.get(t, lopts, testRP.Origin), testRP); !errors.Is(err, ErrPasskeyFailed) {
		t.Fatalf("replayed token err=%v", err)
	}
	// A response made for another origin is refused.
	lopts, ltoken, _ = BeginPasskeyLogin(ctx, testRP)
	if _, err := FinishPasskeyLogin(ctx, ltoken, auth.get(t, lopts, "https://evil.example"), testRP); !errors.Is(err, ErrPasskeyFailed) {
		t.Fatalf("wrong origin err=%v", err)
	}
	// An unknown key is refused.
	stranger := newSoftAuthenticator(t)
	stranger.handle = auth.handle
	lopts, ltoken, _ = BeginPasskeyLogin(ctx, testRP)
	if _, err := FinishPasskeyLogin(ctx, ltoken, stranger.get(t, lopts, testRP.Origin), testRP); !errors.Is(err, ErrPasskeyFailed) {
		t.Fatalf("unknown key err=%v", err)
	}

	keys, err := ListPasskeysForUser(ctx, uid)
	if err != nil || len(keys) == 0 || keys[len(keys)-1].LastUsedAt == nil {
		t.Fatalf("list: %+v err=%v", keys, err)
	}
	if err := RenamePasskeyForUser(ctx, 2, pk.ID, "mine now"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("rename other's err=%v", err)
	}
	if err := DeletePasskeyForUser(ctx, uid, pk.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	lopts, ltoken, _ = BeginPasskeyLogin(ctx, testRP)
	if _, err := FinishPasskeyLogin(ctx, ltoken, auth.get(t, lopts, testRP.Origin), testRP); !errors.Is(err, ErrPasskeyFailed) {
		t.Fatalf("deleted key err=%v", err)
	}
	if n, _ := storage.CountPasskeys(uid); n != len(keys)-1 {
		t.Fatalf("count=%d", n)
	}
}
