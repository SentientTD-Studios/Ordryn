package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	BasePath        string `json:"basePath"`
	UseHTTPS        bool   `json:"useHttps"`
	AssetVersion    string `json:"assetVersion,omitempty"`
	FromEmail       string `json:"from_email,omitempty"`
	ShowChangelog   bool   `json:"showChangelog,omitempty"`
	SiteName        string `json:"siteName,omitempty"`
	SiteVersion     string `json:"siteVersion,omitempty"`
	DefaultTimezone string `json:"defaultTimezone,omitempty"`
}

var Cfg Config

const (
	defaultAssetVersion    = "20251130"
	defaultFromEmail       = "no-reply@example.com"
	defaultSiteName        = "GoTodo"
	defaultSiteVersion     = "v0.0.0"
	defaultDefaultTimezone = "America/New_York"
	minSessionKeyLen       = 32
)

// envLoaded tracks whether LoadDotEnv has succeeded in this process.
var envLoaded bool

// requireDotEnvOverride, when non-nil, forces whether `.env` must exist (tests only).
var requireDotEnvOverride *bool

// RunningGoTest reports whether the current process is a `go test` binary.
func RunningGoTest() bool {
	if strings.HasSuffix(os.Args[0], ".test") {
		return true
	}
	for _, arg := range os.Args {
		if strings.HasPrefix(arg, "-test.") {
			return true
		}
	}
	return false
}

func mustHaveDotEnvFile() bool {
	if requireDotEnvOverride != nil {
		return *requireDotEnvOverride
	}
	return !RunningGoTest()
}

// SetRequireDotEnvForTest controls whether LoadDotEnv requires a `.env` file.
// Pass nil to restore default (require outside go test).
func SetRequireDotEnvForTest(require *bool) {
	requireDotEnvOverride = require
}

// LoadDotEnv loads `.env` from the process working directory.
// Outside tests, a missing or unreadable `.env` is an error.
func LoadDotEnv() error {
	if !mustHaveDotEnvFile() {
		_ = godotenv.Load()
		envLoaded = true
		return nil
	}
	if _, err := os.Stat(".env"); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf(".env file is required in the working directory (copy .env.example to .env)")
		}
		return fmt.Errorf("cannot read .env: %w", err)
	}
	if err := godotenv.Load(); err != nil {
		return fmt.Errorf("failed to load .env: %w", err)
	}
	envLoaded = true
	return nil
}

// skipRequiredOverride, when non-nil, forces whether ValidateRequired is soft (tests).
var skipRequiredOverride *bool

func skipFullRequiredCheck() bool {
	if skipRequiredOverride != nil {
		return *skipRequiredOverride
	}
	return RunningGoTest()
}

// SetSkipRequiredForTest controls soft validation (tests only). Pass nil to restore default.
func SetSkipRequiredForTest(skip *bool) {
	skipRequiredOverride = skip
}

// ValidateRequired checks README-minimum env vars after .env is loaded.
// Under tests, DB/Redis may be absent; SESSION_KEY may use the test fallback.
func ValidateRequired() error {
	if skipFullRequiredCheck() {
		key := strings.TrimSpace(os.Getenv("SESSION_KEY"))
		if key != "" && len(key) < minSessionKeyLen {
			return fmt.Errorf("SESSION_KEY must be at least %d characters long", minSessionKeyLen)
		}
		return nil
	}

	var missing []string
	for _, key := range []string{"DB_HOST", "DB_PORT", "DB_USER", "DB_PASSWORD", "DB_NAME"} {
		if strings.TrimSpace(os.Getenv(key)) == "" {
			missing = append(missing, key)
		}
	}
	sessionKey := strings.TrimSpace(os.Getenv("SESSION_KEY"))
	if sessionKey == "" {
		missing = append(missing, "SESSION_KEY")
	}
	redisURL := strings.TrimSpace(os.Getenv("REDIS_URL"))
	redisAddr := strings.TrimSpace(os.Getenv("REDIS_ADDR"))
	if redisURL == "" && redisAddr == "" {
		missing = append(missing, "REDIS_URL (or REDIS_ADDR)")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required environment variables: %s", strings.Join(missing, ", "))
	}
	if len(sessionKey) < minSessionKeyLen {
		return fmt.Errorf("SESSION_KEY must be at least %d characters long", minSessionKeyLen)
	}
	return nil
}

// envIsSet reports whether a non-empty value is present in the environment.
func envIsSet(key string) bool {
	return strings.TrimSpace(os.Getenv(key)) != ""
}

func applyEnvToCfg() {
	if envIsSet("BASE_PATH") {
		Cfg.BasePath = strings.TrimSpace(os.Getenv("BASE_PATH"))
	}
	if Cfg.BasePath == "" {
		Cfg.BasePath = "/"
	}

	if envIsSet("USE_HTTPS") {
		Cfg.UseHTTPS = strings.EqualFold(strings.TrimSpace(os.Getenv("USE_HTTPS")), "true")
	}

	if envIsSet("ASSET_VERSION") {
		Cfg.AssetVersion = strings.TrimSpace(os.Getenv("ASSET_VERSION"))
	}
	if Cfg.AssetVersion == "" {
		Cfg.AssetVersion = defaultAssetVersion
	}

	if envIsSet("FROM_EMAIL") {
		Cfg.FromEmail = strings.TrimSpace(os.Getenv("FROM_EMAIL"))
	}
	if Cfg.FromEmail == "" {
		Cfg.FromEmail = defaultFromEmail
	}

	if envIsSet("SHOW_CHANGELOG") {
		Cfg.ShowChangelog = !strings.EqualFold(strings.TrimSpace(os.Getenv("SHOW_CHANGELOG")), "false")
	} else if !jsonHadShowChangelog {
		Cfg.ShowChangelog = true
	}

	if envIsSet("SITE_NAME") {
		Cfg.SiteName = strings.TrimSpace(os.Getenv("SITE_NAME"))
	}
	if Cfg.SiteName == "" {
		Cfg.SiteName = defaultSiteName
	}

	if envIsSet("SITE_VERSION") {
		Cfg.SiteVersion = strings.TrimSpace(os.Getenv("SITE_VERSION"))
	}
	if Cfg.SiteVersion == "" {
		Cfg.SiteVersion = defaultSiteVersion
	}

	if envIsSet("DEFAULT_TIMEZONE") {
		Cfg.DefaultTimezone = strings.TrimSpace(os.Getenv("DEFAULT_TIMEZONE"))
	}
	if Cfg.DefaultTimezone == "" {
		Cfg.DefaultTimezone = defaultDefaultTimezone
	}
}

// jsonHadShowChangelog is set when config.json explicitly includes showChangelog.
var jsonHadShowChangelog bool

func mergeOptionalJSON() error {
	jsonHadShowChangelog = false
	data, err := os.ReadFile("config/config.json")
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("config/config.json: %w", err)
	}
	var fileCfg Config
	if err := json.Unmarshal(data, &fileCfg); err != nil {
		return fmt.Errorf("config/config.json: %w", err)
	}
	jsonHadShowChangelog = strings.Contains(string(data), `"showChangelog"`)

	// Env wins: only fill from JSON when the corresponding env var is unset.
	if !envIsSet("BASE_PATH") && fileCfg.BasePath != "" {
		Cfg.BasePath = fileCfg.BasePath
	}
	if !envIsSet("USE_HTTPS") {
		Cfg.UseHTTPS = fileCfg.UseHTTPS
	}
	if !envIsSet("ASSET_VERSION") && fileCfg.AssetVersion != "" {
		Cfg.AssetVersion = fileCfg.AssetVersion
	}
	if !envIsSet("FROM_EMAIL") && fileCfg.FromEmail != "" {
		Cfg.FromEmail = fileCfg.FromEmail
	}
	if !envIsSet("SHOW_CHANGELOG") && jsonHadShowChangelog {
		Cfg.ShowChangelog = fileCfg.ShowChangelog
	}
	if !envIsSet("SITE_NAME") && fileCfg.SiteName != "" {
		Cfg.SiteName = fileCfg.SiteName
	}
	if !envIsSet("SITE_VERSION") && fileCfg.SiteVersion != "" {
		Cfg.SiteVersion = fileCfg.SiteVersion
	}
	if !envIsSet("DEFAULT_TIMEZONE") && fileCfg.DefaultTimezone != "" {
		Cfg.DefaultTimezone = fileCfg.DefaultTimezone
	}
	return nil
}

// Load loads `.env` (required outside tests), validates required vars, applies
// env into Cfg, then optionally merges config.json for unset site fields.
func Load() error {
	if err := LoadDotEnv(); err != nil {
		return err
	}
	if err := ValidateRequired(); err != nil {
		return err
	}
	Cfg = Config{}
	jsonHadShowChangelog = false
	if err := mergeOptionalJSON(); err != nil {
		return err
	}
	applyEnvToCfg()
	return nil
}

// MustLoad calls Load and exits the process on failure.
func MustLoad() {
	if err := Load(); err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}
}

// SessionKey returns SESSION_KEY from the environment, or the test key under go test.
func SessionKey() (string, error) {
	key := strings.TrimSpace(os.Getenv("SESSION_KEY"))
	if key == "" {
		if RunningGoTest() {
			return "test-session-key-for-unit-tests-32chars!!", nil
		}
		return "", errors.New("SESSION_KEY environment variable is not set")
	}
	if len(key) < minSessionKeyLen {
		return "", fmt.Errorf("SESSION_KEY must be at least %d characters long", minSessionKeyLen)
	}
	return key, nil
}

// EnvLoaded reports whether LoadDotEnv has completed successfully.
func EnvLoaded() bool {
	return envLoaded
}
