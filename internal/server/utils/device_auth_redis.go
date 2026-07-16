package utils

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"time"
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

// DeviceAuthRecord is stored in Redis while a device authorization is active.
type DeviceAuthRecord struct {
	DeviceCode string           `json:"device_code"`
	UserCode   string           `json:"user_code"`
	ClientName string           `json:"client_name"`
	Status     DeviceAuthStatus `json:"status"`
	APIKey     string           `json:"api_key,omitempty"`
	KeyPrefix  string           `json:"key_prefix,omitempty"`
	KeyName    string           `json:"key_name,omitempty"`
	UserID     int              `json:"user_id,omitempty"`
}

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

func saveDeviceAuthRecord(ctx context.Context, record *DeviceAuthRecord) error {
	if RedisClient == nil {
		return fmt.Errorf("redis unavailable")
	}
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	pipe := RedisClient.TxPipeline()
	pipe.Set(ctx, deviceAuthCodeKey(record.DeviceCode), data, time.Duration(DeviceAuthTTLSeconds)*time.Second)
	pipe.Set(ctx, deviceAuthUserKey(record.UserCode), record.DeviceCode, time.Duration(DeviceAuthTTLSeconds)*time.Second)
	_, err = pipe.Exec(ctx)
	return err
}

func loadDeviceAuthByDeviceCode(ctx context.Context, deviceCode string) (*DeviceAuthRecord, error) {
	if RedisClient == nil {
		return nil, fmt.Errorf("redis unavailable")
	}
	data, err := RedisClient.Get(ctx, deviceAuthCodeKey(deviceCode)).Bytes()
	if err != nil {
		return nil, err
	}
	var record DeviceAuthRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, err
	}
	return &record, nil
}

func loadDeviceAuthByUserCode(ctx context.Context, userCode string) (*DeviceAuthRecord, error) {
	if RedisClient == nil {
		return nil, fmt.Errorf("redis unavailable")
	}
	deviceCode, err := RedisClient.Get(ctx, deviceAuthUserKey(userCode)).Result()
	if err != nil {
		return nil, err
	}
	return loadDeviceAuthByDeviceCode(ctx, deviceCode)
}

// CreateDeviceAuthRequest stores a new pending device authorization request.
func CreateDeviceAuthRequest(ctx context.Context, clientName string) (*DeviceAuthRecord, error) {
	deviceCode, err := generateDeviceCode()
	if err != nil {
		return nil, err
	}
	rawUserCode, err := generateUserCode()
	if err != nil {
		return nil, err
	}
	record := &DeviceAuthRecord{
		DeviceCode: deviceCode,
		UserCode:   FormatUserCode(rawUserCode),
		ClientName: NormalizeClientName(clientName),
		Status:     DeviceAuthPending,
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
func ApproveDeviceAuth(ctx context.Context, userCode string, userID int, apiKey, keyPrefix, keyName string) error {
	record, err := loadDeviceAuthByUserCode(ctx, userCode)
	if err != nil {
		return err
	}
	if record.Status != DeviceAuthPending {
		return fmt.Errorf("device auth not pending")
	}
	record.Status = DeviceAuthApproved
	record.UserID = userID
	record.APIKey = apiKey
	record.KeyPrefix = keyPrefix
	record.KeyName = keyName
	return saveDeviceAuthRecord(ctx, record)
}

// DenyDeviceAuth marks a request denied.
func DenyDeviceAuth(ctx context.Context, userCode string) error {
	record, err := loadDeviceAuthByUserCode(ctx, userCode)
	if err != nil {
		return err
	}
	if record.Status != DeviceAuthPending {
		return fmt.Errorf("device auth not pending")
	}
	record.Status = DeviceAuthDenied
	return saveDeviceAuthRecord(ctx, record)
}

// ConsumeApprovedDeviceToken returns the approved API key once and clears it from Redis.
func ConsumeApprovedDeviceToken(ctx context.Context, deviceCode string) (record *DeviceAuthRecord, plaintext string, err error) {
	record, err = loadDeviceAuthByDeviceCode(ctx, deviceCode)
	if err != nil {
		return nil, "", err
	}
	if record.Status == DeviceAuthApproved && record.APIKey != "" {
		plaintext = record.APIKey
		record.APIKey = ""
		if err := saveDeviceAuthRecord(ctx, record); err != nil {
			return nil, "", err
		}
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

	base := strings.TrimSuffix(GetBasePath(), "/")
	if base == "" || base == "/" {
		if returnTo == "/auth/device" || strings.HasPrefix(returnTo, "/auth/device?") || strings.HasPrefix(returnTo, "/auth/device/") {
			return returnTo
		}
		return ""
	}

	if returnTo == base+"/auth/device" || strings.HasPrefix(returnTo, base+"/auth/device?") || strings.HasPrefix(returnTo, base+"/auth/device/") {
		return returnTo
	}
	if returnTo == "/auth/device" || strings.HasPrefix(returnTo, "/auth/device?") || strings.HasPrefix(returnTo, "/auth/device/") {
		return base + returnTo
	}
	return ""
}
