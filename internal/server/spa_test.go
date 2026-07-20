package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"GoTodo/internal/server/utils"
)

func TestDocumentationAPIV1Redirect(t *testing.T) {
	orig := utils.BasePath
	t.Cleanup(func() { utils.BasePath = orig })
	utils.BasePath = "/"

	req := httptest.NewRequest(http.MethodGet, "/documentation/api/v1", nil)
	rec := httptest.NewRecorder()
	documentationAPIV1Redirect(rec, req)
	if rec.Code != http.StatusTemporaryRedirect {
		t.Fatalf("status = %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/docs/api/v1" {
		t.Fatalf("Location = %q", loc)
	}
}

func TestDocumentationAPIV1RedirectWithBasePath(t *testing.T) {
	orig := utils.BasePath
	t.Cleanup(func() { utils.BasePath = orig })
	utils.BasePath = "/gotodo"

	req := httptest.NewRequest(http.MethodGet, "/gotodo/documentation/api/v1", nil)
	rec := httptest.NewRecorder()
	documentationAPIV1Redirect(rec, req)
	if loc := rec.Header().Get("Location"); loc != "/gotodo/docs/api/v1" {
		t.Fatalf("Location = %q", loc)
	}
}

func TestLegacyAppRedirect(t *testing.T) {
	orig := utils.BasePath
	t.Cleanup(func() { utils.BasePath = orig })
	utils.BasePath = "/gotodo"

	req := httptest.NewRequest(http.MethodGet, "/gotodo/app/login", nil)
	rec := httptest.NewRecorder()
	legacyAppRedirect(rec, req)
	if rec.Code != http.StatusTemporaryRedirect {
		t.Fatalf("status = %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/gotodo/login" {
		t.Fatalf("Location = %q", loc)
	}
}

func TestServeSPAFallbackToIndex(t *testing.T) {
	dir := t.TempDir()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })

	if err := os.MkdirAll("web/dist/assets", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("web/dist/index.html", []byte(
		`<html><head><title>GoTodo</title><script type="module" src="./assets/app.js"></script><link rel="stylesheet" href="./assets/app.css"></head><body>spa</body></html>`,
	), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("web/dist/assets/app.js", []byte("console.log(1)"), 0o644); err != nil {
		t.Fatal(err)
	}

	orig := utils.BasePath
	t.Cleanup(func() { utils.BasePath = orig })
	utils.BasePath = "/gotodo"

	fs := http.StripPrefix("/gotodo/", http.FileServer(http.Dir("web/dist")))

	req := httptest.NewRequest(http.MethodGet, "/gotodo/tasks/1", nil)
	rec := httptest.NewRecorder()
	serveSPA(rec, req, "/gotodo", fs)
	if rec.Code != http.StatusOK {
		t.Fatalf("fallback status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "spa") {
		t.Fatalf("fallback body = %q", body)
	}
	if !strings.Contains(body, `name="gotodo-base" content="/gotodo/"`) {
		t.Fatalf("missing base inject in %q", body)
	}
	if !strings.Contains(body, `name="gotodo-site-name" content="GoTodo"`) {
		t.Fatalf("missing site name meta in %q", body)
	}
	if !strings.Contains(body, `<title>GoTodo</title>`) {
		t.Fatalf("missing title in %q", body)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	if strings.Contains(body, `<base href=`) {
		t.Fatalf("must not inject <base href> (breaks hash anchors): %q", body)
	}
	if !strings.Contains(body, `src="/gotodo/assets/app.js"`) || !strings.Contains(body, `href="/gotodo/assets/app.css"`) {
		t.Fatalf("relative assets not absolutized: %q", body)
	}

	req = httptest.NewRequest(http.MethodGet, "/gotodo/assets/app.js", nil)
	rec = httptest.NewRecorder()
	serveSPA(rec, req, "/gotodo", fs)
	if rec.Code != http.StatusOK {
		t.Fatalf("asset status = %d", rec.Code)
	}
	if got := rec.Body.String(); got != "console.log(1)" {
		t.Fatalf("asset body = %q", got)
	}
}

func TestNonceInlineScripts(t *testing.T) {
	in := `<head><script>var x=1</script><script type="module" src="/a.js"></script></head>`
	out := nonceInlineScripts(in, "abc123")
	if !strings.Contains(out, `<script nonce="abc123">var x=1</script>`) {
		t.Fatalf("inline script not nonced: %q", out)
	}
	if !strings.Contains(out, `<script type="module" src="/a.js"></script>`) {
		t.Fatalf("external script should be unchanged: %q", out)
	}
}

func TestAbsolutizeRelativeAssetURLs(t *testing.T) {
	in := `<script src="./assets/a.js"></script><link href='./assets/a.css'><a href="#tasks">t</a>`
	out := absolutizeRelativeAssetURLs(in, "/gotodo/")
	if out != `<script src="/gotodo/assets/a.js"></script><link href='/gotodo/assets/a.css'><a href="#tasks">t</a>` {
		t.Fatalf("got %q", out)
	}
}

func TestReplaceHTMLTitle(t *testing.T) {
	out := replaceHTMLTitle(`<html><head><title>GoTodo</title></head></html>`, `Acme & Co`)
	if out != `<html><head><title>Acme &amp; Co</title></head></html>` {
		t.Fatalf("replace existing: %q", out)
	}
	out = replaceHTMLTitle(`<html><head></head></html>`, `My Site`)
	if out != `<html><head><title>My Site</title></head></html>` {
		t.Fatalf("insert missing: %q", out)
	}
	out = replaceHTMLTitle(`<HTML><HEAD><TITLE>Old</TITLE></HEAD></HTML>`, `New`)
	if !strings.Contains(out, `<title>New</title>`) {
		t.Fatalf("case insensitive: %q", out)
	}
}
