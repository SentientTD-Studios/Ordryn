package domain

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"golang.org/x/crypto/bcrypt"

	"GoTodo/internal/storage"
)

// passkeyCeremonyTTL is how long a started registration or sign-in stays valid.
const passkeyCeremonyTTL = 5 * time.Minute

const (
	ceremonyRegister = "register"
	ceremonyLogin    = "login"
)

// ErrPasskeyFailed is returned when a passkey sign-in can't be verified. It is
// deliberately vague: the client learns nothing about which check failed.
var ErrPasskeyFailed = errors.New("passkey sign-in failed")

// PasskeyRP identifies this site to authenticators: the relying party ID (the
// host), the origin browsers must report, and the name they show.
type PasskeyRP struct {
	ID          string
	Origin      string
	DisplayName string
}

func (rp PasskeyRP) webauthn() (*webauthn.WebAuthn, error) {
	if rp.ID == "" || rp.Origin == "" {
		return nil, fmt.Errorf("%w: passkeys need the site's address", ErrValidation)
	}
	name := rp.DisplayName
	if strings.TrimSpace(name) == "" {
		name = "GoTodo"
	}
	return webauthn.New(&webauthn.Config{
		RPID:          rp.ID,
		RPDisplayName: name,
		RPOrigins:     []string{rp.Origin},
	})
}

// passkeyUser adapts an account to the webauthn.User interface.
type passkeyUser struct {
	id      int
	handle  []byte
	name    string
	display string
	creds   []webauthn.Credential
}

func (u *passkeyUser) WebAuthnID() []byte                         { return u.handle }
func (u *passkeyUser) WebAuthnName() string                       { return u.name }
func (u *passkeyUser) WebAuthnDisplayName() string                { return u.display }
func (u *passkeyUser) WebAuthnCredentials() []webauthn.Credential { return u.creds }

func loadPasskeyUser(userID int) (*passkeyUser, error) {
	handle, err := storage.WebAuthnHandle(userID)
	if err != nil {
		return nil, err
	}
	name, email := storage.UserNameAndEmail(userID)
	u := &passkeyUser{id: userID, handle: handle, name: email, display: email}
	if strings.TrimSpace(name) != "" {
		u.display = name
	}
	keys, err := storage.ListPasskeys(userID)
	if err != nil {
		return nil, err
	}
	for _, k := range keys {
		var c webauthn.Credential
		if err := json.Unmarshal(k.Credential, &c); err == nil {
			u.creds = append(u.creds, c)
		}
	}
	return u, nil
}

func newCeremonyToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func saveCeremony(kind string, userID int, session *webauthn.SessionData) (string, error) {
	token, err := newCeremonyToken()
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(session)
	if err != nil {
		return "", err
	}
	if err := storage.SaveWebAuthnCeremony(token, kind, userID, raw, time.Now().Add(passkeyCeremonyTTL)); err != nil {
		return "", err
	}
	return token, nil
}

func takeCeremony(token, kind string) (int, *webauthn.SessionData, error) {
	userID, raw, err := storage.TakeWebAuthnCeremony(strings.TrimSpace(token), kind)
	if err != nil {
		return 0, nil, err
	}
	var s webauthn.SessionData
	if err := json.Unmarshal(raw, &s); err != nil {
		return 0, nil, err
	}
	return userID, &s, nil
}

// confirmPassword checks the account's current password before adding a
// passkey, so a stolen session can't quietly add a way back in. Sessions that
// signed in through SSO skip it: those accounts may have no known password.
func confirmPassword(userID int, password string, viaSSO bool) error {
	if viaSSO {
		return nil
	}
	_, email := storage.UserNameAndEmail(userID)
	hash, _, _, err := storage.GetAuthCredentials(email)
	if err != nil || hash == "" {
		SpendPasswordCheckTime(password)
		return fmt.Errorf("%w: enter your current password", ErrValidation)
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return fmt.Errorf("%w: that password isn't right", ErrValidation)
	}
	return nil
}

// BeginPasskeyRegistration starts adding a passkey. It returns the options to
// pass to navigator.credentials.create and a token for the finish call.
func BeginPasskeyRegistration(ctx context.Context, userID int, password string, viaSSO bool, rp PasskeyRP) (json.RawMessage, string, error) {
	_ = ctx
	if storage.IsAgentUser(userID) || storage.IsSystemUser(userID) {
		return nil, "", ErrForbidden
	}
	if err := confirmPassword(userID, password, viaSSO); err != nil {
		return nil, "", err
	}
	if n, err := storage.CountPasskeys(userID); err != nil {
		return nil, "", err
	} else if n >= storage.MaxPasskeysPerUser {
		return nil, "", fmt.Errorf("%w: you can have at most %d passkeys", ErrValidation, storage.MaxPasskeysPerUser)
	}
	w, err := rp.webauthn()
	if err != nil {
		return nil, "", err
	}
	u, err := loadPasskeyUser(userID)
	if err != nil {
		return nil, "", err
	}
	required := true
	creation, session, err := w.BeginRegistration(u,
		webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
			ResidentKey:        protocol.ResidentKeyRequirementRequired,
			RequireResidentKey: &required,
			UserVerification:   protocol.VerificationRequired,
		}),
		webauthn.WithExclusions(webauthn.Credentials(u.creds).CredentialDescriptors()),
		webauthn.WithConveyancePreference(protocol.PreferNoAttestation),
	)
	if err != nil {
		return nil, "", err
	}
	token, err := saveCeremony(ceremonyRegister, userID, session)
	if err != nil {
		return nil, "", err
	}
	opts, err := json.Marshal(creation)
	if err != nil {
		return nil, "", err
	}
	return opts, token, nil
}

// FinishPasskeyRegistration verifies the authenticator's response and stores the passkey.
func FinishPasskeyRegistration(ctx context.Context, userID int, token, name string, response []byte, rp PasskeyRP) (*storage.Passkey, error) {
	_ = ctx
	ownerID, session, err := takeCeremony(token, ceremonyRegister)
	if err != nil || ownerID != userID {
		return nil, fmt.Errorf("%w: that passkey setup expired; start again", ErrValidation)
	}
	w, err := rp.webauthn()
	if err != nil {
		return nil, err
	}
	parsed, err := protocol.ParseCredentialCreationResponseBytes(response)
	if err != nil {
		return nil, fmt.Errorf("%w: the passkey response couldn't be read", ErrValidation)
	}
	u, err := loadPasskeyUser(userID)
	if err != nil {
		return nil, err
	}
	cred, err := w.CreateCredential(u, *session, parsed)
	if err != nil {
		return nil, fmt.Errorf("%w: the passkey couldn't be verified", ErrValidation)
	}
	raw, err := json.Marshal(cred)
	if err != nil {
		return nil, err
	}
	name = cleanPasskeyName(name)
	pk, err := storage.AddPasskey(userID, cred.ID, raw, name)
	if err != nil {
		return nil, fmt.Errorf("%w: that passkey is already registered", ErrValidation)
	}
	_ = storage.LogAdminEvent(userID, "passkey_added", "user", int64(userID), storage.UserAuditLabel(userID), map[string]interface{}{"name": name})
	return pk, nil
}

func cleanPasskeyName(name string) string {
	name = strings.Join(strings.Fields(name), " ")
	if name == "" {
		name = "Passkey"
	}
	if r := []rune(name); len(r) > 60 {
		name = string(r[:60])
	}
	return name
}

// BeginPasskeyLogin starts a passkey sign-in where the authenticator picks the
// account (no email typed). It returns the options for navigator.credentials.get.
func BeginPasskeyLogin(ctx context.Context, rp PasskeyRP) (json.RawMessage, string, error) {
	_ = ctx
	w, err := rp.webauthn()
	if err != nil {
		return nil, "", err
	}
	assertion, session, err := w.BeginDiscoverableLogin(webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		return nil, "", err
	}
	token, err := saveCeremony(ceremonyLogin, 0, session)
	if err != nil {
		return nil, "", err
	}
	opts, err := json.Marshal(assertion)
	if err != nil {
		return nil, "", err
	}
	return opts, token, nil
}

// FinishPasskeyLogin verifies a passkey sign-in and returns the account. The
// caller still applies the account checks (deactivated, SSO-only, unverified email).
func FinishPasskeyLogin(ctx context.Context, token string, response []byte, rp PasskeyRP) (int, error) {
	_ = ctx
	_, session, err := takeCeremony(token, ceremonyLogin)
	if err != nil {
		return 0, ErrPasskeyFailed
	}
	w, err := rp.webauthn()
	if err != nil {
		return 0, err
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(response)
	if err != nil {
		return 0, ErrPasskeyFailed
	}
	var found *passkeyUser
	handler := func(rawID, userHandle []byte) (webauthn.User, error) {
		id, err := storage.UserIDForWebAuthnHandle(userHandle)
		if err != nil {
			return nil, err
		}
		if storage.IsAgentUser(id) || storage.IsSystemUser(id) {
			return nil, ErrPasskeyFailed
		}
		u, err := loadPasskeyUser(id)
		if err != nil {
			return nil, err
		}
		found = u
		return u, nil
	}
	_, cred, err := w.ValidatePasskeyLogin(handler, *session, parsed)
	if err != nil || found == nil || cred == nil {
		return 0, ErrPasskeyFailed
	}
	// A sign count that went backwards suggests a cloned authenticator.
	if cred.Authenticator.CloneWarning {
		return 0, ErrPasskeyFailed
	}
	if raw, err := json.Marshal(cred); err == nil {
		_ = storage.UpdatePasskeyAfterLogin(found.id, cred.ID, raw)
	}
	return found.id, nil
}

// ListPasskeysForUser returns the caller's passkeys.
func ListPasskeysForUser(ctx context.Context, userID int) ([]storage.Passkey, error) {
	_ = ctx
	return storage.ListPasskeys(userID)
}

// RenamePasskeyForUser relabels one of the caller's passkeys.
func RenamePasskeyForUser(ctx context.Context, userID, id int, name string) error {
	_ = ctx
	if err := storage.RenamePasskey(userID, id, cleanPasskeyName(name)); err != nil {
		if errors.Is(err, storage.ErrPasskeyNotFound) {
			return ErrNotFound
		}
		return err
	}
	return nil
}

// DeletePasskeyForUser removes one of the caller's passkeys.
func DeletePasskeyForUser(ctx context.Context, userID, id int) error {
	_ = ctx
	if err := storage.DeletePasskey(userID, id); err != nil {
		if errors.Is(err, storage.ErrPasskeyNotFound) {
			return ErrNotFound
		}
		return err
	}
	_ = storage.LogAdminEvent(userID, "passkey_removed", "user", int64(userID), storage.UserAuditLabel(userID), map[string]interface{}{"id": id})
	return nil
}
