package utils

import (
	"os"
	"strings"
)

// UIHtmx serves the legacy HTMX UI at /.
const UIHtmx = "htmx"

// UISPA prefers the Vue SPA (served under /app, and at / when selected).
const UISPA = "spa"

var activeUI = UISPA

// SetRuntimeUI records the selected web UI for diagnostics/routing.
func SetRuntimeUI(ui string) {
	activeUI = normalizeUI(ui)
}

// GetRuntimeUI returns the UI selected at startup.
func GetRuntimeUI() string {
	return activeUI
}

// ResolveUI returns GOTODO_UI / --ui (default: spa after Phase C cutover).
func ResolveUI(args []string) string {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "--ui=") {
			return normalizeUI(strings.TrimPrefix(a, "--ui="))
		}
		if a == "--ui" && i+1 < len(args) {
			return normalizeUI(args[i+1])
		}
	}
	if v := os.Getenv("GOTODO_UI"); v != "" {
		return normalizeUI(v)
	}
	return UISPA
}

func normalizeUI(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case UIHtmx:
		return UIHtmx
	default:
		return UISPA
	}
}
