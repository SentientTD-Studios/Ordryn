package utils

import (
	"net/http/httptest"
	"slices"
	"testing"
)

// Every way of signing in has to keep working while the site is read-only.
func TestMaintenanceOpenPathsCoverSignIn(t *testing.T) {
	for _, p := range []string{
		"/api/v2/auth/login", "/api/v2/auth/logout", "/api/v2/auth/mfa/verify",
		"/api/v2/auth/passkey/begin", "/api/v2/auth/passkey/finish", "/api/v1/auth/passkey/finish/",
	} {
		r := httptest.NewRequest("POST", p, nil)
		if !slices.Contains(maintenanceOpenPaths, apiPathSuffixForMaintenance(r)) {
			t.Errorf("%s is blocked during maintenance", p)
		}
	}
}
