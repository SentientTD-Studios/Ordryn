package server

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// Public /api/v2 routes that are authenticated by something other than a
// session cookie or user API key (or that are intentionally anonymous).
var publicAPIAllowlist = map[string]string{
	"/health":                  "liveness",
	"/site":                    "public site config",
	"/auth/register":           "authPublic",
	"/auth/login":              "authPublic",
	"/auth/mfa/verify":         "authPublic",
	"/auth/logout":             "clears cookie",
	"/auth/username-available": "authPublic",
	"/auth/forgot-password":    "rate-limited",
	"/join-requests":           "rate-limited",
	"/auth/reset-password":     "token in body",
	"/auth/github/callback":    "oauth redirect",
	"/webhooks/github":         "github HMAC",
	"/webhooks/inbound":        "inbound secret/HMAC + rate limit",
	"/ext/callback":            "extension callback bearer + rate limit",
	"/share-links/view/":       "share token in path",
	"/auth/device/code":        "devicePublic",
	"/auth/device/token":       "devicePublic",
	"/auth/device/status":      "device user_code poll",
}

var protectedWrappers = []string{
	"APIChain",
	"AuthSessionChain",
	"AuthMeChain",
	"AdminAPIChain",
	"InviteAPIChain",
	"v1(",
	"authPublic(",
	"devicePublic(",
	"RateLimitMiddleware",
	"RequireAPIEnabled",
}

func TestAPIRoutesUseAuthOrAllowlist(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(file), "server.go"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)
	if !strings.Contains(src, `handleBoth("/api/v2"+suffix, fn)`) || !strings.Contains(src, `handleBoth("/api/v1"+suffix, fn)`) {
		t.Fatal("handleAPI must register both /api/v2 and the /api/v1 alias")
	}
	re := regexp.MustCompile(`handleAPI\("(/[^"]+)",\s*(.+)\)`)
	matches := re.FindAllStringSubmatch(src, -1)
	if len(matches) < 40 {
		t.Fatalf("expected many API routes, got %d", len(matches))
	}
	seen := map[string]bool{}
	for _, m := range matches {
		path, handler := m[1], strings.TrimSpace(m[2])
		seen[path] = true
		if _, pub := publicAPIAllowlist[path]; pub {
			continue
		}
		ok := false
		for _, wrap := range protectedWrappers {
			if strings.Contains(handler, wrap) {
				ok = true
				break
			}
		}
		if !ok {
			t.Errorf("/api/v2%s is not behind session/API-key auth and is not on the public allowlist: %s", path, handler)
		}
	}
	for path := range publicAPIAllowlist {
		if !seen[path] {
			t.Errorf("allowlist path /api/v2%s is not registered in server.go", path)
		}
	}
}
