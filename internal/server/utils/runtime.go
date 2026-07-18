package utils

import (
	"os"
	"strings"

	"GoTodo/internal/config"
)

// ModeFull serves the Vue SPA plus API routes.
const ModeFull = "full"

// ModeAPI serves JSON API routes only (no templates or static UI).
const ModeAPI = "api"

// activeMode is set once at process start via SetRuntimeMode.
var activeMode = ModeFull

// BasePath is the HTTP path prefix (e.g. /gotodo) from config.
var BasePath string

// GetBasePath returns the configured path prefix.
func GetBasePath() string {
	return BasePath
}

// SetRuntimeMode records the process mode for health/diagnostics.
func SetRuntimeMode(mode string) {
	activeMode = normalizeMode(mode)
}

// GetRuntimeMode returns the mode selected at startup.
func GetRuntimeMode() string {
	return activeMode
}

// ResolveMode returns the runtime mode from --mode / GOTODO_MODE (default: full).
func ResolveMode(args []string) string {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "--mode=") {
			return normalizeMode(strings.TrimPrefix(a, "--mode="))
		}
		if a == "--mode" && i+1 < len(args) {
			return normalizeMode(args[i+1])
		}
	}
	if v := os.Getenv("GOTODO_MODE"); v != "" {
		return normalizeMode(v)
	}
	return ModeFull
}

func normalizeMode(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case ModeAPI:
		return ModeAPI
	default:
		return ModeFull
	}
}

// LoadRuntimeConfig loads config.json/env and applies BasePath without parsing templates.
func LoadRuntimeConfig() {
	config.Load()
	ApplyBasePathFromConfig()
}

// ApplyBasePathFromConfig sets utils.BasePath from config.Cfg.
func ApplyBasePathFromConfig() {
	BasePath = config.Cfg.BasePath
	if BasePath == "" {
		BasePath = "/"
	}
	BasePath = strings.TrimSuffix(BasePath, "/")
	if BasePath == "" {
		BasePath = "/"
	}
}
