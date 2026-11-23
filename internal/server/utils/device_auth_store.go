package utils

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"strings"
	"time"

	"GoTodo/internal/storage"
)

const (
	DeviceAuthTTLSeconds = 600
	DeviceAuthInterval   = 5
	deviceAuthCodePrefix = "device_auth:dc:"
	deviceAuthUserPrefix = "device_auth:uc:"
)

// DeviceAuthStatus tracks the lifecycle of a device authorization request.
type DeviceAuthStatus string

const (
	DeviceAuthPending  DeviceAuthStatus = "pending"
	DeviceAuthApproved DeviceAuthStatus = "approved"
	DeviceAuthDenied   DeviceAuthStatus = "denied"
)

// DeviceAuthRecord is kept in the state store while a device authorization is active.
type DeviceAuthRecord struct {
	DeviceCode  string           `json:"device_code"`
	UserCode    string           `json:"user_code"`
	ClientName  string           `json:"client_name"`
	RedirectURI string           `json:"redirect_uri,omitempty"`
	Status      DeviceAuthStatus `json:"status"`
	APIKey      string           `json:"api_key,omitempty"`
	KeyPrefix   string           `json:"key_prefix,omitempty"`
	KeyName     string           `json:"key_name,omitempty"`
	UserID      int              `json:"user_id,omitempty"`
	// Scopes is empty for a full-access personal key, or the identity scope
	// (see NormalizeDeviceScopes) for a sign-in-only key.
	Scopes    []string   `json:"scopes,omitempty"`
	KeyID     int        `json:"key_id,omitempty"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	// ChatServer is the registered chat server that started a sign-in-only
	// request (see storage.ChatServer); nil for every other request.
	ChatServer *DeviceChatServer `json:"chat_server,omitempty"`
}

// DeviceChatServer identifies the registered chat server behind a request.
type DeviceChatServer struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

// IsIdentity reports whether the request asked for a sign-in-only key.
func (r *DeviceAuthRecord) IsIdentity() bool {
	return len(r.Scopes) == 1 && r.Scopes[0] == storage.APIScopeProfileRead
}

// NormalizeDeviceScopes validates the scopes a device asks for. No scopes means
// a full-access personal key (the original behaviour); ["profile:read"] means
// an identity key. Anything else is rejected rather than silently widened.
func NormalizeDeviceScopes(scopes []string) ([]string, error) {
	var out []string
	for _, s := range scopes {
		s = strings.TrimSpace(strings.ToLower(s))
		if s == "" {
			continue
		}
		if s != storage.APIScopeProfileRead {
			return nil, fmt.Errorf("unsupported scope %q", s)
		}
		if len(out) == 0 {
			out = append(out, s)
		}
	}
	return out, nil
}

// Allowed device deep-link scheme/host for post-approval return to native apps.
const (
	deviceRedirectScheme = "ordryn"
	deviceRedirectHost   = "auth-complete"
	maxDeviceRedirectLen = 512
)

const deviceAuthUserCodeAlphabet = "BCDFGHJKMNPQRSTVWXYZ23456789"

// NormalizeClientName trims and defaults empty names to "Android app".
func NormalizeClientName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "Android app"
	}
	if len(name) > 80 {
		return name[:80]
	}
	return name
}

// NormalizeUserCode uppercases and strips separators for lookup.
func NormalizeUserCode(userCode string) string {
	userCode = strings.ToUpper(strings.TrimSpace(userCode))
	userCode = strings.ReplaceAll(userCode, "-", "")
	userCode = strings.ReplaceAll(userCode, " ", "")
	return userCode
}

// FormatUserCode formats an 8-character code as XXXX-XXXX.
func FormatUserCode(raw string) string {
	raw = NormalizeUserCode(raw)
	if len(raw) != 8 {
		return raw
	}
	return raw[:4] + "-" + raw[4:]
}

func generateDeviceCode() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", b), nil
}

func generateUserCode() (string, error) {
	var sb strings.Builder
	sb.Grow(8)
	max := big.NewInt(int64(len(deviceAuthUserCodeAlphabet)))
	for i := 0; i < 8; i++ {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		sb.WriteByte(deviceAuthUserCodeAlphabet[n.Int64()])
	}
	return sb.String(), nil
}

func deviceAuthCodeKey(deviceCode string) string {
	return deviceAuthCodePrefix + deviceCode
}

func deviceAuthUserKey(userCode string) string {
	return deviceAuthUserPrefix + NormalizeUserCode(userCode)
}

func deviceAuthTTL() time.Duration {
	return time.Duration(DeviceAuthTTLSeconds) * time.Second
}

func saveDeviceAuthRecord(ctx context.Context, record *DeviceAuthRecord) error {
	store := stateStore()
	if store == nil {
		return fmt.Errorf("state store unavailable")
	}
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	return store.SetMany(ctx, map[string][]byte{
		deviceAuthCodeKey(record.DeviceCode): data,
		deviceAuthUserKey(record.UserCode):   []byte(record.DeviceCode),
	}, deviceAuthTTL())
}

// loadDeviceAuthRaw returns the record and the exact bytes it was stored as,
// for updateDeviceAuthRecord's compare-and-set.
func loadDeviceAuthRaw(ctx context.Context, deviceCode string) (*DeviceAuthRecord, []byte, error) {
	store := stateStore()
	if store == nil {
		return nil, nil, fmt.Errorf("state store unavailable")
	}
	data, err := store.Get(ctx, deviceAuthCodeKey(deviceCode))
	if err != nil {
		return nil, nil, err
	}
	var record DeviceAuthRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, nil, err
	}
	return &record, data, nil
}

func loadDeviceAuthByDeviceCode(ctx context.Context, deviceCode string) (*DeviceAuthRecord, error) {
	record, _, err := loadDeviceAuthRaw(ctx, deviceCode)
	return record, err
}

func deviceCodeForUserCode(ctx context.Context, userCode string) (string, error) {
	store := stateStore()
	if store == nil {
		return "", fmt.Errorf("state store unavailable")
	}
	deviceCode, err := store.Get(ctx, deviceAuthUserKey(userCode))
	if err != nil {
		return "", err
	}
	return string(deviceCode), nil
}

func loadDeviceAuthByUserCode(ctx context.Context, userCode string) (*DeviceAuthRecord, error) {
	deviceCode, err := deviceCodeForUserCode(ctx, userCode)
	if err != nil {
		return nil, err
	}
	return loadDeviceAuthByDeviceCode(ctx, deviceCode)
}

var (
	// errDeviceAuthUnchanged tells updateDeviceAuthRecord to leave the record as is.
	errDeviceAuthUnchanged = errors.New("device auth unchanged")
	// errDeviceAuthConflict means another request changed the record first.
	errDeviceAuthConflict = errors.New("device auth changed concurrently")
)

// updateDeviceAuthRecord applies change to the stored record and writes it back
// only if nobody else changed it in between, so two approvals, or two polls
// collecting the API key, can't both succeed. change may return
// errDeviceAuthUnchanged to skip the write.
func updateDeviceAuthRecord(ctx context.Context, deviceCode string, change func(*DeviceAuthRecord) error) (*DeviceAuthRecord, error) {
	record, old, err := loadDeviceAuthRaw(ctx, deviceCode)
	if err != nil {
		return nil, err
	}
	if err := change(record); err != nil {
		if errors.Is(err, errDeviceAuthUnchanged) {
			return record, nil
		}
		return nil, err
	}
	data, err := json.Marshal(record)
	if err != nil {
		return nil, err
	}
	store := stateStore()
	ok, err := store.CompareAndSet(ctx, deviceAuthCodeKey(deviceCode), old, data, deviceAuthTTL())
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errDeviceAuthConflict
	}
	// Keep the user-code lookup alive as long as the record, as saves always have.
	_ = store.Set(ctx, deviceAuthUserKey(record.UserCode), []byte(deviceCode), deviceAuthTTL())
	return record, nil
}

// SafeDeviceRedirectURI validates an optional native-app deep link for post-approval
// browser return (e.g. ordryn://auth-complete). Empty input is allowed (no redirect).
// This is intentionally separate from SafeDeviceReturnTo (same-origin SPA paths only).
func SafeDeviceRedirectURI(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if len(raw) > maxDeviceRedirectLen {
		return "", fmt.Errorf("redirect_uri too long")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid redirect_uri")
	}
	if !strings.EqualFold(u.Scheme, deviceRedirectScheme) {
		return "", fmt.Errorf("unsupported redirect_uri scheme")
	}
	if u.User != nil {
		return "", fmt.Errorf("redirect_uri must not include userinfo")
	}
	if strings.ToLower(u.Host) != deviceRedirectHost {
		return "", fmt.Errorf("unsupported redirect_uri host")
	}
	path := u.EscapedPath()
	if path != "" && path != "/" {
		return "", fmt.Errorf("unsupported redirect_uri path")
	}
	// Canonical form; drop query/fragment from client input (server adds status later).
	return (&url.URL{
		Scheme: deviceRedirectScheme,
		Host:   deviceRedirectHost,
	}).String(), nil
}

// DeviceDecisionRedirectURI returns the deep link to open after approve/deny.
func DeviceDecisionRedirectURI(base string, approved bool) string {
	base = strings.TrimSpace(base)
	if base == "" {
		return ""
	}
	u, err := url.Parse(base)
	if err != nil || u.Scheme == "" {
		return ""
	}
	q := u.Query()
	if approved {
		q.Set("status", "approved")
		q.Del("error")
	} else {
		q.Set("error", "access_denied")
		q.Del("status")
	}
	u.RawQuery = q.Encode()
	u.Fragment = ""
	return u.String()
}

// CreateDeviceAuthRequest stores a new pending device authorization request.
// chat is the authenticated chat server for a sign-in-only request, or nil.
func CreateDeviceAuthRequest(ctx context.Context, clientName, redirectURI string, scopes []string, chat *DeviceChatServer) (*DeviceAuthRecord, error) {
	safeRedirect, err := SafeDeviceRedirectURI(redirectURI)
	if err != nil {
		return nil, err
	}
	scopes, err = NormalizeDeviceScopes(scopes)
	if err != nil {
		return nil, err
	}
	deviceCode, err := generateDeviceCode()
	if err != nil {
		return nil, err
	}
	rawUserCode, err := generateUserCode()
	if err != nil {
		return nil, err
	}
	record := &DeviceAuthRecord{
		DeviceCode:  deviceCode,
		UserCode:    FormatUserCode(rawUserCode),
		ClientName:  NormalizeClientName(clientName),
		RedirectURI: safeRedirect,
		Status:      DeviceAuthPending,
		Scopes:      scopes,
		ChatServer:  chat,
	}
	if chat != nil {
		record.ClientName = NormalizeClientName(chat.Name)
	}
	if err := saveDeviceAuthRecord(ctx, record); err != nil {
		return nil, err
	}
	return record, nil
}

// GetDeviceAuthByDeviceCode loads a device authorization by device code.
func GetDeviceAuthByDeviceCode(ctx context.Context, deviceCode string) (*DeviceAuthRecord, error) {
	return loadDeviceAuthByDeviceCode(ctx, deviceCode)
}

// GetDeviceAuthByUserCode loads a device authorization by user-facing code.
func GetDeviceAuthByUserCode(ctx context.Context, userCode string) (*DeviceAuthRecord, error) {
	return loadDeviceAuthByUserCode(ctx, userCode)
}

// ApproveDeviceAuth marks a request approved and stores the one-time API key payload.
func ApproveDeviceAuth(ctx context.Context, userCode string, userID int, apiKey string, key *storage.APIKey) error {
	deviceCode, err := deviceCodeForUserCode(ctx, userCode)
	if err != nil {
		return err
	}
	_, err = updateDeviceAuthRecord(ctx, deviceCode, func(record *DeviceAuthRecord) error {
		if record.Status != DeviceAuthPending {
			return fmt.Errorf("device auth not pending")
		}
		record.Status = DeviceAuthApproved
		record.UserID = userID
		record.APIKey = apiKey
		record.KeyPrefix = key.KeyPrefix
		record.KeyName = key.Name
		record.KeyID = key.ID
		record.ExpiresAt = key.ExpiresAt
		return nil
	})
	return err
}

// DenyDeviceAuth marks a request denied.
func DenyDeviceAuth(ctx context.Context, userCode string) error {
	deviceCode, err := deviceCodeForUserCode(ctx, userCode)
	if err != nil {
		return err
	}
	_, err = updateDeviceAuthRecord(ctx, deviceCode, func(record *DeviceAuthRecord) error {
		if record.Status != DeviceAuthPending {
			return fmt.Errorf("device auth not pending")
		}
		record.Status = DeviceAuthDenied
		return nil
	})
	return err
}

// ConsumeApprovedDeviceToken returns the approved API key once and clears it
// from the state store. Of two concurrent polls, only one gets the key.
func ConsumeApprovedDeviceToken(ctx context.Context, deviceCode string) (record *DeviceAuthRecord, plaintext string, err error) {
	record, err = updateDeviceAuthRecord(ctx, deviceCode, func(r *DeviceAuthRecord) error {
		if r.Status != DeviceAuthApproved || r.APIKey == "" {
			return errDeviceAuthUnchanged
		}
		plaintext = r.APIKey
		r.APIKey = ""
		return nil
	})
	if errors.Is(err, errDeviceAuthConflict) {
		// Another poll collected the key first; answer as a repeat poll would.
		record, err = loadDeviceAuthByDeviceCode(ctx, deviceCode)
		plaintext = ""
	}
	if err != nil {
		return nil, "", err
	}
	return record, plaintext, nil
}

// SafeDeviceReturnTo validates post-login redirects for the device auth page.
func SafeDeviceReturnTo(returnTo string) string {
	returnTo = strings.TrimSpace(returnTo)
	if returnTo == "" {
		return ""
	}
	if strings.Contains(returnTo, "://") || strings.HasPrefix(returnTo, "//") {
		return ""
	}
	if !strings.HasPrefix(returnTo, "/") {
		return ""
	}

	base := PublicPathPrefix()
	rel := returnTo
	if base != "" && strings.HasPrefix(returnTo, base+"/") {
		rel = strings.TrimPrefix(returnTo, base)
	} else if base != "" && returnTo == base {
		rel = "/"
	}
	rel = normalizeDeviceAuthPath(rel)
	if !isDeviceAuthReturnPath(rel) {
		return ""
	}
	if base == "" {
		return rel
	}
	return base + rel
}

func normalizeDeviceAuthPath(path string) string {
	if strings.HasPrefix(path, "/app/auth/device") {
		return "/auth/device" + strings.TrimPrefix(path, "/app/auth/device")
	}
	return path
}

func isDeviceAuthReturnPath(path string) bool {
	return path == "/auth/device" || strings.HasPrefix(path, "/auth/device?") || strings.HasPrefix(path, "/auth/device/")
}
